package deepreview

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSanitizePublicTextRedactsSensitivePatterns(t *testing.T) {
	input := "email alice@corp.com path /Users/alice/private token ghp_abcdefghijklmnopqrstuvwxyz1234567890 ssn 123-45-6789 phone 415-555-1234 placeholder deepreview@example.com"
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
	if strings.Contains(out, "/Users/alice/private") {
		t.Fatalf("expected local path to be redacted, got: %s", out)
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

	err := o.secretHygieneScan("candidate")
	if err == nil {
		t.Fatalf("expected privacy scan to fail for personal info pattern")
	}
	if !strings.Contains(err.Error(), "privacy scan failed") {
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

	if err := o.secretHygieneScan("candidate"); err != nil {
		t.Fatalf("expected placeholder email to pass scan, got: %v", err)
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
