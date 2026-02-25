package deepreview

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
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
	if !strings.Contains(output, "deepreview completed:") {
		t.Fatalf("expected completion summary output, got: %s", output)
	}
	if !strings.Contains(output, "PR created: https://example.com/olliecrow/test/pull/123") {
		t.Fatalf("expected pr created summary output, got: %s", output)
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
	prBodyBytes, err := os.ReadFile(filepath.Join(runDir, "pr-body.md"))
	if err != nil {
		t.Fatalf("missing pr-body.md: %v", err)
	}
	prBody := string(prBodyBytes)
	if strings.Contains(prBody, "/Users/") {
		t.Fatalf("pr body must not contain local absolute user paths")
	}
	atAGlanceIdx := strings.Index(prBody, "## at a glance")
	if atAGlanceIdx == -1 {
		t.Fatalf("pr body missing top-level at a glance summary section")
	}
	deepreviewReportIdx := strings.Index(prBody, "## deepreview report")
	if deepreviewReportIdx == -1 {
		t.Fatalf("pr body missing deepreview report section")
	}
	if atAGlanceIdx > deepreviewReportIdx {
		t.Fatalf("at a glance section must appear before deepreview report details")
	}
	if !strings.Contains(prBody, "- main change areas:") {
		t.Fatalf("pr body missing main change areas bullet")
	}
	if !strings.Contains(prBody, "- key changed files:") {
		t.Fatalf("pr body missing key changed files bullet")
	}
	if !strings.Contains(prBody, "### independent reviews") {
		t.Fatalf("pr body missing independent reviews section")
	}
	if !strings.Contains(prBody, "### execute artifacts") {
		t.Fatalf("pr body missing execute artifacts section")
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
	if !strings.Contains(output, "delivery skipped: no deliverable repository changes were produced") {
		t.Fatalf("expected skipped-delivery summary output, got: %s", output)
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

func TestRunFailsWhenMaxRoundsPreventsRequiredPostChangeReview(t *testing.T) {
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
	out := runCmdExpectFailure(t, root, env,
		bin,
		"review",
		userClone,
		"--source-branch", "main",
		"--concurrency", "1",
		"--max-rounds", "1",
		"--mode", "yolo",
		"--no-tui",
	)
	if !strings.Contains(out, "requires at least one additional review round after code changes") {
		t.Fatalf("expected max-rounds enforcement error, got: %s", out)
	}

	runCmd(t, td, nil, "git", "-C", userClone, "fetch", "origin")
	after := runCmd(t, td, nil, "git", "-C", userClone, "rev-parse", "origin/main")
	if before != after {
		t.Fatalf("remote source branch should remain unchanged when run fails before delivery")
	}
}
