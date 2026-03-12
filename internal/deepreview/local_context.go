package deepreview

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

type LocalGitHubRepoState struct {
	Path          string
	RemoteURL     string
	Owner         string
	Name          string
	CurrentBranch string
}

const deepreviewCallerCWDEnv = "DEEPREVIEW_CALLER_CWD"

var detectDeepreviewSourceRoot = defaultDeepreviewSourceRoot
var branchReadinessRemoteTimeout = 30 * time.Second

func defaultDeepreviewSourceRoot() (string, bool) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		return "", false
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", ".."))
	if st, err := os.Stat(filepath.Join(root, "prompts")); err != nil || !st.IsDir() {
		return "", false
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", false
	}
	return abs, true
}

func samePath(a, b string) bool {
	aAbs, errA := filepath.Abs(filepath.Clean(a))
	bAbs, errB := filepath.Abs(filepath.Clean(b))
	return errA == nil && errB == nil && aAbs == bAbs
}

func resolveImplicitRepoState(gitBin string, cwdState *LocalGitHubRepoState) *LocalGitHubRepoState {
	if state := resolveCallerCWDRepoState(gitBin, cwdState); state != nil {
		return state
	}
	if callerContextFallbackAllowed(cwdState) {
		if state := resolveOldPWDRepoState(gitBin, cwdState); state != nil {
			return state
		}
	}
	return cwdState
}

func callerContextFallbackAllowed(cwdState *LocalGitHubRepoState) bool {
	if cwdState == nil {
		return false
	}
	sourceRoot, ok := detectDeepreviewSourceRoot()
	return ok && samePath(cwdState.Path, sourceRoot)
}

func resolveCallerCWDRepoState(gitBin string, cwdState *LocalGitHubRepoState) *LocalGitHubRepoState {
	callerCWD := strings.TrimSpace(os.Getenv(deepreviewCallerCWDEnv))
	if callerCWD == "" {
		return nil
	}
	state, err := detectGitHubRepoState(gitBin, callerCWD)
	if err != nil || state == nil {
		return nil
	}
	if cwdState != nil && samePath(cwdState.Path, state.Path) {
		return cwdState
	}
	return state
}

func resolveOldPWDRepoState(gitBin string, cwdState *LocalGitHubRepoState) *LocalGitHubRepoState {
	if !callerContextFallbackAllowed(cwdState) {
		return nil
	}

	oldpwd := strings.TrimSpace(os.Getenv("OLDPWD"))
	if oldpwd == "" {
		return nil
	}
	state, err := detectGitHubRepoState(gitBin, oldpwd)
	if err != nil || state == nil {
		return nil
	}
	if samePath(cwdState.Path, state.Path) {
		return nil
	}
	return state
}

func detectGitHubRepoState(gitBin, path string) (*LocalGitHubRepoState, error) {
	completed, err := RunCommand([]string{gitBin, "-C", path, "rev-parse", "--show-toplevel"}, "", "", false, 0)
	if err != nil {
		return nil, err
	}
	if completed.ReturnCode != 0 {
		return nil, nil
	}

	topLevel := strings.TrimSpace(completed.Stdout)
	if topLevel == "" {
		return nil, nil
	}
	topLevel, err = filepath.Abs(topLevel)
	if err != nil {
		return nil, err
	}

	remoteURL, err := tryReadRemoteURL(gitBin, topLevel)
	if err != nil {
		return nil, err
	}
	owner, name, ok := parseOwnerRepo(remoteURL)
	if !ok {
		return nil, nil
	}

	branchResult, err := RunCommand([]string{gitBin, "-C", topLevel, "symbolic-ref", "--quiet", "--short", "HEAD"}, "", "", false, 0)
	if err != nil {
		return nil, err
	}
	currentBranch := ""
	if branchResult.ReturnCode == 0 {
		currentBranch = strings.TrimSpace(branchResult.Stdout)
	}

	return &LocalGitHubRepoState{
		Path:          topLevel,
		RemoteURL:     remoteURL,
		Owner:         owner,
		Name:          name,
		CurrentBranch: currentBranch,
	}, nil
}

func repoLocatorMatchesState(repo string, state *LocalGitHubRepoState) bool {
	if state == nil {
		return false
	}

	if st, err := os.Stat(repo); err == nil && st.IsDir() {
		repoAbs, err := filepath.Abs(filepath.Clean(repo))
		if err == nil && repoAbs == state.Path {
			return true
		}
	}

	owner, name, ok := parseOwnerRepo(repo)
	if !ok {
		return false
	}
	return owner == state.Owner && name == state.Name
}

func inferSourceBranchFromState(state *LocalGitHubRepoState) (string, error) {
	if state == nil {
		return "", NewDeepReviewError("unable to infer source branch from current context")
	}
	if strings.TrimSpace(state.CurrentBranch) == "" {
		return "", NewDeepReviewError("unable to infer --source-branch: repository is in detached HEAD; pass --source-branch explicitly")
	}
	return state.CurrentBranch, nil
}

func ensureExplicitSourceBranchReadyForRemoteReview(gitBin, resolvedRepo, explicitBranch string, cwdState *LocalGitHubRepoState) error {
	branch := strings.TrimSpace(explicitBranch)
	if branch == "" {
		return nil
	}

	stateForBranch := (*LocalGitHubRepoState)(nil)
	if cwdState != nil && repoLocatorMatchesState(resolvedRepo, cwdState) {
		stateForBranch = cwdState
	} else {
		state, err := detectGitHubRepoState(gitBin, resolvedRepo)
		if err != nil {
			return err
		}
		stateForBranch = state
	}
	if stateForBranch == nil {
		return nil
	}
	if strings.TrimSpace(stateForBranch.CurrentBranch) == "" {
		return nil
	}
	if stateForBranch.CurrentBranch != branch {
		return nil
	}
	return ensureBranchReadyForRemoteReview(gitBin, stateForBranch, branch)
}

func validateLocalBranchReadyForRemoteReview(gitBin, resolvedRepo, sourceBranch string) error {
	cwdState, err := detectGitHubRepoState(gitBin, ".")
	if err != nil {
		return err
	}
	cwdState = resolveImplicitRepoState(gitBin, cwdState)
	// Keep repo/branch inference shared across commands, but only review runs
	// should fail on local-current-branch readiness problems.
	return ensureExplicitSourceBranchReadyForRemoteReview(gitBin, resolvedRepo, sourceBranch, cwdState)
}

func ensureBranchReadyForRemoteReview(gitBin string, state *LocalGitHubRepoState, branch string) error {
	if state == nil {
		return NewDeepReviewError("unable to validate local branch readiness: no local repository context")
	}

	status, err := Git(state.Path, gitBin, true, "status", "--porcelain", "--untracked-files=no")
	if err != nil {
		return err
	}
	if strings.TrimSpace(status) != "" {
		return NewDeepReviewError("local tracked changes are present on `%s`; commit/stash tracked changes before running deepreview", branch)
	}

	upstreamRef, err := resolveUpstreamRef(gitBin, state.Path, branch)
	if err != nil {
		return err
	}

	localSHA, err := Git(state.Path, gitBin, true, "rev-parse", "--verify", "HEAD")
	if err != nil {
		return err
	}
	upstreamSHA, err := remoteRefSHA(gitBin, state.Path, upstreamRef)
	if err != nil {
		return err
	}
	if localSHA == upstreamSHA {
		return nil
	}

	behind, ahead, countsKnown := branchDivergenceAgainstCommit(gitBin, state.Path, upstreamSHA)
	message := "local branch `%s` is not synchronized with `%s` (local=%s remote=%s); commit/push/pull so remote matches local before review"
	args := []any{
		branch,
		upstreamRef,
		shortSHA(localSHA),
		shortSHA(upstreamSHA),
	}
	if countsKnown {
		message = "local branch `%s` is not synchronized with `%s` (local=%s remote=%s ahead=%d behind=%d); commit/push/pull so remote matches local before review"
		args = append(args, ahead, behind)
	}
	return NewDeepReviewError(message, args...)
}

func remoteRefSHA(gitBin, repoPath, upstreamRef string) (string, error) {
	parts := strings.SplitN(strings.TrimSpace(upstreamRef), "/", 2)
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		return "", NewDeepReviewError("unable to query upstream ref `%s`: unsupported upstream format", upstreamRef)
	}

	remoteResult, err := RunCommand(
		[]string{gitBin, "-C", repoPath, "ls-remote", "--exit-code", "--heads", parts[0], "refs/heads/" + parts[1]},
		"",
		"",
		false,
		branchReadinessRemoteTimeout,
	)
	if err != nil {
		return "", NewDeepReviewError("unable to query upstream ref `%s` before readiness check: %s", upstreamRef, err.Error())
	}
	if remoteResult.ReturnCode != 0 {
		return "", NewDeepReviewError("unable to query upstream ref `%s` before readiness check", upstreamRef)
	}
	fields := strings.Fields(strings.TrimSpace(remoteResult.Stdout))
	if len(fields) == 0 || strings.TrimSpace(fields[0]) == "" {
		return "", NewDeepReviewError("unable to query upstream ref `%s` before readiness check", upstreamRef)
	}
	return strings.TrimSpace(fields[0]), nil
}

func resolveUpstreamRef(gitBin, repoPath, branch string) (string, error) {
	upstreamResult, err := RunCommand([]string{gitBin, "-C", repoPath, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{u}"}, "", "", false, 0)
	if err != nil {
		return "", err
	}
	if upstreamResult.ReturnCode == 0 {
		upstreamRef := strings.TrimSpace(upstreamResult.Stdout)
		if upstreamRef != "" {
			return upstreamRef, nil
		}
	}

	fallback := "origin/" + branch
	fallbackResult, err := RunCommand([]string{gitBin, "-C", repoPath, "rev-parse", "--verify", "--quiet", fallback}, "", "", false, 0)
	if err != nil {
		return "", err
	}
	if fallbackResult.ReturnCode == 0 {
		return fallback, nil
	}

	return "", NewDeepReviewError(
		"unable to verify push state for `%s`: no upstream tracking branch is configured; push with `git push -u origin %s` first",
		branch,
		branch,
	)
}

func branchDivergenceAgainstCommit(gitBin, repoPath, commit string) (behind int, ahead int, ok bool) {
	trimmed := strings.TrimSpace(commit)
	if trimmed == "" {
		return 0, 0, false
	}
	if _, err := Git(repoPath, gitBin, false, "cat-file", "-e", trimmed+"^{commit}"); err != nil {
		return 0, 0, false
	}
	out, err := Git(repoPath, gitBin, false, "rev-list", "--left-right", "--count", trimmed+"...HEAD")
	if err != nil {
		return 0, 0, false
	}
	fields := strings.Fields(strings.TrimSpace(out))
	if len(fields) != 2 {
		return 0, 0, false
	}
	behind, _ = strconv.Atoi(fields[0])
	ahead, _ = strconv.Atoi(fields[1])
	return behind, ahead, true
}

func inferRepoAndBranch(gitBin, repo, sourceBranch string) (resolvedRepo string, resolvedBranch string, err error) {
	cwdState, err := detectGitHubRepoState(gitBin, ".")
	if err != nil {
		return "", "", err
	}
	cwdState = resolveImplicitRepoState(gitBin, cwdState)

	if strings.TrimSpace(repo) == "" {
		if cwdState == nil {
			return "", "", NewDeepReviewError("repo locator not provided and current directory is not a valid GitHub repo with an origin remote")
		}
		resolvedRepo = cwdState.Path
	} else {
		resolvedRepo = repo
	}

	if strings.TrimSpace(sourceBranch) != "" {
		return resolvedRepo, sourceBranch, nil
	}

	stateForBranch := (*LocalGitHubRepoState)(nil)
	if cwdState != nil && repoLocatorMatchesState(resolvedRepo, cwdState) {
		stateForBranch = cwdState
	} else {
		stateForBranch, err = detectGitHubRepoState(gitBin, resolvedRepo)
		if err != nil {
			return "", "", err
		}
	}
	if stateForBranch == nil {
		return "", "", NewDeepReviewError("--source-branch not provided and unable to infer branch from a valid local GitHub repository; provide --source-branch explicitly")
	}

	inferredBranch, err := inferSourceBranchFromState(stateForBranch)
	if err != nil {
		return "", "", err
	}
	return resolvedRepo, inferredBranch, nil
}
