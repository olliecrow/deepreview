package deepreview

import (
	"strings"
	"testing"
)

func TestProgressMessageIncludesCommandSnippet(t *testing.T) {
	err := &CommandExecutionError{
		Message: "command failed: git fetch",
		Command: []string{"git", "fetch"},
		Code:    1,
		Stderr:  "fatal: unable to access repository\nextra line",
	}
	msg := progressMessage(err)
	if !strings.Contains(msg, "command failed: git fetch") {
		t.Fatalf("expected base message in progress output: %s", msg)
	}
	if !strings.Contains(msg, "fatal: unable to access repository") {
		t.Fatalf("expected stderr snippet in progress output: %s", msg)
	}
}

func TestProgressMessageFallsBackToErrorText(t *testing.T) {
	err := NewDeepReviewError("plain failure")
	msg := progressMessage(err)
	if msg != "plain failure" {
		t.Fatalf("unexpected message: %s", msg)
	}
}

func TestProgressMessageKeepsLocalPathInCommandFailure(t *testing.T) {
	err := &CommandExecutionError{
		Message: "command failed: gh pr create --body-file /Users/YOU/deepreview/runs/run-123/pr-body.md",
		Command: []string{"gh", "pr", "create"},
		Code:    1,
		Stderr:  "exit status 1",
	}
	msg := progressMessage(err)
	if !strings.Contains(msg, "/Users/YOU/deepreview/runs/run-123/pr-body.md") {
		t.Fatalf("expected local path in progress message: %s", msg)
	}
}
