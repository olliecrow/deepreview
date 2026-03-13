package deepreview

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
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
	runner := CodexRunner{CodexBin: "codex"}
	got := runner.buildCommand(&CodexContext{ThreadID: "thread-123"})
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

func TestCodexRunnerBuildCommandWithPinnedMulticodexProfile(t *testing.T) {
	runner := CodexRunner{CodexBin: "codex"}
	launcher := codexLauncher{
		Command: "multicodex",
		Args:    []string{"run", "beta", "--", "codex"},
		Display: "multicodex",
	}
	got := runner.buildCommandWithLauncher(launcher, &CodexContext{
		ThreadID:          "thread-123",
		MulticodexProfile: "beta",
	})
	want := []string{
		"multicodex",
		"run", "beta", "--", "codex",
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
	t.Setenv(envRequireMulticodex, "")
	fakeBin := t.TempDir()
	writeExecutable(t, filepath.Join(fakeBin, "multicodex"), "#!/bin/sh\nexit 0\n")
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))

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
	t.Setenv(envRequireMulticodex, "")
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
	t.Setenv(envRequireMulticodex, "")
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
	t.Setenv(envRequireMulticodex, "")
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

func TestCodexRunnerResolveLauncherForPinnedMulticodexProfile(t *testing.T) {
	fakeBin := t.TempDir()
	writeExecutable(t, filepath.Join(fakeBin, "multicodex"), "#!/bin/sh\nexit 0\n")
	t.Setenv("PATH", fakeBin)

	runner := CodexRunner{CodexBin: "codex"}
	got, err := runner.resolveLauncherForContext(&CodexContext{MulticodexProfile: "beta"})
	if err != nil {
		t.Fatalf("resolveLauncherForContext failed: %v", err)
	}
	if got.Display != "multicodex" {
		t.Fatalf("expected multicodex launcher, got %+v", got)
	}
	wantArgs := []string{"run", "beta", "--", "codex"}
	if !reflect.DeepEqual(got.Args, wantArgs) {
		t.Fatalf("unexpected launcher args: got=%#v want=%#v", got.Args, wantArgs)
	}
}

func TestReadSelectedMulticodexProfile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "selected-profile.json")
	payload, err := json.Marshal(struct {
		Profile string `json:"profile"`
	}{Profile: "beta"})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	if err := os.WriteFile(path, append(payload, '\n'), 0o644); err != nil {
		t.Fatalf("write payload: %v", err)
	}
	if got := readSelectedMulticodexProfile(path); got != "beta" {
		t.Fatalf("expected beta profile, got %q", got)
	}
}

func TestCodexRunnerRunPromptCapturesSelectedMulticodexProfile(t *testing.T) {
	fakeBin := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "codex.log")
	writeExecutable(t, filepath.Join(fakeBin, "multicodex"), `#!/bin/sh
set -eu
cmd="$1"
shift
case "$cmd" in
  exec)
    profile="${FAKE_SELECTED_PROFILE:-beta}"
    if [ -n "${MULTICODEX_SELECTED_PROFILE_PATH:-}" ]; then
      printf '{"profile":"%s"}\n' "$profile" > "$MULTICODEX_SELECTED_PROFILE_PATH"
    fi
    export MULTICODEX_ACTIVE_PROFILE="$profile"
    exec codex exec "$@"
    ;;
  run)
    profile="$1"
    shift
    if [ "$1" != "--" ] || [ "$2" != "codex" ]; then
      echo "unexpected run invocation" >&2
      exit 1
    fi
    shift 2
    export MULTICODEX_ACTIVE_PROFILE="$profile"
    exec codex "$@"
    ;;
  *)
    echo "unexpected multicodex command: $cmd" >&2
    exit 1
    ;;
esac
`)
	writeExecutable(t, filepath.Join(fakeBin, "codex"), `#!/bin/sh
set -eu
cat >/dev/null
{
  printf 'profile=%s\n' "${MULTICODEX_ACTIVE_PROFILE:-}"
  i=0
  for arg in "$@"; do
    printf 'arg[%d]=%s\n' "$i" "$arg"
    i=$((i+1))
  done
} > "${FAKE_LOG_PATH}"
printf '{"type":"thread.started","thread_id":"thread-new"}\n'
printf '{"type":"item.completed","item":{"type":"agent_message","text":"ok"}}\n'
`)
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FAKE_LOG_PATH", logPath)
	t.Setenv("FAKE_SELECTED_PROFILE", "beta")

	runner := CodexRunner{CodexBin: "codex", Timeout: time.Second}
	result, err := runner.RunPrompt(t.TempDir(), "hello", nil, filepath.Join(t.TempDir(), "logs", "codex"))
	if err != nil {
		t.Fatalf("RunPrompt failed: %s", describeRunPromptError(err))
	}
	if !result.UsedMulticodex {
		t.Fatalf("expected multicodex launcher")
	}
	if result.MulticodexProfile != "beta" {
		t.Fatalf("expected selected profile beta, got %q", result.MulticodexProfile)
	}
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read codex log: %v", err)
	}
	log := string(data)
	if !strings.Contains(log, "profile=beta") {
		t.Fatalf("expected beta profile in codex env, got %q", log)
	}
}

func TestCodexRunnerRunPromptUsesPinnedMulticodexProfileForResume(t *testing.T) {
	fakeBin := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "codex.log")
	writeExecutable(t, filepath.Join(fakeBin, "multicodex"), `#!/bin/sh
set -eu
cmd="$1"
shift
case "$cmd" in
  exec)
    profile="${FAKE_SELECTED_PROFILE:-beta}"
    if [ -n "${MULTICODEX_SELECTED_PROFILE_PATH:-}" ]; then
      printf '{"profile":"%s"}\n' "$profile" > "$MULTICODEX_SELECTED_PROFILE_PATH"
    fi
    export MULTICODEX_ACTIVE_PROFILE="$profile"
    exec codex exec "$@"
    ;;
  run)
    profile="$1"
    shift
    if [ "$1" != "--" ] || [ "$2" != "codex" ]; then
      echo "unexpected run invocation" >&2
      exit 1
    fi
    shift 2
    export MULTICODEX_ACTIVE_PROFILE="$profile"
    exec codex "$@"
    ;;
  *)
    echo "unexpected multicodex command: $cmd" >&2
    exit 1
    ;;
esac
`)
	writeExecutable(t, filepath.Join(fakeBin, "codex"), `#!/bin/sh
set -eu
cat >/dev/null
{
  printf 'profile=%s\n' "${MULTICODEX_ACTIVE_PROFILE:-}"
  i=0
  for arg in "$@"; do
    printf 'arg[%d]=%s\n' "$i" "$arg"
    i=$((i+1))
  done
} > "${FAKE_LOG_PATH}"
thread_id="thread-new"
if [ "${1:-}" = "exec" ] && [ "${2:-}" = "resume" ]; then
  thread_id="${3:-missing}"
fi
printf '{"type":"thread.started","thread_id":"%s"}\n' "$thread_id"
printf '{"type":"item.completed","item":{"type":"agent_message","text":"ok"}}\n'
`)
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FAKE_LOG_PATH", logPath)

	runner := CodexRunner{CodexBin: "codex", Timeout: time.Second}
	result, err := runner.RunPrompt(t.TempDir(), "hello", &CodexContext{
		ThreadID:          "thread-123",
		MulticodexProfile: "beta",
	}, filepath.Join(t.TempDir(), "logs", "codex"))
	if err != nil {
		t.Fatalf("RunPrompt failed: %s", describeRunPromptError(err))
	}
	if result.MulticodexProfile != "beta" {
		t.Fatalf("expected pinned profile beta, got %q", result.MulticodexProfile)
	}
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read codex log: %v", err)
	}
	log := string(data)
	if !strings.Contains(log, "profile=beta") {
		t.Fatalf("expected beta profile in codex env, got %q", log)
	}
	if !strings.Contains(log, "arg[0]=exec") || !strings.Contains(log, "arg[1]=resume") || !strings.Contains(log, "arg[2]=thread-123") {
		t.Fatalf("expected pinned resume invocation, got %q", log)
	}
}

func writeExecutable(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("write executable %s: %v", path, err)
	}
}

func describeRunPromptError(err error) string {
	if err == nil {
		return ""
	}
	var builder strings.Builder
	builder.WriteString(err.Error())
	var execErr *CommandExecutionError
	if errors.As(err, &execErr) {
		if strings.TrimSpace(execErr.Stdout) != "" {
			builder.WriteString("\nstdout:\n")
			builder.WriteString(execErr.Stdout)
		}
		if strings.TrimSpace(execErr.Stderr) != "" {
			builder.WriteString("\nstderr:\n")
			builder.WriteString(execErr.Stderr)
		}
	}
	return builder.String()
}
