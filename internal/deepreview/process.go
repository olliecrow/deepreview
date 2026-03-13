package deepreview

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
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
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return CompletedProcess{}, err
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return CompletedProcess{}, err
	}

	if err := cmd.Start(); err != nil {
		return CompletedProcess{}, err
	}
	registerActiveCommand(cmd)
	defer unregisterActiveCommand(cmd)

	stdoutDone := make(chan error, 1)
	stderrDone := make(chan error, 1)
	go func() {
		writer := &streamCaptureWriter{
			buffer:  &stdout,
			onChunk: nil,
		}
		if callbacks != nil {
			writer.onChunk = callbacks.OnStdoutChunk
		}
		_, copyErr := io.Copy(writer, stdoutPipe)
		stdoutDone <- copyErr
	}()
	go func() {
		writer := &streamCaptureWriter{
			buffer:  &stderr,
			onChunk: nil,
		}
		if callbacks != nil {
			writer.onChunk = callbacks.OnStderrChunk
		}
		_, copyErr := io.Copy(writer, stderrPipe)
		stderrDone <- copyErr
	}()

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
	stdoutErr := <-stdoutDone
	stderrErr := <-stderrDone
	code := 0
	if cmd.ProcessState != nil {
		code = cmd.ProcessState.ExitCode()
	}

	completed := CompletedProcess{
		Stdout:     stdout.String(),
		Stderr:     stderr.String(),
		ReturnCode: code,
	}

	if stdoutErr != nil && !isIgnorableStreamCopyError(stdoutErr) {
		return completed, stdoutErr
	}
	if stderrErr != nil && !isIgnorableStreamCopyError(stderrErr) {
		return completed, stderrErr
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

func isIgnorableStreamCopyError(err error) bool {
	if err == nil {
		return true
	}
	if errors.Is(err, os.ErrClosed) {
		return true
	}
	lower := strings.ToLower(strings.TrimSpace(err.Error()))
	if strings.Contains(lower, "file already closed") {
		return true
	}
	if strings.Contains(lower, "use of closed file") {
		return true
	}
	return false
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
