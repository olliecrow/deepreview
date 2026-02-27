package deepreview

import (
	"context"
	"errors"
	"flag"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseReviewArgsYoloAliasOverridesMode(t *testing.T) {
	parsed, err := ParseReviewArgs([]string{"owner/repo", "--source-branch", "feature/test", "--mode", "pr", "--yolo"}, time.Unix(1700000000, 0))
	if err != nil {
		t.Fatalf("ParseReviewArgs failed: %v", err)
	}
	if parsed.Config.Mode != ModeYolo {
		t.Fatalf("expected mode yolo, got %s", parsed.Config.Mode)
	}
}

func TestParseReviewArgsModeIsCaseInsensitive(t *testing.T) {
	parsed, err := ParseReviewArgs([]string{"owner/repo", "--source-branch", "feature/test", "--mode", "YOLO"}, time.Unix(1700000000, 0))
	if err != nil {
		t.Fatalf("ParseReviewArgs failed: %v", err)
	}
	if parsed.Config.Mode != ModeYolo {
		t.Fatalf("expected mode yolo, got %s", parsed.Config.Mode)
	}
}

func TestParseReviewArgsLegacyUppercaseYoloAlias(t *testing.T) {
	parsed, err := ParseReviewArgs([]string{"owner/repo", "--source-branch", "feature/test", "--YOLO"}, time.Unix(1700000000, 0))
	if err != nil {
		t.Fatalf("ParseReviewArgs failed: %v", err)
	}
	if parsed.Config.Mode != ModeYolo {
		t.Fatalf("expected mode yolo, got %s", parsed.Config.Mode)
	}
}

func TestParseReviewArgsDefaults(t *testing.T) {
	parsed, err := ParseReviewArgs([]string{"owner/repo", "--source-branch", "feature"}, time.Unix(1700000000, 0))
	if err != nil {
		t.Fatalf("ParseReviewArgs failed: %v", err)
	}
	if parsed.Config.Concurrency != defaultConcurrency {
		t.Fatalf("expected concurrency %d, got %d", defaultConcurrency, parsed.Config.Concurrency)
	}
	if parsed.Config.MaxRounds != defaultMaxRounds {
		t.Fatalf("expected max rounds %d, got %d", defaultMaxRounds, parsed.Config.MaxRounds)
	}
	if parsed.Config.Mode != ModePR {
		t.Fatalf("expected mode pr, got %s", parsed.Config.Mode)
	}
	if parsed.Config.CodexTimeoutSeconds != defaultCodexTimeoutSeconds {
		t.Fatalf("expected codex timeout %d, got %d", defaultCodexTimeoutSeconds, parsed.Config.CodexTimeoutSeconds)
	}
	if parsed.Config.CodexModel != forcedCodexModel {
		t.Fatalf("expected codex model %s, got %s", forcedCodexModel, parsed.Config.CodexModel)
	}
	if parsed.Config.CodexReasoning != forcedCodexReasoningEffort {
		t.Fatalf("expected codex reasoning %s, got %s", forcedCodexReasoningEffort, parsed.Config.CodexReasoning)
	}
	if parsed.NoTUI {
		t.Fatalf("expected --no-tui default to false")
	}
}

func TestParseReviewArgsHelp(t *testing.T) {
	_, err := ParseReviewArgs([]string{"--help"}, time.Unix(1700000000, 0))
	if !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("expected flag.ErrHelp, got: %v", err)
	}
}

func TestMainHelpTextIncludesCoreSections(t *testing.T) {
	help := MainHelpText()
	for _, want := range []string{
		"deepreview review [<repo>] [--source-branch <branch>]",
		"deepreview doctor [<repo>] [--source-branch <branch>]",
		"deepreview dry-run [<repo>] [--source-branch <branch>]",
		"deepreview completion [bash|zsh]",
		"Commands:",
		"deepreview review --help",
		"deepreview doctor --help",
		"deepreview dry-run --help",
		"deepreview completion --help",
	} {
		if !strings.Contains(help, want) {
			t.Fatalf("main help missing %q", want)
		}
	}
}

func TestReviewHelpTextIncludesDefaultsAndTroubleshooting(t *testing.T) {
	help := ReviewHelpText()
	for _, want := range []string{
		"--concurrency <int>   (default: 4)",
		"--max-rounds <int>    (default: 5)",
		"--mode <pr|yolo>      (default: pr)",
		"DEEPREVIEW_WORKSPACE_ROOT",
		"DEEPREVIEW_CALLER_CWD",
		"If <repo> is omitted:",
		"Troubleshooting:",
		"press Ctrl+C once",
		"Codex model: gpt-5.3-codex",
		"Codex reasoning effort: xhigh",
		"Codex prompt timeout per prompt: 3600s",
	} {
		if !strings.Contains(help, want) {
			t.Fatalf("review help missing %q", want)
		}
	}
}

func TestRunCLIHelpEntrypointsReturnZero(t *testing.T) {
	tests := [][]string{
		{"--help"},
		{"help"},
		{"review", "--help"},
		{"review", "help"},
		{"doctor", "--help"},
		{"doctor", "help"},
		{"dry-run", "--help"},
		{"dry-run", "help"},
		{"completion", "--help"},
		{"completion", "help"},
	}
	for _, tc := range tests {
		if code := RunCLI(tc); code != 0 {
			t.Fatalf("expected exit code 0 for %v, got %d", tc, code)
		}
	}
}

func TestRunCLIUnsupportedCommandShowsRawUserInput(t *testing.T) {
	originalStderr := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe create failed: %v", err)
	}
	os.Stderr = w
	code := RunCLI([]string{"/Users/oc/private-command"})
	_ = w.Close()
	os.Stderr = originalStderr

	outputBytes, readErr := io.ReadAll(r)
	_ = r.Close()
	if readErr != nil {
		t.Fatalf("stderr read failed: %v", readErr)
	}
	output := string(outputBytes)

	if code != 1 {
		t.Fatalf("expected exit code 1 for unsupported command, got %d", code)
	}
	if !strings.Contains(output, "unsupported command: /Users/oc/private-command") {
		t.Fatalf("expected unsupported-command output to keep original token, got:\n%s", output)
	}
}

func TestDoctorHelpTextIncludesCoreSections(t *testing.T) {
	help := DoctorHelpText()
	for _, want := range []string{
		"deepreview doctor",
		"Run non-mutating preflight checks",
		"source branch is reachable on remote",
	} {
		if !strings.Contains(help, want) {
			t.Fatalf("doctor help missing %q", want)
		}
	}
}

func TestDryRunHelpTextIncludesCoreSections(t *testing.T) {
	help := DryRunHelpText()
	for _, want := range []string{
		"deepreview dry-run",
		"planned execution order",
		"does not run Codex",
	} {
		if !strings.Contains(help, want) {
			t.Fatalf("dry-run help missing %q", want)
		}
	}
}

func TestCompletionHelpTextIncludesInstallExamples(t *testing.T) {
	help := CompletionHelpText()
	for _, want := range []string{
		"deepreview completion",
		"Usage:",
		"deepreview completion [bash|zsh]",
		"deepreview completion bash",
		"deepreview completion zsh",
	} {
		if !strings.Contains(help, want) {
			t.Fatalf("completion help missing %q", want)
		}
	}
}

func TestCompletionScriptSupportsBashAndZsh(t *testing.T) {
	bashScript, err := completionScript("bash")
	if err != nil {
		t.Fatalf("unexpected bash completion error: %v", err)
	}
	if !strings.Contains(bashScript, "complete -F _deepreview_completion deepreview") {
		t.Fatalf("expected bash completion directive, got:\n%s", bashScript)
	}

	zshScript, err := completionScript("zsh")
	if err != nil {
		t.Fatalf("unexpected zsh completion error: %v", err)
	}
	if !strings.Contains(zshScript, "#compdef deepreview") {
		t.Fatalf("expected zsh completion header, got:\n%s", zshScript)
	}
}

func TestCompletionScriptRejectsUnsupportedShell(t *testing.T) {
	_, err := completionScript("fish")
	if err == nil {
		t.Fatalf("expected unsupported shell error")
	}
	if !strings.Contains(err.Error(), "unsupported shell") {
		t.Fatalf("expected unsupported shell error, got %v", err)
	}
}

func TestShouldEnableTUI(t *testing.T) {
	if shouldEnableTUI(true, true, true, "xterm-256color", 120, 40, nil) {
		t.Fatalf("expected false when --no-tui is set")
	}
	if shouldEnableTUI(false, false, true, "xterm-256color", 120, 40, nil) {
		t.Fatalf("expected false when stdin is not a terminal")
	}
	if shouldEnableTUI(false, true, false, "xterm-256color", 120, 40, nil) {
		t.Fatalf("expected false when stdout is not a terminal")
	}
	if shouldEnableTUI(false, true, true, "dumb", 120, 40, nil) {
		t.Fatalf("expected false for TERM=dumb")
	}
	if shouldEnableTUI(false, true, true, "xterm-256color", 0, 40, nil) {
		t.Fatalf("expected false for zero width")
	}
	if shouldEnableTUI(false, true, true, "xterm-256color", 120, 0, nil) {
		t.Fatalf("expected false for zero height")
	}
	if shouldEnableTUI(false, true, true, "xterm-256color", 120, 40, errors.New("size unavailable")) {
		t.Fatalf("expected false for terminal size error")
	}
	if !shouldEnableTUI(false, true, true, "xterm-256color", 120, 40, nil) {
		t.Fatalf("expected true when terminal is valid and --no-tui is not set")
	}
}

func TestParseReviewArgsRejectsLegacyTUIFlag(t *testing.T) {
	_, err := ParseReviewArgs([]string{"owner/repo", "--source-branch", "feature/test", "--tui"}, time.Unix(1700000000, 0))
	if err == nil {
		t.Fatalf("expected error for unsupported --tui flag")
	}
	if !strings.Contains(err.Error(), "flag provided but not defined: -tui") {
		t.Fatalf("expected unknown flag error for --tui, got %v", err)
	}
}

func TestParseReviewArgsInfersRepoAndBranchFromCurrentRepo(t *testing.T) {
	repo := createSyncedGitHubLikeRepo(t, "feature/test")
	withWorkingDir(t, repo, func() {
		parsed, err := ParseReviewArgs([]string{}, time.Unix(1700000000, 0))
		if err != nil {
			t.Fatalf("ParseReviewArgs failed: %v", err)
		}
		repoAbs := canonicalPath(t, repo)
		if parsed.Config.Repo != repoAbs {
			t.Fatalf("expected inferred repo %s, got %s", repoAbs, parsed.Config.Repo)
		}
		if parsed.Config.SourceBranch != "feature/test" {
			t.Fatalf("expected inferred source branch feature/test, got %s", parsed.Config.SourceBranch)
		}
	})
}

func TestIsInterruptError(t *testing.T) {
	if !isInterruptError(context.Canceled) {
		t.Fatalf("expected context.Canceled to be treated as interrupt")
	}
	if !isInterruptError(&CommandExecutionError{Canceled: true}) {
		t.Fatalf("expected canceled command error to be treated as interrupt")
	}
	if isInterruptError(errors.New("boom")) {
		t.Fatalf("unexpected interrupt classification for generic error")
	}
}

func TestReadCompletionReviewSnapshotUsesLatestValidRoundStatus(t *testing.T) {
	runRoot := t.TempDir()
	round1 := filepath.Join(runRoot, "round-01")
	round2 := filepath.Join(runRoot, "round-02")
	if err := os.MkdirAll(round1, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(round2, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(round1, "round-status.json"),
		[]byte("{\"decision\":\"continue\",\"reason\":\"keep going\",\"confidence\":0.45}\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(round2, "round-status.json"),
		[]byte("{\"decision\":\"stop\",\"reason\":\"all major issues addressed\",\"confidence\":0.93}\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	snapshot := readCompletionReviewSnapshot(runRoot)
	if snapshot.CompletedRounds != 2 {
		t.Fatalf("expected 2 completed rounds, got %d", snapshot.CompletedRounds)
	}
	if !snapshot.HasFinalStatus {
		t.Fatalf("expected final status to be present")
	}
	if snapshot.FinalStatus.Decision != "stop" {
		t.Fatalf("expected final decision stop, got %s", snapshot.FinalStatus.Decision)
	}
	if snapshot.FinalStatus.Confidence == nil || *snapshot.FinalStatus.Confidence != 0.93 {
		t.Fatalf("expected final confidence 0.93, got %#v", snapshot.FinalStatus.Confidence)
	}
}

func TestReadCompletionReviewSnapshotSkipsInvalidRoundStatus(t *testing.T) {
	runRoot := t.TempDir()
	round1 := filepath.Join(runRoot, "round-01")
	round2 := filepath.Join(runRoot, "round-02")
	if err := os.MkdirAll(round1, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(round2, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(round1, "round-status.json"), []byte("{invalid json}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(round2, "round-status.json"),
		[]byte("{\"decision\":\"stop\",\"reason\":\"done\"}\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	snapshot := readCompletionReviewSnapshot(runRoot)
	if snapshot.CompletedRounds != 2 {
		t.Fatalf("expected 2 completed rounds, got %d", snapshot.CompletedRounds)
	}
	if !snapshot.HasFinalStatus {
		t.Fatalf("expected final status to be present")
	}
	if snapshot.FinalStatus.Decision != "stop" {
		t.Fatalf("expected final decision stop, got %s", snapshot.FinalStatus.Decision)
	}
	if snapshot.FinalStatus.Reason != "done" {
		t.Fatalf("expected final reason done, got %s", snapshot.FinalStatus.Reason)
	}
}
