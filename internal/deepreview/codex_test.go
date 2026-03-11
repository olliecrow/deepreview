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

func TestCodexRunnerBuildEnvironmentUsesWorktreeLocalSandboxPaths(t *testing.T) {
	runner := CodexRunner{CodexBin: "codex"}
	cwd := t.TempDir()

	env, err := runner.buildEnvironment(cwd)
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
		"GOCACHE":    filepath.Join(cwd, ".deepreview", "runtime", "go-build-cache"),
		"GOMODCACHE": filepath.Join(cwd, ".deepreview", "runtime", "go-mod-cache"),
		"GOTMPDIR":   filepath.Join(cwd, ".deepreview", "runtime", "go-tmp"),
		"TMPDIR":     filepath.Join(cwd, ".deepreview", "runtime", "tmp"),
		"TMP":        filepath.Join(cwd, ".deepreview", "runtime", "tmp"),
		"TEMP":       filepath.Join(cwd, ".deepreview", "runtime", "tmp"),
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
