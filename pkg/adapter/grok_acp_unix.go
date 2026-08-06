//go:build unix

package adapter

import (
	"os"
	"os/exec"
	"syscall"
)

// configureGrokACPProcess puts the child in its own process group so Close can
// signal the whole tree (OBJECTIVE Phase 3 Unix process-group cleanup).
func configureGrokACPProcess(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

// signalGrokACPProcess sends SIGINT (graceful) or SIGKILL (force) to the process group.
func signalGrokACPProcess(cmd *exec.Cmd, force bool) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	pid := cmd.Process.Pid
	sig := syscall.SIGINT
	if force {
		sig = syscall.SIGKILL
	}
	// Negative PID targets the process group when Setpgid was used.
	if err := syscall.Kill(-pid, sig); err != nil {
		// Fallback to the single process if group signal fails.
		if force {
			return cmd.Process.Kill()
		}
		return cmd.Process.Signal(os.Interrupt)
	}
	return nil
}
