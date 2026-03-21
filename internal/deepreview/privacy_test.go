package deepreview

import (
	"fmt"
	"os"
	"os/exec"
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

func TestSanitizePublicTextRedactsDriveLetterPath(t *testing.T) {
	input := "path " + sensitiveDriveLetterPathFixture()
	out := sanitizePublicText(input)
	if strings.Contains(out, sensitiveDriveLetterPathFixture()) {
		t.Fatalf("expected drive-letter path to be redacted, got: %s", out)
	}
	if !strings.Contains(out, "[redacted-path]") {
		t.Fatalf("expected redacted path marker, got: %s", out)
	}
}

func TestSanitizePublicTextRedactsTestDotComEmail(t *testing.T) {
	out := sanitizePublicText("contact alice@test.com")
	if !strings.Contains(out, "[redacted-email]") {
		t.Fatalf("expected test.com email to be redacted, got: %s", out)
	}
	if strings.Contains(out, "alice@test.com") {
		t.Fatalf("expected test.com email to be removed, got: %s", out)
	}
}

func TestSensitivePatternHelpersCoverExpandedSecretFamilies(t *testing.T) {
	cases := []string{
		buildSensitiveToken("github_pat_"),
		buildSensitiveToken("gho_"),
		buildSensitiveToken("ghu_"),
		buildSensitiveToken("ghs_"),
		buildSensitiveToken("ghr_"),
		buildSensitiveToken("sk-"),
		buildSensitiveToken("sk-admin-"),
		buildSensitiveToken("sk-proj-"),
		fmt.Sprintf(`token="%s"`, testSecretAssignmentValue()),
		fmt.Sprintf("api_key: %s", testSecretAssignmentValue()),
	}

	for _, tc := range cases {
		if !textHasDisallowedSensitivePattern(tc) {
			t.Fatalf("expected sensitive pattern detection for %q", tc)
		}
		if err := assertPublicTextSafe(tc, "test surface"); err == nil {
			t.Fatalf("expected public-text guard to reject %q", tc)
		}
		out := sanitizePublicText(tc)
		if strings.Contains(out, tc) {
			t.Fatalf("expected %q to be redacted, got: %s", tc, out)
		}
		if !strings.Contains(out, "[redacted-secret]") {
			t.Fatalf("expected secret redaction marker for %q, got: %s", tc, out)
		}
	}
}

func TestAssertPublicTextSafeRejectsDisallowedSensitiveContent(t *testing.T) {
	if err := assertPublicTextSafe("contact alice@corp.com", "test surface"); err == nil {
		t.Fatalf("expected disallowed email to be rejected")
	}
	if err := assertPublicTextSafe("contact alice@test.com", "test surface"); err == nil {
		t.Fatalf("expected test.com email to be rejected")
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

func TestAssertPublicTextSafeRejectsDriveLetterPathSyntax(t *testing.T) {
	driveLetterPath := "path " + sensitiveDriveLetterPathFixture()
	if err := assertPublicTextSafe(driveLetterPath, "test surface"); err == nil {
		t.Fatalf("expected drive-letter path syntax to be rejected")
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

func TestDeliveryCommitMessageScanRejectsTestDotComEmail(t *testing.T) {
	o, repoPath, sourceSHA := newPrivacyScanOrchestrator(t)
	repoParent := filepath.Dir(repoPath)

	runGitTest(t, repoParent, "-C", repoPath, "checkout", "-B", "candidate", sourceSHA)
	if err := os.WriteFile(filepath.Join(repoPath, "placeholder.txt"), []byte("value\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, repoParent, "-C", repoPath, "add", "placeholder.txt")
	runGitTest(t, repoParent, "-C", repoPath, "commit", "-m", "notify alice@test.com")

	err := o.deliveryCommitMessageScan("candidate")
	if err == nil {
		t.Fatalf("expected test.com email commit message to fail")
	}
	if !strings.Contains(err.Error(), "commit message") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDeliveryCommitMessageScanRejectsExpandedGitHubToken(t *testing.T) {
	o, repoPath, sourceSHA := newPrivacyScanOrchestrator(t)
	repoParent := filepath.Dir(repoPath)

	runGitTest(t, repoParent, "-C", repoPath, "checkout", "-B", "candidate", sourceSHA)
	if err := os.WriteFile(filepath.Join(repoPath, "placeholder.txt"), []byte("value\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, repoParent, "-C", repoPath, "add", "placeholder.txt")
	runGitTest(t, repoParent, "-C", repoPath, "commit", "-m", "token "+buildSensitiveToken("github_pat_"))

	err := o.deliveryCommitMessageScan("candidate")
	if err == nil {
		t.Fatalf("expected expanded GitHub token commit message to fail")
	}
	if !strings.Contains(err.Error(), "commit message") {
		t.Fatalf("unexpected error: %v", err)
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

func TestSecretHygieneScanRejectsSecretAssignmentInChangedFiles(t *testing.T) {
	o, repoPath, sourceSHA := newPrivacyScanOrchestrator(t)
	repoParent := filepath.Dir(repoPath)

	runGitTest(t, repoParent, "-C", repoPath, "checkout", "-B", "candidate", sourceSHA)
	if err := os.WriteFile(filepath.Join(repoPath, "secret.txt"), []byte(fmt.Sprintf("token = %s\n", testSecretAssignmentValue())), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, repoParent, "-C", repoPath, "add", "secret.txt")
	runGitTest(t, repoParent, "-C", repoPath, "commit", "-m", "add assignment secret")

	err := o.secretHygieneScan(repoPath, "candidate")
	if err == nil {
		t.Fatalf("expected privacy scan to fail for secret assignment")
	}
	if !strings.Contains(err.Error(), "secret-like pattern") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSecretHygieneScanRejectsSecretPatternInAddedBinaryFile(t *testing.T) {
	o, repoPath, sourceSHA := newPrivacyScanOrchestrator(t)
	repoParent := filepath.Dir(repoPath)

	runGitTest(t, repoParent, "-C", repoPath, "checkout", "-B", "candidate", sourceSHA)
	previousSecretPatterns := secretRiskyPatterns
	secretRiskyPatterns = []*regexp.Regexp{regexp.MustCompile(`SECRETTOKEN123`)}
	defer func() {
		secretRiskyPatterns = previousSecretPatterns
	}()

	if err := os.WriteFile(filepath.Join(repoPath, "secret.bin"), []byte("prefix\x00SECRETTOKEN123\x00suffix"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, repoParent, "-C", repoPath, "add", "secret.bin")
	runGitTest(t, repoParent, "-C", repoPath, "commit", "-m", "add binary secret pattern")

	err := o.secretHygieneScan(repoPath, "candidate")
	if err == nil {
		t.Fatalf("expected privacy scan to fail for secret pattern in added binary file")
	}
	if !strings.Contains(err.Error(), "secret-like pattern") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSecretHygieneScanRejectsSecretPatternInModifiedBinaryFile(t *testing.T) {
	o, repoPath, _ := newPrivacyScanOrchestrator(t)
	repoParent := filepath.Dir(repoPath)

	runGitTest(t, repoParent, "-C", repoPath, "checkout", "feature/test")
	binaryPath := filepath.Join(repoPath, "secret.bin")
	if err := os.WriteFile(binaryPath, []byte("prefix\x00safe\x00suffix"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, repoParent, "-C", repoPath, "add", "secret.bin")
	runGitTest(t, repoParent, "-C", repoPath, "commit", "-m", "seed binary")
	sourceSHA := runGitTest(t, repoParent, "-C", repoPath, "rev-parse", "HEAD")
	runGitTest(t, repoParent, "-C", repoPath, "update-ref", "refs/remotes/origin/feature/test", sourceSHA)

	runGitTest(t, repoParent, "-C", repoPath, "checkout", "-B", "candidate", sourceSHA)
	previousSecretPatterns := secretRiskyPatterns
	secretRiskyPatterns = []*regexp.Regexp{regexp.MustCompile(`SECRETTOKEN123`)}
	defer func() {
		secretRiskyPatterns = previousSecretPatterns
	}()

	if err := os.WriteFile(binaryPath, []byte("prefix\x00SECRETTOKEN123\x00suffix"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, repoParent, "-C", repoPath, "add", "secret.bin")
	runGitTest(t, repoParent, "-C", repoPath, "commit", "-m", "modify binary secret pattern")

	err := o.secretHygieneScan(repoPath, "candidate")
	if err == nil {
		t.Fatalf("expected privacy scan to fail for secret pattern in modified binary file")
	}
	if !strings.Contains(err.Error(), "secret-like pattern") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSecretHygieneScanIgnoresPreexistingSecretPatternInModifiedBinaryFile(t *testing.T) {
	o, repoPath, _ := newPrivacyScanOrchestrator(t)
	repoParent := filepath.Dir(repoPath)

	runGitTest(t, repoParent, "-C", repoPath, "checkout", "feature/test")
	binaryPath := filepath.Join(repoPath, "secret.bin")
	if err := os.WriteFile(binaryPath, []byte("prefix\x00SECRETTOKEN123\x00suffix"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, repoParent, "-C", repoPath, "add", "secret.bin")
	runGitTest(t, repoParent, "-C", repoPath, "commit", "-m", "seed binary secret fixture")
	sourceSHA := runGitTest(t, repoParent, "-C", repoPath, "rev-parse", "HEAD")
	runGitTest(t, repoParent, "-C", repoPath, "update-ref", "refs/remotes/origin/feature/test", sourceSHA)

	runGitTest(t, repoParent, "-C", repoPath, "checkout", "-B", "candidate", sourceSHA)
	previousSecretPatterns := secretRiskyPatterns
	secretRiskyPatterns = []*regexp.Regexp{regexp.MustCompile(`SECRETTOKEN123`)}
	defer func() {
		secretRiskyPatterns = previousSecretPatterns
	}()

	if err := os.WriteFile(binaryPath, []byte("prefix\x00SECRETTOKEN123\x00suffix\x00harmless"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, repoParent, "-C", repoPath, "add", "secret.bin")
	runGitTest(t, repoParent, "-C", repoPath, "commit", "-m", "append harmless bytes")

	if err := o.secretHygieneScan(repoPath, "candidate"); err != nil {
		t.Fatalf("expected preexisting binary secret outside changed bytes to be ignored, got: %v", err)
	}
}

func TestSecretHygieneScanRejectsBoundarySpanningLocalPathInModifiedBinaryFile(t *testing.T) {
	o, repoPath, _ := newPrivacyScanOrchestrator(t)
	repoParent := filepath.Dir(repoPath)
	basePath := strings.Join([]string{"", "Users", "alice", "project"}, "/")
	headPath := strings.Join([]string{"", "Users", "bob", "project"}, "/")

	runGitTest(t, repoParent, "-C", repoPath, "checkout", "feature/test")
	binaryPath := filepath.Join(repoPath, "secret.bin")
	if err := os.WriteFile(binaryPath, []byte("prefix\x00"+basePath+"\x00suffix"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, repoParent, "-C", repoPath, "add", "secret.bin")
	runGitTest(t, repoParent, "-C", repoPath, "commit", "-m", "seed binary path fixture")
	sourceSHA := runGitTest(t, repoParent, "-C", repoPath, "rev-parse", "HEAD")
	runGitTest(t, repoParent, "-C", repoPath, "update-ref", "refs/remotes/origin/feature/test", sourceSHA)

	runGitTest(t, repoParent, "-C", repoPath, "checkout", "-B", "candidate", sourceSHA)
	if err := os.WriteFile(binaryPath, []byte("prefix\x00"+headPath+"\x00suffix"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, repoParent, "-C", repoPath, "add", "secret.bin")
	runGitTest(t, repoParent, "-C", repoPath, "commit", "-m", "modify binary local path")

	err := o.secretHygieneScan(repoPath, "candidate")
	if err == nil {
		t.Fatalf("expected privacy scan to fail for boundary-spanning local path in modified binary file")
	}
	if !strings.Contains(err.Error(), "local path pattern") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSecretHygieneScanRejectsBoundarySpanningAKIAPatternInModifiedBinaryFile(t *testing.T) {
	o, repoPath, _ := newPrivacyScanOrchestrator(t)
	repoParent := filepath.Dir(repoPath)
	baseToken := "AKIA" + strings.Repeat("0", 16)
	headToken := "AKIA" + "ABCDEFGHIJKLMNOP"

	runGitTest(t, repoParent, "-C", repoPath, "checkout", "feature/test")
	binaryPath := filepath.Join(repoPath, "secret.bin")
	if err := os.WriteFile(binaryPath, []byte("prefix\x00"+baseToken+"\x00suffix"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, repoParent, "-C", repoPath, "add", "secret.bin")
	runGitTest(t, repoParent, "-C", repoPath, "commit", "-m", "seed binary secret fixture")
	sourceSHA := runGitTest(t, repoParent, "-C", repoPath, "rev-parse", "HEAD")
	runGitTest(t, repoParent, "-C", repoPath, "update-ref", "refs/remotes/origin/feature/test", sourceSHA)

	runGitTest(t, repoParent, "-C", repoPath, "checkout", "-B", "candidate", sourceSHA)
	if err := os.WriteFile(binaryPath, []byte("prefix\x00"+headToken+"\x00suffix"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, repoParent, "-C", repoPath, "add", "secret.bin")
	runGitTest(t, repoParent, "-C", repoPath, "commit", "-m", "modify binary boundary-spanning secret")

	err := o.secretHygieneScan(repoPath, "candidate")
	if err == nil {
		t.Fatalf("expected privacy scan to fail for boundary-spanning AKIA pattern in modified binary file")
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

func TestSecretHygieneScanIgnoresPreexistingSensitiveFixtureOutsideAddedLines(t *testing.T) {
	o, repoPath, _ := newPrivacyScanOrchestrator(t)
	repoParent := filepath.Dir(repoPath)

	runGitTest(t, repoParent, "-C", repoPath, "checkout", "feature/test")
	fixturePath := filepath.Join(repoPath, "fixtures.txt")
	if err := os.WriteFile(fixturePath, []byte("contact alice@corp.com\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, repoParent, "-C", repoPath, "add", "fixtures.txt")
	runGitTest(t, repoParent, "-C", repoPath, "commit", "-m", "seed privacy fixture")
	sourceSHA := runGitTest(t, repoParent, "-C", repoPath, "rev-parse", "HEAD")
	runGitTest(t, repoParent, "-C", repoPath, "update-ref", "refs/remotes/origin/feature/test", sourceSHA)

	runGitTest(t, repoParent, "-C", repoPath, "checkout", "-B", "candidate", sourceSHA)
	if err := os.WriteFile(fixturePath, []byte("contact alice@corp.com\nharmless update\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, repoParent, "-C", repoPath, "add", "fixtures.txt")
	runGitTest(t, repoParent, "-C", repoPath, "commit", "-m", "edit fixture file")

	if err := o.secretHygieneScan(repoPath, "candidate"); err != nil {
		t.Fatalf("expected preexisting sensitive fixture outside added lines to be ignored, got: %v", err)
	}
}

func TestSecretHygieneScanRejectsSensitiveLineAddedToExistingFile(t *testing.T) {
	o, repoPath, _ := newPrivacyScanOrchestrator(t)
	repoParent := filepath.Dir(repoPath)

	runGitTest(t, repoParent, "-C", repoPath, "checkout", "feature/test")
	filePath := filepath.Join(repoPath, "notes.txt")
	if err := os.WriteFile(filePath, []byte("safe baseline\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, repoParent, "-C", repoPath, "add", "notes.txt")
	runGitTest(t, repoParent, "-C", repoPath, "commit", "-m", "seed notes")
	sourceSHA := runGitTest(t, repoParent, "-C", repoPath, "rev-parse", "HEAD")
	runGitTest(t, repoParent, "-C", repoPath, "update-ref", "refs/remotes/origin/feature/test", sourceSHA)

	runGitTest(t, repoParent, "-C", repoPath, "checkout", "-B", "candidate", sourceSHA)
	if err := os.WriteFile(filePath, []byte("safe baseline\ncontact alice@corp.com\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, repoParent, "-C", repoPath, "add", "notes.txt")
	runGitTest(t, repoParent, "-C", repoPath, "commit", "-m", "add sensitive line")

	err := o.secretHygieneScan(repoPath, "candidate")
	if err == nil {
		t.Fatalf("expected privacy scan to fail for added sensitive line in existing file")
	}
	if !strings.Contains(err.Error(), "privacy scan failed") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSecretHygieneScanFailsClosedWhenAddedLinesReadFails(t *testing.T) {
	o, repoPath, sourceSHA := newPrivacyScanOrchestrator(t)
	repoParent := filepath.Dir(repoPath)

	runGitTest(t, repoParent, "-C", repoPath, "checkout", "-B", "candidate", sourceSHA)
	if err := os.WriteFile(filepath.Join(repoPath, "notes.txt"), []byte("safe update\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, repoParent, "-C", repoPath, "add", "notes.txt")
	runGitTest(t, repoParent, "-C", repoPath, "commit", "-m", "add notes")

	o.config.GitBin = buildSelectiveGitFailureWrapper(t, "--unified=0 origin/feature/test..candidate -- notes.txt", "forced diff failure")
	err := o.secretHygieneScan(repoPath, "candidate")
	if err == nil {
		t.Fatalf("expected privacy scan to fail closed on added-lines read failure")
	}
	if !strings.Contains(err.Error(), "unable to read added lines for notes.txt") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSecretHygieneScanFailsClosedWhenBinaryContentReadFails(t *testing.T) {
	o, repoPath, sourceSHA := newPrivacyScanOrchestrator(t)
	repoParent := filepath.Dir(repoPath)

	runGitTest(t, repoParent, "-C", repoPath, "checkout", "-B", "candidate", sourceSHA)
	if err := os.WriteFile(filepath.Join(repoPath, "secret.bin"), []byte("prefix\x00safe\x00suffix"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, repoParent, "-C", repoPath, "add", "secret.bin")
	runGitTest(t, repoParent, "-C", repoPath, "commit", "-m", "add binary file")

	o.config.GitBin = buildSelectiveGitFailureWrapper(t, "show candidate:secret.bin", "forced show failure")
	err := o.secretHygieneScan(repoPath, "candidate")
	if err == nil {
		t.Fatalf("expected privacy scan to fail closed on binary content read failure")
	}
	if !strings.Contains(err.Error(), "unable to read candidate file contents for secret.bin") {
		t.Fatalf("unexpected error: %v", err)
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

func buildSelectiveGitFailureWrapper(t *testing.T, needle, failureText string) string {
	t.Helper()
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatalf("locate git: %v", err)
	}
	wrapperPath := filepath.Join(t.TempDir(), "git-wrapper")
	script := fmt.Sprintf(`#!/bin/sh
set -eu
args="$*"
if printf '%%s' "$args" | grep -F -q -- %q; then
  echo %q >&2
  exit 1
fi
exec %q "$@"
`, needle, failureText, realGit)
	if err := os.WriteFile(wrapperPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write git wrapper: %v", err)
	}
	return wrapperPath
}

func buildSensitiveToken(prefix string) string {
	return prefix + strings.Repeat("a", 26) + "012345"
}

func testSecretAssignmentValue() string {
	return strings.Repeat("a", 16)
}
