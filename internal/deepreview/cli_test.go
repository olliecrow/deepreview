package deepreview

import (
	"context"
	"errors"
	"flag"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func captureCommandOutput(t *testing.T, fn func() int) (int, string, string) {
	t.Helper()

	stdoutReader, stdoutWriter, err := os.Pipe()
	if err != nil {
		t.Fatalf("stdout pipe create failed: %v", err)
	}
	stderrReader, stderrWriter, err := os.Pipe()
	if err != nil {
		t.Fatalf("stderr pipe create failed: %v", err)
	}

	originalStdout := os.Stdout
	originalStderr := os.Stderr
	os.Stdout = stdoutWriter
	os.Stderr = stderrWriter

	code := fn()

	_ = stdoutWriter.Close()
	_ = stderrWriter.Close()
	os.Stdout = originalStdout
	os.Stderr = originalStderr

	stdoutBytes, stdoutErr := io.ReadAll(stdoutReader)
	_ = stdoutReader.Close()
	if stdoutErr != nil {
		t.Fatalf("stdout read failed: %v", stdoutErr)
	}
	stderrBytes, stderrErr := io.ReadAll(stderrReader)
	_ = stderrReader.Close()
	if stderrErr != nil {
		t.Fatalf("stderr read failed: %v", stderrErr)
	}

	return code, string(stdoutBytes), string(stderrBytes)
}

func writeFakeTool(t *testing.T, dir, name string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	script := "#!/bin/sh\nexit 0\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("failed to write fake tool %s: %v", name, err)
	}
	return path
}

func writeFakeMulticodexTool(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "multicodex")
	script := `#!/bin/sh
case "$1" in
  status)
    printf '%s\n' 'profile: logged-in'
    exit 0
    ;;
  exec)
    exit 0
    ;;
esac
exit 0
`
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("failed to write fake multicodex: %v", err)
	}
	return path
}

func writePromptsRootWithoutDeliveryTemplate(t *testing.T) string {
	t.Helper()

	root := t.TempDir()
	sourceRoot := filepath.Join(repoRoot(t), "prompts")
	for _, rel := range []string{
		filepath.Join("review", "independent-review.md"),
		filepath.Join("execute", "queue.txt"),
		filepath.Join("execute", "01-triage-plan.md"),
		filepath.Join("execute", "02-implement-verify-finalize.md"),
	} {
		payload, err := os.ReadFile(filepath.Join(sourceRoot, rel))
		if err != nil {
			t.Fatal(err)
		}
		dst := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(dst, payload, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	return root
}

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
	if parsed.Config.ReviewInactivitySec != defaultReviewInactivitySec {
		t.Fatalf("expected review inactivity seconds %d, got %d", defaultReviewInactivitySec, parsed.Config.ReviewInactivitySec)
	}
	if parsed.Config.ReviewActivityPollS != defaultReviewActivityPollS {
		t.Fatalf("expected review activity poll seconds %d, got %d", defaultReviewActivityPollS, parsed.Config.ReviewActivityPollS)
	}
	if parsed.Config.ReviewMaxRestarts != defaultReviewMaxRestarts {
		t.Fatalf("expected review max restarts %d, got %d", defaultReviewMaxRestarts, parsed.Config.ReviewMaxRestarts)
	}
	if parsed.NoTUI {
		t.Fatalf("expected --no-tui default to false")
	}
	if parsed.Config.TrustRepo {
		t.Fatalf("expected --trust-repo default to false")
	}
	if parsed.Config.RetainRawLogs {
		t.Fatalf("expected --retain-raw-logs default to false")
	}
}

func TestParseReviewArgsTrustRepo(t *testing.T) {
	parsed, err := ParseReviewArgs([]string{"owner/repo", "--source-branch", "feature", "--trust-repo"}, time.Unix(1700000000, 0))
	if err != nil {
		t.Fatalf("ParseReviewArgs failed: %v", err)
	}
	if !parsed.Config.TrustRepo {
		t.Fatal("expected --trust-repo to set repository execution trust")
	}
}

func TestParseReviewArgsRetainRawLogs(t *testing.T) {
	parsed, err := ParseReviewArgs([]string{"owner/repo", "--source-branch", "feature", "--retain-raw-logs"}, time.Unix(1700000000, 0))
	if err != nil {
		t.Fatalf("ParseReviewArgs failed: %v", err)
	}
	if !parsed.Config.RetainRawLogs {
		t.Fatal("expected --retain-raw-logs to keep raw Codex logs")
	}
}

func TestParseReviewArgsReviewPolicyEnvOverrides(t *testing.T) {
	t.Setenv("DEEPREVIEW_REVIEW_INACTIVITY_SECONDS", "75")
	t.Setenv("DEEPREVIEW_REVIEW_ACTIVITY_POLL_SECONDS", "7")
	t.Setenv("DEEPREVIEW_REVIEW_MAX_RESTARTS", "3")
	parsed, err := ParseReviewArgs([]string{"owner/repo", "--source-branch", "feature"}, time.Unix(1700000000, 0))
	if err != nil {
		t.Fatalf("ParseReviewArgs failed: %v", err)
	}
	if parsed.Config.ReviewInactivitySec != 75 {
		t.Fatalf("expected review inactivity seconds 75, got %d", parsed.Config.ReviewInactivitySec)
	}
	if parsed.Config.ReviewActivityPollS != 7 {
		t.Fatalf("expected review activity poll seconds 7, got %d", parsed.Config.ReviewActivityPollS)
	}
	if parsed.Config.ReviewMaxRestarts != 3 {
		t.Fatalf("expected review max restarts 3, got %d", parsed.Config.ReviewMaxRestarts)
	}
	if parsed.Config.ReviewInactivity != 75*time.Second {
		t.Fatalf("expected review inactivity 75s, got %s", parsed.Config.ReviewInactivity)
	}
	if parsed.Config.ReviewActivityPoll != 7*time.Second {
		t.Fatalf("expected review activity poll 7s, got %s", parsed.Config.ReviewActivityPoll)
	}
}

func TestParseReviewArgsRejectsInvalidReviewPolicyEnv(t *testing.T) {
	t.Setenv("DEEPREVIEW_REVIEW_INACTIVITY_SECONDS", "-1")
	_, err := ParseReviewArgs([]string{"owner/repo", "--source-branch", "feature"}, time.Unix(1700000000, 0))
	if err == nil {
		t.Fatalf("expected parse error for invalid review inactivity env")
	}
	if !strings.Contains(err.Error(), "DEEPREVIEW_REVIEW_INACTIVITY_SECONDS") {
		t.Fatalf("expected review inactivity env in error, got: %v", err)
	}
}

func TestParseReviewArgsRejectsUnboundedCountsAndDurations(t *testing.T) {
	now := time.Unix(1700000000, 0)
	cases := []struct {
		name string
		args []string
		want string
	}{
		{name: "concurrency zero", args: []string{"--concurrency", "0", "--max-rounds", "1"}, want: "--concurrency"},
		{name: "concurrency negative", args: []string{"--concurrency", "-1", "--max-rounds", "1"}, want: "--concurrency"},
		{name: "concurrency above ceiling", args: []string{"--concurrency", strconv.Itoa(maxConcurrency + 1), "--max-rounds", "1"}, want: "--concurrency"},
		{name: "concurrency max int", args: []string{"--concurrency", strconv.Itoa(math.MaxInt), "--max-rounds", "1"}, want: "--concurrency"},
		{name: "rounds zero", args: []string{"--concurrency", "1", "--max-rounds", "0"}, want: "--max-rounds"},
		{name: "rounds negative", args: []string{"--concurrency", "1", "--max-rounds", "-1"}, want: "--max-rounds"},
		{name: "rounds above ceiling", args: []string{"--concurrency", "1", "--max-rounds", strconv.Itoa(maxReviewRounds + 1)}, want: "--max-rounds"},
		{name: "rounds max int", args: []string{"--concurrency", "1", "--max-rounds", strconv.Itoa(math.MaxInt)}, want: "--max-rounds"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseReviewArgs(tc.args, now)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected error containing %q, got %v", tc.want, err)
			}
		})
	}

	overflow := strconv.FormatInt(int64(math.MaxInt64)/int64(time.Second)+1, 10)
	t.Setenv("DEEPREVIEW_REVIEW_INACTIVITY_SECONDS", overflow)
	_, err := ParseReviewArgs([]string{"owner/repo", "--source-branch", "feature", "--concurrency", "1", "--max-rounds", "1"}, now)
	if err == nil || !strings.Contains(err.Error(), "overflows a duration") {
		t.Fatalf("expected duration overflow, got %v", err)
	}
}

func TestParseReviewArgsAcceptsLimitBoundaries(t *testing.T) {
	now := time.Unix(1700000000, 0)
	parsed, err := ParseReviewArgs([]string{
		"owner/repo",
		"--source-branch", "feature",
		"--concurrency", strconv.Itoa(maxConcurrency),
		"--max-rounds", strconv.Itoa(maxReviewRounds),
	}, now)
	if err != nil {
		t.Fatalf("ParseReviewArgs failed: %v", err)
	}
	if parsed.Config.Concurrency != maxConcurrency || parsed.Config.MaxRounds != maxReviewRounds {
		t.Fatalf("expected ceilings %d and %d, got %d and %d", maxConcurrency, maxReviewRounds, parsed.Config.Concurrency, parsed.Config.MaxRounds)
	}

	omitted, err := ParseReviewArgs([]string{"owner/repo", "--source-branch", "feature"}, now)
	if err != nil {
		t.Fatalf("ParseReviewArgs defaults failed: %v", err)
	}
	if omitted.Config.Concurrency != defaultConcurrency || omitted.Config.MaxRounds != defaultMaxRounds {
		t.Fatalf("expected defaults %d and %d, got %d and %d", defaultConcurrency, defaultMaxRounds, omitted.Config.Concurrency, omitted.Config.MaxRounds)
	}

	limitSeconds := int64(math.MaxInt64) / int64(time.Second)
	t.Setenv("DEEPREVIEW_REVIEW_INACTIVITY_SECONDS", strconv.FormatInt(limitSeconds, 10))
	bounded, err := ParseReviewArgs([]string{"owner/repo", "--source-branch", "feature", "--concurrency", "1", "--max-rounds", "1"}, now)
	if err != nil {
		t.Fatalf("max safe duration failed: %v", err)
	}
	if bounded.Config.ReviewInactivity != time.Duration(limitSeconds)*time.Second {
		t.Fatalf("expected max safe inactivity, got %s", bounded.Config.ReviewInactivity)
	}

	help := ReviewHelpText()
	for _, want := range []string{"maximum: 64", "maximum: 100", "does not reduce it"} {
		if !strings.Contains(help, want) {
			t.Fatalf("review help missing %q", want)
		}
	}
}

func TestNewOrchestratorRejectsUnboundedCountsBeforeWork(t *testing.T) {
	workspace := t.TempDir()
	base := ReviewConfig{
		Repo:          "example-org/example-repo",
		SourceBranch:  "main",
		Mode:          ModePR,
		RunID:         "run-1",
		WorkspaceRoot: workspace,
		GitBin:        "git",
		MaxRounds:     1,
	}
	cases := []struct {
		name        string
		concurrency int
		maxRounds   int
		timeoutSec  int
		want        string
	}{
		{name: "concurrency zero", concurrency: 0, maxRounds: 1, want: "--concurrency"},
		{name: "concurrency negative", concurrency: -1, maxRounds: 1, want: "--concurrency"},
		{name: "concurrency above ceiling", concurrency: maxConcurrency + 1, maxRounds: 1, want: "--concurrency"},
		{name: "concurrency max int", concurrency: math.MaxInt, maxRounds: 1, want: "--concurrency"},
		{name: "rounds zero", concurrency: 1, maxRounds: 0, want: "--max-rounds"},
		{name: "rounds negative", concurrency: 1, maxRounds: -1, want: "--max-rounds"},
		{name: "rounds above ceiling", concurrency: 1, maxRounds: maxReviewRounds + 1, want: "--max-rounds"},
		{name: "rounds max int", concurrency: 1, maxRounds: math.MaxInt, want: "--max-rounds"},
		{name: "timeout overflow", concurrency: 1, maxRounds: 1, timeoutSec: int(int64(math.MaxInt64)/int64(time.Second)) + 1, want: "overflows a duration"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := base
			cfg.Concurrency = tc.concurrency
			cfg.MaxRounds = tc.maxRounds
			cfg.CodexTimeoutSeconds = tc.timeoutSec
			_, err := NewOrchestrator(cfg, nil)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected error containing %q, got %v", tc.want, err)
			}
			entries, readErr := os.ReadDir(workspace)
			if readErr != nil || len(entries) != 0 {
				t.Fatalf("constructor changed workspace: %v entries=%d", readErr, len(entries))
			}
		})
	}

	accepted := base
	accepted.Concurrency = maxConcurrency
	accepted.MaxRounds = maxReviewRounds
	orchestrator, err := NewOrchestrator(accepted, nil)
	if err != nil {
		t.Fatalf("ceiling constructor failed: %v", err)
	}
	if orchestrator.config.Concurrency != maxConcurrency || orchestrator.config.MaxRounds != maxReviewRounds {
		t.Fatalf("ceiling constructor stored %d and %d", orchestrator.config.Concurrency, orchestrator.config.MaxRounds)
	}
	entries, readErr := os.ReadDir(workspace)
	if readErr != nil || len(entries) != 0 {
		t.Fatalf("accepted constructor changed workspace: %v entries=%d", readErr, len(entries))
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
		"--concurrency <int>   (default: 4, maximum: 64)",
		"--max-rounds <int>    (default: 5, maximum: 100)",
		"does not reduce it",
		"--mode <pr|yolo>      (default: pr)",
		"PR mode requires a GitHub-backed remote",
		"DEEPREVIEW_WORKSPACE_ROOT",
		"DEEPREVIEW_CALLER_CWD",
		"DEEPREVIEW_REQUIRE_MULTICODEX",
		"DEEPREVIEW_REVIEW_INACTIVITY_SECONDS",
		"DEEPREVIEW_REVIEW_ACTIVITY_POLL_SECONDS",
		"DEEPREVIEW_REVIEW_MAX_RESTARTS",
		"If <repo> is omitted:",
		"Troubleshooting:",
		"press Ctrl+C once",
		"Codex execution uses your normal local Codex config/profile",
		"Codex prompt timeout per prompt: 3600s",
		"--trust-repo",
		"deepreview is not a sandbox",
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
	code := RunCLI([]string{"/Users/YOU/private-command"})
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
	if !strings.Contains(output, "unsupported command: /Users/YOU/private-command") {
		t.Fatalf("expected unsupported-command output to keep original token, got:\n%s", output)
	}
}

func TestDoctorHelpTextIncludesCoreSections(t *testing.T) {
	help := DoctorHelpText()
	for _, want := range []string{
		"deepreview doctor",
		"Run non-mutating preflight checks",
		"the selected Codex launcher is runnable",
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

func TestReviewHelpTextMentionsBranchScopedManagedCopy(t *testing.T) {
	help := ReviewHelpText()
	if !strings.Contains(help, "branch-scoped managed copy") {
		t.Fatalf("expected review help to mention branch-scoped managed copy, got:\n%s", help)
	}
}

func TestReviewHelpExamplesParseWithoutTrustError(t *testing.T) {
	examples := reviewHelpExamples(ReviewHelpText())
	if len(examples) < 6 {
		t.Fatalf("expected every review help example, got %d", len(examples))
	}
	for _, example := range examples {
		args := strings.Fields(strings.TrimPrefix(example, "deepreview review"))
		parsed, err := ParseReviewArgs(args, time.Unix(1700000000, 0))
		if err != nil {
			t.Fatalf("example %q failed to parse: %v", example, err)
		}
		if !parsed.Config.TrustRepo {
			t.Fatalf("example %q: %s", example, repositoryTrustRequiredMessage)
		}
	}
}

func reviewHelpExamples(help string) []string {
	var examples []string
	inExamples := false
	for _, line := range strings.Split(help, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "Examples:" {
			inExamples = true
			continue
		}
		if !inExamples {
			continue
		}
		if trimmed == "" {
			continue
		}
		if !strings.HasPrefix(trimmed, "deepreview review ") {
			break
		}
		examples = append(examples, trimmed)
	}
	return examples
}

func TestPrintDryRunPlanUsesRepoBranchLockLanguage(t *testing.T) {
	o := &Orchestrator{
		config: ReviewConfig{
			SourceBranch: "feature/test",
			Mode:         ModePR,
			Concurrency:  2,
			MaxRounds:    3,
		},
		repoIdentity:    RepoIdentity{SourceType: RepoSourceGitHub, Owner: "example", Name: "repo"},
		workspaceRoot:   "/tmp/workspace",
		managedRepoPath: "/tmp/workspace/repos/github/example/repo/branches/feature-test-1234567890abcdef",
		promptsRoot:     filepath.Join(repoRoot(t), "prompts"),
	}

	var out strings.Builder
	printDryRunPlan(&out, o)
	text := out.String()
	if !strings.Contains(text, "acquire per-repo+branch run lock") {
		t.Fatalf("expected dry-run plan to mention repo+branch lock, got:\n%s", text)
	}
	if !strings.Contains(text, "managed repo path: /tmp/workspace/repos/github/example/repo/branches/") {
		t.Fatalf("expected dry-run plan to show branch-scoped managed repo path, got:\n%s", text)
	}
	if !strings.Contains(text, "sync branch-scoped managed repository copy") {
		t.Fatalf("expected dry-run plan to describe branch-scoped managed repo sync, got:\n%s", text)
	}
	if !strings.Contains(text, "repository execution trust: not granted") {
		t.Fatalf("expected dry-run plan to show missing repository trust, got:\n%s", text)
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

func TestClearTerminalForCompletionSummaryWritesEscapeSequence(t *testing.T) {
	var out strings.Builder
	clearTerminalForCompletionSummary(&out)
	if out.String() != "\x1b[2J\x1b[H" {
		t.Fatalf("unexpected clear sequence: %q", out.String())
	}
}

func TestFormatFinalReviewNextFocus(t *testing.T) {
	if got := formatFinalReviewNextFocus(RoundStatus{}); got != "" {
		t.Fatalf("expected empty next focus when field is absent, got %q", got)
	}

	raw := "  tighten flaky tests\nfor parser edge cases  "
	got := formatFinalReviewNextFocus(RoundStatus{NextFocus: &raw})
	if got != "tighten flaky tests for parser edge cases" {
		t.Fatalf("unexpected formatted next focus: %q", got)
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

func TestRunDryRunCommandKeepsReadinessValidation(t *testing.T) {
	repo := createSyncedGitHubLikeRepo(t, "feature/test")
	t.Setenv("DEEPREVIEW_WORKSPACE_ROOT", t.TempDir())

	withWorkingDir(t, repo, func() {
		if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("ahead\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		runGitCommand(t, repo, "add", "README.md")
		runGitCommand(t, repo, "commit", "-m", "ahead")

		code, stdout, stderr := captureCommandOutput(t, func() int {
			return runDryRunCommand([]string{})
		})
		if code != 1 {
			t.Fatalf("expected dry-run to fail, got code=%d stdout=\n%s\nstderr=\n%s", code, stdout, stderr)
		}
		if !strings.Contains(stderr, "not synchronized") {
			t.Fatalf("expected readiness failure in stderr, got:\n%s", stderr)
		}
	})
}

func TestRunReviewCommandKeepsReadinessValidation(t *testing.T) {
	repo := createSyncedGitHubLikeRepo(t, "feature/test")
	t.Setenv("DEEPREVIEW_WORKSPACE_ROOT", t.TempDir())

	withWorkingDir(t, repo, func() {
		if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("ahead\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		runGitCommand(t, repo, "add", "README.md")
		runGitCommand(t, repo, "commit", "-m", "ahead")

		code, stdout, stderr := captureCommandOutput(t, func() int {
			return runReviewCommand([]string{"--no-tui", "--trust-repo"})
		})
		if code != 1 {
			t.Fatalf("expected review to fail, got code=%d stdout=\n%s\nstderr=\n%s", code, stdout, stderr)
		}
		if !strings.Contains(stderr, "not synchronized") {
			t.Fatalf("expected readiness failure in stderr, got:\n%s", stderr)
		}
	})
}

func TestRunReviewCommandRejectsTrackedChangesForFilesystemOriginRepo(t *testing.T) {
	repo := createSyncedFilesystemRepo(t, "feature/test")
	t.Setenv("DEEPREVIEW_WORKSPACE_ROOT", t.TempDir())

	withWorkingDir(t, t.TempDir(), func() {
		if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("modified\n"), 0o644); err != nil {
			t.Fatal(err)
		}

		code, stdout, stderr := captureCommandOutput(t, func() int {
			return runReviewCommand([]string{repo, "--source-branch", "feature/test", "--no-tui", "--trust-repo"})
		})
		if code != 1 {
			t.Fatalf("expected review to fail, got code=%d stdout=\n%s\nstderr=\n%s", code, stdout, stderr)
		}
		if !strings.Contains(stderr, "local tracked changes are present") {
			t.Fatalf("expected readiness failure in stderr, got:\n%s", stderr)
		}
	})
}

func TestRunReviewCommandRejectsAheadOfRemoteForFilesystemOriginRepo(t *testing.T) {
	repo := createSyncedFilesystemRepo(t, "feature/test")
	t.Setenv("DEEPREVIEW_WORKSPACE_ROOT", t.TempDir())

	withWorkingDir(t, t.TempDir(), func() {
		if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("ahead\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		runGitCommand(t, repo, "add", "README.md")
		runGitCommand(t, repo, "commit", "-m", "ahead")

		code, stdout, stderr := captureCommandOutput(t, func() int {
			return runReviewCommand([]string{repo, "--source-branch", "feature/test", "--no-tui", "--trust-repo"})
		})
		if code != 1 {
			t.Fatalf("expected review to fail, got code=%d stdout=\n%s\nstderr=\n%s", code, stdout, stderr)
		}
		if !strings.Contains(stderr, "not synchronized") {
			t.Fatalf("expected readiness failure in stderr, got:\n%s", stderr)
		}
	})
}

func TestRunDoctorCommandKeepsReadinessValidation(t *testing.T) {
	repo := createSyncedGitHubLikeRepo(t, "feature/test")
	t.Setenv("DEEPREVIEW_WORKSPACE_ROOT", t.TempDir())
	toolDir := t.TempDir()
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Fatalf("resolve git: %v", err)
	}
	t.Setenv("PATH", toolDir+string(os.PathListSeparator)+filepath.Dir(gitPath))
	t.Setenv("DEEPREVIEW_GIT_BIN", gitPath)
	writeFakeMulticodexTool(t, toolDir)
	t.Setenv("DEEPREVIEW_GH_BIN", writeFakeTool(t, toolDir, "gh"))

	withWorkingDir(t, repo, func() {
		if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("ahead\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		runGitCommand(t, repo, "add", "README.md")
		runGitCommand(t, repo, "commit", "-m", "ahead")

		code, stdout, stderr := captureCommandOutput(t, func() int {
			return runDoctorCommand([]string{})
		})
		if code != 1 {
			t.Fatalf("expected doctor to fail, got code=%d stdout=\n%s\nstderr=\n%s", code, stdout, stderr)
		}
		if !strings.Contains(stdout, "local source branch ready for review") {
			t.Fatalf("expected readiness check output, got:\n%s", stdout)
		}
		if !strings.Contains(stdout, "doctor result: FAIL") {
			t.Fatalf("expected doctor fail output, got:\n%s", stdout)
		}
		if !strings.Contains(stdout, "not synchronized") {
			t.Fatalf("expected readiness failure detail, got:\n%s", stdout)
		}
	})
}

func TestRunDoctorCommandFailsWhenDeliveryTemplateMissingInYoloMode(t *testing.T) {
	repo := createSyncedFilesystemRepo(t, "feature/test")
	t.Setenv("DEEPREVIEW_WORKSPACE_ROOT", t.TempDir())
	t.Setenv("DEEPREVIEW_PROMPTS_ROOT", writePromptsRootWithoutDeliveryTemplate(t))
	toolDir := t.TempDir()
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Fatalf("resolve git: %v", err)
	}
	t.Setenv("PATH", toolDir+string(os.PathListSeparator)+filepath.Dir(gitPath))
	t.Setenv("DEEPREVIEW_GIT_BIN", gitPath)
	writeFakeMulticodexTool(t, toolDir)
	t.Setenv("SHELL", "")

	withWorkingDir(t, repo, func() {
		code, stdout, stderr := captureCommandOutput(t, func() int {
			return runDoctorCommand([]string{repo, "--source-branch", "feature/test", "--mode", "yolo"})
		})
		if code != 1 {
			t.Fatalf("expected doctor to fail, got code=%d stdout=\n%s\nstderr=\n%s", code, stdout, stderr)
		}
		if !strings.Contains(stdout, "delivery prompt template missing") {
			t.Fatalf("expected delivery template failure in stdout, got:\n%s", stdout)
		}
		if !strings.Contains(stdout, "doctor result: FAIL") {
			t.Fatalf("expected doctor fail output, got:\n%s", stdout)
		}
	})
}

func TestRunDryRunCommandFailsWhenDeliveryTemplateMissingInYoloMode(t *testing.T) {
	repo := createSyncedFilesystemRepo(t, "feature/test")
	t.Setenv("DEEPREVIEW_WORKSPACE_ROOT", t.TempDir())
	t.Setenv("DEEPREVIEW_PROMPTS_ROOT", writePromptsRootWithoutDeliveryTemplate(t))

	withWorkingDir(t, repo, func() {
		code, stdout, stderr := captureCommandOutput(t, func() int {
			return runDryRunCommand([]string{repo, "--source-branch", "feature/test", "--mode", "yolo"})
		})
		if code != 1 {
			t.Fatalf("expected dry-run to fail, got code=%d stdout=\n%s\nstderr=\n%s", code, stdout, stderr)
		}
		if !strings.Contains(stderr, "delivery prompt template missing") {
			t.Fatalf("expected delivery template failure in stderr, got:\n%s", stderr)
		}
		if strings.Contains(stdout, "deepreview dry-run") {
			t.Fatalf("did not expect dry-run plan output on prompt validation failure, got:\n%s", stdout)
		}
	})
}

func TestRunDoctorCommandFailsWhenMulticodexIsRequiredButUnavailable(t *testing.T) {
	repo := createSyncedGitHubLikeRepo(t, "feature/test")
	t.Setenv("DEEPREVIEW_WORKSPACE_ROOT", t.TempDir())
	toolDir := t.TempDir()
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Fatalf("resolve git: %v", err)
	}
	t.Setenv("PATH", toolDir)
	t.Setenv("DEEPREVIEW_GIT_BIN", gitPath)
	t.Setenv("DEEPREVIEW_CODEX_BIN", writeFakeTool(t, toolDir, "codex"))
	t.Setenv("DEEPREVIEW_GH_BIN", writeFakeTool(t, toolDir, "gh"))
	t.Setenv("DEEPREVIEW_REQUIRE_MULTICODEX", "1")
	t.Setenv("SHELL", "")

	withWorkingDir(t, repo, func() {
		code, stdout, stderr := captureCommandOutput(t, func() int {
			return runDoctorCommand([]string{})
		})
		if code != 1 {
			t.Fatalf("expected doctor to fail, got code=%d stdout=\n%s\nstderr=\n%s", code, stdout, stderr)
		}
		if !strings.Contains(stdout, envRequireMulticodex) {
			t.Fatalf("expected multicodex requirement failure in stdout, got:\n%s", stdout)
		}
		if !strings.Contains(stdout, "doctor result: FAIL") {
			t.Fatalf("expected doctor fail output, got:\n%s", stdout)
		}
	})
}

func TestRunDoctorCommandUsesResolvedMulticodexWithoutCodexOnPath(t *testing.T) {
	repo := createSyncedGitHubLikeRepo(t, "feature/test")
	t.Setenv("DEEPREVIEW_WORKSPACE_ROOT", t.TempDir())
	toolDir := t.TempDir()
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Fatalf("resolve git: %v", err)
	}
	multicodexPath := filepath.Join(toolDir, "multicodex")
	writeExecutable(t, multicodexPath, "#!/bin/sh\nif [ \"$1\" = \"status\" ]; then\n  echo logged-in profile\n  exit 0\nfi\nexit 0\n")
	t.Setenv("PATH", toolDir)
	t.Setenv("DEEPREVIEW_GIT_BIN", gitPath)
	t.Setenv("DEEPREVIEW_GH_BIN", writeFakeTool(t, toolDir, "gh"))
	t.Setenv("DEEPREVIEW_CODEX_BIN", "codex")
	t.Setenv("SHELL", "")

	withWorkingDir(t, repo, func() {
		code, stdout, stderr := captureCommandOutput(t, func() int {
			return runDoctorCommand([]string{})
		})
		if code != 0 {
			t.Fatalf("expected doctor to succeed, got code=%d stdout=\n%s\nstderr=\n%s", code, stdout, stderr)
		}
		if !strings.Contains(stdout, "doctor result: PASS") {
			t.Fatalf("expected doctor pass output, got:\n%s", stdout)
		}
		if !strings.Contains(stdout, "repository execution trust") || !strings.Contains(stdout, "not granted") {
			t.Fatalf("expected informational trust status, got:\n%s", stdout)
		}
		if strings.Contains(stdout, "tool available: codex") {
			t.Fatalf("did not expect raw codex tool check in multicodex mode, got:\n%s", stdout)
		}
		if !strings.Contains(stdout, "multicodex status") {
			t.Fatalf("expected multicodex auth check output, got:\n%s", stdout)
		}
	})
}

func TestDoctorAndDryRunDoNotMutateWorkspace(t *testing.T) {
	repo := createSyncedGitHubLikeRepo(t, "feature/test")
	workspace := t.TempDir()
	t.Setenv("DEEPREVIEW_WORKSPACE_ROOT", workspace)
	t.Setenv("DEEPREVIEW_PROMPTS_ROOT", "")
	toolDir := t.TempDir()
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Fatalf("resolve git: %v", err)
	}
	before := gitContentSnapshot(t, gitPath, repo)
	multicodexPath := filepath.Join(toolDir, "multicodex")
	writeExecutable(t, multicodexPath, "#!/bin/sh\nif [ \"$1\" = \"status\" ]; then\n  echo logged-in profile\n  exit 0\nfi\nexit 0\n")
	t.Setenv("PATH", toolDir)
	t.Setenv("DEEPREVIEW_GIT_BIN", gitPath)
	t.Setenv("DEEPREVIEW_GH_BIN", writeFakeTool(t, toolDir, "gh"))
	t.Setenv("DEEPREVIEW_CODEX_BIN", "codex")
	t.Setenv("SHELL", "")

	withWorkingDir(t, repo, func() {
		code, stdout, stderr := captureCommandOutput(t, func() int {
			return runDoctorCommand([]string{})
		})
		if code != 0 {
			t.Fatalf("expected doctor to succeed, got code=%d stdout=\n%s\nstderr=\n%s", code, stdout, stderr)
		}
		if !strings.Contains(stdout, "doctor result: PASS") {
			t.Fatalf("expected doctor pass output, got:\n%s", stdout)
		}
		code, stdout, stderr = captureCommandOutput(t, func() int {
			return runDryRunCommand([]string{})
		})
		if code != 0 {
			t.Fatalf("expected dry-run to succeed, got code=%d stdout=\n%s\nstderr=\n%s", code, stdout, stderr)
		}
		if !strings.Contains(stdout, "triage and plan") {
			t.Fatalf("expected embedded execute queue step, got:\n%s", stdout)
		}
	})

	after := gitContentSnapshot(t, gitPath, repo)
	if before != after {
		t.Fatalf("doctor or dry-run changed git state\nbefore:\n%s\nafter:\n%s", before, after)
	}
	entries, err := os.ReadDir(workspace)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		t.Fatalf("doctor or dry-run wrote workspace entries: %s", strings.Join(names, ", "))
	}
}

func TestRunDoctorCommandRejectsPRModeForFilesystemOriginRemote(t *testing.T) {
	td := t.TempDir()
	remote := filepath.Join(td, "remote.git")
	repo := filepath.Join(td, "repo")
	runGitCommand(t, td, "init", "--bare", remote)
	runGitCommand(t, td, "init", "-b", "main", repo)
	runGitCommand(t, td, "-C", repo, "remote", "add", "origin", remote)
	t.Setenv("DEEPREVIEW_WORKSPACE_ROOT", t.TempDir())

	code, stdout, stderr := captureCommandOutput(t, func() int {
		return runDoctorCommand([]string{repo, "--source-branch", "main"})
	})
	if code != 1 {
		t.Fatalf("expected doctor to fail, got code=%d stdout=\n%s\nstderr=\n%s", code, stdout, stderr)
	}
	if !strings.Contains(stderr, "--mode pr requires a GitHub-backed repo identity") {
		t.Fatalf("expected PR-mode filesystem remote error, got stderr=\n%s", stderr)
	}
}

func TestRunDryRunCommandRejectsPRModeForFilesystemOriginRemote(t *testing.T) {
	td := t.TempDir()
	remote := filepath.Join(td, "remote.git")
	repo := filepath.Join(td, "repo")
	runGitCommand(t, td, "init", "--bare", remote)
	runGitCommand(t, td, "init", "-b", "main", repo)
	runGitCommand(t, td, "-C", repo, "remote", "add", "origin", remote)
	t.Setenv("DEEPREVIEW_WORKSPACE_ROOT", t.TempDir())

	code, stdout, stderr := captureCommandOutput(t, func() int {
		return runDryRunCommand([]string{repo, "--source-branch", "main"})
	})
	if code != 1 {
		t.Fatalf("expected dry-run to fail, got code=%d stdout=\n%s\nstderr=\n%s", code, stdout, stderr)
	}
	if !strings.Contains(stderr, "--mode pr requires a GitHub-backed repo identity") {
		t.Fatalf("expected PR-mode filesystem remote error, got stderr=\n%s", stderr)
	}
}

func TestRunDryRunCommandInfersBranchForFilesystemOriginRepoInYoloMode(t *testing.T) {
	repo := createSyncedFilesystemRepo(t, "feature/test")
	t.Setenv("DEEPREVIEW_WORKSPACE_ROOT", t.TempDir())

	withWorkingDir(t, t.TempDir(), func() {
		code, stdout, stderr := captureCommandOutput(t, func() int {
			return runDryRunCommand([]string{repo, "--mode", "yolo"})
		})
		if code != 0 {
			t.Fatalf("expected dry-run to succeed, got code=%d stdout=\n%s\nstderr=\n%s", code, stdout, stderr)
		}
		if strings.TrimSpace(stderr) != "" {
			t.Fatalf("did not expect stderr output, got:\n%s", stderr)
		}
		if !strings.Contains(stdout, "source branch: feature/test") {
			t.Fatalf("expected inferred source branch in dry-run output, got:\n%s", stdout)
		}
		if !strings.Contains(stdout, "mode: yolo") {
			t.Fatalf("expected yolo mode output, got:\n%s", stdout)
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

func TestShouldPrintFailureArtifactSummary(t *testing.T) {
	if shouldPrintFailureArtifactSummary(nil) {
		t.Fatalf("nil error should not request failure summary")
	}
	if !shouldPrintFailureArtifactSummary(errors.New("different failure")) {
		t.Fatalf("expected summary flag for any run error")
	}
	if !shouldPrintFailureArtifactSummary(errors.New("deepreview requires at least one additional review round after code changes")) {
		t.Fatalf("expected summary flag for max-rounds post-change review failure")
	}
}

func TestPrintFailureArtifactSummary(t *testing.T) {
	var out strings.Builder
	config := ReviewConfig{
		WorkspaceRoot: t.TempDir(),
		RunID:         "20260228T002535Z-39dab37e",
		Repo:          "owner/repo",
		SourceBranch:  "main",
	}
	orchestrator := &Orchestrator{
		runRoot: filepath.Join(config.WorkspaceRoot, "runs", config.RunID),
	}

	printFailureArtifactSummary(&out, orchestrator, config)
	text := out.String()
	for _, want := range []string{
		"deepreview failure summary:\n",
		"run: `20260228T002535Z-39dab37e`\n",
		"repository reviewed: `owner/repo`\n",
		"source branch reviewed: `main`\n",
		"run exited before delivery; no push or PR was created.\n",
		"inspect these paths to review what deepreview produced:\n",
		"run artifacts: ",
		"run health: ",
		"logs: ",
		"reviews: ",
		"round artifacts: ",
		"round status files: ",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected failure summary to include %q, got:\n%s", want, text)
		}
	}
}

func TestPrintFailureArtifactSummaryWithoutOrchestrator(t *testing.T) {
	var out strings.Builder
	config := ReviewConfig{
		WorkspaceRoot: t.TempDir(),
		RunID:         "20260228T002535Z-39dab37e",
		Repo:          "owner/repo",
		SourceBranch:  "main",
	}

	printFailureArtifactSummary(&out, nil, config)
	text := out.String()
	for _, want := range []string{
		"deepreview failure summary:\n",
		"run: `20260228T002535Z-39dab37e`\n",
		"repository reviewed: `owner/repo`\n",
		"source branch reviewed: `main`\n",
		"run exited before delivery; no push or PR was created.\n",
		"run artifacts: ",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected failure summary to include %q, got:\n%s", want, text)
		}
	}
}

func TestCleanupInterruptedRunArtifactsRemovesOwnedWorktreesOnly(t *testing.T) {
	td := t.TempDir()
	repo := filepath.Join(td, "repo")
	runRoot := filepath.Join(td, "run")
	owned := filepath.Join(runRoot, "round-01", "review", "worker-01", "worktree")
	unowned := filepath.Join(runRoot, "round-01", "notes", "worktree")
	canonical := filepath.Join(runRoot, "round-01", "round-status.json")
	initCommittedRepo(t, repo)
	if err := AddDetachedWorktree(repo, "git", owned, "HEAD"); err != nil {
		t.Fatalf("AddDetachedWorktree failed: %v", err)
	}
	if err := os.MkdirAll(unowned, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(unowned, "leftover.txt"), []byte("leftover\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(canonical), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(canonical, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := cleanupInterruptedRunArtifacts(runRoot, "git"); err != nil {
		t.Fatalf("cleanup failed: %v", err)
	}
	if _, err := os.Stat(owned); !os.IsNotExist(err) {
		t.Fatalf("expected owned worktree removal, err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(unowned, "leftover.txt")); err != nil {
		t.Fatalf("unowned directory was removed: %v", err)
	}
	if _, err := os.Stat(canonical); err != nil {
		t.Fatalf("canonical artifact was removed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repo, "README.md")); err != nil {
		t.Fatalf("repository checkout was damaged: %v", err)
	}
	list := runGitCommand(t, td, "-C", repo, "worktree", "list", "--porcelain")
	if strings.Contains(list, owned) {
		t.Fatalf("owned worktree registration remained:\n%s", list)
	}
}

func TestPrintFailureArtifactSummaryMentionsExistingDeliveryArtifacts(t *testing.T) {
	var out strings.Builder
	config := ReviewConfig{
		WorkspaceRoot: t.TempDir(),
		RunID:         "20260228T002535Z-39dab37e",
		Repo:          "owner/repo",
		SourceBranch:  "main",
	}
	orchestrator := &Orchestrator{
		runRoot: filepath.Join(config.WorkspaceRoot, "runs", config.RunID),
		lastDelivery: &DeliveryResult{
			Mode:       ModePR,
			PRURL:      "https://example.com/owner/repo/pull/123",
			CommitsURL: "https://example.com/owner/repo/commits/deepreview/main/run",
		},
	}

	printFailureArtifactSummary(&out, orchestrator, config)
	text := out.String()
	for _, want := range []string{
		"run failed after delivery artifacts were created.\n",
		"latest PR: https://example.com/owner/repo/pull/123\n",
		"latest delivery commits: https://example.com/owner/repo/commits/deepreview/main/run\n",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected failure summary to include %q, got:\n%s", want, text)
		}
	}
	if strings.Contains(text, "run exited before delivery; no push or PR was created.") {
		t.Fatalf("expected delivery-aware failure summary, got:\n%s", text)
	}
}

func TestPrintFailureArtifactSummaryHidesUnpushedCommitsURL(t *testing.T) {
	var out strings.Builder
	config := ReviewConfig{
		WorkspaceRoot: t.TempDir(),
		RunID:         "20260228T002535Z-39dab37e",
		Repo:          "owner/repo",
		SourceBranch:  "main",
	}
	orchestrator := &Orchestrator{
		runRoot: filepath.Join(config.WorkspaceRoot, "runs", config.RunID),
		lastDelivery: &DeliveryResult{
			Mode:        ModeYolo,
			CommitsURL:  "https://example.com/owner/repo/commits/main",
			Publication: publicationNotAttempted,
		},
	}

	printFailureArtifactSummary(&out, orchestrator, config)
	text := out.String()
	if strings.Contains(text, "https://example.com/owner/repo/commits/main") {
		t.Fatalf("failure summary advertised a commits URL before push:\n%s", text)
	}
	if !strings.Contains(text, "run exited before delivery; no push or PR was created.") {
		t.Fatalf("expected no-publication failure text, got:\n%s", text)
	}
	if !strings.Contains(text, "outcome: failed\n") {
		t.Fatalf("expected failed outcome, got:\n%s", text)
	}
}

func TestPrintFailureArtifactSummaryShowsPushWithoutPullRequest(t *testing.T) {
	var out strings.Builder
	config := ReviewConfig{
		WorkspaceRoot: t.TempDir(),
		RunID:         "20260228T002535Z-39dab37e",
		Repo:          "owner/repo",
		SourceBranch:  "main",
	}
	orchestrator := &Orchestrator{
		runRoot: filepath.Join(config.WorkspaceRoot, "runs", config.RunID),
		lastDelivery: &DeliveryResult{
			Mode:        ModePR,
			CommitsURL:  "https://example.com/owner/repo/commits/deepreview/main/run",
			PRURL:       "https://example.com/owner/repo/pull/123",
			Publication: publicationPushed,
		},
	}

	printFailureArtifactSummary(&out, orchestrator, config)
	text := out.String()
	if !strings.Contains(text, "delivery branch was pushed; pull request was not created.") {
		t.Fatalf("expected pushed-only failure text, got:\n%s", text)
	}
	if !strings.Contains(text, "latest delivery commits: https://example.com/owner/repo/commits/deepreview/main/run") {
		t.Fatalf("expected commits URL after push, got:\n%s", text)
	}
	if strings.Contains(text, "latest PR:") {
		t.Fatalf("failure summary advertised a pull request before creation:\n%s", text)
	}
}

func TestPrintCompletionSummaryMentionsRunHealthArtifact(t *testing.T) {
	runRoot := t.TempDir()
	roundDir := filepath.Join(runRoot, "round-01")
	if err := os.MkdirAll(roundDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(roundDir, "round.json"), []byte("{\"round\":1,\"summary\":\"round-summary.md\",\"status\":{\"decision\":\"stop\",\"reason\":\"done\",\"confidence\":0.9}}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	orchestrator := &Orchestrator{
		runRoot: runRoot,
		lastDelivery: &DeliveryResult{
			Mode:  ModePR,
			PRURL: "https://example.com/owner/repo/pull/123",
		},
	}
	config := ReviewConfig{
		RunID:        "20260323T114331Z-d36ba590",
		Repo:         "owner/repo",
		SourceBranch: "main",
		Mode:         ModePR,
	}

	code, stdout, stderr := captureCommandOutput(t, func() int {
		printCompletionSummary(orchestrator, config)
		return 0
	})
	if code != 0 {
		t.Fatalf("expected zero code, got %d", code)
	}
	if stderr != "" {
		t.Fatalf("expected no stderr, got:\n%s", stderr)
	}
	for _, want := range []string{
		"deepreview completed: run `20260323T114331Z-d36ba590`\n",
		"run artifacts: " + runRoot + "\n",
		"final summary: " + filepath.Join(runRoot, "final-summary.md") + "\n",
		"run health: " + filepath.Join(runRoot, runHealthMDName) + "\n",
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("expected completion summary to include %q, got:\n%s", want, stdout)
		}
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
		filepath.Join(round1, "round.json"),
		[]byte("{\"round\":1,\"summary\":\"round-summary.md\",\"status\":{\"decision\":\"continue\",\"reason\":\"keep going\",\"confidence\":0.45}}\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(round2, "round.json"),
		[]byte("{\"round\":2,\"summary\":\"round-summary.md\",\"status\":{\"decision\":\"stop\",\"reason\":\"all major issues addressed\",\"confidence\":0.93,\"next_focus\":\"monitor integration failure drift\"}}\n"),
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
	if snapshot.FinalStatus.NextFocus == nil || *snapshot.FinalStatus.NextFocus != "monitor integration failure drift" {
		t.Fatalf("expected final next_focus to be preserved, got %#v", snapshot.FinalStatus.NextFocus)
	}
}

func TestReadCompletionReviewSnapshotSkipsInvalidRoundRecord(t *testing.T) {
	runRoot := t.TempDir()
	round1 := filepath.Join(runRoot, "round-01")
	round2 := filepath.Join(runRoot, "round-02")
	if err := os.MkdirAll(round1, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(round2, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(round1, "round.json"), []byte("{invalid json}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(round2, "round.json"),
		[]byte("{\"round\":2,\"summary\":\"round-summary.md\",\"status\":{\"decision\":\"stop\",\"reason\":\"done\"}}\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	snapshot := readCompletionReviewSnapshot(runRoot)
	if snapshot.CompletedRounds != 1 {
		t.Fatalf("expected only valid round records to count, got %d", snapshot.CompletedRounds)
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

func TestReadCompletionReviewSnapshotSkipsRoundRecordWithInvalidStatus(t *testing.T) {
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
		filepath.Join(round1, "round.json"),
		[]byte("{\"round\":1,\"summary\":\"round-summary.md\",\"status\":{\"decision\":\"pause\",\"reason\":\"bad\"}}\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(round2, "round.json"),
		[]byte("{\"round\":2,\"summary\":\"round-summary.md\",\"status\":{\"decision\":\"stop\",\"reason\":\"done\"}}\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	snapshot := readCompletionReviewSnapshot(runRoot)
	if snapshot.CompletedRounds != 1 {
		t.Fatalf("expected invalid status record to be skipped, got %d", snapshot.CompletedRounds)
	}
	if !snapshot.HasFinalStatus {
		t.Fatalf("expected final status from valid round record")
	}
	if snapshot.FinalStatus.Reason != "done" {
		t.Fatalf("expected valid round record to win, got %q", snapshot.FinalStatus.Reason)
	}
}

func TestReadCompletionReviewSnapshotIgnoresMissingRoundRecord(t *testing.T) {
	runRoot := t.TempDir()
	round1 := filepath.Join(runRoot, "round-01")
	round2 := filepath.Join(runRoot, "round-02")
	if err := os.MkdirAll(round1, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(round2, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(round1, "round-summary.md"), []byte("# round 01\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(round1, "round.json"),
		[]byte("{\"round\":1,\"summary\":\"round-summary.md\",\"status\":{\"decision\":\"stop\",\"reason\":\"round one committed\"}}\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	snapshot := readCompletionReviewSnapshot(runRoot)
	if snapshot.CompletedRounds != 1 {
		t.Fatalf("expected only round records to count, got %d", snapshot.CompletedRounds)
	}
	if !snapshot.HasFinalStatus {
		t.Fatalf("expected final status from committed round")
	}
	if snapshot.FinalStatus.Reason != "round one committed" {
		t.Fatalf("expected committed round status to win, got %q", snapshot.FinalStatus.Reason)
	}
}
