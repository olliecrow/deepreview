//go:build darwin || linux

package deepreview

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

func configureCommandForManagedCancellation(cmd *exec.Cmd) {
	if cmd == nil {
		return
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = commandWaitDelay
	cmd.Cancel = func() error {
		return terminateCommandProcessTree(cmd)
	}
}

func terminateCommandProcessTree(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return os.ErrProcessDone
	}
	return terminatePIDTree(cmd.Process.Pid)
}

func terminateActiveProcessByPID(pid int) {
	_ = terminatePIDTree(pid)
}

func terminatePIDTree(pid int) error {
	if pid <= 0 {
		return os.ErrProcessDone
	}
	_ = syscall.Kill(-pid, syscall.SIGKILL)
	err := syscall.Kill(pid, syscall.SIGKILL)
	if err == nil {
		return nil
	}
	if errors.Is(err, syscall.ESRCH) {
		return os.ErrProcessDone
	}
	return err
}
