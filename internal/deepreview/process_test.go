package deepreview

import (
	"context"
	"errors"
	"runtime"
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
	if runtime.GOOS == "windows" {
		t.Skip("unix-only process group behavior")
	}

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
