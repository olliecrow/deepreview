package deepreview

import (
	"context"
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

func TestCodexRunnerResolveLauncherPrefersShellResolvedMulticodex(t *testing.T) {
	fakeBin := t.TempDir()
	writeFakeShell(t, filepath.Join(fakeBin, "zsh"))
	t.Setenv("SHELL", filepath.Join(fakeBin, "zsh"))
	t.Setenv("FAKE_SHELL_HAS_MULTICODEX", "1")

	runner := CodexRunner{CodexBin: "codex"}
	got, err := runner.resolveLauncher(context.Background())
	if err != nil {
		t.Fatalf("resolveLauncher failed: %v", err)
	}
	if got.Display != "multicodex" {
		t.Fatalf("expected multicodex launcher, got %+v", got)
	}
	if got.Command != filepath.Join(fakeBin, "zsh") {
		t.Fatalf("expected fake shell launcher command, got %q", got.Command)
	}
	if !reflect.DeepEqual(got.Args, []string{"-ic", `unsetopt monitor; multicodex "$@"`, "multicodex"}) {
		t.Fatalf("unexpected shell launcher args: %#v", got.Args)
	}
}

func TestCodexRunnerResolveLauncherPrefersPathMulticodex(t *testing.T) {
	fakeBin := t.TempDir()
	writeExecutable(t, filepath.Join(fakeBin, "multicodex"), "#!/bin/sh\nexit 0\n")
	t.Setenv("PATH", fakeBin)
	t.Setenv("SHELL", "")

	runner := CodexRunner{CodexBin: "codex"}
	got, err := runner.resolveLauncher(context.Background())
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
	t.Setenv("SHELL", "")

	runner := CodexRunner{CodexBin: "codex"}
	got, err := runner.resolveLauncher(context.Background())
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
	t.Setenv("SHELL", "")
	t.Setenv(envRequireMulticodex, "1")

	runner := CodexRunner{CodexBin: "codex"}
	_, err := runner.resolveLauncher(context.Background())
	if err == nil {
		t.Fatal("expected multicodex requirement failure")
	}
	if !strings.Contains(err.Error(), envRequireMulticodex) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCodexRunnerResolveLauncherSkipsUnsupportedShellWrapper(t *testing.T) {
	fakeBin := t.TempDir()
	writeExecutable(t, filepath.Join(fakeBin, "multicodex"), "#!/bin/sh\nexit 0\n")
	t.Setenv("PATH", fakeBin)
	t.Setenv("SHELL", "/usr/local/bin/fish")

	runner := CodexRunner{CodexBin: "codex"}
	got, err := runner.resolveLauncher(context.Background())
	if err != nil {
		t.Fatalf("resolveLauncher failed: %v", err)
	}
	if got.Display != "multicodex" {
		t.Fatalf("expected multicodex launcher, got %+v", got)
	}
	if got.Command != filepath.Join(fakeBin, "multicodex") {
		t.Fatalf("expected PATH multicodex launcher, got %q", got.Command)
	}
	if len(got.Args) != 0 {
		t.Fatalf("expected no shell wrapper args for unsupported shell, got %#v", got.Args)
	}
}

func TestCodexRunnerResolveLauncherRespectsExplicitCodexOverride(t *testing.T) {
	fakeBin := t.TempDir()
	codexPath := filepath.Join(fakeBin, "custom-codex")
	writeExecutable(t, codexPath, "#!/bin/sh\nexit 0\n")
	writeExecutable(t, filepath.Join(fakeBin, "multicodex"), "#!/bin/sh\nexit 0\n")
	writeFakeShell(t, filepath.Join(fakeBin, "zsh"))
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("SHELL", filepath.Join(fakeBin, "zsh"))
	t.Setenv("FAKE_SHELL_HAS_MULTICODEX", "1")

	runner := CodexRunner{CodexBin: codexPath}
	got, err := runner.resolveLauncher(context.Background())
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

func writeFakeShell(t *testing.T, path string) {
	t.Helper()
	script := `#!/bin/sh
set -eu
if [ "${1-}" != "-ic" ]; then
  echo "expected -ic invocation" >&2
  exit 1
fi
script="$2"
if [ "$script" = "command -v multicodex >/dev/null 2>&1" ]; then
  if [ "${FAKE_SHELL_HAS_MULTICODEX:-0}" = "1" ]; then
    exit 0
  fi
  exit 1
fi
if [ "$script" = "unsetopt monitor; multicodex \"$@\"" ]; then
  shift 3
  exec multicodex "$@"
fi
if [ "${FAKE_SHELL_HAS_MULTICODEX:-0}" != "1" ]; then
  exit 127
fi
shift 3
exec multicodex "$@"
`
	writeExecutable(t, path, script)
}
