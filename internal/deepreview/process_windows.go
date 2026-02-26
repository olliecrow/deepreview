//go:build windows

package deepreview

import (
	"os"
	"os/exec"
)

func configureCommandForManagedCancellation(cmd *exec.Cmd) {}

func terminateCommandProcessTree(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	_ = cmd.Process.Kill()
}

func terminateActiveProcessByPID(pid int) {
	if pid <= 0 {
		return
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return
	}
	_ = process.Kill()
}
