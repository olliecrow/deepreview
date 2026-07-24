package deepreview

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"strings"
	"sync"
	"time"
)

type CompletedProcess struct {
	Stdout     string
	Stderr     string
	ReturnCode int
}

type CommandStreamCallbacks struct {
	OnStdoutChunk func(chunk []byte)
	OnStderrChunk func(chunk []byte)
}

var (
	runCommandContextMu sync.RWMutex
	runCommandContext   context.Context
	activeCommandMu     sync.Mutex
	activeCommandPIDs   = map[int]struct{}{}
)

func setRunCommandContext(ctx context.Context) func() {
	runCommandContextMu.Lock()
	previous := runCommandContext
	runCommandContext = ctx
	runCommandContextMu.Unlock()
	return func() {
		runCommandContextMu.Lock()
		runCommandContext = previous
		runCommandContextMu.Unlock()
	}
}

func currentRunCommandContext() context.Context {
	runCommandContextMu.RLock()
	ctx := runCommandContext
	runCommandContextMu.RUnlock()
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

func RunCommand(command []string, cwd string, input string, check bool, timeout time.Duration) (CompletedProcess, error) {
	return RunCommandContextWithCallbacks(currentRunCommandContext(), command, cwd, input, check, timeout, nil)
}

func RunCommandContext(parent context.Context, command []string, cwd string, input string, check bool, timeout time.Duration) (CompletedProcess, error) {
	return RunCommandContextWithCallbacks(parent, command, cwd, input, check, timeout, nil)
}

func RunCommandContextWithEnvAndCallbacks(
	parent context.Context,
	command []string,
	cwd string,
	env []string,
	input string,
	check bool,
	timeout time.Duration,
	callbacks *CommandStreamCallbacks,
) (CompletedProcess, error) {
	return runCommandInvocation(parent, command, cwd, env, input, check, timeout, callbacks)
}

func RunCommandContextWithCallbacks(
	parent context.Context,
	command []string,
	cwd string,
	input string,
	check bool,
	timeout time.Duration,
	callbacks *CommandStreamCallbacks,
) (CompletedProcess, error) {
	return runCommandInvocation(parent, command, cwd, nil, input, check, timeout, callbacks)
}

func runCommandInvocation(
	parent context.Context,
	command []string,
	cwd string,
	env []string,
	input string,
	check bool,
	timeout time.Duration,
	callbacks *CommandStreamCallbacks,
) (CompletedProcess, error) {
	if len(command) == 0 {
		return CompletedProcess{}, NewDeepReviewError("empty command")
	}

	ctx := parent
	if ctx == nil {
		ctx = context.Background()
	}
	var cancel context.CancelFunc
	if timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	if err := ctx.Err(); err != nil {
		return CompletedProcess{}, commandContextError(err, command, timeout, CompletedProcess{})
	}

	cmd := exec.Command(command[0], command[1:]...)
	configureCommandForManagedCancellation(cmd)
	if cwd != "" {
		cmd.Dir = cwd
	}
	if len(env) > 0 {
		cmd.Env = env
	}
	if input != "" {
		cmd.Stdin = strings.NewReader(input)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	stdoutWriter := &streamCaptureWriter{buffer: &stdout}
	stderrWriter := &streamCaptureWriter{buffer: &stderr}
	if callbacks != nil {
		stdoutWriter.onChunk = callbacks.OnStdoutChunk
		stderrWriter.onChunk = callbacks.OnStderrChunk
	}
	cmd.Stdout = stdoutWriter
	cmd.Stderr = stderrWriter

	if err := cmd.Start(); err != nil {
		return CompletedProcess{}, err
	}
	registerActiveCommand(cmd)
	defer unregisterActiveCommand(cmd)

	waitCh := make(chan error, 1)
	go func() {
		waitCh <- cmd.Wait()
	}()

	var waitErr error
	select {
	case waitErr = <-waitCh:
	case <-ctx.Done():
		terminateCommandProcessTree(cmd)
		waitErr = <-waitCh
	}
	code := 0
	if cmd.ProcessState != nil {
		code = cmd.ProcessState.ExitCode()
	}

	completed := CompletedProcess{
		Stdout:     stdout.String(),
		Stderr:     stderr.String(),
		ReturnCode: code,
	}

	if waitErr == nil {
		return completed, nil
	}

	timedOut := timeout > 0 && ctx.Err() == context.DeadlineExceeded
	cancelled := errors.Is(ctx.Err(), context.Canceled)
	if timedOut {
		return completed, commandContextError(context.DeadlineExceeded, command, timeout, completed)
	}
	if cancelled {
		return completed, commandContextError(context.Canceled, command, timeout, completed)
	}

	if check {
		return completed, &CommandExecutionError{
			Message:  "command failed: " + strings.Join(command, " "),
			Command:  command,
			Code:     code,
			Stdout:   completed.Stdout,
			Stderr:   completed.Stderr,
			TimedOut: false,
			Canceled: false,
		}
	}

	return completed, nil
}

type streamCaptureWriter struct {
	buffer  *bytes.Buffer
	onChunk func(chunk []byte)
}

func (w *streamCaptureWriter) Write(p []byte) (int, error) {
	if w == nil || w.buffer == nil {
		return len(p), nil
	}
	n, err := w.buffer.Write(p)
	if n > 0 && w.onChunk != nil {
		w.onChunk(p[:n])
	}
	return n, err
}

func registerActiveCommand(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil || cmd.Process.Pid <= 0 {
		return
	}
	activeCommandMu.Lock()
	activeCommandPIDs[cmd.Process.Pid] = struct{}{}
	activeCommandMu.Unlock()
}

func unregisterActiveCommand(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil || cmd.Process.Pid <= 0 {
		return
	}
	activeCommandMu.Lock()
	delete(activeCommandPIDs, cmd.Process.Pid)
	activeCommandMu.Unlock()
}

func terminateActiveCommands() {
	activeCommandMu.Lock()
	pids := make([]int, 0, len(activeCommandPIDs))
	for pid := range activeCommandPIDs {
		pids = append(pids, pid)
	}
	activeCommandMu.Unlock()

	for _, pid := range pids {
		terminateActiveProcessByPID(pid)
	}

	waitForActiveCommandsToExit(3 * time.Second)
}

func waitForActiveCommandsToExit(timeout time.Duration) {
	if timeout <= 0 {
		return
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		activeCommandMu.Lock()
		activeCount := len(activeCommandPIDs)
		activeCommandMu.Unlock()
		if activeCount == 0 {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func commandContextError(ctxErr error, command []string, timeout time.Duration, completed CompletedProcess) error {
	if errors.Is(ctxErr, context.DeadlineExceeded) {
		return &CommandExecutionError{
			Message:  "command timed out after " + timeout.String() + ": " + strings.Join(command, " "),
			Command:  command,
			Code:     124,
			Stdout:   completed.Stdout,
			Stderr:   completed.Stderr,
			TimedOut: true,
			Canceled: false,
		}
	}
	return &CommandExecutionError{
		Message:  "command canceled: " + strings.Join(command, " "),
		Command:  command,
		Code:     130,
		Stdout:   completed.Stdout,
		Stderr:   completed.Stderr,
		TimedOut: false,
		Canceled: true,
	}
}
