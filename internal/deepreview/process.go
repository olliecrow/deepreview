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

const (
	commandWaitDelay           = 2 * time.Second
	commandDiagnosticTailBytes = 256 << 10
)

type processSession struct {
	mu   sync.Mutex
	pids map[int]struct{}
}

type processSessionKey struct{}

func newProcessSession() *processSession {
	return &processSession{pids: map[int]struct{}{}}
}

func contextWithProcessSession(ctx context.Context, session *processSession) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if session == nil {
		return ctx
	}
	return context.WithValue(ctx, processSessionKey{}, session)
}

func processSessionFromContext(ctx context.Context) *processSession {
	if ctx == nil {
		return nil
	}
	session, _ := ctx.Value(processSessionKey{}).(*processSession)
	return session
}

func (s *processSession) register(cmd *exec.Cmd) {
	if s == nil || cmd == nil || cmd.Process == nil || cmd.Process.Pid <= 0 {
		return
	}
	s.mu.Lock()
	s.pids[cmd.Process.Pid] = struct{}{}
	s.mu.Unlock()
}

func (s *processSession) unregister(cmd *exec.Cmd) {
	if s == nil || cmd == nil || cmd.Process == nil || cmd.Process.Pid <= 0 {
		return
	}
	s.mu.Lock()
	delete(s.pids, cmd.Process.Pid)
	s.mu.Unlock()
}

func (s *processSession) terminateProcesses() {
	if s == nil {
		return
	}
	s.mu.Lock()
	pids := make([]int, 0, len(s.pids))
	for pid := range s.pids {
		pids = append(pids, pid)
	}
	s.mu.Unlock()
	for _, pid := range pids {
		terminateActiveProcessByPID(pid)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		activeCount := len(s.pids)
		s.mu.Unlock()
		if activeCount == 0 {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
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

	cmd := exec.CommandContext(ctx, command[0], command[1:]...)
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

	retainFull := callbacks == nil
	stdoutWriter := newStreamCaptureWriter(retainFull)
	stderrWriter := newStreamCaptureWriter(retainFull)
	if callbacks != nil {
		stdoutWriter.onChunk = callbacks.OnStdoutChunk
		stderrWriter.onChunk = callbacks.OnStderrChunk
	}
	cmd.Stdout = stdoutWriter
	cmd.Stderr = stderrWriter

	if err := cmd.Start(); err != nil {
		if ctx.Err() != nil {
			return CompletedProcess{}, commandContextError(ctx.Err(), command, timeout, CompletedProcess{})
		}
		return CompletedProcess{}, err
	}
	if session := processSessionFromContext(ctx); session != nil {
		session.register(cmd)
		defer session.unregister(cmd)
	} else {
		registerActiveCommand(cmd)
		defer unregisterActiveCommand(cmd)
	}

	waitErr := cmd.Wait()
	code := 0
	if cmd.ProcessState != nil {
		code = cmd.ProcessState.ExitCode()
	}
	completed := CompletedProcess{
		Stdout:     stdoutWriter.String(),
		Stderr:     stderrWriter.String(),
		ReturnCode: code,
	}
	if ctx.Err() != nil {
		return completed, commandContextError(ctx.Err(), command, timeout, completed)
	}
	if waitErr == nil || (errors.Is(waitErr, exec.ErrWaitDelay) && cmd.ProcessState != nil && cmd.ProcessState.Success()) {
		return completed, nil
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
	retainFull bool
	buffer     *bytes.Buffer
	tail       *tailBuffer
	onChunk    func(chunk []byte)
}

func newStreamCaptureWriter(retainFull bool) *streamCaptureWriter {
	writer := &streamCaptureWriter{retainFull: retainFull}
	if retainFull {
		writer.buffer = &bytes.Buffer{}
	} else {
		writer.tail = &tailBuffer{max: commandDiagnosticTailBytes}
	}
	return writer
}

func (w *streamCaptureWriter) Write(p []byte) (int, error) {
	if w == nil {
		return len(p), nil
	}
	if w.retainFull {
		if w.buffer != nil {
			_, _ = w.buffer.Write(p)
		}
	} else if w.tail != nil {
		w.tail.Write(p)
	}
	if len(p) > 0 && w.onChunk != nil {
		w.onChunk(p)
	}
	return len(p), nil
}

func (w *streamCaptureWriter) String() string {
	if w == nil {
		return ""
	}
	if w.retainFull && w.buffer != nil {
		return w.buffer.String()
	}
	if w.tail != nil {
		return w.tail.String()
	}
	return ""
}

type tailBuffer struct {
	max int
	buf []byte
}

func (t *tailBuffer) Write(p []byte) {
	if t == nil || t.max <= 0 || len(p) == 0 {
		return
	}
	if len(p) >= t.max {
		t.buf = append(t.buf[:0], p[len(p)-t.max:]...)
		return
	}
	t.buf = append(t.buf, p...)
	if len(t.buf) > t.max {
		overflow := len(t.buf) - t.max
		t.buf = append([]byte(nil), t.buf[overflow:]...)
	}
}

func (t *tailBuffer) String() string {
	if t == nil {
		return ""
	}
	return string(t.buf)
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
