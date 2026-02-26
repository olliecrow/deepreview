package deepreview

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
)

type Orchestrator struct {
	config          ReviewConfig
	toolRoot        string
	promptsRoot     string
	workspaceRoot   string
	runRoot         string
	repoIdentity    RepoIdentity
	managedRepoPath string
	codexRunner     CodexRunner
	reporter        ProgressReporter
	pushCount       int
	lastDelivery    *DeliveryResult
	repoLockPath    string
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

	if reporter == nil {
		reporter = &NullProgressReporter{}
	}

	runRoot := filepath.Join(workspaceRoot, "runs", config.RunID)
	managedRepoPath := filepath.Join(workspaceRoot, "repos", repoIdentity.Owner, repoIdentity.Name)
	if config.CodexTimeout <= 0 {
		config.CodexTimeout = time.Duration(config.CodexTimeoutSeconds) * time.Second
	}
	// Enforce globally pinned Codex settings regardless of caller-supplied config.
	config.CodexModel = forcedCodexModel
	config.CodexReasoning = forcedCodexReasoningEffort

	return &Orchestrator{
		config:          config,
		toolRoot:        toolRoot,
		promptsRoot:     promptsRoot,
		workspaceRoot:   workspaceRoot,
		runRoot:         runRoot,
		repoIdentity:    repoIdentity,
		managedRepoPath: managedRepoPath,
		codexRunner: CodexRunner{
			CodexBin:   config.CodexBin,
			CodexModel: config.CodexModel,
			Reasoning:  config.CodexReasoning,
			Timeout:    config.CodexTimeout,
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

	candidates := []string{}
	if cwd, err := os.Getwd(); err == nil {
		candidates = append(candidates, filepath.Join(cwd, "prompts"))
	}
	if exe, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exe)
		candidates = append(candidates,
			filepath.Join(exeDir, "prompts"),
			filepath.Join(exeDir, "..", "prompts"),
			filepath.Join(exeDir, "..", "..", "prompts"),
		)
	}
	_, thisFile, _, ok := runtime.Caller(0)
	if ok {
		candidate := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", "..", "prompts"))
		candidates = append(candidates, candidate)
	}

	for _, candidate := range candidates {
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
			owner := "local"
			name := SanitizeSegment(filepath.Base(repoPath))
			remote, err := tryReadRemoteURL(config.GitBin, repoPath)
			if err != nil {
				return RepoIdentity{}, err
			}
			if strings.TrimSpace(remote) == "" {
				return RepoIdentity{}, NewDeepReviewError("local repo input must have remote.origin.url configured: %s", repoPath)
			}
			if o, n, ok := parseOwnerRepo(remote); ok {
				owner, name = o, n
			}
			return RepoIdentity{Owner: owner, Name: name, CloneSource: remote}, nil
		}
	}

	if owner, name, ok := parseOwnerRepo(repo); ok {
		source := repo
		if !(strings.HasPrefix(repo, "http://") || strings.HasPrefix(repo, "https://") || strings.HasPrefix(repo, "git@")) {
			source = fmt.Sprintf("https://github.com/%s/%s.git", owner, name)
		}
		return RepoIdentity{Owner: owner, Name: name, CloneSource: source}, nil
	}

	return RepoIdentity{}, NewDeepReviewError("unable to resolve repo locator: %s", repo)
}

var ownerRepoRemoteRe = regexp.MustCompile(`github\.com[:/]([A-Za-z0-9_.-]+)/([A-Za-z0-9_.-]+?)(?:\.git)?$`)
var secretRiskyPatterns = []*regexp.Regexp{
	regexp.MustCompile(`AKIA[0-9A-Z]{16}`),
	regexp.MustCompile(`ghp_[A-Za-z0-9]{36,}`),
	regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----`),
	regexp.MustCompile(`xox[baprs]-[A-Za-z0-9-]{10,}`),
}
var emailPattern = regexp.MustCompile(`(?i)\b[A-Z0-9._%+\-]+@(?:[A-Z0-9\-]+\.)+[A-Z]{2,}\b`)
var personalRiskyPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?m)\b\d{3}-\d{2}-\d{4}\b`),
	regexp.MustCompile(`(?m)\b(?:\+?1[-.\s]?)?(?:\(\d{3}\)|\d{3})[-.\s]\d{3}[-.\s]\d{4}\b`),
}
var privatePathPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?m)/Users/\S+`),
	regexp.MustCompile(`(?m)/home/\S+`),
	regexp.MustCompile(`(?m)[A-Za-z]:\\\S+`),
}
var allowedPlaceholderEmailDomains = map[string]struct{}{
	"example.com": {},
	"example.org": {},
	"example.net": {},
	"test.com":    {},
	"localhost":   {},
	"local":       {},
	"invalid":     {},
}

func parseOwnerRepo(text string) (string, string, bool) {
	if m := ownerRepoRemoteRe.FindStringSubmatch(text); m != nil {
		return SanitizeSegment(m[1]), SanitizeSegment(m[2]), true
	}
	if strings.Count(text, "/") == 1 && !strings.HasPrefix(text, "/") && !strings.Contains(text, "://") && !strings.HasPrefix(text, "git@") {
		parts := strings.SplitN(text, "/", 2)
		return SanitizeSegment(parts[0]), SanitizeSegment(parts[1]), true
	}
	return "", "", false
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

func (o *Orchestrator) Run() error {
	if err := o.preflight(); err != nil {
		return err
	}
	if err := o.acquireRepoRunLock(); err != nil {
		return err
	}
	defer o.releaseRepoRunLock()
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

	var finalErr error
	defer func() {
		if finalErr != nil {
			o.reporter.RunFinished(false, finalErr.Error())
		}
	}()

	prepareStage := "prepare"
	o.reporter.StageStarted(prepareStage, nil, "syncing managed repository copy")
	if err := CloneOrFetch(o.managedRepoPath, o.repoIdentity.CloneSource, o.config.GitBin); err != nil {
		o.reporter.StageFinished(prepareStage, nil, false, progressMessage(err))
		finalErr = err
		return err
	}
	defaultBranch, err := ResolveDefaultBranch(o.managedRepoPath, o.config.GitBin)
	if err != nil {
		o.reporter.StageFinished(prepareStage, nil, false, progressMessage(err))
		finalErr = err
		return err
	}
	sourceSHA, err := RequireRemoteBranch(o.managedRepoPath, o.config.GitBin, o.config.SourceBranch)
	if err != nil {
		o.reporter.StageFinished(prepareStage, nil, false, progressMessage(err))
		finalErr = err
		return err
	}

	candidateBranch := o.candidateBranchName(o.config.SourceBranch, o.config.RunID)
	if err := SetBranchToRef(o.managedRepoPath, o.config.GitBin, candidateBranch, sourceSHA); err != nil {
		o.reporter.StageFinished(prepareStage, nil, false, progressMessage(err))
		finalErr = err
		return err
	}
	if o.config.Mode == ModeYolo && o.config.SourceBranch == defaultBranch {
		if err := o.verifyYoloDefaultBranchPushAllowed(candidateBranch, defaultBranch); err != nil {
			o.reporter.StageFinished(prepareStage, nil, false, progressMessage(err))
			finalErr = err
			return err
		}
	}
	o.reporter.StageFinished(
		prepareStage,
		nil,
		true,
		fmt.Sprintf("managed repo ready: default branch `%s`, source head `%s`", defaultBranch, shortSHA(sourceSHA)),
	)

	roundSummaries := make([]string, 0)

	for round := 1; round <= o.config.MaxRounds; round++ {
		roundDir := filepath.Join(o.runRoot, fmt.Sprintf("round-%02d", round))
		if err := os.MkdirAll(roundDir, 0o755); err != nil {
			finalErr = err
			return err
		}

		candidateHeadBefore, err := RevParse(o.managedRepoPath, o.config.GitBin, candidateBranch)
		if err != nil {
			finalErr = err
			return err
		}

		reviewReports, err := o.runReviewStage(round, roundDir, candidateHeadBefore, defaultBranch)
		if err != nil {
			finalErr = err
			return err
		}

		_, summaryPath, err := o.runExecuteStage(round, roundDir, candidateBranch, candidateHeadBefore, defaultBranch, reviewReports)
		if err != nil {
			finalErr = err
			return err
		}
		roundSummaries = append(roundSummaries, summaryPath)

		candidateHeadAfter, err := RevParse(o.managedRepoPath, o.config.GitBin, candidateBranch)
		if err != nil {
			finalErr = err
			return err
		}
		roundChangedFiles, err := ListChangedFiles(o.managedRepoPath, o.config.GitBin, candidateHeadBefore, candidateHeadAfter)
		if err != nil {
			finalErr = err
			return err
		}
		changed := len(roundChangedFiles) > 0

		if changed {
			o.reporter.StageProgress(
				"execute stage",
				fmt.Sprintf("round produced %d repository change(s); scheduling next review round", len(roundChangedFiles)),
				roundPtr(round),
			)
			if round >= o.config.MaxRounds {
				err := NewDeepReviewError(
					"max rounds reached after execute changes in round %d; deepreview requires at least one additional review round after code changes (increase --max-rounds)",
					round,
				)
				finalErr = err
				return err
			}
			continue
		}
		o.reporter.StageProgress("execute stage", "round produced no repository changes; stopping additional rounds", roundPtr(round))
		break
	}

	if len(roundSummaries) == 0 {
		err := NewDeepReviewError("internal run state invalid: no review/execute rounds were completed")
		finalErr = err
		return err
	}

	deliveryStage := "delivery"
	o.reporter.StageStarted(deliveryStage, nil, "validating delivery and publishing results")
	changedFiles, err := o.validateDeliveryFiles(candidateBranch)
	if err != nil {
		o.reporter.StageFinished(deliveryStage, nil, false, progressMessage(err))
		finalErr = err
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
			finalErr = err
			return err
		}
		o.reporter.StageFinished(deliveryStage, nil, true, delivery.SkipReason)
		o.reporter.RunFinished(true, "run completed successfully (no deliverable repository changes)")
		return nil
	}
	if err := o.deliveryCommitMessageScan(candidateBranch); err != nil {
		o.reporter.StageFinished(deliveryStage, nil, false, progressMessage(err))
		finalErr = err
		return err
	}
	if err := o.secretHygieneScan(candidateBranch); err != nil {
		o.reporter.StageFinished(deliveryStage, nil, false, progressMessage(err))
		finalErr = err
		return err
	}

	delivery, err := o.deliver(defaultBranch, candidateBranch, roundSummaries, changedFiles)
	if err != nil {
		o.reporter.StageFinished(deliveryStage, nil, false, progressMessage(err))
		finalErr = err
		return err
	}
	o.lastDelivery = &delivery
	if err := o.writeFinalSummary(defaultBranch, candidateBranch, delivery, roundSummaries); err != nil {
		o.reporter.StageFinished(deliveryStage, nil, false, progressMessage(err))
		finalErr = err
		return err
	}
	o.reporter.StageFinished(deliveryStage, nil, true, fmt.Sprintf("delivery completed in `%s` mode", delivery.Mode))
	o.reporter.RunFinished(true, "run completed successfully")
	return nil
}

func (o *Orchestrator) preflight() error {
	requiredBins := []string{o.config.GitBin, o.config.CodexBin}
	if o.config.Mode == ModePR {
		requiredBins = append(requiredBins, o.config.GhBin)
	}
	for _, tool := range requiredBins {
		ok, err := which(tool)
		if err != nil {
			return err
		}
		if !ok {
			return NewDeepReviewError("required tool not found in PATH: %s", tool)
		}
	}
	queuePath := filepath.Join(o.promptsRoot, "execute", "queue.txt")
	queue, err := ReadQueue(queuePath)
	if err != nil {
		return err
	}
	for _, templateName := range queue {
		templatePath := filepath.Join(o.promptsRoot, "execute", templateName)
		if _, err := os.Stat(templatePath); err != nil {
			return NewDeepReviewError("execute template file not found: %s", templatePath)
		}
	}
	if _, err := os.Stat(filepath.Join(o.promptsRoot, "review", "independent-review.md")); err != nil {
		return NewDeepReviewError("independent review prompt template missing")
	}
	if o.config.Mode == ModePR {
		deliveryTemplate := filepath.Join(o.promptsRoot, "delivery", "pr-description-summary.md")
		if _, err := os.Stat(deliveryTemplate); err != nil {
			return NewDeepReviewError("delivery prompt template missing: %s", deliveryTemplate)
		}
	}
	return nil
}

type repoRunLockRecord struct {
	RunID     string `json:"run_id"`
	PID       int    `json:"pid"`
	Repo      string `json:"repo"`
	CreatedAt string `json:"created_at"`
}

func (o *Orchestrator) repoLockFilePath() string {
	return filepath.Join(o.workspaceRoot, "locks", o.repoIdentity.Owner, o.repoIdentity.Name+".lock")
}

func (o *Orchestrator) acquireRepoRunLock() error {
	lockPath := o.repoLockFilePath()
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
		return err
	}
	record := repoRunLockRecord{
		RunID:     o.config.RunID,
		PID:       os.Getpid(),
		Repo:      o.repoIdentity.Slug(),
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
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
		o.repoLockPath = lockPath
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
			o.repoLockPath = lockPath
			return nil
		} else if !os.IsExist(err) {
			return err
		}
	}

	return NewDeepReviewError("another deepreview run is active for repo `%s`; wait for it to finish before starting another run", o.repoIdentity.Slug())
}

func (o *Orchestrator) releaseRepoRunLock() {
	if strings.TrimSpace(o.repoLockPath) == "" {
		return
	}
	if err := os.Remove(o.repoLockPath); err != nil && !os.IsNotExist(err) {
		// best-effort cleanup
	}
	o.repoLockPath = ""
}

func lockLooksStale(lockPath string, payload []byte) bool {
	var record repoRunLockRecord
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
	if runtime.GOOS == "windows" {
		return true
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
	completed, err := RunCommand([]string{"/usr/bin/env", "sh", "-lc", "command -v " + command}, "", "", false, 0)
	if err != nil {
		return false, err
	}
	return completed.ReturnCode == 0 && strings.TrimSpace(completed.Stdout) != "", nil
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

	worktrees := make([]string, 0, o.config.Concurrency)
	reviewPaths := make([]string, 0, o.config.Concurrency)
	workerReviewPaths := make([]string, 0, o.config.Concurrency)
	defer func() {
		for _, worktree := range worktrees {
			_ = RemoveWorktree(o.managedRepoPath, o.config.GitBin, worktree)
		}
	}()

	for workerID := 1; workerID <= o.config.Concurrency; workerID++ {
		workerDir := filepath.Join(reviewDir, fmt.Sprintf("worker-%02d", workerID))
		worktreePath := filepath.Join(workerDir, "worktree")
		reviewPath := filepath.Join(roundDir, fmt.Sprintf("review-%02d.md", workerID))
		workerReviewPath := filepath.Join(worktreePath, ".deepreview", fmt.Sprintf("review-%02d.md", workerID))
		if err := os.MkdirAll(workerDir, 0o755); err != nil {
			return nil, err
		}
		if err := AddDetachedWorktree(o.managedRepoPath, o.config.GitBin, worktreePath, candidateHead); err != nil {
			return nil, err
		}
		if err := os.MkdirAll(filepath.Dir(workerReviewPath), 0o755); err != nil {
			return nil, err
		}
		worktrees = append(worktrees, worktreePath)
		reviewPaths = append(reviewPaths, reviewPath)
		workerReviewPaths = append(workerReviewPaths, workerReviewPath)
	}

	var wg sync.WaitGroup
	errCh := make(chan error, o.config.Concurrency)
	doneCh := make(chan struct{}, o.config.Concurrency)

	for idx := range reviewPaths {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()

			workerID := i + 1
			worktreePath := worktrees[i]
			workerReviewRelPath := filepath.ToSlash(filepath.Join(".deepreview", fmt.Sprintf("review-%02d.md", workerID)))
			variables := map[string]string{
				"REPO_SLUG":          o.repoIdentity.Slug(),
				"SOURCE_BRANCH":      o.config.SourceBranch,
				"DEFAULT_BRANCH":     defaultBranch,
				"WORKER_ID":          fmt.Sprintf("%d", workerID),
				"CONCURRENCY":        fmt.Sprintf("%d", o.config.Concurrency),
				"WORKTREE_PATH":      ".",
				"OUTPUT_REVIEW_PATH": workerReviewRelPath,
			}
			prompt, err := RenderTemplate(templateText, variables)
			if err != nil {
				errCh <- err
				return
			}
			logPrefix := filepath.Join(reviewDir, fmt.Sprintf("worker-%02d", workerID), "codex")
			if _, err := o.codexRunner.RunPrompt(worktreePath, prompt, nil, logPrefix); err != nil {
				errCh <- err
				return
			}
			doneCh <- struct{}{}
		}(idx)
	}

	go func() {
		wg.Wait()
		close(errCh)
		close(doneCh)
	}()

	completedCount := 0
	for {
		select {
		case err, ok := <-errCh:
			if ok && err != nil {
				o.reporter.StageFinished("independent review stage", roundPtr(round), false, progressMessage(err))
				return nil, err
			}
			if !ok {
				errCh = nil
			}
		case _, ok := <-doneCh:
			if ok {
				completedCount++
				o.reporter.StageProgress(
					"independent review stage",
					fmt.Sprintf("completed reviewer workers: %d/%d", completedCount, o.config.Concurrency),
					roundPtr(round),
				)
			} else {
				doneCh = nil
			}
		}
		if errCh == nil && doneCh == nil {
			break
		}
	}

	for idx, reviewPath := range reviewPaths {
		candidates := []string{
			workerReviewPaths[idx],
			filepath.Join(worktrees[idx], fmt.Sprintf("review-%02d.md", idx+1)),
			filepath.Join(worktrees[idx], "review.md"),
			reviewPath,
		}
		foundPath := ""
		for _, candidate := range candidates {
			if _, err := os.Stat(candidate); err == nil {
				foundPath = candidate
				break
			}
		}
		if foundPath == "" {
			err := NewDeepReviewError("independent review output missing: %s", reviewPath)
			o.reporter.StageFinished("independent review stage", roundPtr(round), false, progressMessage(err))
			return nil, err
		}
		if foundPath != reviewPath {
			content, err := os.ReadFile(foundPath)
			if err != nil {
				o.reporter.StageFinished("independent review stage", roundPtr(round), false, progressMessage(err))
				return nil, err
			}
			if err := os.WriteFile(reviewPath, content, 0o644); err != nil {
				o.reporter.StageFinished("independent review stage", roundPtr(round), false, progressMessage(err))
				return nil, err
			}
		}
		if _, err := os.Stat(reviewPath); err != nil {
			err := NewDeepReviewError("independent review output missing: %s", reviewPath)
			o.reporter.StageFinished("independent review stage", roundPtr(round), false, progressMessage(err))
			return nil, err
		}
	}

	o.reporter.StageFinished(
		"independent review stage",
		roundPtr(round),
		true,
		fmt.Sprintf("generated %d independent review report(s)", len(reviewPaths)),
	)
	return reviewPaths, nil
}

func (o *Orchestrator) runExecuteStage(round int, roundDir, candidateBranch, candidateHead, defaultBranch string, reviewReports []string) (RoundStatus, string, error) {
	o.reporter.StageStarted("execute stage", roundPtr(round), "running execute workflow (triage, plan, execute+verify, cleanup)")
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

	roundStatusPath := filepath.Join(roundDir, "round-status.json")
	roundSummaryPath := filepath.Join(roundDir, "round-summary.md")
	roundTriagePath := filepath.Join(roundDir, "round-triage.md")
	roundPlanPath := filepath.Join(roundDir, "round-plan.md")
	roundVerificationPath := filepath.Join(roundDir, "round-verification.md")
	executeArtifactsDir := filepath.Join(executeWorktree, ".deepreview", "artifacts")
	roundStatusRelPath := filepath.ToSlash(filepath.Join(".deepreview", "artifacts", "round-status.json"))
	roundSummaryRelPath := filepath.ToSlash(filepath.Join(".deepreview", "artifacts", "round-summary.md"))
	roundTriageRelPath := filepath.ToSlash(filepath.Join(".deepreview", "artifacts", "round-triage.md"))
	roundPlanRelPath := filepath.ToSlash(filepath.Join(".deepreview", "artifacts", "round-plan.md"))
	roundVerificationRelPath := filepath.ToSlash(filepath.Join(".deepreview", "artifacts", "round-verification.md"))
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

	reviewInjection, err := buildReviewInjection(reviewReports)
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
		rel, err := filepath.Rel(executeWorktree, dst)
		if err != nil {
			rel = filepath.Base(dst)
		}
		localReviewReports = append(localReviewReports, filepath.ToSlash(rel))
	}

	reviewReportPathsBullet := ""
	for _, p := range localReviewReports {
		reviewReportPathsBullet += "- " + p + "\n"
	}
	reviewReportPathsBullet = strings.TrimSpace(reviewReportPathsBullet)

	variables := map[string]string{
		"REPO_SLUG":               o.repoIdentity.Slug(),
		"SOURCE_BRANCH":           o.config.SourceBranch,
		"DEFAULT_BRANCH":          defaultBranch,
		"ROUND_NUMBER":            fmt.Sprintf("%d", round),
		"MAX_ROUNDS":              fmt.Sprintf("%d", o.config.MaxRounds),
		"WORKTREE_PATH":           ".",
		"REVIEW_REPORT_PATHS":     reviewReportPathsBullet,
		"REVIEW_REPORTS_MARKDOWN": reviewInjection,
		// Backward compatibility for older templates that still use fanout placeholders.
		"FANOUT_REVIEW_PATHS":     reviewReportPathsBullet,
		"FANOUT_REVIEWS_MARKDOWN": reviewInjection,
		"ROUND_TRIAGE_PATH":       roundTriageRelPath,
		"ROUND_PLAN_PATH":         roundPlanRelPath,
		"ROUND_VERIFICATION_PATH": roundVerificationRelPath,
		"ROUND_STATUS_PATH":       roundStatusRelPath,
		"ROUND_SUMMARY_PATH":      roundSummaryRelPath,
	}

	var threadID *string
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
		result, err := o.codexRunner.RunPrompt(executeWorktree, prompt, threadID, logPrefix)
		if err != nil {
			o.reporter.StageFinished(stageName, roundPtr(round), false, progressMessage(err))
			o.reporter.StageFinished("execute stage", roundPtr(round), false, progressMessage(err))
			return RoundStatus{}, "", err
		}
		threadID = &result.ThreadID
		o.reporter.StageFinished(stageName, roundPtr(round), true, "completed")
	}

	if err := ensureCanonicalArtifact(roundStatusPath, []string{
		roundStatusWorktreePath,
		filepath.Join(executeWorktree, "round-status.json"),
		roundStatusPath,
	}); err != nil {
		err := NewDeepReviewError("round status file missing: %s", roundStatusPath)
		o.reporter.StageFinished("execute stage", roundPtr(round), false, progressMessage(err))
		return RoundStatus{}, "", err
	}
	if err := ensureCanonicalArtifact(roundSummaryPath, []string{
		roundSummaryWorktreePath,
		filepath.Join(executeWorktree, "round-summary.md"),
		roundSummaryPath,
	}); err != nil {
		err := NewDeepReviewError("round summary file missing: %s", roundSummaryPath)
		o.reporter.StageFinished("execute stage", roundPtr(round), false, progressMessage(err))
		return RoundStatus{}, "", err
	}
	if err := ensureCanonicalArtifact(roundTriagePath, []string{
		roundTriageWorktreePath,
		filepath.Join(executeWorktree, "round-triage.md"),
		roundTriagePath,
	}); err != nil {
		err := NewDeepReviewError("round triage file missing: %s", roundTriagePath)
		o.reporter.StageFinished("execute stage", roundPtr(round), false, progressMessage(err))
		return RoundStatus{}, "", err
	}
	if err := ensureCanonicalArtifact(roundPlanPath, []string{
		roundPlanWorktreePath,
		filepath.Join(executeWorktree, "round-plan.md"),
		roundPlanPath,
	}); err != nil {
		err := NewDeepReviewError("round plan file missing: %s", roundPlanPath)
		o.reporter.StageFinished("execute stage", roundPtr(round), false, progressMessage(err))
		return RoundStatus{}, "", err
	}
	if err := ensureCanonicalArtifact(roundVerificationPath, []string{
		roundVerificationWorktreePath,
		filepath.Join(executeWorktree, "round-verification.md"),
		roundVerificationPath,
	}); err != nil {
		err := NewDeepReviewError("round verification file missing: %s", roundVerificationPath)
		o.reporter.StageFinished("execute stage", roundPtr(round), false, progressMessage(err))
		return RoundStatus{}, "", err
	}
	// Internal deepreview prompt artifacts must never end up in candidate commits.
	if err := os.RemoveAll(filepath.Join(executeWorktree, ".deepreview")); err != nil {
		o.reporter.StageFinished("execute stage", roundPtr(round), false, progressMessage(err))
		return RoundStatus{}, "", err
	}

	changed, err := HasUncommittedChanges(executeWorktree, o.config.GitBin)
	if err != nil {
		o.reporter.StageFinished("execute stage", roundPtr(round), false, progressMessage(err))
		return RoundStatus{}, "", err
	}
	if changed {
		if err := CommitAllChanges(executeWorktree, o.config.GitBin, fmt.Sprintf("deepreview: round %02d execute updates", round)); err != nil {
			o.reporter.StageFinished("execute stage", roundPtr(round), false, progressMessage(err))
			return RoundStatus{}, "", err
		}
	}

	changed, err = HasUncommittedChanges(executeWorktree, o.config.GitBin)
	if err != nil {
		o.reporter.StageFinished("execute stage", roundPtr(round), false, progressMessage(err))
		return RoundStatus{}, "", err
	}
	if changed {
		err := NewDeepReviewError("execute worktree has uncommitted changes after auto-commit: %s", executeWorktree)
		o.reporter.StageFinished("execute stage", roundPtr(round), false, progressMessage(err))
		return RoundStatus{}, "", err
	}

	candidateHeadAfter, err := RevParse(o.managedRepoPath, o.config.GitBin, candidateBranch)
	if err != nil {
		o.reporter.StageFinished("execute stage", roundPtr(round), false, progressMessage(err))
		return RoundStatus{}, "", err
	}
	if err := o.validateNoInternalArtifactChanges(candidateHead, candidateHeadAfter); err != nil {
		o.reporter.StageFinished("execute stage", roundPtr(round), false, progressMessage(err))
		return RoundStatus{}, "", err
	}

	status, err := readRoundStatus(roundStatusPath)
	if err != nil {
		o.reporter.StageFinished("execute stage", roundPtr(round), false, progressMessage(err))
		return RoundStatus{}, "", err
	}
	o.reporter.StageFinished("execute stage", roundPtr(round), true, fmt.Sprintf("round status recorded (decision=%s)", status.Decision))
	return status, roundSummaryPath, nil
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

func isInternalArtifactPath(path string) bool {
	normalized := filepath.ToSlash(strings.TrimSpace(path))
	return strings.HasPrefix(normalized, ".deepreview/")
}

func (o *Orchestrator) validateNoInternalArtifactChanges(baseRef, headRef string) error {
	files, err := ListChangedFiles(o.managedRepoPath, o.config.GitBin, baseRef, headRef)
	if err != nil {
		return err
	}
	for _, file := range files {
		if isInternalArtifactPath(file) {
			return NewDeepReviewError("internal deepreview artifacts must not be committed: %s", file)
		}
	}
	return nil
}

func (o *Orchestrator) validateDeliveryFiles(candidateBranch string) ([]string, error) {
	files, err := ListChangedFiles(o.managedRepoPath, o.config.GitBin, "origin/"+o.config.SourceBranch, candidateBranch)
	if err != nil {
		return nil, err
	}
	for _, file := range files {
		if isInternalArtifactPath(file) {
			return nil, NewDeepReviewError("delivery blocked: internal deepreview artifacts detected in branch diff: %s", file)
		}
	}
	return files, nil
}

func executePromptLabel(templateName string) string {
	switch templateName {
	case "01-consolidate-reviews.md", "01-review-triage.md":
		return "consolidate reviews"
	case "02-plan.md", "02-change-plan.md":
		return "plan"
	case "03-execute-verify.md":
		return "execute and verify"
	case "04-cleanup-summary-commit.md":
		return "cleanup, summary, commit"
	default:
		return templateName
	}
}

func buildReviewInjection(reportPaths []string) (string, error) {
	chunks := make([]string, 0, len(reportPaths))
	for _, path := range reportPaths {
		b, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		chunks = append(chunks, fmt.Sprintf("## %s\n\n%s\n", filepath.Base(path), strings.TrimSpace(string(b))))
	}
	return strings.TrimSpace(strings.Join(chunks, "\n")), nil
}

func readRoundStatus(path string) (RoundStatus, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return RoundStatus{}, err
	}

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

func (o *Orchestrator) secretHygieneScan(candidateBranch string) error {
	changedFiles, err := ListChangedFiles(o.managedRepoPath, o.config.GitBin, "origin/"+o.config.SourceBranch, candidateBranch)
	if err != nil {
		return err
	}

	for _, rel := range changedFiles {
		path := filepath.Join(o.managedRepoPath, rel)
		st, err := os.Stat(path)
		if err != nil || st.IsDir() {
			continue
		}
		content, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		text := string(content)
		if containsDisallowedEmail(text) {
			return NewDeepReviewError("privacy scan failed: disallowed email-like value detected in %s", rel)
		}
		for _, pattern := range secretRiskyPatterns {
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
	}
	return nil
}

func (o *Orchestrator) deliveryCommitMessageScan(candidateBranch string) error {
	out, err := Git(
		o.managedRepoPath,
		o.config.GitBin,
		true,
		"log",
		"--format=%s%n%b%x00",
		"origin/"+o.config.SourceBranch+".."+candidateBranch,
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

func (o *Orchestrator) deliver(defaultBranch, candidateBranch string, summaries []string, changedFiles []string) (DeliveryResult, error) {
	if o.config.Mode == ModeYolo {
		refspec := candidateBranch + ":" + o.config.SourceBranch
		if err := PushRefspec(o.managedRepoPath, o.config.GitBin, refspec); err != nil {
			return DeliveryResult{}, err
		}
		o.pushCount++
		return DeliveryResult{
			Mode:          ModeYolo,
			PushedRefspec: refspec,
			CommitsURL:    fmt.Sprintf("https://github.com/%s/commits/%s", o.repoIdentity.Slug(), escapeBranchForURL(o.config.SourceBranch)),
		}, nil
	}

	deliveryBranch := "deepreview/" + SanitizeSegment(o.config.SourceBranch) + "/" + SanitizeSegment(o.config.RunID)
	refspec := candidateBranch + ":" + deliveryBranch
	o.reporter.StageProgress("delivery", "pushing delivery branch and creating pull request", nil)
	if err := PushRefspec(o.managedRepoPath, o.config.GitBin, refspec); err != nil {
		return DeliveryResult{}, err
	}
	o.pushCount++

	prTitle := sanitizePublicText("deepreview: review updates")
	if err := assertPublicTextSafe(prTitle, "pr title"); err != nil {
		return DeliveryResult{}, err
	}
	prBody := o.buildPRBody(defaultBranch, candidateBranch, summaries, changedFiles)
	if err := assertPublicTextSafe(prBody, "pr body"); err != nil {
		return DeliveryResult{}, err
	}
	prBodyBasePath := filepath.Join(o.runRoot, "pr-body.base.md")
	prBodyPath := filepath.Join(o.runRoot, "pr-body.md")
	if err := os.WriteFile(prBodyBasePath, []byte(prBody), 0o644); err != nil {
		return DeliveryResult{}, err
	}
	if err := os.WriteFile(prBodyPath, []byte(prBody), 0o644); err != nil {
		return DeliveryResult{}, err
	}

	completed, err := RunCommand(
		[]string{
			o.config.GhBin,
			"pr", "create",
			"--repo", o.repoIdentity.Slug(),
			"--base", o.config.SourceBranch,
			"--head", deliveryBranch,
			"--title", prTitle,
			"--body-file", prBodyPath,
		},
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
	o.reporter.StageProgress("delivery", "running post-pr codex summary and updating pr body", nil)
	if err := o.enhancePRDescription(defaultBranch, candidateBranch, deliveryBranch, prTitle, prURL, prBodyBasePath, prBodyPath, summaries, changedFiles); err != nil {
		o.reporter.StageProgress("delivery", "post-pr description enhancement failed; keeping base body: "+progressMessage(err), nil)
	}

	return DeliveryResult{
		Mode:          ModePR,
		PushedRefspec: refspec,
		PRURL:         prURL,
		CommitsURL:    fmt.Sprintf("https://github.com/%s/commits/%s", o.repoIdentity.Slug(), escapeBranchForURL(deliveryBranch)),
	}, nil
}

func (o *Orchestrator) enhancePRDescription(defaultBranch, candidateBranch, deliveryBranch, prTitle, prURL, baseBodyPath, finalBodyPath string, summaries, changedFiles []string) error {
	templatePath := filepath.Join(o.promptsRoot, "delivery", "pr-description-summary.md")
	templateText, err := ReadTemplate(templatePath)
	if err != nil {
		return err
	}

	summaryOutputPath := filepath.Join(o.runRoot, "pr-top-summary.md")
	baseBodyRelPath := filepath.Base(baseBodyPath)
	summaryOutputRelPath := filepath.Base(summaryOutputPath)
	variables := map[string]string{
		"REPO_SLUG":            o.repoIdentity.Slug(),
		"SOURCE_BRANCH":        o.config.SourceBranch,
		"DEFAULT_BRANCH":       defaultBranch,
		"CANDIDATE_BRANCH":     candidateBranch,
		"DELIVERY_BRANCH":      deliveryBranch,
		"RUN_ID":               o.config.RunID,
		"PR_TITLE":             prTitle,
		"PR_URL":               prURL,
		"MANAGED_REPO_PATH":    ".",
		"RUN_ROOT":             ".",
		"BASE_PR_BODY_PATH":    baseBodyRelPath,
		"OUTPUT_SUMMARY_PATH":  summaryOutputRelPath,
		"CHANGED_FILES_LIST":   formatChangedFilesList(changedFiles),
		"ROUND_ARTIFACT_INDEX": buildRoundArtifactIndex(o.runRoot, summaries),
	}
	prompt, err := RenderTemplate(templateText, variables)
	if err != nil {
		return err
	}
	logPrefix := filepath.Join(o.runRoot, "logs", "delivery-pr-description")
	if _, err := o.codexRunner.RunPrompt(o.runRoot, prompt, nil, logPrefix); err != nil {
		return err
	}

	if err := ensureCanonicalArtifact(summaryOutputPath, []string{
		summaryOutputPath,
		filepath.Join(o.runRoot, "pr-summary.md"),
		filepath.Join(o.runRoot, "summary.md"),
	}); err != nil {
		return NewDeepReviewError("enhanced pr summary output missing: %s", summaryOutputPath)
	}
	baseBodyRaw, err := os.ReadFile(baseBodyPath)
	if err != nil {
		return err
	}
	generatedSummaryRaw, err := os.ReadFile(summaryOutputPath)
	if err != nil {
		return err
	}

	generatedSummary := strings.TrimSpace(sanitizePublicText(string(generatedSummaryRaw)))
	if generatedSummary == "" {
		return NewDeepReviewError("enhanced pr summary was empty: %s", summaryOutputPath)
	}
	if err := assertPublicTextSafe(generatedSummary, "enhanced pr summary"); err != nil {
		return err
	}
	baseBody := strings.TrimSpace(sanitizePublicText(string(baseBodyRaw)))
	if err := assertPublicTextSafe(baseBody, "base pr body"); err != nil {
		return err
	}
	combined := sanitizePublicText(strings.TrimSpace(generatedSummary + "\n\n---\n\n" + baseBody + "\n"))
	if err := assertPublicTextSafe(combined, "combined pr body"); err != nil {
		return err
	}
	if err := os.WriteFile(finalBodyPath, []byte(combined), 0o644); err != nil {
		return err
	}

	prRef := strings.TrimSpace(prURL)
	if prRef == "" {
		prRef = deliveryBranch
	}
	_, err = RunCommand(
		[]string{
			o.config.GhBin,
			"pr", "edit", prRef,
			"--repo", o.repoIdentity.Slug(),
			"--body-file", finalBodyPath,
		},
		o.managedRepoPath,
		"",
		true,
		0,
	)
	return err
}

func formatChangedFilesList(changedFiles []string) string {
	if len(changedFiles) == 0 {
		return "- none"
	}
	unique := make([]string, 0, len(changedFiles))
	seen := map[string]struct{}{}
	for _, raw := range changedFiles {
		path := strings.TrimSpace(raw)
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
	lines := make([]string, 0, len(unique))
	for _, path := range unique {
		lines = append(lines, "- `"+sanitizePublicText(path)+"`")
	}
	if len(lines) == 0 {
		return "- none"
	}
	return strings.Join(lines, "\n")
}

func buildRoundArtifactIndex(runRoot string, summaries []string) string {
	if len(summaries) == 0 {
		return "- none"
	}
	sections := make([]string, 0, len(summaries))
	for _, summaryPath := range summaries {
		roundDir := filepath.Dir(summaryPath)
		roundLabel := filepath.Base(roundDir)
		lines := []string{roundLabel + ":"}

		artifactPaths := []string{
			filepath.Join(roundDir, "round-triage.md"),
			filepath.Join(roundDir, "round-plan.md"),
			filepath.Join(roundDir, "round-verification.md"),
			filepath.Join(roundDir, "round-summary.md"),
			filepath.Join(roundDir, "round-status.json"),
		}
		reviewPaths, _ := filepath.Glob(filepath.Join(roundDir, "review-*.md"))
		sort.Strings(reviewPaths)
		artifactPaths = append(reviewPaths, artifactPaths...)

		for _, path := range artifactPaths {
			if _, err := os.Stat(path); err != nil {
				continue
			}
			rel := path
			if r, err := filepath.Rel(runRoot, path); err == nil {
				rel = r
			}
			lines = append(lines, "- "+filepath.ToSlash(rel))
		}
		sections = append(sections, strings.Join(lines, "\n"))
	}
	return strings.Join(sections, "\n\n")
}

func (o *Orchestrator) buildPRBody(_ string, _ string, summaries, changedFiles []string) string {
	var b strings.Builder
	write := func(format string, args ...any) {
		_, _ = fmt.Fprintf(&b, format, args...)
	}

	write("## at a glance\n")
	write("- reviewed requested source changes across `%d` round(s).\n", len(summaries))
	if len(changedFiles) == 0 {
		write("- no repository file changes were delivered.\n")
	} else {
		areaSummary := summarizeChangedAreas(changedFiles, 6)
		filePreview, omitted := summarizeChangedFilePreview(changedFiles, 8)
		write("- delivered `%d` changed file(s).\n", len(changedFiles))
		write("- main change areas: %s.\n", sanitizePublicText(areaSummary))
		write("- key changed files: %s", sanitizePublicText(filePreview))
		if omitted > 0 {
			write(" (+%d more)", omitted)
		}
		write(".\n")
	}
	write("- detailed round-by-round evidence is included below.\n\n")

	write("## deepreview report\n")
	write("- run id: `%s`\n", o.config.RunID)
	write("- source/default/candidate branch metadata omitted for privacy\n")
	write("- rounds executed: `%d`\n\n", len(summaries))

	write("## changed files\n")
	if len(changedFiles) == 0 {
		write("- no repository file changes in delivery diff.\n\n")
	} else {
		sort.Strings(changedFiles)
		for _, file := range changedFiles {
			write("- `%s`\n", sanitizePublicText(file))
		}
		write("\n")
	}

	for _, summaryPath := range summaries {
		roundDir := filepath.Dir(summaryPath)
		roundLabel := filepath.Base(roundDir)
		write("## %s\n\n", roundLabel)

		statusPath := filepath.Join(roundDir, "round-status.json")
		if status, err := readRoundStatus(statusPath); err == nil {
			confidence := "n/a"
			if status.Confidence != nil {
				confidence = fmt.Sprintf("%.2f", *status.Confidence)
			}
			write("- decision: `%s`\n", status.Decision)
			write("- confidence: `%s`\n", confidence)
			write("- reason: %s\n\n", sanitizePublicText(strings.TrimSpace(status.Reason)))
		}

		reviewPaths, _ := filepath.Glob(filepath.Join(roundDir, "review-*.md"))
		sort.Strings(reviewPaths)
		if len(reviewPaths) > 0 {
			write("### independent reviews\n")
			for _, reviewPath := range reviewPaths {
				if content, err := os.ReadFile(reviewPath); err == nil {
					write("%s\n", detailsBlock(filepath.Base(reviewPath), "markdown", sanitizePublicText(string(content))))
				}
			}
		}

		write("### execute artifacts\n")
		for _, name := range []string{
			"round-triage.md",
			"round-plan.md",
			"round-verification.md",
			"round-summary.md",
			"round-status.json",
		} {
			path := filepath.Join(roundDir, name)
			content, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			lang := "markdown"
			if strings.HasSuffix(name, ".json") {
				lang = "json"
			}
			write("%s\n", detailsBlock(name, lang, sanitizePublicText(string(content))))
		}
	}

	return sanitizePublicText(strings.TrimSpace(b.String()) + "\n")
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
	} else if o.pushCount != 1 {
		return NewDeepReviewError("invalid delivery push count: expected 1, got %d", o.pushCount)
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
	} else {
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
	for _, pattern := range secretRiskyPatterns {
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
	for _, pattern := range secretRiskyPatterns {
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
		return trimForDisplay(sanitizePublicText(message), 220)
	}

	return trimForDisplay(sanitizePublicText(err.Error()), 220)
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
		"repo":                  sanitizePublicText(o.config.Repo),
		"source_branch":         sanitizePublicText(o.config.SourceBranch),
		"concurrency":           o.config.Concurrency,
		"max_rounds":            o.config.MaxRounds,
		"mode":                  o.config.Mode,
		"workspace_root":        sanitizePublicText(o.workspaceRoot),
		"run_id":                o.config.RunID,
		"git_bin":               sanitizePublicText(o.config.GitBin),
		"codex_bin":             sanitizePublicText(o.config.CodexBin),
		"codex_model":           o.config.CodexModel,
		"codex_reasoning":       o.config.CodexReasoning,
		"gh_bin":                sanitizePublicText(o.config.GhBin),
		"codex_timeout_seconds": o.config.CodexTimeoutSeconds,
	}
	b, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(o.runRoot, "run-config.json"), append(b, '\n'), 0o644)
}
