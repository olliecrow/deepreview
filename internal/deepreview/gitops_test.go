package deepreview

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestFilesystemSafeKeyIsStableDistinctAndPathSafe(t *testing.T) {
	first := FilesystemSafeKey("feature/test")
	second := FilesystemSafeKey("feature:test")
	repeat := FilesystemSafeKey("feature/test")

	if first != repeat {
		t.Fatalf("expected stable key, got %q and %q", first, repeat)
	}
	if first == second {
		t.Fatalf("expected distinct keys for distinct branches, both were %q", first)
	}
	if matched := regexp.MustCompile(`^[A-Za-z0-9._-]+$`).MatchString(first); !matched {
		t.Fatalf("expected path-safe key, got %q", first)
	}
	if strings.Contains(first, "..") || strings.Contains(first, "/") {
		t.Fatalf("expected key without traversal/path separators, got %q", first)
	}
}

func TestCloneOrFetchReplacesStaleDirectory(t *testing.T) {
	td := t.TempDir()
	remote := filepath.Join(td, "remote.git")
	seed := filepath.Join(td, "seed")
	managed := filepath.Join(td, "managed")

	runGitCommand(t, td, "init", "--bare", remote)
	runGitCommand(t, td, "clone", remote, seed)
	runGitCommand(t, td, "-C", seed, "config", "user.email", testPlaceholderEmail("test"))
	runGitCommand(t, td, "-C", seed, "config", "user.name", "Test User")
	runGitCommand(t, td, "-C", seed, "checkout", "-b", "main")
	if err := os.WriteFile(filepath.Join(seed, "README.md"), []byte("seed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitCommand(t, td, "-C", seed, "add", "README.md")
	runGitCommand(t, td, "-C", seed, "commit", "-m", "seed")
	runGitCommand(t, td, "-C", seed, "push", "-u", "origin", "main")

	if err := os.MkdirAll(managed, 0o755); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(managed, "stale.txt")
	if err := os.WriteFile(stale, []byte("stale\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := CloneOrFetch(managed, remote, "git"); err != nil {
		t.Fatalf("CloneOrFetch failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(managed, ".git")); err != nil {
		t.Fatalf("expected cloned repo with .git, got error: %v", err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatalf("expected stale file removal, got err=%v", err)
	}
}

func TestDryRunPushRefspec(t *testing.T) {
	td := t.TempDir()
	remote := filepath.Join(td, "remote.git")
	repo := filepath.Join(td, "repo")

	runGitCommand(t, td, "init", "--bare", remote)
	runGitCommand(t, td, "clone", remote, repo)
	runGitCommand(t, td, "-C", repo, "config", "user.email", "test@example.com")
	runGitCommand(t, td, "-C", repo, "config", "user.name", "Test User")
	runGitCommand(t, td, "-C", repo, "checkout", "-b", "main")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("seed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitCommand(t, td, "-C", repo, "add", "README.md")
	runGitCommand(t, td, "-C", repo, "commit", "-m", "seed")
	runGitCommand(t, td, "-C", repo, "push", "-u", "origin", "main")

	if err := DryRunPushRefspec(repo, "git", "HEAD:main"); err != nil {
		t.Fatalf("DryRunPushRefspec failed: %v", err)
	}
}

func TestPublicationRefspecUsesTheCommitAndFullRef(t *testing.T) {
	sha := strings.Repeat("a", 40)
	refspec, err := PublicationRefspec("git", sha, "deepreview/feature/test")
	if err != nil {
		t.Fatal(err)
	}
	if refspec != sha+":refs/heads/deepreview/feature/test" {
		t.Fatalf("unexpected refspec %q", refspec)
	}

	if _, err := PublicationRefspec("git", "main", "main"); err == nil {
		t.Fatal("expected a branch name to be rejected as a publication commit")
	}
	if _, err := PublicationRefspec("git", sha, "-force"); err == nil {
		t.Fatal("expected an option-shaped branch to be rejected")
	}
	if _, err := PublicationRefspec("git", sha, "bad:ref"); err == nil {
		t.Fatal("expected a refspec-shaped branch to be rejected")
	}
}

func TestPushVerifiedRefPushesTheRequestedCommit(t *testing.T) {
	td := t.TempDir()
	remote := filepath.Join(td, "remote.git")
	repo := filepath.Join(td, "repo")
	runGitCommand(t, td, "init", "--bare", remote)
	runGitCommand(t, td, "clone", remote, repo)
	runGitCommand(t, td, "-C", repo, "config", "user.email", "test@example.com")
	runGitCommand(t, td, "-C", repo, "config", "user.name", "Test User")
	runGitCommand(t, td, "-C", repo, "checkout", "-b", "main")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("seed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitCommand(t, td, "-C", repo, "add", "README.md")
	runGitCommand(t, td, "-C", repo, "commit", "-m", "seed")
	reviewed := strings.TrimSpace(runGitCommand(t, td, "-C", repo, "rev-parse", "HEAD"))

	if err := os.WriteFile(filepath.Join(repo, "later.txt"), []byte("later\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitCommand(t, td, "-C", repo, "add", "later.txt")
	runGitCommand(t, td, "-C", repo, "commit", "-m", "later")

	refspec, err := PushVerifiedRef(repo, "git", reviewed, "release/topic")
	if err != nil {
		t.Fatalf("PushVerifiedRef failed: %v", err)
	}
	if !strings.HasPrefix(refspec, reviewed+":refs/heads/release/topic") {
		t.Fatalf("unexpected refspec %q", refspec)
	}
	remoteHead := strings.TrimSpace(runGitCommand(t, td, "--git-dir", remote, "rev-parse", "refs/heads/release/topic"))
	if remoteHead != reviewed {
		t.Fatalf("remote ref = %s, want reviewed commit %s", remoteHead, reviewed)
	}

	runGitCommand(t, td, "-C", repo, "checkout", "--orphan", "unrelated")
	if err := os.WriteFile(filepath.Join(repo, "unrelated.txt"), []byte("unrelated\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitCommand(t, td, "-C", repo, "add", "unrelated.txt")
	runGitCommand(t, td, "-C", repo, "commit", "-m", "unrelated")
	unrelated := strings.TrimSpace(runGitCommand(t, td, "-C", repo, "rev-parse", "HEAD"))
	if _, err := PushVerifiedRef(repo, "git", unrelated, "release/topic"); err == nil {
		t.Fatal("expected a non-fast-forward publication to fail")
	}
	remoteHead = strings.TrimSpace(runGitCommand(t, td, "--git-dir", remote, "rev-parse", "refs/heads/release/topic"))
	if remoteHead != reviewed {
		t.Fatalf("rejected publication moved the remote ref to %s", remoteHead)
	}
}

func TestRefContainsCommit(t *testing.T) {
	td := t.TempDir()
	repo := filepath.Join(td, "repo")

	runGitCommand(t, td, "init", "-b", "main", repo)
	runGitCommand(t, td, "-C", repo, "config", "user.email", "test@example.com")
	runGitCommand(t, td, "-C", repo, "config", "user.name", "Test User")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("seed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitCommand(t, td, "-C", repo, "add", "README.md")
	runGitCommand(t, td, "-C", repo, "commit", "-m", "seed")

	runGitCommand(t, td, "-C", repo, "checkout", "-b", "candidate")
	if err := os.WriteFile(filepath.Join(repo, "feature.txt"), []byte("feature\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitCommand(t, td, "-C", repo, "add", "feature.txt")
	runGitCommand(t, td, "-C", repo, "commit", "-m", "feature")
	candidateHead := runGitCommand(t, td, "-C", repo, "rev-parse", "HEAD")

	runGitCommand(t, td, "-C", repo, "checkout", "-b", "delivery")
	if err := os.WriteFile(filepath.Join(repo, "delivery.txt"), []byte("delivery\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitCommand(t, td, "-C", repo, "add", "delivery.txt")
	runGitCommand(t, td, "-C", repo, "commit", "-m", "delivery")

	contains, err := RefContainsCommit(repo, "git", candidateHead, "delivery")
	if err != nil {
		t.Fatalf("RefContainsCommit delivery failed: %v", err)
	}
	if !contains {
		t.Fatalf("expected delivery branch to contain %s", candidateHead)
	}

	contains, err = RefContainsCommit(repo, "git", candidateHead, "main")
	if err != nil {
		t.Fatalf("RefContainsCommit main failed: %v", err)
	}
	if contains {
		t.Fatalf("did not expect main to contain candidate head %s", candidateHead)
	}
}

func TestFastForwardBranchRejectsNonDescendantHistory(t *testing.T) {
	td := t.TempDir()
	repo := filepath.Join(td, "repo")

	runGitCommand(t, td, "init", "-b", "main", repo)
	runGitCommand(t, td, "-C", repo, "config", "user.email", "test@example.com")
	runGitCommand(t, td, "-C", repo, "config", "user.name", "Test User")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitCommand(t, td, "-C", repo, "add", "README.md")
	runGitCommand(t, td, "-C", repo, "commit", "-m", "base")
	base := strings.TrimSpace(runGitCommand(t, td, "-C", repo, "rev-parse", "HEAD"))
	if err := CreateBranchAtRef(repo, "git", "candidate", base); err != nil {
		t.Fatalf("create candidate branch: %v", err)
	}

	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("forward\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitCommand(t, td, "-C", repo, "commit", "-am", "forward")
	forward := strings.TrimSpace(runGitCommand(t, td, "-C", repo, "rev-parse", "HEAD"))
	if err := FastForwardBranch(repo, "git", "candidate", forward); err != nil {
		t.Fatalf("fast-forward candidate: %v", err)
	}

	runGitCommand(t, td, "-C", repo, "checkout", "-b", "diverged", base)
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("diverged\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitCommand(t, td, "-C", repo, "commit", "-am", "diverged")
	diverged := strings.TrimSpace(runGitCommand(t, td, "-C", repo, "rev-parse", "HEAD"))
	if err := FastForwardBranch(repo, "git", "candidate", diverged); err == nil {
		t.Fatal("expected non-descendant candidate update to fail")
	}
	if got := strings.TrimSpace(runGitCommand(t, td, "-C", repo, "rev-parse", "candidate")); got != forward {
		t.Fatalf("candidate moved after rejected update: got %s want %s", got, forward)
	}
}

func TestRefsHaveSameTree(t *testing.T) {
	td := t.TempDir()
	repo := filepath.Join(td, "repo")

	runGitCommand(t, td, "init", "-b", "main", repo)
	runGitCommand(t, td, "-C", repo, "config", "user.email", "test@example.com")
	runGitCommand(t, td, "-C", repo, "config", "user.name", "Test User")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("seed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitCommand(t, td, "-C", repo, "add", "README.md")
	runGitCommand(t, td, "-C", repo, "commit", "-m", "seed")

	runGitCommand(t, td, "-C", repo, "checkout", "-b", "candidate")
	if err := os.WriteFile(filepath.Join(repo, "feature.txt"), []byte("feature\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitCommand(t, td, "-C", repo, "add", "feature.txt")
	runGitCommand(t, td, "-C", repo, "commit", "-m", "feature")

	runGitCommand(t, td, "-C", repo, "checkout", "main")
	runGitCommand(t, td, "-C", repo, "checkout", "-b", "rebuilt")
	runGitCommand(t, td, "-C", repo, "restore", "--source", "candidate", "--staged", "--worktree", ":/")
	runGitCommand(t, td, "-C", repo, "commit", "-m", "rebuilt")

	sameTree, err := RefsHaveSameTree(repo, "git", "candidate", "rebuilt")
	if err != nil {
		t.Fatalf("RefsHaveSameTree candidate/rebuilt failed: %v", err)
	}
	if !sameTree {
		t.Fatalf("expected rebuilt branch to match candidate tree")
	}

	sameTree, err = RefsHaveSameTree(repo, "git", "main", "candidate")
	if err != nil {
		t.Fatalf("RefsHaveSameTree main/candidate failed: %v", err)
	}
	if sameTree {
		t.Fatalf("did not expect main and candidate trees to match")
	}
}

func TestResolveDefaultBranchFallsBackToExactRemoteRef(t *testing.T) {
	cases := []struct {
		name   string
		branch string
		want   string
	}{
		{name: "main", branch: "main", want: "main"},
		{name: "master", branch: "master", want: "master"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := createRepoForDefaultBranchFallbackTest(t, tc.branch)
			got, err := ResolveDefaultBranch(repo, "git")
			if err != nil {
				t.Fatalf("ResolveDefaultBranch failed: %v", err)
			}
			if got != tc.want {
				t.Fatalf("ResolveDefaultBranch = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestResolveDefaultBranchDoesNotMatchPrefixBranches(t *testing.T) {
	for _, branch := range []string{"mainline", "master-old"} {
		t.Run(branch, func(t *testing.T) {
			repo := createRepoForDefaultBranchFallbackTest(t, branch)
			_, err := ResolveDefaultBranch(repo, "git")
			if err == nil {
				t.Fatalf("expected ResolveDefaultBranch to reject %q without exact main/master ref", branch)
			}
			if !strings.Contains(err.Error(), "unable to resolve default branch") {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func createRepoForDefaultBranchFallbackTest(t *testing.T, branch string) string {
	t.Helper()
	td := t.TempDir()
	remote := filepath.Join(td, "remote.git")
	seed := filepath.Join(td, "seed")
	repo := filepath.Join(td, "repo")

	runGitCommand(t, td, "init", "--bare", remote)
	runGitCommand(t, td, "clone", remote, seed)
	runGitCommand(t, td, "-C", seed, "config", "user.email", testPlaceholderEmail("test"))
	runGitCommand(t, td, "-C", seed, "config", "user.name", "Test User")
	runGitCommand(t, td, "-C", seed, "checkout", "-b", branch)
	if err := os.WriteFile(filepath.Join(seed, "README.md"), []byte("seed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitCommand(t, td, "-C", seed, "add", "README.md")
	runGitCommand(t, td, "-C", seed, "commit", "-m", "seed")
	runGitCommand(t, td, "-C", seed, "push", "-u", "origin", branch)

	runGitCommand(t, td, "clone", remote, repo)
	_ = os.Remove(filepath.Join(repo, ".git", "refs", "remotes", "origin", "HEAD"))
	return repo
}

func TestEnsureWorktreeOperationalExcludesResolvesRelativeGitPathAgainstRepo(t *testing.T) {
	td := t.TempDir()
	repo := filepath.Join(td, "repo")
	caller := filepath.Join(td, "caller")

	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(caller, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(caller, ".git"), []byte("not-a-directory\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	runGitCommand(t, td, "init", "-b", "main", repo)
	runGitCommand(t, td, "-C", repo, "config", "user.email", "test@example.com")
	runGitCommand(t, td, "-C", repo, "config", "user.name", "Test User")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("seed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitCommand(t, td, "-C", repo, "add", "README.md")
	runGitCommand(t, td, "-C", repo, "commit", "-m", "seed")

	excludePath := filepath.Join(repo, ".git", "info", "exclude")
	withWorkingDir(t, caller, func() {
		if err := EnsureWorktreeOperationalExcludes(repo, "git"); err != nil {
			t.Fatalf("EnsureWorktreeOperationalExcludes failed: %v", err)
		}
	})

	excludeBytes, err := os.ReadFile(excludePath)
	if err != nil {
		t.Fatalf("expected repo exclude file to be written: %v", err)
	}
	excludeContent := string(excludeBytes)
	if !strings.Contains(excludeContent, operationalExcludeBlockStart) {
		t.Fatalf("expected managed exclude block start; content follows\n%s", excludeContent)
	}
	if !strings.Contains(excludeContent, operationalExcludeBlockEnd) {
		t.Fatalf("expected managed exclude block end; content follows\n%s", excludeContent)
	}
	if !strings.Contains(excludeContent, ".deepreview/") {
		t.Fatalf("expected .deepreview pattern in exclude block; content follows\n%s", excludeContent)
	}

	withWorkingDir(t, caller, func() {
		if err := EnsureWorktreeOperationalExcludes(repo, "git"); err != nil {
			t.Fatalf("EnsureWorktreeOperationalExcludes second run failed: %v", err)
		}
	})
	excludeBytesAfter, err := os.ReadFile(excludePath)
	if err != nil {
		t.Fatalf("expected repo exclude file after second run: %v", err)
	}
	if string(excludeBytesAfter) != excludeContent {
		t.Fatalf("expected idempotent exclude content\nprevious content\n%s\ncurrent content\n%s", excludeContent, string(excludeBytesAfter))
	}
}

func TestCleanupUntrackedOperationalArtifactsRemovesReadOnlyGoModuleCache(t *testing.T) {
	td := t.TempDir()
	repo := filepath.Join(td, "repo")
	runGitCommand(t, td, "init", "-b", "main", repo)
	runGitCommand(t, td, "-C", repo, "config", "user.email", "test@example.com")
	runGitCommand(t, td, "-C", repo, "config", "user.name", "Test User")

	readonlyDir := filepath.Join(repo, ".tmp", "go-mod-cache", "golang.org", "x", "sys@v0.37.0")
	if err := os.MkdirAll(readonlyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cacheFile := filepath.Join(readonlyDir, "go.mod")
	if err := os.WriteFile(cacheFile, []byte("module golang.org/x/sys\n"), 0o444); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(readonlyDir, 0o555); err != nil {
		t.Fatal(err)
	}

	if err := CleanupUntrackedOperationalArtifacts(repo, "git"); err != nil {
		t.Fatalf("CleanupUntrackedOperationalArtifacts failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repo, ".tmp")); !os.IsNotExist(err) {
		t.Fatalf("expected .tmp cache tree to be removed, got err=%v", err)
	}
}

func TestCommitAllChangesUsesProvidedIdentityAndDisablesSigning(t *testing.T) {
	td := t.TempDir()
	repo := filepath.Join(td, "repo")
	runGitCommand(t, td, "init", "-b", "main", repo)
	runGitCommand(t, td, "-C", repo, "config", "user.email", "wrong@example.com")
	runGitCommand(t, td, "-C", repo, "config", "user.name", "Wrong User")
	runGitCommand(t, td, "-C", repo, "config", "commit.gpgsign", "true")
	runGitCommand(t, td, "-C", repo, "config", "gpg.program", "/nonexistent-gpg")

	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("seed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := CommitAllChanges(repo, "git", "seed", CommitIdentity{Name: "Loopy Test", Email: "loopy-test@example.com"}); err != nil {
		t.Fatalf("initial commit failed: %v", err)
	}

	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("updated\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := CommitAllChanges(repo, "git", "update", CommitIdentity{Name: "Loopy Test", Email: "loopy-test@example.com"}); err != nil {
		t.Fatalf("CommitAllChanges failed with signing disabled path: %v", err)
	}

	authorName := strings.TrimSpace(runGitCommand(t, td, "-C", repo, "log", "-1", "--format=%an"))
	if authorName != "Loopy Test" {
		t.Fatalf("expected author name Loopy Test, got %q", authorName)
	}
	authorEmail := strings.TrimSpace(runGitCommand(t, td, "-C", repo, "log", "-1", "--format=%ae"))
	if authorEmail != "loopy-test@example.com" {
		t.Fatalf("expected author email loopy-test@example.com, got %q", authorEmail)
	}
}

func TestResolveCommitIdentityUsesRepoConfigForLocalRepo(t *testing.T) {
	td := t.TempDir()
	repo := filepath.Join(td, "repo")
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(td, "empty.gitconfig"))
	runGitCommand(t, td, "init", "-b", "main", repo)
	runGitCommand(t, td, "-C", repo, "config", "user.email", "repo-user@example.com")
	runGitCommand(t, td, "-C", repo, "config", "user.name", "Repo User")

	identity, err := ResolveCommitIdentity("git", repo)
	if err != nil {
		t.Fatalf("ResolveCommitIdentity failed: %v", err)
	}
	if identity.Name != "Repo User" {
		t.Fatalf("expected repo user name, got %q", identity.Name)
	}
	if identity.Email != "repo-user@example.com" {
		t.Fatalf("expected repo user email, got %q", identity.Email)
	}
}

func TestResolveCommitIdentityUsesRepoConfigForLocalWorktreePath(t *testing.T) {
	td := t.TempDir()
	repo := filepath.Join(td, "repo")
	worktree := filepath.Join(td, "worktrees", "feature")
	globalConfig := filepath.Join(td, "global.gitconfig")

	t.Setenv("GIT_CONFIG_GLOBAL", globalConfig)
	runGitCommand(t, td, "config", "--global", "user.name", "Global User")
	runGitCommand(t, td, "config", "--global", "user.email", "global-user@example.com")

	runGitCommand(t, td, "init", "-b", "main", repo)
	runGitCommand(t, td, "-C", repo, "config", "user.email", "repo-user@example.com")
	runGitCommand(t, td, "-C", repo, "config", "user.name", "Repo User")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("seed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitCommand(t, td, "-C", repo, "add", "README.md")
	runGitCommand(t, td, "-C", repo, "commit", "-m", "seed")
	runGitCommand(t, td, "-C", repo, "worktree", "add", "-b", "feature/test", worktree)

	identity, err := ResolveCommitIdentity("git", worktree)
	if err != nil {
		t.Fatalf("ResolveCommitIdentity failed for worktree path: %v", err)
	}
	if identity.Name != "Repo User" {
		t.Fatalf("expected repo worktree user name, got %q", identity.Name)
	}
	if identity.Email != "repo-user@example.com" {
		t.Fatalf("expected repo worktree user email, got %q", identity.Email)
	}
}

func TestResolveCommitIdentityUsesGlobalConfigWhenRepoConfigMissing(t *testing.T) {
	td := t.TempDir()
	repo := filepath.Join(td, "repo")
	globalConfig := filepath.Join(td, "global.gitconfig")
	runGitCommand(t, td, "init", "-b", "main", repo)
	t.Setenv("GIT_CONFIG_GLOBAL", globalConfig)
	runGitCommand(t, td, "config", "--global", "user.name", "Global User")
	runGitCommand(t, td, "config", "--global", "user.email", "global-user@example.com")

	identity, err := ResolveCommitIdentity("git", repo)
	if err != nil {
		t.Fatalf("ResolveCommitIdentity failed: %v", err)
	}
	if identity.Name != "Global User" {
		t.Fatalf("expected global user name, got %q", identity.Name)
	}
	if identity.Email != "global-user@example.com" {
		t.Fatalf("expected global user email, got %q", identity.Email)
	}
}

func TestResolveCommitIdentityUsesMatchedLocalRepoConfigForRepoLocator(t *testing.T) {
	repo := createSyncedGitHubLikeRepo(t, "feature/test")
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "empty.gitconfig"))

	testCases := []struct {
		name    string
		locator string
	}{
		{name: "owner repo", locator: "example-org/example-repo"},
		{name: "owner repo case variant", locator: "Example-Org/Example-Repo"},
		{name: "remote url", locator: "https://github.com/example-org/example-repo.git"},
		{name: "remote url case variant", locator: "https://github.com/Example-Org/Example-Repo.git"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			withWorkingDir(t, repo, func() {
				identity, err := ResolveCommitIdentity("git", tc.locator)
				if err != nil {
					t.Fatalf("ResolveCommitIdentity failed: %v", err)
				}
				if identity.Name != "Test User" {
					t.Fatalf("expected repo-matched user name, got %q", identity.Name)
				}
				if identity.Email != "test@example.com" {
					t.Fatalf("expected repo-matched user email, got %q", identity.Email)
				}
			})
		})
	}
}

func TestResolveCommitIdentityHonorsCallerCWDOutsideSourceRootForRepoLocator(t *testing.T) {
	currentRepo := createSyncedGitHubLikeRepo(t, "feature/current")
	callerRepo := createSyncedGitHubLikeRepo(t, "feature/caller")
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "empty.gitconfig"))
	t.Setenv(deepreviewCallerCWDEnv, callerRepo)

	runGitCommand(t, filepath.Dir(currentRepo), "-C", currentRepo, "config", "user.name", "Current User")
	runGitCommand(t, filepath.Dir(currentRepo), "-C", currentRepo, "config", "user.email", "current-user@example.com")
	runGitCommand(t, filepath.Dir(callerRepo), "-C", callerRepo, "config", "user.name", "Caller User")
	runGitCommand(t, filepath.Dir(callerRepo), "-C", callerRepo, "config", "user.email", "caller-user@example.com")

	withWorkingDir(t, currentRepo, func() {
		identity, err := ResolveCommitIdentity("git", "example-org/example-repo")
		if err != nil {
			t.Fatalf("ResolveCommitIdentity failed: %v", err)
		}
		if identity.Name != "Caller User" {
			t.Fatalf("expected caller repo user name, got %q", identity.Name)
		}
		if identity.Email != "caller-user@example.com" {
			t.Fatalf("expected caller repo user email, got %q", identity.Email)
		}
	})
}

func TestResolveCommitIdentityRejectsInvalidCallerCWDForRepoLocator(t *testing.T) {
	currentRepo := createSyncedGitHubLikeRepo(t, "feature/current")
	callerRepo := createSyncedGitHubLikeRepo(t, "feature/caller")
	currentRepoAbs := canonicalPath(t, currentRepo)
	setSourceRootDetectorForTest(t, func() (string, bool) {
		return currentRepoAbs, true
	})
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "empty.gitconfig"))
	t.Setenv("OLDPWD", callerRepo)
	t.Setenv(deepreviewCallerCWDEnv, filepath.Join(t.TempDir(), "missing"))

	withWorkingDir(t, currentRepo, func() {
		_, err := ResolveCommitIdentity("git", "example-org/example-repo")
		if err == nil {
			t.Fatalf("expected invalid %s override to fail", deepreviewCallerCWDEnv)
		}
		if !strings.Contains(err.Error(), deepreviewCallerCWDEnv) {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestConfigureManagedGitIdentityEnablesPlainGitCommitWithoutSigner(t *testing.T) {
	td := t.TempDir()
	repo := filepath.Join(td, "repo")
	runGitCommand(t, td, "init", "-b", "main", repo)
	runGitCommand(t, td, "-C", repo, "config", "commit.gpgsign", "true")
	runGitCommand(t, td, "-C", repo, "config", "gpg.program", "/nonexistent-gpg")

	if err := ConfigureManagedGitIdentity(repo, "git", CommitIdentity{Name: "Loopy Test", Email: "loopy-test@example.com"}); err != nil {
		t.Fatalf("ConfigureManagedGitIdentity failed: %v", err)
	}

	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("seed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitCommand(t, td, "-C", repo, "add", "README.md")
	runGitCommand(t, td, "-C", repo, "commit", "-m", "seed")

	authorName := strings.TrimSpace(runGitCommand(t, td, "-C", repo, "log", "-1", "--format=%an"))
	if authorName != "Loopy Test" {
		t.Fatalf("expected author name Loopy Test, got %q", authorName)
	}
	authorEmail := strings.TrimSpace(runGitCommand(t, td, "-C", repo, "log", "-1", "--format=%ae"))
	if authorEmail != "loopy-test@example.com" {
		t.Fatalf("expected author email loopy-test@example.com, got %q", authorEmail)
	}
}

func initCommittedRepo(t *testing.T, repo string) {
	t.Helper()
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	runGitCommand(t, repo, "init", "-b", "main")
	runGitCommand(t, repo, "-C", repo, "config", "user.email", "test@example.com")
	runGitCommand(t, repo, "-C", repo, "config", "user.name", "Test User")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("seed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitCommand(t, repo, "-C", repo, "add", "README.md")
	runGitCommand(t, repo, "-C", repo, "commit", "-m", "seed")
}

func TestRemoveWorktreePrunesMissingDirectory(t *testing.T) {
	td := t.TempDir()
	repo := filepath.Join(td, "repo")
	worktree := filepath.Join(td, "worktree")
	initCommittedRepo(t, repo)
	if err := AddDetachedWorktree(repo, "git", worktree, "HEAD"); err != nil {
		t.Fatalf("AddDetachedWorktree failed: %v", err)
	}
	if err := os.RemoveAll(worktree); err != nil {
		t.Fatal(err)
	}

	if err := RemoveWorktree(repo, "git", worktree); err != nil {
		t.Fatalf("RemoveWorktree failed: %v", err)
	}
	list := runGitCommand(t, td, "-C", repo, "worktree", "list", "--porcelain")
	if strings.Contains(list, worktree) {
		t.Fatalf("stale worktree registration remained:\n%s", list)
	}
	if _, err := os.Stat(filepath.Join(repo, "README.md")); err != nil {
		t.Fatalf("repository checkout was damaged: %v", err)
	}
}

func TestRemoveWorktreeDeletesReadOnlyOwnedFiles(t *testing.T) {
	td := t.TempDir()
	repo := filepath.Join(td, "repo")
	worktree := filepath.Join(td, "worktree")
	sibling := filepath.Join(td, "keep.txt")
	initCommittedRepo(t, repo)
	if err := AddDetachedWorktree(repo, "git", worktree, "HEAD"); err != nil {
		t.Fatalf("AddDetachedWorktree failed: %v", err)
	}
	cacheDir := filepath.Join(worktree, "cache")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cacheDir, "locked"), []byte("locked\n"), 0o444); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(cacheDir, 0o555); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sibling, []byte("keep\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := RemoveWorktree(repo, "git", worktree); err != nil {
		t.Fatalf("RemoveWorktree failed: %v", err)
	}
	if _, err := os.Stat(worktree); !os.IsNotExist(err) {
		t.Fatalf("expected worktree removal, err=%v", err)
	}
	if _, err := os.Stat(sibling); err != nil {
		t.Fatalf("sibling file was removed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repo, "README.md")); err != nil {
		t.Fatalf("repository checkout was damaged: %v", err)
	}
}

func TestRemoveWorktreeAcceptsSymlinkParentAlias(t *testing.T) {
	td := t.TempDir()
	realBase := filepath.Join(td, "real")
	if err := os.MkdirAll(realBase, 0o755); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(td, "alias")
	if err := os.Symlink(realBase, alias); err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(alias, "repo")
	worktree := filepath.Join(alias, "worktree")
	initCommittedRepo(t, repo)
	if err := AddDetachedWorktree(repo, "git", worktree, "HEAD"); err != nil {
		t.Fatalf("AddDetachedWorktree failed: %v", err)
	}

	gitFile := filepath.Join(worktree, ".git")
	raw, err := os.ReadFile(gitFile)
	if err != nil {
		t.Fatal(err)
	}
	pointer, err := parseGitdirPointer(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(pointer) {
		pointer = filepath.Join(worktree, pointer)
	}
	resolvedGit, err := filepath.EvalSymlinks(gitFile)
	if err != nil {
		t.Fatal(err)
	}
	if resolvedGit == filepath.Clean(gitFile) {
		t.Fatal("symlink alias did not change the worktree path")
	}
	if err := os.WriteFile(filepath.Join(pointer, "gitdir"), []byte(resolvedGit+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := RemoveWorktree(repo, "git", worktree); err != nil {
		t.Fatalf("RemoveWorktree failed: %v", err)
	}
	if _, err := os.Stat(worktree); !os.IsNotExist(err) {
		t.Fatalf("expected worktree removal, err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(repo, "README.md")); err != nil {
		t.Fatalf("repository checkout was damaged: %v", err)
	}
	list := runGitCommand(t, td, "-C", filepath.Join(realBase, "repo"), "worktree", "list", "--porcelain")
	for _, line := range strings.Split(list, "\n") {
		if strings.HasPrefix(line, "worktree ") && strings.HasSuffix(strings.TrimSpace(line), string(filepath.Separator)+"worktree") {
			t.Fatalf("worktree registration remained:\n%s", list)
		}
	}
}

func TestRemoveWorktreeRejectsUnownedPath(t *testing.T) {
	td := t.TempDir()
	repo := filepath.Join(td, "repo")
	unowned := filepath.Join(td, "notes")
	initCommittedRepo(t, repo)
	if err := os.MkdirAll(unowned, 0o755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(unowned, "keep.txt")
	if err := os.WriteFile(marker, []byte("keep\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := RemoveWorktree(repo, "git", unowned)
	var notOwned *worktreeNotOwnedError
	if !errors.As(err, &notOwned) {
		t.Fatalf("expected ownership refusal, got %v", err)
	}
	if _, statErr := os.Stat(marker); statErr != nil {
		t.Fatalf("unowned path was removed: %v", statErr)
	}
}

func TestRemoveWorktreeRejectsRepositoryCheckout(t *testing.T) {
	td := t.TempDir()
	repo := filepath.Join(td, "repo")
	initCommittedRepo(t, repo)

	err := RemoveWorktree(repo, "git", repo)
	var notOwned *worktreeNotOwnedError
	if !errors.As(err, &notOwned) {
		t.Fatalf("expected checkout refusal, got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(repo, "README.md")); statErr != nil {
		t.Fatalf("repository checkout was damaged: %v", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(repo, ".git")); statErr != nil {
		t.Fatalf("repository git directory was damaged: %v", statErr)
	}
}

func TestRemoveWorktreeBoundsHungGit(t *testing.T) {
	td := t.TempDir()
	repo := filepath.Join(td, "repo")
	worktree := filepath.Join(td, "worktree")
	initCommittedRepo(t, repo)
	if err := AddDetachedWorktree(repo, "git", worktree, "HEAD"); err != nil {
		t.Fatalf("AddDetachedWorktree failed: %v", err)
	}
	fakeGit := filepath.Join(td, "git")
	writeShellScript(t, fakeGit, "#!/bin/sh\nexec sleep 60\n")

	started := time.Now()
	err := RemoveWorktree(repo, fakeGit, worktree)
	elapsed := time.Since(started)
	if err == nil {
		t.Fatal("hung git should not report cleanup success")
	}
	if elapsed > worktreeCleanupBudget+commandWaitDelay+4*time.Second {
		t.Fatalf("cleanup returned in %s", elapsed)
	}
	if _, statErr := os.Stat(filepath.Join(repo, "README.md")); statErr != nil {
		t.Fatalf("repository checkout was damaged: %v", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(repo, ".git")); statErr != nil {
		t.Fatalf("repository git directory was damaged: %v", statErr)
	}
}

func TestAddDetachedWorktreeFailureLeavesRepositoryIntact(t *testing.T) {
	td := t.TempDir()
	repo := filepath.Join(td, "repo")
	worktree := filepath.Join(td, "missing-ref-worktree")
	initCommittedRepo(t, repo)

	err := AddDetachedWorktree(repo, "git", worktree, "refs/heads/missing-ref")
	if err == nil {
		t.Fatal("expected worktree add to fail")
	}
	if _, statErr := os.Stat(filepath.Join(repo, "README.md")); statErr != nil {
		t.Fatalf("repository checkout was damaged: %v", statErr)
	}
	info, statErr := os.Stat(worktree)
	if statErr == nil && info.IsDir() {
		entries, readErr := os.ReadDir(worktree)
		if readErr != nil || len(entries) != 0 {
			t.Fatalf("failed worktree add left content at %s", worktree)
		}
	} else if statErr != nil && !os.IsNotExist(statErr) {
		t.Fatalf("stat failed worktree: %v", statErr)
	}
}
