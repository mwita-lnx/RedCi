// Package runner executes a job's steps safely. Every step runs via
// exec.CommandContext with arguments as a list (never a shell), in its own
// process group, with a from-scratch environment and a per-step timeout.
// Resolved secrets are redacted from every emitted line.
package runner

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/mwita-lnx/RedCi/shared/jobs"
)

// LineSink receives one output line. stream is stdout, stderr or system.
type LineSink func(stream, line string)

// Options configure a Runner.
type Options struct {
	DryRun  bool     // log the command instead of running it
	PathEnv string   // value for $PATH
	Home    string   // value for $HOME
	Extra   []string // extra KEY=VALUE always added to the environment
}

// Runner executes steps.
type Runner struct {
	opts Options
}

// New builds a Runner.
func New(opts Options) *Runner {
	if opts.PathEnv == "" {
		opts.PathEnv = "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
	}
	if opts.Home == "" {
		// Default to the agent process's own $HOME, which systemd (Environment=
		// HOME=/home/<user>) and launchd both set to the service user's home.
		// This makes job steps (npm/pip/pm2/bench cache under $HOME) work for
		// whatever user the agent runs as — on Ubuntu that may be frappe, ubuntu,
		// deploy, etc., not a hardcoded /home/frappe. Falls back only if unset.
		if h := os.Getenv("HOME"); h != "" {
			opts.Home = h
		} else {
			opts.Home = "/home/frappe"
		}
	}
	return &Runner{opts: opts}
}

// Result is the outcome of a job run.
type Result struct {
	ExitCode int
	Err      error
}

// Run executes every step in order, emitting a "== step: <name>" system line
// before each. It stops at the first failing step. jobTimeout bounds the whole
// job; a step's own Timeout (if set) bounds that step. onStep is called with
// each step name so the journal can record progress.
func (r *Runner) Run(ctx context.Context, steps []jobs.Step, jobTimeout time.Duration, sink LineSink, onStep func(name string)) Result {
	ctx, cancel := context.WithTimeout(ctx, jobTimeout)
	defer cancel()

	var firstErr error
	firstCode := 0
	for _, step := range steps {
		// Once a step has failed, skip the rest EXCEPT deferred-cleanup steps
		// (AlwaysRun), e.g. turning maintenance mode back off.
		if firstErr != nil && !step.AlwaysRun {
			continue
		}
		if onStep != nil {
			onStep(step.Name)
		}
		sink("system", "== step: "+step.Name)

		if r.opts.DryRun {
			sink("system", "dry-run: "+redact(step.Command+" "+strings.Join(step.Args, " "), step.Redact))
			if step.Fallback != nil {
				sink("system", fmt.Sprintf("dry-run (fallback on %q): ", step.OnErrorContains)+redact(step.Fallback.Command+" "+strings.Join(step.Fallback.Args, " "), step.Fallback.Redact))
			}
			continue
		}
		code, err := r.runStepWithFallback(ctx, step, sink)
		if err != nil {
			sink("system", fmt.Sprintf("step %q failed: %v", step.Name, redact(err.Error(), step.Redact)))
			if firstErr == nil {
				firstErr, firstCode = err, code
			}
		}
	}
	return Result{ExitCode: firstCode, Err: firstErr}
}

// runStepWithFallback runs step; if it fails and the step declares an
// OnErrorContains + Fallback, and the captured output contained that substring,
// it runs the fallback step instead of propagating the error. This mirrors
// Press's `install-app --force` → plain `install-app` recovery on old benches.
func (r *Runner) runStepWithFallback(ctx context.Context, step jobs.Step, sink LineSink) (int, error) {
	if step.Fallback == nil || step.OnErrorContains == "" {
		code, err := r.runStep(ctx, step, sink, nil)
		return code, err
	}

	var captured strings.Builder
	code, err := r.runStep(ctx, step, sink, &captured)
	if err == nil {
		return 0, nil
	}
	if !strings.Contains(captured.String(), step.OnErrorContains) {
		return code, err // a different failure — do not recover.
	}
	sink("system", fmt.Sprintf("step %q hit %q; falling back", step.Name, step.OnErrorContains))
	sink("system", "== step: "+step.Fallback.Name)
	return r.runStep(ctx, *step.Fallback, sink, nil)
}

// resolve turns a bare command name into an absolute path by searching the
// runner's PathEnv — NOT the agent process's $PATH. This matters because
// exec.Command resolves bare names against the inherited process environment,
// which under a service manager (launchd/systemd) is a bare default that does
// not include bench/mariadb/etc. Setting cmd.Env alone does not fix lookup, so
// we resolve here and hand exec an absolute path. Absolute/relative paths and
// unresolved names pass through unchanged.
func (r *Runner) resolve(command string) string {
	if command == "" || strings.ContainsRune(command, '/') {
		return command
	}
	for _, dir := range strings.Split(r.opts.PathEnv, ":") {
		if dir == "" {
			continue
		}
		cand := dir + "/" + command
		if fi, err := os.Stat(cand); err == nil && !fi.IsDir() && fi.Mode()&0o111 != 0 {
			return cand
		}
	}
	return command // let exec surface the "not found" error
}

// LookPath reports whether a bare command resolves to an executable on the
// runner's PATH (the same PATH used to run steps). Returns false for unresolved
// names so callers can pre-check a required tool and fail with a clear message.
func (r *Runner) LookPath(command string) bool {
	if command == "" {
		return false
	}
	if strings.ContainsRune(command, '/') {
		fi, err := os.Stat(command)
		return err == nil && !fi.IsDir() && fi.Mode()&0o111 != 0
	}
	return r.resolve(command) != command
}

func (r *Runner) runStep(ctx context.Context, step jobs.Step, sink LineSink, capture *strings.Builder) (int, error) {
	stepCtx := ctx
	if step.Timeout > 0 {
		var cancel context.CancelFunc
		stepCtx, cancel = context.WithTimeout(ctx, step.Timeout)
		defer cancel()
	}

	cmd := exec.CommandContext(stepCtx, r.resolve(step.Command), step.Args...)
	if step.Dir != "" {
		cmd.Dir = step.Dir
	}
	// Environment built from scratch: nothing is inherited from the service.
	cmd.Env = append([]string{
		"PATH=" + r.opts.PathEnv,
		"HOME=" + r.opts.Home,
		"LANG=C.UTF-8",
	}, r.opts.Extra...)
	cmd.Env = append(cmd.Env, step.Env...)
	setProcAttr(cmd) // own process group (platform-specific)

	// CommandContext kills with SIGKILL on ctx cancel; we want SIGTERM first,
	// then SIGKILL, applied to the whole process group.
	cmd.Cancel = func() error { return signalGroup(cmd, syscallTERM) }
	cmd.WaitDelay = 10 * time.Second

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return -1, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return -1, err
	}
	if err := cmd.Start(); err != nil {
		return -1, err
	}

	var wg sync.WaitGroup
	var mu sync.Mutex // guards capture across the two stream goroutines
	wg.Add(2)
	go streamLines(stdout, "stdout", step.Redact, sink, &wg, capture, &mu)
	go streamLines(stderr, "stderr", step.Redact, sink, &wg, capture, &mu)
	wg.Wait()

	err = cmd.Wait()
	if err != nil {
		code := -1
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		}
		return code, err
	}
	return 0, nil
}

// streamLines reads lines, caps each at the protocol limit, redacts secrets,
// and forwards to the sink. When capture is non-nil, the redacted line is also
// appended to it (guarded by mu) so the caller can test it for a fallback
// trigger substring.
func streamLines(rc io.ReadCloser, stream string, redactVals []string, sink LineSink, wg *sync.WaitGroup, capture *strings.Builder, mu *sync.Mutex) {
	defer wg.Done()
	sc := bufio.NewScanner(rc)
	sc.Buffer(make([]byte, 0, 8192), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		if len(line) > maxLineBytes {
			line = line[:maxLineBytes]
		}
		red := redact(line, redactVals)
		if capture != nil {
			mu.Lock()
			capture.WriteString(red)
			capture.WriteByte('\n')
			mu.Unlock()
		}
		sink(stream, red)
	}
}

const maxLineBytes = 4096

// redact replaces each secret value with *** (spec: before logging/journaling).
func redact(s string, vals []string) string {
	for _, v := range vals {
		if v == "" {
			continue
		}
		s = strings.ReplaceAll(s, v, "***")
	}
	return s
}
