package deepreview

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
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
  deepreview doctor [<repo>] [--source-branch <branch>] [options]
  deepreview dry-run [<repo>] [--source-branch <branch>] [options]
  deepreview help
  deepreview --help

Commands:
  review    Run the deepreview pipeline for one source branch.
  doctor    Run non-mutating preflight checks for a planned run.
  dry-run   Print the planned execution order without mutating anything.
  help      Show this help text.

Get command-specific help:
  deepreview review --help
  deepreview doctor --help
  deepreview dry-run --help
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
    Force structured text progress logs (disables full-screen TUI).
    Default behavior is TUI-on when terminal capabilities are valid.

Environment overrides:
  DEEPREVIEW_WORKSPACE_ROOT   (default: ~/deepreview)
    Root for managed repos + run artifacts.

  DEEPREVIEW_CALLER_CWD       (optional)
    Explicit caller working directory for repo/branch inference when wrappers launch deepreview
    from another directory.

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
  deepreview review owner/repo --source-branch feature/login
  deepreview review owner/repo --source-branch feature/login --no-tui

Troubleshooting:
  - Missing tools: ensure git/codex/(gh for pr mode) are on PATH or set env overrides.
  - Auth failures: run local auth flows for codex and gh.
  - Terminal rendering issues: pass --no-tui for stable text logs.
  - To stop a run safely at any time, press Ctrl+C once (deepreview cancels and runs cleanup).
  - Invalid mode: allowed values are only pr or yolo (case-insensitive).
`, defaultConcurrency, defaultMaxRounds, ModePR, forcedCodexModel, forcedCodexReasoningEffort, defaultCodexTimeoutSeconds)
}

func DoctorHelpText() string {
	return fmt.Sprintf(`deepreview doctor

Run non-mutating preflight checks for a planned deepreview run.

What this checks:
  - required tools are available on PATH
  - prompt templates and execute queue are present
  - Codex login status is healthy
  - GitHub auth status is healthy (when mode=pr)
  - source branch is reachable on remote

Usage:
  deepreview doctor [<repo>] [--source-branch <branch>] [--concurrency N] [--max-rounds N] [--mode pr|yolo] [--yolo]

Notes:
  - doctor does not clone, commit, push, or open PRs.
  - argument inference and validation follow the same rules as "deepreview review".

Examples:
  deepreview doctor
  deepreview doctor owner/repo --source-branch feature/login
  deepreview doctor owner/repo --source-branch feature/login --mode yolo

Defaults:
  --concurrency %d
  --max-rounds %d
  --mode %s
`, defaultConcurrency, defaultMaxRounds, ModePR)
}

func DryRunHelpText() string {
	return fmt.Sprintf(`deepreview dry-run

Print the planned execution order for a run without mutating anything.

What this prints:
  - resolved repo, source branch, mode, and key settings
  - ordered stage list for prepare, round loop, and delivery
  - execute-stage step order from the configured queue
  - stop conditions and delivery behavior

Usage:
  deepreview dry-run [<repo>] [--source-branch <branch>] [--concurrency N] [--max-rounds N] [--mode pr|yolo] [--yolo]

Notes:
  - dry-run does not run Codex, does not mutate git state, and does not push.
  - argument inference and validation follow the same rules as "deepreview review".

Examples:
  deepreview dry-run
  deepreview dry-run owner/repo --source-branch feature/login
  deepreview dry-run owner/repo --source-branch feature/login --mode yolo

Defaults:
  --concurrency %d
  --max-rounds %d
  --mode %s
`, defaultConcurrency, defaultMaxRounds, ModePR)
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
	switch argv[0] {
	case "review":
		return runReviewCommand(argv[1:])
	case "doctor":
		return runDoctorCommand(argv[1:])
	case "dry-run":
		return runDryRunCommand(argv[1:])
	default:
		fmt.Fprintf(os.Stderr, "deepreview error: unsupported command: %s\n", argv[0])
		PrintUsage()
		return 1
	}
}

func runReviewCommand(args []string) int {
	if len(args) >= 1 && isHelpArg(args[0]) {
		fmt.Fprint(os.Stderr, ReviewHelpText())
		return 0
	}

	runCtx, cancelRun := context.WithCancel(context.Background())
	defer cancelRun()
	restoreCommandContext := setRunCommandContext(runCtx)
	defer restoreCommandContext()

	var interruptCount atomic.Int32
	var cancelOnce sync.Once
	requestCancel := func() {
		cancelOnce.Do(func() {
			cancelRun()
			terminateActiveCommands()
		})
	}
	interrupts := make(chan os.Signal, 2)
	interruptWatcherDone := make(chan struct{})
	signal.Notify(interrupts, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(interrupts)
	defer close(interruptWatcherDone)
	go func() {
		for {
			select {
			case sig := <-interrupts:
				count := interruptCount.Add(1)
				if count == 1 {
					_, _ = fmt.Fprintf(os.Stderr, "deepreview: received %s, canceling run and cleaning up...\n", sig.String())
					requestCancel()
					continue
				}
				_, _ = fmt.Fprintln(os.Stderr, "deepreview: forcing immediate exit")
				os.Exit(130)
			case <-interruptWatcherDone:
				return
			}
		}
	}()

	parsed, err := ParseReviewArgs(args, time.Now())
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		if isInterruptError(err) || interruptCount.Load() > 0 {
			fmt.Fprintln(os.Stderr, "deepreview: run canceled by user; cleanup completed")
			return 130
		}
		fmt.Fprintf(os.Stderr, "deepreview error: %s\n", sanitizePublicText(err.Error()))
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
		fmt.Fprintf(os.Stderr, "deepreview error: %s\n", sanitizePublicText(err.Error()))
		return 1
	}

	if enableTUI {
		err = RunTUIWithWorker(state, termWidth, termHeight, requestCancel, func() error { return orchestrator.Run() })
	} else {
		err = orchestrator.Run()
	}
	if err != nil {
		if isInterruptError(err) || interruptCount.Load() > 0 {
			fmt.Fprintln(os.Stderr, "deepreview: run canceled by user; cleanup completed")
			return 130
		}
		fmt.Fprintf(os.Stderr, "deepreview error: %s\n", sanitizePublicText(err.Error()))
		return 1
	}
	if interruptCount.Load() > 0 {
		fmt.Fprintln(os.Stderr, "deepreview: run canceled by user; cleanup completed")
		return 130
	}
	printCompletionSummary(orchestrator, parsed.Config)
	return 0
}

type doctorCheck struct {
	Name   string
	Passed bool
	Detail string
}

func runDoctorCommand(args []string) int {
	if len(args) >= 1 && isHelpArg(args[0]) {
		fmt.Fprint(os.Stdout, DoctorHelpText())
		return 0
	}

	parsed, err := ParseReviewArgs(args, time.Now())
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		fmt.Fprintf(os.Stderr, "deepreview error: %s\n", sanitizePublicText(err.Error()))
		return 1
	}
	orchestrator, err := NewOrchestrator(parsed.Config, &NullProgressReporter{})
	if err != nil {
		fmt.Fprintf(os.Stderr, "deepreview error: %s\n", sanitizePublicText(err.Error()))
		return 1
	}

	checks := buildDoctorChecks(orchestrator)
	fmt.Fprintf(os.Stdout, "deepreview doctor\n")
	fmt.Fprintf(os.Stdout, "repo: %s\n", sanitizePublicText(orchestrator.repoIdentity.Slug()))
	fmt.Fprintf(os.Stdout, "source branch: %s\n", sanitizePublicText(parsed.Config.SourceBranch))
	fmt.Fprintf(os.Stdout, "mode: %s\n", sanitizePublicText(parsed.Config.Mode))
	fmt.Fprintf(os.Stdout, "workspace root: %s\n\n", sanitizePublicText(orchestrator.workspaceRoot))

	passedAll := true
	for _, check := range checks {
		state := "ok"
		if !check.Passed {
			state = "fail"
			passedAll = false
		}
		fmt.Fprintf(os.Stdout, "[%s] %s", state, check.Name)
		if strings.TrimSpace(check.Detail) != "" {
			fmt.Fprintf(os.Stdout, " - %s", sanitizePublicText(check.Detail))
		}
		fmt.Fprintln(os.Stdout)
	}
	if passedAll {
		fmt.Fprintln(os.Stdout, "\ndoctor result: PASS")
		return 0
	}
	fmt.Fprintln(os.Stdout, "\ndoctor result: FAIL")
	return 1
}

func buildDoctorChecks(o *Orchestrator) []doctorCheck {
	cfg := o.config
	checks := make([]doctorCheck, 0, 8)

	requiredTools := []string{cfg.GitBin, cfg.CodexBin}
	if cfg.Mode == ModePR {
		requiredTools = append(requiredTools, cfg.GhBin)
	}
	for _, tool := range requiredTools {
		path, err := exec.LookPath(tool)
		if err != nil {
			checks = append(checks, doctorCheck{
				Name:   fmt.Sprintf("tool available: %s", tool),
				Passed: false,
				Detail: "not found on PATH",
			})
			continue
		}
		checks = append(checks, doctorCheck{
			Name:   fmt.Sprintf("tool available: %s", tool),
			Passed: true,
			Detail: path,
		})
	}

	if err := checkPromptTemplates(o.promptsRoot); err != nil {
		checks = append(checks, doctorCheck{
			Name:   "prompt templates and execute queue",
			Passed: false,
			Detail: err.Error(),
		})
	} else {
		checks = append(checks, doctorCheck{
			Name:   "prompt templates and execute queue",
			Passed: true,
			Detail: "ready",
		})
	}

	codexStatus, codexErr := RunCommand([]string{cfg.CodexBin, "login", "status"}, "", "", false, 20*time.Second)
	if codexErr != nil {
		checks = append(checks, doctorCheck{
			Name:   "codex login status",
			Passed: false,
			Detail: codexErr.Error(),
		})
	} else if codexStatus.ReturnCode != 0 {
		checks = append(checks, doctorCheck{
			Name:   "codex login status",
			Passed: false,
			Detail: firstOutputLine(codexStatus.Stdout, codexStatus.Stderr),
		})
	} else {
		checks = append(checks, doctorCheck{
			Name:   "codex login status",
			Passed: true,
			Detail: "authenticated",
		})
	}

	if cfg.Mode == ModePR {
		ghStatus, ghErr := RunCommand([]string{cfg.GhBin, "auth", "status"}, "", "", false, 20*time.Second)
		if ghErr != nil {
			checks = append(checks, doctorCheck{
				Name:   "gh auth status",
				Passed: false,
				Detail: ghErr.Error(),
			})
		} else if ghStatus.ReturnCode != 0 {
			checks = append(checks, doctorCheck{
				Name:   "gh auth status",
				Passed: false,
				Detail: firstOutputLine(ghStatus.Stdout, ghStatus.Stderr),
			})
		} else {
			checks = append(checks, doctorCheck{
				Name:   "gh auth status",
				Passed: true,
				Detail: "authenticated",
			})
		}
	}

	sourceRef := "refs/heads/" + cfg.SourceBranch
	lsRemote, lsRemoteErr := RunCommand([]string{cfg.GitBin, "ls-remote", "--exit-code", o.repoIdentity.CloneSource, sourceRef}, "", "", false, 30*time.Second)
	if lsRemoteErr != nil {
		checks = append(checks, doctorCheck{
			Name:   "remote source branch reachable",
			Passed: false,
			Detail: lsRemoteErr.Error(),
		})
	} else if lsRemote.ReturnCode != 0 {
		checks = append(checks, doctorCheck{
			Name:   "remote source branch reachable",
			Passed: false,
			Detail: firstOutputLine(lsRemote.Stdout, lsRemote.Stderr),
		})
	} else {
		checks = append(checks, doctorCheck{
			Name:   "remote source branch reachable",
			Passed: true,
			Detail: fmt.Sprintf("%s (%s)", cfg.SourceBranch, o.repoIdentity.CloneSource),
		})
	}

	return checks
}

func checkPromptTemplates(promptsRoot string) error {
	queuePath := filepath.Join(promptsRoot, "execute", "queue.txt")
	queue, err := ReadQueue(queuePath)
	if err != nil {
		return err
	}
	for _, templateName := range queue {
		templatePath := filepath.Join(promptsRoot, "execute", templateName)
		if _, err := os.Stat(templatePath); err != nil {
			return NewDeepReviewError("execute template file not found: %s", templatePath)
		}
	}
	if _, err := os.Stat(filepath.Join(promptsRoot, "review", "independent-review.md")); err != nil {
		return NewDeepReviewError("independent review prompt template missing")
	}
	return nil
}

func firstOutputLine(stdout, stderr string) string {
	raw := strings.TrimSpace(stderr)
	if raw == "" {
		raw = strings.TrimSpace(stdout)
	}
	if raw == "" {
		return "no output"
	}
	line := strings.TrimSpace(strings.Split(raw, "\n")[0])
	if line == "" {
		return "no output"
	}
	return trimForDisplay(sanitizePublicText(line), 220)
}

func runDryRunCommand(args []string) int {
	if len(args) >= 1 && isHelpArg(args[0]) {
		fmt.Fprint(os.Stdout, DryRunHelpText())
		return 0
	}

	parsed, err := ParseReviewArgs(args, time.Now())
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		fmt.Fprintf(os.Stderr, "deepreview error: %s\n", sanitizePublicText(err.Error()))
		return 1
	}
	orchestrator, err := NewOrchestrator(parsed.Config, &NullProgressReporter{})
	if err != nil {
		fmt.Fprintf(os.Stderr, "deepreview error: %s\n", sanitizePublicText(err.Error()))
		return 1
	}
	printDryRunPlan(os.Stdout, orchestrator)
	return 0
}

func printDryRunPlan(out io.Writer, o *Orchestrator) {
	cfg := o.config
	fmt.Fprintln(out, "deepreview dry-run")
	fmt.Fprintf(out, "repo: %s\n", sanitizePublicText(o.repoIdentity.Slug()))
	fmt.Fprintf(out, "source branch: %s\n", sanitizePublicText(cfg.SourceBranch))
	fmt.Fprintf(out, "mode: %s\n", sanitizePublicText(cfg.Mode))
	fmt.Fprintf(out, "concurrency: %d\n", cfg.Concurrency)
	fmt.Fprintf(out, "max rounds: %d\n", cfg.MaxRounds)
	fmt.Fprintf(out, "workspace root: %s\n", sanitizePublicText(o.workspaceRoot))
	fmt.Fprintf(out, "managed repo path: %s\n\n", sanitizePublicText(o.managedRepoPath))

	fmt.Fprintln(out, "planned order:")
	fmt.Fprintln(out, "1. preflight checks")
	fmt.Fprintln(out, "2. acquire per-repo run lock")
	fmt.Fprintln(out, "3. prepare stage")
	fmt.Fprintln(out, "   - sync managed repository copy")
	fmt.Fprintln(out, "   - resolve default branch and source branch head")
	fmt.Fprintln(out, "   - create candidate branch from source head")
	if cfg.Mode == ModeYolo {
		fmt.Fprintln(out, "   - if source branch is default branch, run yolo push preflight")
	}
	fmt.Fprintf(out, "4. round loop, up to %d round(s)\n", cfg.MaxRounds)
	fmt.Fprintln(out, "   - independent review stage, run parallel reviewers and collect review markdown reports")
	fmt.Fprintln(out, "   - execute stage, run queue steps in order:")

	queuePath := filepath.Join(o.promptsRoot, "execute", "queue.txt")
	if queue, err := ReadQueue(queuePath); err == nil {
		for idx, templateName := range queue {
			fmt.Fprintf(out, "     %d. %s\n", idx+1, executePromptLabel(templateName))
		}
	} else {
		fmt.Fprintln(out, "     1. consolidate reviews")
		fmt.Fprintln(out, "     2. plan changes")
		fmt.Fprintln(out, "     3. execute and verify")
		fmt.Fprintln(out, "     4. cleanup, summary, commit")
	}

	fmt.Fprintln(out, "   - if execute changed repository files, run another review round")
	fmt.Fprintln(out, "   - if execute made no repository changes, stop additional rounds")
	fmt.Fprintln(out, "5. delivery stage")
	fmt.Fprintln(out, "   - validate delivery files and run secret hygiene scan")
	if cfg.Mode == ModePR {
		fmt.Fprintln(out, "   - push candidate branch and open one pull request into source branch")
	} else {
		fmt.Fprintln(out, "   - push final changes directly to source branch")
	}
	fmt.Fprintln(out, "6. write final summary and print completion output")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "dry-run only: no prompts were executed, no commits were pushed, and no pull request was created.")
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

func isInterruptError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) {
		return true
	}
	var commandErr *CommandExecutionError
	if errors.As(err, &commandErr) && commandErr.Canceled {
		return true
	}
	return false
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
		_, _ = fmt.Fprintf(os.Stdout, "repository reviewed: `%s`\n", sanitizePublicText(repoSlug))
	}
	_, _ = fmt.Fprintf(os.Stdout, "source branch reviewed: `%s`\n", sanitizePublicText(config.SourceBranch))
	if managedRepoPath != "" {
		_, _ = fmt.Fprintf(os.Stdout, "reviewed directory: %s\n", sanitizePublicText(managedRepoPath))
	}
	if isLocalDirectory(config.Repo) {
		_, _ = fmt.Fprintf(os.Stdout, "requested local repo: %s\n", sanitizePublicText(config.Repo))
	}
	_, _ = fmt.Fprintf(os.Stdout, "delivery mode: `%s`\n", sanitizePublicText(config.Mode))
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
		_, _ = fmt.Fprintf(os.Stdout, "run artifacts: %s\n", sanitizePublicText(runRoot))
		_, _ = fmt.Fprintf(os.Stdout, "final summary: %s\n", sanitizePublicText(filepath.Join(runRoot, "final-summary.md")))
		return
	}

	if delivery.Skipped {
		_, _ = fmt.Fprintf(os.Stdout, "delivery skipped: %s\n", sanitizePublicText(delivery.SkipReason))
		_, _ = fmt.Fprintf(os.Stdout, "no push or PR was created because no deliverable repository changes were found.\n")
		_, _ = fmt.Fprintf(os.Stdout, "run artifacts: %s\n", sanitizePublicText(runRoot))
		_, _ = fmt.Fprintf(os.Stdout, "final summary: %s\n", sanitizePublicText(filepath.Join(runRoot, "final-summary.md")))
		return
	}

	switch delivery.Mode {
	case ModePR:
		if strings.TrimSpace(delivery.PRURL) != "" {
			_, _ = fmt.Fprintf(os.Stdout, "PR created: %s\n", sanitizePublicText(delivery.PRURL))
		} else {
			_, _ = fmt.Fprintf(os.Stdout, "delivery completed in PR mode.\n")
		}
	case ModeYolo:
		if strings.TrimSpace(delivery.CommitsURL) != "" {
			_, _ = fmt.Fprintf(os.Stdout, "changes pushed: %s\n", sanitizePublicText(delivery.CommitsURL))
		} else {
			_, _ = fmt.Fprintf(os.Stdout, "delivery completed in YOLO mode.\n")
		}
	}
	_, _ = fmt.Fprintf(os.Stdout, "run artifacts: %s\n", sanitizePublicText(runRoot))
	_, _ = fmt.Fprintf(os.Stdout, "final summary: %s\n", sanitizePublicText(filepath.Join(runRoot, "final-summary.md")))
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
