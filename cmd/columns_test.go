package cmd

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestResolveColumnsDefaultsAndErrors(t *testing.T) {
	cols, needs, err := resolveColumns(nil)
	if err != nil || len(cols) != len(DefaultColumns) {
		t.Fatalf("defaults: %v %v", cols, err)
	}
	if needs&needsMetrics != 0 || needs&needsIngress != 0 {
		t.Errorf("the default columns must not cost extra calls, got needs=%b", needs)
	}

	if _, _, err := resolveColumns([]string{"alias", "nope"}); err == nil ||
		!strings.Contains(err.Error(), "unknown column") {
		t.Errorf("want an unknown-column error listing the options, got %v", err)
	}

	_, needs, err = resolveColumns([]string{"alias", "cpu", "url"})
	if err != nil {
		t.Fatal(err)
	}
	if needs&needsMetrics == 0 || needs&needsIngress == 0 {
		t.Errorf("cpu and url should declare their datasets, got needs=%b", needs)
	}
	// whitespace and case are tolerated, since these come off a command line
	if _, _, err := resolveColumns([]string{" Alias ", "URL"}); err != nil {
		t.Errorf("column names should be case- and space-insensitive: %v", err)
	}
}

func TestPodReason(t *testing.T) {
	var p podItem
	if got := podReason(p); got != dash {
		t.Errorf("a healthy pod has no reason, got %q", got)
	}

	// an init container still running: this is the state your build-on-boot
	// pods sit in, and plain phase reporting calls it "Pending"
	p.Status.InitContainerStatuses = make([]containerStatus, 2)
	p.Status.InitContainerStatuses[0].State.Terminated = &struct {
		Reason string `json:"reason"`
	}{Reason: "Completed"}
	if got := podReason(p); !strings.Contains(got, "Init:1/2") {
		t.Errorf("want Init:1/2, got %q", got)
	}

	// a crash reason wins once init is done
	p = podItem{}
	p.Status.ContainerStatuses = make([]containerStatus, 1)
	p.Status.ContainerStatuses[0].State.Waiting = &struct {
		Reason string `json:"reason"`
	}{Reason: "CrashLoopBackOff"}
	if got := podReason(p); !strings.Contains(got, "CrashLoopBackOff") {
		t.Errorf("want CrashLoopBackOff, got %q", got)
	}
}

func TestAbsentStatusDistinguishesScaledFromMissing(t *testing.T) {
	c := cell{ns: &nsData{deploys: map[string]deployInfo{"worker": {desired: 0}}}}
	c.target.Workloads = []string{"worker"}
	if got := absentStatus(c); !strings.Contains(got, "scaled to 0") {
		t.Errorf("a deployment at 0 replicas should say so, got %q", got)
	}

	c.target.Workloads = []string{"auth"}
	if got := absentStatus(c); !strings.Contains(got, "not deployed") {
		t.Errorf("a workload with no deployment should say so, got %q", got)
	}

	c.ns.deploys["auth"] = deployInfo{desired: 2}
	if got := absentStatus(c); !strings.Contains(got, "no pods") {
		t.Errorf("wanting 2 replicas but having none is a problem, got %q", got)
	}
}

func TestParsers(t *testing.T) {
	m := parseTopPods([]byte("backend-5f8-nmp5z   11m   679Mi\nqueue-abc  1m  20Mi\n\n"))
	if len(m) != 2 || m["backend-5f8-nmp5z"].cpu != "11m" || m["queue-abc"].mem != "20Mi" {
		t.Errorf("parseTopPods = %+v", m)
	}

	d := parseDeploys([]byte(`{"items":[
	  {"metadata":{"name":"worker"},"spec":{"replicas":0},"status":{}},
	  {"metadata":{"name":"queue"},"status":{"readyReplicas":1}}]}`))
	if d["worker"].desired != 0 {
		t.Errorf("worker desired = %d, want 0", d["worker"].desired)
	}
	if d["queue"].desired != 1 {
		t.Errorf("an omitted replicas field defaults to 1, got %d", d["queue"].desired)
	}

	in := parseIngresses([]byte(`{"items":[{"spec":{
	  "tls":[{}],
	  "rules":[{"host":"api.example.com","http":{"paths":[
	    {"path":"/","backend":{"service":{"name":"api-v2"}}}]}}]}}]}`))
	if len(in) != 1 || in[0].service != "api-v2" || in[0].url != "https://api.example.com/" {
		t.Errorf("parseIngresses = %+v", in)
	}
}

const podsWithIngress = `{"items":[{"metadata":{"name":"api-server-1","labels":{"app":"api-server"},
 "creationTimestamp":"2026-09-03T10:00:00Z"},
 "status":{"phase":"Running","containerStatuses":[{"ready":true,"restartCount":0}]}}]}`

func TestPodsRendersUrlAndCpuColumns(t *testing.T) {
	cfg := testConfig(t)
	f := &fakeRunner{out: map[string]string{
		"get pods": podsWithIngress,
		"top pods": "api-server-1  7m  120Mi\n",
		"get ingress": `{"items":[{"spec":{"tls":[{}],"rules":[{"host":"api.example.com",
		  "http":{"paths":[{"path":"/","backend":{"service":{"name":"api-server"}}}]}}]}}]}`,
	}}

	var out bytes.Buffer
	err := runPods(context.Background(), cfg, f, &out, "local", []string{"api"}, nil,
		[]string{"alias", "cpu", "mem", "url"})
	if err != nil {
		t.Fatal(err)
	}
	s := out.String()
	for _, want := range []string{"7m", "120Mi", "api.example.com", "https://api.example.com/"} {
		if !strings.Contains(s, want) {
			t.Errorf("output missing %q:\n%q", want, s)
		}
	}
}

// The deployment list only answers "why is this row empty", so it must not be
// fetched when every alias has pods.
func TestPodsSkipsDeploymentCallWhenNothingIsAbsent(t *testing.T) {
	cfg := testConfig(t)
	f := &fakeRunner{out: map[string]string{"get pods": podsWithIngress}}

	var out bytes.Buffer
	if err := runPods(context.Background(), cfg, f, &out, "local", []string{"api"}, nil, nil); err != nil {
		t.Fatal(err)
	}
	for _, c := range f.calls {
		if strings.Contains(strings.Join(c, " "), "get deploy") {
			t.Errorf("fetched deployments with nothing to explain: %v", f.calls)
		}
	}

	// but it is fetched when a row is empty
	f2 := &fakeRunner{out: map[string]string{"get pods": `{"items":[]}`,
		"get deploy": `{"items":[{"metadata":{"name":"api-server"},"spec":{"replicas":0}}]}`}}
	out.Reset()
	if err := runPods(context.Background(), cfg, f2, &out, "local", []string{"api"}, nil, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "scaled to 0") {
		t.Errorf("an empty row should be explained:\n%s", out.String())
	}
}
