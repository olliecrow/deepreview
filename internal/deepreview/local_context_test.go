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
		beforeFetchHead, beforeFetchHeadExists := readGitPathFile(t, repo, "FETCH_HEAD")
		beforeUpstreamSHA := strings.TrimSpace(runGitCommand(t, repo, "rev-parse", "--verify", "refs/remotes/origin/feature/test"))

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

		afterFetchHead, afterFetchHeadExists := readGitPathFile(t, repo, "FETCH_HEAD")
		if beforeFetchHeadExists != afterFetchHeadExists || beforeFetchHead != afterFetchHead {
			t.Fatalf("expected FETCH_HEAD to remain unchanged, before exists=%t after exists=%t", beforeFetchHeadExists, afterFetchHeadExists)
		}
		afterUpstreamSHA := strings.TrimSpace(runGitCommand(t, repo, "rev-parse", "--verify", "refs/remotes/origin/feature/test"))
		if afterUpstreamSHA != beforeUpstreamSHA {
			t.Fatalf("expected remote-tracking ref to remain unchanged, before=%s after=%s", beforeUpstreamSHA, afterUpstreamSHA)
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

func TestEnsureBranchReadyForRemoteReviewRejectsStaleTrackingRef(t *testing.T) {
	td := t.TempDir()
	remote := filepath.Join(td, "remote.git")
	seed := filepath.Join(td, "seed")
	user := filepath.Join(td, "user")
	other := filepath.Join(td, "other")

	runGitCommand(t, td, "init", "--bare", remote)
	runGitCommand(t, td, "clone", remote, seed)
	runGitCommand(t, seed, "config", "user.email", "test@example.com")
	runGitCommand(t, seed, "config", "user.name", "Test User")
	runGitCommand(t, seed, "checkout", "-b", "main")
	if err := os.WriteFile(filepath.Join(seed, "README.md"), []byte("seed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitCommand(t, seed, "add", "README.md")
	runGitCommand(t, seed, "commit", "-m", "seed")
	runGitCommand(t, seed, "push", "-u", "origin", "main")
	runGitCommand(t, seed, "checkout", "-b", "feature/test")
	if err := os.WriteFile(filepath.Join(seed, "feature.txt"), []byte("v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitCommand(t, seed, "add", "feature.txt")
	runGitCommand(t, seed, "commit", "-m", "feature")
	runGitCommand(t, seed, "push", "-u", "origin", "feature/test")

	runGitCommand(t, td, "clone", remote, user)
	runGitCommand(t, user, "checkout", "feature/test")
	beforeFetchHead, beforeFetchHeadExists := readGitPathFile(t, user, "FETCH_HEAD")
	beforeUpstreamSHA := strings.TrimSpace(runGitCommand(t, user, "rev-parse", "--verify", "refs/remotes/origin/feature/test"))

	runGitCommand(t, td, "clone", remote, other)
	runGitCommand(t, other, "checkout", "feature/test")
	runGitCommand(t, other, "config", "user.email", "test@example.com")
	runGitCommand(t, other, "config", "user.name", "Test User")
	if err := os.WriteFile(filepath.Join(other, "feature.txt"), []byte("v2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitCommand(t, other, "add", "feature.txt")
	runGitCommand(t, other, "commit", "-m", "remote advance")
	runGitCommand(t, other, "push", "origin", "feature/test")

	state := &LocalGitHubRepoState{
		Path:          user,
		CurrentBranch: "feature/test",
	}
	err := ensureBranchReadyForRemoteReview("git", state, "feature/test")
	if err == nil {
		t.Fatalf("expected stale remote-tracking ref to be rejected")
	}
	if !strings.Contains(err.Error(), "stale versus remote branch") {
		t.Fatalf("unexpected error: %v", err)
	}
	afterFetchHead, afterFetchHeadExists := readGitPathFile(t, user, "FETCH_HEAD")
	if beforeFetchHeadExists != afterFetchHeadExists || beforeFetchHead != afterFetchHead {
		t.Fatalf("expected FETCH_HEAD to remain unchanged, before exists=%t after exists=%t", beforeFetchHeadExists, afterFetchHeadExists)
	}
	afterUpstreamSHA := strings.TrimSpace(runGitCommand(t, user, "rev-parse", "--verify", "refs/remotes/origin/feature/test"))
	if afterUpstreamSHA != beforeUpstreamSHA {
		t.Fatalf("expected remote-tracking ref to remain unchanged, before=%s after=%s", beforeUpstreamSHA, afterUpstreamSHA)
	}
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
