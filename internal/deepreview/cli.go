package deepreview

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
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
	maxConcurrency             = 64
	maxReviewRounds            = 100
	defaultCodexTimeoutSeconds = 3600
	defaultReviewInactivitySec = 300
	defaultReviewActivityPollS = 15
	defaultReviewMaxRestarts   = 1
)

func ParseReviewArgs(args []string, now time.Time) (ParsedArgs, error) {
	var repoFromPrefix string
	flagArgs := args
	if len(args) > 0 && args[0] != "" && args[0][0] != '-' {
		repoFromPrefix = args[0]
		flagArgs = args[1:]
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
	trustRepo := fs.Bool("trust-repo", false, "run repository code with operator privileges")
	retainRawLogs := fs.Bool("retain-raw-logs", false, "keep raw Codex stdout and stderr logs")

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
	if *concurrency < 1 || *concurrency > maxConcurrency {
		return ParsedArgs{}, NewDeepReviewError("--concurrency must be from 1 to %d", maxConcurrency)
	}
	if *maxRounds < 1 || *maxRounds > maxReviewRounds {
		return ParsedArgs{}, NewDeepReviewError("--max-rounds must be from 1 to %d", maxReviewRounds)
	}
	reviewInactivitySec, err := envNonNegativeInt("DEEPREVIEW_REVIEW_INACTIVITY_SECONDS", defaultReviewInactivitySec)
	if err != nil {
		return ParsedArgs{}, err
	}
	reviewActivityPollS, err := envNonNegativeInt("DEEPREVIEW_REVIEW_ACTIVITY_POLL_SECONDS", defaultReviewActivityPollS)
	if err != nil {
		return ParsedArgs{}, err
	}
	reviewMaxRestarts, err := envNonNegativeInt("DEEPREVIEW_REVIEW_MAX_RESTARTS", defaultReviewMaxRestarts)
	if err != nil {
		return ParsedArgs{}, err
	}
	reviewInactivity, err := durationFromSeconds("DEEPREVIEW_REVIEW_INACTIVITY_SECONDS", reviewInactivitySec)
	if err != nil {
		return ParsedArgs{}, err
	}
	reviewActivityPoll, err := durationFromSeconds("DEEPREVIEW_REVIEW_ACTIVITY_POLL_SECONDS", reviewActivityPollS)
	if err != nil {
		return ParsedArgs{}, err
	}
	codexTimeout, err := durationFromSeconds("codex timeout", defaultCodexTimeoutSeconds)
	if err != nil {
		return ParsedArgs{}, err
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
		ReviewInactivitySec: reviewInactivitySec,
		ReviewActivityPollS: reviewActivityPollS,
		ReviewMaxRestarts:   reviewMaxRestarts,
		Mode:                finalMode,
		TrustRepo:           *trustRepo,
		RetainRawLogs:       *retainRawLogs,
		WorkspaceRoot:       workspaceRoot,
		RunID:               runID,
		GitBin:              gitBin,
		CodexBin:            envOrDefault("DEEPREVIEW_CODEX_BIN", "codex"),
		GhBin:               envOrDefault("DEEPREVIEW_GH_BIN", "gh"),
		CodexTimeoutSeconds: defaultCodexTimeoutSeconds,
		CodexTimeout:        codexTimeout,
		ReviewInactivity:    reviewInactivity,
		ReviewActivityPoll:  reviewActivityPoll,
	}

	return ParsedArgs{Config: cfg, NoTUI: *noTUI}, nil
}

func durationFromSeconds(name string, seconds int) (time.Duration, error) {
	if seconds < 0 {
		return 0, NewDeepReviewError("%s must be >= 0", name)
	}
	limit := int64(math.MaxInt64) / int64(time.Second)
	if int64(seconds) > limit {
		return 0, NewDeepReviewError("%s overflows a duration", name)
	}
	return time.Duration(seconds) * time.Second, nil
}

func validateReviewLimits(cfg *ReviewConfig) error {
	if cfg.Concurrency < 1 || cfg.Concurrency > maxConcurrency {
		return NewDeepReviewError("--concurrency must be from 1 to %d", maxConcurrency)
	}
	if cfg.MaxRounds < 1 || cfg.MaxRounds > maxReviewRounds {
		return NewDeepReviewError("--max-rounds must be from 1 to %d", maxReviewRounds)
	}
	if _, err := durationFromSeconds("DEEPREVIEW_REVIEW_INACTIVITY_SECONDS", cfg.ReviewInactivitySec); err != nil {
		return err
	}
	if _, err := durationFromSeconds("DEEPREVIEW_REVIEW_ACTIVITY_POLL_SECONDS", cfg.ReviewActivityPollS); err != nil {
		return err
	}
	timeout, err := durationFromSeconds("codex timeout", cfg.CodexTimeoutSeconds)
	if err != nil {
		return err
	}
	if cfg.CodexTimeout <= 0 {
		cfg.CodexTimeout = timeout
	}
	if cfg.ReviewInactivity <= 0 && cfg.ReviewInactivitySec > 0 {
		inactivity, err := durationFromSeconds("DEEPREVIEW_REVIEW_INACTIVITY_SECONDS", cfg.ReviewInactivitySec)
		if err != nil {
			return err
		}
		cfg.ReviewInactivity = inactivity
	}
	if cfg.ReviewActivityPoll <= 0 && cfg.ReviewActivityPollS > 0 {
		poll, err := durationFromSeconds("DEEPREVIEW_REVIEW_ACTIVITY_POLL_SECONDS", cfg.ReviewActivityPollS)
		if err != nil {
			return err
		}
		cfg.ReviewActivityPoll = poll
	}
	return nil
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envNonNegativeInt(key string, fallback int) (int, error) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, NewDeepReviewError("%s must be a non-negative integer, got %q", key, raw)
	}
	if value < 0 {
		return 0, NewDeepReviewError("%s must be >= 0, got %d", key, value)
	}
	return value, nil
}

func MainHelpText() string {
	return `deepreview

Deep branch review orchestrator using isolated worktrees plus Codex.

Usage:
  deepreview review [<repo>] [--source-branch <branch>] [options]
  deepreview doctor [<repo>] [--source-branch <branch>] [options]
  deepreview dry-run [<repo>] [--source-branch <branch>] [options]
  deepreview completion [bash|zsh]
  deepreview help
  deepreview --help

Commands:
  review    Run the deepreview pipeline for one source branch.
  doctor    Run non-mutating preflight checks for a planned run.
  dry-run   Print the planned execution order without mutating anything.
  completion Print shell tab-completion script output.
  help      Show this help text.

Get command-specific help:
  deepreview review --help
  deepreview doctor --help
  deepreview dry-run --help
  deepreview completion --help
`
}

func ReviewHelpText() string {
	return fmt.Sprintf(`deepreview review

Run a deep, multi-stage review pipeline against a source branch in an isolated workspace.

What this command does:
  1) Clones/fetches a branch-scoped managed copy of the repository under ~/deepreview (or override env).
  2) Runs independent review workers in isolated worktrees.
  3) Runs two execute-stage prompts in one fresh Codex thread: triage+plan, then implement+verify+finalize.
  4) Repeats rounds up to max rounds while another round is still required.
     A `+"`continue`"+` decision always forces another round.
     Repository changes always force another review round.
     A `+"`stop`"+` with no repository changes ends the loop.
  5) Delivers once at the end:
     - mode=pr (default): runs one fresh Codex delivery stage for final local merge-readiness assessment, validates the exact reviewed history, then deepreview pushes, opens the PR, and performs bounded post-create mergeability validation
     - mode=yolo: pushes directly to source branch

Usage:
  deepreview review [<repo>] [--source-branch <branch>] [--concurrency N] [--max-rounds N] [--mode pr|yolo] [--yolo] [--no-tui] [--trust-repo] [--retain-raw-logs]

Arguments:
  <repo>
    Repository locator. Supported forms:
      - local git repo path (directory containing .git and a supported GitHub or local filesystem origin remote)
      - owner/repo
      - GitHub remote URL (HTTPS, SSH, or SCP-style GitHub clone URL)
    Context:
      Local path mode reads remote.origin.url and clones/fetches in managed workspace.
      deepreview never runs review work directly in your local repo directory.
      PR mode requires a GitHub-backed remote; local filesystem origin remotes are rejected for `+"`--mode pr`"+`.

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
  --concurrency <int>   (default: %d, maximum: %d)
    Number of independent review workers (separate worktrees).
    Higher means more parallel review coverage but more local load.
    A value outside 1 to %d is rejected. deepreview does not reduce it.

  --max-rounds <int>    (default: %d, maximum: %d)
    Maximum review/execute rounds before stopping.
    A value outside 1 to %d is rejected. deepreview does not reduce it.
    `+"`continue`"+` always requires another round.
    Repository changes require another review round even when the decision is `+"`stop`"+`.
    A `+"`stop`"+` with no repository changes ends the loop.
    If another round is still required at max rounds, deepreview fails and asks you to rerun with a higher limit.

  --mode <pr|yolo>      (default: %s)
    Delivery strategy:
      pr   -> open a pull request into the original source branch (GitHub-backed repos only)
      yolo -> push directly to source branch
    Values are case-insensitive.

  --yolo                (default: false)
    Alias for --mode yolo. If set, it overrides --mode.

  --no-tui              (default: false)
    Force structured text progress logs (disables full-screen terminal user interface).
    Default behavior enables terminal user interface when terminal capabilities are valid.

  --trust-repo          (default: false)
    Required before review runs repository instructions, tests, hooks, filters,
    and policy scripts with your user privileges. deepreview is not a sandbox.
    doctor and dry-run show this state and do not execute those scripts.

  --retain-raw-logs     (default: false)
    Keep raw Codex stdout JSONL and stderr logs under this run's directory.
    Those files can contain source, credentials, and prompts. Delete the run
    directory under runs/ when you no longer need them.

	Environment overrides:
  DEEPREVIEW_WORKSPACE_ROOT   (default: ~/deepreview)
    Root for managed repos + run artifacts.

  DEEPREVIEW_CALLER_CWD       (optional)
    Explicit caller working directory override for repo/branch inference.
    This override is honored even when wrappers launch deepreview from another repo
    or a non-repo directory.

  DEEPREVIEW_GIT_BIN          (default: git)
  DEEPREVIEW_CODEX_BIN        (default: codex)
    Fallback `+"`codex`"+` binary override used only when `+"`multicodex`"+` is unavailable.
  DEEPREVIEW_GH_BIN           (default: gh)
    Tool binary overrides (name or absolute path).

  DEEPREVIEW_REQUIRE_MULTICODEX (default: off)
    Require `+"`multicodex exec`"+` for prompt runs. When unset, deepreview prefers
    `+"`multicodex`"+` from PATH when available and otherwise falls back to `+"`codex exec`"+`.

  DEEPREVIEW_REVIEW_INACTIVITY_SECONDS (default: %d)
    Maximum time a worker can run with no evidence of activity before it is restarted.
    Set to 0 to disable inactivity restarts.

  DEEPREVIEW_REVIEW_ACTIVITY_POLL_SECONDS (default: %d)
    Poll interval for activity signals (stdout/stderr output plus filesystem/git-change evidence).

  DEEPREVIEW_REVIEW_MAX_RESTARTS (default: %d)
    Maximum inactivity-triggered restarts per worker before the stage fails.

Operational defaults:
  Codex execution uses your normal local Codex config/profile for model, approvals, and network access.
  Codex prompt timeout per prompt: %ds
  Run ID: auto-generated UTC timestamp + random suffix

Examples:
  deepreview review /path/to/repo --source-branch feature/login --trust-repo
  deepreview review owner/repo --source-branch feature/login --trust-repo --concurrency 6 --max-rounds 2
  deepreview review owner/repo --source-branch feature/login --trust-repo --mode yolo
  deepreview review owner/repo --source-branch feature/login --trust-repo --yolo
  deepreview review owner/repo --source-branch feature/login --trust-repo
  deepreview review owner/repo --source-branch feature/login --trust-repo --no-tui

Troubleshooting:
  - Missing tools: ensure git/codex/(gh for pr mode) are on PATH or set env overrides.
  - Multicodex routing: install/expose `+"`multicodex`"+` on PATH; set DEEPREVIEW_REQUIRE_MULTICODEX=1 to fail instead of falling back to codex.
  - Auth failures: run local auth flows for the launcher deepreview is using (`+"`multicodex status`"+` or `+"`codex login status`"+`) and gh.
  - Terminal rendering issues: pass --no-tui for stable text logs.
  - Installed binaries use the embedded prompt pack. Set DEEPREVIEW_PROMPTS_ROOT to replace it. Set DEEPREVIEW_CALLER_CWD when a launcher changes directories. deepreview does not read OLDPWD.
  - To stop a run safely at any time, press Ctrl+C once (deepreview cancels and runs cleanup).
  - Invalid mode: allowed values are only pr or yolo (case-insensitive).
`, defaultConcurrency, maxConcurrency, maxConcurrency, defaultMaxRounds, maxReviewRounds, maxReviewRounds, ModePR, defaultReviewInactivitySec, defaultReviewActivityPollS, defaultReviewMaxRestarts, defaultCodexTimeoutSeconds)
}

func DoctorHelpText() string {
	return fmt.Sprintf(`deepreview doctor

Run non-mutating preflight checks for a planned deepreview run.

What this checks:
  - required tools are available on PATH
  - the selected Codex launcher is runnable
  - prompt templates and execute queue are present
  - launcher auth status is healthy (`+"`multicodex status`"+` or `+"`codex login status`"+`)
  - GitHub auth status is healthy (when mode=pr)
  - source branch is reachable on remote
  - whether --trust-repo is set; this check is informational and does not execute repository scripts

Usage:
  deepreview doctor [<repo>] [--source-branch <branch>] [--concurrency N] [--max-rounds N] [--mode pr|yolo] [--yolo] [--trust-repo]

Notes:
  - doctor does not clone, commit, push, open PRs, or create a run directory.
  - doctor reports whether --trust-repo is set. It does not execute repository instructions, tests, hooks, filters, or policy scripts.
  - review requires --trust-repo because that execution uses operator privileges and is not a sandbox.
  - argument inference and validation follow the same rules as "deepreview review".
  - `+"`--mode pr`"+` requires a GitHub-backed repo identity.

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
  deepreview dry-run [<repo>] [--source-branch <branch>] [--concurrency N] [--max-rounds N] [--mode pr|yolo] [--yolo] [--trust-repo]

Notes:
  - dry-run does not run Codex, does not mutate git state, does not push, and does not create a run directory.
  - dry-run reports whether --trust-repo is set. It does not execute repository instructions, tests, hooks, filters, or policy scripts.
  - argument inference and validation follow the same rules as "deepreview review".
  - `+"`--mode pr`"+` requires a GitHub-backed repo identity.

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

func CompletionHelpText() string {
	return `deepreview completion

Print a shell completion script for deepreview.

Usage:
  deepreview completion [bash|zsh]

Defaults:
  If shell is omitted, output defaults to bash.

Examples:
  deepreview completion bash > ~/.local/share/bash-completion/completions/deepreview
  mkdir -p ~/.zsh/completions
  deepreview completion zsh > ~/.zsh/completions/_deepreview
`
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
	case "completion":
		return runCompletionCommand(argv[1:])
	default:
		fmt.Fprintf(os.Stderr, "deepreview error: unsupported command: %s\n", argv[0])
		PrintUsage()
		return 1
	}
}

func runCompletionCommand(args []string) int {
	if len(args) >= 1 && isHelpArg(args[0]) {
		fmt.Fprint(os.Stdout, CompletionHelpText())
		return 0
	}
	if len(args) > 1 {
		fmt.Fprintln(os.Stderr, "deepreview error: completion accepts zero or one shell argument (bash or zsh)")
		return 1
	}
	shell := "bash"
	if len(args) == 1 {
		shell = strings.ToLower(strings.TrimSpace(args[0]))
	}
	script, err := completionScript(shell)
	if err != nil {
		fmt.Fprintf(os.Stderr, "deepreview error: %s\n", err.Error())
		return 1
	}
	fmt.Fprint(os.Stdout, script)
	return 0
}

func completionScript(shell string) (string, error) {
	switch shell {
	case "bash":
		return `# bash completion for deepreview
_deepreview_completion() {
  local cur prev words cword
  _init_completion || return
  local commands="review doctor dry-run completion help"
  if [[ ${cword} -eq 1 ]]; then
    COMPREPLY=( $(compgen -W "${commands}" -- "${cur}") )
    return
  fi
  case "${words[1]}" in
    completion)
      COMPREPLY=( $(compgen -W "bash zsh" -- "${cur}") )
      ;;
    review|doctor|dry-run)
      COMPREPLY=( $(compgen -W "--source-branch --concurrency --max-rounds --mode --yolo --no-tui --trust-repo --retain-raw-logs" -- "${cur}") )
      ;;
    *)
      COMPREPLY=( $(compgen -W "${commands}" -- "${cur}") )
      ;;
  esac
}
complete -F _deepreview_completion deepreview
`, nil
	case "zsh":
		return `#compdef deepreview
_deepreview() {
  local -a commands
  commands=(
    'review:run deep review pipeline for one source branch'
    'doctor:run non-mutating preflight checks'
    'dry-run:print planned execution order without mutating state'
    'completion:print shell completion script'
    'help:show help text'
  )
  if (( CURRENT == 2 )); then
    _describe 'command' commands
    return
  fi
  case "${words[2]}" in
    completion)
      _values 'shell' bash zsh
      ;;
    review|doctor|dry-run)
      _values 'flag' --source-branch --concurrency --max-rounds --mode --yolo --no-tui --trust-repo --retain-raw-logs
      ;;
  esac
}
_deepreview "$@"
`, nil
	default:
		return "", NewDeepReviewError("unsupported shell %q (expected bash or zsh)", shell)
	}
}

func runReviewCommand(args []string) int {
	if len(args) >= 1 && isHelpArg(args[0]) {
		fmt.Fprint(os.Stderr, ReviewHelpText())
		return 0
	}

	runCtx, cancelRun := context.WithCancel(context.Background())
	defer cancelRun()
	commandSession := newProcessSession()
	runCtx = contextWithProcessSession(runCtx, commandSession)
	restoreCommandContext := setRunCommandContext(runCtx)
	defer restoreCommandContext()

	var interruptCount atomic.Int32
	var cancelOnce sync.Once
	requestCancel := func() {
		cancelOnce.Do(func() {
			cancelRun()
			commandSession.terminateProcesses()
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
				_, _ = fmt.Fprintln(os.Stderr, "deepreview: cancellation already requested; waiting for cleanup...")
				requestCancel()
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
			printFailureArtifactSummary(os.Stderr, nil, ReviewConfig{})
			fmt.Fprintln(os.Stderr, "deepreview: run canceled by user; cleanup completed")
			return 130
		}
		fmt.Fprintf(os.Stderr, "deepreview error: %s\n", err.Error())
		return 1
	}
	if !parsed.Config.TrustRepo {
		fmt.Fprintf(os.Stderr, "deepreview error: %s\n", repositoryTrustRequiredMessage)
		return 1
	}
	if err := validateLocalBranchReadyForRemoteReview(parsed.Config.GitBin, parsed.Config.Repo, parsed.Config.SourceBranch); err != nil {
		if isInterruptError(err) || interruptCount.Load() > 0 {
			reportCanceledRun(nil, parsed.Config, filepath.Join(parsed.Config.WorkspaceRoot, "runs", parsed.Config.RunID), parsed.Config.GitBin)
			return 130
		}
		fmt.Fprintf(os.Stderr, "deepreview error: %s\n", err.Error())
		return 1
	}

	stdoutFD := int(os.Stdout.Fd())
	stdinFD := int(os.Stdin.Fd())
	stdinIsTerminal := term.IsTerminal(stdinFD)
	stdoutIsTerminal := term.IsTerminal(stdoutFD)
	termWidth, termHeight, sizeErr := term.GetSize(stdoutFD)
	enableTUI := shouldEnableTUI(
		parsed.NoTUI,
		stdinIsTerminal,
		stdoutIsTerminal,
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
		fmt.Fprintf(os.Stderr, "deepreview error: %s\n", err.Error())
		return 1
	}
	orchestrator.commandCtx = runCtx

	if enableTUI {
		err = RunTUIWithWorker(state, termWidth, termHeight, requestCancel, func() error { return orchestrator.Run() })
	} else {
		err = orchestrator.Run()
	}
	if err != nil {
		if isInterruptError(err) || interruptCount.Load() > 0 {
			reportCanceledRun(orchestrator, parsed.Config, orchestrator.RunRoot(), parsed.Config.GitBin)
			return 130
		}
		if isIncompleteRunError(err) {
			fmt.Fprintf(os.Stderr, "deepreview: run ended incomplete: %s\n", err.Error())
			if diag := orchestrator.SummaryDiagnostic(); diag != nil {
				fmt.Fprintf(os.Stderr, "deepreview: run summary diagnostic: %s\n", diag.Error())
			}
			if enableTUI {
				clearTerminalForCompletionSummary(os.Stdout)
			}
			printCompletionSummary(orchestrator, parsed.Config)
			return exitCodeIncomplete
		}
		fmt.Fprintf(os.Stderr, "deepreview error: %s\n", err.Error())
		if shouldPrintFailureArtifactSummary(err) {
			printFailureArtifactSummary(os.Stderr, orchestrator, parsed.Config)
		}
		return 1
	}
	if interruptCount.Load() > 0 {
		reportCanceledRun(orchestrator, parsed.Config, orchestrator.RunRoot(), parsed.Config.GitBin)
		return 130
	}
	if enableTUI {
		clearTerminalForCompletionSummary(os.Stdout)
	}
	if diag := orchestrator.SummaryDiagnostic(); diag != nil {
		fmt.Fprintf(os.Stderr, "deepreview: run summary diagnostic: %s\n", diag.Error())
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
		fmt.Fprintf(os.Stderr, "deepreview error: %s\n", err.Error())
		return 1
	}
	orchestrator, err := NewOrchestrator(parsed.Config, &NullProgressReporter{})
	if err != nil {
		fmt.Fprintf(os.Stderr, "deepreview error: %s\n", err.Error())
		return 1
	}

	checks := buildDoctorChecks(orchestrator)
	fmt.Fprintf(os.Stdout, "deepreview doctor\n")
	fmt.Fprintf(os.Stdout, "repo: %s\n", orchestrator.repoIdentity.Slug())
	fmt.Fprintf(os.Stdout, "source branch: %s\n", parsed.Config.SourceBranch)
	fmt.Fprintf(os.Stdout, "mode: %s\n", parsed.Config.Mode)
	fmt.Fprintf(os.Stdout, "workspace root: %s\n\n", orchestrator.workspaceRoot)

	passedAll := true
	for _, check := range checks {
		state := "ok"
		if !check.Passed {
			state = "fail"
			passedAll = false
		}
		fmt.Fprintf(os.Stdout, "[%s] %s", state, check.Name)
		if strings.TrimSpace(check.Detail) != "" {
			fmt.Fprintf(os.Stdout, " - %s", check.Detail)
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
	checks := make([]doctorCheck, 0, 12)
	checks = append(checks, doctorCheck{
		Name:   "repository execution trust",
		Passed: true,
		Detail: repositoryTrustDoctorDetail(cfg.TrustRepo),
	})

	for _, tool := range requiredHostTools(cfg) {
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

	launcher, launcherErr := o.codexRunner.resolveLauncher()
	if launcherErr != nil {
		checks = append(checks, doctorCheck{
			Name:   "codex launcher",
			Passed: false,
			Detail: launcherErr.Error(),
		})
	} else {
		launcherHelp, err := RunCommand(launcher.withArgs("exec", "--help"), "", "", false, 20*time.Second)
		if err != nil {
			checks = append(checks, doctorCheck{
				Name:   "codex launcher",
				Passed: false,
				Detail: err.Error(),
			})
		} else if launcherHelp.ReturnCode != 0 {
			checks = append(checks, doctorCheck{
				Name:   "codex launcher",
				Passed: false,
				Detail: firstOutputLine(launcherHelp.Stdout, launcherHelp.Stderr),
			})
		} else {
			checks = append(checks, doctorCheck{
				Name:   "codex launcher",
				Passed: true,
				Detail: fmt.Sprintf("%s exec available", launcher.Display),
			})
		}
	}

	if err := o.checkPromptTemplates(); err != nil {
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

	if launcherErr != nil {
		checks = append(checks, doctorCheck{
			Name:   "codex auth status",
			Passed: false,
			Detail: launcherErr.Error(),
		})
	} else if launcher.Display == "multicodex" {
		multicodexStatus, multicodexErr := RunCommand(launcher.withArgs("status"), "", "", false, 20*time.Second)
		if multicodexErr != nil {
			checks = append(checks, doctorCheck{
				Name:   "multicodex status",
				Passed: false,
				Detail: multicodexErr.Error(),
			})
		} else if multicodexStatus.ReturnCode != 0 {
			checks = append(checks, doctorCheck{
				Name:   "multicodex status",
				Passed: false,
				Detail: firstOutputLine(multicodexStatus.Stdout, multicodexStatus.Stderr),
			})
		} else if !strings.Contains(strings.ToLower(multicodexStatus.Stdout), "logged-in") {
			checks = append(checks, doctorCheck{
				Name:   "multicodex status",
				Passed: false,
				Detail: "no logged-in multicodex profiles detected",
			})
		} else {
			checks = append(checks, doctorCheck{
				Name:   "multicodex status",
				Passed: true,
				Detail: "at least one logged-in profile available",
			})
		}
	} else {
		codexStatus, codexErr := RunCommand(launcher.withArgs("login", "status"), "", "", false, 20*time.Second)
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
	lsRemote, lsRemoteErr := inspectRemote(cfg.GitBin, o.repoIdentity.CloneSource, sourceRef, 30*time.Second)
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
	if err := validateLocalBranchReadyForRemoteReview(cfg.GitBin, cfg.Repo, cfg.SourceBranch); err != nil {
		checks = append(checks, doctorCheck{
			Name:   "local source branch ready for review",
			Passed: false,
			Detail: err.Error(),
		})
	} else {
		checks = append(checks, doctorCheck{
			Name:   "local source branch ready for review",
			Passed: true,
			Detail: "ready",
		})
	}

	return checks
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
	return trimForDisplay(line, 220)
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
		fmt.Fprintf(os.Stderr, "deepreview error: %s\n", err.Error())
		return 1
	}
	orchestrator, err := NewOrchestrator(parsed.Config, &NullProgressReporter{})
	if err != nil {
		fmt.Fprintf(os.Stderr, "deepreview error: %s\n", err.Error())
		return 1
	}
	if err := validateLocalBranchReadyForRemoteReview(parsed.Config.GitBin, parsed.Config.Repo, parsed.Config.SourceBranch); err != nil {
		fmt.Fprintf(os.Stderr, "deepreview error: %s\n", err.Error())
		return 1
	}
	if err := orchestrator.checkPromptTemplates(); err != nil {
		fmt.Fprintf(os.Stderr, "deepreview error: %s\n", err.Error())
		return 1
	}
	printDryRunPlan(os.Stdout, orchestrator)
	return 0
}

func printDryRunPlan(out io.Writer, o *Orchestrator) {
	cfg := o.config
	fmt.Fprintln(out, "deepreview dry-run")
	fmt.Fprintf(out, "repo: %s\n", o.repoIdentity.Slug())
	fmt.Fprintf(out, "source branch: %s\n", cfg.SourceBranch)
	fmt.Fprintf(out, "mode: %s\n", cfg.Mode)
	fmt.Fprintf(out, "repository execution trust: %s\n", repositoryTrustStatus(cfg.TrustRepo))
	fmt.Fprintln(out, "dry-run does not execute repository instructions, tests, hooks, filters, or policy scripts.")
	fmt.Fprintf(out, "concurrency: %d\n", cfg.Concurrency)
	fmt.Fprintf(out, "max rounds: %d\n", cfg.MaxRounds)
	fmt.Fprintf(out, "workspace root: %s\n", o.workspaceRoot)
	fmt.Fprintf(out, "managed repo path: %s\n\n", o.managedRepoPath)

	fmt.Fprintln(out, "planned order:")
	fmt.Fprintln(out, "1. preflight checks")
	fmt.Fprintln(out, "2. acquire per-repo+branch run lock")
	fmt.Fprintln(out, "3. prepare stage")
	fmt.Fprintln(out, "   - sync branch-scoped managed repository copy")
	fmt.Fprintln(out, "   - resolve default branch and source branch head")
	fmt.Fprintln(out, "   - create candidate branch from source head")
	if cfg.Mode == ModeYolo {
		fmt.Fprintln(out, "   - if source branch is default branch, run yolo push preflight")
	}
	fmt.Fprintf(out, "4. round loop, up to %d round(s)\n", cfg.MaxRounds)
	fmt.Fprintln(out, "   - independent review stage, run parallel reviewers and collect review markdown reports")
	fmt.Fprintln(out, "   - execute stage, run queue steps in order:")

	if queue, err := o.readExecuteQueue(); err == nil {
		for idx, templateName := range queue {
			fmt.Fprintf(out, "     %d. %s\n", idx+1, executePromptLabel(templateName))
		}
	} else {
		fmt.Fprintln(out, "     1. triage and plan")
		fmt.Fprintln(out, "     2. implement, verify, finalize")
	}

	fmt.Fprintln(out, "   - if round status is `continue`, run another review round")
	fmt.Fprintln(out, "   - if execute changed the repository, run another review round")
	fmt.Fprintln(out, "   - if round status is `stop` and execute made no repository changes, stop the round loop")
	fmt.Fprintln(out, "5. delivery stage")
	if cfg.Mode == ModePR {
		fmt.Fprintln(out, "   - validate every outgoing commit and delivery file")
		fmt.Fprintln(out, "   - run one fresh Codex delivery stage")
		fmt.Fprintln(out, "   - confirm merge-readiness without mutating tracked repository content or history")
		fmt.Fprintln(out, "   - push the reviewed candidate branch to the deepreview PR branch, open the PR, and wait briefly for mergeability to settle")
	} else {
		fmt.Fprintln(out, "   - validate delivery files")
		fmt.Fprintln(out, "   - run one fresh Codex delivery stage and push the source branch after final verification")
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

func clearTerminalForCompletionSummary(out io.Writer) {
	if out == nil {
		return
	}
	// Clear full screen and move cursor to the top-left before summary output.
	_, _ = io.WriteString(out, "\x1b[2J\x1b[H")
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

func shouldPrintFailureArtifactSummary(err error) bool {
	return err != nil
}

func reportCanceledRun(orchestrator *Orchestrator, config ReviewConfig, runRoot, gitBin string) {
	cleanupErr := cleanupInterruptedRunArtifacts(runRoot, gitBin)
	printFailureArtifactSummary(os.Stderr, orchestrator, config)
	if cleanupErr != nil {
		fmt.Fprintf(os.Stderr, "deepreview: cleanup incomplete: %s\n", cleanupErr)
		return
	}
	fmt.Fprintln(os.Stderr, "deepreview: run canceled by user; cleanup completed")
}

func cleanupInterruptedRunArtifacts(runRoot, gitBin string) error {
	if strings.TrimSpace(runRoot) == "" {
		return nil
	}
	if strings.TrimSpace(gitBin) == "" {
		gitBin = "git"
	}
	_ = mkdirPrivate(runRoot)
	waitForActiveCommandsToExit(15 * time.Second)
	deadline := time.Now().Add(worktreeCleanupBudget)
	var lastErr error
	for {
		var cleanupErr error
		for _, path := range worktreeDirsUnder(runRoot) {
			repoPath, ownedErr := discoverWorktreeRepository(path)
			if ownedErr != nil {
				if _, err := removeEmptyWorktreeDir(runRoot, path); err != nil {
					cleanupErr = errors.Join(cleanupErr, err)
				}
				continue
			}
			if err := RemoveWorktree(repoPath, gitBin, path); err != nil {
				cleanupErr = errors.Join(cleanupErr, err)
			}
		}
		remaining := ownedWorktreeDirsUnder(runRoot)
		if cleanupErr == nil && len(remaining) == 0 {
			return nil
		}
		if cleanupErr != nil {
			lastErr = cleanupErr
		} else {
			lastErr = NewDeepReviewError("worktree cleanup left residual paths: %s", strings.Join(remaining, ", "))
		}
		if time.Now().After(deadline) {
			return lastErr
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func worktreeDirsUnder(root string) []string {
	paths := make([]string, 0)
	_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil || path == root {
			return nil
		}
		if entry.IsDir() && filepath.Base(path) == "worktree" {
			paths = append(paths, path)
			return filepath.SkipDir
		}
		return nil
	})
	return paths
}

func ownedWorktreeDirsUnder(root string) []string {
	remaining := make([]string, 0)
	for _, path := range worktreeDirsUnder(root) {
		if _, err := discoverWorktreeRepository(path); err == nil {
			remaining = append(remaining, path)
		}
	}
	return remaining
}

func removeEmptyWorktreeDir(runRoot, path string) (bool, error) {
	if !pathIsWithinDir(runRoot, path) {
		return false, NewDeepReviewError("refusing to clean a worktree outside the run root: %s", path)
	}
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return true, nil
		}
		return false, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return false, nil
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return false, err
	}
	if len(entries) != 0 {
		return false, nil
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return false, err
	}
	return true, nil
}

func printFailureArtifactSummary(out io.Writer, orchestrator *Orchestrator, config ReviewConfig) {
	if out == nil {
		return
	}
	runRoot := ""
	repoSlug := strings.TrimSpace(config.Repo)
	if orchestrator != nil {
		runRoot = orchestrator.RunRoot()
		if candidate := strings.TrimSpace(orchestrator.RepoSlug()); strings.Trim(candidate, "/") != "" {
			repoSlug = candidate
		}
	}
	if strings.TrimSpace(runRoot) == "" {
		runRoot = filepath.Join(config.WorkspaceRoot, "runs", config.RunID)
	}
	logsPath := filepath.Join(runRoot, "logs")
	if strings.Trim(repoSlug, "/") == "" {
		repoSlug = ""
	}
	reviewSnapshot := readCompletionReviewSnapshot(runRoot)

	_, _ = fmt.Fprintln(out, "deepreview failure summary:")
	_, _ = fmt.Fprintf(out, "outcome: %s\n", outcomeFailed)
	if strings.TrimSpace(config.RunID) != "" {
		_, _ = fmt.Fprintf(out, "run: `%s`\n", config.RunID)
	}
	if repoSlug != "" {
		_, _ = fmt.Fprintf(out, "repository reviewed: `%s`\n", repoSlug)
	}
	if strings.TrimSpace(config.SourceBranch) != "" {
		_, _ = fmt.Fprintf(out, "source branch reviewed: `%s`\n", config.SourceBranch)
	}
	if reviewSnapshot.CompletedRounds > 0 {
		_, _ = fmt.Fprintf(out, "review rounds completed before exit: %d\n", reviewSnapshot.CompletedRounds)
	}
	if reviewSnapshot.HasFinalStatus {
		_, _ = fmt.Fprintf(out, "latest review decision: %s\n", formatFinalReviewStatus(reviewSnapshot.FinalStatus))
		if reason := formatFinalReviewReason(reviewSnapshot.FinalStatus); reason != "" {
			_, _ = fmt.Fprintf(out, "latest review summary: %s\n", reason)
		}
	}
	var delivery *DeliveryResult
	if orchestrator != nil {
		delivery = orchestrator.LastDelivery()
	}
	state := publicationNotAttempted
	if delivery != nil {
		state = delivery.publicationState()
	}
	switch {
	case delivery == nil || delivery.Skipped || state == publicationNotAttempted:
		_, _ = fmt.Fprintln(out, "run exited before delivery; no push or PR was created.")
	case state == publicationPushed:
		_, _ = fmt.Fprintln(out, "delivery branch was pushed; pull request was not created.")
		if delivery.showCommitsURL() {
			_, _ = fmt.Fprintf(out, "latest delivery commits: %s\n", delivery.CommitsURL)
		}
	default:
		_, _ = fmt.Fprintln(out, "run failed after delivery artifacts were created.")
		if delivery.showPRURL() {
			_, _ = fmt.Fprintf(out, "latest PR: %s\n", delivery.PRURL)
		}
		if delivery.showCommitsURL() {
			_, _ = fmt.Fprintf(out, "latest delivery commits: %s\n", delivery.CommitsURL)
		}
	}
	_, _ = fmt.Fprintln(out, "inspect these paths to review what deepreview produced:")
	_, _ = fmt.Fprintf(out, "run artifacts: %s\n", runRoot)
	_, _ = fmt.Fprintf(out, "run health: %s\n", filepath.Join(runRoot, runHealthMDName))
	_, _ = fmt.Fprintf(out, "logs: %s\n", logsPath)
	_, _ = fmt.Fprintf(out, "reviews: %s\n", filepath.Join(runRoot, "round-*", "review-*.md"))
	_, _ = fmt.Fprintf(out, "round artifacts: %s\n", filepath.Join(runRoot, "round-*", "round-*.md"))
	_, _ = fmt.Fprintf(out, "round records: %s\n", filepath.Join(runRoot, "round-*", "round.json"))
	_, _ = fmt.Fprintf(out, "round status files: %s\n", filepath.Join(runRoot, "round-*", "round-status.json"))
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
	finalSummaryPath := filepath.Join(runRoot, "final-summary.md")
	runHealthPath := filepath.Join(runRoot, runHealthMDName)
	printArtifactPaths := func() {
		_, _ = fmt.Fprintf(os.Stdout, "run artifacts: %s\n", runRoot)
		_, _ = fmt.Fprintf(os.Stdout, "final summary: %s\n", finalSummaryPath)
		_, _ = fmt.Fprintf(os.Stdout, "run health: %s\n", runHealthPath)
	}

	label := "completed"
	outcomeName := outcomeComplete
	if delivery != nil && delivery.Skipped {
		outcomeName = outcomeNoChange
	}
	if delivery != nil && delivery.Incomplete {
		label = "incomplete"
		outcomeName = outcomeIncomplete
	}
	_, _ = fmt.Fprintf(os.Stdout, "deepreview %s: run `%s`\n", label, config.RunID)
	_, _ = fmt.Fprintf(os.Stdout, "outcome: %s\n", outcomeName)
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
		if nextFocus := formatFinalReviewNextFocus(reviewSnapshot.FinalStatus); nextFocus != "" {
			_, _ = fmt.Fprintf(os.Stdout, "next review focus: %s\n", nextFocus)
		}
	}
	if delivery == nil {
		printArtifactPaths()
		return
	}

	if delivery.Skipped {
		_, _ = fmt.Fprintf(os.Stdout, "delivery skipped: %s\n", delivery.SkipReason)
		_, _ = fmt.Fprintf(os.Stdout, "no push or PR was created because no deliverable repository changes were found.\n")
		printArtifactPaths()
		return
	}

	if delivery.Incomplete {
		if delivery.showPRURL() {
			_, _ = fmt.Fprintf(os.Stdout, "Draft PR created: %s\n", delivery.PRURL)
		}
		if strings.TrimSpace(delivery.IncompleteReason) != "" {
			_, _ = fmt.Fprintf(os.Stdout, "delivery status: incomplete (%s)\n", delivery.IncompleteReason)
		} else {
			_, _ = fmt.Fprintf(os.Stdout, "delivery status: incomplete\n")
		}
		if delivery.showCommitsURL() {
			_, _ = fmt.Fprintf(os.Stdout, "delivery commits: %s\n", delivery.CommitsURL)
		}
		printArtifactPaths()
		return
	}

	switch delivery.Mode {
	case ModePR:
		if delivery.showPRURL() {
			_, _ = fmt.Fprintf(os.Stdout, "PR created: %s\n", delivery.PRURL)
		}
		if delivery.showCommitsURL() {
			_, _ = fmt.Fprintf(os.Stdout, "delivery commits: %s\n", delivery.CommitsURL)
		}
	case ModeYolo:
		if delivery.showCommitsURL() {
			_, _ = fmt.Fprintf(os.Stdout, "changes pushed: %s\n", delivery.CommitsURL)
		} else {
			_, _ = fmt.Fprintf(os.Stdout, "delivery completed in YOLO mode.\n")
		}
	}
	printArtifactPaths()
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
	recordPaths, err := filepath.Glob(filepath.Join(runRoot, "round-*", "round.json"))
	if err != nil {
		return snapshot
	}
	sort.Strings(recordPaths)
	for _, recordPath := range recordPaths {
		record, err := readRoundRecord(recordPath)
		if err != nil {
			continue
		}
		snapshot.CompletedRounds++
		snapshot.FinalStatus = record.Status
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
	return trimForDisplay(reason, 220)
}

func formatFinalReviewNextFocus(status RoundStatus) string {
	if status.NextFocus == nil {
		return ""
	}
	nextFocus := strings.TrimSpace(strings.ReplaceAll(*status.NextFocus, "\n", " "))
	if nextFocus == "" {
		return ""
	}
	return trimForDisplay(nextFocus, 180)
}

func isLocalDirectory(path string) bool {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return false
	}
	st, err := os.Stat(trimmed)
	return err == nil && st.IsDir()
}
