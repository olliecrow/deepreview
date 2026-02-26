package deepreview

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"golang.org/x/term"
)

type ParsedArgs struct {
	Config ReviewConfig
	NoTUI  bool
}

const (
	defaultConcurrency         = 4
	defaultMaxRounds           = 5
	defaultCodexTimeoutSeconds = 3600
	forcedCodexModel           = "gpt-5.3-codex"
	forcedCodexReasoningEffort = "xhigh"
)

func ParseReviewArgs(args []string, now time.Time) (ParsedArgs, error) {
	normalizedArgs := normalizeLegacyArgs(args)
	var repoFromPrefix string
	flagArgs := normalizedArgs
	if len(normalizedArgs) > 0 && normalizedArgs[0] != "" && normalizedArgs[0][0] != '-' {
		repoFromPrefix = normalizedArgs[0]
		flagArgs = normalizedArgs[1:]
	}

	fs := flag.NewFlagSet("review", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, ReviewHelpText())
	}

	sourceBranch := fs.String("source-branch", "", "source branch to review")
	concurrency := fs.Int("concurrency", defaultConcurrency, "independent review concurrency")
	maxRounds := fs.Int("max-rounds", defaultMaxRounds, "max rounds")
	mode := fs.String("mode", ModePR, "delivery mode")
	yolo := fs.Bool("yolo", false, "alias for --mode yolo")
	noTUI := fs.Bool("no-tui", false, "disable terminal UI")

	if err := fs.Parse(flagArgs); err != nil {
		return ParsedArgs{}, err
	}

	repo := strings.TrimSpace(repoFromPrefix)
	remaining := fs.Args()
	if repo == "" && len(remaining) > 0 {
		if len(remaining) != 1 {
			return ParsedArgs{}, NewDeepReviewError("expected at most one repo locator (path, owner/repo, or URL)")
		}
		repo = strings.TrimSpace(remaining[0])
	} else if len(remaining) > 0 {
		return ParsedArgs{}, NewDeepReviewError("unexpected extra arguments: %v", remaining)
	}

	finalMode := strings.ToLower(strings.TrimSpace(*mode))
	if *yolo {
		finalMode = ModeYolo
	}
	if finalMode != ModePR && finalMode != ModeYolo {
		return ParsedArgs{}, NewDeepReviewError("--mode must be one of: pr, yolo")
	}
	if *concurrency < 1 {
		return ParsedArgs{}, NewDeepReviewError("--concurrency must be >= 1")
	}
	if *maxRounds < 1 {
		return ParsedArgs{}, NewDeepReviewError("--max-rounds must be >= 1")
	}

	workspaceRoot, err := WorkspaceRootFromEnv()
	if err != nil {
		return ParsedArgs{}, err
	}
	gitBin := envOrDefault("DEEPREVIEW_GIT_BIN", "git")
	resolvedRepo, resolvedBranch, err := inferRepoAndBranch(gitBin, repo, strings.TrimSpace(*sourceBranch))
	if err != nil {
		return ParsedArgs{}, err
	}

	runID, err := BuildRunID(now)
	if err != nil {
		return ParsedArgs{}, err
	}

	cfg := ReviewConfig{
		Repo:                resolvedRepo,
		SourceBranch:        resolvedBranch,
		Concurrency:         *concurrency,
		MaxRounds:           *maxRounds,
		Mode:                finalMode,
		WorkspaceRoot:       workspaceRoot,
		RunID:               runID,
		GitBin:              gitBin,
		CodexBin:            envOrDefault("DEEPREVIEW_CODEX_BIN", "codex"),
		CodexModel:          forcedCodexModel,
		CodexReasoning:      forcedCodexReasoningEffort,
		GhBin:               envOrDefault("DEEPREVIEW_GH_BIN", "gh"),
		CodexTimeoutSeconds: defaultCodexTimeoutSeconds,
		CodexTimeout:        defaultCodexTimeoutSeconds * time.Second,
	}

	return ParsedArgs{Config: cfg, NoTUI: *noTUI}, nil
}

func normalizeLegacyArgs(args []string) []string {
	if len(args) == 0 {
		return args
	}
	normalized := make([]string, len(args))
	copy(normalized, args)
	for i, arg := range normalized {
		if arg == "--YOLO" {
			normalized[i] = "--yolo"
		}
	}
	return normalized
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func MainHelpText() string {
	return `deepreview

Deep branch review orchestrator using isolated worktrees plus Codex.

Usage:
  deepreview review [<repo>] [--source-branch <branch>] [options]
  deepreview help
  deepreview --help

Commands:
  review    Run the deepreview pipeline for one source branch.
  help      Show this help text.

Get command-specific help:
  deepreview review --help
`
}

func ReviewHelpText() string {
	return fmt.Sprintf(`deepreview review

Run a deep, multi-stage review pipeline against a source branch in an isolated workspace.

What this command does:
  1) Clones/fetches a managed copy of the repository under ~/deepreview (or override env).
  2) Runs independent review workers in isolated worktrees.
  3) Runs execute-stage prompts to consolidate, plan, execute, and verify changes.
  4) Repeats rounds up to max rounds while execute rounds produce repository changes.
     Stops early when an execute round produces no repository changes.
  5) Delivers once at the end:
     - mode=pr (default): pushes candidate branch + opens PR back into source branch
     - mode=yolo: pushes directly to source branch

Usage:
  deepreview review [<repo>] [--source-branch <branch>] [--concurrency N] [--max-rounds N] [--mode pr|yolo] [--yolo] [--no-tui]

Arguments:
  <repo>
    Repository locator. Supported forms:
      - local git repo path (directory containing .git and origin remote)
      - owner/repo
      - github remote URL (https://... or git@...)
    Context:
      Local path mode reads remote.origin.url and clones/fetches in managed workspace.
      deepreview never runs review work directly in your local repo directory.

Inference behavior:
  If <repo> is omitted:
    deepreview infers repo from the current directory when it is a valid GitHub repo
    (git repo with origin remote resolvable to github owner/repo).

  If --source-branch is omitted:
    deepreview infers the current branch from local repo context.
    Before continuing, deepreview verifies branch readiness:
      - no tracked (non-untracked) local changes
      - local branch is synchronized with upstream remote branch
    If not synchronized, deepreview errors and asks you to commit/push/pull first.

Optional source branch:
  --source-branch <branch>
    Source branch to review. If omitted, inferred from local repo context when available.

Optional flags:
  --concurrency <int>   (default: %d)
    Number of independent review workers (separate worktrees).
    Higher means more parallel review coverage but more local load.

  --max-rounds <int>    (default: %d)
    Maximum review/execute rounds before stopping.
    Process stops early when an execute round produces no repository changes.

  --mode <pr|yolo>      (default: %s)
    Delivery strategy:
      pr   -> open PR into the original source branch
      yolo -> push directly to source branch
    Values are case-insensitive.

  --yolo                (default: false)
    Alias for --mode yolo. If set, it overrides --mode.
    Legacy alias --YOLO is also accepted.

  --no-tui              (default: false)
    Disable full-screen TUI and emit structured text progress logs instead.
    Context:
      TUI auto-enables only when stdin/stdout are terminals with non-dumb TERM
      and a valid terminal size.

Environment overrides:
  DEEPREVIEW_WORKSPACE_ROOT   (default: ~/deepreview)
    Root for managed repos + run artifacts.

  DEEPREVIEW_GIT_BIN          (default: git)
  DEEPREVIEW_CODEX_BIN        (default: codex)
  DEEPREVIEW_GH_BIN           (default: gh)
    Tool binary overrides (name or absolute path).

Operational defaults:
  Codex model: %s
  Codex reasoning effort: %s
  Codex prompt timeout per prompt: %ds
  Run ID: auto-generated UTC timestamp + random suffix

Examples:
  deepreview review /path/to/repo --source-branch feature/login
  deepreview review owner/repo --source-branch feature/login --concurrency 6 --max-rounds 2
  deepreview review owner/repo --source-branch feature/login --mode yolo
  deepreview review owner/repo --source-branch feature/login --yolo
  deepreview review owner/repo --source-branch feature/login --no-tui

Troubleshooting:
  - Missing tools: ensure git/codex/(gh for pr mode) are on PATH or set env overrides.
  - Auth failures: run local auth flows for codex and gh.
  - Non-interactive environments: use --no-tui for stable text logs.
  - Invalid mode: allowed values are only pr or yolo (case-insensitive).
`, defaultConcurrency, defaultMaxRounds, ModePR, forcedCodexModel, forcedCodexReasoningEffort, defaultCodexTimeoutSeconds)
}

func PrintUsage() {
	fmt.Fprint(os.Stderr, MainHelpText())
}

func isHelpArg(arg string) bool {
	return arg == "-h" || arg == "--help" || arg == "help"
}

func RunCLI(argv []string) int {
	if len(argv) == 0 {
		PrintUsage()
		return 1
	}
	if isHelpArg(argv[0]) {
		PrintUsage()
		return 0
	}
	if argv[0] != "review" {
		fmt.Fprintf(os.Stderr, "deepreview error: unsupported command: %s\n", argv[0])
		PrintUsage()
		return 1
	}
	if len(argv) >= 2 && isHelpArg(argv[1]) {
		fmt.Fprint(os.Stderr, ReviewHelpText())
		return 0
	}

	parsed, err := ParseReviewArgs(argv[1:], time.Now())
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		fmt.Fprintf(os.Stderr, "deepreview error: %v\n", err)
		return 1
	}

	stdoutFD := int(os.Stdout.Fd())
	stdinFD := int(os.Stdin.Fd())
	termWidth, termHeight, sizeErr := term.GetSize(stdoutFD)
	enableTUI := shouldEnableTUI(
		parsed.NoTUI,
		term.IsTerminal(stdinFD),
		term.IsTerminal(stdoutFD),
		os.Getenv("TERM"),
		termWidth,
		termHeight,
		sizeErr,
	)

	reporter := ProgressReporter(NewTextProgressReporter(os.Stderr))
	var state *SharedProgressState
	if enableTUI {
		state = NewSharedProgressState()
		reporter = NewTUIProgressReporter(state)
	}

	orchestrator, err := NewOrchestrator(parsed.Config, reporter)
	if err != nil {
		fmt.Fprintf(os.Stderr, "deepreview error: %v\n", err)
		return 1
	}

	if enableTUI {
		err = RunTUIWithWorker(state, termWidth, termHeight, func() error { return orchestrator.Run() })
	} else {
		err = orchestrator.Run()
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "deepreview error: %v\n", err)
		return 1
	}
	printCompletionSummary(orchestrator, parsed.Config)
	return 0
}

func shouldEnableTUI(noTUI, stdinIsTerminal, stdoutIsTerminal bool, termName string, width, height int, sizeErr error) bool {
	if noTUI || !stdinIsTerminal || !stdoutIsTerminal {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(termName), "dumb") {
		return false
	}
	if sizeErr != nil {
		return false
	}
	return width > 0 && height > 0
}

func printCompletionSummary(orchestrator *Orchestrator, config ReviewConfig) {
	if orchestrator == nil {
		return
	}
	delivery := orchestrator.LastDelivery()
	runRoot := orchestrator.RunRoot()
	if runRoot == "" {
		runRoot = filepath.Join(config.WorkspaceRoot, "runs", config.RunID)
	}
	repoSlug := strings.TrimSpace(orchestrator.RepoSlug())
	if repoSlug == "" {
		repoSlug = strings.TrimSpace(config.Repo)
	}
	managedRepoPath := strings.TrimSpace(orchestrator.ManagedRepoPath())
	reviewSnapshot := readCompletionReviewSnapshot(runRoot)

	_, _ = fmt.Fprintf(os.Stdout, "deepreview completed: run `%s`\n", config.RunID)
	if repoSlug != "" {
		_, _ = fmt.Fprintf(os.Stdout, "repository reviewed: `%s`\n", repoSlug)
	}
	_, _ = fmt.Fprintf(os.Stdout, "source branch reviewed: `%s`\n", config.SourceBranch)
	if managedRepoPath != "" {
		_, _ = fmt.Fprintf(os.Stdout, "reviewed directory: %s\n", managedRepoPath)
	}
	if isLocalDirectory(config.Repo) {
		_, _ = fmt.Fprintf(os.Stdout, "requested local repo: %s\n", config.Repo)
	}
	_, _ = fmt.Fprintf(os.Stdout, "delivery mode: `%s`\n", config.Mode)
	if reviewSnapshot.CompletedRounds > 0 {
		_, _ = fmt.Fprintf(os.Stdout, "review rounds completed: %d\n", reviewSnapshot.CompletedRounds)
	}
	if reviewSnapshot.HasFinalStatus {
		_, _ = fmt.Fprintf(os.Stdout, "final review status: %s\n", formatFinalReviewStatus(reviewSnapshot.FinalStatus))
		if reason := formatFinalReviewReason(reviewSnapshot.FinalStatus); reason != "" {
			_, _ = fmt.Fprintf(os.Stdout, "final review summary: %s\n", reason)
		}
	}
	if delivery == nil {
		_, _ = fmt.Fprintf(os.Stdout, "run artifacts: %s\n", runRoot)
		_, _ = fmt.Fprintf(os.Stdout, "final summary: %s\n", filepath.Join(runRoot, "final-summary.md"))
		return
	}

	if delivery.Skipped {
		_, _ = fmt.Fprintf(os.Stdout, "delivery skipped: %s\n", delivery.SkipReason)
		_, _ = fmt.Fprintf(os.Stdout, "no push or PR was created because no deliverable repository changes were found.\n")
		_, _ = fmt.Fprintf(os.Stdout, "run artifacts: %s\n", runRoot)
		_, _ = fmt.Fprintf(os.Stdout, "final summary: %s\n", filepath.Join(runRoot, "final-summary.md"))
		return
	}

	switch delivery.Mode {
	case ModePR:
		if strings.TrimSpace(delivery.PRURL) != "" {
			_, _ = fmt.Fprintf(os.Stdout, "PR created: %s\n", delivery.PRURL)
		} else {
			_, _ = fmt.Fprintf(os.Stdout, "delivery completed in PR mode.\n")
		}
	case ModeYolo:
		if strings.TrimSpace(delivery.CommitsURL) != "" {
			_, _ = fmt.Fprintf(os.Stdout, "changes pushed: %s\n", delivery.CommitsURL)
		} else {
			_, _ = fmt.Fprintf(os.Stdout, "delivery completed in YOLO mode.\n")
		}
	}
	_, _ = fmt.Fprintf(os.Stdout, "run artifacts: %s\n", runRoot)
	_, _ = fmt.Fprintf(os.Stdout, "final summary: %s\n", filepath.Join(runRoot, "final-summary.md"))
}

type completionReviewSnapshot struct {
	CompletedRounds int
	HasFinalStatus  bool
	FinalStatus     RoundStatus
}

func readCompletionReviewSnapshot(runRoot string) completionReviewSnapshot {
	snapshot := completionReviewSnapshot{}
	if strings.TrimSpace(runRoot) == "" {
		return snapshot
	}
	statusPaths, err := filepath.Glob(filepath.Join(runRoot, "round-*", "round-status.json"))
	if err != nil {
		return snapshot
	}
	sort.Strings(statusPaths)
	snapshot.CompletedRounds = len(statusPaths)
	for _, statusPath := range statusPaths {
		status, err := readRoundStatus(statusPath)
		if err != nil {
			continue
		}
		snapshot.FinalStatus = status
		snapshot.HasFinalStatus = true
	}
	return snapshot
}

func formatFinalReviewStatus(status RoundStatus) string {
	if status.Confidence != nil {
		return fmt.Sprintf("decision `%s` (confidence %.2f)", status.Decision, *status.Confidence)
	}
	return fmt.Sprintf("decision `%s`", status.Decision)
}

func formatFinalReviewReason(status RoundStatus) string {
	reason := strings.TrimSpace(strings.ReplaceAll(status.Reason, "\n", " "))
	if reason == "" {
		return ""
	}
	return trimForDisplay(sanitizePublicText(reason), 220)
}

func isLocalDirectory(path string) bool {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return false
	}
	st, err := os.Stat(trimmed)
	return err == nil && st.IsDir()
}
