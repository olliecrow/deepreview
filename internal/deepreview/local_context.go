package deepreview

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type LocalGitHubRepoState struct {
	Path          string
	RemoteURL     string
	Owner         string
	Name          string
	CurrentBranch string
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
	upstreamSHA, err := Git(state.Path, gitBin, true, "rev-parse", "--verify", upstreamRef)
	if err != nil {
		return err
	}
	if localSHA == upstreamSHA {
		return nil
	}

	behind, ahead := branchDivergence(gitBin, state.Path, upstreamRef)
	return NewDeepReviewError(
		"local branch `%s` is not synchronized with `%s` (local=%s remote=%s ahead=%d behind=%d); commit/push/pull so remote matches local before review",
		branch,
		upstreamRef,
		shortSHA(localSHA),
		shortSHA(upstreamSHA),
		ahead,
		behind,
	)
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

func branchDivergence(gitBin, repoPath, upstreamRef string) (behind int, ahead int) {
	out, err := Git(repoPath, gitBin, false, "rev-list", "--left-right", "--count", upstreamRef+"...HEAD")
	if err != nil {
		return 0, 0
	}
	fields := strings.Fields(strings.TrimSpace(out))
	if len(fields) != 2 {
		return 0, 0
	}
	behind, _ = strconv.Atoi(fields[0])
	ahead, _ = strconv.Atoi(fields[1])
	return behind, ahead
}

func inferRepoAndBranch(gitBin, repo, sourceBranch string) (resolvedRepo string, resolvedBranch string, err error) {
	cwdState, err := detectGitHubRepoState(gitBin, ".")
	if err != nil {
		return "", "", err
	}

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
	if err := ensureBranchReadyForRemoteReview(gitBin, stateForBranch, inferredBranch); err != nil {
		return "", "", err
	}
	return resolvedRepo, inferredBranch, nil
}
