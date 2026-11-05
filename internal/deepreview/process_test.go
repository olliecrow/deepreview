package deepreview

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestRunCommandContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := RunCommandContext(ctx, []string{"git", "--version"}, "", "", true, 0)
	if err == nil {
		t.Fatalf("expected canceled command error")
	}

	var commandErr *CommandExecutionError
	if !errors.As(err, &commandErr) {
		t.Fatalf("expected CommandExecutionError, got %T", err)
	}
	if !commandErr.Canceled {
		t.Fatalf("expected Canceled=true, got false")
	}
	if commandErr.Code != 130 {
		t.Fatalf("expected cancel exit code 130, got %d", commandErr.Code)
	}
}

func TestSetRunCommandContextAppliesToRunCommand(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	restore := setRunCommandContext(ctx)
	defer restore()

	_, err := RunCommand([]string{"git", "--version"}, "", "", true, 0)
	if err == nil {
		t.Fatalf("expected canceled command error")
	}

	var commandErr *CommandExecutionError
	if !errors.As(err, &commandErr) {
		t.Fatalf("expected CommandExecutionError, got %T", err)
	}
	if !commandErr.Canceled {
		t.Fatalf("expected Canceled=true, got false")
	}
}

func TestTerminateActiveCommandsStopsInFlightCommand(t *testing.T) {
	done := make(chan error, 1)
	go func() {
		_, err := RunCommandContext(context.Background(), []string{"/usr/bin/env", "sh", "-lc", "sleep 5"}, "", "", true, 0)
		done <- err
	}()

	deadline := time.Now().Add(2 * time.Second)
	for {
		activeCommandMu.Lock()
		activeCount := len(activeCommandPIDs)
		activeCommandMu.Unlock()
		if activeCount > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("command did not reach active state before timeout")
		}
		time.Sleep(20 * time.Millisecond)
	}

	terminateActiveCommands()

	select {
	case err := <-done:
		if err == nil {
			t.Fatalf("expected terminated command to return an error")
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("timed out waiting for command termination")
	}
}

func TestRunCommandContextWithEnvAndCallbacksSetsEnvironment(t *testing.T) {
	completed, err := RunCommandContextWithEnvAndCallbacks(
		context.Background(),
		[]string{"/usr/bin/env", "sh", "-lc", "printf %s \"$DEEPREVIEW_TEST_ENV\""},
		"",
		[]string{"DEEPREVIEW_TEST_ENV=expected"},
		"",
		true,
		0,
		nil,
	)
	if err != nil {
		t.Fatalf("expected command to succeed, got error: %v", err)
	}
	if got := strings.TrimSpace(completed.Stdout); got != "expected" {
		t.Fatalf("expected environment override to propagate, got %q", got)
	}
}

func TestRunCommandDiagnosticTailKeepsSuffix(t *testing.T) {
	tail := &tailBuffer{max: 8}
	tail.Write([]byte("abcdefghij"))
	tail.Write([]byte("KL"))
	if got := tail.String(); got != "efghijKL" {
		t.Fatalf("diagnostic tail = %q", got)
	}
	if len(tail.buf) > tail.max {
		t.Fatalf("diagnostic tail retained %d bytes", len(tail.buf))
	}
}

func TestRunCommandStreamCallbackKeepsDiagnosticTail(t *testing.T) {
	var seen int
	callbacks := &CommandStreamCallbacks{
		OnStdoutChunk: func(chunk []byte) {
			seen += len(chunk)
		},
	}
	completed, err := RunCommandContextWithCallbacks(
		context.Background(),
		[]string{"/bin/sh", "-c", "i=0; while [ \"$i\" -lt 400 ]; do printf '%1024s' x; i=$((i+1)); done; printf ENDMARKER"},
		"",
		"",
		true,
		5*time.Second,
		callbacks,
	)
	if err != nil {
		t.Fatalf("command failed: %v", err)
	}
	if seen <= commandDiagnosticTailBytes {
		t.Fatalf("callback saw %d bytes, want more than the diagnostic tail", seen)
	}
	if len(completed.Stdout) > commandDiagnosticTailBytes {
		t.Fatalf("retained stdout is %d bytes", len(completed.Stdout))
	}
	if !strings.HasSuffix(completed.Stdout, "ENDMARKER") {
		t.Fatalf("diagnostic tail lost the suffix")
	}
}

func TestRunCommandNonZeroExitIsCommandFailure(t *testing.T) {
	_, err := RunCommandContext(
		context.Background(),
		[]string{"/bin/sh", "-c", "printf 'out-text\\n'; printf 'err-text\\n' >&2; exit 3"},
		"",
		"",
		true,
		5*time.Second,
	)
	var commandErr *CommandExecutionError
	if !errors.As(err, &commandErr) {
		t.Fatalf("expected CommandExecutionError, got %v", err)
	}
	if commandErr.Canceled || commandErr.TimedOut {
		t.Fatalf("nonzero exit classified as cancel/timeout: %+v", commandErr)
	}
	if commandErr.Code != 3 {
		t.Fatalf("exit code = %d", commandErr.Code)
	}
	if !strings.Contains(commandErr.Stdout, "out-text") || !strings.Contains(commandErr.Stderr, "err-text") {
		t.Fatalf("stdout=%q stderr=%q", commandErr.Stdout, commandErr.Stderr)
	}
}

func TestRunCommandTimeoutIsDistinctFromCommandFailure(t *testing.T) {
	started := time.Now()
	_, err := RunCommandContext(context.Background(), []string{"/bin/sleep", "30"}, "", "", true, 200*time.Millisecond)
	elapsed := time.Since(started)
	if elapsed > 200*time.Millisecond+commandWaitDelay+2*time.Second {
		t.Fatalf("timeout returned in %s", elapsed)
	}
	var commandErr *CommandExecutionError
	if !errors.As(err, &commandErr) {
		t.Fatalf("expected CommandExecutionError, got %v", err)
	}
	if !commandErr.TimedOut || commandErr.Canceled || commandErr.Code != 124 {
		t.Fatalf("timeout classification = %+v", commandErr)
	}
}

func TestRunCommandCancelKillsProcessGroupIgnoringSIGTERM(t *testing.T) {
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "sleep.pid")
	script := filepath.Join(dir, "ignore-term.sh")
	writeShellScript(t, script, "#!/bin/sh\nset -eu\ntrap '' TERM\nprintf '%s\\n' \"$$\" > "+shellSingleQuote(pidFile)+"\nexec sleep 30\n")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	started := time.Now()
	go func() {
		_, err := RunCommandContext(ctx, []string{"/bin/sh", script}, "", "", true, 0)
		done <- err
	}()
	pid := waitForPIDFile(t, pidFile)
	cancel()
	assertCommandCanceled(t, done, started)
	assertPIDGone(t, pid)
}

func TestRunCommandCancelKillsGrandchildInGroup(t *testing.T) {
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "child.pid")
	script := filepath.Join(dir, "grandchild.sh")
	writeShellScript(t, script, "#!/bin/sh\nset -eu\npidfile=$1\nsh -c 'printf %s \"$$\" > \"$1\"; exec sleep 30' sh \"$pidfile\" &\ni=0\nwhile [ ! -s \"$pidfile\" ]; do\n  i=$((i+1))\n  if [ \"$i\" -gt 50 ]; then\n    exit 1\n  fi\n  sleep 0.05\ndone\nwait\n")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	started := time.Now()
	go func() {
		_, err := RunCommandContext(ctx, []string{"/bin/sh", script, pidFile}, "", "", true, 0)
		done <- err
	}()
	pid := waitForPIDFile(t, pidFile)
	cancel()
	assertCommandCanceled(t, done, started)
	assertPIDGone(t, pid)
}

func TestRunCommandDetachedStdoutReturnsWithinDrainBound(t *testing.T) {
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "detached.pid")
	script := filepath.Join(dir, "detach.sh")
	writeShellScript(t, script, "#!/bin/sh\nset -eu\npidfile=$1\ndetach='setsid'\nif ! command -v setsid >/dev/null 2>&1; then\n  if ! command -v perl >/dev/null 2>&1; then\n    echo 'setsid is unavailable' >&2\n    exit 1\n  fi\n  detach='perl'\nfi\nif [ \"$detach\" = setsid ]; then\n  setsid sh -c 'printf %s \"$$\" > \"$1\"; exec sleep 30' sh \"$pidfile\" &\nelse\n  perl -MPOSIX -e 'fork and exit; POSIX::setsid() or die \"setsid failed: $!\"; exec @ARGV or die $!' sh -c 'printf %s \"$$\" > \"$1\"; exec sleep 30' sh \"$pidfile\" &\nfi\ni=0\nwhile [ ! -s \"$pidfile\" ]; do\n  i=$((i+1))\n  if [ \"$i\" -gt 50 ]; then\n    echo 'detached child did not write its pid' >&2\n    exit 1\n  fi\n  sleep 0.05\ndone\nexit 0\n")
	t.Cleanup(func() {
		killPIDFile(pidFile)
	})
	started := time.Now()
	completed, err := RunCommandContext(context.Background(), []string{"/bin/sh", script, pidFile}, "", "", true, 0)
	elapsed := time.Since(started)
	if err != nil {
		t.Fatalf("detached holder should not fail the exited command: %s", describeRunPromptError(err))
	}
	if completed.ReturnCode != 0 {
		t.Fatalf("return code = %d", completed.ReturnCode)
	}
	if elapsed > commandWaitDelay+6*time.Second {
		t.Fatalf("returned in %s, want within the drain bound", elapsed)
	}
	pid := waitForPIDFile(t, pidFile)
	if killErr := syscall.Kill(pid, 0); killErr != nil {
		t.Fatalf("detached child pid %d is gone (%v); the wait should have returned while it still held the pipe", pid, killErr)
	}
}

func TestRunCommandSessionTerminateDoesNotKillOtherSession(t *testing.T) {
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "session.pid")
	script := filepath.Join(dir, "session.sh")
	writeShellScript(t, script, "#!/bin/sh\nset -eu\nprintf '%s\\n' \"$$\" > "+shellSingleQuote(pidFile)+"\nexec sleep 30\n")
	sessionA := newProcessSession()
	ctxA, cancelA := context.WithCancel(context.Background())
	defer cancelA()
	ctxA = contextWithProcessSession(ctxA, sessionA)
	done := make(chan error, 1)
	started := time.Now()
	go func() {
		_, err := RunCommandContext(ctxA, []string{"/bin/sh", script}, "", "", true, 0)
		done <- err
	}()
	pid := waitForPIDFile(t, pidFile)
	newProcessSession().terminateProcesses()
	if killErr := syscall.Kill(pid, 0); killErr != nil {
		t.Fatalf("unrelated session termination killed pid %d: %v", pid, killErr)
	}
	cancelA()
	assertCommandCanceled(t, done, started)
	assertPIDGone(t, pid)
}

func assertCommandCanceled(t *testing.T, done <-chan error, started time.Time) {
	t.Helper()
	select {
	case err := <-done:
		if time.Since(started) > commandWaitDelay+4*time.Second {
			t.Fatalf("canceled command returned in %s", time.Since(started))
		}
		var commandErr *CommandExecutionError
		if !errors.As(err, &commandErr) {
			t.Fatalf("expected CommandExecutionError, got %v", err)
		}
		if !commandErr.Canceled || commandErr.TimedOut {
			t.Fatalf("cancel classification = %+v", commandErr)
		}
	case <-time.After(commandWaitDelay + 4*time.Second):
		t.Fatal("canceled command did not return within the drain bound")
	}
}

func assertPIDGone(t *testing.T, pid int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if err := syscall.Kill(pid, 0); err != nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	_ = syscall.Kill(pid, syscall.SIGKILL)
	t.Fatalf("pid %d still running", pid)
}

func waitForPIDFile(t *testing.T, path string) int {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		if err == nil {
			pid, convErr := strconv.Atoi(strings.TrimSpace(string(data)))
			if convErr == nil && pid > 0 {
				return pid
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("pid file %s was not written", path)
	return 0
}

func killPIDFile(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		return
	}
	_ = syscall.Kill(pid, syscall.SIGKILL)
}

func writeShellScript(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}
}

func shellSingleQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}
