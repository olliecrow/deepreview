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
	return RunCommandContext(currentRunCommandContext(), command, cwd, input, check, timeout)
}

func RunCommandContext(parent context.Context, command []string, cwd string, input string, check bool, timeout time.Duration) (CompletedProcess, error) {
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
	if input != "" {
		cmd.Stdin = strings.NewReader(input)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		return CompletedProcess{}, err
	}
	registerActiveCommand(cmd)
	defer unregisterActiveCommand(cmd)

	waitCh := make(chan error, 1)
	go func() {
		waitCh <- cmd.Wait()
	}()

	var err error
	select {
	case err = <-waitCh:
	case <-ctx.Done():
		terminateCommandProcessTree(cmd)
		err = <-waitCh
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

	if err == nil {
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
