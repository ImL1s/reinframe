//go:build windows

package adapter

import (
	"os"
	"os/exec"
	"syscall"
)

// configureGrokACPProcess creates a new process group so tree signals can target the child.
// Full Windows Job Object binding is not required for foundation; CREATE_NEW_PROCESS_GROUP
// enables Ctrl-break style group signals without shell interpolation.
func configureGrokACPProcess(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	// CREATE_NEW_PROCESS_GROUP = 0x00000200
	cmd.SysProcAttr.CreationFlags |= 0x00000200
}

// signalGrokACPProcess interrupts or kills the process (and group when available).
func signalGrokACPProcess(cmd *exec.Cmd, force bool) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	if force {
		return cmd.Process.Kill()
	}
	// Interrupt is best-effort on Windows.
	if err := cmd.Process.Signal(os.Interrupt); err != nil {
		return cmd.Process.Kill()
	}
	return nil
}
