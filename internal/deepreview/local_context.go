package deepreview

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type LocalGitHubRepoState struct {
	Path          string
	RemoteURL     string
	SourceType    RepoSourceType
	CloneSource   string
	Owner         string
	Name          string
	CurrentBranch string
}

const deepreviewCallerCWDEnv = "DEEPREVIEW_CALLER_CWD"

var branchReadinessRemoteTimeout = 30 * time.Second

func samePath(a, b string) bool {
	aAbs, errA := filepath.Abs(filepath.Clean(a))
	bAbs, errB := filepath.Abs(filepath.Clean(b))
	return errA == nil && errB == nil && aAbs == bAbs
}

func resolveImplicitRepoState(gitBin string, cwdState *LocalGitHubRepoState) (*LocalGitHubRepoState, error) {
	state, explicitOverride, err := resolveCallerCWDRepoState(gitBin, cwdState)
	if err != nil {
		return nil, err
	}
	if explicitOverride {
		return state, nil
	}
	return cwdState, nil
}

func resolveCallerCWDRepoState(gitBin string, cwdState *LocalGitHubRepoState) (*LocalGitHubRepoState, bool, error) {
	callerCWD := strings.TrimSpace(os.Getenv(deepreviewCallerCWDEnv))
	if callerCWD == "" {
		return nil, false, nil
	}
	state, err := detectLocalRepoState(gitBin, callerCWD)
	if err != nil {
		return nil, true, NewDeepReviewError(
			"%s is set but could not be resolved; unset it or point it at the original repo checkout: %s",
			deepreviewCallerCWDEnv,
			err.Error(),
		)
	}
	if state == nil {
		return nil, true, NewDeepReviewError(
			"%s is set but does not resolve to a valid local repo context; unset it or point it at the original repo checkout",
			deepreviewCallerCWDEnv,
		)
	}
	if cwdState != nil && samePath(cwdState.Path, state.Path) {
		return cwdState, true, nil
	}
	return state, true, nil
}

func detectLocalRepoState(gitBin, path string) (*LocalGitHubRepoState, error) {
	if st, err := os.Stat(path); err != nil || !st.IsDir() {
		return nil, nil
	}
	completed, err := inspectGit(gitBin, path, 0, "rev-parse", "--show-toplevel")
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
	state := &LocalGitHubRepoState{
		Path:      topLevel,
		RemoteURL: remoteURL,
	}
	cloneSource, ok := localCloneSource(remoteURL, topLevel)
	if ok {
		state.SourceType = RepoSourceFilesystem
		state.CloneSource = cloneSource
		state.Name = filesystemRepoDisplayName(cloneSource)
	} else {
		owner, name, ok := parseOwnerRepo(remoteURL)
		if !ok {
			return nil, nil
		}
		state.SourceType = RepoSourceGitHub
		state.Owner = owner
		state.Name = name
	}

	branchResult, err := inspectGit(gitBin, topLevel, 0, "symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil {
		return nil, err
	}
	currentBranch := ""
	if branchResult.ReturnCode == 0 {
		currentBranch = strings.TrimSpace(branchResult.Stdout)
	}

	state.CurrentBranch = currentBranch
	return state, nil
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
	return state.SourceType == RepoSourceGitHub &&
		strings.EqualFold(owner, state.Owner) &&
		strings.EqualFold(name, state.Name)
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
		state, err := detectLocalRepoState(gitBin, resolvedRepo)
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
	cwdState, err := detectLocalRepoState(gitBin, ".")
	if err != nil {
		return err
	}
	cwdState, err = resolveImplicitRepoState(gitBin, cwdState)
	if err != nil {
		return err
	}
	// Share repo/branch inference across command surfaces, and only enforce
	// readiness when the resolved source branch matches the current local branch
	// context for the same repository.
	return ensureExplicitSourceBranchReadyForRemoteReview(gitBin, resolvedRepo, sourceBranch, cwdState)
}

func ensureBranchReadyForRemoteReview(gitBin string, state *LocalGitHubRepoState, branch string) error {
	if state == nil {
		return NewDeepReviewError("unable to validate local branch readiness: no local repository context")
	}

	status, err := inspectGitOutput(gitBin, state.Path, "status", "--porcelain", "--untracked-files=no", "--ignore-submodules=dirty")
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

	localSHA, err := inspectGitOutput(gitBin, state.Path, "rev-parse", "--verify", "HEAD")
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

	remoteURL, err := inspectGitOutput(gitBin, repoPath, "remote", "get-url", "--", parts[0])
	if err != nil {
		return "", NewDeepReviewError("unable to query upstream ref `%s` before readiness check: %s", upstreamRef, err.Error())
	}
	// ls-remote runs outside the checkout, so resolve a relative path here.
	if localPath, ok := localCloneSource(remoteURL, repoPath); ok {
		remoteURL = localPath
	}
	sha, err := inspectRemoteRef(gitBin, remoteURL, "refs/heads/"+parts[1], branchReadinessRemoteTimeout)
	if err != nil {
		return "", NewDeepReviewError("unable to query upstream ref `%s` before readiness check: %s", upstreamRef, err.Error())
	}
	return sha, nil
}

func resolveUpstreamRef(gitBin, repoPath, branch string) (string, error) {
	expected := "origin/" + branch
	currentUpstream := ""
	upstreamResult, err := inspectGit(gitBin, repoPath, 0, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{u}")
	if err != nil {
		return "", err
	}
	if upstreamResult.ReturnCode == 0 {
		currentUpstream = strings.TrimSpace(upstreamResult.Stdout)
		if currentUpstream == expected {
			return currentUpstream, nil
		}
	}

	fallbackResult, err := inspectGit(gitBin, repoPath, 0, "rev-parse", "--verify", "--quiet", expected)
	if err != nil {
		return "", err
	}
	if fallbackResult.ReturnCode == 0 {
		return expected, nil
	}

	if currentUpstream != "" {
		return "", NewDeepReviewError(
			"unable to verify push state for `%s`: current branch tracks `%s`, but deepreview requires `%s`; push with `git push -u origin %s` first",
			branch,
			currentUpstream,
			expected,
			branch,
		)
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
	if _, err := inspectGitOutput(gitBin, repoPath, "cat-file", "-e", trimmed+"^{commit}"); err != nil {
		return 0, 0, false
	}
	out, err := inspectGitOutput(gitBin, repoPath, "rev-list", "--left-right", "--count", trimmed+"...HEAD")
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
	cwdState, err := detectLocalRepoState(gitBin, ".")
	if err != nil {
		return "", "", err
	}
	cwdState, err = resolveImplicitRepoState(gitBin, cwdState)
	if err != nil {
		return "", "", err
	}
	explicitRepoProvided := strings.TrimSpace(repo) != ""
	if cwdState != nil && cwdState.SourceType != RepoSourceGitHub {
		cwdState = nil
	}

	if !explicitRepoProvided {
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
		stateForBranch, err = detectLocalRepoState(gitBin, resolvedRepo)
		if err != nil {
			return "", "", err
		}
	}
	if stateForBranch == nil {
		if explicitRepoProvided {
			return "", "", NewDeepReviewError("--source-branch not provided and unable to infer branch from local repo context; provide --source-branch explicitly")
		}
		return "", "", NewDeepReviewError("--source-branch not provided and unable to infer branch from a valid local GitHub repository; provide --source-branch explicitly")
	}

	inferredBranch, err := inferSourceBranchFromState(stateForBranch)
	if err != nil {
		return "", "", err
	}
	return resolvedRepo, inferredBranch, nil
}
