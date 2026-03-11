package deepreview

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckPushRangeIgnoresDeletedSensitiveLines(t *testing.T) {
	repo := initSecurityScriptRepo(t)
	writeRepoFile(t, repo, "notes.txt", "cleanup "+sensitiveLocalPathFixture()+"\n")
	runCmd(t, repo, nil, "git", "add", "notes.txt")
	runCmd(t, repo, nil, "git", "commit", "-m", "seed sensitive note")
	base := runCmd(t, repo, nil, "git", "rev-parse", "HEAD")

	writeRepoFile(t, repo, "notes.txt", "cleanup /path/to/project\n")
	runCmd(t, repo, nil, "git", "add", "notes.txt")
	runCmd(t, repo, nil, "git", "commit", "-m", "sanitize local path")
	head := runCmd(t, repo, nil, "git", "rev-parse", "HEAD")

	runSecurityScript(t, repo, base, head)
}

func TestCheckPushRangeRejectsAddedSensitiveLines(t *testing.T) {
	repo := initSecurityScriptRepo(t)
	writeRepoFile(t, repo, "notes.txt", "clean\n")
	runCmd(t, repo, nil, "git", "add", "notes.txt")
	runCmd(t, repo, nil, "git", "commit", "-m", "seed clean note")
	base := runCmd(t, repo, nil, "git", "rev-parse", "HEAD")

	writeRepoFile(t, repo, "notes.txt", "cleanup "+sensitiveLocalPathFixture()+"\n")
	runCmd(t, repo, nil, "git", "add", "notes.txt")
	runCmd(t, repo, nil, "git", "commit", "-m", "add sensitive local path")
	head := runCmd(t, repo, nil, "git", "rev-parse", "HEAD")

	output := runSecurityScriptExpectFailure(t, repo, base, head)
	if !strings.Contains(output, "policy violation in push-diff") {
		t.Fatalf("expected push-diff policy violation, got:\n%s", output)
	}
}

func initSecurityScriptRepo(t *testing.T) string {
	t.Helper()
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	runCmd(t, repo, nil, "git", "init")
	runCmd(t, repo, nil, "git", "config", "user.email", "test@example.com")
	runCmd(t, repo, nil, "git", "config", "user.name", "Test User")
	copySecurityScript(t, repo, "scripts/security/check-push-range.sh")
	copySecurityScript(t, repo, "scripts/security/check-sensitive-text.sh")
	return repo
}

func copySecurityScript(t *testing.T, dstRepo, relPath string) {
	t.Helper()
	srcPath := filepath.Join(repoRoot(t), relPath)
	payload, err := os.ReadFile(srcPath)
	if err != nil {
		t.Fatal(err)
	}
	dstPath := filepath.Join(dstRepo, relPath)
	if err := os.MkdirAll(filepath.Dir(dstPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dstPath, payload, 0o755); err != nil {
		t.Fatal(err)
	}
}

func writeRepoFile(t *testing.T, repo, relPath, content string) {
	t.Helper()
	fullPath := filepath.Join(repo, relPath)
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fullPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func sensitiveLocalPathFixture() string {
	root := string([]byte{47, 85, 115, 101, 114, 115})
	parts := []string{root, "alice", "private", "project"}
	return strings.Join(parts, string(os.PathSeparator))
}

func runSecurityScript(t *testing.T, repo, fromRef, toRef string) {
	t.Helper()
	cmd := exec.Command("bash", "scripts/security/check-push-range.sh")
	cmd.Dir = repo
	cmd.Env = append(os.Environ(),
		"PRE_COMMIT_FROM_REF="+fromRef,
		"PRE_COMMIT_TO_REF="+toRef,
	)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("security script failed unexpectedly: %v\nstdout=\n%s\nstderr=\n%s", err, stdout.String(), stderr.String())
	}
}

func runSecurityScriptExpectFailure(t *testing.T, repo, fromRef, toRef string) string {
	t.Helper()
	cmd := exec.Command("bash", "scripts/security/check-push-range.sh")
	cmd.Dir = repo
	cmd.Env = append(os.Environ(),
		"PRE_COMMIT_FROM_REF="+fromRef,
		"PRE_COMMIT_TO_REF="+toRef,
	)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err == nil {
		t.Fatalf("expected security script failure\nstdout=\n%s\nstderr=\n%s", stdout.String(), stderr.String())
	}
	return strings.TrimSpace(stdout.String() + "\n" + stderr.String())
}
