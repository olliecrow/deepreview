package deepreview

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInferRepoAndBranchFromCurrentDirectory(t *testing.T) {
	repo := createSyncedGitHubLikeRepo(t, "feature/test")
	withWorkingDir(t, repo, func() {
		resolvedRepo, resolvedBranch, err := inferRepoAndBranch("git", "", "")
		if err != nil {
			t.Fatalf("inferRepoAndBranch failed: %v", err)
		}
		repoAbs := canonicalPath(t, repo)
		if resolvedRepo != repoAbs {
			t.Fatalf("expected inferred repo %s, got %s", repoAbs, resolvedRepo)
		}
		if resolvedBranch != "feature/test" {
			t.Fatalf("expected inferred branch feature/test, got %s", resolvedBranch)
		}
	})
}

func TestInferRepoAndBranchFailsOutsideGitHubRepo(t *testing.T) {
	td := t.TempDir()
	withWorkingDir(t, td, func() {
		_, _, err := inferRepoAndBranch("git", "", "")
		if err == nil {
			t.Fatalf("expected inference error outside github repo")
		}
		if !strings.Contains(err.Error(), "current directory is not a valid GitHub repo") {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestInferRepoAndBranchAllowsUntrackedFiles(t *testing.T) {
	repo := createSyncedGitHubLikeRepo(t, "feature/test")
	withWorkingDir(t, repo, func() {
		if err := os.WriteFile(filepath.Join(repo, "UNTRACKED.tmp"), []byte("ok\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		_, _, err := inferRepoAndBranch("git", "", "")
		if err != nil {
			t.Fatalf("expected untracked files to be allowed, got: %v", err)
		}
	})
}

func TestInferRepoAndBranchRejectsTrackedUncommittedChanges(t *testing.T) {
	repo := createSyncedGitHubLikeRepo(t, "feature/test")
	withWorkingDir(t, repo, func() {
		if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("modified\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		_, _, err := inferRepoAndBranch("git", "", "")
		if err == nil {
			t.Fatalf("expected tracked-change error")
		}
		if !strings.Contains(err.Error(), "local tracked changes are present") {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestInferRepoAndBranchRejectsAheadOfRemote(t *testing.T) {
	repo := createSyncedGitHubLikeRepo(t, "feature/test")
	withWorkingDir(t, repo, func() {
		if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("next\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		runGitCommand(t, repo, "add", "README.md")
		runGitCommand(t, repo, "commit", "-m", "ahead")
		_, _, err := inferRepoAndBranch("git", "", "")
		if err == nil {
			t.Fatalf("expected ahead-of-remote error")
		}
		if !strings.Contains(err.Error(), "not synchronized") {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestInferRepoAndBranchFromProvidedLocalRepoPath(t *testing.T) {
	repo := createSyncedGitHubLikeRepo(t, "feature/test")
	td := t.TempDir()
	withWorkingDir(t, td, func() {
		resolvedRepo, resolvedBranch, err := inferRepoAndBranch("git", repo, "")
		if err != nil {
			t.Fatalf("inferRepoAndBranch failed: %v", err)
		}
		repoAbs, err := filepath.Abs(repo)
		if err != nil {
			t.Fatal(err)
		}
		if resolvedRepo != repoAbs {
			t.Fatalf("expected repo %s, got %s", repoAbs, resolvedRepo)
		}
		if resolvedBranch != "feature/test" {
			t.Fatalf("expected branch feature/test, got %s", resolvedBranch)
		}
	})
}
