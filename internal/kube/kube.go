// Package kube builds and executes argv against a configured environment.
// kmap never links client-go: an environment may be any command, including a
// wrapper script that is not kubectl at all.
package kube

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"

	"github.com/aymenkrifa/kmap/internal/config"
)

// Runner executes an argv. Commands depend on this interface so tests never
// contact a cluster.
type Runner interface {
	Run(ctx context.Context, argv []string, stdout, stderr io.Writer) error
}

// ExecRunner runs the argv as a child process.
type ExecRunner struct{}

func (ExecRunner) Run(ctx context.Context, argv []string, stdout, stderr io.Writer) error {
	if len(argv) == 0 {
		return errors.New("empty command")
	}
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Stdout, cmd.Stderr = stdout, stderr
	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return err
		}
		return fmt.Errorf("%s: %w", argv[0], err)
	}
	return nil
}

// Argv prefixes args with the environment's command, or with kubectl and the
// environment's context. The configured slice is never aliased.
func Argv(env config.Environment, args ...string) []string {
	var prefix []string
	if len(env.Command) > 0 {
		prefix = env.Command
	} else {
		prefix = []string{"kubectl", "--context", env.Context}
	}
	out := make([]string, 0, len(prefix)+len(args))
	out = append(out, prefix...)
	return append(out, args...)
}

// ExitCode extracts a child process exit status, or 1 for any other error.
func ExitCode(err error) int {
	if err == nil {
		return 0
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode()
	}
	return 1
}
