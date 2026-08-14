//go:build unix

package adapter

import (
	"os"
	"os/exec"
	"syscall"
)

// codexProcPlatform holds Unix process-group ownership (Setpgid).
type codexProcPlatform struct{}

// configureCodexProcess puts the child in its own process group so Close can
// signal the whole tree (#184 / Unix process-group cleanup).
func configureCodexProcess(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

// attachCodexProcess is a no-op on Unix (Setpgid applied before Start).
func attachCodexProcess(cmd *exec.Cmd) (codexProcPlatform, error) {
	return codexProcPlatform{}, nil
}

// signalCodexProcess sends SIGINT (graceful) or SIGKILL (force) to the process group.
func signalCodexProcess(cmd *exec.Cmd, _ *codexProcPlatform, force bool) error {
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

// releaseCodexProcess has no handles to close on Unix.
func releaseCodexProcess(_ *codexProcPlatform) {}
