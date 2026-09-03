package kube

import (
	"bytes"
	"context"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/aymenkrifa/kmap/internal/config"
)

func TestArgvCommandEnvironment(t *testing.T) {
	env := config.Environment{Command: []string{"klocal"}}
	got := Argv(env, "get", "pods", "-n", "default")
	want := []string{"klocal", "get", "pods", "-n", "default"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Argv = %v, want %v", got, want)
	}
}

func TestArgvContextEnvironment(t *testing.T) {
	env := config.Environment{Context: "Production"}
	got := Argv(env, "get", "pods")
	want := []string{"kubectl", "--context", "Production", "get", "pods"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Argv = %v, want %v", got, want)
	}
}

func TestArgvMultiWordCommand(t *testing.T) {
	// a wrapper with its own arguments, e.g. `ssh box kubectl`
	env := config.Environment{Command: []string{"ssh", "box", "kubectl"}}
	got := Argv(env, "get", "pods")
	want := []string{"ssh", "box", "kubectl", "get", "pods"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Argv = %v, want %v", got, want)
	}
}

func TestArgvDoesNotAliasCommandSlice(t *testing.T) {
	env := config.Environment{Command: []string{"klocal"}}
	_ = Argv(env, "get", "pods")
	_ = Argv(env, "logs", "-f")
	if len(env.Command) != 1 || env.Command[0] != "klocal" {
		t.Fatalf("Argv mutated the configured command: %v", env.Command)
	}
}

// FakeRunner records argv and replays canned output. Used by every command test.
type FakeRunner struct {
	Calls [][]string
	Out   map[string]string // joined argv substring -> stdout
	Err   error
}

func (f *FakeRunner) Run(ctx context.Context, argv []string, stdout, stderr io.Writer) error {
	f.Calls = append(f.Calls, argv)
	joined := strings.Join(argv, " ")
	for k, v := range f.Out {
		if strings.Contains(joined, k) {
			stdout.Write([]byte(v))
			break
		}
	}
	return f.Err
}

var _ Runner = (*FakeRunner)(nil)

func TestExecRunnerRunsAndCaptures(t *testing.T) {
	var out bytes.Buffer
	r := ExecRunner{}
	if err := r.Run(context.Background(), []string{"echo", "hello"}, &out, &out); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if strings.TrimSpace(out.String()) != "hello" {
		t.Errorf("stdout = %q", out.String())
	}
}

func TestExecRunnerReportsExitCode(t *testing.T) {
	var out bytes.Buffer
	err := ExecRunner{}.Run(context.Background(), []string{"sh", "-c", "exit 7"}, &out, &out)
	if err == nil {
		t.Fatal("want error")
	}
	if got := ExitCode(err); got != 7 {
		t.Errorf("ExitCode = %d, want 7", got)
	}
}

func TestExecRunnerMissingBinary(t *testing.T) {
	var out bytes.Buffer
	err := ExecRunner{}.Run(context.Background(), []string{"kmap-no-such-binary"}, &out, &out)
	if err == nil || !strings.Contains(err.Error(), "kmap-no-such-binary") {
		t.Fatalf("want the binary named in the error, got %v", err)
	}
}
