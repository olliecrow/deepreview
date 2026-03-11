package deepreview

import (
	"bytes"
	"errors"
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
	return fakeCodex, fakeGH
}

func baseEnv(root, workspace, codexBin, ghBin string) []string {
	env := append([]string{}, os.Environ()...)
	env = append(env,
		"DEEPREVIEW_WORKSPACE_ROOT="+workspace,
		"DEEPREVIEW_CODEX_BIN="+codexBin,
		"DEEPREVIEW_GH_BIN="+ghBin,
		"DEEPREVIEW_PROMPTS_ROOT="+filepath.Join(root, "prompts"),
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

	runCmd(t, td, nil, "git", "clone", remote, userClone)
	runCmd(t, td, nil, "git", "-C", userClone, "checkout", "feature/test")
	before := runCmd(t, td, nil, "git", "-C", userClone, "rev-parse", "origin/feature/test")

	env := baseEnv(root, workspace, fakeCodex, fakeGH)
	env = append(env,
		"FAKE_CODEX_WRITE_OPERATIONAL_TMP=1",
		"DEEPREVIEW_REVIEW_INACTIVITY_SECONDS=2",
		"DEEPREVIEW_REVIEW_ACTIVITY_POLL_SECONDS=1",
		"DEEPREVIEW_REVIEW_MAX_RESTARTS=1",
		"FAKE_CODEX_REQUIRE_SKIP_GIT_REPO_CHECK=1",
		"FAKE_CODEX_STALL_ONCE_CONTAINS=post-delivery PR description enhancement stage",
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
	if !strings.Contains(logs, "delivery codex summary inactive for") {
		t.Fatalf("expected delivery inactivity restart evidence in logs, got:\n%s", logs)
	}
	if !strings.Contains(output, "PR created: https://example.com/olliecrow/test/pull/123") {
		t.Fatalf("expected pr created summary output, got: %s", output)
	}
	if !strings.Contains(output, "delivery commits: https://github.com/local/user/commits/deepreview/feature/test/") {
		t.Fatalf("expected delivery commits summary output, got: %s", output)
	}
	if !strings.Contains(output, "repository reviewed: `local/user`") {
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
	if _, err := os.Stat(filepath.Join(runDir, "pr-title.base.txt")); err != nil {
		t.Fatalf("pr-title.base.txt missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(runDir, "pr-title.txt")); err != nil {
		t.Fatalf("pr-title.txt missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(runDir, "pr-top-title.txt")); err != nil {
		t.Fatalf("pr-top-title.txt missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(runDir, "pr-body.base.md")); err != nil {
		t.Fatalf("pr-body.base.md missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(runDir, "pr-top-summary.md")); err != nil {
		t.Fatalf("pr-top-summary.md missing: %v", err)
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

func TestEndToEndPRModeReportsRecoveryHintsWhenPRURLMissing(t *testing.T) {
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

	env := baseEnv(root, workspace, fakeCodex, fakeGH)
	env = append(env, "FAKE_GH_PR_CREATE_SILENT=1")
	output := runCmd(t, root, env,
		bin,
		"review",
		userClone,
		"--source-branch", "feature/test",
		"--concurrency", "1",
		"--max-rounds", "1",
		"--mode", "pr",
		"--no-tui",
	)
	if !strings.Contains(output, "delivery completed in PR mode, but no PR URL was returned.") {
		t.Fatalf("expected missing-pr-url summary output, got: %s", output)
	}
	if !strings.Contains(output, "inspect delivery commits and recover the PR manually if needed.") {
		t.Fatalf("expected manual recovery guidance, got: %s", output)
	}
	if !strings.Contains(output, "delivery commits: https://github.com/local/user/commits/deepreview/feature/test/") {
		t.Fatalf("expected delivery commits summary output, got: %s", output)
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

	runCmd(t, td, nil, "git", "clone", remote, userClone)
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

	lockFiles, err := filepath.Glob(filepath.Join(workspace, "locks", "*", "*.lock"))
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
	err = filepath.WalkDir(runsGlob[0], func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() && filepath.Base(path) == "worktree" {
			return NewDeepReviewError("unexpected leftover worktree: %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("expected worktree cleanup after interrupt: %v", err)
	}

	runCmd(t, td, nil, "git", "-C", userClone, "fetch", "origin")
	after := runCmd(t, td, nil, "git", "-C", userClone, "rev-parse", "origin/feature/test")
	if before != after {
		t.Fatalf("source branch should remain unchanged after interrupt")
	}
}

func TestEndToEndPRModePrivacyFixAttemptsProceedAfterMax(t *testing.T) {
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

	env := baseEnv(root, workspace, fakeCodex, fakeGH)
	env = append(env,
		"FAKE_CODEX_CHANGE_COMMIT_MESSAGE=contact alice@corp.com",
		"FAKE_CODEX_PRIVACY_DECISION=continue",
		"FAKE_CODEX_REQUIRE_PRIVACY_STATUS_WITHIN_CWD=1",
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
	if !strings.Contains(output, "PR created: https://example.com/olliecrow/test/pull/123") {
		t.Fatalf("expected PR creation despite unresolved privacy scan findings, got: %s", output)
	}
	if !strings.Contains(logs, "privacy fix gate reached max attempts (3); proceeding with delivery by policy") {
		t.Fatalf("expected max-attempt privacy policy log, got:\n%s", logs)
	}

	runsGlob, err := filepath.Glob(filepath.Join(workspace, "runs", "*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(runsGlob) != 1 {
		t.Fatalf("expected 1 run dir, got %d", len(runsGlob))
	}
	runDir := runsGlob[0]
	for _, attempt := range []string{"attempt-01", "attempt-02", "attempt-03"} {
		statusPath := filepath.Join(runDir, "delivery", "privacy-fix", attempt, "privacy-status.json")
		if _, err := os.Stat(statusPath); err != nil {
			t.Fatalf("missing privacy status artifact %s: %v", statusPath, err)
		}
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

	runCmd(t, td, nil, "git", "clone", remote, userClone)
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
		t.Fatalf("default branch doc must not be mutated by candidate privacy remediation: %s", mainDoc)
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

	runCmd(t, td, nil, "git", "clone", remote, userClone)
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
		"--max-rounds", "1",
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

func TestEndToEndPRModeSetsSandboxSafeGoEnvForCodexRuns(t *testing.T) {
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
	runCmd(t, td, nil, "git", "-C", seed, "add", "README.md", "go.mod")
	runCmd(t, td, nil, "git", "-C", seed, "commit", "-m", "seed")
	runCmd(t, td, nil, "git", "-C", seed, "push", "-u", "origin", "main")

	runCmd(t, td, nil, "git", "clone", remote, userClone)
	runCmd(t, td, nil, "git", "-C", userClone, "checkout", "main")

	env := baseEnv(root, workspace, fakeCodex, fakeGH)
	env = append(env,
		"FAKE_CODEX_SKIP_CODE_CHANGE=1",
		"FAKE_CODEX_REQUIRE_SANDBOX_GO_ENV_WITHIN_CWD=1",
	)
	output, _ := runCmdCapture(t, root, env,
		bin,
		"review",
		userClone,
		"--source-branch", "main",
		"--concurrency", "1",
		"--max-rounds", "1",
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

	runCmd(t, td, nil, "git", "clone", remote, userClone)
	runCmd(t, td, nil, "git", "-C", userClone, "checkout", "main")

	env := baseEnv(root, workspace, fakeCodex, fakeGH)
	env = append(env,
		"FAKE_CODEX_SKIP_CODE_CHANGE=1",
		"FAKE_CODEX_WRITE_OPERATIONAL_TMP=1",
	)
	output, _ := runCmdCapture(t, root, env,
		bin,
		"review",
		userClone,
		"--source-branch", "main",
		"--concurrency", "1",
		"--max-rounds", "1",
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
	if _, err := os.Stat(filepath.Join(runDir, "round-02")); !os.IsNotExist(err) {
		t.Fatalf("expected no second round when only operational artifacts were produced")
	}
	finalSummaryBytes, err := os.ReadFile(filepath.Join(runDir, "final-summary.md"))
	if err != nil {
		t.Fatalf("missing final-summary.md: %v", err)
	}
	if !strings.Contains(string(finalSummaryBytes), "- delivery: `skipped`") {
		t.Fatalf("expected skipped delivery in final summary")
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

	runCmd(t, td, nil, "git", "clone", remote, userClone)
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
		"--max-rounds", "1",
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

	runCmd(t, td, nil, "git", "clone", remote, userClone)
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

func TestEndToEndPRModeSkipsDeliveryWhenNoChangesEvenIfStatusSaysContinue(t *testing.T) {
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

	runCmd(t, td, nil, "git", "clone", remote, userClone)
	runCmd(t, td, nil, "git", "-C", userClone, "checkout", "main")
	before := runCmd(t, td, nil, "git", "-C", userClone, "rev-parse", "origin/main")

	env := baseEnv(root, workspace, fakeCodex, fakeGH)
	env = append(env,
		"FAKE_CODEX_SKIP_CODE_CHANGE=1",
		"FAKE_CODEX_DECISION=continue",
		"DEEPREVIEW_REVIEW_INACTIVITY_SECONDS=2",
		"DEEPREVIEW_REVIEW_ACTIVITY_POLL_SECONDS=1",
		"DEEPREVIEW_REVIEW_MAX_RESTARTS=1",
		"FAKE_CODEX_STALL_ONCE_CONTAINS=prompt 2 of 4",
		"FAKE_CODEX_STALL_ONCE_MS_MATCH=15000",
	)
	output, logs := runCmdCapture(t, root, env,
		bin,
		"review",
		userClone,
		"--source-branch", "main",
		"--concurrency", "1",
		"--max-rounds", "1",
		"--mode", "pr",
		"--no-tui",
	)
	if !strings.Contains(output, "delivery skipped: no deliverable repository changes were produced") {
		t.Fatalf("expected skipped-delivery summary output, got: %s", output)
	}
	if !strings.Contains(logs, "execute / plan inactive for") {
		t.Fatalf("expected execute inactivity restart evidence in logs, got:\n%s", logs)
	}

	runCmd(t, td, nil, "git", "-C", userClone, "fetch", "origin")
	after := runCmd(t, td, nil, "git", "-C", userClone, "rev-parse", "origin/main")
	if before != after {
		t.Fatalf("source branch should remain unchanged when delivery is skipped")
	}

	refsOut := runCmd(t, td, nil, "git", "-C", userClone, "for-each-ref", "--format=%(refname:short)", "refs/remotes/origin/deepreview")
	if strings.TrimSpace(refsOut) != "" {
		t.Fatalf("expected no delivery branch refs when delivery is skipped, got: %s", refsOut)
	}

	runsGlob, err := filepath.Glob(filepath.Join(workspace, "runs", "*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(runsGlob) != 1 {
		t.Fatalf("expected 1 run dir, got %d", len(runsGlob))
	}
	runDir := runsGlob[0]
	if _, err := os.Stat(filepath.Join(runDir, "round-02")); !os.IsNotExist(err) {
		t.Fatalf("expected no second round when execute produced no changes")
	}
	finalSummaryBytes, err := os.ReadFile(filepath.Join(runDir, "final-summary.md"))
	if err != nil {
		t.Fatalf("missing final-summary.md: %v", err)
	}
	finalSummary := string(finalSummaryBytes)
	if !strings.Contains(finalSummary, "- delivery: `skipped`") {
		t.Fatalf("expected skipped delivery in final summary")
	}
	if _, err := os.Stat(filepath.Join(runDir, "pr-body.md")); !os.IsNotExist(err) {
		t.Fatalf("pr-body.md should not be created on skipped delivery")
	}
}

func TestRunAutoSchedulesFinalAuditRoundWhenLastAllowedExecuteRoundChangesCode(t *testing.T) {
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
	out := runCmd(t, root, env,
		bin,
		"review",
		userClone,
		"--source-branch", "feature/test",
		"--concurrency", "1",
		"--max-rounds", "1",
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
		t.Fatalf("expected auto-audit run to complete delivery and update remote source branch")
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
		t.Fatalf("expected automatic audit round artifacts, missing round-02 summary: %v", err)
	}
	finalSummaryBytes, err := os.ReadFile(filepath.Join(runDir, "final-summary.md"))
	if err != nil {
		t.Fatalf("missing final-summary.md: %v", err)
	}
	if !strings.Contains(string(finalSummaryBytes), "- rounds: `2`") {
		t.Fatalf("expected final summary to record both execute and automatic audit rounds, got:\n%s", string(finalSummaryBytes))
	}
}

func TestRunPRModePublishesIncompleteDraftPRAfterAuditContinue(t *testing.T) {
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

	runCmd(t, td, nil, "git", "clone", remote, userClone)
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
		"--max-rounds", "1",
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

func TestRunAuditRoundFailsWhenCandidateHeadMovesWithoutTreeChanges(t *testing.T) {
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
	env = append(env, "FAKE_CODEX_AUDIT_ALLOW_EMPTY_COMMIT=1")
	out := runCmdExpectFailure(t, root, env,
		bin,
		"review",
		userClone,
		"--source-branch", "feature/test",
		"--concurrency", "1",
		"--max-rounds", "1",
		"--mode", "yolo",
		"--no-tui",
	)
	if !strings.Contains(out, "automatic final audit round 2 moved candidate branch HEAD; audit rounds must remain read-only") {
		t.Fatalf("expected audit round head-movement error, got: %s", out)
	}

	runCmd(t, td, nil, "git", "-C", userClone, "fetch", "origin")
	after := runCmd(t, td, nil, "git", "-C", userClone, "rev-parse", "origin/feature/test")
	if before != after {
		t.Fatalf("expected failed audit round to leave remote source branch unchanged")
	}

	managedRepo := filepath.Join(workspace, "repos", "local", "user")
	logs := runCmd(t, td, nil, "git", "-C", managedRepo, "log", "--oneline", "--all", "-n", "10")
	if !strings.Contains(logs, "audit empty commit") {
		t.Fatalf("expected audit-only empty commit repro to move candidate history, got:\n%s", logs)
	}
}

func TestRunAuditRoundFailsBeforeAutoCommitOnDirtyWorktree(t *testing.T) {
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
	env = append(env, "FAKE_CODEX_AUDIT_WRITE_FILE_CHANGE=1")
	out := runCmdExpectFailure(t, root, env,
		bin,
		"review",
		userClone,
		"--source-branch", "feature/test",
		"--concurrency", "1",
		"--max-rounds", "1",
		"--mode", "yolo",
		"--no-tui",
	)
	if !strings.Contains(out, "automatic final audit round 2 left uncommitted changes in execute worktree; audit rounds must remain read-only") {
		t.Fatalf("expected audit round dirty-worktree error, got: %s", out)
	}

	runCmd(t, td, nil, "git", "-C", userClone, "fetch", "origin")
	after := runCmd(t, td, nil, "git", "-C", userClone, "rev-parse", "origin/feature/test")
	if before != after {
		t.Fatalf("expected failed audit round to leave remote source branch unchanged")
	}

	managedRepo := filepath.Join(workspace, "repos", "local", "user")
	logs := runCmd(t, td, nil, "git", "-C", managedRepo, "log", "--oneline", "--all", "-n", "10")
	if strings.Contains(logs, "deepreview: round 02 execute updates") {
		t.Fatalf("expected dirty audit round to fail before deepreview auto-commit, got:\n%s", logs)
	}
}
