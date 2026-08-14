//go:build windows

package adapter

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// codexProcPlatform holds a Windows Job Object with KILL_ON_JOB_CLOSE so the
// entire owned process tree is reaped (#184).
type codexProcPlatform struct {
	job windows.Handle
}

// configureCodexProcess creates a new process group (interrupt surface) before Start.
func configureCodexProcess(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	// CREATE_NEW_PROCESS_GROUP = 0x00000200
	cmd.SysProcAttr.CreationFlags |= 0x00000200
}

// attachCodexProcess binds the started process to a kill-on-close Job Object.
func attachCodexProcess(cmd *exec.Cmd) (codexProcPlatform, error) {
	var plat codexProcPlatform
	if cmd == nil || cmd.Process == nil {
		return plat, fmt.Errorf("nil process")
	}
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return plat, err
	}
	var info windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(
		job,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)),
		uint32(unsafe.Sizeof(info)),
	); err != nil {
		_ = windows.CloseHandle(job)
		return plat, err
	}
	// Open a handle with rights required for job assignment.
	const access = windows.PROCESS_SET_QUOTA | windows.PROCESS_TERMINATE
	ph, err := windows.OpenProcess(access, false, uint32(cmd.Process.Pid))
	if err != nil {
		_ = windows.CloseHandle(job)
		return plat, err
	}
	err = windows.AssignProcessToJobObject(job, ph)
	_ = windows.CloseHandle(ph)
	if err != nil {
		_ = windows.CloseHandle(job)
		return plat, err
	}
	plat.job = job
	return plat, nil
}

// signalCodexProcess graceful-interrupts the root, or force-terminates the whole job tree.
func signalCodexProcess(cmd *exec.Cmd, plat *codexProcPlatform, force bool) error {
	if force {
		if plat != nil && plat.job != 0 {
			// Terminate entire job tree (parent + children).
			if err := windows.TerminateJobObject(plat.job, 1); err != nil {
				if cmd != nil && cmd.Process != nil {
					return cmd.Process.Kill()
				}
				return err
			}
			return nil
		}
		if cmd != nil && cmd.Process != nil {
			return cmd.Process.Kill()
		}
		return nil
	}
	// Graceful: best-effort interrupt of the root process.
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	if err := cmd.Process.Signal(os.Interrupt); err != nil {
		// Fall through to force on next escalation; root kill is last resort here.
		return cmd.Process.Kill()
	}
	return nil
}

// releaseCodexProcess closes the job handle; with KILL_ON_JOB_CLOSE any remaining
// members are terminated.
func releaseCodexProcess(plat *codexProcPlatform) {
	if plat == nil || plat.job == 0 {
		return
	}
	_ = windows.CloseHandle(plat.job)
	plat.job = 0
}
