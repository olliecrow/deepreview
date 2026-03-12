package deepreview

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestCodexRunnerBuildCommandNewThread(t *testing.T) {
	runner := CodexRunner{CodexBin: "codex"}
	got := runner.buildCommand(nil)
	want := []string{
		"codex",
		"exec",
		"--skip-git-repo-check",
		"--json", "-",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected command:\n got: %#v\nwant: %#v", got, want)
	}
}

func TestCodexRunnerBuildCommandResumeThread(t *testing.T) {
	threadID := "thread-123"
	runner := CodexRunner{CodexBin: "codex"}
	got := runner.buildCommand(&threadID)
	want := []string{
		"codex",
		"exec",
		"resume", "thread-123",
		"--skip-git-repo-check",
		"--json", "-",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected command:\n got: %#v\nwant: %#v", got, want)
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

func TestCodexRunnerResolveLauncherPrefersMulticodexOverExplicitCodexFallback(t *testing.T) {
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
	if got.Display != "multicodex" {
		t.Fatalf("expected multicodex launcher to win when available, got %+v", got)
	}
	if got.Command != filepath.Join(fakeBin, "multicodex") {
		t.Fatalf("expected multicodex path, got %q", got.Command)
	}
}

func TestCodexRunnerResolveLauncherUsesExplicitCodexFallbackWhenMulticodexMissing(t *testing.T) {
	fakeBin := t.TempDir()
	codexPath := filepath.Join(fakeBin, "custom-codex")
	writeExecutable(t, codexPath, "#!/bin/sh\nexit 0\n")
	t.Setenv("PATH", fakeBin)

	runner := CodexRunner{CodexBin: codexPath}
	got, err := runner.resolveLauncher()
	if err != nil {
		t.Fatalf("resolveLauncher failed: %v", err)
	}
	if got.Display != "codex" {
		t.Fatalf("expected codex fallback launcher, got %+v", got)
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
