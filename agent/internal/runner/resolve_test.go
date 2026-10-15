package runner

import (
	"os"
	"path/filepath"
	"testing"
)

// TestResolveUsesPathEnvNotProcessPath verifies the runner resolves a bare
// command name against its own PathEnv, not the process $PATH. This is the bug
// behind "bench: executable file not found" under launchd/systemd, where the
// service's process PATH is a bare default.
func TestResolveUsesPathEnvNotProcessPath(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "mytool")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\necho hi\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	// PathEnv contains the dir; process $PATH does not.
	r := New(Options{PathEnv: dir + ":/usr/bin:/bin"})

	if got := r.resolve("mytool"); got != bin {
		t.Errorf("resolve(mytool) = %q, want %q", got, bin)
	}
	// Absolute path passes through untouched.
	if got := r.resolve("/sbin/thing"); got != "/sbin/thing" {
		t.Errorf("absolute path altered: %q", got)
	}
	// Unknown command passes through so exec surfaces a normal not-found error.
	if got := r.resolve("definitely-not-a-real-binary"); got != "definitely-not-a-real-binary" {
		t.Errorf("unknown command altered: %q", got)
	}
	// A non-executable file in PathEnv is skipped.
	noexec := filepath.Join(dir, "data")
	_ = os.WriteFile(noexec, []byte("x"), 0o644)
	if got := r.resolve("data"); got != "data" {
		t.Errorf("non-executable resolved: %q", got)
	}
}
