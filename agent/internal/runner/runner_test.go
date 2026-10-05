package runner

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/mwita-lnx/RedCi/shared/jobs"
)

// TestAlwaysRunAfterFailure confirms a failing step stops normal steps but
// deferred-cleanup (AlwaysRun) steps still run.
func TestAlwaysRunAfterFailure(t *testing.T) {
	r := New(Options{})
	var ran []string
	steps := []jobs.Step{
		{Name: "ok", Command: "true"},
		{Name: "boom", Command: "false"},       // fails
		{Name: "skipped", Command: "true"},     // must be skipped
		{Name: "cleanup", Command: "true", AlwaysRun: true}, // must run
	}
	sink := func(stream, line string) {
		if strings.HasPrefix(line, "== step: ") {
			ran = append(ran, strings.TrimPrefix(line, "== step: "))
		}
	}
	res := r.Run(context.Background(), steps, 10*time.Second, sink, nil)
	if res.Err == nil {
		t.Fatal("expected failure from the false step")
	}
	got := strings.Join(ran, ",")
	if got != "ok,boom,cleanup" {
		t.Fatalf("ran = %q, want ok,boom,cleanup (skipped must not run)", got)
	}
}

// TestRedaction confirms secret values never reach the sink.
func TestRedaction(t *testing.T) {
	r := New(Options{})
	var lines []string
	sink := func(stream, line string) { lines = append(lines, line) }
	steps := []jobs.Step{
		{Name: "echo", Command: "printf", Args: []string{"token=SECRET123\n"}, Redact: []string{"SECRET123"}},
	}
	res := r.Run(context.Background(), steps, 10*time.Second, sink, nil)
	if res.Err != nil {
		t.Fatalf("unexpected err: %v", res.Err)
	}
	for _, l := range lines {
		if strings.Contains(l, "SECRET123") {
			t.Fatalf("secret leaked: %q", l)
		}
	}
	// And the redacted marker should appear.
	var sawRedacted bool
	for _, l := range lines {
		if strings.Contains(l, "***") {
			sawRedacted = true
		}
	}
	if !sawRedacted {
		t.Error("expected *** in output")
	}
}
