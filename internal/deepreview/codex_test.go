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

func TestCodexRunnerResolveLauncherPrefersPathMulticodex(t *testing.T) {
	fakeBin := t.TempDir()
	writeExecutable(t, filepath.Join(fakeBin, "multicodex"), "#!/bin/sh\nexit 0\n")
	t.Setenv("PATH", fakeBin)

	runner := CodexRunner{CodexBin: "codex"}
	got, err := runner.resolveLauncher()
	if err != nil {
		t.Fatalf("resolveLauncher failed: %v", err)
	}
	if got.Display != "multicodex" {
		t.Fatalf("expected multicodex launcher, got %+v", got)
	}
	if got.Command != filepath.Join(fakeBin, "multicodex") {
		t.Fatalf("expected PATH multicodex launcher, got %q", got.Command)
	}
}

func TestCodexRunnerResolveLauncherFallsBackToCodex(t *testing.T) {
	fakeBin := t.TempDir()
	writeExecutable(t, filepath.Join(fakeBin, "codex"), "#!/bin/sh\nexit 0\n")
	t.Setenv("PATH", fakeBin)

	runner := CodexRunner{CodexBin: "codex"}
	got, err := runner.resolveLauncher()
	if err != nil {
		t.Fatalf("resolveLauncher failed: %v", err)
	}
	if got.Display != "codex" {
		t.Fatalf("expected codex launcher, got %+v", got)
	}
	if got.Command != filepath.Join(fakeBin, "codex") {
		t.Fatalf("expected codex launcher path, got %q", got.Command)
	}
}

func TestCodexRunnerResolveLauncherRequiresMulticodex(t *testing.T) {
	fakeBin := t.TempDir()
	writeExecutable(t, filepath.Join(fakeBin, "codex"), "#!/bin/sh\nexit 0\n")
	t.Setenv("PATH", fakeBin)
	t.Setenv(envRequireMulticodex, "1")

	runner := CodexRunner{CodexBin: "codex"}
	_, err := runner.resolveLauncher()
	if err == nil {
		t.Fatal("expected multicodex requirement failure")
	}
	if !strings.Contains(err.Error(), envRequireMulticodex) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCodexRunnerResolveLauncherRespectsExplicitCodexOverride(t *testing.T) {
	fakeBin := t.TempDir()
	codexPath := filepath.Join(fakeBin, "custom-codex")
	writeExecutable(t, codexPath, "#!/bin/sh\nexit 0\n")
	writeExecutable(t, filepath.Join(fakeBin, "multicodex"), "#!/bin/sh\nexit 0\n")
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))

	runner := CodexRunner{CodexBin: codexPath}
	got, err := runner.resolveLauncher()
	if err != nil {
		t.Fatalf("resolveLauncher failed: %v", err)
	}
	if got.Display != "codex" {
		t.Fatalf("expected explicit codex override to win, got %+v", got)
	}
	if got.Command != codexPath {
		t.Fatalf("expected custom codex path, got %q", got.Command)
	}
}

func writeExecutable(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("write executable %s: %v", path, err)
	}
}
