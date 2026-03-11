package deepreview

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestSanitizePublicTextRedactsSensitivePatterns(t *testing.T) {
	input := "email alice@corp.com path /Users/YOU/private key -----BEGIN PRIVATE KEY----- ssn 123-45-6789 phone 415-555-1234 placeholder deepreview@example.com"
	out := sanitizePublicText(input)

	expectContains := []string{
		"[redacted-email]",
		"[redacted-path]",
		"[redacted-secret]",
		"[redacted-personal]",
		"deepreview@example.com",
	}
	for _, token := range expectContains {
		if !strings.Contains(out, token) {
			t.Fatalf("expected redacted output to contain %q, got: %s", token, out)
		}
	}
	if strings.Contains(out, "alice@corp.com") {
		t.Fatalf("expected disallowed email to be redacted, got: %s", out)
	}
	if strings.Contains(out, "/Users/YOU/private") {
		t.Fatalf("expected local path to be redacted, got: %s", out)
	}
}

func TestSanitizePublicTextRedactsGhBodyFilePath(t *testing.T) {
	input := "command failed: gh pr create --repo owner/repo --body-file /Users/YOU/deepreview/runs/run-123/pr-body.md"
	out := sanitizePublicText(input)
	if strings.Contains(out, "/Users/YOU/deepreview/runs/run-123/pr-body.md") {
		t.Fatalf("expected gh --body-file local path to be redacted, got: %s", out)
	}
	if !strings.Contains(out, "--body-file [redacted-path]") {
		t.Fatalf("expected redacted body-file path marker, got: %s", out)
	}
}

func TestAssertPublicTextSafeRejectsDisallowedSensitiveContent(t *testing.T) {
	if err := assertPublicTextSafe("contact alice@corp.com", "test surface"); err == nil {
		t.Fatalf("expected disallowed email to be rejected")
	}
	if err := assertPublicTextSafe("placeholder deepreview@example.com", "test surface"); err != nil {
		t.Fatalf("expected placeholder email to be allowed, got: %v", err)
	}
}

func TestAssertPublicTextSafeAllowsShellPathExpansionFragments(t *testing.T) {
	text := `export PATH="\${VIRTUAL_ENV}/bin:\${PATH}"`
	if err := assertPublicTextSafe(text, "test surface"); err != nil {
		t.Fatalf("expected shell path expansion fragment to pass, got: %v", err)
	}
}

func TestAssertPublicTextSafeIgnoresUnsupportedDriveLetterPathSyntax(t *testing.T) {
	driveLetterPath := "path " + "C:" + `\` + `Users\alice\secret.txt`
	if err := assertPublicTextSafe(driveLetterPath, "test surface"); err != nil {
		t.Fatalf("expected unsupported drive-letter path syntax to be ignored, got: %v", err)
	}
}

func TestDeliveryCommitMessageScanRejectsSensitiveContent(t *testing.T) {
	o, repoPath, sourceSHA := newPrivacyScanOrchestrator(t)
	repoParent := filepath.Dir(repoPath)

	runGitTest(t, repoParent, "-C", repoPath, "checkout", "-B", "candidate", sourceSHA)
	if err := os.WriteFile(filepath.Join(repoPath, "sensitive.txt"), []byte("value\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, repoParent, "-C", repoPath, "add", "sensitive.txt")
	runGitTest(t, repoParent, "-C", repoPath, "commit", "-m", "contact alice@corp.com")

	err := o.deliveryCommitMessageScan("candidate")
	if err == nil {
		t.Fatalf("expected commit message privacy scan to fail")
	}
	if !strings.Contains(err.Error(), "commit message") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDeliveryCommitMessageScanAllowsPlaceholderEmail(t *testing.T) {
	o, repoPath, sourceSHA := newPrivacyScanOrchestrator(t)
	repoParent := filepath.Dir(repoPath)

	runGitTest(t, repoParent, "-C", repoPath, "checkout", "-B", "candidate", sourceSHA)
	if err := os.WriteFile(filepath.Join(repoPath, "placeholder.txt"), []byte("value\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, repoParent, "-C", repoPath, "add", "placeholder.txt")
	runGitTest(t, repoParent, "-C", repoPath, "commit", "-m", "notify deepreview@example.com")

	if err := o.deliveryCommitMessageScan("candidate"); err != nil {
		t.Fatalf("expected placeholder email commit message to pass, got: %v", err)
	}
}

func TestSecretHygieneScanRejectsPersonalInfoInChangedFiles(t *testing.T) {
	o, repoPath, sourceSHA := newPrivacyScanOrchestrator(t)
	repoParent := filepath.Dir(repoPath)

	runGitTest(t, repoParent, "-C", repoPath, "checkout", "-B", "candidate", sourceSHA)
	if err := os.WriteFile(filepath.Join(repoPath, "data.txt"), []byte("call 415-555-1234\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, repoParent, "-C", repoPath, "add", "data.txt")
	runGitTest(t, repoParent, "-C", repoPath, "commit", "-m", "add data")

	err := o.secretHygieneScan(repoPath, "candidate")
	if err == nil {
		t.Fatalf("expected privacy scan to fail for personal info pattern")
	}
	if !strings.Contains(err.Error(), "privacy scan failed") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSecretHygieneScanRejectsSecretPatternInChangedFiles(t *testing.T) {
	o, repoPath, sourceSHA := newPrivacyScanOrchestrator(t)
	repoParent := filepath.Dir(repoPath)

	runGitTest(t, repoParent, "-C", repoPath, "checkout", "-B", "candidate", sourceSHA)
	previousSecretPatterns := secretRiskyPatterns
	secretRiskyPatterns = []*regexp.Regexp{regexp.MustCompile(`SECRETTOKEN123`)}
	defer func() {
		secretRiskyPatterns = previousSecretPatterns
	}()

	if err := os.WriteFile(filepath.Join(repoPath, "secret.txt"), []byte("key SECRETTOKEN123\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, repoParent, "-C", repoPath, "add", "secret.txt")
	runGitTest(t, repoParent, "-C", repoPath, "commit", "-m", "add secret pattern")

	err := o.secretHygieneScan(repoPath, "candidate")
	if err == nil {
		t.Fatalf("expected privacy scan to fail for secret pattern")
	}
	if !strings.Contains(err.Error(), "secret-like pattern") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSecretHygieneScanAllowsPlaceholderEmailInChangedFiles(t *testing.T) {
	o, repoPath, sourceSHA := newPrivacyScanOrchestrator(t)
	repoParent := filepath.Dir(repoPath)

	runGitTest(t, repoParent, "-C", repoPath, "checkout", "-B", "candidate", sourceSHA)
	if err := os.WriteFile(filepath.Join(repoPath, "docs.txt"), []byte("contact deepreview@example.com\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, repoParent, "-C", repoPath, "add", "docs.txt")
	runGitTest(t, repoParent, "-C", repoPath, "commit", "-m", "docs")

	if err := o.secretHygieneScan(repoPath, "candidate"); err != nil {
		t.Fatalf("expected placeholder email to pass scan, got: %v", err)
	}
}

func TestSummarizePrivacyScanIssues(t *testing.T) {
	commitErr := NewDeepReviewError("privacy scan failed: disallowed sensitive content detected in delivery commit message")
	fileErr := NewDeepReviewError("privacy scan failed: local path pattern matched in docs/example.md")
	summary := summarizePrivacyScanIssues(commitErr, fileErr)
	if !strings.Contains(summary, "commit-message scan:") {
		t.Fatalf("expected commit message scan section, got: %s", summary)
	}
	if !strings.Contains(summary, "changed-file scan:") {
		t.Fatalf("expected changed-file scan section, got: %s", summary)
	}
}

func TestTryAutoRemediateLocalPathPrivacyViolationInDocs(t *testing.T) {
	o, repoPath, sourceSHA := newPrivacyScanOrchestrator(t)
	repoParent := filepath.Dir(repoPath)

	if err := os.MkdirAll(filepath.Join(repoPath, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, repoParent, "-C", repoPath, "checkout", "-B", "candidate", sourceSHA)
	docPath := filepath.Join(repoPath, "docs", "decision_capture.md")
	if err := os.WriteFile(docPath, []byte("ref LOCALPATHTOKEN\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, repoParent, "-C", repoPath, "add", "docs/decision_capture.md")
	runGitTest(t, repoParent, "-C", repoPath, "commit", "-m", "add decision note")

	previousPatterns := privatePathPatterns
	privatePathPatterns = []*regexp.Regexp{regexp.MustCompile(`LOCALPATHTOKEN`)}
	defer func() {
		privatePathPatterns = previousPatterns
	}()

	remediated, remediationErr := o.tryAutoRemediateLocalPathPrivacyViolation(
		repoPath,
		"candidate",
		NewDeepReviewError("privacy scan failed: local path pattern matched in docs/decision_capture.md"),
	)
	if remediationErr != nil {
		t.Fatalf("auto-remediation failed: %v", remediationErr)
	}
	if !remediated {
		t.Fatalf("expected docs local-path violation to be auto-remediated")
	}
	if err := o.secretHygieneScan(repoPath, "candidate"); err != nil {
		t.Fatalf("expected privacy scan to pass after remediation, got: %v", err)
	}

	content, readErr := os.ReadFile(docPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	text := string(content)
	if strings.Contains(text, "LOCALPATHTOKEN") {
		t.Fatalf("expected local path token to be removed after remediation, got: %s", text)
	}
	if !strings.Contains(text, "/path/to/project") {
		t.Fatalf("expected placeholder path after remediation, got: %s", text)
	}

	lastMessage := runGitTest(t, repoParent, "-C", repoPath, "log", "-1", "--pretty=%s")
	if lastMessage != "deepreview: sanitize local paths for privacy scan" {
		t.Fatalf("unexpected remediation commit message: %q", lastMessage)
	}
}

func newPrivacyScanOrchestrator(t *testing.T) (*Orchestrator, string, string) {
	t.Helper()
	td := t.TempDir()
	repoPath := filepath.Join(td, "repo")
	if err := os.MkdirAll(repoPath, 0o755); err != nil {
		t.Fatal(err)
	}

	runGitTest(t, td, "init", repoPath)
	runGitTest(t, td, "-C", repoPath, "config", "user.email", "test@example.com")
	runGitTest(t, td, "-C", repoPath, "config", "user.name", "Test User")
	runGitTest(t, td, "-C", repoPath, "checkout", "-b", "main")
	if err := os.WriteFile(filepath.Join(repoPath, "README.md"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, td, "-C", repoPath, "add", "README.md")
	runGitTest(t, td, "-C", repoPath, "commit", "-m", "base")

	runGitTest(t, td, "-C", repoPath, "checkout", "-b", "feature/test")
	if err := os.WriteFile(filepath.Join(repoPath, "feature.txt"), []byte("feature\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, td, "-C", repoPath, "add", "feature.txt")
	runGitTest(t, td, "-C", repoPath, "commit", "-m", "feature")
	sourceSHA := runGitTest(t, td, "-C", repoPath, "rev-parse", "HEAD")
	runGitTest(t, td, "-C", repoPath, "update-ref", "refs/remotes/origin/feature/test", sourceSHA)

	o := &Orchestrator{
		managedRepoPath: repoPath,
		config: ReviewConfig{
			SourceBranch: "feature/test",
			GitBin:       "git",
		},
	}
	return o, repoPath, sourceSHA
}
