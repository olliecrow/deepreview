package deepreview

import (
	"strings"
)

func ResolveCommitIdentity(gitBin, repo string) (CommitIdentity, error) {
	if repoPath, err := resolveCommitIdentityRepoPath(gitBin, repo); err != nil {
		return CommitIdentity{}, err
	} else if repoPath != "" {
		identity, found, err := gitConfigIdentityGet(gitBin, repoPath, nil)
		if err != nil {
			return CommitIdentity{}, err
		}
		if found {
			return identity, nil
		}
	}

	identity, found, err := gitConfigIdentityGet(gitBin, "", []string{"--global"})
	if err != nil {
		return CommitIdentity{}, err
	}
	if found {
		return identity, nil
	}

	return CommitIdentity{}, NewDeepReviewError(
		"git commit identity is not configured; set user.name and user.email in the source repo or in your global git config before running deepreview",
	)
}

func resolveCommitIdentityRepoPath(gitBin, repo string) (string, error) {
	if isLocalGitRepo(gitBin, repo) {
		return repo, nil
	}

	cwdState, err := detectLocalRepoState(gitBin, ".")
	if err != nil {
		return "", err
	}
	cwdState = resolveImplicitRepoState(gitBin, cwdState)
	if cwdState != nil && repoLocatorMatchesState(repo, cwdState) {
		return cwdState.Path, nil
	}
	return "", nil
}

func ConfigureManagedGitIdentity(repoPath, gitBin string, identity CommitIdentity) error {
	for _, pair := range []struct {
		key   string
		value string
	}{
		{key: "user.name", value: strings.TrimSpace(identity.Name)},
		{key: "user.email", value: strings.TrimSpace(identity.Email)},
		{key: "commit.gpgsign", value: "false"},
	} {
		if _, err := RunCommand([]string{gitBin, "-C", repoPath, "config", pair.key, pair.value}, "", "", true, 0); err != nil {
			return err
		}
	}
	return nil
}

func gitConfigIdentityGet(gitBin, repoPath string, extraArgs []string) (CommitIdentity, bool, error) {
	name, err := gitConfigGetWithArgs(gitBin, extraArgs, repoPath, "user.name")
	if err != nil {
		return CommitIdentity{}, false, err
	}
	email, err := gitConfigGetWithArgs(gitBin, extraArgs, repoPath, "user.email")
	if err != nil {
		return CommitIdentity{}, false, err
	}
	if strings.TrimSpace(name) == "" && strings.TrimSpace(email) == "" {
		return CommitIdentity{}, false, nil
	}
	if strings.TrimSpace(name) == "" || strings.TrimSpace(email) == "" {
		scope := "global git config"
		if strings.TrimSpace(repoPath) != "" {
			scope = "source repo git config"
		}
		return CommitIdentity{}, false, NewDeepReviewError(
			"%s is incomplete; configure both user.name and user.email before running deepreview",
			scope,
		)
	}
	return CommitIdentity{Name: name, Email: email}, true, nil
}

func gitConfigGet(gitBin, repoPath, key string) (string, error) {
	return gitConfigGetWithArgs(gitBin, nil, repoPath, key)
}

func gitConfigGetWithArgs(gitBin string, extraArgs []string, repoPath, key string) (string, error) {
	command := []string{gitBin}
	if strings.TrimSpace(repoPath) != "" {
		command = append(command, "-C", repoPath)
	}
	command = append(command, "config")
	command = append(command, extraArgs...)
	command = append(command, "--get", key)
	completed, err := RunCommand(command, "", "", false, 0)
	if err != nil {
		return "", err
	}
	if completed.ReturnCode != 0 {
		return "", nil
	}
	return strings.TrimSpace(completed.Stdout), nil
}

func isLocalGitRepo(gitBin, path string) bool {
	if !isLocalDirectory(path) {
		return false
	}
	completed, err := RunCommand([]string{gitBin, "-C", path, "rev-parse", "--git-dir"}, "", "", false, 0)
	return err == nil && completed.ReturnCode == 0 && strings.TrimSpace(completed.Stdout) != ""
}
