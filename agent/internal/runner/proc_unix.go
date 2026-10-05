//go:build unix

package runner

import (
	"os/exec"
	"syscall"
)

const syscallTERM = syscall.SIGTERM

// setProcAttr puts the command in its own process group so we can signal the
// whole group (the process and any children it spawns) on timeout or cancel.
func setProcAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// signalGroup sends sig to the command's entire process group.
func signalGroup(cmd *exec.Cmd, sig syscall.Signal) error {
	if cmd.Process == nil {
		return nil
	}
	// Negative pid targets the process group.
	return syscall.Kill(-cmd.Process.Pid, sig)
}
