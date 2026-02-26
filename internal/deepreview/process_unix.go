//go:build !windows

package deepreview

import (
	"os/exec"
	"syscall"
	"time"
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
	_ = syscall.Kill(-pid, syscall.SIGTERM)
	_ = syscall.Kill(pid, syscall.SIGTERM)
	deadline := time.Now().Add(700 * time.Millisecond)
	for time.Now().Before(deadline) {
		if !isProcessPIDAlive(pid) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	_ = syscall.Kill(-pid, syscall.SIGKILL)
	_ = syscall.Kill(pid, syscall.SIGKILL)
}

func isProcessPIDAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, syscall.Signal(0))
	if err == nil {
		return true
	}
	return err == syscall.EPERM
}
