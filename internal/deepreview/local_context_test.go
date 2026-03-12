package deepreview

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func setSourceRootDetectorForTest(t *testing.T, detector func() (string, bool)) {
	t.Helper()
	previous := detectDeepreviewSourceRoot
	detectDeepreviewSourceRoot = detector
	t.Cleanup(func() {
		detectDeepreviewSourceRoot = previous
	})
}

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

func TestInferRepoAndBranchRejectsWhenRemoteAdvancedWithoutLocalFetch(t *testing.T) {
	repo := createSyncedGitHubLikeRepo(t, "feature/test")
	originURL := strings.TrimSpace(runGitCommand(t, repo, "config", "--get", "remote.origin.url"))
	if originURL == "" {
		t.Fatal("expected origin remote URL")
	}

	otherClone := filepath.Join(t.TempDir(), "other")
	runGitCommand(t, filepath.Dir(otherClone), "clone", originURL, otherClone)
	runGitCommand(t, otherClone, "config", "user.email", "test@example.com")
	runGitCommand(t, otherClone, "config", "user.name", "Test User")
	runGitCommand(t, otherClone, "checkout", "feature/test")
	if err := os.WriteFile(filepath.Join(otherClone, "README.md"), []byte("remote advanced\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitCommand(t, otherClone, "add", "README.md")
	runGitCommand(t, otherClone, "commit", "-m", "remote advance")
	runGitCommand(t, otherClone, "push", "origin", "feature/test")

	withWorkingDir(t, repo, func() {
		_, _, err := inferRepoAndBranch("git", "", "")
		if err == nil {
			t.Fatalf("expected stale local tracking ref to be detected after refresh")
		}
		if !strings.Contains(err.Error(), "not synchronized") {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestInferRepoAndBranchExplicitSourceBranchRejectsTrackedUncommittedChanges(t *testing.T) {
	repo := createSyncedGitHubLikeRepo(t, "feature/test")
	withWorkingDir(t, repo, func() {
		if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("modified\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		_, _, err := inferRepoAndBranch("git", "", "feature/test")
		if err == nil {
			t.Fatalf("expected tracked-change error for explicit source branch")
		}
		if !strings.Contains(err.Error(), "local tracked changes are present") {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestInferRepoAndBranchExplicitSourceBranchRejectsAheadOfRemote(t *testing.T) {
	repo := createSyncedGitHubLikeRepo(t, "feature/test")
	withWorkingDir(t, repo, func() {
		if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("next\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		runGitCommand(t, repo, "add", "README.md")
		runGitCommand(t, repo, "commit", "-m", "ahead")
		_, _, err := inferRepoAndBranch("git", "", "feature/test")
		if err == nil {
			t.Fatalf("expected ahead-of-remote error for explicit source branch")
		}
		if !strings.Contains(err.Error(), "not synchronized") {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestInferRepoAndBranchExplicitDifferentBranchSkipsCurrentBranchReadinessCheck(t *testing.T) {
	repo := createSyncedGitHubLikeRepo(t, "feature/test")
	withWorkingDir(t, repo, func() {
		if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("modified\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		resolvedRepo, resolvedBranch, err := inferRepoAndBranch("git", "", "feature/other")
		if err != nil {
			t.Fatalf("expected explicit non-current branch to bypass current-branch readiness check, got: %v", err)
		}
		repoAbs := canonicalPath(t, repo)
		if resolvedRepo != repoAbs {
			t.Fatalf("expected repo %s, got %s", repoAbs, resolvedRepo)
		}
		if resolvedBranch != "feature/other" {
			t.Fatalf("expected explicit branch feature/other, got %s", resolvedBranch)
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

func TestInferRepoAndBranchFallsBackToOldPWDFromSourceRoot(t *testing.T) {
	sourceRepo := createSyncedGitHubLikeRepo(t, "main")
	callerRepo := createSyncedGitHubLikeRepo(t, "feature/test")
	sourceRepoAbs := canonicalPath(t, sourceRepo)
	callerRepoAbs := canonicalPath(t, callerRepo)
	setSourceRootDetectorForTest(t, func() (string, bool) {
		return sourceRepoAbs, true
	})
	t.Setenv("OLDPWD", callerRepo)

	withWorkingDir(t, sourceRepo, func() {
		resolvedRepo, resolvedBranch, err := inferRepoAndBranch("git", "", "")
		if err != nil {
			t.Fatalf("inferRepoAndBranch failed: %v", err)
		}
		if resolvedRepo != callerRepoAbs {
			t.Fatalf("expected inferred repo %s, got %s", callerRepoAbs, resolvedRepo)
		}
		if resolvedBranch != "feature/test" {
			t.Fatalf("expected inferred branch feature/test, got %s", resolvedBranch)
		}
	})
}

func TestInferRepoAndBranchOldPWDFallbackIgnoredOutsideSourceRoot(t *testing.T) {
	currentRepo := createSyncedGitHubLikeRepo(t, "feature/current")
	otherRepo := createSyncedGitHubLikeRepo(t, "feature/other")
	currentRepoAbs := canonicalPath(t, currentRepo)
	setSourceRootDetectorForTest(t, func() (string, bool) {
		return canonicalPath(t, otherRepo), true
	})
	t.Setenv("OLDPWD", otherRepo)

	withWorkingDir(t, currentRepo, func() {
		resolvedRepo, resolvedBranch, err := inferRepoAndBranch("git", "", "")
		if err != nil {
			t.Fatalf("inferRepoAndBranch failed: %v", err)
		}
		if resolvedRepo != currentRepoAbs {
			t.Fatalf("expected inferred repo %s, got %s", currentRepoAbs, resolvedRepo)
		}
		if resolvedBranch != "feature/current" {
			t.Fatalf("expected inferred branch feature/current, got %s", resolvedBranch)
		}
	})
}

func TestInferRepoAndBranchPrefersCallerCWDEnv(t *testing.T) {
	sourceRepo := createSyncedGitHubLikeRepo(t, "main")
	callerRepo := createSyncedGitHubLikeRepo(t, "feature/caller")
	sourceRepoAbs := canonicalPath(t, sourceRepo)
	callerRepoAbs := canonicalPath(t, callerRepo)
	setSourceRootDetectorForTest(t, func() (string, bool) {
		return sourceRepoAbs, true
	})
	t.Setenv("OLDPWD", sourceRepo)
	t.Setenv(deepreviewCallerCWDEnv, callerRepo)

	withWorkingDir(t, sourceRepo, func() {
		resolvedRepo, resolvedBranch, err := inferRepoAndBranch("git", "", "")
		if err != nil {
			t.Fatalf("inferRepoAndBranch failed: %v", err)
		}
		if resolvedRepo != callerRepoAbs {
			t.Fatalf("expected inferred repo %s, got %s", callerRepoAbs, resolvedRepo)
		}
		if resolvedBranch != "feature/caller" {
			t.Fatalf("expected inferred branch feature/caller, got %s", resolvedBranch)
		}
	})
}
