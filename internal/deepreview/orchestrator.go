package deepreview

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"
)

type Orchestrator struct {
	config                ReviewConfig
	toolRoot              string
	promptsRoot           string
	workspaceRoot         string
	runRoot               string
	repoIdentity          RepoIdentity
	managedRepoPath       string
	codexRunner           CodexRunner
	reporter              ProgressReporter
	pushCount             int
	lastDelivery          *DeliveryResult
	prDelivery            prDeliveryState
	runLockPath           string
	commitIdentity        CommitIdentity
	reviewedCandidateHead string
}

type prDeliveryState struct {
	branch  string
	refspec string
	pushed  bool
	prURL   string
}

type promptDeliveryResult struct {
	Mode             string `json:"mode"`
	DeliveryBranch   string `json:"delivery_branch,omitempty"`
	Incomplete       bool   `json:"incomplete,omitempty"`
	IncompleteReason string `json:"incomplete_reason,omitempty"`
}

const (
	stageHeartbeatInterval       = 15 * time.Second
	mergeReadyPRPollInterval     = 1 * time.Second
	mergeReadyPRValidationWindow = 30 * time.Second
	deliveryRecoveryRoundBudget  = 2
)

type executeRoundOptions struct {
	modeNote            string
	executeModeOverride string
}

type pullRequestMergeState struct {
	URL              string `json:"url"`
	State            string `json:"state"`
	IsDraft          bool   `json:"isDraft"`
	Mergeable        string `json:"mergeable"`
	MergeStateStatus string `json:"mergeStateStatus"`
}

func NewOrchestrator(config ReviewConfig, reporter ProgressReporter) (*Orchestrator, error) {
	workspaceRoot, err := filepath.Abs(config.WorkspaceRoot)
	if err != nil {
		return nil, err
	}
	promptsRoot, toolRoot, err := findPromptsRoot()
	if err != nil {
		return nil, err
	}

	repoIdentity, err := resolveRepoIdentity(config, config.Repo)
	if err != nil {
		return nil, err
	}
	if err := validateDeliveryModeRepoIdentity(config.Mode, repoIdentity); err != nil {
		return nil, err
	}

	if reporter == nil {
		reporter = &NullProgressReporter{}
	}

	runRoot := filepath.Join(workspaceRoot, "runs", config.RunID)
	if err := os.MkdirAll(runRoot, 0o755); err != nil {
		return nil, err
	}
	managedRepoPath := filepath.Join(
		append(
			[]string{workspaceRoot, "repos"},
			append(repoIdentity.NamespaceSegments(), "branches", FilesystemSafeKey(config.SourceBranch))...,
		)...,
	)
	if config.CodexTimeout <= 0 {
		config.CodexTimeout = time.Duration(config.CodexTimeoutSeconds) * time.Second
	}
	if config.ReviewInactivity <= 0 && config.ReviewInactivitySec > 0 {
		config.ReviewInactivity = time.Duration(config.ReviewInactivitySec) * time.Second
	}
	if config.ReviewActivityPoll <= 0 && config.ReviewActivityPollS > 0 {
		config.ReviewActivityPoll = time.Duration(config.ReviewActivityPollS) * time.Second
	}
	return &Orchestrator{
		config:          config,
		toolRoot:        toolRoot,
		promptsRoot:     promptsRoot,
		workspaceRoot:   workspaceRoot,
		runRoot:         runRoot,
		repoIdentity:    repoIdentity,
		managedRepoPath: managedRepoPath,
		codexRunner: CodexRunner{
			CodexBin: config.CodexBin,
			Timeout:  config.CodexTimeout,
		},
		reporter: reporter,
	}, nil
}

func (o *Orchestrator) RunRoot() string {
	return o.runRoot
}

func (o *Orchestrator) RepoSlug() string {
	return o.repoIdentity.Slug()
}

func (o *Orchestrator) ManagedRepoPath() string {
	return o.managedRepoPath
}

func (o *Orchestrator) LastDelivery() *DeliveryResult {
	if o.lastDelivery == nil {
		return nil
	}
	copyValue := *o.lastDelivery
	return &copyValue
}

func findPromptsRoot() (string, string, error) {
	if override := os.Getenv("DEEPREVIEW_PROMPTS_ROOT"); override != "" {
		prompts := filepath.Clean(override)
		if st, err := os.Stat(prompts); err == nil && st.IsDir() {
			return prompts, filepath.Dir(prompts), nil
		}
		return "", "", NewDeepReviewError("prompts root not found: %s", prompts)
	}

	_, thisFile, _, ok := runtime.Caller(0)
	if ok {
		candidate := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", "..", "prompts"))
		if st, err := os.Stat(candidate); err == nil && st.IsDir() {
			abs, err := filepath.Abs(candidate)
			if err != nil {
				return "", "", err
			}
			return abs, filepath.Dir(abs), nil
		}
	}

	return "", "", NewDeepReviewError("unable to locate prompts root; set DEEPREVIEW_PROMPTS_ROOT")
}

func resolveRepoIdentity(config ReviewConfig, repo string) (RepoIdentity, error) {
	repoPath := filepath.Clean(repo)
	if st, err := os.Stat(repoPath); err == nil && st.IsDir() {
		if gitSt, err := os.Stat(filepath.Join(repoPath, ".git")); err == nil && gitSt != nil {
			remote, err := tryReadRemoteURL(config.GitBin, repoPath)
			if err != nil {
				return RepoIdentity{}, err
			}
			remote = strings.TrimSpace(remote)
			if remote == "" {
				return RepoIdentity{}, NewDeepReviewError("local repo input must have remote.origin.url configured: %s", repoPath)
			}
			if localCloneSource, ok := localCloneSource(remote, repoPath); ok {
				return RepoIdentity{
					SourceType:  RepoSourceFilesystem,
					Name:        filesystemRepoDisplayName(localCloneSource),
					CloneSource: localCloneSource,
				}, nil
			}
			owner, name, ok := parseOwnerRepo(remote)
			if ok {
				return RepoIdentity{
					SourceType:  RepoSourceGitHub,
					Owner:       owner,
					Name:        name,
					CloneSource: remote,
				}, nil
			}
			return RepoIdentity{}, NewDeepReviewError("local repo input must have a supported GitHub or local filesystem origin remote: %s", repoPath)
		}
	}

	if owner, name, ok := parseOwnerRepo(repo); ok {
		source := strings.TrimSpace(repo)
		if isOwnerRepoSlug(source) {
			source = fmt.Sprintf("https://github.com/%s/%s.git", owner, name)
		}
		return RepoIdentity{
			SourceType:  RepoSourceGitHub,
			Owner:       owner,
			Name:        name,
			CloneSource: source,
		}, nil
	}

	return RepoIdentity{}, NewDeepReviewError("unable to resolve repo locator: %s", repo)
}

func filesystemRepoDisplayName(cloneSource string) string {
	base := strings.TrimSpace(filepath.Base(filepath.Clean(cloneSource)))
	base = strings.TrimSuffix(base, ".git")
	name := SanitizeSegment(base)
	if name == "" || name == "." || name == "value" {
		return FilesystemSafeKey(cloneSource)
	}
	return name
}

func localCloneSource(remote, repoPath string) (string, bool) {
	trimmed := strings.TrimSpace(remote)
	if trimmed == "" {
		return "", false
	}
	if strings.HasPrefix(trimmed, "file://") {
		parsed, err := url.Parse(trimmed)
		if err != nil || parsed.Scheme != "file" || strings.TrimSpace(parsed.Path) == "" {
			return "", false
		}
		if abs, err := filepath.Abs(filepath.Clean(parsed.Path)); err == nil {
			return abs, true
		}
		return "", false
	}
	if filepath.IsAbs(trimmed) {
		if abs, err := filepath.Abs(filepath.Clean(trimmed)); err == nil {
			return abs, true
		}
		return "", false
	}
	candidate := filepath.Join(repoPath, trimmed)
	if _, err := os.Stat(candidate); err == nil {
		if abs, err := filepath.Abs(filepath.Clean(candidate)); err == nil {
			return abs, true
		}
		return "", false
	}
	return "", false
}

func validateDeliveryModeRepoIdentity(mode string, repoIdentity RepoIdentity) error {
	if strings.TrimSpace(mode) != ModePR {
		return nil
	}
	if repoIdentity.SupportsPRDelivery() {
		return nil
	}
	return NewDeepReviewError(
		"--mode pr requires a GitHub-backed repo identity; local filesystem origin remotes are not supported for PR delivery",
	)
}

var ownerRepoSlugRe = regexp.MustCompile(`^([A-Za-z0-9_.-]+)/([A-Za-z0-9_.-]+)$`)
var ownerRepoSegmentRe = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)
var githubSCPLikeRemoteRe = regexp.MustCompile(`^git@github\.com:([A-Za-z0-9_.-]+)/([A-Za-z0-9_.-]+?)(?:\.git)?$`)
var secretRiskyPatterns = []*regexp.Regexp{
	regexp.MustCompile(`AKIA[0-9A-Z]{16}`),
	regexp.MustCompile(`(?:ghp|gho|ghu|ghs|ghr)_[A-Za-z0-9]{20,}`),
	regexp.MustCompile(`github_pat_[A-Za-z0-9_]{20,}`),
	regexp.MustCompile(`sk(?:-[A-Za-z0-9]+)?-[A-Za-z0-9]{20,}`),
	regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----`),
	regexp.MustCompile(`xox[baprs]-[A-Za-z0-9-]{10,}`),
}
var secretAssignmentRiskyPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\b(?:api[_-]?key|token|password|secret)\b[[:space:]]*[:=][[:space:]]*["']?[A-Za-z0-9_./+=-]{12,}`),
}
var emailPattern = regexp.MustCompile(`(?i)\b[A-Z0-9._%+\-]+@(?:[A-Z0-9\-]+\.)+[A-Z]{2,}\b`)
var personalRiskyPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?m)\b\d{3}-\d{2}-\d{4}\b`),
	regexp.MustCompile(`(?m)\b(?:\+?1[-.\s]?)?(?:\(\d{3}\)|\d{3})[-.\s]\d{3}[-.\s]\d{4}\b`),
}
var privatePathPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?m)/Users/\S+`),
	regexp.MustCompile(`(?m)/home/\S+`),
	regexp.MustCompile(`(?mi)\b[A-Z]:\\+Users\\+\S+`),
}
var allowedPlaceholderEmailDomains = map[string]struct{}{
	"example.com": {},
	"example.org": {},
	"example.net": {},
	"localhost":   {},
	"local":       {},
	"invalid":     {},
}

const (
	githubPRBodyMaxChars     = 65536
	githubPRBodyTargetChars  = 64000
	githubPRTitleTargetChars = 240
)

func parseOwnerRepo(text string) (string, string, bool) {
	trimmed := strings.TrimSpace(text)
	if m := githubSCPLikeRemoteRe.FindStringSubmatch(trimmed); m != nil {
		return SanitizeSegment(m[1]), SanitizeSegment(m[2]), true
	}
	if owner, name, ok := parseGitHubOwnerRepoURL(trimmed); ok {
		return owner, name, true
	}
	if m := ownerRepoSlugRe.FindStringSubmatch(trimmed); m != nil {
		return SanitizeSegment(m[1]), SanitizeSegment(m[2]), true
	}
	return "", "", false
}

func parseGitHubOwnerRepoURL(text string) (string, string, bool) {
	if !strings.Contains(text, "://") {
		return "", "", false
	}
	parsed, err := url.Parse(text)
	if err != nil {
		return "", "", false
	}
	if parsed.Hostname() != "github.com" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", "", false
	}
	return parseGitHubOwnerRepoPath(parsed.Path)
}

func parseGitHubOwnerRepoPath(path string) (string, string, bool) {
	trimmed := strings.Trim(strings.TrimSpace(path), "/")
	if trimmed == "" {
		return "", "", false
	}
	parts := strings.Split(trimmed, "/")
	if len(parts) != 2 {
		return "", "", false
	}
	owner := parts[0]
	name := strings.TrimSuffix(parts[1], ".git")
	if owner == "" || name == "" {
		return "", "", false
	}
	if !ownerRepoSegmentValid(owner) || !ownerRepoSegmentValid(name) {
		return "", "", false
	}
	return SanitizeSegment(owner), SanitizeSegment(name), true
}

func ownerRepoSegmentValid(text string) bool {
	return ownerRepoSegmentRe.MatchString(text)
}

func isOwnerRepoSlug(text string) bool {
	return ownerRepoSlugRe.MatchString(strings.TrimSpace(text))
}

func allSecretRiskyPatterns() []*regexp.Regexp {
	patterns := make([]*regexp.Regexp, 0, len(secretRiskyPatterns)+len(secretAssignmentRiskyPatterns))
	patterns = append(patterns, secretRiskyPatterns...)
	patterns = append(patterns, secretAssignmentRiskyPatterns...)
	return patterns
}

func tryReadRemoteURL(gitBin, repoPath string) (string, error) {
	completed, err := RunCommand([]string{gitBin, "-C", repoPath, "config", "--get", "remote.origin.url"}, "", "", false, 0)
	if err != nil {
		return "", err
	}
	if completed.ReturnCode != 0 {
		return "", nil
	}
	return strings.TrimSpace(completed.Stdout), nil
}

func (o *Orchestrator) Run() (retErr error) {
	if err := o.preflight(); err != nil {
		return err
	}
	if err := o.acquireRunLock(); err != nil {
		return err
	}
	defer o.releaseRunLock()
	if err := os.MkdirAll(filepath.Join(o.runRoot, "logs"), 0o755); err != nil {
		return err
	}
	if err := o.writeRunConfig(); err != nil {
		return err
	}
	if reporterWithMaxRounds, ok := o.reporter.(MaxRoundsAwareProgressReporter); ok {
		reporterWithMaxRounds.SetMaxRounds(o.config.MaxRounds)
	}
	o.reporter.RunStarted(o.config.RunID, o.repoIdentity.Slug(), o.config.SourceBranch, o.config.Mode, o.runRoot)

	var (
		defaultBranch   string
		candidateBranch string
		roundSummaries  []string
		successMessage  string
	)
	defer func() {
		if retErr != nil {
			recovered, recoveryErr := o.tryPublishIncompleteDraftPR(defaultBranch, candidateBranch, roundSummaries, retErr)
			switch {
			case recoveryErr != nil:
				retErr = errors.Join(retErr, recoveryErr)
			case recovered:
				successMessage = "run completed with incomplete draft PR"
				retErr = nil
			}
		}
		if retErr == nil {
			if err := o.ensureTerminalFinalSummary(defaultBranch, candidateBranch, roundSummaries); err != nil {
				retErr = err
			}
		}
		if retErr != nil {
			o.reporter.RunFinished(false, retErr.Error())
			return
		}
		if successMessage != "" {
			o.reporter.RunFinished(true, successMessage)
		}
	}()

	prepareStage := "prepare"
	o.reporter.StageStarted(prepareStage, nil, "syncing branch-scoped managed repository copy")
	if err := CloneOrFetch(o.managedRepoPath, o.repoIdentity.CloneSource, o.config.GitBin); err != nil {
		o.reporter.StageFinished(prepareStage, nil, false, progressMessage(err))
		return err
	}
	identity := o.config.CommitIdentity
	if strings.TrimSpace(identity.Name) == "" || strings.TrimSpace(identity.Email) == "" {
		resolvedIdentity, resolveErr := ResolveCommitIdentity(o.config.GitBin, o.config.Repo)
		if resolveErr != nil {
			o.reporter.StageFinished(prepareStage, nil, false, progressMessage(resolveErr))
			return resolveErr
		}
		identity = resolvedIdentity
	}
	if err := ConfigureManagedGitIdentity(o.managedRepoPath, o.config.GitBin, identity); err != nil {
		o.reporter.StageFinished(prepareStage, nil, false, progressMessage(err))
		return err
	}
	o.commitIdentity = identity
	if err := EnsureWorktreeOperationalExcludes(o.managedRepoPath, o.config.GitBin); err != nil {
		o.reporter.StageFinished(prepareStage, nil, false, progressMessage(err))
		return err
	}
	var err error
	defaultBranch, err = ResolveDefaultBranch(o.managedRepoPath, o.config.GitBin)
	if err != nil {
		o.reporter.StageFinished(prepareStage, nil, false, progressMessage(err))
		return err
	}
	sourceSHA, err := RequireRemoteBranch(o.managedRepoPath, o.config.GitBin, o.config.SourceBranch)
	if err != nil {
		o.reporter.StageFinished(prepareStage, nil, false, progressMessage(err))
		return err
	}

	candidateBranch = o.candidateBranchName(o.config.SourceBranch, o.config.RunID)
	if err := SetBranchToRef(o.managedRepoPath, o.config.GitBin, candidateBranch, sourceSHA); err != nil {
		o.reporter.StageFinished(prepareStage, nil, false, progressMessage(err))
		return err
	}
	if o.config.Mode == ModeYolo && o.config.SourceBranch == defaultBranch {
		if err := o.verifyYoloDefaultBranchPushAllowed(candidateBranch, defaultBranch); err != nil {
			o.reporter.StageFinished(prepareStage, nil, false, progressMessage(err))
			return err
		}
	}
	o.reporter.StageFinished(
		prepareStage,
		nil,
		true,
		fmt.Sprintf("managed repo ready: default branch `%s`, source head `%s`", defaultBranch, shortSHA(sourceSHA)),
	)

	roundSummaries = make([]string, 0)
	consecutiveStopDecisions := 0

	for round := 1; round <= o.config.MaxRounds; round++ {
		roundDir := filepath.Join(o.runRoot, fmt.Sprintf("round-%02d", round))
		if err := os.MkdirAll(roundDir, 0o755); err != nil {
			return err
		}

		candidateHeadBefore, err := RevParse(o.managedRepoPath, o.config.GitBin, candidateBranch)
		if err != nil {
			return err
		}

		reviewReports, err := o.runReviewStage(round, roundDir, candidateHeadBefore, defaultBranch)
		if err != nil {
			return err
		}

		status, summaryPath, err := o.runExecuteStage(
			round,
			roundDir,
			candidateBranch,
			candidateHeadBefore,
			defaultBranch,
			reviewReports,
			o.config.MaxRounds,
			executeRoundOptions{},
		)
		if err != nil {
			return err
		}
		roundSummaries = append(roundSummaries, summaryPath)

		candidateHeadAfter, err := RevParse(o.managedRepoPath, o.config.GitBin, candidateBranch)
		if err != nil {
			return err
		}

		roundChangedFiles, err := ListChangedFiles(o.managedRepoPath, o.config.GitBin, candidateHeadBefore, candidateHeadAfter)
		if err != nil {
			return err
		}
		changed := len(roundChangedFiles) > 0
		control := evaluateRoundLoopControl(consecutiveStopDecisions, status, changed, len(roundChangedFiles))
		consecutiveStopDecisions = control.nextStopStreak
		if !control.shouldContinue {
			o.reporter.StageProgress("execute stage", control.message, roundPtr(round))
			break
		}
		if round >= o.config.MaxRounds {
			return NewDeepReviewError(
				"round %d/%d requires another review round (decision `%s`, consecutive stop decisions `%d`, repository changes `%d`); rerun deepreview with a higher --max-rounds",
				round,
				o.config.MaxRounds,
				status.Decision,
				consecutiveStopDecisions,
				len(roundChangedFiles),
			)
		}
		o.reporter.StageProgress("execute stage", control.message, roundPtr(round))
	}

	if len(roundSummaries) == 0 {
		return NewDeepReviewError("internal run state invalid: no review/execute rounds were completed")
	}
	o.reviewedCandidateHead, err = RevParse(o.managedRepoPath, o.config.GitBin, candidateBranch)
	if err != nil {
		return err
	}

	deliveryStage := "delivery"
	o.reporter.StageStarted(deliveryStage, nil, "validating delivery inputs and publishing results")
	changedFiles, err := o.validateDeliveryFiles(candidateBranch)
	if err != nil {
		o.reporter.StageFinished(deliveryStage, nil, false, progressMessage(err))
		return err
	}
	if len(changedFiles) == 0 {
		delivery := DeliveryResult{
			Mode:       o.config.Mode,
			Skipped:    true,
			SkipReason: "no deliverable repository changes were produced",
		}
		o.lastDelivery = &delivery
		if err := o.writeFinalSummary(defaultBranch, candidateBranch, delivery, roundSummaries); err != nil {
			o.reporter.StageFinished(deliveryStage, nil, false, progressMessage(err))
			return err
		}
		o.reporter.StageFinished(deliveryStage, nil, true, delivery.SkipReason)
		successMessage = "run completed successfully (no deliverable repository changes)"
		return nil
	}
	changedFiles, err = o.recoverCandidateDeliveryReadiness(defaultBranch, candidateBranch, &roundSummaries)
	if err != nil {
		o.reporter.StageFinished(deliveryStage, nil, false, progressMessage(err))
		return err
	}
	if len(changedFiles) == 0 {
		delivery := DeliveryResult{
			Mode:       o.config.Mode,
			Skipped:    true,
			SkipReason: "delivery recovery removed all deliverable repository changes",
		}
		o.lastDelivery = &delivery
		if err := o.writeFinalSummary(defaultBranch, candidateBranch, delivery, roundSummaries); err != nil {
			o.reporter.StageFinished(deliveryStage, nil, false, progressMessage(err))
			return err
		}
		o.reporter.StageFinished(deliveryStage, nil, true, delivery.SkipReason)
		successMessage = "run completed successfully (no deliverable repository changes)"
		return nil
	}
	delivery, err := o.runDeliveryStage(defaultBranch, candidateBranch, roundSummaries, changedFiles)
	if err != nil {
		o.reporter.StageFinished(deliveryStage, nil, false, progressMessage(err))
		return err
	}
	o.lastDelivery = &delivery
	if err := o.writeFinalSummary(defaultBranch, candidateBranch, delivery, roundSummaries); err != nil {
		o.reporter.StageFinished(deliveryStage, nil, false, progressMessage(err))
		return err
	}
	o.reporter.StageFinished(deliveryStage, nil, true, fmt.Sprintf("delivery completed in `%s` mode", delivery.Mode))
	successMessage = "run completed successfully"
	return nil
}

func (o *Orchestrator) ensureTerminalFinalSummary(defaultBranch, candidateBranch string, summaries []string) error {
	if o.lastDelivery == nil {
		return nil
	}
	summaryPath := filepath.Join(o.runRoot, "final-summary.md")
	if _, err := os.Stat(summaryPath); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	if len(summaries) == 0 {
		discovered, err := o.discoverCompletedRoundSummaries()
		if err != nil {
			return err
		}
		summaries = discovered
	}
	if len(summaries) == 0 {
		return NewDeepReviewError("cannot write final summary: no completed round records are available")
	}
	return o.writeFinalSummary(defaultBranch, candidateBranch, *o.lastDelivery, summaries)
}

func (o *Orchestrator) preflight() error {
	for _, tool := range requiredHostTools(o.config) {
		ok, err := which(tool)
		if err != nil {
			return err
		}
		if !ok {
			return NewDeepReviewError("required tool not found in PATH: %s", tool)
		}
	}
	if _, err := o.codexRunner.resolveLauncher(); err != nil {
		return err
	}
	if err := checkPromptTemplates(o.promptsRoot); err != nil {
		return err
	}
	return nil
}

func requiredHostTools(cfg ReviewConfig) []string {
	required := []string{cfg.GitBin}
	if cfg.Mode == ModePR {
		required = append(required, cfg.GhBin)
	}
	return required
}

type runLockRecord struct {
	RunID        string `json:"run_id"`
	PID          int    `json:"pid"`
	Repo         string `json:"repo"`
	SourceBranch string `json:"source_branch"`
	CreatedAt    string `json:"created_at"`
}

func (o *Orchestrator) runLockFilePath() string {
	return filepath.Join(
		append(
			[]string{o.workspaceRoot, "locks"},
			append(o.repoIdentity.NamespaceSegments(), FilesystemSafeKey(o.config.SourceBranch)+".lock")...,
		)...,
	)
}

func (o *Orchestrator) acquireRunLock() error {
	lockPath := o.runLockFilePath()
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
		return err
	}
	record := runLockRecord{
		RunID:        o.config.RunID,
		PID:          os.Getpid(),
		Repo:         o.repoIdentity.Slug(),
		SourceBranch: o.config.SourceBranch,
		CreatedAt:    time.Now().UTC().Format(time.RFC3339),
	}
	payload, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}

	writeLock := func() error {
		file, err := os.OpenFile(lockPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err != nil {
			return err
		}
		defer file.Close()
		if _, err := file.Write(append(payload, '\n')); err != nil {
			return err
		}
		return nil
	}

	if err := writeLock(); err == nil {
		o.runLockPath = lockPath
		return nil
	} else if !os.IsExist(err) {
		return err
	}

	existingPayload, readErr := os.ReadFile(lockPath)
	if readErr != nil && !os.IsNotExist(readErr) {
		return readErr
	}
	if lockLooksStale(lockPath, existingPayload) {
		_ = os.Remove(lockPath)
		if err := writeLock(); err == nil {
			o.runLockPath = lockPath
			return nil
		} else if !os.IsExist(err) {
			return err
		}
	}

	return NewDeepReviewError(
		"another deepreview run is active for repo `%s` on source branch `%s`; wait for it to finish before starting another run",
		o.repoIdentity.Slug(),
		o.config.SourceBranch,
	)
}

func (o *Orchestrator) releaseRunLock() {
	if strings.TrimSpace(o.runLockPath) == "" {
		return
	}
	if err := os.Remove(o.runLockPath); err != nil && !os.IsNotExist(err) {
		// best-effort cleanup
	}
	o.runLockPath = ""
}

func lockLooksStale(lockPath string, payload []byte) bool {
	var record runLockRecord
	if len(payload) > 0 && json.Unmarshal(payload, &record) == nil && record.PID > 0 {
		if !isPIDAlive(record.PID) {
			return true
		}
		return false
	}

	info, err := os.Stat(lockPath)
	if err != nil {
		return true
	}
	return time.Since(info.ModTime()) > 12*time.Hour
}

func isPIDAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = process.Signal(syscall.Signal(0))
	if err == nil {
		return true
	}
	if errors.Is(err, os.ErrProcessDone) {
		return false
	}
	if errno, ok := err.(syscall.Errno); ok && errno == syscall.ESRCH {
		return false
	}
	return true
}

func which(command string) (bool, error) {
	_, err := exec.LookPath(command)
	if err == nil {
		return true, nil
	}
	var execErr *exec.Error
	if errors.As(err, &execErr) {
		return false, nil
	}
	return false, err
}

func (o *Orchestrator) candidateBranchName(sourceBranch, runID string) string {
	return "deepreview/candidate/" + SanitizeSegment(sourceBranch) + "/" + SanitizeSegment(runID)
}

func (o *Orchestrator) verifyYoloDefaultBranchPushAllowed(candidateBranch, defaultBranch string) error {
	refspec := candidateBranch + ":" + o.config.SourceBranch
	if err := DryRunPushRefspec(o.managedRepoPath, o.config.GitBin, refspec); err != nil {
		return NewDeepReviewError(
			"yolo preflight failed: cannot push to default branch `%s`; use --mode pr or adjust branch protections/permissions",
			defaultBranch,
		)
	}
	return nil
}

func roundPtr(round int) *int {
	v := round
	return &v
}

type roundLoopControl struct {
	nextStopStreak int
	shouldContinue bool
	message        string
}

func evaluateRoundLoopControl(previousStopStreak int, status RoundStatus, changed bool, changedCount int) roundLoopControl {
	if status.Decision == "continue" {
		message := "codex requested another round; continuing review loop"
		if changed {
			message = fmt.Sprintf(
				"codex requested another round and execute produced %d repository change(s); continuing review loop",
				changedCount,
			)
		}
		return roundLoopControl{
			nextStopStreak: 0,
			shouldContinue: true,
			message:        message,
		}
	}

	stopStreak := previousStopStreak + 1
	if stopStreak >= 2 {
		message := "codex produced a second consecutive stop decision; stopping round loop"
		if changed {
			message = fmt.Sprintf(
				"codex produced a second consecutive stop decision; stopping round loop despite %d repository change(s) in this round",
				changedCount,
			)
		}
		return roundLoopControl{
			nextStopStreak: stopStreak,
			shouldContinue: false,
			message:        message,
		}
	}

	message := "codex produced the first stop decision; running one confirmation round"
	if changed {
		message = fmt.Sprintf(
			"codex produced the first stop decision, but execute also produced %d repository change(s); running one confirmation round",
			changedCount,
		)
	}
	return roundLoopControl{
		nextStopStreak: stopStreak,
		shouldContinue: true,
		message:        message,
	}
}

type promptWatchdogPolicy struct {
	inactivity   time.Duration
	pollInterval time.Duration
	maxRestarts  int
}

func (o *Orchestrator) promptWatchdogPolicy() promptWatchdogPolicy {
	inactivity := o.config.ReviewInactivity
	if inactivity < 0 {
		inactivity = 0
	}
	pollInterval := o.config.ReviewActivityPoll
	if pollInterval <= 0 {
		pollInterval = stageHeartbeatInterval
	}
	if inactivity > 0 && pollInterval > inactivity {
		pollInterval = inactivity
	}
	maxRestarts := o.config.ReviewMaxRestarts
	if maxRestarts < 0 {
		maxRestarts = 0
	}
	return promptWatchdogPolicy{
		inactivity:   inactivity,
		pollInterval: pollInterval,
		maxRestarts:  maxRestarts,
	}
}

type promptInactivityError struct {
	inactivity time.Duration
	silence    time.Duration
}

func (e *promptInactivityError) Error() string {
	inactivity := e.inactivity.Round(time.Second)
	silence := e.silence.Round(time.Second)
	if inactivity <= 0 {
		return fmt.Sprintf("prompt stalled with %s of inactivity", silence)
	}
	return fmt.Sprintf("prompt stalled with %s of inactivity (limit %s)", silence, inactivity)
}

type monitoredPromptRequest struct {
	label               string
	cwd                 string
	prompt              string
	codexContext        *CodexContext
	resetContextOnRetry bool
	logPrefix           string
	useGitStatus        bool
	monitoredPaths      []string
}

type monitoredPromptCallbacks struct {
	onHeartbeat   func(elapsed, silence time.Duration)
	onBeforeRetry func(nextAttempt, maxAttempts int, inactivityErr *promptInactivityError) error
	onRestart     func(nextAttempt, maxAttempts int, inactivityErr *promptInactivityError)
}

func (o *Orchestrator) runPromptWithWatchdog(
	parent context.Context,
	request monitoredPromptRequest,
	callbacks monitoredPromptCallbacks,
) (CodexRunResult, int, error) {
	policy := o.promptWatchdogPolicy()
	maxAttempts := policy.maxRestarts + 1
	restarts := 0
	currentRequest := request
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		result, err := o.runPromptAttemptWithWatchdog(parent, currentRequest, policy, callbacks.onHeartbeat)
		if err == nil {
			return result, restarts, nil
		}
		var inactivityErr *promptInactivityError
		if !errors.As(err, &inactivityErr) {
			return CodexRunResult{}, restarts, err
		}
		if attempt >= maxAttempts {
			return CodexRunResult{}, restarts, NewDeepReviewError(
				"%s stalled after %d attempt(s): %s",
				currentRequest.label,
				maxAttempts,
				inactivityErr.Error(),
			)
		}
		restarts++
		if callbacks.onBeforeRetry != nil {
			if retryErr := callbacks.onBeforeRetry(attempt+1, maxAttempts, inactivityErr); retryErr != nil {
				return CodexRunResult{}, restarts, retryErr
			}
		}
		if callbacks.onRestart != nil {
			callbacks.onRestart(attempt+1, maxAttempts, inactivityErr)
		}
		currentRequest = preparePromptRetryRequest(currentRequest)
	}
	return CodexRunResult{}, restarts, NewDeepReviewError("%s failed unexpectedly", currentRequest.label)
}

func preparePromptRetryRequest(request monitoredPromptRequest) monitoredPromptRequest {
	retryRequest := request
	if retryRequest.resetContextOnRetry {
		retryRequest.codexContext = nil
	}
	return retryRequest
}

func (o *Orchestrator) runPromptAttemptWithWatchdog(
	parent context.Context,
	request monitoredPromptRequest,
	policy promptWatchdogPolicy,
	onHeartbeat func(elapsed, silence time.Duration),
) (CodexRunResult, error) {
	if parent == nil {
		parent = context.Background()
	}
	start := time.Now()
	tracker := newPromptActivityTracker(start)
	lastGitSignature := ""
	if request.useGitStatus {
		if sig, ok := o.gitStatusActivitySignature(parent, request.cwd); ok {
			lastGitSignature = sig
		}
	}
	lastPathSignature := pathsActivitySignature(request.monitoredPaths)

	attemptCtx, cancelAttempt := context.WithCancel(parent)
	defer cancelAttempt()

	type runOutcome struct {
		result CodexRunResult
		err    error
	}
	resultCh := make(chan runOutcome, 1)
	go func() {
		result, err := o.codexRunner.RunPromptWithHooks(
			request.cwd,
			request.prompt,
			request.codexContext,
			request.logPrefix,
			&CodexRunHooks{
				Context: attemptCtx,
				OnStdoutChunk: func(_ []byte) {
					tracker.Mark()
				},
				OnStderrChunk: func(_ []byte) {
					tracker.Mark()
				},
			},
		)
		resultCh <- runOutcome{result: result, err: err}
	}()

	pollTicker := time.NewTicker(policy.pollInterval)
	defer pollTicker.Stop()
	var heartbeatTicker *time.Ticker
	var heartbeatCh <-chan time.Time
	if onHeartbeat != nil {
		heartbeatTicker = time.NewTicker(stageHeartbeatInterval)
		defer heartbeatTicker.Stop()
		heartbeatCh = heartbeatTicker.C
	}

	for {
		select {
		case outcome := <-resultCh:
			return outcome.result, outcome.err
		case <-pollTicker.C:
			if request.useGitStatus {
				if sig, ok := o.gitStatusActivitySignature(attemptCtx, request.cwd); ok && sig != lastGitSignature {
					lastGitSignature = sig
					tracker.Mark()
				}
			}
			pathSignature := pathsActivitySignature(request.monitoredPaths)
			if pathSignature != lastPathSignature {
				lastPathSignature = pathSignature
				tracker.Mark()
			}
			silence := tracker.Silence()
			if policy.inactivity > 0 && silence >= policy.inactivity {
				cancelAttempt()
				outcome := <-resultCh
				if outcome.err == nil {
					return outcome.result, nil
				}
				return CodexRunResult{}, &promptInactivityError{
					inactivity: policy.inactivity,
					silence:    silence,
				}
			}
		case <-heartbeatCh:
			onHeartbeat(time.Since(start), tracker.Silence())
		}
	}
}

type promptActivityTracker struct {
	mu           sync.Mutex
	lastActivity time.Time
}

func newPromptActivityTracker(initial time.Time) *promptActivityTracker {
	return &promptActivityTracker{lastActivity: initial}
}

func (t *promptActivityTracker) Mark() {
	t.mu.Lock()
	t.lastActivity = time.Now()
	t.mu.Unlock()
}

func (t *promptActivityTracker) Silence() time.Duration {
	t.mu.Lock()
	last := t.lastActivity
	t.mu.Unlock()
	silence := time.Since(last)
	if silence < 0 {
		return 0
	}
	return silence
}

func (o *Orchestrator) gitStatusActivitySignature(parent context.Context, cwd string) (string, bool) {
	completed, err := RunCommandContext(
		parent,
		[]string{o.config.GitBin, "-C", cwd, "status", "--porcelain", "--untracked-files=all"},
		"",
		"",
		false,
		10*time.Second,
	)
	if err != nil {
		return "", false
	}
	if completed.ReturnCode != 0 {
		return "", false
	}
	return completed.Stdout, true
}

func pathsActivitySignature(paths []string) string {
	if len(paths) == 0 {
		return ""
	}
	parts := make([]string, 0, len(paths))
	for _, path := range paths {
		trimmed := strings.TrimSpace(path)
		if trimmed == "" {
			continue
		}
		parts = append(parts, trimmed+"="+pathActivitySignature(trimmed))
	}
	sort.Strings(parts)
	return strings.Join(parts, "|")
}

func pathActivitySignature(path string) string {
	st, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "missing"
		}
		return "error:" + sanitizePublicText(err.Error())
	}
	if !st.IsDir() {
		return fmt.Sprintf("file:%d:%d", st.Size(), st.ModTime().UnixNano())
	}
	entries := make([]string, 0, 16)
	walkErr := filepath.WalkDir(path, func(current string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			entries = append(entries, fmt.Sprintf("%s:error:%s", current, sanitizePublicText(walkErr.Error())))
			return nil
		}
		info, err := d.Info()
		if err != nil {
			entries = append(entries, fmt.Sprintf("%s:error:%s", current, sanitizePublicText(err.Error())))
			return nil
		}
		rel := current
		if relValue, relErr := filepath.Rel(path, current); relErr == nil {
			rel = relValue
		}
		entries = append(entries, fmt.Sprintf("%s:%t:%d:%d", rel, d.IsDir(), info.Size(), info.ModTime().UnixNano()))
		return nil
	})
	if walkErr != nil {
		return "walk-error:" + sanitizePublicText(walkErr.Error())
	}
	sort.Strings(entries)
	return strings.Join(entries, ";")
}

type reviewPromptScope struct {
	ModeLabel    string
	ModeNote     string
	ProcessStep1 string
}

func buildReviewPromptScope(sourceBranch, defaultBranch string) reviewPromptScope {
	if strings.TrimSpace(sourceBranch) == strings.TrimSpace(defaultBranch) {
		return reviewPromptScope{
			ModeLabel: "current-state repository audit",
			ModeNote: strings.TrimSpace(`
Self-audit note:
- Source branch and default branch are the same for this run.
- Treat branch-diff inspection as orientation only; do not stop at "no diff" or "already on main".
- Continue with a current-state repository audit of the codebase as it exists now, including likely high-risk integration and verification paths.
`),
			ProcessStep1: "Use source-branch vs default-branch diff only as orientation; if it is empty, continue into a current-state repository audit rather than concluding there is nothing to review.",
		}
	}
	return reviewPromptScope{
		ModeLabel:    "source-branch change review",
		ModeNote:     "",
		ProcessStep1: "Build a concrete change map from source branch vs default branch.",
	}
}

func (o *Orchestrator) runReviewStage(round int, roundDir, candidateHead, defaultBranch string) ([]string, error) {
	o.reporter.StageStarted("independent review stage", roundPtr(round), "launching independent reviewers in parallel")
	reviewDir := filepath.Join(roundDir, "review")
	if err := os.MkdirAll(reviewDir, 0o755); err != nil {
		o.reporter.StageFinished("independent review stage", roundPtr(round), false, progressMessage(err))
		return nil, err
	}

	templateText, err := ReadTemplate(filepath.Join(o.promptsRoot, "review", "independent-review.md"))
	if err != nil {
		o.reporter.StageFinished("independent review stage", roundPtr(round), false, progressMessage(err))
		return nil, err
	}
	policy := o.promptWatchdogPolicy()
	policyMessage := fmt.Sprintf(
		"worker policy: require %d/%d successes; inactivity timeout %s; poll interval %s; max restarts %d",
		o.config.Concurrency,
		o.config.Concurrency,
		policy.inactivity.Round(time.Second),
		policy.pollInterval.Round(time.Second),
		policy.maxRestarts,
	)
	if policy.inactivity <= 0 {
		policyMessage = fmt.Sprintf(
			"worker policy: require %d/%d successes; inactivity restart disabled; poll interval %s; max restarts %d",
			o.config.Concurrency,
			o.config.Concurrency,
			policy.pollInterval.Round(time.Second),
			policy.maxRestarts,
		)
	}
	o.reporter.StageProgress("independent review stage", policyMessage, roundPtr(round))

	stageCtx, cancelStage := context.WithCancel(currentRunCommandContext())
	restoreCommandContext := setRunCommandContext(stageCtx)
	defer func() {
		restoreCommandContext()
	}()

	worktrees := make([]string, 0, o.config.Concurrency)
	reviewPaths := make([]string, 0, o.config.Concurrency)
	workerReviewPaths := make([]string, 0, o.config.Concurrency)
	workerNotesPaths := make([]string, 0, o.config.Concurrency)
	var workers sync.WaitGroup
	defer func() {
		cancelStage()
		workers.Wait()
		waitForActiveCommandsToExit(5 * time.Second)
		for _, worktree := range worktrees {
			_ = RemoveWorktree(o.managedRepoPath, o.config.GitBin, worktree)
		}
	}()

	for workerID := 1; workerID <= o.config.Concurrency; workerID++ {
		workerDir := filepath.Join(reviewDir, fmt.Sprintf("worker-%02d", workerID))
		worktreePath := filepath.Join(workerDir, "worktree")
		reviewPath := filepath.Join(roundDir, fmt.Sprintf("review-%02d.md", workerID))
		workerReviewPath := filepath.Join(worktreePath, ".deepreview", fmt.Sprintf("review-%02d.md", workerID))
		workerNotesPath := filepath.Join(worktreePath, ".deepreview", fmt.Sprintf("review-%02d.notes.md", workerID))
		if err := os.MkdirAll(workerDir, 0o755); err != nil {
			return nil, err
		}
		if err := AddDetachedWorktree(o.managedRepoPath, o.config.GitBin, worktreePath, candidateHead); err != nil {
			return nil, err
		}
		worktrees = append(worktrees, worktreePath)
		if err := EnsureWorktreeOperationalExcludes(worktreePath, o.config.GitBin); err != nil {
			return nil, err
		}
		if err := os.MkdirAll(filepath.Dir(workerReviewPath), 0o755); err != nil {
			return nil, err
		}
		reviewPaths = append(reviewPaths, reviewPath)
		workerReviewPaths = append(workerReviewPaths, workerReviewPath)
		workerNotesPaths = append(workerNotesPaths, workerNotesPath)
	}

	type reviewWorkerResult struct {
		workerID int
		restarts int
		err      error
	}
	resultsCh := make(chan reviewWorkerResult, o.config.Concurrency)

	for idx := range reviewPaths {
		workers.Add(1)
		go func(i int) {
			defer workers.Done()
			workerID := i + 1
			worktreePath := worktrees[i]
			workerReviewPath := workerReviewPaths[i]
			workerNotesPath := workerNotesPaths[i]
			scope := buildReviewPromptScope(o.config.SourceBranch, defaultBranch)
			variables := map[string]string{
				"REPO_SLUG":          o.repoIdentity.Slug(),
				"SOURCE_BRANCH":      o.config.SourceBranch,
				"DEFAULT_BRANCH":     defaultBranch,
				"WORKER_ID":          fmt.Sprintf("%d", workerID),
				"CONCURRENCY":        fmt.Sprintf("%d", o.config.Concurrency),
				"WORKTREE_PATH":      filepath.ToSlash(worktreePath),
				"OUTPUT_REVIEW_PATH": filepath.ToSlash(workerReviewPath),
				"WORKER_NOTES_PATH":  filepath.ToSlash(workerNotesPath),
				"REVIEW_MODE_LABEL":  scope.ModeLabel,
				"REVIEW_MODE_NOTE":   scope.ModeNote,
				"REVIEW_PROCESS_1":   scope.ProcessStep1,
			}
			prompt, err := RenderTemplate(templateText, variables)
			if err != nil {
				resultsCh <- reviewWorkerResult{workerID: workerID, err: err}
				return
			}
			logPrefix := filepath.Join(reviewDir, fmt.Sprintf("worker-%02d", workerID), "codex")
			_, restarts, err := o.runPromptWithWatchdog(
				stageCtx,
				monitoredPromptRequest{
					label:        fmt.Sprintf("worker-%02d", workerID),
					cwd:          worktreePath,
					prompt:       prompt,
					codexContext: nil,
					logPrefix:    logPrefix,
					useGitStatus: true,
					monitoredPaths: []string{
						workerReviewPath,
						workerNotesPath,
						logPrefix + ".stdout.jsonl",
						logPrefix + ".stderr.log",
					},
				},
				monitoredPromptCallbacks{
					onBeforeRetry: func(_ int, _ int, _ *promptInactivityError) error {
						return prepareReviewWorkerRetry(worktreePath, o.config.GitBin, candidateHead, workerReviewPath, workerNotesPath)
					},
					onRestart: func(nextAttempt, maxAttempts int, inactivityErr *promptInactivityError) {
						o.reporter.StageProgress(
							"independent review stage",
							fmt.Sprintf(
								"worker-%02d inactive for %s; restarting attempt %d/%d",
								workerID,
								inactivityErr.silence.Round(time.Second),
								nextAttempt,
								maxAttempts,
							),
							roundPtr(round),
						)
					},
				},
			)
			resultsCh <- reviewWorkerResult{workerID: workerID, restarts: restarts, err: err}
		}(idx)
	}

	successCount := 0
	totalRestarts := 0
	heartbeatStart := time.Now()
	heartbeatTicker := time.NewTicker(stageHeartbeatInterval)
	defer heartbeatTicker.Stop()
	for successCount < o.config.Concurrency {
		pendingCount := o.config.Concurrency - successCount
		select {
		case result := <-resultsCh:
			if result.err != nil {
				cancelStage()
				err := NewDeepReviewError("independent review stage failed: worker-%02d error: %v", result.workerID, result.err)
				o.reporter.StageFinished("independent review stage", roundPtr(round), false, progressMessage(err))
				return nil, err
			}
			successCount++
			totalRestarts += result.restarts
			o.reporter.StageProgress(
				"independent review stage",
				fmt.Sprintf(
					"completed reviewer worker-%02d (%d/%d complete, %d total restarts)",
					result.workerID,
					successCount,
					o.config.Concurrency,
					totalRestarts,
				),
				roundPtr(round),
			)
		case <-heartbeatTicker.C:
			if pendingCount > 0 {
				waitingMessage := fmt.Sprintf(
					"waiting on reviewer workers: %d/%d complete, %d pending, %d restarts",
					successCount,
					o.config.Concurrency,
					pendingCount,
					totalRestarts,
				)
				o.reporter.StageProgress("independent review stage", stageHeartbeatMessage(waitingMessage, heartbeatStart), roundPtr(round))
			}
		}
	}

	selectedReviewPaths := make([]string, 0, o.config.Concurrency)
	for workerID := 1; workerID <= o.config.Concurrency; workerID++ {
		idx := workerID - 1
		reviewPath := reviewPaths[idx]
		if err := ensureCanonicalArtifact(reviewPath, []string{
			workerReviewPaths[idx],
			filepath.Join(worktrees[idx], fmt.Sprintf("review-%02d.md", idx+1)),
			filepath.Join(worktrees[idx], "review.md"),
			reviewPath,
		}); err != nil {
			err := NewDeepReviewError("independent review output missing: %s", reviewPath)
			o.reporter.StageFinished("independent review stage", roundPtr(round), false, progressMessage(err))
			return nil, err
		}
		if _, err := os.Stat(reviewPath); err != nil {
			err := NewDeepReviewError("independent review output missing: %s", reviewPath)
			o.reporter.StageFinished("independent review stage", roundPtr(round), false, progressMessage(err))
			return nil, err
		}
		selectedReviewPaths = append(selectedReviewPaths, reviewPath)
	}

	o.reporter.StageFinished(
		"independent review stage",
		roundPtr(round),
		true,
		fmt.Sprintf(
			"generated %d/%d independent review report(s); inactivity restarts: %d",
			len(selectedReviewPaths),
			o.config.Concurrency,
			totalRestarts,
		),
	)
	return selectedReviewPaths, nil
}

func prepareReviewWorkerRetry(worktreePath, gitBin, baselineRef, reviewPath, notesPath string) error {
	if err := resetMutablePromptWorktree(worktreePath, gitBin, baselineRef, nil); err != nil {
		return err
	}
	for _, path := range []string{reviewPath, notesPath} {
		trimmed := strings.TrimSpace(path)
		if trimmed == "" {
			continue
		}
		if err := os.Remove(trimmed); err != nil && !os.IsNotExist(err) {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(trimmed), 0o755); err != nil {
			return err
		}
	}
	return nil
}

func (o *Orchestrator) runExecuteStage(
	round int,
	roundDir, candidateBranch, candidateHead, defaultBranch string,
	reviewReports []string,
	maxRounds int,
	options executeRoundOptions,
) (RoundStatus, string, error) {
	o.reporter.StageStarted("execute stage", roundPtr(round), "running execute workflow (triage+plan, implement+verify+finalize)")
	executeDir := filepath.Join(roundDir, "execute")
	executeWorktree := filepath.Join(executeDir, "worktree")
	if err := os.MkdirAll(executeDir, 0o755); err != nil {
		o.reporter.StageFinished("execute stage", roundPtr(round), false, progressMessage(err))
		return RoundStatus{}, "", err
	}
	if err := AddBranchWorktree(o.managedRepoPath, o.config.GitBin, executeWorktree, candidateBranch, candidateHead); err != nil {
		o.reporter.StageFinished("execute stage", roundPtr(round), false, progressMessage(err))
		return RoundStatus{}, "", err
	}
	defer func() {
		_ = RemoveWorktree(o.managedRepoPath, o.config.GitBin, executeWorktree)
	}()
	if err := EnsureWorktreeOperationalExcludes(executeWorktree, o.config.GitBin); err != nil {
		o.reporter.StageFinished("execute stage", roundPtr(round), false, progressMessage(err))
		return RoundStatus{}, "", err
	}

	roundStatusPath := filepath.Join(roundDir, "round-status.json")
	roundSummaryPath := filepath.Join(roundDir, "round-summary.md")
	roundTriagePath := filepath.Join(roundDir, "round-triage.md")
	roundPlanPath := filepath.Join(roundDir, "round-plan.md")
	roundVerificationPath := filepath.Join(roundDir, "round-verification.md")
	roundRecordPath := filepath.Join(roundDir, "round.json")
	executeArtifactsDir := filepath.Join(executeWorktree, ".deepreview", "artifacts")
	roundStatusWorktreePath := filepath.Join(executeArtifactsDir, "round-status.json")
	roundSummaryWorktreePath := filepath.Join(executeArtifactsDir, "round-summary.md")
	roundTriageWorktreePath := filepath.Join(executeArtifactsDir, "round-triage.md")
	roundPlanWorktreePath := filepath.Join(executeArtifactsDir, "round-plan.md")
	roundVerificationWorktreePath := filepath.Join(executeArtifactsDir, "round-verification.md")
	if err := os.MkdirAll(executeArtifactsDir, 0o755); err != nil {
		o.reporter.StageFinished("execute stage", roundPtr(round), false, progressMessage(err))
		return RoundStatus{}, "", err
	}

	queue, err := ReadQueue(filepath.Join(o.promptsRoot, "execute", "queue.txt"))
	if err != nil {
		o.reporter.StageFinished("execute stage", roundPtr(round), false, progressMessage(err))
		return RoundStatus{}, "", err
	}

	reviewInputsDir := filepath.Join(executeArtifactsDir, "review-inputs")
	if err := os.MkdirAll(reviewInputsDir, 0o755); err != nil {
		o.reporter.StageFinished("execute stage", roundPtr(round), false, progressMessage(err))
		return RoundStatus{}, "", err
	}
	localReviewReports := make([]string, 0, len(reviewReports))
	for idx, src := range reviewReports {
		dst := filepath.Join(reviewInputsDir, fmt.Sprintf("review-%02d.md", idx+1))
		content, err := os.ReadFile(src)
		if err != nil {
			o.reporter.StageFinished("execute stage", roundPtr(round), false, progressMessage(err))
			return RoundStatus{}, "", err
		}
		if err := os.WriteFile(dst, content, 0o644); err != nil {
			o.reporter.StageFinished("execute stage", roundPtr(round), false, progressMessage(err))
			return RoundStatus{}, "", err
		}
		localReviewReports = append(localReviewReports, filepath.ToSlash(dst))
	}
	reviewReportPathsBullet := ""
	for _, p := range localReviewReports {
		reviewReportPathsBullet += "- " + p + "\n"
	}
	reviewReportPathsBullet = strings.TrimSpace(reviewReportPathsBullet)
	reviewInputManifest, err := buildReviewInputManifest(localReviewReports)
	if err != nil {
		o.reporter.StageFinished("execute stage", roundPtr(round), false, progressMessage(err))
		return RoundStatus{}, "", err
	}
	roundModeNote := strings.TrimSpace(options.modeNote)
	roundExecuteModeOverride := strings.TrimSpace(options.executeModeOverride)

	variables := map[string]string{
		"REPO_SLUG":                   o.repoIdentity.Slug(),
		"SOURCE_BRANCH":               o.config.SourceBranch,
		"DEFAULT_BRANCH":              defaultBranch,
		"ROUND_NUMBER":                fmt.Sprintf("%d", round),
		"MAX_ROUNDS":                  fmt.Sprintf("%d", maxRounds),
		"WORKTREE_PATH":               filepath.ToSlash(executeWorktree),
		"REVIEW_REPORT_PATHS":         reviewReportPathsBullet,
		"REVIEW_INPUT_MANIFEST":       reviewInputManifest,
		"ROUND_MODE_NOTE":             roundModeNote,
		"ROUND_EXECUTE_MODE_OVERRIDE": roundExecuteModeOverride,
		"ROUND_TRIAGE_PATH":           filepath.ToSlash(roundTriageWorktreePath),
		"ROUND_PLAN_PATH":             filepath.ToSlash(roundPlanWorktreePath),
		"ROUND_VERIFICATION_PATH":     filepath.ToSlash(roundVerificationWorktreePath),
		"ROUND_STATUS_PATH":           filepath.ToSlash(roundStatusWorktreePath),
		"ROUND_SUMMARY_PATH":          filepath.ToSlash(roundSummaryWorktreePath),
	}

	var codexContext *CodexContext
	for idx, templateName := range queue {
		label := executePromptLabel(templateName)
		stageName := "execute / " + label
		o.reporter.StageStarted(stageName, roundPtr(round), fmt.Sprintf("running execute step %d of %d", idx+1, len(queue)))

		templatePath := filepath.Join(o.promptsRoot, "execute", templateName)
		templateText, err := ReadTemplate(templatePath)
		if err != nil {
			o.reporter.StageFinished(stageName, roundPtr(round), false, progressMessage(err))
			o.reporter.StageFinished("execute stage", roundPtr(round), false, progressMessage(err))
			return RoundStatus{}, "", err
		}
		prompt, err := RenderTemplate(templateText, variables)
		if err != nil {
			o.reporter.StageFinished(stageName, roundPtr(round), false, progressMessage(err))
			o.reporter.StageFinished("execute stage", roundPtr(round), false, progressMessage(err))
			return RoundStatus{}, "", err
		}
		logPrefix := filepath.Join(executeDir, fmt.Sprintf("prompt-%02d", idx+1))
		preservePaths := executeRetryPreservePathsForPrompt(
			idx,
			reviewInputsDir,
			roundTriageWorktreePath,
			roundPlanWorktreePath,
			roundVerificationWorktreePath,
		)
		result, err := o.runPromptWithHeartbeat(
			executeWorktree,
			prompt,
			codexContext,
			true,
			logPrefix,
			[]string{
				roundTriageWorktreePath,
				roundPlanWorktreePath,
				roundVerificationWorktreePath,
				roundStatusWorktreePath,
				roundSummaryWorktreePath,
			},
			round,
			stageName,
			fmt.Sprintf("running execute step %d of %d", idx+1, len(queue)),
			"execute stage",
			fmt.Sprintf("running execute workflow (step %d/%d: %s)", idx+1, len(queue), label),
			func(_ int, _ int, _ *promptInactivityError) error {
				return resetMutablePromptWorktree(executeWorktree, o.config.GitBin, candidateHead, preservePaths)
			},
		)
		if err != nil {
			o.reporter.StageFinished(stageName, roundPtr(round), false, progressMessage(err))
			o.reporter.StageFinished("execute stage", roundPtr(round), false, progressMessage(err))
			return RoundStatus{}, "", err
		}
		nextContext := &CodexContext{
			ThreadID:          result.ThreadID,
			MulticodexProfile: strings.TrimSpace(result.MulticodexProfile),
		}
		if result.UsedMulticodex && idx+1 < len(queue) && nextContext.MulticodexProfile == "" {
			err := NewDeepReviewError(
				"multicodex did not report the selected profile for execute step %d; cannot safely resume thread %q. upgrade multicodex to a version that supports selected-profile metadata handoff",
				idx+1,
				result.ThreadID,
			)
			o.reporter.StageFinished(stageName, roundPtr(round), false, progressMessage(err))
			o.reporter.StageFinished("execute stage", roundPtr(round), false, progressMessage(err))
			return RoundStatus{}, "", err
		}
		codexContext = nextContext
		o.reporter.StageFinished(stageName, roundPtr(round), true, "completed")
	}

	if err := ensureCanonicalArtifact(roundStatusPath, []string{
		roundStatusWorktreePath,
		filepath.Join(executeWorktree, "round-status.json"),
		roundStatusPath,
	}); err != nil {
		o.reporter.StageFinished("execute stage", roundPtr(round), false, progressMessage(err))
		return RoundStatus{}, "", err
	}
	if err := ensureCanonicalArtifact(roundSummaryPath, []string{
		roundSummaryWorktreePath,
		filepath.Join(executeWorktree, "round-summary.md"),
		roundSummaryPath,
	}); err != nil {
		o.reporter.StageFinished("execute stage", roundPtr(round), false, progressMessage(err))
		return RoundStatus{}, "", err
	}
	if err := ensureCanonicalArtifact(roundTriagePath, []string{
		roundTriageWorktreePath,
		filepath.Join(executeWorktree, "round-triage.md"),
		roundTriagePath,
	}); err != nil {
		o.reporter.StageFinished("execute stage", roundPtr(round), false, progressMessage(err))
		return RoundStatus{}, "", err
	}
	if err := validateRoundTriagePolicy(roundTriagePath); err != nil {
		o.reporter.StageFinished("execute stage", roundPtr(round), false, progressMessage(err))
		return RoundStatus{}, "", err
	}
	if err := ensureCanonicalArtifact(roundPlanPath, []string{
		roundPlanWorktreePath,
		filepath.Join(executeWorktree, "round-plan.md"),
		roundPlanPath,
	}); err != nil {
		o.reporter.StageFinished("execute stage", roundPtr(round), false, progressMessage(err))
		return RoundStatus{}, "", err
	}
	if err := ensureCanonicalArtifact(roundVerificationPath, []string{
		roundVerificationWorktreePath,
		filepath.Join(executeWorktree, "round-verification.md"),
		roundVerificationPath,
	}); err != nil {
		o.reporter.StageFinished("execute stage", roundPtr(round), false, progressMessage(err))
		return RoundStatus{}, "", err
	}
	// Internal deepreview prompt artifacts must never end up in candidate commits.
	if err := os.RemoveAll(filepath.Join(executeWorktree, ".deepreview")); err != nil {
		o.reporter.StageFinished("execute stage", roundPtr(round), false, progressMessage(err))
		return RoundStatus{}, "", err
	}
	if err := CleanupUntrackedOperationalArtifacts(executeWorktree, o.config.GitBin); err != nil {
		o.reporter.StageFinished("execute stage", roundPtr(round), false, progressMessage(err))
		return RoundStatus{}, "", err
	}

	changed, err := HasUncommittedChanges(executeWorktree, o.config.GitBin)
	if err != nil {
		o.reporter.StageFinished("execute stage", roundPtr(round), false, progressMessage(err))
		return RoundStatus{}, "", err
	}
	if changed {
		err := NewDeepReviewError("execute worktree has uncommitted changes after prompt completion; prompt 2 must finish with a clean worktree: %s", executeWorktree)
		o.reporter.StageFinished("execute stage", roundPtr(round), false, progressMessage(err))
		return RoundStatus{}, "", err
	}

	candidateHeadAfter, err := RevParse(o.managedRepoPath, o.config.GitBin, candidateBranch)
	if err != nil {
		o.reporter.StageFinished("execute stage", roundPtr(round), false, progressMessage(err))
		return RoundStatus{}, "", err
	}
	if err := o.validateNoManagedOperationalArtifactChanges(candidateHead, candidateHeadAfter); err != nil {
		o.reporter.StageFinished("execute stage", roundPtr(round), false, progressMessage(err))
		return RoundStatus{}, "", err
	}

	status, err := readRoundStatus(roundStatusPath)
	if err != nil {
		o.reporter.StageFinished("execute stage", roundPtr(round), false, progressMessage(err))
		return RoundStatus{}, "", err
	}
	if err := writeRoundRecord(roundRecordPath, RoundRecord{
		Round:   round,
		Status:  status,
		Summary: filepath.Base(roundSummaryPath),
	}); err != nil {
		o.reporter.StageFinished("execute stage", roundPtr(round), false, progressMessage(err))
		return RoundStatus{}, "", err
	}
	o.reporter.StageFinished("execute stage", roundPtr(round), true, fmt.Sprintf("round status recorded (decision=%s)", status.Decision))
	return status, roundSummaryPath, nil
}

func stageHeartbeatMessage(base string, start time.Time) string {
	elapsed := time.Since(start).Round(time.Second)
	if elapsed < 0 {
		elapsed = 0
	}
	return fmt.Sprintf("%s (elapsed %s)", base, elapsed)
}

func (o *Orchestrator) runPromptWithHeartbeat(
	cwd, prompt string,
	codexContext *CodexContext,
	resetContextOnRetry bool,
	logPrefix string,
	monitoredPaths []string,
	round int,
	stageName, stageBaseMessage string,
	parentStageName, parentStageBaseMessage string,
	onBeforeRetry func(nextAttempt, maxAttempts int, inactivityErr *promptInactivityError) error,
) (CodexRunResult, error) {
	policy := o.promptWatchdogPolicy()
	result, _, err := o.runPromptWithWatchdog(
		currentRunCommandContext(),
		monitoredPromptRequest{
			label:               stageName,
			cwd:                 cwd,
			prompt:              prompt,
			codexContext:        codexContext,
			resetContextOnRetry: resetContextOnRetry,
			logPrefix:           logPrefix,
			useGitStatus:        true,
			monitoredPaths:      append(append([]string{}, monitoredPaths...), logPrefix+".stdout.jsonl", logPrefix+".stderr.log"),
		},
		monitoredPromptCallbacks{
			onHeartbeat: func(elapsed, silence time.Duration) {
				message := fmt.Sprintf("%s (elapsed %s)", stageBaseMessage, elapsed.Round(time.Second))
				if policy.inactivity > 0 {
					remaining := policy.inactivity - silence
					if remaining < 0 {
						remaining = 0
					}
					message = fmt.Sprintf("%s | inactivity timeout in %s", message, remaining.Round(time.Second))
				}
				o.reporter.StageProgress(stageName, message, roundPtr(round))
				if parentStageName != "" {
					parentMessage := fmt.Sprintf("%s (elapsed %s)", parentStageBaseMessage, elapsed.Round(time.Second))
					if policy.inactivity > 0 {
						remaining := policy.inactivity - silence
						if remaining < 0 {
							remaining = 0
						}
						parentMessage = fmt.Sprintf("%s | inactivity timeout in %s", parentMessage, remaining.Round(time.Second))
					}
					o.reporter.StageProgress(parentStageName, parentMessage, roundPtr(round))
				}
			},
			onBeforeRetry: onBeforeRetry,
			onRestart: func(nextAttempt, maxAttempts int, inactivityErr *promptInactivityError) {
				msg := fmt.Sprintf(
					"%s inactive for %s; restarting attempt %d/%d",
					stageName,
					inactivityErr.silence.Round(time.Second),
					nextAttempt,
					maxAttempts,
				)
				o.reporter.StageProgress(stageName, msg, roundPtr(round))
				if parentStageName != "" {
					o.reporter.StageProgress(parentStageName, msg, roundPtr(round))
				}
			},
		},
	)
	if err != nil {
		return CodexRunResult{}, err
	}
	return result, nil
}

func ensureCanonicalArtifact(canonicalPath string, candidates []string) error {
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		if _, err := os.Stat(candidate); err == nil {
			if candidate == canonicalPath {
				return nil
			}
			content, err := os.ReadFile(candidate)
			if err != nil {
				return err
			}
			return os.WriteFile(canonicalPath, content, 0o644)
		}
	}
	return os.ErrNotExist
}

func writeRoundRecord(path string, record RoundRecord) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	payload, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(payload, '\n'), 0o644)
}

func readRoundRecord(path string) (RoundRecord, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return RoundRecord{}, err
	}
	var record RoundRecord
	if err := json.Unmarshal(body, &record); err != nil {
		return RoundRecord{}, NewDeepReviewError("invalid round record %s: %v", path, err)
	}
	if record.Round < 1 {
		return RoundRecord{}, NewDeepReviewError("invalid round record %s: missing positive round number", path)
	}
	if strings.TrimSpace(record.Summary) == "" {
		return RoundRecord{}, NewDeepReviewError("invalid round record %s: missing summary artifact path", path)
	}
	statusPayload, err := json.Marshal(record.Status)
	if err != nil {
		return RoundRecord{}, NewDeepReviewError("invalid round record %s: %v", path, err)
	}
	status, err := readRoundStatusFromBytes(statusPayload)
	if err != nil {
		return RoundRecord{}, NewDeepReviewError("invalid round record %s: %v", path, err)
	}
	record.Status = status
	return record, nil
}

var (
	triageDispositionRegex = regexp.MustCompile(`(?i)\bdisposition\b[^a-z0-9]*(accept|reject|defer)\b`)
	triageImpactRegex      = regexp.MustCompile(`(?i)\bimpact\b[^a-z0-9]*(material|minor|unclear)\b`)
	triageConfidenceRegex  = regexp.MustCompile(`(?i)\bconfidence\b[^a-z0-9]*(high|medium|low)\b`)
	triageHeadingRegex     = regexp.MustCompile(`(?m)^###\s+(.+)$`)
)

func validateRoundTriagePolicy(path string) error {
	body, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	violations := triagePolicyViolations(string(body))
	if len(violations) == 0 {
		return nil
	}
	return NewDeepReviewError(
		"round triage validation failed: accepted items must be impact material with high confidence: %s",
		strings.Join(violations, "; "),
	)
}

func triagePolicyViolations(markdown string) []string {
	dispositions := triageDispositionRegex.FindAllStringSubmatchIndex(markdown, -1)
	if len(dispositions) == 0 {
		return nil
	}
	violations := make([]string, 0)
	for _, match := range dispositions {
		if len(match) < 4 {
			continue
		}
		disposition := strings.ToLower(markdown[match[2]:match[3]])
		if disposition != "accept" {
			continue
		}
		heading, section := triageSectionAt(markdown, match[0])
		impactMatch := triageImpactRegex.FindStringSubmatch(section)
		if len(impactMatch) < 2 {
			violations = append(violations, fmt.Sprintf("%s missing impact tag", heading))
		} else {
			impact := strings.ToLower(strings.TrimSpace(impactMatch[1]))
			if impact != "material" {
				violations = append(violations, fmt.Sprintf("%s has disallowed impact %q", heading, impact))
			}
		}

		confidenceMatch := triageConfidenceRegex.FindStringSubmatch(section)
		if len(confidenceMatch) < 2 {
			violations = append(violations, fmt.Sprintf("%s missing confidence tag", heading))
		} else {
			confidence := strings.ToLower(strings.TrimSpace(confidenceMatch[1]))
			if confidence != "high" {
				violations = append(violations, fmt.Sprintf("%s has disallowed confidence %q", heading, confidence))
			}
		}
	}
	return violations
}

func triageSectionAt(markdown string, offset int) (string, string) {
	if offset < 0 {
		offset = 0
	}
	if offset > len(markdown) {
		offset = len(markdown)
	}
	headings := triageHeadingRegex.FindAllStringSubmatchIndex(markdown, -1)
	if len(headings) == 0 {
		return "accepted item", markdown
	}
	for idx, match := range headings {
		if len(match) < 4 {
			continue
		}
		start := match[0]
		end := len(markdown)
		if idx+1 < len(headings) {
			end = headings[idx+1][0]
		}
		if offset < start || offset >= end {
			continue
		}
		heading := strings.TrimSpace(markdown[match[2]:match[3]])
		if heading == "" {
			heading = "accepted item"
		}
		return heading, markdown[start:end]
	}
	return "accepted item", markdown
}

func isInternalArtifactPath(path string) bool {
	normalized := filepath.ToSlash(strings.TrimSpace(path))
	return strings.HasPrefix(normalized, ".deepreview/") || strings.HasPrefix(normalized, ".tmp/deepreview/")
}

func managedOperationalArtifactRoot(path string) (string, bool) {
	normalized := filepath.ToSlash(strings.TrimSpace(path))
	longest := ""
	for _, pattern := range worktreeOperationalExcludePatterns {
		root := strings.TrimSuffix(strings.TrimSpace(pattern), "/")
		if root == "" {
			continue
		}
		if normalized == root || strings.HasPrefix(normalized, root+"/") {
			if len(root) > len(longest) {
				longest = root
			}
		}
	}
	if longest == "" {
		return "", false
	}
	return longest, true
}

func (o *Orchestrator) validateNoManagedOperationalArtifactChanges(baseRef, headRef string) error {
	files, err := ListChangedFiles(o.managedRepoPath, o.config.GitBin, baseRef, headRef)
	if err != nil {
		return err
	}
	for _, file := range files {
		if isInternalArtifactPath(file) {
			return NewDeepReviewError("internal deepreview artifacts must not be committed: %s", file)
		}
		root, ok := managedOperationalArtifactRoot(file)
		if !ok {
			continue
		}
		trackedAtBase, err := RefHasTrackedEntries(o.managedRepoPath, o.config.GitBin, baseRef, root)
		if err != nil {
			return err
		}
		if trackedAtBase {
			continue
		}
		return NewDeepReviewError("operational artifacts must not be committed: %s", file)
	}
	return nil
}

func (o *Orchestrator) validateDeliveryFiles(candidateBranch string) ([]string, error) {
	return o.validateDeliveryFilesBetween("origin/"+o.config.SourceBranch, candidateBranch)
}

func (o *Orchestrator) validateDeliveryFilesBetween(baseRef, headRef string) ([]string, error) {
	files, err := ListChangedFiles(o.managedRepoPath, o.config.GitBin, baseRef, headRef)
	if err != nil {
		return nil, err
	}
	for _, file := range files {
		if isInternalArtifactPath(file) {
			return nil, NewDeepReviewError("delivery blocked: internal deepreview artifacts detected in branch diff: %s", file)
		}
		root, ok := managedOperationalArtifactRoot(file)
		if !ok {
			continue
		}
		trackedAtSource, err := RefHasTrackedEntries(o.managedRepoPath, o.config.GitBin, baseRef, root)
		if err != nil {
			return nil, err
		}
		if trackedAtSource {
			continue
		}
		return nil, NewDeepReviewError("delivery blocked: operational artifacts detected in branch diff: %s", file)
	}
	return files, nil
}

func summarizePrivacyScanIssues(commitScanErr, fileScanErr error) string {
	issues := make([]string, 0, 2)
	if commitScanErr != nil {
		issues = append(issues, "commit-message scan: "+progressMessage(commitScanErr))
	}
	if fileScanErr != nil {
		issues = append(issues, "changed-file scan: "+progressMessage(fileScanErr))
	}
	if len(issues) == 0 {
		return "none"
	}
	return strings.Join(issues, " | ")
}

func snapshotPaths(basePath string, preservePaths []string) (string, error) {
	if len(preservePaths) == 0 {
		return "", nil
	}
	snapshotDir, err := os.MkdirTemp("", "deepreview-worktree-reset-*")
	if err != nil {
		return "", err
	}
	for _, preservePath := range preservePaths {
		trimmed := strings.TrimSpace(preservePath)
		if trimmed == "" {
			continue
		}
		if err := snapshotOnePath(basePath, snapshotDir, trimmed); err != nil {
			_ = os.RemoveAll(snapshotDir)
			return "", err
		}
	}
	return snapshotDir, nil
}

func snapshotOnePath(basePath, snapshotDir, preservePath string) error {
	info, err := os.Lstat(preservePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	relPath, err := filepath.Rel(basePath, preservePath)
	if err != nil {
		return err
	}
	if relPath == "." || strings.HasPrefix(relPath, ".."+string(os.PathSeparator)) || relPath == ".." {
		return NewDeepReviewError("preserved path must stay within worktree: %s", preservePath)
	}
	dstPath := filepath.Join(snapshotDir, relPath)
	if info.IsDir() {
		return copyDirectory(preservePath, dstPath)
	}
	return copyFileWithMode(preservePath, dstPath, info.Mode())
}

func restoreSnapshot(basePath, snapshotDir string) error {
	if strings.TrimSpace(snapshotDir) == "" {
		return nil
	}
	defer func() {
		_ = os.RemoveAll(snapshotDir)
	}()
	return filepath.Walk(snapshotDir, func(current string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if current == snapshotDir {
			return nil
		}
		relPath, err := filepath.Rel(snapshotDir, current)
		if err != nil {
			return err
		}
		dstPath := filepath.Join(basePath, relPath)
		if info.IsDir() {
			return os.MkdirAll(dstPath, info.Mode().Perm())
		}
		if info.Mode()&os.ModeSymlink != 0 {
			target, err := os.Readlink(current)
			if err != nil {
				return err
			}
			if err := os.MkdirAll(filepath.Dir(dstPath), 0o755); err != nil {
				return err
			}
			_ = os.Remove(dstPath)
			return os.Symlink(target, dstPath)
		}
		return copyFileWithMode(current, dstPath, info.Mode())
	})
}

func copyDirectory(srcPath, dstPath string) error {
	return filepath.Walk(srcPath, func(current string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relPath, err := filepath.Rel(srcPath, current)
		if err != nil {
			return err
		}
		targetPath := dstPath
		if relPath != "." {
			targetPath = filepath.Join(dstPath, relPath)
		}
		if info.IsDir() {
			return os.MkdirAll(targetPath, info.Mode().Perm())
		}
		if info.Mode()&os.ModeSymlink != 0 {
			linkTarget, err := os.Readlink(current)
			if err != nil {
				return err
			}
			if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
				return err
			}
			return os.Symlink(linkTarget, targetPath)
		}
		return copyFileWithMode(current, targetPath, info.Mode())
	})
}

func copyFileWithMode(srcPath, dstPath string, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(dstPath), 0o755); err != nil {
		return err
	}
	src, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer src.Close()
	dst, err := os.OpenFile(dstPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode.Perm())
	if err != nil {
		return err
	}
	defer dst.Close()
	if _, err := io.Copy(dst, src); err != nil {
		return err
	}
	return nil
}

func resetMutablePromptWorktree(worktreePath, gitBin, baselineRef string, preservePaths []string) error {
	snapshotDir, err := snapshotPaths(worktreePath, preservePaths)
	if err != nil {
		return err
	}
	if err := ResetWorktreeToRef(worktreePath, gitBin, baselineRef); err != nil {
		if snapshotDir != "" {
			_ = os.RemoveAll(snapshotDir)
		}
		return err
	}
	return restoreSnapshot(worktreePath, snapshotDir)
}

func executeRetryPreservePathsForPrompt(promptIdx int, reviewInputsDir, roundTriagePath, roundPlanPath, roundVerificationPath string) []string {
	// Preserve only artifacts from earlier successful prompts. Final round
	// status and summary must be rewritten by the successful final prompt attempt.
	paths := []string{reviewInputsDir}
	if promptIdx >= 1 {
		paths = append(paths, roundTriagePath, roundPlanPath)
	}
	if promptIdx >= 2 {
		paths = append(paths, roundVerificationPath)
	}
	return paths
}

func executePromptLabel(templateName string) string {
	switch templateName {
	case "01-triage-plan.md":
		return "triage and plan"
	case "02-implement-verify-finalize.md":
		return "implement, verify, finalize"
	default:
		return templateName
	}
}

func buildReviewInputManifest(reportPaths []string) (string, error) {
	chunks := make([]string, 0, len(reportPaths))
	for _, path := range reportPaths {
		b, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		chunks = append(chunks, summarizeReviewVerdict(filepath.Base(path), string(b)))
	}
	return strings.TrimSpace(strings.Join(chunks, "\n")), nil
}

func summarizeReviewVerdict(reportName, markdown string) string {
	lines := strings.Split(markdown, "\n")
	summary := []string{fmt.Sprintf("- `%s`", reportName)}
	section := ""
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		switch {
		case strings.HasPrefix(line, "## "):
			section = strings.TrimSpace(strings.TrimPrefix(line, "## "))
			continue
		}
		if strings.EqualFold(section, "Verdict") && strings.HasPrefix(line, "- ") {
			summary = append(summary, "  "+trimForDisplay(line, 280))
		}
	}
	return strings.Join(summary, "\n")
}

func readRoundStatus(path string) (RoundStatus, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return RoundStatus{}, err
	}
	return readRoundStatusFromBytes(b)
}

func readRoundStatusFromBytes(b []byte) (RoundStatus, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil || raw == nil {
		return RoundStatus{}, NewDeepReviewError("round status must be a JSON object")
	}

	decisionRaw, ok := raw["decision"]
	if !ok {
		return RoundStatus{}, NewDeepReviewError("round status validation failed: decision field is required")
	}
	var decision string
	if err := json.Unmarshal(decisionRaw, &decision); err != nil {
		return RoundStatus{}, NewDeepReviewError("round status validation failed: decision must be a string")
	}
	if decision != "continue" && decision != "stop" {
		return RoundStatus{}, NewDeepReviewError("round status validation failed: decision must be 'continue' or 'stop'")
	}

	reasonRaw, ok := raw["reason"]
	if !ok {
		return RoundStatus{}, NewDeepReviewError("round status validation failed: reason field is required")
	}
	var reason string
	if err := json.Unmarshal(reasonRaw, &reason); err != nil {
		return RoundStatus{}, NewDeepReviewError("round status validation failed: reason must be a string")
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return RoundStatus{}, NewDeepReviewError("round status validation failed: reason must be a non-empty string")
	}

	status := RoundStatus{
		Decision: decision,
		Reason:   reason,
	}

	if confidenceRaw, ok := raw["confidence"]; ok {
		var confidence float64
		if err := json.Unmarshal(confidenceRaw, &confidence); err != nil {
			return RoundStatus{}, NewDeepReviewError("round status validation failed: confidence must be numeric")
		}
		if confidence < 0.0 || confidence > 1.0 {
			return RoundStatus{}, NewDeepReviewError("round status validation failed: confidence must be between 0.0 and 1.0")
		}
		status.Confidence = &confidence
	}

	if nextFocusRaw, ok := raw["next_focus"]; ok {
		var nextFocus string
		if err := json.Unmarshal(nextFocusRaw, &nextFocus); err != nil {
			return RoundStatus{}, NewDeepReviewError("round status validation failed: next_focus must be a string")
		}
		status.NextFocus = &nextFocus
	}

	return status, nil
}

func (o *Orchestrator) secretHygieneScan(repoPath, candidateBranch string) error {
	return o.secretHygieneScanBetween("origin/"+o.config.SourceBranch, candidateBranch)
}

func (o *Orchestrator) secretHygieneScanBetween(baseRef, headRef string) error {
	changedFiles, err := ListChangedFiles(o.managedRepoPath, o.config.GitBin, baseRef, headRef)
	if err != nil {
		return err
	}

	for _, rel := range changedFiles {
		addedLines, err := AddedLinesBetweenRefs(o.managedRepoPath, o.config.GitBin, baseRef, headRef, rel)
		if err != nil {
			return privacyScanReadFailure(rel, "read added lines")
		}
		if len(addedLines) > 0 {
			if err := privacyScanText(strings.Join(addedLines, "\n"), rel); err != nil {
				return err
			}
			continue
		}
		status, err := ChangedFileStatusBetweenRefs(o.managedRepoPath, o.config.GitBin, baseRef, headRef, rel)
		if err != nil {
			return privacyScanReadFailure(rel, "read file change status")
		}
		if status == "D" {
			continue
		}
		// Binary diffs do not expose textual added lines, so scan the candidate-side bytes directly.
		isBinaryDiff, err := DiffIsBinaryBetweenRefs(o.managedRepoPath, o.config.GitBin, baseRef, headRef, rel)
		if err != nil {
			return privacyScanReadFailure(rel, "inspect binary diff status")
		}
		if !isBinaryDiff {
			continue
		}
		headContent, err := FileContentAtRef(o.managedRepoPath, o.config.GitBin, headRef, rel)
		if err != nil {
			return privacyScanReadFailure(rel, "read candidate file contents")
		}
		if len(headContent) == 0 {
			continue
		}
		var baseContent []byte
		if status == "M" {
			baseContent, err = FileContentAtRef(o.managedRepoPath, o.config.GitBin, baseRef, rel)
			if err != nil {
				return privacyScanReadFailure(rel, "read base file contents")
			}
		}
		if err := privacyScanModifiedBinaryText(string(baseContent), string(headContent), rel); err != nil {
			return err
		}
	}
	return nil
}

func privacyScanReadFailure(rel, action string) error {
	return NewDeepReviewError("privacy scan failed: unable to %s for %s", action, rel)
}

func privacyScanText(text, rel string) error {
	if containsDisallowedEmail(text) {
		return NewDeepReviewError("privacy scan failed: disallowed email-like value detected in %s", rel)
	}
	for _, pattern := range allSecretRiskyPatterns() {
		if pattern.MatchString(text) {
			return NewDeepReviewError("privacy scan failed: secret-like pattern matched in %s", rel)
		}
	}
	for _, pattern := range personalRiskyPatterns {
		if pattern.MatchString(text) {
			return NewDeepReviewError("privacy scan failed: personal-info-like pattern matched in %s", rel)
		}
	}
	for _, pattern := range privatePathPatterns {
		if pattern.MatchString(text) {
			return NewDeepReviewError("privacy scan failed: local path pattern matched in %s", rel)
		}
	}
	return nil
}

func privacyScanModifiedBinaryText(baseText, headText, rel string) error {
	if hasNewDisallowedEmail(baseText, headText) {
		return NewDeepReviewError("privacy scan failed: disallowed email-like value detected in %s", rel)
	}
	if hasNewRegexMatch(baseText, headText, allSecretRiskyPatterns()) {
		return NewDeepReviewError("privacy scan failed: secret-like pattern matched in %s", rel)
	}
	if hasNewRegexMatch(baseText, headText, personalRiskyPatterns) {
		return NewDeepReviewError("privacy scan failed: personal-info-like pattern matched in %s", rel)
	}
	if hasNewRegexMatch(baseText, headText, privatePathPatterns) {
		return NewDeepReviewError("privacy scan failed: local path pattern matched in %s", rel)
	}
	return nil
}

func hasNewDisallowedEmail(baseText, headText string) bool {
	return hasNewExactMatch(disallowedEmailMatches(baseText), disallowedEmailMatches(headText))
}

func disallowedEmailMatches(text string) []string {
	allMatches := emailPattern.FindAllString(text, -1)
	matches := make([]string, 0, len(allMatches))
	for _, match := range allMatches {
		if isAllowedPlaceholderEmail(match) {
			continue
		}
		matches = append(matches, match)
	}
	return matches
}

func hasNewRegexMatch(baseText, headText string, patterns []*regexp.Regexp) bool {
	return hasNewExactMatch(regexMatchKeys(baseText, patterns), regexMatchKeys(headText, patterns))
}

func regexMatchKeys(text string, patterns []*regexp.Regexp) []string {
	var matches []string
	for idx, pattern := range patterns {
		for _, match := range pattern.FindAllString(text, -1) {
			matches = append(matches, fmt.Sprintf("%d:%s", idx, match))
		}
	}
	return matches
}

func hasNewExactMatch(baseMatches, headMatches []string) bool {
	if len(headMatches) == 0 {
		return false
	}
	counts := make(map[string]int, len(baseMatches))
	for _, match := range baseMatches {
		counts[match]++
	}
	for _, match := range headMatches {
		if counts[match] > 0 {
			counts[match]--
			continue
		}
		return true
	}
	return false
}

const localPathScanFailurePrefix = "privacy scan failed: local path pattern matched in "

func (o *Orchestrator) tryAutoRemediateLocalPathPrivacyViolation(repoPath, candidateBranch string, scanErr error) (bool, error) {
	relPath, ok := extractLocalPathScanFailurePath(scanErr)
	if !ok {
		return false, nil
	}
	if !isAutoSanitizableDocPath(relPath) {
		return false, nil
	}
	if _, err := Git(repoPath, o.config.GitBin, true, "checkout", candidateBranch); err != nil {
		return false, err
	}
	targetPath := filepath.Join(repoPath, relPath)
	content, err := os.ReadFile(targetPath)
	if err != nil {
		if !os.IsNotExist(err) {
			return false, err
		}
		content, err = FileContentAtRef(repoPath, o.config.GitBin, candidateBranch, relPath)
		if err != nil {
			return false, err
		}
	}
	sanitized := replaceLocalPathsWithPlaceholder(string(content))
	if sanitized == string(content) {
		return false, nil
	}
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return false, err
	}
	if err := os.WriteFile(targetPath, []byte(sanitized), 0o644); err != nil {
		return false, err
	}
	if err := CommitAllChanges(repoPath, o.config.GitBin, "deepreview: sanitize local paths for privacy scan", o.commitIdentity); err != nil {
		return false, err
	}
	if err := CleanupUntrackedOperationalArtifacts(repoPath, o.config.GitBin); err != nil {
		return false, err
	}
	if err := o.validateNoManagedOperationalArtifactChanges("origin/"+o.config.SourceBranch, candidateBranch); err != nil {
		return false, err
	}
	return true, nil
}

func extractLocalPathScanFailurePath(scanErr error) (string, bool) {
	if scanErr == nil {
		return "", false
	}
	message := strings.TrimSpace(scanErr.Error())
	if !strings.HasPrefix(message, localPathScanFailurePrefix) {
		return "", false
	}
	relPath := strings.TrimSpace(strings.TrimPrefix(message, localPathScanFailurePrefix))
	if relPath == "" {
		return "", false
	}
	return relPath, true
}

func isAutoSanitizableDocPath(relPath string) bool {
	normalized := filepath.ToSlash(strings.TrimSpace(relPath))
	if !strings.HasPrefix(normalized, "docs/") {
		return false
	}
	ext := strings.ToLower(filepath.Ext(normalized))
	switch ext {
	case ".md", ".markdown", ".txt", ".rst", ".adoc":
		return true
	default:
		return false
	}
}

func replaceLocalPathsWithPlaceholder(text string) string {
	sanitized := text
	for _, pattern := range privatePathPatterns {
		sanitized = pattern.ReplaceAllString(sanitized, "/path/to/project")
	}
	return sanitized
}

func (o *Orchestrator) autoCommitWorktreeChangesIfNeeded(repoPath, message string) error {
	changed, err := HasUncommittedChanges(repoPath, o.config.GitBin)
	if err != nil {
		return err
	}
	if !changed {
		return nil
	}
	if err := CommitAllChanges(repoPath, o.config.GitBin, message, o.commitIdentity); err != nil {
		return err
	}
	changed, err = HasUncommittedChanges(repoPath, o.config.GitBin)
	if err != nil {
		return err
	}
	if changed {
		return NewDeepReviewError("worktree still has uncommitted changes after auto-commit: %s", repoPath)
	}
	return nil
}

func (o *Orchestrator) deliveryCommitMessageScan(candidateBranch string) error {
	return o.deliveryCommitMessageScanBetween("origin/"+o.config.SourceBranch, candidateBranch)
}

func (o *Orchestrator) deliveryCommitMessageScanBetween(baseRef, headRef string) error {
	out, err := Git(
		o.managedRepoPath,
		o.config.GitBin,
		true,
		"log",
		"--format=%s%n%b%x00",
		baseRef+".."+headRef,
	)
	if err != nil {
		return err
	}
	for _, rawEntry := range strings.Split(out, "\x00") {
		entry := strings.TrimSpace(rawEntry)
		if entry == "" {
			continue
		}
		if textHasDisallowedSensitivePattern(entry) {
			return NewDeepReviewError("privacy scan failed: disallowed sensitive content detected in delivery commit message")
		}
	}
	return nil
}

func (o *Orchestrator) deliveryPushRangePolicyScanBetween(baseRef, headRef string) error {
	policyWorktree, cleanup, err := o.addTemporaryPolicyWorktree(headRef)
	if err != nil {
		return err
	}
	defer cleanup()
	scriptPath := filepath.Join(policyWorktree, "scripts", "security", "check-push-range.sh")
	if _, err := os.Stat(scriptPath); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	env := append(
		os.Environ(),
		"PRE_COMMIT_FROM_REF="+strings.TrimSpace(baseRef),
		"PRE_COMMIT_TO_REF="+strings.TrimSpace(headRef),
	)
	completed, err := RunCommandContextWithEnvAndCallbacks(
		currentRunCommandContext(),
		[]string{"bash", "scripts/security/check-push-range.sh"},
		policyWorktree,
		env,
		"",
		false,
		0,
		nil,
	)
	if err != nil {
		return err
	}
	if completed.ReturnCode == 0 {
		return nil
	}
	snippet := firstNonEmptyLine(completed.Stderr)
	if snippet == "" {
		snippet = firstNonEmptyLine(completed.Stdout)
	}
	snippet = sanitizePublicText(strings.TrimSpace(snippet))
	if snippet == "" {
		snippet = "repo push-range sensitive-text policy failed"
	}
	return NewDeepReviewError(
		"delivery push-range policy failed for %s..%s: %s",
		strings.TrimSpace(baseRef),
		strings.TrimSpace(headRef),
		trimForDisplay(snippet, 220),
	)
}

func markdownBulletList(items []string) string {
	if len(items) == 0 {
		return "none"
	}
	lines := make([]string, 0, len(items))
	for _, item := range items {
		trimmed := strings.TrimSpace(item)
		if trimmed == "" {
			continue
		}
		lines = append(lines, "- `"+sanitizePublicText(trimmed)+"`")
	}
	if len(lines) == 0 {
		return "none"
	}
	return strings.Join(lines, "\n")
}

func readPromptDeliveryResult(path string, expectedMode string) (promptDeliveryResult, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return promptDeliveryResult{}, err
	}
	var result promptDeliveryResult
	if err := json.Unmarshal(body, &result); err != nil {
		return promptDeliveryResult{}, NewDeepReviewError("invalid delivery result %s: %v", path, err)
	}
	result.Mode = strings.TrimSpace(result.Mode)
	result.DeliveryBranch = strings.TrimSpace(result.DeliveryBranch)
	result.IncompleteReason = strings.TrimSpace(result.IncompleteReason)
	if result.Mode == "" {
		return promptDeliveryResult{}, NewDeepReviewError("invalid delivery result %s: missing mode", path)
	}
	if expectedMode != "" && result.Mode != expectedMode {
		return promptDeliveryResult{}, NewDeepReviewError("invalid delivery result %s: mode %q does not match expected %q", path, result.Mode, expectedMode)
	}
	return result, nil
}

func (o *Orchestrator) resolvePreparedDeliveryRef(candidateBranch string) (string, string, string, error) {
	switch o.config.Mode {
	case ModePR:
		deliveryBranch := o.deliveryBranchName()
		return candidateBranch, candidateBranch + ":" + deliveryBranch, deliveryBranch, nil
	case ModeYolo:
		return candidateBranch, candidateBranch + ":" + o.config.SourceBranch, o.config.SourceBranch, nil
	default:
		return "", "", "", NewDeepReviewError("unsupported delivery mode: %s", o.config.Mode)
	}
}

func (o *Orchestrator) validatePreparedDeliveryRef(candidateHead, candidateBranch, preparedRef string) error {
	trimmedPreparedRef := strings.TrimSpace(preparedRef)
	if trimmedPreparedRef == "" {
		return NewDeepReviewError("delivery publish ref is required")
	}
	if strings.TrimSpace(candidateHead) == "" {
		return NewDeepReviewError("delivery candidate head is required")
	}
	trimmedCandidateBranch := strings.TrimSpace(candidateBranch)
	if trimmedCandidateBranch == "" {
		return NewDeepReviewError("delivery candidate branch is required")
	}
	candidateContainsOriginal, err := RefContainsCommit(o.managedRepoPath, o.config.GitBin, candidateHead, trimmedCandidateBranch)
	if err != nil {
		return err
	}
	if !candidateContainsOriginal {
		return NewDeepReviewError(
			"delivery candidate branch lost the reviewed candidate tip; rerun recovery on the candidate branch instead of preparing a different publish ref: %s",
			trimmedCandidateBranch,
		)
	}
	if trimmedPreparedRef != trimmedCandidateBranch {
		return NewDeepReviewError(
			"delivery must publish the reviewed candidate branch directly, not a separate prepared ref: %s",
			trimmedPreparedRef,
		)
	}
	return nil
}

func (o *Orchestrator) addTemporaryPolicyWorktree(ref string) (string, func(), error) {
	worktreeRoot := filepath.Join(o.runRoot, "tmp")
	if err := os.MkdirAll(worktreeRoot, 0o755); err != nil {
		return "", nil, err
	}
	worktreePath, err := os.MkdirTemp(worktreeRoot, "push-range-policy-")
	if err != nil {
		return "", nil, err
	}
	if err := os.RemoveAll(worktreePath); err != nil {
		return "", nil, err
	}
	if err := AddDetachedWorktree(o.managedRepoPath, o.config.GitBin, worktreePath, ref); err != nil {
		return "", nil, err
	}
	// Run repo-native push-range policy from the exact ref being published so
	// branch-specific scripts and helper files travel with that ref.
	cleanup := func() {
		_ = RemoveWorktree(o.managedRepoPath, o.config.GitBin, worktreePath)
		_ = os.RemoveAll(worktreePath)
	}
	return worktreePath, cleanup, nil
}

func (o *Orchestrator) validatePublishableDeliveryRef(baseRef, candidateHead, candidateBranch, publishRef string) ([]string, error) {
	if err := o.validatePreparedDeliveryRef(candidateHead, candidateBranch, publishRef); err != nil {
		return nil, err
	}
	// Both complete delivery and incomplete fallback must validate the exact ref
	// that will be pushed, not just the current candidate tip.
	changedFiles, err := o.validateDeliveryFilesBetween(baseRef, publishRef)
	if err != nil {
		return nil, err
	}
	if len(changedFiles) == 0 {
		return changedFiles, nil
	}
	if err := o.deliveryPushRangePolicyScanBetween(baseRef, publishRef); err != nil {
		return nil, err
	}
	if err := o.deliveryCommitMessageScanBetween(baseRef, publishRef); err != nil {
		return nil, err
	}
	if err := o.secretHygieneScanBetween(baseRef, publishRef); err != nil {
		return nil, err
	}
	return changedFiles, nil
}

func isRecoverableDeliveryBlocker(err error) bool {
	if err == nil {
		return false
	}
	message := strings.TrimSpace(err.Error())
	switch {
	case strings.HasPrefix(message, "delivery blocked:"):
		return true
	case strings.HasPrefix(message, "delivery push-range policy failed"):
		return true
	case strings.HasPrefix(message, "privacy scan failed:"):
		return true
	default:
		return false
	}
}

func deliveryRecoveryModeNote(baseRef, blocker string) string {
	return strings.TrimSpace(fmt.Sprintf(
		"## Delivery Recovery Mode\n"+
			"- This round was triggered by a delivery-readiness blocker after the normal review loop.\n"+
			"- Delivery is read-only for tracked repository content. Any required publishability fix must happen here on the candidate branch, not in delivery.\n"+
			"- Focus only on the blocker below and its immediate consequences.\n"+
			"- Publish base ref: `%s`\n"+
			"- Delivery blocker: %s\n",
		strings.TrimSpace(baseRef),
		sanitizePublicText(trimForDisplay(strings.TrimSpace(blocker), 500)),
	))
}

func deliveryRecoveryExecuteOverride(baseRef string) string {
	return strings.TrimSpace(fmt.Sprintf(
		"This is a delivery-recovery round focused only on restoring publishability against `%s`.\n\n"+
			"If delivery readiness is blocked by tracked content or history in the candidate branch, fix that here on the candidate branch. When the blocker is history-scoped and the intended current tree is otherwise correct, you may rebuild the candidate branch itself onto the correct base and replay the intended final tree there, provided that:\n"+
			"- the candidate branch remains the single branch that will later be published\n"+
			"- the rebuilt candidate state is fully verified in this round\n"+
			"- you do not create or rely on a separate delivery-only branch\n",
		strings.TrimSpace(baseRef),
	))
}

func deliveryConfirmationModeNote(baseRef string) string {
	return strings.TrimSpace(fmt.Sprintf(
		"## Delivery Confirmation Mode\n"+
			"- This is an automatic final audit round after a delivery-recovery change.\n"+
			"- Prioritize verifying that the candidate branch is now publishable against `%s` and that recovery did not introduce unrelated churn.\n",
		strings.TrimSpace(baseRef),
	))
}

func deliveryConfirmationExecuteOverride() string {
	return "This is an automatic final audit round. Prefer verification, review, and narrowly scoped follow-up over new broad changes."
}

func buildDeliveryRecoveryReviewReport(path, baseRef, blocker string) error {
	body := strings.TrimSpace(fmt.Sprintf(
		"# Delivery Recovery Review\n\n"+
			"## Verdict\n"+
			"- material_findings_found: yes\n"+
			"- merge_readiness: needs_fixes\n\n"+
			"## Material Findings\n"+
			"### delivery publishability blocker\n"+
			"- Category: delivery-readiness\n"+
			"- Impact: material\n"+
			"- Location: candidate branch vs `%s`\n"+
			"- Why it matters: deepreview must publish the exact reviewed candidate branch, and the current candidate branch is not yet publishable.\n"+
			"- Evidence: %s\n"+
			"- Recommendation: fix only this publishability blocker on the candidate branch, verify it locally, and keep delivery read-only for tracked repository content.\n"+
			"- Confidence: high\n\n"+
			"## Verification ideas\n"+
			"- rerun repo-native push-range/privacy checks against `%s..candidate`\n"+
			"- rerun targeted tests and checks for the touched files\n",
		strings.TrimSpace(baseRef),
		sanitizePublicText(trimForDisplay(strings.TrimSpace(blocker), 500)),
		strings.TrimSpace(baseRef),
	)) + "\n"
	return os.WriteFile(path, []byte(body), 0o644)
}

func (o *Orchestrator) runDeliveryRecoveryCycle(defaultBranch, candidateBranch string, roundSummaries *[]string, blocker error) ([]string, error) {
	baseRef := "origin/" + o.config.SourceBranch
	initialRound := len(*roundSummaries) + 1
	totalRounds := initialRound + deliveryRecoveryRoundBudget - 1
	if reporterWithMaxRounds, ok := o.reporter.(MaxRoundsAwareProgressReporter); ok {
		reporterWithMaxRounds.SetMaxRounds(totalRounds)
	}
	o.reporter.StageProgress(
		"delivery",
		fmt.Sprintf(
			"candidate branch is not yet publishable; running one bounded delivery-recovery cycle (%s)",
			sanitizePublicText(trimForDisplay(strings.TrimSpace(blocker.Error()), 220)),
		),
		nil,
	)

	recoveryRoundDir := filepath.Join(o.runRoot, fmt.Sprintf("round-%02d", initialRound))
	if err := os.MkdirAll(recoveryRoundDir, 0o755); err != nil {
		return nil, err
	}
	recoveryReviewPath := filepath.Join(recoveryRoundDir, "review-01.md")
	if err := buildDeliveryRecoveryReviewReport(recoveryReviewPath, baseRef, blocker.Error()); err != nil {
		return nil, err
	}
	candidateHeadBefore, err := RevParse(o.managedRepoPath, o.config.GitBin, candidateBranch)
	if err != nil {
		return nil, err
	}
	_, recoverySummaryPath, err := o.runExecuteStage(
		initialRound,
		recoveryRoundDir,
		candidateBranch,
		candidateHeadBefore,
		defaultBranch,
		[]string{recoveryReviewPath},
		totalRounds,
		executeRoundOptions{
			modeNote:            deliveryRecoveryModeNote(baseRef, blocker.Error()),
			executeModeOverride: deliveryRecoveryExecuteOverride(baseRef),
		},
	)
	if err != nil {
		return nil, err
	}
	*roundSummaries = append(*roundSummaries, recoverySummaryPath)

	confirmationRound := initialRound + 1
	confirmationRoundDir := filepath.Join(o.runRoot, fmt.Sprintf("round-%02d", confirmationRound))
	if err := os.MkdirAll(confirmationRoundDir, 0o755); err != nil {
		return nil, err
	}
	candidateHeadBefore, err = RevParse(o.managedRepoPath, o.config.GitBin, candidateBranch)
	if err != nil {
		return nil, err
	}
	reviewReports, err := o.runReviewStage(confirmationRound, confirmationRoundDir, candidateHeadBefore, defaultBranch)
	if err != nil {
		return nil, err
	}
	confirmationStatus, confirmationSummaryPath, err := o.runExecuteStage(
		confirmationRound,
		confirmationRoundDir,
		candidateBranch,
		candidateHeadBefore,
		defaultBranch,
		reviewReports,
		totalRounds,
		executeRoundOptions{
			modeNote:            deliveryConfirmationModeNote(baseRef),
			executeModeOverride: deliveryConfirmationExecuteOverride(),
		},
	)
	if err != nil {
		return nil, err
	}
	*roundSummaries = append(*roundSummaries, confirmationSummaryPath)
	if confirmationStatus.Decision == "continue" {
		return nil, NewDeepReviewError(
			"delivery recovery confirmation round still requires further work; rerun deepreview with a higher --max-rounds if more recovery is needed",
		)
	}

	candidateHead, err := RevParse(o.managedRepoPath, o.config.GitBin, candidateBranch)
	if err != nil {
		return nil, err
	}
	o.reviewedCandidateHead = candidateHead
	return o.validatePublishableDeliveryRef(baseRef, candidateHead, candidateBranch, candidateBranch)
}

func (o *Orchestrator) recoverCandidateDeliveryReadiness(defaultBranch, candidateBranch string, roundSummaries *[]string) ([]string, error) {
	baseRef := "origin/" + o.config.SourceBranch
	candidateHead, err := o.reviewedCandidateHeadForPublish(candidateBranch)
	if err != nil {
		return nil, err
	}
	validatedChangedFiles, err := o.validatePublishableDeliveryRef(baseRef, candidateHead, candidateBranch, candidateBranch)
	if err == nil {
		return validatedChangedFiles, nil
	}
	if !isRecoverableDeliveryBlocker(err) {
		return nil, err
	}
	return o.runDeliveryRecoveryCycle(defaultBranch, candidateBranch, roundSummaries, err)
}

func (o *Orchestrator) reviewedCandidateHeadForPublish(candidateBranch string) (string, error) {
	if trimmed := strings.TrimSpace(o.reviewedCandidateHead); trimmed != "" {
		return trimmed, nil
	}
	return RevParse(o.managedRepoPath, o.config.GitBin, candidateBranch)
}

func (o *Orchestrator) validateMergeReadyPR(prURL string) error {
	deadline := time.Now().Add(mergeReadyPRValidationWindow)
	reportedPending := false
	for {
		state, err := o.readPullRequestMergeState(prURL)
		if err != nil {
			return err
		}
		if strings.TrimSpace(state.URL) == "" {
			return NewDeepReviewError("gh pr view returned empty url for %s", prURL)
		}
		if normalizeMergeReadyStateValue(state.State) != "OPEN" {
			return NewDeepReviewError("pull request is not open: %s", prURL)
		}
		if state.IsDraft {
			return NewDeepReviewError("pull request is still draft: %s", prURL)
		}
		if mergeReadyPRStateIsSuccess(state) {
			return nil
		}
		if mergeReadyPRStateIsPending(state) {
			if time.Now().After(deadline) {
				return NewDeepReviewError(
					"pull request mergeability did not settle within %s (mergeable=%s, mergeStateStatus=%s): %s",
					mergeReadyPRValidationWindow,
					displayMergeReadyStateValue(state.Mergeable),
					displayMergeReadyStateValue(state.MergeStateStatus),
					prURL,
				)
			}
			if !reportedPending {
				o.reporter.StageProgress(
					"delivery",
					fmt.Sprintf(
						"waiting for pull request mergeability to settle (mergeable=%s, mergeStateStatus=%s)",
						displayMergeReadyStateValue(state.Mergeable),
						displayMergeReadyStateValue(state.MergeStateStatus),
					),
					nil,
				)
				reportedPending = true
			}
			time.Sleep(mergeReadyPRPollInterval)
			continue
		}
		if normalizeMergeReadyStateValue(state.Mergeable) != "MERGEABLE" {
			return NewDeepReviewError("pull request is not mergeable yet (%s): %s", displayMergeReadyStateValue(state.Mergeable), prURL)
		}
		return NewDeepReviewError("pull request merge state is not clean (%s): %s", displayMergeReadyStateValue(state.MergeStateStatus), prURL)
	}
}

func (o *Orchestrator) readPullRequestMergeState(prURL string) (pullRequestMergeState, error) {
	completed, err := RunCommand(
		[]string{
			o.config.GhBin,
			"pr", "view", prURL,
			"--repo", o.repoIdentity.Slug(),
			"--json", "url,state,isDraft,mergeable,mergeStateStatus",
		},
		o.managedRepoPath,
		"",
		true,
		0,
	)
	if err != nil {
		return pullRequestMergeState{}, err
	}
	var state pullRequestMergeState
	if err := json.Unmarshal([]byte(completed.Stdout), &state); err != nil {
		return pullRequestMergeState{}, NewDeepReviewError("invalid gh pr view output for %s: %v", prURL, err)
	}
	return state, nil
}

func normalizeMergeReadyStateValue(value string) string {
	return strings.ToUpper(strings.TrimSpace(value))
}

func displayMergeReadyStateValue(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "<empty>"
	}
	return trimmed
}

func mergeReadyPRStateIsSuccess(state pullRequestMergeState) bool {
	mergeState := normalizeMergeReadyStateValue(state.MergeStateStatus)
	return normalizeMergeReadyStateValue(state.Mergeable) == "MERGEABLE" && (mergeState == "CLEAN" || mergeState == "HAS_HOOKS")
}

func mergeReadyPRStateIsPending(state pullRequestMergeState) bool {
	if normalizeMergeReadyStateValue(state.Mergeable) == "UNKNOWN" {
		return true
	}
	switch normalizeMergeReadyStateValue(state.MergeStateStatus) {
	case "UNKNOWN", "UNSTABLE":
		return true
	default:
		return false
	}
}

func (o *Orchestrator) runDeliveryStage(defaultBranch, candidateBranch string, summaries, changedFiles []string) (DeliveryResult, error) {
	candidateHead, err := RevParse(o.managedRepoPath, o.config.GitBin, candidateBranch)
	if err != nil {
		return DeliveryResult{}, err
	}
	deliveryDir := filepath.Join(o.runRoot, "delivery")
	deliveryWorktree := filepath.Join(deliveryDir, "worktree")
	if err := os.MkdirAll(deliveryDir, 0o755); err != nil {
		return DeliveryResult{}, err
	}
	if err := AddBranchWorktree(o.managedRepoPath, o.config.GitBin, deliveryWorktree, candidateBranch, candidateHead); err != nil {
		return DeliveryResult{}, err
	}
	defer func() {
		_ = RemoveWorktree(o.managedRepoPath, o.config.GitBin, deliveryWorktree)
	}()
	if err := EnsureWorktreeOperationalExcludes(deliveryWorktree, o.config.GitBin); err != nil {
		return DeliveryResult{}, err
	}

	templatePath := filepath.Join(o.promptsRoot, "delivery", "01-deliver.md")
	templateText, err := ReadTemplate(templatePath)
	if err != nil {
		return DeliveryResult{}, err
	}

	deliveryArtifactsDir := filepath.Join(deliveryWorktree, ".deepreview", "artifacts")
	if err := os.MkdirAll(deliveryArtifactsDir, 0o755); err != nil {
		return DeliveryResult{}, err
	}
	resultWorktreePath := filepath.Join(deliveryArtifactsDir, "delivery-result.json")
	resultPath := filepath.Join(deliveryDir, "delivery-result.json")

	variables := map[string]string{
		"REPO_SLUG":           o.repoIdentity.Slug(),
		"SOURCE_BRANCH":       o.config.SourceBranch,
		"DEFAULT_BRANCH":      defaultBranch,
		"CANDIDATE_BRANCH":    candidateBranch,
		"DELIVERY_BRANCH":     o.deliveryBranchName(),
		"MODE":                o.config.Mode,
		"RUN_ID":              o.config.RunID,
		"WORKTREE_PATH":       filepath.ToSlash(deliveryWorktree),
		"RUN_ROOT":            filepath.ToSlash(o.runRoot),
		"ROUND_SUMMARY_PATHS": markdownBulletList(summaries),
		"CHANGED_FILES":       markdownBulletList(changedFiles),
		"OUTPUT_RESULT_PATH":  filepath.ToSlash(resultWorktreePath),
	}
	prompt, err := RenderTemplate(templateText, variables)
	if err != nil {
		return DeliveryResult{}, err
	}

	logPrefix := filepath.Join(deliveryDir, "deliver")
	_, err = func() (CodexRunResult, error) {
		result, err := o.runPromptWithHeartbeat(
			deliveryWorktree,
			prompt,
			nil,
			true,
			logPrefix,
			[]string{resultWorktreePath},
			0,
			"delivery / merge-ready publish",
			"running codex delivery workflow",
			"delivery",
			"getting branch and PR into merge-ready state",
			func(_ int, _ int, _ *promptInactivityError) error {
				return resetMutablePromptWorktree(deliveryWorktree, o.config.GitBin, candidateHead, nil)
			},
		)
		return result, err
	}()
	if err != nil {
		return DeliveryResult{}, err
	}

	if err := ensureCanonicalArtifact(resultPath, []string{resultWorktreePath, resultPath}); err != nil {
		return DeliveryResult{}, err
	}
	result, err := readPromptDeliveryResult(resultPath, o.config.Mode)
	if err != nil {
		return DeliveryResult{}, err
	}
	if strings.TrimSpace(result.DeliveryBranch) != "" {
		return DeliveryResult{}, NewDeepReviewError(
			"delivery prompt must not select a separate publish ref; route tracked-code fixes back through execute rounds instead: %s",
			result.DeliveryBranch,
		)
	}

	if err := os.RemoveAll(filepath.Join(deliveryWorktree, ".deepreview")); err != nil {
		return DeliveryResult{}, err
	}
	if err := CleanupUntrackedOperationalArtifacts(deliveryWorktree, o.config.GitBin); err != nil {
		return DeliveryResult{}, err
	}
	changed, err := HasUncommittedChanges(deliveryWorktree, o.config.GitBin)
	if err != nil {
		return DeliveryResult{}, err
	}
	if changed {
		return DeliveryResult{}, NewDeepReviewError("delivery worktree has uncommitted changes after prompt completion: %s", deliveryWorktree)
	}
	candidateHeadAfterPrompt, err := RevParse(o.managedRepoPath, o.config.GitBin, candidateBranch)
	if err != nil {
		return DeliveryResult{}, err
	}
	if candidateHeadAfterPrompt != candidateHead {
		return DeliveryResult{}, NewDeepReviewError(
			"delivery prompt must not mutate the candidate branch; route tracked-code fixes back through execute rounds instead",
		)
	}

	baseRef := "origin/" + o.config.SourceBranch
	preparedRef, pushRefspec, commitsBranch, err := o.resolvePreparedDeliveryRef(candidateBranch)
	if err != nil {
		return DeliveryResult{}, err
	}
	// The delivery prompt can still refine local branch state, but deepreview
	// validates the exact ref to be published before any push or PR creation.
	deliveryChangedFiles, err := o.validatePublishableDeliveryRef(baseRef, candidateHead, candidateBranch, preparedRef)
	if err != nil {
		return DeliveryResult{}, err
	}
	if len(deliveryChangedFiles) == 0 {
		return DeliveryResult{
			Mode:       o.config.Mode,
			Skipped:    true,
			SkipReason: "delivery preparation removed all deliverable repository changes",
		}, nil
	}
	if o.config.Mode == ModePR {
		delivery, err := o.deliverPR(defaultBranch, preparedRef, summaries, deliveryChangedFiles, prDeliveryOptions{
			draft:            result.Incomplete,
			incomplete:       result.Incomplete,
			incompleteReason: result.IncompleteReason,
		})
		if err != nil {
			return DeliveryResult{}, err
		}
		if !result.Incomplete {
			if err := o.validateMergeReadyPR(delivery.PRURL); err != nil {
				return DeliveryResult{}, err
			}
		}
		return delivery, nil
	}

	partialDelivery := DeliveryResult{
		Mode:          result.Mode,
		PushedRefspec: pushRefspec,
		CommitsURL:    fmt.Sprintf("https://github.com/%s/commits/%s", o.repoIdentity.Slug(), escapeBranchForURL(commitsBranch)),
	}
	o.lastDelivery = &partialDelivery
	if err := PushRefspec(o.managedRepoPath, o.config.GitBin, pushRefspec); err != nil {
		return DeliveryResult{}, err
	}
	o.pushCount++
	delivery := DeliveryResult{
		Mode:          result.Mode,
		PushedRefspec: pushRefspec,
		CommitsURL:    partialDelivery.CommitsURL,
	}
	o.lastDelivery = &delivery
	return delivery, nil
}

type prDeliveryOptions struct {
	draft            bool
	incomplete       bool
	incompleteReason string
}

func (o *Orchestrator) deliverPR(defaultBranch, candidateBranch string, summaries, changedFiles []string, opts prDeliveryOptions) (DeliveryResult, error) {
	deliveryBranch, refspec, reusedPush, err := o.ensurePRDeliveryBranchPushed(candidateBranch)
	if err != nil {
		return DeliveryResult{}, err
	}
	if reusedPush {
		o.reporter.StageProgress("delivery", "creating pull request from existing delivery branch", nil)
	}
	partialDelivery := DeliveryResult{
		Mode:             ModePR,
		PushedRefspec:    refspec,
		CommitsURL:       fmt.Sprintf("https://github.com/%s/commits/%s", o.repoIdentity.Slug(), escapeBranchForURL(deliveryBranch)),
		Incomplete:       opts.incomplete,
		IncompleteReason: strings.TrimSpace(opts.incompleteReason),
	}
	o.lastDelivery = &partialDelivery

	prTitle := basePRTitleFromChanges(changedFiles)
	if opts.incomplete {
		prTitle = ensureIncompletePRTitlePrefix(prTitle)
	}
	if err := assertPublicTextSafe(prTitle, "pr title"); err != nil {
		return DeliveryResult{}, err
	}
	prBody := o.buildPRBody(defaultBranch, candidateBranch, summaries, changedFiles, opts)
	prBody = o.capPRBodyForGitHub(prBody, summaries, changedFiles, opts)
	if err := assertPublicTextSafe(prBody, "pr body"); err != nil {
		return DeliveryResult{}, err
	}
	prTitleBasePath := filepath.Join(o.runRoot, "pr-title.base.txt")
	prTitlePath := filepath.Join(o.runRoot, "pr-title.txt")
	prBodyBasePath := filepath.Join(o.runRoot, "pr-body.base.md")
	prBodyPath := filepath.Join(o.runRoot, "pr-body.md")
	if err := os.WriteFile(prTitleBasePath, []byte(prTitle+"\n"), 0o644); err != nil {
		return DeliveryResult{}, err
	}
	if err := os.WriteFile(prTitlePath, []byte(prTitle+"\n"), 0o644); err != nil {
		return DeliveryResult{}, err
	}
	if err := os.WriteFile(prBodyBasePath, []byte(prBody), 0o644); err != nil {
		return DeliveryResult{}, err
	}
	if err := os.WriteFile(prBodyPath, []byte(prBody), 0o644); err != nil {
		return DeliveryResult{}, err
	}

	createArgs := []string{
		o.config.GhBin,
		"pr", "create",
		"--repo", o.repoIdentity.Slug(),
		"--base", o.config.SourceBranch,
		"--head", deliveryBranch,
		"--title", prTitle,
		"--body-file", prBodyPath,
	}
	if opts.draft {
		createArgs = append(createArgs, "--draft")
	}
	completed, err := RunCommand(
		createArgs,
		o.managedRepoPath,
		"",
		true,
		0,
	)
	if err != nil {
		return DeliveryResult{}, err
	}
	prURL := ""
	trimmed := strings.TrimSpace(completed.Stdout)
	if trimmed != "" {
		lines := strings.Split(trimmed, "\n")
		prURL = strings.TrimSpace(lines[len(lines)-1])
	}
	if prURL == "" {
		return DeliveryResult{}, NewDeepReviewError("gh pr create did not return a pull request URL")
	}
	o.prDelivery.prURL = prURL
	delivery := DeliveryResult{
		Mode:             ModePR,
		PushedRefspec:    refspec,
		PRURL:            prURL,
		CommitsURL:       partialDelivery.CommitsURL,
		Incomplete:       opts.incomplete,
		IncompleteReason: strings.TrimSpace(opts.incompleteReason),
	}
	o.lastDelivery = &delivery
	return delivery, nil
}

func (o *Orchestrator) deliveryBranchName() string {
	return "deepreview/" + SanitizeSegment(o.config.SourceBranch) + "/" + SanitizeSegment(o.config.RunID)
}

func (o *Orchestrator) ensurePRDeliveryBranchPushed(candidateBranch string) (string, string, bool, error) {
	if o.prDelivery.pushed {
		return o.prDelivery.branch, o.prDelivery.refspec, true, nil
	}
	deliveryBranch := o.deliveryBranchName()
	refspec := candidateBranch + ":" + deliveryBranch
	o.reporter.StageProgress("delivery", "pushing delivery branch and creating pull request", nil)
	if err := PushRefspec(o.managedRepoPath, o.config.GitBin, refspec); err != nil {
		return "", "", false, err
	}
	o.pushCount++
	o.prDelivery.branch = deliveryBranch
	o.prDelivery.refspec = refspec
	o.prDelivery.pushed = true
	return deliveryBranch, refspec, false, nil
}

func (o *Orchestrator) tryPublishIncompleteDraftPR(defaultBranch, candidateBranch string, summaries []string, cause error) (bool, error) {
	if o.config.Mode != ModePR || strings.TrimSpace(candidateBranch) == "" || strings.TrimSpace(o.prDelivery.prURL) != "" {
		return false, nil
	}
	if len(summaries) == 0 {
		discovered, err := o.discoverCompletedRoundSummaries()
		if err != nil {
			return false, err
		}
		summaries = discovered
	}

	o.reporter.StageStarted("delivery", nil, "publishing incomplete draft PR to preserve work")
	baseRef := "origin/" + o.config.SourceBranch
	candidateHead, err := o.reviewedCandidateHeadForPublish(candidateBranch)
	if err != nil {
		o.reporter.StageFinished("delivery", nil, false, "incomplete draft PR unavailable: "+progressMessage(err))
		return false, err
	}
	changedFiles, err := o.validateDeliveryFiles(candidateBranch)
	if err != nil {
		o.reporter.StageFinished("delivery", nil, false, "incomplete draft PR unavailable: "+progressMessage(err))
		return false, err
	}
	if len(changedFiles) == 0 {
		o.reporter.StageFinished("delivery", nil, false, "incomplete draft PR not needed: no deliverable repository changes")
		return false, nil
	}
	changedFiles, err = o.validatePublishableDeliveryRef(baseRef, candidateHead, candidateBranch, candidateBranch)
	if err != nil {
		remediated, remediationErr := o.tryAutoRemediateLocalPathPrivacyViolation(o.managedRepoPath, candidateBranch, err)
		if remediationErr != nil {
			o.reporter.StageFinished("delivery", nil, false, "incomplete draft PR unavailable: "+progressMessage(remediationErr))
			return false, remediationErr
		}
		if !remediated {
			o.reporter.StageFinished("delivery", nil, false, "incomplete draft PR unavailable: "+progressMessage(err))
			return false, err
		}
		changedFiles, err = o.validatePublishableDeliveryRef(baseRef, candidateHead, candidateBranch, candidateBranch)
		if err != nil {
			o.reporter.StageFinished("delivery", nil, false, "incomplete draft PR unavailable: "+progressMessage(err))
			return false, err
		}
	}
	if len(changedFiles) == 0 {
		o.reporter.StageFinished("delivery", nil, false, "incomplete draft PR not needed: no deliverable repository changes")
		return false, nil
	}

	reason := trimForDisplay(strings.TrimSpace(strings.ReplaceAll(cause.Error(), "\n", " ")), 500)
	delivery, err := o.deliverPR(defaultBranch, candidateBranch, summaries, changedFiles, prDeliveryOptions{
		draft:            true,
		incomplete:       true,
		incompleteReason: reason,
	})
	if err != nil {
		o.reporter.StageFinished("delivery", nil, false, "incomplete draft PR delivery failed: "+progressMessage(err))
		return false, err
	}
	o.lastDelivery = &delivery
	if err := o.writeFinalSummary(defaultBranch, candidateBranch, delivery, summaries); err != nil {
		o.reporter.StageFinished("delivery", nil, false, "incomplete draft PR final summary failed: "+progressMessage(err))
		return false, err
	}
	o.reporter.StageFinished("delivery", nil, true, "incomplete draft PR created to preserve work")
	return true, nil
}

func (o *Orchestrator) capPRBodyForGitHub(body string, summaries, changedFiles []string, opts prDeliveryOptions) string {
	if withinPRBodyTarget(body) {
		return body
	}
	compact := o.buildCompactPRBody(summaries, changedFiles, opts)
	if withinPRBodyTarget(compact) {
		return compact
	}
	return trimForDisplay(compact, githubPRBodyTargetChars)
}

func withinPRBodyTarget(body string) bool {
	return utf8.RuneCountInString(body) <= githubPRBodyTargetChars
}

func (o *Orchestrator) buildCompactPRBody(summaries, changedFiles []string, opts prDeliveryOptions) string {
	areaSummary := summarizeChangedAreas(changedFiles, 6)
	filePreview, omitted := summarizeChangedFilePreview(changedFiles, 12)

	lines := []string{
		"## summary",
		"- deepreview generated this compact body to stay within GitHub PR body limits.",
		fmt.Sprintf("- run id: `%s`", o.config.RunID),
		fmt.Sprintf("- rounds executed: `%d`", len(summaries)),
	}
	if opts.incomplete {
		lines = append(lines,
			"- status: `[INCOMPLETE]` draft PR created because the run made repository changes but did not finish cleanly.",
			fmt.Sprintf("- blocking reason: %s", sanitizePublicText(trimForDisplay(strings.TrimSpace(opts.incompleteReason), 240))),
		)
	}
	lines = append(lines, "", "## what changed and why")
	lines = append(lines,
		fmt.Sprintf("- changed files: `%d`", len(changedFiles)),
		fmt.Sprintf("- main change areas: %s", sanitizePublicText(areaSummary)),
	)
	previewLine := "- key changed files: " + sanitizePublicText(filePreview)
	if omitted > 0 {
		previewLine += fmt.Sprintf(" (+%d more)", omitted)
	}
	lines = append(lines, previewLine)

	lines = append(lines, "", "## round outcomes")
	lines = append(lines, roundDecisionLines(summaries)...)

	lines = append(lines,
		"",
		"## verification",
		"- deepreview ran execute-stage verification before delivery.",
	)

	lines = append(lines,
		"",
		"## risks and follow-ups",
		"- deepreview performs a separate post-create mergeability check before reporting successful delivery.",
	)

	lines = append(lines,
		"",
		"## final status",
		"- published by deepreview; check the terminal run summary for final delivery status.",
	)

	return sanitizePublicText(strings.Join(lines, "\n") + "\n")
}

func (o *Orchestrator) buildIncompletePRBody(summaries, changedFiles []string, blockingReason string) string {
	areaSummary := summarizeChangedAreas(changedFiles, 6)
	filePreview, omitted := summarizeChangedFilePreview(changedFiles, 12)

	lines := []string{
		"## summary",
		"- `[INCOMPLETE]` draft PR created to preserve deepreview work that made real repository changes.",
		fmt.Sprintf("- run id: `%s`", o.config.RunID),
		fmt.Sprintf("- rounds completed: `%d`", len(summaries)),
		"",
		"## what changed and why",
		fmt.Sprintf("- changed files: `%d`", len(changedFiles)),
		fmt.Sprintf("- main change areas: %s", sanitizePublicText(areaSummary)),
	}
	previewLine := "- key changed files: " + sanitizePublicText(filePreview)
	if omitted > 0 {
		previewLine += fmt.Sprintf(" (+%d more)", omitted)
	}
	lines = append(lines, previewLine)
	lines = append(lines,
		"",
		"## incomplete status",
		"- do not merge this PR as-is; deepreview ended before full completion.",
		fmt.Sprintf("- blocking reason: %s", sanitizePublicText(trimForDisplay(strings.TrimSpace(blockingReason), 500))),
	)
	if latestRound, latestStatus, ok := latestRoundStatus(summaries); ok {
		lines = append(lines,
			fmt.Sprintf("- latest round: `%s`", latestRound),
			fmt.Sprintf("- latest decision: `%s`", latestStatus.Decision),
			fmt.Sprintf("- latest reason: %s", sanitizePublicText(trimForDisplay(strings.TrimSpace(strings.ReplaceAll(latestStatus.Reason, "\n", " ")), 320))),
		)
		if latestStatus.NextFocus != nil && strings.TrimSpace(*latestStatus.NextFocus) != "" {
			lines = append(lines, fmt.Sprintf("- next focus: %s", sanitizePublicText(trimForDisplay(strings.TrimSpace(*latestStatus.NextFocus), 280))))
		}
	}
	lines = append(lines, "", "## round outcomes")
	lines = append(lines, roundDecisionLines(summaries)...)
	lines = append(lines,
		"",
		"## verification",
		"- Verification evidence was captured during the completed rounds and is reflected in the associated round summaries and verification artifacts.",
		"",
		"## risks and follow-ups",
		"- Remaining blocking work is still required before this branch is ready for normal delivery.",
		"",
		"## final status",
		"- deepreview preserved the current candidate branch as a draft PR because the run ended incomplete after tangible repository changes.",
	)

	return sanitizePublicText(strings.Join(lines, "\n") + "\n")
}

func (o *Orchestrator) buildPRBody(_ string, _ string, summaries, changedFiles []string, opts prDeliveryOptions) string {
	if opts.incomplete {
		return o.buildIncompletePRBody(summaries, changedFiles, opts.incompleteReason)
	}
	return o.buildCompactPRBody(summaries, changedFiles, opts)
}

func basePRTitleFromChanges(changedFiles []string) string {
	if len(changedFiles) == 0 {
		return "deepreview: review updates"
	}
	type pair struct {
		area  string
		count int
	}
	counts := map[string]int{}
	seenFiles := map[string]struct{}{}
	for _, raw := range changedFiles {
		path := strings.TrimSpace(raw)
		if path == "" {
			continue
		}
		if _, seen := seenFiles[path]; seen {
			continue
		}
		seenFiles[path] = struct{}{}
		area := "repo root"
		if idx := strings.Index(path, "/"); idx > 0 {
			area = path[:idx]
		}
		counts[area]++
	}
	if len(counts) == 0 {
		return "deepreview: review updates"
	}
	areas := make([]pair, 0, len(counts))
	for area, count := range counts {
		areas = append(areas, pair{area: area, count: count})
	}
	sort.Slice(areas, func(i, j int) bool {
		if areas[i].count == areas[j].count {
			return areas[i].area < areas[j].area
		}
		return areas[i].count > areas[j].count
	})
	top := sanitizePublicText(strings.TrimSpace(areas[0].area))
	totalFiles := len(seenFiles)
	switch top {
	case "", "repo root":
		return fmt.Sprintf("deepreview: review updates (%d files)", totalFiles)
	default:
		return fmt.Sprintf("deepreview: %s updates (%d files)", top, totalFiles)
	}
}

func normalizePRTitle(rawTitle, fallback string) string {
	title := strings.TrimSpace(strings.ReplaceAll(rawTitle, "\n", " "))
	title = strings.Join(strings.Fields(title), " ")
	title = sanitizePublicText(title)
	if title == "" {
		title = strings.TrimSpace(sanitizePublicText(fallback))
	}
	title = ensurePRTitlePrefix(title)
	return trimForDisplay(title, githubPRTitleTargetChars)
}

func ensureIncompletePRTitlePrefix(title string) string {
	normalized := normalizePRTitle(title, "deepreview: review updates")
	if strings.HasPrefix(normalized, "[INCOMPLETE] ") {
		return normalized
	}
	return trimForDisplay("[INCOMPLETE] "+normalized, githubPRTitleTargetChars)
}

func ensurePRTitlePrefix(title string) string {
	trimmed := strings.TrimSpace(title)
	if trimmed == "" {
		return "deepreview: review updates"
	}
	for {
		lower := strings.ToLower(trimmed)
		switch {
		case strings.HasPrefix(lower, "deepreview:"):
			return trimmed
		case strings.HasPrefix(lower, "deepreview "):
			trimmed = strings.TrimSpace(trimmed[len("deepreview "):])
		case strings.HasPrefix(lower, "deepreview-"):
			trimmed = strings.TrimSpace(trimmed[len("deepreview-"):])
		default:
			goto done
		}
	}
done:
	trimmed = strings.TrimLeft(trimmed, " :-")
	trimmed = strings.TrimSpace(trimmed)
	if trimmed == "" {
		return "deepreview: review updates"
	}
	return "deepreview: " + trimmed
}

func roundDecisionLines(summaries []string) []string {
	if len(summaries) == 0 {
		return []string{"- no round artifacts available"}
	}
	lines := make([]string, 0, len(summaries))
	for _, path := range summaries {
		roundLabel := filepath.Base(filepath.Dir(path))
		statusPath := filepath.Join(filepath.Dir(path), "round-status.json")
		status, err := readRoundStatus(statusPath)
		if err != nil {
			lines = append(lines, fmt.Sprintf("- %s: status unavailable", roundLabel))
			continue
		}
		reason := trimForDisplay(strings.TrimSpace(strings.ReplaceAll(status.Reason, "\n", " ")), 220)
		lines = append(lines, fmt.Sprintf("- %s: decision=`%s`, reason=%s", roundLabel, status.Decision, sanitizePublicText(reason)))
	}
	return lines
}

func latestRoundStatus(summaries []string) (string, RoundStatus, bool) {
	if len(summaries) == 0 {
		return "", RoundStatus{}, false
	}
	summaryPath := summaries[len(summaries)-1]
	statusPath := filepath.Join(filepath.Dir(summaryPath), "round-status.json")
	status, err := readRoundStatus(statusPath)
	if err != nil {
		return "", RoundStatus{}, false
	}
	return filepath.Base(filepath.Dir(summaryPath)), status, true
}

func summarizeChangedAreas(changedFiles []string, limit int) string {
	if len(changedFiles) == 0 {
		return "none"
	}
	counts := map[string]int{}
	for _, path := range changedFiles {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		area := "repo root"
		if idx := strings.Index(path, "/"); idx > 0 {
			area = path[:idx]
		}
		counts[area]++
	}
	type pair struct {
		area  string
		count int
	}
	pairs := make([]pair, 0, len(counts))
	for area, count := range counts {
		pairs = append(pairs, pair{area: area, count: count})
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].count == pairs[j].count {
			return pairs[i].area < pairs[j].area
		}
		return pairs[i].count > pairs[j].count
	})
	if limit <= 0 || limit > len(pairs) {
		limit = len(pairs)
	}
	parts := make([]string, 0, limit)
	for _, p := range pairs[:limit] {
		parts = append(parts, fmt.Sprintf("`%s` (%d)", p.area, p.count))
	}
	if limit < len(pairs) {
		parts = append(parts, fmt.Sprintf("+%d more area(s)", len(pairs)-limit))
	}
	return strings.Join(parts, ", ")
}

func summarizeChangedFilePreview(changedFiles []string, limit int) (string, int) {
	if len(changedFiles) == 0 {
		return "none", 0
	}
	unique := make([]string, 0, len(changedFiles))
	seen := map[string]struct{}{}
	for _, path := range changedFiles {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		unique = append(unique, path)
	}
	sort.Strings(unique)
	if limit <= 0 || limit > len(unique) {
		limit = len(unique)
	}
	preview := unique[:limit]
	parts := make([]string, 0, len(preview))
	for _, p := range preview {
		parts = append(parts, fmt.Sprintf("`%s`", p))
	}
	return strings.Join(parts, ", "), len(unique) - len(preview)
}

func (o *Orchestrator) writeFinalSummary(_ string, _ string, delivery DeliveryResult, summaries []string) error {
	if delivery.Skipped {
		if o.pushCount != 0 {
			return NewDeepReviewError("invalid delivery push count: expected 0 for skipped delivery, got %d", o.pushCount)
		}
	} else if strings.TrimSpace(delivery.PushedRefspec) == "" {
		return NewDeepReviewError("invalid delivery state: non-skipped delivery is missing pushed refspec")
	}

	lines := []string{
		"# deepreview final summary",
		"",
		fmt.Sprintf("- run id: `%s`", o.config.RunID),
		"- repo/branch metadata omitted for privacy",
		fmt.Sprintf("- mode: `%s`", delivery.Mode),
		fmt.Sprintf("- rounds: `%d`", len(summaries)),
		fmt.Sprintf("- run artifacts: `%s`", filepath.ToSlash(filepath.Join("runs", o.config.RunID))),
	}
	if delivery.Skipped {
		lines = append(lines,
			"- delivery: `skipped`",
			fmt.Sprintf("- reason: `%s`", delivery.SkipReason),
		)
	} else if delivery.Incomplete {
		lines = append(lines,
			"- delivery: `incomplete-draft`",
			fmt.Sprintf("- reason: `%s`", sanitizePublicText(strings.TrimSpace(delivery.IncompleteReason))),
		)
	}
	if !delivery.Skipped {
		lines = append(lines, fmt.Sprintf("- push refspec: `%s`", delivery.PushedRefspec))
	}
	lines = append(lines, "", "## round artifacts")
	for _, path := range summaries {
		rel := path
		if relative, err := filepath.Rel(o.runRoot, path); err == nil {
			rel = filepath.ToSlash(relative)
		}
		lines = append(lines, fmt.Sprintf("- `%s`", rel))
	}
	lines = append(lines, "", "## round decisions")
	for _, path := range summaries {
		statusPath := filepath.Join(filepath.Dir(path), "round-status.json")
		status, err := readRoundStatus(statusPath)
		if err != nil {
			rel := filepath.ToSlash(filepath.Join(filepath.Base(filepath.Dir(path)), "round-status.json"))
			lines = append(lines, fmt.Sprintf("- `%s`: unable to parse round status (`%v`)", rel, err))
			continue
		}
		confidence := "n/a"
		if status.Confidence != nil {
			confidence = fmt.Sprintf("%.2f", *status.Confidence)
		}
		reason := strings.TrimSpace(strings.ReplaceAll(status.Reason, "\n", " "))
		rel := filepath.ToSlash(filepath.Join(filepath.Base(filepath.Dir(path)), "round-status.json"))
		lines = append(lines, fmt.Sprintf("- `%s`: decision=`%s`, confidence=`%s`, reason=%s", rel, status.Decision, confidence, sanitizePublicText(reason)))
	}
	if delivery.PRURL != "" {
		lines = append(lines, "", "## pull request", "- "+delivery.PRURL)
	}
	if delivery.CommitsURL != "" {
		lines = append(lines, "", "## commits", "- "+delivery.CommitsURL)
	}

	finalText := sanitizePublicText(strings.Join(lines, "\n") + "\n")
	if err := assertPublicTextSafe(finalText, "final summary"); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(o.runRoot, "final-summary.md"), []byte(finalText), 0o644)
}

func (o *Orchestrator) discoverCompletedRoundSummaries() ([]string, error) {
	recordPaths, err := filepath.Glob(filepath.Join(o.runRoot, "round-*", "round.json"))
	if err != nil {
		return nil, err
	}
	sort.Strings(recordPaths)

	summaries := make([]string, 0, len(recordPaths))
	for _, recordPath := range recordPaths {
		record, err := readRoundRecord(recordPath)
		if err != nil {
			continue
		}
		summaryPath := filepath.Join(filepath.Dir(recordPath), record.Summary)
		if _, err := os.Stat(summaryPath); err != nil {
			continue
		}
		summaries = append(summaries, summaryPath)
	}
	return summaries, nil
}

func detailsBlock(title, language, content string) string {
	title = strings.TrimSpace(title)
	if title == "" {
		title = "artifact"
	}
	if language == "" {
		language = "text"
	}
	body := strings.TrimSpace(content)
	return fmt.Sprintf("<details>\n<summary>%s</summary>\n\n```%s\n%s\n```\n</details>", title, language, body)
}

func escapeBranchForURL(branch string) string {
	escaped := url.PathEscape(branch)
	return strings.ReplaceAll(escaped, "%2F", "/")
}

func sanitizePublicText(text string) string {
	sanitized := text
	for _, pattern := range allSecretRiskyPatterns() {
		sanitized = pattern.ReplaceAllString(sanitized, "[redacted-secret]")
	}
	for _, pattern := range privatePathPatterns {
		sanitized = pattern.ReplaceAllString(sanitized, "[redacted-path]")
	}
	for _, pattern := range personalRiskyPatterns {
		sanitized = pattern.ReplaceAllString(sanitized, "[redacted-personal]")
	}
	sanitized = emailPattern.ReplaceAllStringFunc(sanitized, func(match string) string {
		if isAllowedPlaceholderEmail(match) {
			return match
		}
		return "[redacted-email]"
	})
	return sanitized
}

func isAllowedPlaceholderEmail(email string) bool {
	parts := strings.Split(strings.ToLower(strings.TrimSpace(email)), "@")
	if len(parts) != 2 {
		return false
	}
	domain := strings.TrimSpace(parts[1])
	if domain == "" {
		return false
	}
	if _, ok := allowedPlaceholderEmailDomains[domain]; ok {
		return true
	}
	return strings.HasSuffix(domain, ".example.com") || strings.HasSuffix(domain, ".example.org") || strings.HasSuffix(domain, ".example.net")
}

func containsDisallowedEmail(text string) bool {
	matches := emailPattern.FindAllString(text, -1)
	for _, match := range matches {
		if !isAllowedPlaceholderEmail(match) {
			return true
		}
	}
	return false
}

func textHasDisallowedSensitivePattern(text string) bool {
	if containsDisallowedEmail(text) {
		return true
	}
	for _, pattern := range allSecretRiskyPatterns() {
		if pattern.MatchString(text) {
			return true
		}
	}
	for _, pattern := range privatePathPatterns {
		if pattern.MatchString(text) {
			return true
		}
	}
	for _, pattern := range personalRiskyPatterns {
		if pattern.MatchString(text) {
			return true
		}
	}
	return false
}

func assertPublicTextSafe(text, surface string) error {
	if textHasDisallowedSensitivePattern(text) {
		return NewDeepReviewError("privacy guard blocked %s: disallowed sensitive content remained after sanitization", surface)
	}
	return nil
}

func shortSHA(sha string) string {
	if len(sha) > 12 {
		return sha[:12]
	}
	return sha
}

func progressMessage(err error) string {
	if err == nil {
		return ""
	}

	var commandErr *CommandExecutionError
	if errors.As(err, &commandErr) {
		if commandErr.Canceled {
			return "canceled by user interrupt"
		}
		message := commandErr.Message
		snippet := firstNonEmptyLine(commandErr.Stderr)
		if snippet == "" {
			snippet = firstNonEmptyLine(commandErr.Stdout)
		}
		snippet = strings.TrimSpace(snippet)
		if snippet != "" {
			message += " | " + trimForDisplay(snippet, 180)
		}
		return trimForDisplay(message, 220)
	}

	return trimForDisplay(err.Error(), 220)
}

func firstNonEmptyLine(text string) string {
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		if line != "" {
			return line
		}
	}
	return ""
}

func trimForDisplay(text string, maxRunes int) string {
	if maxRunes <= 0 {
		return ""
	}
	runes := []rune(text)
	if len(runes) <= maxRunes {
		return text
	}
	if maxRunes <= 3 {
		return string(runes[:maxRunes])
	}
	return string(runes[:maxRunes-3]) + "..."
}

func (o *Orchestrator) writeRunConfig() error {
	payload := map[string]any{
		"repo":          sanitizePublicText(o.config.Repo),
		"source_branch": sanitizePublicText(o.config.SourceBranch),
		"concurrency":   o.config.Concurrency,
		"max_rounds":    o.config.MaxRounds,
		"review_inactivity_seconds": func() int {
			if o.config.ReviewInactivitySec > 0 {
				return o.config.ReviewInactivitySec
			}
			if o.config.ReviewInactivity > 0 {
				return int(o.config.ReviewInactivity / time.Second)
			}
			return 0
		}(),
		"review_activity_poll_seconds": func() int {
			if o.config.ReviewActivityPollS > 0 {
				return o.config.ReviewActivityPollS
			}
			if o.config.ReviewActivityPoll > 0 {
				return int(o.config.ReviewActivityPoll / time.Second)
			}
			return 0
		}(),
		"review_max_restarts":   o.config.ReviewMaxRestarts,
		"mode":                  o.config.Mode,
		"workspace_root":        sanitizePublicText(o.workspaceRoot),
		"run_id":                o.config.RunID,
		"git_bin":               sanitizePublicText(o.config.GitBin),
		"codex_bin":             sanitizePublicText(o.config.CodexBin),
		"gh_bin":                sanitizePublicText(o.config.GhBin),
		"codex_timeout_seconds": o.config.CodexTimeoutSeconds,
	}
	b, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(o.runRoot, "run-config.json"), append(b, '\n'), 0o644)
}
