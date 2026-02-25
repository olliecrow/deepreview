package deepreview

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func runGitTest(t *testing.T, cwd string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = cwd
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git command failed: %v\nargs=%v\noutput=%s", err, args, string(out))
	}
	return strings.TrimSpace(string(out))
}

func TestResolveRepoIdentityLocalPathRequiresOriginRemote(t *testing.T) {
	td := t.TempDir()
	repo := filepath.Join(td, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, td, "init", repo)

	_, err := resolveRepoIdentity(ReviewConfig{GitBin: "git"}, repo)
	if err == nil {
		t.Fatalf("expected error when local repo has no origin remote")
	}
	if !strings.Contains(err.Error(), "remote.origin.url") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestResolveRepoIdentityLocalPathUsesOriginRemoteAsCloneSource(t *testing.T) {
	td := t.TempDir()
	repo := filepath.Join(td, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, td, "init", repo)
	runGitTest(t, td, "-C", repo, "remote", "add", "origin", "https://github.com/example-org/example-repo.git")

	identity, err := resolveRepoIdentity(ReviewConfig{GitBin: "git"}, repo)
	if err != nil {
		t.Fatalf("resolveRepoIdentity failed: %v", err)
	}
	if identity.CloneSource != "https://github.com/example-org/example-repo.git" {
		t.Fatalf("expected clone source to be origin remote, got: %s", identity.CloneSource)
	}
	if identity.Owner != "example-org" || identity.Name != "example-repo" {
		t.Fatalf("unexpected slug: %s/%s", identity.Owner, identity.Name)
	}
}

func TestExecutePromptLabel(t *testing.T) {
	cases := []struct {
		templateName string
		want         string
	}{
		{templateName: "01-consolidate-reviews.md", want: "consolidate reviews"},
		{templateName: "02-plan.md", want: "plan"},
		{templateName: "03-execute-verify.md", want: "execute and verify"},
		{templateName: "04-cleanup-summary-commit.md", want: "cleanup, summary, commit"},
		{templateName: "01-review-triage.md", want: "consolidate reviews"},
		{templateName: "02-change-plan.md", want: "plan"},
		{templateName: "unknown.md", want: "unknown.md"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.templateName, func(t *testing.T) {
			got := executePromptLabel(tc.templateName)
			if got != tc.want {
				t.Fatalf("executePromptLabel(%q) = %q, want %q", tc.templateName, got, tc.want)
			}
		})
	}
}

func TestAcquireRepoRunLockPreventsConcurrentSameRepo(t *testing.T) {
	td := t.TempDir()
	shared := Orchestrator{
		workspaceRoot: td,
		repoIdentity:  RepoIdentity{Owner: "example", Name: "repo"},
		config:        ReviewConfig{RunID: "run-1"},
	}
	if err := shared.acquireRepoRunLock(); err != nil {
		t.Fatalf("acquire lock failed: %v", err)
	}
	defer shared.releaseRepoRunLock()

	second := Orchestrator{
		workspaceRoot: td,
		repoIdentity:  RepoIdentity{Owner: "example", Name: "repo"},
		config:        ReviewConfig{RunID: "run-2"},
	}
	err := second.acquireRepoRunLock()
	if err == nil {
		second.releaseRepoRunLock()
		t.Fatalf("expected lock acquisition to fail for concurrent same-repo run")
	}
	if !strings.Contains(err.Error(), "another deepreview run is active") {
		t.Fatalf("unexpected lock error: %v", err)
	}
}

func TestAcquireRepoRunLockReplacesStaleLock(t *testing.T) {
	td := t.TempDir()
	o := Orchestrator{
		workspaceRoot: td,
		repoIdentity:  RepoIdentity{Owner: "example", Name: "repo"},
		config:        ReviewConfig{RunID: "new-run"},
	}
	lockPath := o.repoLockFilePath()
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
		t.Fatal(err)
	}
	stale := repoRunLockRecord{
		RunID:     "old-run",
		PID:       999999,
		Repo:      "example/repo",
		CreatedAt: time.Now().Add(-24 * time.Hour).UTC().Format(time.RFC3339),
	}
	payload, err := json.Marshal(stale)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lockPath, payload, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := o.acquireRepoRunLock(); err != nil {
		t.Fatalf("expected stale lock replacement, got error: %v", err)
	}
	defer o.releaseRepoRunLock()

	currentBytes, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("expected fresh lock file, read failed: %v", err)
	}
	var current repoRunLockRecord
	if err := json.Unmarshal(currentBytes, &current); err != nil {
		t.Fatalf("invalid lock json: %v", err)
	}
	if current.RunID != "new-run" {
		t.Fatalf("expected lock run id new-run, got %s", current.RunID)
	}
}

func TestAcquireRepoRunLockAllowsDifferentRepos(t *testing.T) {
	td := t.TempDir()
	first := Orchestrator{
		workspaceRoot: td,
		repoIdentity:  RepoIdentity{Owner: "example", Name: "repo-a"},
		config:        ReviewConfig{RunID: "run-a"},
	}
	second := Orchestrator{
		workspaceRoot: td,
		repoIdentity:  RepoIdentity{Owner: "example", Name: "repo-b"},
		config:        ReviewConfig{RunID: "run-b"},
	}
	if err := first.acquireRepoRunLock(); err != nil {
		t.Fatalf("first lock failed: %v", err)
	}
	defer first.releaseRepoRunLock()
	if err := second.acquireRepoRunLock(); err != nil {
		t.Fatalf("second lock for different repo should succeed: %v", err)
	}
	defer second.releaseRepoRunLock()
}
