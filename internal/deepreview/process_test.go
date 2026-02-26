package deepreview

import (
	"context"
	"errors"
	"testing"
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
