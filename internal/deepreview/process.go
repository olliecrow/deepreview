package deepreview

import (
	"bytes"
	"context"
	"os/exec"
	"strings"
	"time"
)

type CompletedProcess struct {
	Stdout     string
	Stderr     string
	ReturnCode int
}

func RunCommand(command []string, cwd string, input string, check bool, timeout time.Duration) (CompletedProcess, error) {
	if len(command) == 0 {
		return CompletedProcess{}, NewDeepReviewError("empty command")
	}

	ctx := context.Background()
	var cancel context.CancelFunc
	if timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	cmd := exec.CommandContext(ctx, command[0], command[1:]...)
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

	err := cmd.Run()
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
	if timedOut {
		return completed, &CommandExecutionError{
			Message:  "command timed out after " + timeout.String() + ": " + strings.Join(command, " "),
			Command:  command,
			Code:     124,
			Stdout:   completed.Stdout,
			Stderr:   completed.Stderr,
			TimedOut: true,
		}
	}

	if check {
		return completed, &CommandExecutionError{
			Message:  "command failed: " + strings.Join(command, " "),
			Command:  command,
			Code:     code,
			Stdout:   completed.Stdout,
			Stderr:   completed.Stderr,
			TimedOut: false,
		}
	}

	return completed, nil
}
