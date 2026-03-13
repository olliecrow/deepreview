package deepreview

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestMain(m *testing.M) {
	_ = os.Unsetenv(deepreviewCallerCWDEnv)
	os.Exit(m.Run())
}

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
	remote := filepath.Join(td, "remote.git")
	seed := filepath.Join(td, "seed")
	repo := filepath.Join(td, "repo")
	if err := os.MkdirAll(filepath.Dir(remote), 0o755); err != nil {
		t.Fatal(err)
	}
	githubURL := githubSCPLikeCloneURL("example-org", "example-repo")
	configureGitHubURLRewrite(t, githubURL, remote)

	runGitCommand(t, td, "init", "--bare", remote)
	runGitCommand(t, td, "clone", githubURL, seed)
	runGitCommand(t, td, "-C", seed, "config", "user.email", testPlaceholderEmail("test"))
	runGitCommand(t, td, "-C", seed, "config", "user.name", "Test User")
	runGitCommand(t, td, "-C", seed, "checkout", "-b", branch)
	if err := os.WriteFile(filepath.Join(seed, "README.md"), []byte("seed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitCommand(t, td, "-C", seed, "add", "README.md")
	runGitCommand(t, td, "-C", seed, "commit", "-m", "seed")
	runGitCommand(t, td, "-C", seed, "push", "-u", "origin", branch)

	runGitCommand(t, td, "clone", githubURL, repo)
	runGitCommand(t, td, "-C", repo, "config", "user.email", testPlaceholderEmail("test"))
	runGitCommand(t, td, "-C", repo, "config", "user.name", "Test User")
	runGitCommand(t, td, "-C", repo, "checkout", branch)
	return repo
}

func createSyncedFilesystemRepo(t *testing.T, branch string) string {
	t.Helper()
	td := t.TempDir()
	remote := filepath.Join(td, "remote.git")
	seed := filepath.Join(td, "seed")
	repo := filepath.Join(td, "repo")
	if err := os.MkdirAll(filepath.Dir(remote), 0o755); err != nil {
		t.Fatal(err)
	}

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
	runGitCommand(t, td, "-C", repo, "config", "user.email", testPlaceholderEmail("test"))
	runGitCommand(t, td, "-C", repo, "config", "user.name", "Test User")
	runGitCommand(t, td, "-C", repo, "checkout", branch)
	return repo
}

func githubSCPLikeCloneURL(owner, name string) string {
	return fmt.Sprintf("git%s%s:%s/%s.git", "@", "github.com", owner, name)
}

func githubSSHCloneURL(owner, name string) string {
	return fmt.Sprintf("ssh://git%s%s/%s/%s.git", "@", "github.com", owner, name)
}

func testPlaceholderEmail(localPart string) string {
	return localPart + "@" + "example.com"
}

func configureGitHubURLRewrite(t *testing.T, githubURL, localPath string) {
	t.Helper()
	globalConfig := os.Getenv("GIT_CONFIG_GLOBAL")
	if globalConfig == "" {
		globalConfig = filepath.Join(t.TempDir(), "global.gitconfig")
		t.Setenv("GIT_CONFIG_GLOBAL", globalConfig)
	}
	if err := os.MkdirAll(filepath.Dir(globalConfig), 0o755); err != nil {
		t.Fatal(err)
	}
	runGitCommand(t, filepath.Dir(globalConfig), "config", "--global", "url."+localPath+".insteadOf", githubURL)
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
