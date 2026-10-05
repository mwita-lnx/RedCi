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
		opts.Home = "/home/frappe"
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
			continue
		}
		code, err := r.runStep(ctx, step, sink)
		if err != nil {
			sink("system", fmt.Sprintf("step %q failed: %v", step.Name, redact(err.Error(), step.Redact)))
			if firstErr == nil {
				firstErr, firstCode = err, code
			}
		}
	}
	return Result{ExitCode: firstCode, Err: firstErr}
}

func (r *Runner) runStep(ctx context.Context, step jobs.Step, sink LineSink) (int, error) {
	stepCtx := ctx
	if step.Timeout > 0 {
		var cancel context.CancelFunc
		stepCtx, cancel = context.WithTimeout(ctx, step.Timeout)
		defer cancel()
	}

	cmd := exec.CommandContext(stepCtx, step.Command, step.Args...)
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
	wg.Add(2)
	go streamLines(stdout, "stdout", step.Redact, sink, &wg)
	go streamLines(stderr, "stderr", step.Redact, sink, &wg)
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
// and forwards to the sink.
func streamLines(rc io.ReadCloser, stream string, redactVals []string, sink LineSink, wg *sync.WaitGroup) {
	defer wg.Done()
	sc := bufio.NewScanner(rc)
	sc.Buffer(make([]byte, 0, 8192), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		if len(line) > maxLineBytes {
			line = line[:maxLineBytes]
		}
		sink(stream, redact(line, redactVals))
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
