package deepreview

import (
	"reflect"
	"testing"
)

func TestCodexRunnerBuildCommandNewThread(t *testing.T) {
	runner := CodexRunner{
		CodexBin:   "codex",
		CodexModel: "gpt-5.4",
		Reasoning:  "high",
	}
	got := runner.buildCommand(nil)
	want := []string{
		"codex",
		"exec",
		"--model", "gpt-5.4",
		"-c", `model_reasoning_effort="high"`,
		"--skip-git-repo-check",
		"--full-auto", "--json", "-",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected command:\n got: %#v\nwant: %#v", got, want)
	}
}

func TestCodexRunnerBuildCommandResumeThread(t *testing.T) {
	threadID := "thread-123"
	runner := CodexRunner{
		CodexBin:   "codex",
		CodexModel: "gpt-5.4",
		Reasoning:  "high",
	}
	got := runner.buildCommand(&threadID)
	want := []string{
		"codex",
		"exec",
		"resume", "thread-123",
		"--model", "gpt-5.4",
		"-c", `model_reasoning_effort="high"`,
		"--skip-git-repo-check",
		"--full-auto", "--json", "-",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected command:\n got: %#v\nwant: %#v", got, want)
	}
}

func TestCodexRunnerBuildCommandForcesPinnedModelAndReasoning(t *testing.T) {
	runner := CodexRunner{
		CodexBin:   "codex",
		CodexModel: "other-model",
		Reasoning:  "low",
	}
	got := runner.buildCommand(nil)
	want := []string{
		"codex",
		"exec",
		"--model", "gpt-5.4",
		"-c", `model_reasoning_effort="high"`,
		"--skip-git-repo-check",
		"--full-auto", "--json", "-",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected command:\n got: %#v\nwant: %#v", got, want)
	}
}
