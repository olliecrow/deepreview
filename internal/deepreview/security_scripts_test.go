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

func TestCheckPushRangeRejectsHistoryOnlySKAdminToken(t *testing.T) {
	repo := initSecurityScriptRepo(t)
	writeRepoFile(t, repo, "notes.txt", "clean\n")
	runCmd(t, repo, nil, "git", "add", "notes.txt")
	runCmd(t, repo, nil, "git", "commit", "-m", "seed clean note")
	base := runCmd(t, repo, nil, "git", "rev-parse", "HEAD")

	writeRepoFile(t, repo, "notes.txt", "token "+buildSensitiveToken("sk-admin-")+"\n")
	runCmd(t, repo, nil, "git", "add", "notes.txt")
	runCmd(t, repo, nil, "git", "commit", "-m", "add admin token")

	writeRepoFile(t, repo, "notes.txt", "clean\n")
	runCmd(t, repo, nil, "git", "add", "notes.txt")
	runCmd(t, repo, nil, "git", "commit", "-m", "remove admin token")
	head := runCmd(t, repo, nil, "git", "rev-parse", "HEAD")

	output := runSecurityScriptExpectFailure(t, repo, base, head)
	if !strings.Contains(output, "policy violation in push-diff") {
		t.Fatalf("expected push-diff policy violation, got:\n%s", output)
	}
}

func TestCheckPushRangeRejectsSensitiveCommitMessage(t *testing.T) {
	repo := initSecurityScriptRepo(t)
	writeRepoFile(t, repo, "notes.txt", "clean\n")
	runCmd(t, repo, nil, "git", "add", "notes.txt")
	runCmd(t, repo, nil, "git", "commit", "-m", "seed clean note")
	base := runCmd(t, repo, nil, "git", "rev-parse", "HEAD")

	writeRepoFile(t, repo, "notes.txt", "updated\n")
	runCmd(t, repo, nil, "git", "add", "notes.txt")
	runCmd(t, repo, nil, "git", "commit", "-m", "commit "+buildSensitiveToken("sk-proj-")+" in message")
	head := runCmd(t, repo, nil, "git", "rev-parse", "HEAD")

	output := runSecurityScriptExpectFailure(t, repo, base, head)
	if !strings.Contains(output, "policy violation in push-commit-message") {
		t.Fatalf("expected push-commit-message policy violation, got:\n%s", output)
	}
}

func TestCheckPushRangeRejectsHistoryOnlyEmail(t *testing.T) {
	repo := initSecurityScriptRepo(t)
	writeRepoFile(t, repo, "notes.txt", "clean\n")
	runCmd(t, repo, nil, "git", "add", "notes.txt")
	runCmd(t, repo, nil, "git", "commit", "-m", "seed clean note")
	base := runCmd(t, repo, nil, "git", "rev-parse", "HEAD")

	writeRepoFile(t, repo, "notes.txt", "contact "+sensitiveEmailFixture()+"\n")
	runCmd(t, repo, nil, "git", "add", "notes.txt")
	runCmd(t, repo, nil, "git", "commit", "-m", "add internal contact")

	writeRepoFile(t, repo, "notes.txt", "contact placeholder@example.com\n")
	runCmd(t, repo, nil, "git", "add", "notes.txt")
	runCmd(t, repo, nil, "git", "commit", "-m", "sanitize contact")
	head := runCmd(t, repo, nil, "git", "rev-parse", "HEAD")

	output := runSecurityScriptExpectFailure(t, repo, base, head)
	if !strings.Contains(output, "policy violation in push-diff") {
		t.Fatalf("expected push-diff policy violation, got:\n%s", output)
	}
}

func TestCheckPushRangeAllowsPlaceholderEmail(t *testing.T) {
	repo := initSecurityScriptRepo(t)
	writeRepoFile(t, repo, "notes.txt", "clean\n")
	runCmd(t, repo, nil, "git", "add", "notes.txt")
	runCmd(t, repo, nil, "git", "commit", "-m", "seed clean note")
	base := runCmd(t, repo, nil, "git", "rev-parse", "HEAD")

	writeRepoFile(t, repo, "notes.txt", "contact placeholder@example.com\n")
	runCmd(t, repo, nil, "git", "add", "notes.txt")
	runCmd(t, repo, nil, "git", "commit", "-m", "add placeholder contact")
	head := runCmd(t, repo, nil, "git", "rev-parse", "HEAD")

	runSecurityScript(t, repo, base, head)
}

func TestCheckPushRangeRejectsSensitiveFilename(t *testing.T) {
	repo := initSecurityScriptRepo(t)
	writeRepoFile(t, repo, "clean.txt", "clean\n")
	runCmd(t, repo, nil, "git", "add", "clean.txt")
	runCmd(t, repo, nil, "git", "commit", "-m", "seed clean note")
	base := runCmd(t, repo, nil, "git", "rev-parse", "HEAD")

	runCmd(t, repo, nil, "git", "mv", "clean.txt", sensitiveEmailFixture()+".txt")
	runCmd(t, repo, nil, "git", "commit", "-m", "rename to sensitive filename")
	head := runCmd(t, repo, nil, "git", "rev-parse", "HEAD")

	output := runSecurityScriptExpectFailure(t, repo, base, head)
	if !strings.Contains(output, "policy violation in push-paths") {
		t.Fatalf("expected push-paths policy violation, got:\n%s", output)
	}
}

func TestCheckPushRangeRejectsSensitiveRenameTarget(t *testing.T) {
	repo := initSecurityScriptRepo(t)
	writeRepoFile(t, repo, "clean.txt", "clean\n")
	runCmd(t, repo, nil, "git", "add", "clean.txt")
	runCmd(t, repo, nil, "git", "commit", "-m", "seed clean note")
	base := runCmd(t, repo, nil, "git", "rev-parse", "HEAD")

	runCmd(t, repo, nil, "git", "mv", "clean.txt", sensitiveDriveLetterPathFixture())
	runCmd(t, repo, nil, "git", "commit", "-m", "rename to sensitive path")
	head := runCmd(t, repo, nil, "git", "rev-parse", "HEAD")

	output := runSecurityScriptExpectFailure(t, repo, base, head)
	if !strings.Contains(output, "policy violation in push-paths") {
		t.Fatalf("expected push-paths policy violation, got:\n%s", output)
	}
}

func TestCheckSensitiveTextRejectsMixedPlaceholderAndSensitiveEmailOnSameLine(t *testing.T) {
	repo := initSecurityScriptRepo(t)
	target := writeRepoFile(t, repo, "mixed.txt", "contacts placeholder@example.com "+sensitiveEmailFixture()+"\n")

	output := runSensitiveTextScriptExpectFailure(t, repo, target)
	if !strings.Contains(output, "policy violation in direct-text") {
		t.Fatalf("expected direct-text policy violation, got:\n%s", output)
	}
	if !strings.Contains(output, sensitiveEmailFixture()) {
		t.Fatalf("expected disallowed email to remain visible in output, got:\n%s", output)
	}
}

func TestCheckSensitiveTextRejectsMixedPlaceholderAndSensitivePathOnSameLine(t *testing.T) {
	repo := initSecurityScriptRepo(t)
	target := writeRepoFile(t, repo, "mixed.txt", "paths /Users/USER/project "+sensitiveLocalPathFixture()+"\n")

	output := runSensitiveTextScriptExpectFailure(t, repo, target)
	if !strings.Contains(output, "policy violation in direct-text") {
		t.Fatalf("expected direct-text policy violation, got:\n%s", output)
	}
	if !strings.Contains(output, sensitiveLocalPathUserPrefix()) {
		t.Fatalf("expected sensitive path token to remain visible in output, got:\n%s", output)
	}
}

func TestCheckPushRangeRejectsHistoryOnlyMixedPlaceholderEmail(t *testing.T) {
	repo := initSecurityScriptRepo(t)
	writeRepoFile(t, repo, "notes.txt", "clean\n")
	runCmd(t, repo, nil, "git", "add", "notes.txt")
	runCmd(t, repo, nil, "git", "commit", "-m", "seed clean note")
	base := runCmd(t, repo, nil, "git", "rev-parse", "HEAD")

	writeRepoFile(t, repo, "notes.txt", "contacts placeholder@example.com "+sensitiveEmailFixture()+"\n")
	runCmd(t, repo, nil, "git", "add", "notes.txt")
	runCmd(t, repo, nil, "git", "commit", "-m", "add mixed contact")

	writeRepoFile(t, repo, "notes.txt", "contacts placeholder@example.com\n")
	runCmd(t, repo, nil, "git", "add", "notes.txt")
	runCmd(t, repo, nil, "git", "commit", "-m", "sanitize mixed contact")
	head := runCmd(t, repo, nil, "git", "rev-parse", "HEAD")

	output := runSecurityScriptExpectFailure(t, repo, base, head)
	if !strings.Contains(output, "policy violation in push-diff") {
		t.Fatalf("expected push-diff policy violation, got:\n%s", output)
	}
}

func TestCheckPushRangeRejectsHistoryOnlyMixedPlaceholderPath(t *testing.T) {
	repo := initSecurityScriptRepo(t)
	writeRepoFile(t, repo, "notes.txt", "clean\n")
	runCmd(t, repo, nil, "git", "add", "notes.txt")
	runCmd(t, repo, nil, "git", "commit", "-m", "seed clean note")
	base := runCmd(t, repo, nil, "git", "rev-parse", "HEAD")

	writeRepoFile(t, repo, "notes.txt", "paths /Users/USER/project "+sensitiveLocalPathFixture()+"\n")
	runCmd(t, repo, nil, "git", "add", "notes.txt")
	runCmd(t, repo, nil, "git", "commit", "-m", "add mixed path")

	writeRepoFile(t, repo, "notes.txt", "paths /Users/USER/project\n")
	runCmd(t, repo, nil, "git", "add", "notes.txt")
	runCmd(t, repo, nil, "git", "commit", "-m", "sanitize mixed path")
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

func writeRepoFile(t *testing.T, repo, relPath, content string) string {
	t.Helper()
	fullPath := filepath.Join(repo, relPath)
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fullPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return fullPath
}

func sensitiveLocalPathFixture() string {
	root := string([]byte{47, 85, 115, 101, 114, 115})
	parts := []string{root, "alice", "private", "project"}
	return strings.Join(parts, string(os.PathSeparator))
}

func sensitiveLocalPathUserPrefix() string {
	return strings.Join([]string{"", "Users", "alice"}, string(os.PathSeparator))
}

func sensitiveEmailFixture() string {
	return strings.Join([]string{"alice", "corp.com"}, "@")
}

func sensitiveDriveLetterPathFixture() string {
	return strings.Join([]string{"C:", "Users", "alice", "private.txt"}, `\`)
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

func runSensitiveTextScriptExpectFailure(t *testing.T, repo, target string) string {
	t.Helper()
	cmd := exec.Command("bash", "scripts/security/check-sensitive-text.sh", "--context=direct-text", target)
	cmd.Dir = repo
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err == nil {
		t.Fatalf("expected sensitive-text script failure\nstdout=\n%s\nstderr=\n%s", stdout.String(), stderr.String())
	}
	return strings.TrimSpace(stdout.String() + "\n" + stderr.String())
}
