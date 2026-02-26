package deepreview

import (
	"errors"
	"flag"
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
	if parsed.TUI {
		t.Fatalf("expected --tui default to false")
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
		"Commands:",
		"deepreview review --help",
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
		"If <repo> is omitted:",
		"Troubleshooting:",
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
	}
	for _, tc := range tests {
		if code := RunCLI(tc); code != 0 {
			t.Fatalf("expected exit code 0 for %v, got %d", tc, code)
		}
	}
}

func TestShouldEnableTUI(t *testing.T) {
	if shouldEnableTUI(true, true, true, true, "xterm-256color", 120, 40, nil) {
		t.Fatalf("expected false when --no-tui is set")
	}
	if shouldEnableTUI(false, false, true, true, "xterm-256color", 120, 40, nil) {
		t.Fatalf("expected false when --tui is not requested")
	}
	if shouldEnableTUI(true, false, false, true, "xterm-256color", 120, 40, nil) {
		t.Fatalf("expected false when stdin is not a terminal")
	}
	if shouldEnableTUI(true, false, true, false, "xterm-256color", 120, 40, nil) {
		t.Fatalf("expected false when stdout is not a terminal")
	}
	if shouldEnableTUI(true, false, true, true, "dumb", 120, 40, nil) {
		t.Fatalf("expected false for TERM=dumb")
	}
	if shouldEnableTUI(true, false, true, true, "xterm-256color", 0, 40, nil) {
		t.Fatalf("expected false for zero width")
	}
	if shouldEnableTUI(true, false, true, true, "xterm-256color", 120, 0, nil) {
		t.Fatalf("expected false for zero height")
	}
	if shouldEnableTUI(true, false, true, true, "xterm-256color", 120, 40, errors.New("size unavailable")) {
		t.Fatalf("expected false for terminal size error")
	}
	if !shouldEnableTUI(true, false, true, true, "xterm-256color", 120, 40, nil) {
		t.Fatalf("expected true when --tui is requested and terminal is valid")
	}
}

func TestParseReviewArgsRejectsConflictingTUIFlags(t *testing.T) {
	_, err := ParseReviewArgs([]string{"owner/repo", "--source-branch", "feature/test", "--tui", "--no-tui"}, time.Unix(1700000000, 0))
	if err == nil {
		t.Fatalf("expected conflict error for --tui + --no-tui")
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
