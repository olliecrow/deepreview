package deepreview

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
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

func TestCodexRunnerBuildEnvironmentUsesRunScopedSandboxPaths(t *testing.T) {
	runner := CodexRunner{CodexBin: "codex"}
	td := t.TempDir()
	logPrefix := filepath.Join(td, "round-01", "execute", "prompt-01")

	env, err := runner.buildEnvironment(logPrefix)
	if err != nil {
		t.Fatalf("buildEnvironment failed: %v", err)
	}

	envMap := map[string]string{}
	for _, entry := range env {
		key, value, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		envMap[key] = value
	}

	want := map[string]string{
		"GOCACHE":    filepath.Join(td, "round-01", "execute", "runtime", "go-build-cache"),
		"GOMODCACHE": filepath.Join(td, "round-01", "execute", "runtime", "go-mod-cache"),
		"GOTMPDIR":   filepath.Join(td, "round-01", "execute", "runtime", "go-tmp"),
		"TMPDIR":     filepath.Join(td, "round-01", "execute", "runtime", "tmp"),
		"TMP":        filepath.Join(td, "round-01", "execute", "runtime", "tmp"),
		"TEMP":       filepath.Join(td, "round-01", "execute", "runtime", "tmp"),
	}
	for key, expectedPath := range want {
		if got := envMap[key]; got != expectedPath {
			t.Fatalf("expected %s=%q, got %q", key, expectedPath, got)
		}
		if info, err := os.Stat(expectedPath); err != nil || !info.IsDir() {
			t.Fatalf("expected %s directory to exist, stat err=%v", expectedPath, err)
		}
	}
}
