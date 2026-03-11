package deepreview

import (
	"os"
	"path/filepath"
	"strings"
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
