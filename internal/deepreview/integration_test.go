package deepreview

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

func runCmd(t *testing.T, cwd string, env []string, cmd ...string) string {
	t.Helper()
	c := exec.Command(cmd[0], cmd[1:]...)
	c.Dir = cwd
	if env != nil {
		c.Env = env
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	c.Stdout = &stdout
	c.Stderr = &stderr
	if err := c.Run(); err != nil {
		t.Fatalf("command failed: %v\ncmd=%v\nstdout=\n%s\nstderr=\n%s", err, cmd, stdout.String(), stderr.String())
	}
	return strings.TrimSpace(stdout.String())
}

func runCmdCapture(t *testing.T, cwd string, env []string, cmd ...string) (string, string) {
	t.Helper()
	c := exec.Command(cmd[0], cmd[1:]...)
	c.Dir = cwd
	if env != nil {
		c.Env = env
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	c.Stdout = &stdout
	c.Stderr = &stderr
	if err := c.Run(); err != nil {
		t.Fatalf("command failed: %v\ncmd=%v\nstdout=\n%s\nstderr=\n%s", err, cmd, stdout.String(), stderr.String())
	}
	return strings.TrimSpace(stdout.String()), strings.TrimSpace(stderr.String())
}

func runCmdExpectFailure(t *testing.T, cwd string, env []string, cmd ...string) string {
	t.Helper()
	c := exec.Command(cmd[0], cmd[1:]...)
	c.Dir = cwd
	if env != nil {
		c.Env = env
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	c.Stdout = &stdout
	c.Stderr = &stderr
	if err := c.Run(); err == nil {
		t.Fatalf("expected command failure but succeeded\ncmd=%v\nstdout=\n%s\nstderr=\n%s", cmd, stdout.String(), stderr.String())
	}
	return strings.TrimSpace(stdout.String() + "\n" + stderr.String())
}

func startCmd(t *testing.T, cwd string, env []string, cmd ...string) (*exec.Cmd, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	c := exec.Command(cmd[0], cmd[1:]...)
	c.Dir = cwd
	if env != nil {
		c.Env = env
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	c.Stdout = &stdout
	c.Stderr = &stderr
	if err := c.Start(); err != nil {
		t.Fatalf("command start failed: %v\ncmd=%v", err, cmd)
	}
	return c, &stdout, &stderr
}

func waitForPath(t *testing.T, path string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		} else if !os.IsNotExist(err) {
			t.Fatalf("path stat failed for %s: %v", path, err)
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for path %s", path)
}

func waitForNoWorktreeDirs(t *testing.T, root string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		lastErr = filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if d.IsDir() && filepath.Base(path) == "worktree" {
				return NewDeepReviewError("unexpected leftover worktree: %s", path)
			}
			return nil
		})
		if lastErr == nil {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("expected worktree cleanup after interrupt: %v", lastErr)
}

func runCmdWithInterrupt(t *testing.T, cwd string, env []string, interruptAfter time.Duration, cmd ...string) (stdout string, stderr string, exitCode int) {
	t.Helper()
	c := exec.Command(cmd[0], cmd[1:]...)
	c.Dir = cwd
	if env != nil {
		c.Env = env
	}
	var stdoutBuf bytes.Buffer
	var stderrBuf bytes.Buffer
	c.Stdout = &stdoutBuf
	c.Stderr = &stderrBuf

	if err := c.Start(); err != nil {
		t.Fatalf("command start failed: %v\ncmd=%v", err, cmd)
	}
	time.Sleep(interruptAfter)
	if err := c.Process.Signal(os.Interrupt); err != nil {
		t.Fatalf("failed to send interrupt: %v", err)
	}
	err := c.Wait()
	if err == nil {
		return strings.TrimSpace(stdoutBuf.String()), strings.TrimSpace(stderrBuf.String()), 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return strings.TrimSpace(stdoutBuf.String()), strings.TrimSpace(stderrBuf.String()), exitErr.ExitCode()
	}
	t.Fatalf("command wait failed unexpectedly: %v\ncmd=%v\nstdout=\n%s\nstderr=\n%s", err, cmd, stdoutBuf.String(), stderrBuf.String())
	return "", "", -1
}

func buildBinary(t *testing.T, root string) string {
	t.Helper()
	binPath := filepath.Join(t.TempDir(), "deepreview")
	runCmd(t, root, nil, "go", "build", "-o", binPath, "./cmd/deepreview")
	return binPath
}

func buildFakeBinaries(t *testing.T, root string) (string, string) {
	t.Helper()
	tmpDir := t.TempDir()
	fakeCodex := filepath.Join(tmpDir, "fake-codex")
	fakeGH := filepath.Join(tmpDir, "fake-gh")
	runCmd(t, root, nil, "go", "build", "-o", fakeCodex, "./cmd/fake-codex")
	runCmd(t, root, nil, "go", "build", "-o", fakeGH, "./cmd/fake-gh")
	if err := os.Symlink(fakeCodex, filepath.Join(tmpDir, "multicodex")); err != nil {
		t.Fatalf("symlink fake multicodex: %v", err)
	}
	return fakeCodex, fakeGH
}

func buildStallCodexWrapperScript(fakeCodex, body string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
prompt_file="$(mktemp "${TMPDIR:-/tmp}/deepreview-stall-prompt.XXXXXX")"
trap 'rm -f "$prompt_file"' EXIT
cat > "$prompt_file"
marker_path="${STALL_MARKER_PATH:?}"
%s
exec %q "$@" < "$prompt_file"
`, body, fakeCodex)
}

func buildExecuteStallCodexWrapper(t *testing.T, fakeCodex string) string {
	t.Helper()
	tmpDir := t.TempDir()
	wrapperPath := filepath.Join(tmpDir, "stalling-codex")
	script := buildStallCodexWrapperScript(fakeCodex, `if grep -q "prompt 2 of 2" "$prompt_file" && [ ! -f "$marker_path" ]; then
  mkdir -p "$(dirname "$marker_path")"
  : > "$marker_path"
  printf 'stale stalled attempt\n' > stale_execute_attempt.txt
  git add stale_execute_attempt.txt
  git commit -m "stale stalled attempt" >/dev/null 2>&1 || true
  sleep "${STALL_SECONDS:-15}"
fi`)
	if err := os.WriteFile(wrapperPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write stalling codex wrapper: %v", err)
	}
	if err := os.Symlink(wrapperPath, filepath.Join(tmpDir, "multicodex")); err != nil {
		t.Fatalf("symlink stalling multicodex: %v", err)
	}
	return wrapperPath
}

func buildExecuteArtifactStallCodexWrapper(t *testing.T, fakeCodex string) string {
	t.Helper()
	tmpDir := t.TempDir()
	wrapperPath := filepath.Join(tmpDir, "artifact-stalling-codex")
	script := buildStallCodexWrapperScript(fakeCodex, `if grep -q "prompt 2 of 2" "$prompt_file" && [ ! -f "$marker_path" ]; then
  mkdir -p "$(dirname "$marker_path")"
  : > "$marker_path"
  mkdir -p .deepreview/artifacts
  cat > .deepreview/artifacts/round-status.json <<'EOF'
{
  "decision": "continue",
  "reason": "stale status from stalled prompt 2"
}
EOF
  printf '# Stale Round Summary\n\n- from killed prompt 2\n' > .deepreview/artifacts/round-summary.md
  sleep "${STALL_SECONDS:-15}"
fi
if grep -q "prompt 2 of 2" "$prompt_file"; then
  thread_id="thread-skip-finalize"
  if [ "${1:-}" = "exec" ] && [ "${2:-}" = "resume" ] && [ -n "${3:-}" ]; then
    thread_id="$3"
  fi
  printf '{"type":"thread.started","thread_id":"%s"}\n' "$thread_id"
  printf '{"type":"turn.started"}\n'
  printf '{"type":"item.completed","item":{"id":"item_0","type":"agent_message","text":"finalize complete"}}\n'
  printf '{"type":"turn.completed","usage":{"input_tokens":1,"output_tokens":1}}\n'
  exit 0
fi`)
	if err := os.WriteFile(wrapperPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write execute artifact stalling codex wrapper: %v", err)
	}
	if err := os.Symlink(wrapperPath, filepath.Join(tmpDir, "multicodex")); err != nil {
		t.Fatalf("symlink execute artifact stalling multicodex: %v", err)
	}
	return wrapperPath
}

func buildStageCommitStallCodexWrapper(t *testing.T, fakeCodex, promptNeedle, staleFile, commitMessage string) string {
	t.Helper()
	tmpDir := t.TempDir()
	wrapperPath := filepath.Join(tmpDir, "stage-stalling-codex")
	script := buildStallCodexWrapperScript(fakeCodex, fmt.Sprintf(`if grep -q %q "$prompt_file" && [ ! -f "$marker_path" ]; then
  mkdir -p "$(dirname "$marker_path")"
  : > "$marker_path"
  printf %q > %q
  git add %q
  git commit -m %q >/dev/null 2>&1 || true
  sleep "${STALL_SECONDS:-15}"
fi`, promptNeedle, "stale staged attempt\n", staleFile, staleFile, commitMessage))
	if err := os.WriteFile(wrapperPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write stage stalling codex wrapper: %v", err)
	}
	if err := os.Symlink(wrapperPath, filepath.Join(tmpDir, "multicodex")); err != nil {
		t.Fatalf("symlink stage stalling multicodex: %v", err)
	}
	return wrapperPath
}

func cloneUserRepoWithGitHubOrigin(t *testing.T, td, remote, userClone string) string {
	t.Helper()
	githubURL := githubSCPLikeCloneURL("example-org", "example-repo")
	configureGitHubURLRewrite(t, githubURL, remote)
	runCmd(t, td, nil, "git", "clone", remote, userClone)
	runCmd(t, td, nil, "git", "-C", userClone, "remote", "set-url", "origin", githubURL)
	runCmd(t, td, nil, "git", "-C", userClone, "config", "user.email", "test@example.com")
	runCmd(t, td, nil, "git", "-C", userClone, "config", "user.name", "Test User")
	return githubURL
}

func baseEnv(root, workspace, codexBin, ghBin string) []string {
	env := append([]string{}, os.Environ()...)
	pathPrefix := filepath.Dir(codexBin)
	currentPath := os.Getenv("PATH")
	if currentPath != "" {
		pathPrefix += string(os.PathListSeparator) + currentPath
	}
	env = append(env,
		"DEEPREVIEW_WORKSPACE_ROOT="+workspace,
		"DEEPREVIEW_CODEX_BIN="+codexBin,
		"DEEPREVIEW_GH_BIN="+ghBin,
		"DEEPREVIEW_PROMPTS_ROOT="+filepath.Join(root, "prompts"),
		"PATH="+pathPrefix,
		"GIT_AUTHOR_NAME=DeepReview Bot",
		"GIT_AUTHOR_EMAIL=deepreview@example.com",
		"GIT_COMMITTER_NAME=DeepReview Bot",
		"GIT_COMMITTER_EMAIL=deepreview@example.com",
	)
	return env
}

func TestEndToEndYoloWithFakeCodex(t *testing.T) {
	root := repoRoot(t)
	bin := buildBinary(t, root)
	fakeCodex, fakeGH := buildFakeBinaries(t, root)

	td := t.TempDir()
	remote := filepath.Join(td, "remote.git")
	seed := filepath.Join(td, "seed")
	userClone := filepath.Join(td, "user")
	workspace := filepath.Join(td, "workspace")

	runCmd(t, td, nil, "git", "init", "--bare", remote)
	runCmd(t, td, nil, "git", "clone", remote, seed)
	runCmd(t, td, nil, "git", "-C", seed, "config", "user.email", "test@example.com")
	runCmd(t, td, nil, "git", "-C", seed, "config", "user.name", "Test User")
	runCmd(t, td, nil, "git", "-C", seed, "checkout", "-b", "main")
	if err := os.WriteFile(filepath.Join(seed, "README.md"), []byte("seed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runCmd(t, td, nil, "git", "-C", seed, "add", "README.md")
	runCmd(t, td, nil, "git", "-C", seed, "commit", "-m", "seed")
	runCmd(t, td, nil, "git", "-C", seed, "push", "-u", "origin", "main")

	runCmd(t, td, nil, "git", "-C", seed, "checkout", "-b", "feature/test")
	if err := os.WriteFile(filepath.Join(seed, "feature.txt"), []byte("feature\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runCmd(t, td, nil, "git", "-C", seed, "add", "feature.txt")
	runCmd(t, td, nil, "git", "-C", seed, "commit", "-m", "feature")
	runCmd(t, td, nil, "git", "-C", seed, "push", "-u", "origin", "feature/test")

	runCmd(t, td, nil, "git", "clone", remote, userClone)
	runCmd(t, td, nil, "git", "-C", userClone, "checkout", "feature/test")
	before := runCmd(t, td, nil, "git", "-C", userClone, "rev-parse", "origin/feature/test")

	env := baseEnv(root, workspace, fakeCodex, fakeGH)
	env = append(env, "FAKE_CODEX_WRITE_OPERATIONAL_TMP=1")
	output := runCmd(t, root, env,
		bin,
		"review",
		userClone,
		"--source-branch", "feature/test",
		"--concurrency", "2",
		"--max-rounds", "2",
		"--mode", "yolo",
		"--no-tui",
	)
	if !strings.Contains(output, "deepreview completed:") {
		t.Fatalf("expected completion summary output, got: %s", output)
	}
	if !strings.Contains(output, "changes pushed:") {
		t.Fatalf("expected yolo pushed link output, got: %s", output)
	}

	runCmd(t, td, nil, "git", "-C", userClone, "fetch", "origin")
	after := runCmd(t, td, nil, "git", "-C", userClone, "rev-parse", "origin/feature/test")
	if before == after {
		t.Fatalf("expected yolo delivery push to update remote source branch")
	}
	deliveredTree := runCmd(t, td, nil, "git", "-C", userClone, "ls-tree", "-r", "--name-only", "origin/feature/test")
	if strings.Contains(deliveredTree, ".deepreview/") {
		t.Fatalf("yolo delivery must not include .deepreview artifacts, tree:\n%s", deliveredTree)
	}
	if strings.Contains(deliveredTree, ".tmp/") {
		t.Fatalf("yolo delivery must not include .tmp artifacts, tree:\n%s", deliveredTree)
	}

	runsGlob, err := filepath.Glob(filepath.Join(workspace, "runs", "*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(runsGlob) != 1 {
		t.Fatalf("expected 1 run dir, got %d", len(runsGlob))
	}
	runDir := runsGlob[0]
	if _, err := os.Stat(filepath.Join(runDir, "final-summary.md")); err != nil {
		t.Fatalf("final-summary.md missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(runDir, "round-01", "round-status.json")); err != nil {
		t.Fatalf("round-status.json missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(runDir, "round-01", "round-summary.md")); err != nil {
		t.Fatalf("round-summary.md missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(runDir, "round-01", "round.json")); err != nil {
		t.Fatalf("round.json missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(runDir, "round-01", "round-triage.md")); err != nil {
		t.Fatalf("round-triage.md missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(runDir, "round-01", "round-plan.md")); err != nil {
		t.Fatalf("round-plan.md missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(runDir, "round-01", "round-verification.md")); err != nil {
		t.Fatalf("round-verification.md missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(runDir, "round-01", "review-01.md")); err != nil {
		t.Fatalf("review-01.md missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(runDir, "round-01", "review", "worker-01", "worktree")); !os.IsNotExist(err) {
		t.Fatalf("expected review worker worktree cleanup")
	}
}

func TestConcurrentRunsSameRepoDifferentBranchesSucceed(t *testing.T) {
	root := repoRoot(t)
	bin := buildBinary(t, root)
	fakeCodex, fakeGH := buildFakeBinaries(t, root)

	td := t.TempDir()
	remote := filepath.Join(td, "remote.git")
	seed := filepath.Join(td, "seed")
	userClone := filepath.Join(td, "user")
	workspace := filepath.Join(td, "workspace")

	runCmd(t, td, nil, "git", "init", "--bare", remote)
	runCmd(t, td, nil, "git", "clone", remote, seed)
	runCmd(t, td, nil, "git", "-C", seed, "config", "user.email", "test@example.com")
	runCmd(t, td, nil, "git", "-C", seed, "config", "user.name", "Test User")
	runCmd(t, td, nil, "git", "-C", seed, "checkout", "-b", "main")
	if err := os.WriteFile(filepath.Join(seed, "README.md"), []byte("seed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runCmd(t, td, nil, "git", "-C", seed, "add", "README.md")
	runCmd(t, td, nil, "git", "-C", seed, "commit", "-m", "seed")
	runCmd(t, td, nil, "git", "-C", seed, "push", "-u", "origin", "main")

	runCmd(t, td, nil, "git", "-C", seed, "checkout", "-b", "feature/a")
	if err := os.WriteFile(filepath.Join(seed, "branch-a.txt"), []byte("branch a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runCmd(t, td, nil, "git", "-C", seed, "add", "branch-a.txt")
	runCmd(t, td, nil, "git", "-C", seed, "commit", "-m", "feature a")
	runCmd(t, td, nil, "git", "-C", seed, "push", "-u", "origin", "feature/a")

	runCmd(t, td, nil, "git", "-C", seed, "checkout", "main")
	runCmd(t, td, nil, "git", "-C", seed, "checkout", "-b", "feature/b")
	if err := os.WriteFile(filepath.Join(seed, "branch-b.txt"), []byte("branch b\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runCmd(t, td, nil, "git", "-C", seed, "add", "branch-b.txt")
	runCmd(t, td, nil, "git", "-C", seed, "commit", "-m", "feature b")
	runCmd(t, td, nil, "git", "-C", seed, "push", "-u", "origin", "feature/b")

	runCmd(t, td, nil, "git", "clone", remote, userClone)
	runCmd(t, td, nil, "git", "-C", userClone, "checkout", "feature/a")

	beforeA := runCmd(t, td, nil, "git", "-C", userClone, "rev-parse", "origin/feature/a")
	beforeB := runCmd(t, td, nil, "git", "-C", userClone, "rev-parse", "origin/feature/b")

	env := baseEnv(root, workspace, fakeCodex, fakeGH)
	env = append(env, "FAKE_CODEX_SLEEP_MS=200")

	cmdA, stdoutA, stderrA := startCmd(t, root, env,
		bin,
		"review",
		userClone,
		"--source-branch", "feature/a",
		"--concurrency", "1",
		"--max-rounds", "2",
		"--mode", "yolo",
		"--no-tui",
	)
	identity, err := resolveRepoIdentity(ReviewConfig{GitBin: "git"}, userClone)
	if err != nil {
		t.Fatalf("resolveRepoIdentity failed: %v", err)
	}
	lockA := filepath.Join(append([]string{workspace, "locks"}, append(identity.NamespaceSegments(), FilesystemSafeKey("feature/a")+".lock")...)...)
	waitForPath(t, lockA, 5*time.Second)

	cmdB, stdoutB, stderrB := startCmd(t, root, env,
		bin,
		"review",
		userClone,
		"--source-branch", "feature/b",
		"--concurrency", "1",
		"--max-rounds", "2",
		"--mode", "yolo",
		"--no-tui",
	)

	if err := cmdA.Wait(); err != nil {
		t.Fatalf("first concurrent run failed: %v\nstdout=\n%s\nstderr=\n%s", err, stdoutA.String(), stderrA.String())
	}
	if err := cmdB.Wait(); err != nil {
		t.Fatalf("second concurrent run failed: %v\nstdout=\n%s\nstderr=\n%s", err, stdoutB.String(), stderrB.String())
	}
	if !strings.Contains(stdoutA.String(), "deepreview completed:") {
		t.Fatalf("expected first run completion summary, got stdout:\n%s\nstderr:\n%s", stdoutA.String(), stderrA.String())
	}
	if !strings.Contains(stdoutB.String(), "deepreview completed:") {
		t.Fatalf("expected second run completion summary, got stdout:\n%s\nstderr:\n%s", stdoutB.String(), stderrB.String())
	}

	runCmd(t, td, nil, "git", "-C", userClone, "fetch", "origin")
	afterA := runCmd(t, td, nil, "git", "-C", userClone, "rev-parse", "origin/feature/a")
	afterB := runCmd(t, td, nil, "git", "-C", userClone, "rev-parse", "origin/feature/b")
	if beforeA == afterA {
		t.Fatalf("expected concurrent branch-a run to update remote branch")
	}
	if beforeB == afterB {
		t.Fatalf("expected concurrent branch-b run to update remote branch")
	}

	managedA := filepath.Join(append([]string{workspace, "repos"}, append(identity.NamespaceSegments(), "branches", FilesystemSafeKey("feature/a"))...)...)
	managedB := filepath.Join(append([]string{workspace, "repos"}, append(identity.NamespaceSegments(), "branches", FilesystemSafeKey("feature/b"))...)...)
	if _, err := os.Stat(filepath.Join(managedA, ".git")); err != nil {
		t.Fatalf("expected branch-a managed repo to exist, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(managedB, ".git")); err != nil {
		t.Fatalf("expected branch-b managed repo to exist, got %v", err)
	}
}

func TestEndToEndPRModeWithFakeGH(t *testing.T) {
	root := repoRoot(t)
	bin := buildBinary(t, root)
	fakeCodex, fakeGH := buildFakeBinaries(t, root)

	td := t.TempDir()
	remote := filepath.Join(td, "remote.git")
	seed := filepath.Join(td, "seed")
	userClone := filepath.Join(td, "user")
	workspace := filepath.Join(td, "workspace")

	runCmd(t, td, nil, "git", "init", "--bare", remote)
	runCmd(t, td, nil, "git", "clone", remote, seed)
	runCmd(t, td, nil, "git", "-C", seed, "config", "user.email", "test@example.com")
	runCmd(t, td, nil, "git", "-C", seed, "config", "user.name", "Test User")
	runCmd(t, td, nil, "git", "-C", seed, "checkout", "-b", "main")
	if err := os.WriteFile(filepath.Join(seed, "README.md"), []byte("seed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runCmd(t, td, nil, "git", "-C", seed, "add", "README.md")
	runCmd(t, td, nil, "git", "-C", seed, "commit", "-m", "seed")
	runCmd(t, td, nil, "git", "-C", seed, "push", "-u", "origin", "main")

	runCmd(t, td, nil, "git", "-C", seed, "checkout", "-b", "feature/test")
	if err := os.WriteFile(filepath.Join(seed, "feature.txt"), []byte("feature\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runCmd(t, td, nil, "git", "-C", seed, "add", "feature.txt")
	runCmd(t, td, nil, "git", "-C", seed, "commit", "-m", "feature")
	runCmd(t, td, nil, "git", "-C", seed, "push", "-u", "origin", "feature/test")

	cloneUserRepoWithGitHubOrigin(t, td, remote, userClone)
	runCmd(t, td, nil, "git", "-C", userClone, "checkout", "feature/test")
	before := runCmd(t, td, nil, "git", "-C", userClone, "rev-parse", "origin/feature/test")

	env := baseEnv(root, workspace, fakeCodex, fakeGH)
	env = append(env,
		"FAKE_CODEX_WRITE_OPERATIONAL_TMP=1",
		"DEEPREVIEW_REVIEW_INACTIVITY_SECONDS=2",
		"DEEPREVIEW_REVIEW_ACTIVITY_POLL_SECONDS=1",
		"DEEPREVIEW_REVIEW_MAX_RESTARTS=1",
		"FAKE_CODEX_REQUIRE_SKIP_GIT_REPO_CHECK=1",
		"FAKE_CODEX_STALL_ONCE_CONTAINS=deepreview final delivery stage",
		"FAKE_CODEX_STALL_ONCE_MS_MATCH=15000",
	)
	output, logs := runCmdCapture(t, root, env,
		bin,
		"review",
		userClone,
		"--source-branch", "feature/test",
		"--concurrency", "1",
		"--max-rounds", "2",
		"--mode", "pr",
		"--no-tui",
	)
	if !strings.Contains(output, "deepreview completed:") {
		t.Fatalf("expected completion summary output, got: %s", output)
	}
	if !strings.Contains(logs, "delivery / merge-ready publish inactive for") {
		t.Fatalf("expected delivery inactivity restart evidence in logs, got:\n%s", logs)
	}
	if !strings.Contains(output, "PR created: https://example.com/olliecrow/test/pull/123") {
		t.Fatalf("expected pr created summary output, got: %s", output)
	}
	if !strings.Contains(output, "delivery commits: https://github.com/example-org/example-repo/commits/deepreview/feature/test/") {
		t.Fatalf("expected delivery commits summary output, got: %s", output)
	}
	if !strings.Contains(output, "repository reviewed: `example-org/example-repo`") {
		t.Fatalf("expected repository context in completion summary, got: %s", output)
	}
	if !strings.Contains(output, "source branch reviewed: `feature/test`") {
		t.Fatalf("expected source branch context in completion summary, got: %s", output)
	}
	if !strings.Contains(output, "reviewed directory:") {
		t.Fatalf("expected reviewed directory context in completion summary, got: %s", output)
	}
	if !strings.Contains(output, "final review status: decision `stop`") {
		t.Fatalf("expected final review status context in completion summary, got: %s", output)
	}

	runCmd(t, td, nil, "git", "-C", userClone, "fetch", "origin")
	after := runCmd(t, td, nil, "git", "-C", userClone, "rev-parse", "origin/feature/test")
	if before != after {
		t.Fatalf("source branch should not be updated in pr mode")
	}

	refsOut := runCmd(t, td, nil, "git", "-C", userClone, "for-each-ref", "--format=%(refname:short)", "refs/remotes/origin/deepreview")
	if !strings.Contains(refsOut, "origin/deepreview/feature/test/") {
		t.Fatalf("expected deepreview remote branch ref, got: %s", refsOut)
	}
	for _, ref := range strings.Split(strings.TrimSpace(refsOut), "\n") {
		ref = strings.TrimSpace(ref)
		if ref == "" {
			continue
		}
		tree := runCmd(t, td, nil, "git", "-C", userClone, "ls-tree", "-r", "--name-only", ref)
		if strings.Contains(tree, ".deepreview/") {
			t.Fatalf("pr delivery branch must not include .deepreview artifacts, ref=%s tree:\n%s", ref, tree)
		}
		if strings.Contains(tree, ".tmp/") {
			t.Fatalf("pr delivery branch must not include .tmp artifacts, ref=%s tree:\n%s", ref, tree)
		}
	}

	runsGlob, err := filepath.Glob(filepath.Join(workspace, "runs", "*"))
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(runsGlob)
	if len(runsGlob) != 1 {
		t.Fatalf("expected 1 run dir, got %d", len(runsGlob))
	}
	runDir := runsGlob[0]
	finalSummaryBytes, err := os.ReadFile(filepath.Join(runDir, "final-summary.md"))
	if err != nil {
		t.Fatalf("missing final-summary.md: %v", err)
	}
	if !strings.Contains(string(finalSummaryBytes), "- mode: `pr`") {
		t.Fatalf("final summary missing pr mode line")
	}
	if !strings.Contains(string(finalSummaryBytes), "## round decisions") {
		t.Fatalf("final summary missing round decisions section")
	}
	if _, err := os.Stat(filepath.Join(runDir, "pr-body.md")); err != nil {
		t.Fatalf("pr-body.md missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(runDir, "pr-title.txt")); err != nil {
		t.Fatalf("pr-title.txt missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(runDir, "delivery", "delivery-result.json")); err != nil {
		t.Fatalf("delivery-result.json missing: %v", err)
	}
	prTitleBytes, err := os.ReadFile(filepath.Join(runDir, "pr-title.txt"))
	if err != nil {
		t.Fatalf("missing pr-title.txt: %v", err)
	}
	prTitle := strings.TrimSpace(string(prTitleBytes))
	if !strings.HasPrefix(strings.ToLower(prTitle), "deepreview:") {
		t.Fatalf("pr title should keep deepreview prefix, got: %q", prTitle)
	}
	if strings.Contains(prTitle, "/Users/") {
		t.Fatalf("pr title must not contain local absolute user paths")
	}
	prBodyBytes, err := os.ReadFile(filepath.Join(runDir, "pr-body.md"))
	if err != nil {
		t.Fatalf("missing pr-body.md: %v", err)
	}
	prBody := string(prBodyBytes)
	if !strings.HasPrefix(strings.TrimSpace(prBody), "## summary") {
		t.Fatalf("pr body should be codex-generated detailed summary, got:\n%s", prBody)
	}
	if strings.Contains(prBody, "\n\n---\n\n") {
		t.Fatalf("pr body must not append deterministic base body below a separator")
	}
	if strings.Contains(prBody, "/Users/") {
		t.Fatalf("pr body must not contain local absolute user paths")
	}
	if !strings.Contains(prBody, "## what changed and why") {
		t.Fatalf("pr body missing what changed and why section")
	}
	if !strings.Contains(prBody, "## round outcomes") {
		t.Fatalf("pr body missing round outcomes section")
	}
	if !strings.Contains(prBody, "## verification") {
		t.Fatalf("pr body missing verification section")
	}
	if !strings.Contains(prBody, "## risks and follow-ups") {
		t.Fatalf("pr body missing risks and follow-ups section")
	}
	if !strings.Contains(prBody, "## final status") {
		t.Fatalf("pr body missing final status section")
	}
	if strings.Contains(prBody, "### independent reviews") {
		t.Fatalf("pr body should not include embedded independent review details")
	}
	if strings.Contains(prBody, "### execute artifacts") {
		t.Fatalf("pr body should not include embedded execute artifact dumps")
	}
	if strings.Contains(prBody, "## deepreview report") {
		t.Fatalf("pr body must not include old deterministic deepreview report sections")
	}
	if strings.Contains(string(finalSummaryBytes), "/Users/") {
		t.Fatalf("final summary must not contain local absolute user paths")
	}
	if _, err := os.Stat(filepath.Join(runDir, "round-01", "round-status.json")); err != nil {
		t.Fatalf("round-status.json missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(runDir, "round-01", "round-summary.md")); err != nil {
		t.Fatalf("round-summary.md missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(runDir, "round-01", "round.json")); err != nil {
		t.Fatalf("round.json missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(runDir, "round-01", "round-triage.md")); err != nil {
		t.Fatalf("round-triage.md missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(runDir, "round-01", "round-plan.md")); err != nil {
		t.Fatalf("round-plan.md missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(runDir, "round-01", "round-verification.md")); err != nil {
		t.Fatalf("round-verification.md missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(runDir, "round-01", "review-01.md")); err != nil {
		t.Fatalf("review-01.md missing: %v", err)
	}
}

func TestEndToEndPRModeRecoversIncompleteDraftWhenPRURLMissingAfterPush(t *testing.T) {
	root := repoRoot(t)
	bin := buildBinary(t, root)
	fakeCodex, fakeGH := buildFakeBinaries(t, root)

	td := t.TempDir()
	remote := filepath.Join(td, "remote.git")
	seed := filepath.Join(td, "seed")
	userClone := filepath.Join(td, "user")
	workspace := filepath.Join(td, "workspace")
	ghArgsPath := filepath.Join(td, "gh-create-args.txt")

	runCmd(t, td, nil, "git", "init", "--bare", remote)
	runCmd(t, td, nil, "git", "clone", remote, seed)
	runCmd(t, td, nil, "git", "-C", seed, "config", "user.email", "test@example.com")
	runCmd(t, td, nil, "git", "-C", seed, "config", "user.name", "Test User")
	runCmd(t, td, nil, "git", "-C", seed, "checkout", "-b", "main")
	if err := os.WriteFile(filepath.Join(seed, "README.md"), []byte("seed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runCmd(t, td, nil, "git", "-C", seed, "add", "README.md")
	runCmd(t, td, nil, "git", "-C", seed, "commit", "-m", "seed")
	runCmd(t, td, nil, "git", "-C", seed, "push", "-u", "origin", "main")

	runCmd(t, td, nil, "git", "-C", seed, "checkout", "-b", "feature/test")
	if err := os.WriteFile(filepath.Join(seed, "feature.txt"), []byte("feature\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runCmd(t, td, nil, "git", "-C", seed, "add", "feature.txt")
	runCmd(t, td, nil, "git", "-C", seed, "commit", "-m", "feature")
	runCmd(t, td, nil, "git", "-C", seed, "push", "-u", "origin", "feature/test")

	cloneUserRepoWithGitHubOrigin(t, td, remote, userClone)
	runCmd(t, td, nil, "git", "-C", userClone, "checkout", "feature/test")

	env := baseEnv(root, workspace, fakeCodex, fakeGH)
	env = append(env,
		"FAKE_GH_PR_CREATE_SILENT=1",
		"FAKE_GH_CAPTURE_ARGS_PATH="+ghArgsPath,
	)
	output := runCmd(t, root, env,
		bin,
		"review",
		userClone,
		"--source-branch", "feature/test",
		"--concurrency", "1",
		"--max-rounds", "2",
		"--mode", "pr",
		"--no-tui",
	)
	if !strings.Contains(output, "Draft PR created: https://example.com/olliecrow/test/pull/123") {
		t.Fatalf("expected incomplete draft PR recovery output, got: %s", output)
	}
	if !strings.Contains(output, "delivery status: incomplete (gh pr create did not return a pull request URL)") {
		t.Fatalf("expected incomplete delivery reason in output, got: %s", output)
	}
	if !strings.Contains(output, "delivery commits: https://github.com/example-org/example-repo/commits/deepreview/feature/test/") {
		t.Fatalf("expected delivery commits summary output, got: %s", output)
	}
	argsBytes, err := os.ReadFile(ghArgsPath)
	if err != nil {
		t.Fatalf("missing gh args capture: %v", err)
	}
	if !strings.Contains(string(argsBytes), "--draft") {
		t.Fatalf("expected recovery pr create call to use --draft, got:\n%s", string(argsBytes))
	}

	runsGlob, err := filepath.Glob(filepath.Join(workspace, "runs", "*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(runsGlob) != 1 {
		t.Fatalf("expected 1 run dir, got %d", len(runsGlob))
	}
	finalSummaryBytes, err := os.ReadFile(filepath.Join(runsGlob[0], "final-summary.md"))
	if err != nil {
		t.Fatalf("missing final-summary.md: %v", err)
	}
	if !strings.Contains(string(finalSummaryBytes), "- delivery: `incomplete-draft`") {
		t.Fatalf("expected incomplete draft marker in final summary, got:\n%s", string(finalSummaryBytes))
	}
}

func TestEndToEndPRModeSupportsIncompletePromptResult(t *testing.T) {
	root := repoRoot(t)
	bin := buildBinary(t, root)
	fakeCodex, fakeGH := buildFakeBinaries(t, root)

	td := t.TempDir()
	remote := filepath.Join(td, "remote.git")
	seed := filepath.Join(td, "seed")
	userClone := filepath.Join(td, "user")
	workspace := filepath.Join(td, "workspace")
	ghArgsPath := filepath.Join(td, "gh-create-args.txt")

	runCmd(t, td, nil, "git", "init", "--bare", remote)
	runCmd(t, td, nil, "git", "clone", remote, seed)
	runCmd(t, td, nil, "git", "-C", seed, "config", "user.email", "test@example.com")
	runCmd(t, td, nil, "git", "-C", seed, "config", "user.name", "Test User")
	runCmd(t, td, nil, "git", "-C", seed, "checkout", "-b", "main")
	if err := os.WriteFile(filepath.Join(seed, "README.md"), []byte("seed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runCmd(t, td, nil, "git", "-C", seed, "add", "README.md")
	runCmd(t, td, nil, "git", "-C", seed, "commit", "-m", "seed")
	runCmd(t, td, nil, "git", "-C", seed, "push", "-u", "origin", "main")

	runCmd(t, td, nil, "git", "-C", seed, "checkout", "-b", "feature/test")
	if err := os.WriteFile(filepath.Join(seed, "feature.txt"), []byte("feature\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runCmd(t, td, nil, "git", "-C", seed, "add", "feature.txt")
	runCmd(t, td, nil, "git", "-C", seed, "commit", "-m", "feature")
	runCmd(t, td, nil, "git", "-C", seed, "push", "-u", "origin", "feature/test")

	cloneUserRepoWithGitHubOrigin(t, td, remote, userClone)
	runCmd(t, td, nil, "git", "-C", userClone, "checkout", "feature/test")

	env := baseEnv(root, workspace, fakeCodex, fakeGH)
	env = append(env,
		"FAKE_CODEX_DELIVERY_INCOMPLETE=1",
		"FAKE_CODEX_DELIVERY_INCOMPLETE_REASON=waiting on mergeability signal",
		"FAKE_GH_CAPTURE_ARGS_PATH="+ghArgsPath,
	)
	output := runCmd(t, root, env,
		bin,
		"review",
		userClone,
		"--source-branch", "feature/test",
		"--concurrency", "1",
		"--max-rounds", "2",
		"--mode", "pr",
		"--no-tui",
	)
	if !strings.Contains(output, "Draft PR created: https://example.com/olliecrow/test/pull/123") {
		t.Fatalf("expected incomplete draft PR summary output, got: %s", output)
	}
	if !strings.Contains(output, "delivery status: incomplete (waiting on mergeability signal)") {
		t.Fatalf("expected incomplete prompt reason in output, got: %s", output)
	}

	argsBytes, err := os.ReadFile(ghArgsPath)
	if err != nil {
		t.Fatalf("missing gh args capture: %v", err)
	}
	if !strings.Contains(string(argsBytes), "--draft") {
		t.Fatalf("expected incomplete prompt result to create a draft PR, got:\n%s", string(argsBytes))
	}

	runsGlob, err := filepath.Glob(filepath.Join(workspace, "runs", "*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(runsGlob) != 1 {
		t.Fatalf("expected 1 run dir, got %d", len(runsGlob))
	}
	finalSummaryBytes, err := os.ReadFile(filepath.Join(runsGlob[0], "final-summary.md"))
	if err != nil {
		t.Fatalf("missing final-summary.md: %v", err)
	}
	if !strings.Contains(string(finalSummaryBytes), "- delivery: `incomplete-draft`") {
		t.Fatalf("expected incomplete draft marker in final summary, got:\n%s", string(finalSummaryBytes))
	}
}

func TestEndToEndPRModeFailsBeforePublishingSecretChange(t *testing.T) {
	root := repoRoot(t)
	bin := buildBinary(t, root)
	fakeCodex, fakeGH := buildFakeBinaries(t, root)

	td := t.TempDir()
	remote := filepath.Join(td, "remote.git")
	seed := filepath.Join(td, "seed")
	userClone := filepath.Join(td, "user")
	workspace := filepath.Join(td, "workspace")
	ghArgsPath := filepath.Join(td, "gh-create-args.txt")

	runCmd(t, td, nil, "git", "init", "--bare", remote)
	runCmd(t, td, nil, "git", "clone", remote, seed)
	runCmd(t, td, nil, "git", "-C", seed, "config", "user.email", "test@example.com")
	runCmd(t, td, nil, "git", "-C", seed, "config", "user.name", "Test User")
	runCmd(t, td, nil, "git", "-C", seed, "checkout", "-b", "main")
	if err := os.WriteFile(filepath.Join(seed, "README.md"), []byte("seed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runCmd(t, td, nil, "git", "-C", seed, "add", "README.md")
	runCmd(t, td, nil, "git", "-C", seed, "commit", "-m", "seed")
	runCmd(t, td, nil, "git", "-C", seed, "push", "-u", "origin", "main")

	runCmd(t, td, nil, "git", "-C", seed, "checkout", "-b", "feature/test")
	if err := os.WriteFile(filepath.Join(seed, "feature.txt"), []byte("feature\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runCmd(t, td, nil, "git", "-C", seed, "add", "feature.txt")
	runCmd(t, td, nil, "git", "-C", seed, "commit", "-m", "feature")
	runCmd(t, td, nil, "git", "-C", seed, "push", "-u", "origin", "feature/test")

	cloneUserRepoWithGitHubOrigin(t, td, remote, userClone)
	runCmd(t, td, nil, "git", "-C", userClone, "checkout", "feature/test")

	env := baseEnv(root, workspace, fakeCodex, fakeGH)
	env = append(env,
		"FAKE_CODEX_WRITE_SECRET_PATTERN_CHANGE=1",
		"FAKE_GH_CAPTURE_ARGS_PATH="+ghArgsPath,
	)
	output := runCmdExpectFailure(t, root, env,
		bin,
		"review",
		userClone,
		"--source-branch", "feature/test",
		"--concurrency", "1",
		"--max-rounds", "2",
		"--mode", "pr",
		"--no-tui",
	)
	if !strings.Contains(output, "privacy scan failed: secret-like pattern matched in deepreview_test_round.txt") {
		t.Fatalf("expected delivery privacy failure before publish, got:\n%s", output)
	}
	if _, err := os.Stat(ghArgsPath); !os.IsNotExist(err) {
		t.Fatalf("did not expect gh pr create to run before privacy validation, err=%v", err)
	}

	runCmd(t, td, nil, "git", "-C", userClone, "fetch", "origin")
	refsOut := runCmd(t, td, nil, "git", "-C", userClone, "for-each-ref", "--format=%(refname:short)", "refs/remotes/origin/deepreview")
	if strings.TrimSpace(refsOut) != "" {
		t.Fatalf("expected no delivery refs after blocked publish, got: %s", refsOut)
	}
}

func TestEndToEndYoloFailsBeforePublishingSecretChange(t *testing.T) {
	root := repoRoot(t)
	bin := buildBinary(t, root)
	fakeCodex, fakeGH := buildFakeBinaries(t, root)

	td := t.TempDir()
	remote := filepath.Join(td, "remote.git")
	seed := filepath.Join(td, "seed")
	userClone := filepath.Join(td, "user")
	workspace := filepath.Join(td, "workspace")

	runCmd(t, td, nil, "git", "init", "--bare", remote)
	runCmd(t, td, nil, "git", "clone", remote, seed)
	runCmd(t, td, nil, "git", "-C", seed, "config", "user.email", "test@example.com")
	runCmd(t, td, nil, "git", "-C", seed, "config", "user.name", "Test User")
	runCmd(t, td, nil, "git", "-C", seed, "checkout", "-b", "main")
	if err := os.WriteFile(filepath.Join(seed, "README.md"), []byte("seed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runCmd(t, td, nil, "git", "-C", seed, "add", "README.md")
	runCmd(t, td, nil, "git", "-C", seed, "commit", "-m", "seed")
	runCmd(t, td, nil, "git", "-C", seed, "push", "-u", "origin", "main")

	runCmd(t, td, nil, "git", "-C", seed, "checkout", "-b", "feature/test")
	if err := os.WriteFile(filepath.Join(seed, "feature.txt"), []byte("feature\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runCmd(t, td, nil, "git", "-C", seed, "add", "feature.txt")
	runCmd(t, td, nil, "git", "-C", seed, "commit", "-m", "feature")
	runCmd(t, td, nil, "git", "-C", seed, "push", "-u", "origin", "feature/test")

	cloneUserRepoWithGitHubOrigin(t, td, remote, userClone)
	runCmd(t, td, nil, "git", "-C", userClone, "checkout", "feature/test")
	before := runCmd(t, td, nil, "git", "-C", userClone, "rev-parse", "origin/feature/test")

	env := baseEnv(root, workspace, fakeCodex, fakeGH)
	env = append(env, "FAKE_CODEX_WRITE_SECRET_PATTERN_CHANGE=1")
	output := runCmdExpectFailure(t, root, env,
		bin,
		"review",
		userClone,
		"--source-branch", "feature/test",
		"--concurrency", "1",
		"--max-rounds", "2",
		"--mode", "yolo",
		"--no-tui",
	)
	if !strings.Contains(output, "privacy scan failed: secret-like pattern matched in deepreview_test_round.txt") {
		t.Fatalf("expected yolo privacy failure before push, got:\n%s", output)
	}

	runCmd(t, td, nil, "git", "-C", userClone, "fetch", "origin")
	after := runCmd(t, td, nil, "git", "-C", userClone, "rev-parse", "origin/feature/test")
	if before != after {
		t.Fatalf("expected source branch to remain unchanged when delivery validation fails")
	}
}

func TestEndToEndPRModeScansPreparedDeliveryBranch(t *testing.T) {
	root := repoRoot(t)
	bin := buildBinary(t, root)
	fakeCodex, fakeGH := buildFakeBinaries(t, root)

	td := t.TempDir()
	remote := filepath.Join(td, "remote.git")
	seed := filepath.Join(td, "seed")
	userClone := filepath.Join(td, "user")
	workspace := filepath.Join(td, "workspace")
	ghArgsPath := filepath.Join(td, "gh-create-args.txt")

	runCmd(t, td, nil, "git", "init", "--bare", remote)
	runCmd(t, td, nil, "git", "clone", remote, seed)
	runCmd(t, td, nil, "git", "-C", seed, "config", "user.email", "test@example.com")
	runCmd(t, td, nil, "git", "-C", seed, "config", "user.name", "Test User")
	runCmd(t, td, nil, "git", "-C", seed, "checkout", "-b", "main")
	if err := os.WriteFile(filepath.Join(seed, "README.md"), []byte("seed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runCmd(t, td, nil, "git", "-C", seed, "add", "README.md")
	runCmd(t, td, nil, "git", "-C", seed, "commit", "-m", "seed")
	runCmd(t, td, nil, "git", "-C", seed, "push", "-u", "origin", "main")

	runCmd(t, td, nil, "git", "-C", seed, "checkout", "-b", "feature/test")
	if err := os.WriteFile(filepath.Join(seed, "feature.txt"), []byte("feature\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runCmd(t, td, nil, "git", "-C", seed, "add", "feature.txt")
	runCmd(t, td, nil, "git", "-C", seed, "commit", "-m", "feature")
	runCmd(t, td, nil, "git", "-C", seed, "push", "-u", "origin", "feature/test")

	cloneUserRepoWithGitHubOrigin(t, td, remote, userClone)
	runCmd(t, td, nil, "git", "-C", userClone, "checkout", "feature/test")

	env := baseEnv(root, workspace, fakeCodex, fakeGH)
	env = append(env,
		"FAKE_CODEX_DELIVERY_BRANCH_SECRET=1",
		"FAKE_GH_CAPTURE_ARGS_PATH="+ghArgsPath,
	)
	output := runCmd(t, root, env,
		bin,
		"review",
		userClone,
		"--source-branch", "feature/test",
		"--concurrency", "1",
		"--max-rounds", "2",
		"--mode", "pr",
		"--no-tui",
	)
	if !strings.Contains(output, "Draft PR created: https://example.com/olliecrow/test/pull/123") {
		t.Fatalf("expected incomplete draft recovery output, got:\n%s", output)
	}
	if !strings.Contains(output, "delivery status: incomplete (privacy scan failed: secret-like pattern matched in delivery_branch_secret.txt)") {
		t.Fatalf("expected prepared delivery branch failure to be surfaced, got:\n%s", output)
	}
	argsBytes, err := os.ReadFile(ghArgsPath)
	if err != nil {
		t.Fatalf("missing gh args capture: %v", err)
	}
	if !strings.Contains(string(argsBytes), "--draft") {
		t.Fatalf("expected safe fallback draft PR creation, got:\n%s", string(argsBytes))
	}
}

func TestEndToEndPRModeReportsExistingPRAfterMergeabilityFailure(t *testing.T) {
	root := repoRoot(t)
	bin := buildBinary(t, root)
	fakeCodex, fakeGH := buildFakeBinaries(t, root)

	td := t.TempDir()
	remote := filepath.Join(td, "remote.git")
	seed := filepath.Join(td, "seed")
	userClone := filepath.Join(td, "user")
	workspace := filepath.Join(td, "workspace")

	runCmd(t, td, nil, "git", "init", "--bare", remote)
	runCmd(t, td, nil, "git", "clone", remote, seed)
	runCmd(t, td, nil, "git", "-C", seed, "config", "user.email", "test@example.com")
	runCmd(t, td, nil, "git", "-C", seed, "config", "user.name", "Test User")
	runCmd(t, td, nil, "git", "-C", seed, "checkout", "-b", "main")
	if err := os.WriteFile(filepath.Join(seed, "README.md"), []byte("seed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runCmd(t, td, nil, "git", "-C", seed, "add", "README.md")
	runCmd(t, td, nil, "git", "-C", seed, "commit", "-m", "seed")
	runCmd(t, td, nil, "git", "-C", seed, "push", "-u", "origin", "main")

	runCmd(t, td, nil, "git", "-C", seed, "checkout", "-b", "feature/test")
	if err := os.WriteFile(filepath.Join(seed, "feature.txt"), []byte("feature\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runCmd(t, td, nil, "git", "-C", seed, "add", "feature.txt")
	runCmd(t, td, nil, "git", "-C", seed, "commit", "-m", "feature")
	runCmd(t, td, nil, "git", "-C", seed, "push", "-u", "origin", "feature/test")

	cloneUserRepoWithGitHubOrigin(t, td, remote, userClone)
	runCmd(t, td, nil, "git", "-C", userClone, "checkout", "feature/test")

	env := baseEnv(root, workspace, fakeCodex, fakeGH)
	env = append(env, "FAKE_GH_PR_VIEW_MERGEABLE=CONFLICTING")
	output := runCmdExpectFailure(t, root, env,
		bin,
		"review",
		userClone,
		"--source-branch", "feature/test",
		"--concurrency", "1",
		"--max-rounds", "2",
		"--mode", "pr",
		"--no-tui",
	)
	for _, want := range []string{
		"pull request is not mergeable yet (CONFLICTING): https://example.com/olliecrow/test/pull/123",
		"run failed after delivery artifacts were created.",
		"latest PR: https://example.com/olliecrow/test/pull/123",
		"latest delivery commits: https://github.com/example-org/example-repo/commits/deepreview/feature/test/",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected mergeability failure summary to include %q, got:\n%s", want, output)
		}
	}
	if strings.Contains(output, "run exited before delivery; no push or PR was created.") {
		t.Fatalf("expected delivery-aware failure summary, got:\n%s", output)
	}
}

func TestInterruptCancelsRunAndCleansUp(t *testing.T) {
	root := repoRoot(t)
	bin := buildBinary(t, root)
	fakeCodex, fakeGH := buildFakeBinaries(t, root)

	td := t.TempDir()
	remote := filepath.Join(td, "remote.git")
	seed := filepath.Join(td, "seed")
	userClone := filepath.Join(td, "user")
	workspace := filepath.Join(td, "workspace")

	runCmd(t, td, nil, "git", "init", "--bare", remote)
	runCmd(t, td, nil, "git", "clone", remote, seed)
	runCmd(t, td, nil, "git", "-C", seed, "config", "user.email", "test@example.com")
	runCmd(t, td, nil, "git", "-C", seed, "config", "user.name", "Test User")
	runCmd(t, td, nil, "git", "-C", seed, "checkout", "-b", "main")
	if err := os.WriteFile(filepath.Join(seed, "README.md"), []byte("seed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runCmd(t, td, nil, "git", "-C", seed, "add", "README.md")
	runCmd(t, td, nil, "git", "-C", seed, "commit", "-m", "seed")
	runCmd(t, td, nil, "git", "-C", seed, "push", "-u", "origin", "main")

	runCmd(t, td, nil, "git", "-C", seed, "checkout", "-b", "feature/test")
	if err := os.WriteFile(filepath.Join(seed, "feature.txt"), []byte("feature\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runCmd(t, td, nil, "git", "-C", seed, "add", "feature.txt")
	runCmd(t, td, nil, "git", "-C", seed, "commit", "-m", "feature")
	runCmd(t, td, nil, "git", "-C", seed, "push", "-u", "origin", "feature/test")

	cloneUserRepoWithGitHubOrigin(t, td, remote, userClone)
	runCmd(t, td, nil, "git", "-C", userClone, "checkout", "feature/test")
	before := runCmd(t, td, nil, "git", "-C", userClone, "rev-parse", "origin/feature/test")

	env := baseEnv(root, workspace, fakeCodex, fakeGH)
	env = append(env, "FAKE_CODEX_SLEEP_MS=2000")
	stdout, stderr, exitCode := runCmdWithInterrupt(
		t,
		root,
		env,
		350*time.Millisecond,
		bin,
		"review",
		userClone,
		"--source-branch", "feature/test",
		"--concurrency", "1",
		"--max-rounds", "2",
		"--mode", "yolo",
		"--no-tui",
	)
	if exitCode != 130 {
		t.Fatalf("expected interrupt exit code 130, got %d\nstdout=\n%s\nstderr=\n%s", exitCode, stdout, stderr)
	}
	if !strings.Contains(stderr, "run canceled by user; cleanup completed") {
		t.Fatalf("expected cancellation completion message, stderr:\n%s", stderr)
	}
	if !strings.Contains(stderr, "deepreview failure summary:") {
		t.Fatalf("expected artifact summary on interrupt, stderr:\n%s", stderr)
	}
	if !strings.Contains(stderr, "run exited before delivery; no push or PR was created.") {
		t.Fatalf("expected delivery guidance in interrupt summary, stderr:\n%s", stderr)
	}
	if strings.Contains(stdout, "deepreview completed:") {
		t.Fatalf("run should not report successful completion after interrupt")
	}

	var lockFiles []string
	err := filepath.Walk(filepath.Join(workspace, "locks"), func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			if os.IsNotExist(walkErr) {
				return nil
			}
			return walkErr
		}
		if !info.IsDir() && strings.HasSuffix(path, ".lock") {
			lockFiles = append(lockFiles, path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(lockFiles) != 0 {
		t.Fatalf("expected lock cleanup, found: %v", lockFiles)
	}

	runsGlob, err := filepath.Glob(filepath.Join(workspace, "runs", "*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(runsGlob) != 1 {
		t.Fatalf("expected one run directory, got %d", len(runsGlob))
	}
	// Interrupt cleanup can lag a bit behind process exit when the full package
	// is under load, so allow a slightly longer window before declaring failure.
	waitForNoWorktreeDirs(t, runsGlob[0], 15*time.Second)

	runCmd(t, td, nil, "git", "-C", userClone, "fetch", "origin")
	after := runCmd(t, td, nil, "git", "-C", userClone, "rev-parse", "origin/feature/test")
	if before != after {
		t.Fatalf("source branch should remain unchanged after interrupt")
	}
}

func TestEndToEndPRModeAutoSanitizesCandidateBranchDocsWithoutMutatingDefaultBranch(t *testing.T) {
	root := repoRoot(t)
	bin := buildBinary(t, root)
	fakeCodex, fakeGH := buildFakeBinaries(t, root)

	td := t.TempDir()
	remote := filepath.Join(td, "remote.git")
	seed := filepath.Join(td, "seed")
	userClone := filepath.Join(td, "user")
	workspace := filepath.Join(td, "workspace")

	runCmd(t, td, nil, "git", "init", "--bare", remote)
	runCmd(t, td, nil, "git", "clone", remote, seed)
	runCmd(t, td, nil, "git", "-C", seed, "config", "user.email", "test@example.com")
	runCmd(t, td, nil, "git", "-C", seed, "config", "user.name", "Test User")
	runCmd(t, td, nil, "git", "-C", seed, "checkout", "-b", "main")
	if err := os.WriteFile(filepath.Join(seed, "README.md"), []byte("seed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(seed, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(seed, "docs", "generated.md"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runCmd(t, td, nil, "git", "-C", seed, "add", "README.md", "docs/generated.md")
	runCmd(t, td, nil, "git", "-C", seed, "commit", "-m", "seed")
	runCmd(t, td, nil, "git", "-C", seed, "push", "-u", "origin", "main")

	runCmd(t, td, nil, "git", "-C", seed, "checkout", "-b", "feature/test")
	if err := os.WriteFile(filepath.Join(seed, "feature.txt"), []byte("feature\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runCmd(t, td, nil, "git", "-C", seed, "add", "feature.txt")
	runCmd(t, td, nil, "git", "-C", seed, "commit", "-m", "feature")
	runCmd(t, td, nil, "git", "-C", seed, "push", "-u", "origin", "feature/test")

	cloneUserRepoWithGitHubOrigin(t, td, remote, userClone)
	runCmd(t, td, nil, "git", "-C", userClone, "checkout", "feature/test")

	env := baseEnv(root, workspace, fakeCodex, fakeGH)
	env = append(env, "FAKE_CODEX_WRITE_DOC_LOCAL_PATH_CHANGE=1")
	output := runCmd(t, root, env,
		bin,
		"review",
		userClone,
		"--source-branch", "feature/test",
		"--concurrency", "1",
		"--max-rounds", "2",
		"--mode", "pr",
		"--no-tui",
	)
	if !strings.Contains(output, "PR created: https://example.com/olliecrow/test/pull/123") {
		t.Fatalf("expected pr delivery output, got: %s", output)
	}

	runCmd(t, td, nil, "git", "-C", userClone, "fetch", "origin")
	mainDoc := runCmd(t, td, nil, "git", "-C", userClone, "show", "origin/main:docs/generated.md")
	if strings.Contains(mainDoc, "/Users/") {
		t.Fatalf("default branch doc must not be mutated by delivery-stage sanitization: %s", mainDoc)
	}
	if strings.TrimSpace(mainDoc) != "base" {
		t.Fatalf("expected default branch doc to remain unchanged, got: %q", mainDoc)
	}

	refsOut := runCmd(t, td, nil, "git", "-C", userClone, "for-each-ref", "--format=%(refname:short)", "refs/remotes/origin/deepreview")
	var deliveryRef string
	for _, ref := range strings.Split(strings.TrimSpace(refsOut), "\n") {
		ref = strings.TrimSpace(ref)
		if strings.Contains(ref, "origin/deepreview/feature/test/") {
			deliveryRef = ref
			break
		}
	}
	if deliveryRef == "" {
		t.Fatalf("expected deepreview delivery ref, got: %s", refsOut)
	}
	deliveredDoc := runCmd(t, td, nil, "git", "-C", userClone, "show", deliveryRef+":docs/generated.md")
	if strings.Contains(deliveredDoc, "/Users/") {
		t.Fatalf("expected candidate doc to be sanitized before delivery, got: %s", deliveredDoc)
	}
	if !strings.Contains(deliveredDoc, "/path/to/project") {
		t.Fatalf("expected candidate doc to contain sanitized placeholder, got: %s", deliveredDoc)
	}
}

func TestReviewStageRestartsStalledWorkerAndRequiresFullCoverage(t *testing.T) {
	root := repoRoot(t)
	bin := buildBinary(t, root)
	fakeCodex, fakeGH := buildFakeBinaries(t, root)

	td := t.TempDir()
	remote := filepath.Join(td, "remote.git")
	seed := filepath.Join(td, "seed")
	userClone := filepath.Join(td, "user")
	workspace := filepath.Join(td, "workspace")

	runCmd(t, td, nil, "git", "init", "--bare", remote)
	runCmd(t, td, nil, "git", "clone", remote, seed)
	runCmd(t, td, nil, "git", "-C", seed, "config", "user.email", "test@example.com")
	runCmd(t, td, nil, "git", "-C", seed, "config", "user.name", "Test User")
	runCmd(t, td, nil, "git", "-C", seed, "checkout", "-b", "main")
	if err := os.WriteFile(filepath.Join(seed, "README.md"), []byte("seed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runCmd(t, td, nil, "git", "-C", seed, "add", "README.md")
	runCmd(t, td, nil, "git", "-C", seed, "commit", "-m", "seed")
	runCmd(t, td, nil, "git", "-C", seed, "push", "-u", "origin", "main")

	cloneUserRepoWithGitHubOrigin(t, td, remote, userClone)
	runCmd(t, td, nil, "git", "-C", userClone, "checkout", "main")

	env := baseEnv(root, workspace, fakeCodex, fakeGH)
	env = append(env,
		"FAKE_CODEX_SKIP_CODE_CHANGE=1",
		"FAKE_CODEX_REQUIRE_SELF_AUDIT_REVIEW_PROMPT=1",
		"DEEPREVIEW_REVIEW_INACTIVITY_SECONDS=2",
		"DEEPREVIEW_REVIEW_ACTIVITY_POLL_SECONDS=1",
		"DEEPREVIEW_REVIEW_MAX_RESTARTS=1",
		"FAKE_CODEX_STALL_ONCE_MS_WORKER_2=15000",
	)

	output, logs := runCmdCapture(t, root, env,
		bin,
		"review",
		userClone,
		"--source-branch", "main",
		"--concurrency", "2",
		"--max-rounds", "2",
		"--mode", "pr",
		"--no-tui",
	)
	if !strings.Contains(output, "delivery skipped: no deliverable repository changes were produced") {
		t.Fatalf("expected skipped-delivery summary output, got: %s", output)
	}
	if !strings.Contains(logs, "worker-02 inactive for") {
		t.Fatalf("expected worker-02 inactivity restart evidence in logs, got:\n%s", logs)
	}

	runsGlob, err := filepath.Glob(filepath.Join(workspace, "runs", "*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(runsGlob) != 1 {
		t.Fatalf("expected 1 run dir, got %d", len(runsGlob))
	}
	runDir := runsGlob[0]
	if _, err := os.Stat(filepath.Join(runDir, "round-01", "review-01.md")); err != nil {
		t.Fatalf("expected worker-01 review artifact, got: %v", err)
	}
	if _, err := os.Stat(filepath.Join(runDir, "round-01", "review-02.md")); err != nil {
		t.Fatalf("expected worker-02 review artifact after restart, got: %v", err)
	}
}

func TestEndToEndPRModeUsesNormalInheritedCodexEnvironment(t *testing.T) {
	root := repoRoot(t)
	bin := buildBinary(t, root)
	fakeCodex, fakeGH := buildFakeBinaries(t, root)

	td := t.TempDir()
	remote := filepath.Join(td, "remote.git")
	seed := filepath.Join(td, "seed")
	userClone := filepath.Join(td, "user")
	workspace := filepath.Join(td, "workspace")

	runCmd(t, td, nil, "git", "init", "--bare", remote)
	runCmd(t, td, nil, "git", "clone", remote, seed)
	runCmd(t, td, nil, "git", "-C", seed, "config", "user.email", "test@example.com")
	runCmd(t, td, nil, "git", "-C", seed, "config", "user.name", "Test User")
	runCmd(t, td, nil, "git", "-C", seed, "checkout", "-b", "main")
	if err := os.WriteFile(filepath.Join(seed, "go.mod"), []byte("module example.com/testrepo\n\ngo 1.25.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(seed, "README.md"), []byte("seed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(seed, "hello"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(seed, "hello", "hello.go"), []byte("package hello\n\nfunc Value() int { return 1 }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(seed, "hello", "hello_test.go"), []byte("package hello\n\nimport \"testing\"\n\nfunc TestValue(t *testing.T) {\n\tif Value() != 1 {\n\t\tt.Fatalf(\"unexpected value\")\n\t}\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runCmd(t, td, nil, "git", "-C", seed, "add", "README.md", "go.mod", "hello/hello.go", "hello/hello_test.go")
	runCmd(t, td, nil, "git", "-C", seed, "commit", "-m", "seed")
	runCmd(t, td, nil, "git", "-C", seed, "push", "-u", "origin", "main")

	cloneUserRepoWithGitHubOrigin(t, td, remote, userClone)
	runCmd(t, td, nil, "git", "-C", userClone, "checkout", "main")

	env := baseEnv(root, workspace, fakeCodex, fakeGH)
	env = append(env,
		"FAKE_CODEX_SKIP_CODE_CHANGE=1",
		"FAKE_CODEX_REQUIRE_PROMPT_OUTPUTS_WITHIN_CWD=1",
		"FAKE_CODEX_RUN_GO_TEST_WITH_INHERITED_ENV=1",
	)
	output := runCmd(t, root, env,
		bin,
		"review",
		userClone,
		"--source-branch", "main",
		"--concurrency", "1",
		"--max-rounds", "2",
		"--mode", "pr",
		"--no-tui",
	)
	if !strings.Contains(output, "delivery skipped: no deliverable repository changes were produced") {
		t.Fatalf("expected skipped-delivery summary output, got: %s", output)
	}
}

func TestEndToEndPRModeIgnoresUntrackedOperationalArtifactsDuringRoundChangeDetection(t *testing.T) {
	root := repoRoot(t)
	bin := buildBinary(t, root)
	fakeCodex, fakeGH := buildFakeBinaries(t, root)

	td := t.TempDir()
	remote := filepath.Join(td, "remote.git")
	seed := filepath.Join(td, "seed")
	userClone := filepath.Join(td, "user")
	workspace := filepath.Join(td, "workspace")

	runCmd(t, td, nil, "git", "init", "--bare", remote)
	runCmd(t, td, nil, "git", "clone", remote, seed)
	runCmd(t, td, nil, "git", "-C", seed, "config", "user.email", "test@example.com")
	runCmd(t, td, nil, "git", "-C", seed, "config", "user.name", "Test User")
	runCmd(t, td, nil, "git", "-C", seed, "checkout", "-b", "main")
	if err := os.WriteFile(filepath.Join(seed, "README.md"), []byte("seed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runCmd(t, td, nil, "git", "-C", seed, "add", "README.md")
	runCmd(t, td, nil, "git", "-C", seed, "commit", "-m", "seed")
	runCmd(t, td, nil, "git", "-C", seed, "push", "-u", "origin", "main")

	cloneUserRepoWithGitHubOrigin(t, td, remote, userClone)
	runCmd(t, td, nil, "git", "-C", userClone, "checkout", "main")

	env := baseEnv(root, workspace, fakeCodex, fakeGH)
	env = append(env,
		"FAKE_CODEX_SKIP_CODE_CHANGE=1",
		"FAKE_CODEX_WRITE_OPERATIONAL_TMP=1",
	)
	output := runCmd(t, root, env,
		bin,
		"review",
		userClone,
		"--source-branch", "main",
		"--concurrency", "1",
		"--max-rounds", "2",
		"--mode", "pr",
		"--no-tui",
	)
	if !strings.Contains(output, "delivery skipped: no deliverable repository changes were produced") {
		t.Fatalf("expected skipped-delivery summary output, got: %s", output)
	}

	runsGlob, err := filepath.Glob(filepath.Join(workspace, "runs", "*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(runsGlob) != 1 {
		t.Fatalf("expected 1 run dir, got %d", len(runsGlob))
	}
	runDir := runsGlob[0]
	if _, err := os.Stat(filepath.Join(runDir, "round-02", "round-summary.md")); err != nil {
		t.Fatalf("expected confirmation round artifacts when first stop had no changes, got: %v", err)
	}
	finalSummaryBytes, err := os.ReadFile(filepath.Join(runDir, "final-summary.md"))
	if err != nil {
		t.Fatalf("expected final-summary.md after skipped delivery: %v", err)
	}
	if !strings.Contains(string(finalSummaryBytes), "- delivery: `skipped`") {
		t.Fatalf("expected skipped delivery marker in final summary, got:\n%s", string(finalSummaryBytes))
	}
	if _, err := os.Stat(filepath.Join(runDir, "pr-body.md")); !os.IsNotExist(err) {
		t.Fatalf("pr-body.md should not be created on skipped delivery")
	}
}

func TestEndToEndPRModeAllowsNewTrackedFilesUnderRepoOwnedOperationalPath(t *testing.T) {
	root := repoRoot(t)
	bin := buildBinary(t, root)
	fakeCodex, fakeGH := buildFakeBinaries(t, root)

	td := t.TempDir()
	remote := filepath.Join(td, "remote.git")
	seed := filepath.Join(td, "seed")
	userClone := filepath.Join(td, "user")
	workspace := filepath.Join(td, "workspace")

	runCmd(t, td, nil, "git", "init", "--bare", remote)
	runCmd(t, td, nil, "git", "clone", remote, seed)
	runCmd(t, td, nil, "git", "-C", seed, "config", "user.email", "test@example.com")
	runCmd(t, td, nil, "git", "-C", seed, "config", "user.name", "Test User")
	runCmd(t, td, nil, "git", "-C", seed, "checkout", "-b", "main")
	if err := os.WriteFile(filepath.Join(seed, "README.md"), []byte("seed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(seed, ".tmp"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(seed, ".tmp", "tracked.txt"), []byte("repo-owned\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runCmd(t, td, nil, "git", "-C", seed, "add", "README.md", ".tmp/tracked.txt")
	runCmd(t, td, nil, "git", "-C", seed, "commit", "-m", "seed")
	runCmd(t, td, nil, "git", "-C", seed, "push", "-u", "origin", "main")

	cloneUserRepoWithGitHubOrigin(t, td, remote, userClone)
	runCmd(t, td, nil, "git", "-C", userClone, "checkout", "main")

	env := baseEnv(root, workspace, fakeCodex, fakeGH)
	env = append(env,
		"FAKE_CODEX_ADD_REPO_TMP_FILE=1",
		"FAKE_CODEX_WRITE_OPERATIONAL_TMP=1",
	)
	output := runCmd(t, root, env,
		bin,
		"review",
		userClone,
		"--source-branch", "main",
		"--concurrency", "1",
		"--max-rounds", "2",
		"--mode", "pr",
		"--no-tui",
	)
	if !strings.Contains(output, "PR created: https://example.com/olliecrow/test/pull/123") {
		t.Fatalf("expected pr delivery output, got: %s", output)
	}

	runCmd(t, td, nil, "git", "-C", userClone, "fetch", "origin")
	refsOut := runCmd(t, td, nil, "git", "-C", userClone, "for-each-ref", "--format=%(refname:short)", "refs/remotes/origin/deepreview")
	var deliveryRef string
	for _, ref := range strings.Split(strings.TrimSpace(refsOut), "\n") {
		ref = strings.TrimSpace(ref)
		if strings.Contains(ref, "origin/deepreview/main/") {
			deliveryRef = ref
			break
		}
	}
	if deliveryRef == "" {
		t.Fatalf("expected deepreview delivery ref, got: %s", refsOut)
	}
	tree := runCmd(t, td, nil, "git", "-C", userClone, "ls-tree", "-r", "--name-only", deliveryRef)
	if !strings.Contains(tree, ".tmp/repo-added.txt") {
		t.Fatalf("expected repo-owned .tmp file to be delivered, ref=%s tree:\n%s", deliveryRef, tree)
	}
	if strings.Contains(tree, ".tmp/go-build-cache/") {
		t.Fatalf("did not expect operational go-build-cache under repo-owned .tmp path, ref=%s tree:\n%s", deliveryRef, tree)
	}
}

func TestEndToEndPRModeReservesDeepreviewTmpArtifactsInsideRepoOwnedTmp(t *testing.T) {
	root := repoRoot(t)
	bin := buildBinary(t, root)
	fakeCodex, fakeGH := buildFakeBinaries(t, root)

	td := t.TempDir()
	remote := filepath.Join(td, "remote.git")
	seed := filepath.Join(td, "seed")
	userClone := filepath.Join(td, "user")
	workspace := filepath.Join(td, "workspace")

	runCmd(t, td, nil, "git", "init", "--bare", remote)
	runCmd(t, td, nil, "git", "clone", remote, seed)
	runCmd(t, td, nil, "git", "-C", seed, "config", "user.email", "test@example.com")
	runCmd(t, td, nil, "git", "-C", seed, "config", "user.name", "Test User")
	runCmd(t, td, nil, "git", "-C", seed, "checkout", "-b", "main")
	if err := os.WriteFile(filepath.Join(seed, "README.md"), []byte("seed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runCmd(t, td, nil, "git", "-C", seed, "add", "README.md")
	runCmd(t, td, nil, "git", "-C", seed, "commit", "-m", "seed main")
	runCmd(t, td, nil, "git", "-C", seed, "push", "-u", "origin", "main")

	runCmd(t, td, nil, "git", "-C", seed, "checkout", "-b", "feature/test")
	if err := os.MkdirAll(filepath.Join(seed, ".tmp"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(seed, ".tmp", "tracked.txt"), []byte("repo-owned\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runCmd(t, td, nil, "git", "-C", seed, "add", "README.md", ".tmp/tracked.txt")
	runCmd(t, td, nil, "git", "-C", seed, "commit", "-m", "seed")
	runCmd(t, td, nil, "git", "-C", seed, "push", "-u", "origin", "feature/test")

	cloneUserRepoWithGitHubOrigin(t, td, remote, userClone)
	runCmd(t, td, nil, "git", "-C", userClone, "checkout", "feature/test")

	env := baseEnv(root, workspace, fakeCodex, fakeGH)
	output := runCmd(t, root, env,
		bin,
		"review",
		userClone,
		"--source-branch", "feature/test",
		"--concurrency", "1",
		"--max-rounds", "2",
		"--mode", "pr",
		"--no-tui",
	)
	if !strings.Contains(output, "PR created: https://example.com/olliecrow/test/pull/123") {
		t.Fatalf("expected PR delivery output, got: %s", output)
	}

	runCmd(t, td, nil, "git", "-C", userClone, "fetch", "origin")
	refsOut := runCmd(t, td, nil, "git", "-C", userClone, "for-each-ref", "--format=%(refname:short)", "refs/remotes/origin/deepreview")
	var deliveryRef string
	for _, ref := range strings.Split(strings.TrimSpace(refsOut), "\n") {
		ref = strings.TrimSpace(ref)
		if strings.Contains(ref, "origin/deepreview/feature/test/") {
			deliveryRef = ref
			break
		}
	}
	if deliveryRef == "" {
		t.Fatalf("expected deepreview delivery ref, got: %s", refsOut)
	}
	tree := runCmd(t, td, nil, "git", "-C", userClone, "ls-tree", "-r", "--name-only", deliveryRef)
	if !strings.Contains(tree, ".tmp/tracked.txt") {
		t.Fatalf("expected repo-owned .tmp baseline content to remain deliverable, ref=%s tree:\n%s", deliveryRef, tree)
	}
	if strings.Contains(tree, ".tmp/deepreview/") {
		t.Fatalf("did not expect reserved deepreview tmp artifacts to be delivered, ref=%s tree:\n%s", deliveryRef, tree)
	}
}

func TestRunFailsWhenExecuteForceCommitsNewOperationalArtifacts(t *testing.T) {
	root := repoRoot(t)
	bin := buildBinary(t, root)
	fakeCodex, fakeGH := buildFakeBinaries(t, root)

	td := t.TempDir()
	remote := filepath.Join(td, "remote.git")
	seed := filepath.Join(td, "seed")
	userClone := filepath.Join(td, "user")
	workspace := filepath.Join(td, "workspace")

	runCmd(t, td, nil, "git", "init", "--bare", remote)
	runCmd(t, td, nil, "git", "clone", remote, seed)
	runCmd(t, td, nil, "git", "-C", seed, "config", "user.email", "test@example.com")
	runCmd(t, td, nil, "git", "-C", seed, "config", "user.name", "Test User")
	runCmd(t, td, nil, "git", "-C", seed, "checkout", "-b", "main")
	if err := os.WriteFile(filepath.Join(seed, "README.md"), []byte("seed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runCmd(t, td, nil, "git", "-C", seed, "add", "README.md")
	runCmd(t, td, nil, "git", "-C", seed, "commit", "-m", "seed")
	runCmd(t, td, nil, "git", "-C", seed, "push", "-u", "origin", "main")

	cloneUserRepoWithGitHubOrigin(t, td, remote, userClone)
	runCmd(t, td, nil, "git", "-C", userClone, "checkout", "main")

	env := baseEnv(root, workspace, fakeCodex, fakeGH)
	env = append(env, "FAKE_CODEX_FORCE_ADD_OPERATIONAL_TMP=1")
	output := runCmdExpectFailure(t, root, env,
		bin,
		"review",
		userClone,
		"--source-branch", "main",
		"--concurrency", "1",
		"--max-rounds", "1",
		"--mode", "pr",
		"--no-tui",
	)
	if !strings.Contains(output, "operational artifacts must not be committed: .tmp/go-build-cache/forced-cache.txt") {
		t.Fatalf("expected operational artifact validation failure, got: %s", output)
	}

	runsGlob, err := filepath.Glob(filepath.Join(workspace, "runs", "*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(runsGlob) != 1 {
		t.Fatalf("expected 1 run dir, got %d", len(runsGlob))
	}
	runDir := runsGlob[0]
	if _, err := os.Stat(filepath.Join(runDir, "final-summary.md")); !os.IsNotExist(err) {
		t.Fatalf("did not expect final-summary.md after execute-stage validation failure")
	}
}

func TestEndToEndPRModeFailsWhenNoChangesButCodexStillRequestsAnotherRound(t *testing.T) {
	root := repoRoot(t)
	bin := buildBinary(t, root)
	fakeCodex, fakeGH := buildFakeBinaries(t, root)

	td := t.TempDir()
	remote := filepath.Join(td, "remote.git")
	seed := filepath.Join(td, "seed")
	userClone := filepath.Join(td, "user")
	workspace := filepath.Join(td, "workspace")

	runCmd(t, td, nil, "git", "init", "--bare", remote)
	runCmd(t, td, nil, "git", "clone", remote, seed)
	runCmd(t, td, nil, "git", "-C", seed, "config", "user.email", "test@example.com")
	runCmd(t, td, nil, "git", "-C", seed, "config", "user.name", "Test User")
	runCmd(t, td, nil, "git", "-C", seed, "checkout", "-b", "main")
	if err := os.WriteFile(filepath.Join(seed, "README.md"), []byte("seed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runCmd(t, td, nil, "git", "-C", seed, "add", "README.md")
	runCmd(t, td, nil, "git", "-C", seed, "commit", "-m", "seed")
	runCmd(t, td, nil, "git", "-C", seed, "push", "-u", "origin", "main")

	cloneUserRepoWithGitHubOrigin(t, td, remote, userClone)
	runCmd(t, td, nil, "git", "-C", userClone, "checkout", "main")

	env := baseEnv(root, workspace, fakeCodex, fakeGH)
	env = append(env,
		"FAKE_CODEX_SKIP_CODE_CHANGE=1",
		"FAKE_CODEX_DECISION=continue",
		"DEEPREVIEW_REVIEW_INACTIVITY_SECONDS=2",
		"DEEPREVIEW_REVIEW_ACTIVITY_POLL_SECONDS=1",
		"DEEPREVIEW_REVIEW_MAX_RESTARTS=1",
		"FAKE_CODEX_STALL_ONCE_CONTAINS=prompt 2 of 2",
		"FAKE_CODEX_STALL_ONCE_MS_MATCH=15000",
	)
	output := runCmdExpectFailure(t, root, env,
		bin,
		"review",
		userClone,
		"--source-branch", "main",
		"--concurrency", "1",
		"--max-rounds", "1",
		"--mode", "pr",
		"--no-tui",
	)
	if !strings.Contains(output, "requires another review round") {
		t.Fatalf("expected max-round failure output, got: %s", output)
	}

	runsGlob, err := filepath.Glob(filepath.Join(workspace, "runs", "*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(runsGlob) != 1 {
		t.Fatalf("expected 1 run dir, got %d", len(runsGlob))
	}
	runDir := runsGlob[0]
	if _, err := os.Stat(filepath.Join(runDir, "final-summary.md")); !os.IsNotExist(err) {
		t.Fatalf("did not expect final-summary.md after max-round failure")
	}
}

func TestEndToEndPRModeResetsExecuteWorktreeBeforeRetryAfterStall(t *testing.T) {
	root := repoRoot(t)
	bin := buildBinary(t, root)
	fakeCodex, fakeGH := buildFakeBinaries(t, root)
	stallingCodex := buildExecuteStallCodexWrapper(t, fakeCodex)

	td := t.TempDir()
	remote := filepath.Join(td, "remote.git")
	seed := filepath.Join(td, "seed")
	userClone := filepath.Join(td, "user")
	workspace := filepath.Join(td, "workspace")
	stallMarkerPath := filepath.Join(td, "stall-once.marker")

	runCmd(t, td, nil, "git", "init", "--bare", remote)
	runCmd(t, td, nil, "git", "clone", remote, seed)
	runCmd(t, td, nil, "git", "-C", seed, "config", "user.email", "test@example.com")
	runCmd(t, td, nil, "git", "-C", seed, "config", "user.name", "Test User")
	runCmd(t, td, nil, "git", "-C", seed, "checkout", "-b", "main")
	if err := os.WriteFile(filepath.Join(seed, "README.md"), []byte("seed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runCmd(t, td, nil, "git", "-C", seed, "add", "README.md")
	runCmd(t, td, nil, "git", "-C", seed, "commit", "-m", "seed")
	runCmd(t, td, nil, "git", "-C", seed, "push", "-u", "origin", "main")

	cloneUserRepoWithGitHubOrigin(t, td, remote, userClone)
	runCmd(t, td, nil, "git", "-C", userClone, "checkout", "main")

	env := baseEnv(root, workspace, stallingCodex, fakeGH)
	env = append(env,
		"DEEPREVIEW_REVIEW_INACTIVITY_SECONDS=2",
		"DEEPREVIEW_REVIEW_ACTIVITY_POLL_SECONDS=1",
		"DEEPREVIEW_REVIEW_MAX_RESTARTS=1",
		"STALL_MARKER_PATH="+stallMarkerPath,
		"STALL_SECONDS=15",
	)
	output, logs := runCmdCapture(t, root, env,
		bin,
		"review",
		userClone,
		"--source-branch", "main",
		"--concurrency", "1",
		"--max-rounds", "2",
		"--mode", "pr",
		"--no-tui",
	)
	if !strings.Contains(output, "PR created: https://example.com/olliecrow/test/pull/123") {
		t.Fatalf("expected PR delivery output, got: %s", output)
	}
	if !strings.Contains(logs, "execute / implement, verify, finalize inactive for") {
		t.Fatalf("expected execute-stage inactivity restart evidence in logs, got:\n%s", logs)
	}

	runCmd(t, td, nil, "git", "-C", userClone, "fetch", "origin")
	refsOut := runCmd(t, td, nil, "git", "-C", userClone, "for-each-ref", "--format=%(refname:short)", "refs/remotes/origin/deepreview")
	var deliveryRef string
	for _, ref := range strings.Split(strings.TrimSpace(refsOut), "\n") {
		ref = strings.TrimSpace(ref)
		if strings.Contains(ref, "origin/deepreview/main/") {
			deliveryRef = ref
			break
		}
	}
	if deliveryRef == "" {
		t.Fatalf("expected deepreview delivery ref, got: %s", refsOut)
	}
	tree := runCmd(t, td, nil, "git", "-C", userClone, "ls-tree", "-r", "--name-only", deliveryRef)
	if strings.Contains(tree, "stale_execute_attempt.txt") {
		t.Fatalf("stalled execute attempt file leaked into delivery ref %s:\n%s", deliveryRef, tree)
	}
	logOut := runCmd(t, td, nil, "git", "-C", userClone, "log", "--format=%s", deliveryRef)
	if strings.Contains(logOut, "stale stalled attempt") {
		t.Fatalf("stalled execute attempt commit leaked into delivery history for %s:\n%s", deliveryRef, logOut)
	}
}

func TestEndToEndPRModeDoesNotReuseStaleExecuteStatusArtifactsFromKilledPrompt(t *testing.T) {
	root := repoRoot(t)
	bin := buildBinary(t, root)
	fakeCodex, fakeGH := buildFakeBinaries(t, root)
	stallingCodex := buildExecuteArtifactStallCodexWrapper(t, fakeCodex)

	td := t.TempDir()
	remote := filepath.Join(td, "remote.git")
	seed := filepath.Join(td, "seed")
	userClone := filepath.Join(td, "user")
	workspace := filepath.Join(td, "workspace")
	stallMarkerPath := filepath.Join(td, "stall-once.marker")

	runCmd(t, td, nil, "git", "init", "--bare", remote)
	runCmd(t, td, nil, "git", "clone", remote, seed)
	runCmd(t, td, nil, "git", "-C", seed, "config", "user.email", "test@example.com")
	runCmd(t, td, nil, "git", "-C", seed, "config", "user.name", "Test User")
	runCmd(t, td, nil, "git", "-C", seed, "checkout", "-b", "main")
	if err := os.WriteFile(filepath.Join(seed, "README.md"), []byte("seed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runCmd(t, td, nil, "git", "-C", seed, "add", "README.md")
	runCmd(t, td, nil, "git", "-C", seed, "commit", "-m", "seed")
	runCmd(t, td, nil, "git", "-C", seed, "push", "-u", "origin", "main")

	cloneUserRepoWithGitHubOrigin(t, td, remote, userClone)
	runCmd(t, td, nil, "git", "-C", userClone, "checkout", "main")

	env := baseEnv(root, workspace, stallingCodex, fakeGH)
	env = append(env,
		"FAKE_CODEX_SKIP_CODE_CHANGE=1",
		"DEEPREVIEW_REVIEW_INACTIVITY_SECONDS=2",
		"DEEPREVIEW_REVIEW_ACTIVITY_POLL_SECONDS=1",
		"DEEPREVIEW_REVIEW_MAX_RESTARTS=1",
		"STALL_MARKER_PATH="+stallMarkerPath,
		"STALL_SECONDS=15",
	)
	output := runCmdExpectFailure(t, root, env,
		bin,
		"review",
		userClone,
		"--source-branch", "main",
		"--concurrency", "1",
		"--max-rounds", "1",
		"--mode", "pr",
		"--no-tui",
	)
	if !strings.Contains(output, "file does not exist") {
		t.Fatalf("expected execute-stage failure from missing fresh status/summary outputs, got: %s", output)
	}
	if strings.Contains(output, "requires another review round") {
		t.Fatalf("did not expect stale round status to drive max-round failure, got: %s", output)
	}

	runsGlob, err := filepath.Glob(filepath.Join(workspace, "runs", "*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(runsGlob) != 1 {
		t.Fatalf("expected 1 run dir, got %d", len(runsGlob))
	}
	runDir := runsGlob[0]
	if _, err := os.Stat(filepath.Join(runDir, "round-01", "round-status.json")); !os.IsNotExist(err) {
		t.Fatalf("did not expect canonical round-status.json after stale-artifact rejection, err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(runDir, "round-01", "round-summary.md")); !os.IsNotExist(err) {
		t.Fatalf("did not expect canonical round-summary.md after stale-artifact rejection, err=%v", err)
	}
}

func TestEndToEndPRModeCompletesAfterTwoConsecutiveStopDecisionsWithoutChanges(t *testing.T) {
	root := repoRoot(t)
	bin := buildBinary(t, root)
	fakeCodex, fakeGH := buildFakeBinaries(t, root)

	td := t.TempDir()
	remote := filepath.Join(td, "remote.git")
	seed := filepath.Join(td, "seed")
	userClone := filepath.Join(td, "user")
	workspace := filepath.Join(td, "workspace")

	runCmd(t, td, nil, "git", "init", "--bare", remote)
	runCmd(t, td, nil, "git", "clone", remote, seed)
	runCmd(t, td, nil, "git", "-C", seed, "config", "user.email", "test@example.com")
	runCmd(t, td, nil, "git", "-C", seed, "config", "user.name", "Test User")
	runCmd(t, td, nil, "git", "-C", seed, "checkout", "-b", "main")
	if err := os.WriteFile(filepath.Join(seed, "README.md"), []byte("seed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runCmd(t, td, nil, "git", "-C", seed, "add", "README.md")
	runCmd(t, td, nil, "git", "-C", seed, "commit", "-m", "seed")
	runCmd(t, td, nil, "git", "-C", seed, "push", "-u", "origin", "main")

	cloneUserRepoWithGitHubOrigin(t, td, remote, userClone)
	runCmd(t, td, nil, "git", "-C", userClone, "checkout", "main")
	before := runCmd(t, td, nil, "git", "-C", userClone, "rev-parse", "origin/main")

	env := baseEnv(root, workspace, fakeCodex, fakeGH)
	env = append(env, "FAKE_CODEX_SKIP_CODE_CHANGE=1")
	output := runCmd(t, root, env,
		bin,
		"review",
		userClone,
		"--source-branch", "main",
		"--concurrency", "1",
		"--max-rounds", "2",
		"--mode", "pr",
		"--no-tui",
	)
	if !strings.Contains(output, "delivery skipped: no deliverable repository changes were produced") {
		t.Fatalf("expected skipped-delivery summary output, got: %s", output)
	}

	runCmd(t, td, nil, "git", "-C", userClone, "fetch", "origin")
	after := runCmd(t, td, nil, "git", "-C", userClone, "rev-parse", "origin/main")
	if before != after {
		t.Fatalf("source branch should remain unchanged after skipped delivery")
	}

	refsOut := runCmd(t, td, nil, "git", "-C", userClone, "for-each-ref", "--format=%(refname:short)", "refs/remotes/origin/deepreview")
	if strings.TrimSpace(refsOut) != "" {
		t.Fatalf("expected no delivery branch refs after no-deliverable failure, got: %s", refsOut)
	}

	runsGlob, err := filepath.Glob(filepath.Join(workspace, "runs", "*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(runsGlob) != 1 {
		t.Fatalf("expected 1 run dir, got %d", len(runsGlob))
	}
	runDir := runsGlob[0]
	if _, err := os.Stat(filepath.Join(runDir, "round-02", "round-summary.md")); err != nil {
		t.Fatalf("expected confirmation round artifacts, got: %v", err)
	}
	finalSummaryBytes, err := os.ReadFile(filepath.Join(runDir, "final-summary.md"))
	if err != nil {
		t.Fatalf("expected final-summary.md after skipped delivery: %v", err)
	}
	if !strings.Contains(string(finalSummaryBytes), "- delivery: `skipped`") {
		t.Fatalf("expected skipped delivery marker in final summary, got:\n%s", string(finalSummaryBytes))
	}
	if _, err := os.Stat(filepath.Join(runDir, "pr-body.md")); !os.IsNotExist(err) {
		t.Fatalf("pr-body.md should not be created on skipped delivery")
	}
}

func TestEndToEndPRModeDeliverySanitizesDocsAndSkipsPRWhenSanitizationRemovesAllChanges(t *testing.T) {
	root := repoRoot(t)
	bin := buildBinary(t, root)
	fakeCodex, fakeGH := buildFakeBinaries(t, root)

	td := t.TempDir()
	remote := filepath.Join(td, "remote.git")
	seed := filepath.Join(td, "seed")
	userClone := filepath.Join(td, "user")
	workspace := filepath.Join(td, "workspace")

	runCmd(t, td, nil, "git", "init", "--bare", remote)
	runCmd(t, td, nil, "git", "clone", remote, seed)
	runCmd(t, td, nil, "git", "-C", seed, "config", "user.email", "test@example.com")
	runCmd(t, td, nil, "git", "-C", seed, "config", "user.name", "Test User")
	runCmd(t, td, nil, "git", "-C", seed, "checkout", "-b", "main")
	if err := os.WriteFile(filepath.Join(seed, "README.md"), []byte("seed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(seed, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(seed, "docs", "generated.md"), []byte("path /path/to/project\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runCmd(t, td, nil, "git", "-C", seed, "add", "README.md", "docs/generated.md")
	runCmd(t, td, nil, "git", "-C", seed, "commit", "-m", "seed")
	runCmd(t, td, nil, "git", "-C", seed, "push", "-u", "origin", "main")

	cloneUserRepoWithGitHubOrigin(t, td, remote, userClone)
	runCmd(t, td, nil, "git", "-C", userClone, "checkout", "main")

	env := baseEnv(root, workspace, fakeCodex, fakeGH)
	env = append(env, "FAKE_CODEX_WRITE_DOC_LOCAL_PATH_CHANGE=1")
	output := runCmd(t, root, env,
		bin,
		"review",
		userClone,
		"--source-branch", "main",
		"--concurrency", "1",
		"--max-rounds", "2",
		"--mode", "pr",
		"--no-tui",
	)
	if !strings.Contains(output, "delivery skipped: delivery preparation removed all deliverable repository changes") {
		t.Fatalf("expected delivery skip after local sanitization removed all changes, got: %s", output)
	}

	runsGlob, err := filepath.Glob(filepath.Join(workspace, "runs", "*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(runsGlob) != 1 {
		t.Fatalf("expected 1 run dir, got %d", len(runsGlob))
	}
	runDir := runsGlob[0]
	finalSummaryBytes, err := os.ReadFile(filepath.Join(runDir, "final-summary.md"))
	if err != nil {
		t.Fatalf("expected final-summary.md after skipped delivery: %v", err)
	}
	finalSummary := string(finalSummaryBytes)
	if !strings.Contains(finalSummary, "- delivery: `skipped`") {
		t.Fatalf("expected skipped delivery in final summary, got:\n%s", finalSummary)
	}
	if _, err := os.Stat(filepath.Join(runDir, "pr-body.md")); !os.IsNotExist(err) {
		t.Fatalf("did not expect pr-body.md when delivery is skipped, err=%v", err)
	}
}

func TestRunStopsAfterSecondStopEvenWhenSecondRoundStillChangesCode(t *testing.T) {
	root := repoRoot(t)
	bin := buildBinary(t, root)
	fakeCodex, fakeGH := buildFakeBinaries(t, root)

	td := t.TempDir()
	remote := filepath.Join(td, "remote.git")
	seed := filepath.Join(td, "seed")
	userClone := filepath.Join(td, "user")
	workspace := filepath.Join(td, "workspace")

	runCmd(t, td, nil, "git", "init", "--bare", remote)
	runCmd(t, td, nil, "git", "clone", remote, seed)
	runCmd(t, td, nil, "git", "-C", seed, "config", "user.email", "test@example.com")
	runCmd(t, td, nil, "git", "-C", seed, "config", "user.name", "Test User")
	runCmd(t, td, nil, "git", "-C", seed, "checkout", "-b", "main")
	if err := os.WriteFile(filepath.Join(seed, "README.md"), []byte("seed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runCmd(t, td, nil, "git", "-C", seed, "add", "README.md")
	runCmd(t, td, nil, "git", "-C", seed, "commit", "-m", "seed")
	runCmd(t, td, nil, "git", "-C", seed, "push", "-u", "origin", "main")

	runCmd(t, td, nil, "git", "-C", seed, "checkout", "-b", "feature/test")
	if err := os.WriteFile(filepath.Join(seed, "feature.txt"), []byte("feature\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runCmd(t, td, nil, "git", "-C", seed, "add", "feature.txt")
	runCmd(t, td, nil, "git", "-C", seed, "commit", "-m", "feature")
	runCmd(t, td, nil, "git", "-C", seed, "push", "-u", "origin", "feature/test")

	cloneUserRepoWithGitHubOrigin(t, td, remote, userClone)
	runCmd(t, td, nil, "git", "-C", userClone, "checkout", "feature/test")
	before := runCmd(t, td, nil, "git", "-C", userClone, "rev-parse", "origin/feature/test")

	env := baseEnv(root, workspace, fakeCodex, fakeGH)
	env = append(env, "FAKE_CODEX_CHANGE_CONTENT_BY_ROUND=1")
	out := runCmd(t, root, env,
		bin,
		"review",
		userClone,
		"--source-branch", "feature/test",
		"--concurrency", "1",
		"--max-rounds", "2",
		"--mode", "yolo",
		"--no-tui",
	)
	if !strings.Contains(out, "deepreview completed:") {
		t.Fatalf("expected successful completion output, got: %s", out)
	}
	if !strings.Contains(out, "changes pushed:") {
		t.Fatalf("expected yolo delivery output, got: %s", out)
	}
	if !strings.Contains(out, "final review status: decision `stop`") {
		t.Fatalf("expected final review status context, got: %s", out)
	}

	runCmd(t, td, nil, "git", "-C", userClone, "fetch", "origin")
	after := runCmd(t, td, nil, "git", "-C", userClone, "rev-parse", "origin/feature/test")
	if before == after {
		t.Fatalf("expected second-stop flow to complete delivery and update remote source branch")
	}

	runsGlob, err := filepath.Glob(filepath.Join(workspace, "runs", "*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(runsGlob) != 1 {
		t.Fatalf("expected 1 run dir, got %d", len(runsGlob))
	}
	runDir := runsGlob[0]
	if _, err := os.Stat(filepath.Join(runDir, "round-02", "round-summary.md")); err != nil {
		t.Fatalf("expected second-round artifacts, missing round-02 summary: %v", err)
	}
	finalSummaryBytes, err := os.ReadFile(filepath.Join(runDir, "final-summary.md"))
	if err != nil {
		t.Fatalf("missing final-summary.md: %v", err)
	}
	if !strings.Contains(string(finalSummaryBytes), "- rounds: `2`") {
		t.Fatalf("expected final summary to record both execute rounds, got:\n%s", string(finalSummaryBytes))
	}
}

func TestEndToEndPRModeDiscardsStaleDeliveryCommitBeforeRetry(t *testing.T) {
	root := repoRoot(t)
	bin := buildBinary(t, root)
	fakeCodex, fakeGH := buildFakeBinaries(t, root)
	stallingCodex := buildStageCommitStallCodexWrapper(
		t,
		fakeCodex,
		"deepreview final delivery stage",
		"stale_delivery_attempt.txt",
		"stale delivery attempt",
	)

	td := t.TempDir()
	remote := filepath.Join(td, "remote.git")
	seed := filepath.Join(td, "seed")
	userClone := filepath.Join(td, "user")
	workspace := filepath.Join(td, "workspace")
	stallMarkerPath := filepath.Join(td, "stall-once.marker")

	runCmd(t, td, nil, "git", "init", "--bare", remote)
	runCmd(t, td, nil, "git", "clone", remote, seed)
	runCmd(t, td, nil, "git", "-C", seed, "config", "user.email", "test@example.com")
	runCmd(t, td, nil, "git", "-C", seed, "config", "user.name", "Test User")
	runCmd(t, td, nil, "git", "-C", seed, "checkout", "-b", "main")
	if err := os.WriteFile(filepath.Join(seed, "README.md"), []byte("seed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runCmd(t, td, nil, "git", "-C", seed, "add", "README.md")
	runCmd(t, td, nil, "git", "-C", seed, "commit", "-m", "seed")
	runCmd(t, td, nil, "git", "-C", seed, "push", "-u", "origin", "main")

	cloneUserRepoWithGitHubOrigin(t, td, remote, userClone)
	runCmd(t, td, nil, "git", "-C", userClone, "checkout", "main")

	env := baseEnv(root, workspace, stallingCodex, fakeGH)
	env = append(env,
		"DEEPREVIEW_REVIEW_INACTIVITY_SECONDS=2",
		"DEEPREVIEW_REVIEW_ACTIVITY_POLL_SECONDS=1",
		"DEEPREVIEW_REVIEW_MAX_RESTARTS=1",
		"STALL_MARKER_PATH="+stallMarkerPath,
		"STALL_SECONDS=15",
	)
	output, logs := runCmdCapture(t, root, env,
		bin,
		"review",
		userClone,
		"--source-branch", "main",
		"--concurrency", "1",
		"--max-rounds", "2",
		"--mode", "pr",
		"--no-tui",
	)
	if !strings.Contains(output, "PR created: https://example.com/olliecrow/test/pull/123") {
		t.Fatalf("expected PR delivery output, got: %s", output)
	}
	if !strings.Contains(logs, "delivery / merge-ready publish inactive for") {
		t.Fatalf("expected delivery retry evidence in logs, got:\n%s", logs)
	}

	runCmd(t, td, nil, "git", "-C", userClone, "fetch", "origin")
	refsOut := runCmd(t, td, nil, "git", "-C", userClone, "for-each-ref", "--format=%(refname:short)", "refs/remotes/origin/deepreview")
	var deliveryRef string
	for _, ref := range strings.Split(strings.TrimSpace(refsOut), "\n") {
		ref = strings.TrimSpace(ref)
		if strings.Contains(ref, "origin/deepreview/main/") {
			deliveryRef = ref
			break
		}
	}
	if deliveryRef == "" {
		t.Fatalf("expected deepreview delivery ref, got: %s", refsOut)
	}
	tree := runCmd(t, td, nil, "git", "-C", userClone, "ls-tree", "-r", "--name-only", deliveryRef)
	if strings.Contains(tree, "stale_delivery_attempt.txt") {
		t.Fatalf("stale delivery file leaked into delivery ref %s:\n%s", deliveryRef, tree)
	}
	logOut := runCmd(t, td, nil, "git", "-C", userClone, "log", "--format=%s", deliveryRef)
	if strings.Contains(logOut, "stale delivery attempt") {
		t.Fatalf("stale delivery commit leaked into delivery history for %s:\n%s", deliveryRef, logOut)
	}
}

func TestRunPRModePublishesIncompleteDraftPRWhenAnotherRoundIsStillRequiredAtMax(t *testing.T) {
	root := repoRoot(t)
	bin := buildBinary(t, root)
	fakeCodex, fakeGH := buildFakeBinaries(t, root)

	td := t.TempDir()
	remote := filepath.Join(td, "remote.git")
	seed := filepath.Join(td, "seed")
	userClone := filepath.Join(td, "user")
	workspace := filepath.Join(td, "workspace")
	ghArgsPath := filepath.Join(td, "gh-create-args.txt")

	runCmd(t, td, nil, "git", "init", "--bare", remote)
	runCmd(t, td, nil, "git", "clone", remote, seed)
	runCmd(t, td, nil, "git", "-C", seed, "config", "user.email", "test@example.com")
	runCmd(t, td, nil, "git", "-C", seed, "config", "user.name", "Test User")
	runCmd(t, td, nil, "git", "-C", seed, "checkout", "-b", "main")
	if err := os.WriteFile(filepath.Join(seed, "README.md"), []byte("seed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runCmd(t, td, nil, "git", "-C", seed, "add", "README.md")
	runCmd(t, td, nil, "git", "-C", seed, "commit", "-m", "seed")
	runCmd(t, td, nil, "git", "-C", seed, "push", "-u", "origin", "main")

	runCmd(t, td, nil, "git", "-C", seed, "checkout", "-b", "feature/test")
	if err := os.WriteFile(filepath.Join(seed, "feature.txt"), []byte("feature\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runCmd(t, td, nil, "git", "-C", seed, "add", "feature.txt")
	runCmd(t, td, nil, "git", "-C", seed, "commit", "-m", "feature")
	runCmd(t, td, nil, "git", "-C", seed, "push", "-u", "origin", "feature/test")

	cloneUserRepoWithGitHubOrigin(t, td, remote, userClone)
	runCmd(t, td, nil, "git", "-C", userClone, "checkout", "feature/test")

	env := baseEnv(root, workspace, fakeCodex, fakeGH)
	env = append(env,
		"FAKE_CODEX_DECISION=continue",
		"FAKE_GH_CAPTURE_ARGS_PATH="+ghArgsPath,
	)
	output := runCmd(t, root, env,
		bin,
		"review",
		userClone,
		"--source-branch", "feature/test",
		"--concurrency", "1",
		"--max-rounds", "2",
		"--mode", "pr",
		"--no-tui",
	)
	if !strings.Contains(output, "Draft PR created: https://example.com/olliecrow/test/pull/123") {
		t.Fatalf("expected incomplete draft PR summary output, got: %s", output)
	}
	if !strings.Contains(output, "delivery status: incomplete") {
		t.Fatalf("expected incomplete delivery status output, got: %s", output)
	}

	argsBytes, err := os.ReadFile(ghArgsPath)
	if err != nil {
		t.Fatalf("missing gh args capture: %v", err)
	}
	argsText := string(argsBytes)
	if !strings.Contains(argsText, "--draft") {
		t.Fatalf("expected draft flag in gh pr create args, got:\n%s", argsText)
	}

	runsGlob, err := filepath.Glob(filepath.Join(workspace, "runs", "*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(runsGlob) != 1 {
		t.Fatalf("expected 1 run dir, got %d", len(runsGlob))
	}
	runDir := runsGlob[0]
	titleBytes, err := os.ReadFile(filepath.Join(runDir, "pr-title.txt"))
	if err != nil {
		t.Fatalf("missing pr-title.txt: %v", err)
	}
	if !strings.HasPrefix(strings.TrimSpace(string(titleBytes)), "[INCOMPLETE] deepreview:") {
		t.Fatalf("expected incomplete title prefix, got: %s", string(titleBytes))
	}
	bodyBytes, err := os.ReadFile(filepath.Join(runDir, "pr-body.md"))
	if err != nil {
		t.Fatalf("missing pr-body.md: %v", err)
	}
	body := string(bodyBytes)
	if !strings.Contains(body, "do not merge this PR as-is") {
		t.Fatalf("expected incomplete warning in pr body, got:\n%s", body)
	}
	if !strings.Contains(body, "latest decision: `continue`") {
		t.Fatalf("expected latest continue status in pr body, got:\n%s", body)
	}

	runCmd(t, td, nil, "git", "-C", userClone, "fetch", "origin")
	refsOut := runCmd(t, td, nil, "git", "-C", userClone, "for-each-ref", "--format=%(refname:short)", "refs/remotes/origin/deepreview")
	var deliveryRef string
	for _, ref := range strings.Split(strings.TrimSpace(refsOut), "\n") {
		ref = strings.TrimSpace(ref)
		if strings.Contains(ref, "origin/deepreview/feature/test/") {
			deliveryRef = ref
			break
		}
	}
	if deliveryRef == "" {
		t.Fatalf("expected deepreview delivery ref, got: %s", refsOut)
	}
	finalSummaryBytes, err := os.ReadFile(filepath.Join(runDir, "final-summary.md"))
	if err != nil {
		t.Fatalf("missing final-summary.md: %v", err)
	}
	finalSummary := string(finalSummaryBytes)
	if !strings.Contains(finalSummary, "- delivery: `incomplete-draft`") {
		t.Fatalf("expected incomplete draft marker in final summary, got:\n%s", finalSummary)
	}
	if !strings.Contains(finalSummary, "## pull request") {
		t.Fatalf("expected pull request section in final summary, got:\n%s", finalSummary)
	}
}

func TestRunPRModeIncompleteDraftIgnoresRoundStatusWithoutRoundRecord(t *testing.T) {
	root := repoRoot(t)
	bin := buildBinary(t, root)
	fakeCodex, fakeGH := buildFakeBinaries(t, root)

	td := t.TempDir()
	remote := filepath.Join(td, "remote.git")
	seed := filepath.Join(td, "seed")
	userClone := filepath.Join(td, "user")
	workspace := filepath.Join(td, "workspace")

	runCmd(t, td, nil, "git", "init", "--bare", remote)
	runCmd(t, td, nil, "git", "clone", remote, seed)
	runCmd(t, td, nil, "git", "-C", seed, "config", "user.email", "test@example.com")
	runCmd(t, td, nil, "git", "-C", seed, "config", "user.name", "Test User")
	runCmd(t, td, nil, "git", "-C", seed, "checkout", "-b", "main")
	if err := os.WriteFile(filepath.Join(seed, "README.md"), []byte("seed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(seed, ".tmp"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(seed, ".tmp", "tracked.txt"), []byte("repo-owned\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runCmd(t, td, nil, "git", "-C", seed, "add", "README.md", ".tmp/tracked.txt")
	runCmd(t, td, nil, "git", "-C", seed, "commit", "-m", "seed")
	runCmd(t, td, nil, "git", "-C", seed, "push", "-u", "origin", "main")

	cloneUserRepoWithGitHubOrigin(t, td, remote, userClone)
	runCmd(t, td, nil, "git", "-C", userClone, "checkout", "main")

	env := baseEnv(root, workspace, fakeCodex, fakeGH)
	env = append(env,
		"FAKE_CODEX_ADD_REPO_TMP_FILE=1",
		"FAKE_CODEX_DECISION=continue",
		"FAKE_CODEX_WRITE_INVALID_ROUND_STATUS=1",
	)
	output := runCmd(t, root, env,
		bin,
		"review",
		userClone,
		"--source-branch", "main",
		"--concurrency", "1",
		"--max-rounds", "1",
		"--mode", "pr",
		"--no-tui",
	)
	if !strings.Contains(output, "Draft PR created: https://example.com/olliecrow/test/pull/123") {
		t.Fatalf("expected incomplete draft PR summary output, got: %s", output)
	}

	runsGlob, err := filepath.Glob(filepath.Join(workspace, "runs", "*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(runsGlob) != 1 {
		t.Fatalf("expected 1 run dir, got %d", len(runsGlob))
	}
	runDir := runsGlob[0]
	if _, err := os.Stat(filepath.Join(runDir, "round-01", "round-summary.md")); err != nil {
		t.Fatalf("missing round-summary.md: %v", err)
	}
	if _, err := os.Stat(filepath.Join(runDir, "round-01", "round-status.json")); err != nil {
		t.Fatalf("missing round-status.json: %v", err)
	}
	if _, err := os.Stat(filepath.Join(runDir, "round-01", "round.json")); !os.IsNotExist(err) {
		t.Fatalf("did not expect round.json for failed round recovery path, got err=%v", err)
	}

	bodyBytes, err := os.ReadFile(filepath.Join(runDir, "pr-body.md"))
	if err != nil {
		t.Fatalf("missing pr-body.md: %v", err)
	}
	body := string(bodyBytes)
	if !strings.Contains(body, "- rounds completed: `0`") {
		t.Fatalf("expected incomplete body to report zero authoritative rounds, got:\n%s", body)
	}
	if strings.Contains(body, "latest decision: `continue`") {
		t.Fatalf("did not expect latest decision from missing round record, got:\n%s", body)
	}

	finalSummaryBytes, err := os.ReadFile(filepath.Join(runDir, "final-summary.md"))
	if err != nil {
		t.Fatalf("missing final-summary.md: %v", err)
	}
	finalSummary := string(finalSummaryBytes)
	if !strings.Contains(finalSummary, "- rounds: `0`") {
		t.Fatalf("expected final summary to report zero authoritative rounds, got:\n%s", finalSummary)
	}
	if strings.Contains(finalSummary, "round-01/round-status.json") {
		t.Fatalf("did not expect round decision entry from missing round record, got:\n%s", finalSummary)
	}
}
