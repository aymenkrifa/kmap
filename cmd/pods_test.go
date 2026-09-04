package cmd

import (
	"bytes"
	"context"
	"errors"
	"io"
	"reflect"
	"strings"
	"sync"
	"testing"
)

// fakeRunner records argv and replays canned stdout. logs runs one goroutine
// per stream, so recording has to be safe under concurrency.
type fakeRunner struct {
	mu    sync.Mutex
	calls [][]string
	out   map[string]string
	fail  map[string]error
}

func (f *fakeRunner) Run(_ context.Context, argv []string, stdout, _ io.Writer) error {
	f.mu.Lock()
	f.calls = append(f.calls, argv)
	f.mu.Unlock()
	joined := strings.Join(argv, " ")
	for k, err := range f.fail {
		if strings.Contains(joined, k) {
			return err
		}
	}
	for k, v := range f.out {
		if strings.Contains(joined, k) {
			io.WriteString(stdout, v)
			return nil
		}
	}
	io.WriteString(stdout, `{"items":[]}`)
	return nil
}

const twoPods = `{"items":[
 {"metadata":{"name":"api-server-abc","labels":{"app":"api-server"},
   "creationTimestamp":"2026-09-02T10:00:00Z"},
  "status":{"phase":"Running","containerStatuses":[{"ready":true,"restartCount":0}]}},
 {"metadata":{"name":"worker-xyz","labels":{"app":"worker"},
   "creationTimestamp":"2026-09-03T10:00:00Z"},
  "status":{"phase":"Running","containerStatuses":[{"ready":false,"restartCount":4}]}}
]}`

func TestPodsBatchesOneCallPerNamespace(t *testing.T) {
	cfg := testConfig(t)
	f := &fakeRunner{out: map[string]string{"get pods": twoPods}}

	var out bytes.Buffer
	if err := runPods(context.Background(), cfg, f, &out, "local", []string{"api", "worker"}, nil, nil); err != nil {
		t.Fatal(err)
	}

	// api and worker both live in the default namespace: one get pods call, not two
	gets := 0
	for _, c := range f.calls {
		if strings.Contains(strings.Join(c, " "), "get pods") {
			gets++
		}
	}
	if gets != 1 {
		t.Errorf("made %d `get pods` calls, want 1 (batched by namespace)", gets)
	}
}

func TestPodsRendersRowPerAlias(t *testing.T) {
	cfg := testConfig(t)
	f := &fakeRunner{out: map[string]string{"get pods": twoPods}}

	var out bytes.Buffer
	if err := runPods(context.Background(), cfg, f, &out, "local", []string{"api", "worker"}, nil, nil); err != nil {
		t.Fatal(err)
	}
	s := out.String()
	for _, want := range []string{"api", "api-server", "Running", "worker", "0/1", "4"} {
		if !strings.Contains(s, want) {
			t.Errorf("output missing %q:\n%s", want, s)
		}
	}
}

func TestPodsShowsDashForAbsentWorkload(t *testing.T) {
	cfg := testConfig(t)
	f := &fakeRunner{out: map[string]string{"get pods": `{"items":[]}`}}

	var out bytes.Buffer
	if err := runPods(context.Background(), cfg, f, &out, "local", []string{"api"}, nil, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "—") {
		t.Errorf("absent workload should render a dash:\n%s", out.String())
	}
}

func TestPodsPassesThroughFlags(t *testing.T) {
	cfg := testConfig(t)
	f := &fakeRunner{out: map[string]string{"get pods": twoPods}}

	var out bytes.Buffer
	if err := runPods(context.Background(), cfg, f, &out, "local", []string{"api"}, []string{"--show-labels"}, nil); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, c := range f.calls {
		if strings.Contains(strings.Join(c, " "), "--show-labels") {
			found = true
		}
	}
	if !found {
		t.Errorf("passthrough flag missing from argv: %v", f.calls)
	}
}

// Ruling R3: kmap always asks kubectl for JSON and renders its own table, so a
// user's -o would be sent twice and kubectl would fail. It is dropped, loudly.
func TestStripOutputFlag(t *testing.T) {
	cases := []struct {
		in      []string
		want    []string
		dropped bool
	}{
		{[]string{"-o", "wide"}, []string{}, true},
		{[]string{"--output", "wide"}, []string{}, true},
		{[]string{"-o=wide"}, []string{}, true},
		{[]string{"--output=json"}, []string{}, true},
		{[]string{"--show-labels"}, []string{"--show-labels"}, false},
		{[]string{"-o", "wide", "--show-labels"}, []string{"--show-labels"}, true},
		{[]string{"--show-labels", "-o", "wide"}, []string{"--show-labels"}, true},
		{[]string{"-l", "app=x"}, []string{"-l", "app=x"}, false},
	}
	for _, c := range cases {
		got, dropped := stripOutputFlag(c.in)
		if !reflect.DeepEqual(got, c.want) || dropped != c.dropped {
			t.Errorf("stripOutputFlag(%v) = %v,%v want %v,%v", c.in, got, dropped, c.want, c.dropped)
		}
	}
}

func TestPodsDropsOutputFlagAndSaysSo(t *testing.T) {
	cfg := testConfig(t)
	f := &fakeRunner{out: map[string]string{"get pods": twoPods}}

	var out bytes.Buffer
	if err := runPods(context.Background(), cfg, f, &out, "local", []string{"api"}, []string{"-o", "wide"}, nil); err != nil {
		t.Fatal(err)
	}
	argv := strings.Join(f.calls[0], " ")
	if strings.Count(argv, "-o ") != 1 {
		t.Errorf("argv should carry exactly one -o (kmap's own json): %q", argv)
	}
	if !strings.Contains(argv, "-o json") {
		t.Errorf("kmap must still request json: %q", argv)
	}
	if !strings.Contains(out.String(), "-o") {
		t.Errorf("dropping -o should be announced:\n%s", out.String())
	}
}

// The shell predecessor took the refresh interval as a bare trailing number:
// `pods staging search -f 5`. Keep that, but never shadow a real alias.
func TestTakeInterval(t *testing.T) {
	cfg := testConfig(t)
	if got, n := takeInterval(cfg, []string{"api", "5"}, 2); !reflect.DeepEqual(got, []string{"api"}) || n != 5 {
		t.Errorf("got %v %d, want [api] 5", got, n)
	}
	if got, n := takeInterval(cfg, []string{"api"}, 2); !reflect.DeepEqual(got, []string{"api"}) || n != 2 {
		t.Errorf("got %v %d, want [api] 2", got, n)
	}
	if got, n := takeInterval(cfg, []string{"api", "worker"}, 2); len(got) != 2 || n != 2 {
		t.Errorf("got %v %d, want both aliases kept", got, n)
	}
}

// Cobra cannot both accept kmap's flags after the positionals and forward
// unknown flags to kubectl, so pods parses its own. These pin that grammar.
func TestParsePodsFlags(t *testing.T) {
	cases := []struct {
		name     string
		in       []string
		watch    bool
		interval int
		rest     []string
	}{
		{"bare", []string{"api"}, false, 2, []string{"api"}},
		{"trailing watch", []string{"api", "-w"}, true, 2, []string{"api"}},
		{"trailing follow", []string{"staging", "search", "-f"}, true, 2, []string{"staging", "search"}},
		{"interval flag", []string{"api", "-w", "--interval", "5"}, true, 5, []string{"api"}},
		{"interval equals", []string{"api", "-w", "--interval=7"}, true, 7, []string{"api"}},
		{"kubectl flag survives", []string{"api", "-l", "app=x"}, false, 2, []string{"api", "-l", "app=x"}},
		{"kubectl output flag survives", []string{"prod", "api", "-o", "wide"}, false, 2, []string{"prod", "api", "-o", "wide"}},
		{"dash dash hands over", []string{"api", "--", "-f", "x"}, false, 2, []string{"api", "-f", "x"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			opts, rest, err := parsePodsFlags(c.in)
			if err != nil {
				t.Fatal(err)
			}
			if opts.watch != c.watch || opts.interval != c.interval || !reflect.DeepEqual(rest, c.rest) {
				t.Errorf("parsePodsFlags(%v) = watch:%v interval:%d rest:%v; want %v %d %v",
					c.in, opts.watch, opts.interval, rest, c.watch, c.interval, c.rest)
			}
		})
	}
}

func TestParsePodsFlagsRejectsBadInterval(t *testing.T) {
	for _, in := range [][]string{{"--interval"}, {"--interval", "nope"}, {"--interval=0"}} {
		if _, _, err := parsePodsFlags(in); err == nil {
			t.Errorf("parsePodsFlags(%v) should have failed", in)
		}
	}
}

// A namespace where kmap may list pods but not ingresses is normal: RBAC is
// granted per resource. One forbidden resource must cost its own column, not
// the whole table.
func TestFetchNSDataDegradesWhenAResourceIsForbidden(t *testing.T) {
	f := &fakeRunner{
		out:  map[string]string{"get deploy": `{"items":[{"metadata":{"name":"queue"},"spec":{"replicas":0}}]}`},
		fail: map[string]error{"get ingress": errors.New("exit status 1")},
	}
	er := envRunner{argv: func(a ...string) []string { return append([]string{"kubectl"}, a...) }}

	d := fetchNSData(context.Background(), f, er, "default", needsDeploys|needsIngress)
	if got, ok := d.deploys["queue"]; !ok || got.desired != 0 {
		t.Errorf("deploys = %v, want the readable resource still parsed", d.deploys)
	}
	if d.missing&needsIngress == 0 {
		t.Error("ingress failure was not recorded in missing")
	}
	if d.missing&needsDeploys != 0 {
		t.Error("deploys marked missing though the call succeeded")
	}
}

func TestPodsReportsWhatItCouldNotRead(t *testing.T) {
	cfg := testConfig(t)
	f := &fakeRunner{
		out:  map[string]string{"get pods": twoPods},
		fail: map[string]error{"get ingress": errors.New("exit status 1")},
	}

	var out bytes.Buffer
	err := runPods(context.Background(), cfg, f, &out, "local",
		[]string{"api"}, nil, []string{"alias", "ready", "docs"})
	if err != nil {
		t.Fatalf("forbidden ingress must not abort the table: %v", err)
	}
	s := out.String()
	if !strings.Contains(s, "api") || !strings.Contains(s, "1/1") {
		t.Errorf("the table did not render:\n%s", s)
	}
	if !strings.Contains(s, "ingress") {
		t.Errorf("no note explaining the empty DOCS column:\n%s", s)
	}
}
