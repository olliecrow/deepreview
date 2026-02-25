package deepreview

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCloneOrFetchReplacesStaleDirectory(t *testing.T) {
	td := t.TempDir()
	remote := filepath.Join(td, "remote.git")
	seed := filepath.Join(td, "seed")
	managed := filepath.Join(td, "managed")

	runGitCommand(t, td, "init", "--bare", remote)
	runGitCommand(t, td, "clone", remote, seed)
	runGitCommand(t, td, "-C", seed, "config", "user.email", "test@example.com")
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
