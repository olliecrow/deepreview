package deepreview

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func runGitCommand(t *testing.T, cwd string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = cwd
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git command failed: %v\nargs=%v\noutput=%s", err, args, string(out))
	}
	return string(out)
}

func withWorkingDir(t *testing.T, dir string, fn func()) {
	t.Helper()
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = os.Chdir(previous)
	}()
	fn()
}

func createSyncedGitHubLikeRepo(t *testing.T, branch string) string {
	t.Helper()
	td := t.TempDir()
	repo := filepath.Join(td, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}

	runGitCommand(t, td, "init", repo)
	runGitCommand(t, td, "-C", repo, "config", "user.email", "test@example.com")
	runGitCommand(t, td, "-C", repo, "config", "user.name", "Test User")
	runGitCommand(t, td, "-C", repo, "checkout", "-b", branch)
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("seed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitCommand(t, td, "-C", repo, "add", "README.md")
	runGitCommand(t, td, "-C", repo, "commit", "-m", "seed")
	runGitCommand(t, td, "-C", repo, "remote", "add", "origin", "https://github.com/example-org/example-repo.git")

	head := strings.TrimSpace(runGitCommand(t, td, "-C", repo, "rev-parse", "HEAD"))
	runGitCommand(t, td, "-C", repo, "update-ref", "refs/remotes/origin/"+branch, head)
	runGitCommand(t, td, "-C", repo, "branch", "--set-upstream-to=origin/"+branch, branch)
	return repo
}

func canonicalPath(t *testing.T, path string) string {
	t.Helper()
	abs, err := filepath.Abs(path)
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return abs
	}
	return resolved
}
