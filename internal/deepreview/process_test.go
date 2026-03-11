package deepreview

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
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

func TestIsIgnorableStreamCopyError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil", err: nil, want: true},
		{name: "os err closed", err: os.ErrClosed, want: true},
		{name: "file already closed text", err: fmt.Errorf("read |0: file already closed"), want: true},
		{name: "use of closed file text", err: fmt.Errorf("write: use of closed file"), want: true},
		{name: "other", err: fmt.Errorf("unexpected stream failure"), want: false},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got := isIgnorableStreamCopyError(tc.err)
			if got != tc.want {
				t.Fatalf("isIgnorableStreamCopyError(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
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
