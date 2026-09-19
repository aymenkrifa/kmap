package cmd

import (
	"bytes"
	"strings"
	"testing"
)

// TestResolveVersionPrefersTheLdflagsOverride pins the precedence
// resolveVersion documents: a release build's -ldflags value must win over
// whatever runtime/debug.ReadBuildInfo reports, not the other way around.
func TestResolveVersionPrefersTheLdflagsOverride(t *testing.T) {
	old := version
	version = "v1.2.3"
	t.Cleanup(func() { version = old })
	if got := resolveVersion(); got != "v1.2.3" {
		t.Errorf("resolveVersion() = %q, want %q", got, "v1.2.3")
	}
}

// TestResolveVersionNeverReturnsEmpty covers the go-install path, where no
// ldflags value was injected: the installer parses this output, so an empty
// line would make it report an upgrade from nothing to nothing.
func TestResolveVersionNeverReturnsEmpty(t *testing.T) {
	old := version
	version = ""
	t.Cleanup(func() { version = old })
	if strings.TrimSpace(resolveVersion()) == "" {
		t.Error("resolveVersion() is empty with no ldflags override")
	}
}

// TestVersionCommandPrintsTheResolvedValue closes the gap between "version
// prints something" and "version prints resolveVersion()'s value" — a command
// printing a hard-coded placeholder would still look fine otherwise.
func TestVersionCommandPrintsTheResolvedValue(t *testing.T) {
	old := version
	version = "v9.9.9-test"
	t.Cleanup(func() { version = old })

	cmd := newVersionCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(nil)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v\n%s", err, out.String())
	}
	if got := strings.TrimSpace(out.String()); got != "v9.9.9-test" {
		t.Errorf("version = %q, want %q", got, "v9.9.9-test")
	}
}
