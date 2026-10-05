package runner

import (
	"bytes"
	"context"
	"os/exec"
	"time"

	"github.com/mwita-lnx/RedCi/shared/jobs"
)

// Exec runs a single step and returns its combined output and exit code. It is
// used by custom ops (nginx, web-app) that need to inspect output and branch,
// rather than stream a fixed plan. The same safety applies: arguments as a
// list, own process group, from-scratch env, per-step timeout.
func (r *Runner) Exec(ctx context.Context, step jobs.Step, timeout time.Duration) (output string, code int, err error) {
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	if r.opts.DryRun {
		return "dry-run", 0, nil
	}
	cmd := exec.CommandContext(ctx, step.Command, step.Args...)
	if step.Dir != "" {
		cmd.Dir = step.Dir
	}
	cmd.Env = append([]string{
		"PATH=" + r.opts.PathEnv,
		"HOME=" + r.opts.Home,
		"LANG=C.UTF-8",
	}, r.opts.Extra...)
	cmd.Env = append(cmd.Env, step.Env...)
	setProcAttr(cmd)
	cmd.Cancel = func() error { return signalGroup(cmd, syscallTERM) }
	cmd.WaitDelay = 10 * time.Second

	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err = cmd.Run()
	out := redact(buf.String(), step.Redact)
	if err != nil {
		code = -1
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		}
		return out, code, err
	}
	return out, 0, nil
}

// DryRun reports whether the runner is in dry-run mode.
func (r *Runner) DryRun() bool { return r.opts.DryRun }
