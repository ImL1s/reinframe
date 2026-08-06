//go:build unix

package adapter

import (
	"os"
	"os/exec"
	"syscall"
)

// grokProcPlatform holds Unix process-group ownership (Setpgid).
type grokProcPlatform struct{}

// configureGrokProcess puts the child in its own process group so Close can
// signal the whole tree (#191 / Phase 3 Unix process-group cleanup).
func configureGrokProcess(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

// attachGrokProcess is a no-op on Unix (Setpgid applied before Start).
func attachGrokProcess(cmd *exec.Cmd) (grokProcPlatform, error) {
	return grokProcPlatform{}, nil
}

// signalGrokProcess sends SIGINT (graceful) or SIGKILL (force) to the process group.
func signalGrokProcess(cmd *exec.Cmd, _ *grokProcPlatform, force bool) error {
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
		if force {
			return cmd.Process.Kill()
		}
		return cmd.Process.Signal(os.Interrupt)
	}
	return nil
}

// releaseGrokProcess has no handles to close on Unix.
func releaseGrokProcess(_ *grokProcPlatform) {}
