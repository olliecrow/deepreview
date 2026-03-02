//go:build !windows

package deepreview

import (
	"os/exec"
	"syscall"
)

func configureCommandForManagedCancellation(cmd *exec.Cmd) {
	if cmd == nil {
		return
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func terminateCommandProcessTree(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	terminatePIDTree(cmd.Process.Pid)
}

func terminateActiveProcessByPID(pid int) {
	terminatePIDTree(pid)
}

func terminatePIDTree(pid int) {
	if pid <= 0 {
		return
	}
	_ = syscall.Kill(-pid, syscall.SIGKILL)
	_ = syscall.Kill(pid, syscall.SIGKILL)
}
