package deepreview

import (
	"os"
	"path/filepath"
	"strings"
)

const (
	deepreviewCommitNameEnv  = "DEEPREVIEW_GIT_USER_NAME"
	deepreviewCommitEmailEnv = "DEEPREVIEW_GIT_USER_EMAIL"
	deepreviewCommitNameKey  = "deepreview.userName"
	deepreviewCommitEmailKey = "deepreview.userEmail"
)

func ResolveCommitIdentity(gitBin, repo string) (CommitIdentity, error) {
	if identity, found, err := commitIdentityFromEnv(); err != nil {
		return CommitIdentity{}, err
	} else if found {
		return identity, nil
	}

	if identity, found, err := gitConfigIdentityGet(gitBin, []string{"--global"}, deepreviewCommitNameKey, deepreviewCommitEmailKey); err != nil {
		return CommitIdentity{}, err
	} else if found {
		return identity, nil
	}

	if identity, found, err := gitConfigIdentityGet(gitBin, []string{"--global"}, "user.name", "user.email"); err != nil {
		return CommitIdentity{}, err
	} else if found {
		return identity, nil
	}

	var identity CommitIdentity
	if isLocalGitRepo(repo) {
		name, err := gitConfigGet(gitBin, repo, "user.name")
		if err != nil {
			return CommitIdentity{}, err
		}
		email, err := gitConfigGet(gitBin, repo, "user.email")
		if err != nil {
			return CommitIdentity{}, err
		}
		identity = CommitIdentity{Name: name, Email: email}
	} else {
		name, err := gitConfigGet(gitBin, "", "user.name")
		if err != nil {
			return CommitIdentity{}, err
		}
		email, err := gitConfigGet(gitBin, "", "user.email")
		if err != nil {
			return CommitIdentity{}, err
		}
		identity = CommitIdentity{Name: name, Email: email}
	}

	if strings.TrimSpace(identity.Name) == "" || strings.TrimSpace(identity.Email) == "" {
		return CommitIdentity{}, NewDeepReviewError(
			"git commit identity is not configured; set DEEPREVIEW_GIT_USER_NAME and DEEPREVIEW_GIT_USER_EMAIL, or configure deepreview.userName/deepreview.userEmail or user.name/user.email in your local git config before running deepreview",
		)
	}
	return identity, nil
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

func commitIdentityFromEnv() (CommitIdentity, bool, error) {
	name := strings.TrimSpace(os.Getenv(deepreviewCommitNameEnv))
	email := strings.TrimSpace(os.Getenv(deepreviewCommitEmailEnv))
	if name == "" && email == "" {
		return CommitIdentity{}, false, nil
	}
	if name == "" || email == "" {
		return CommitIdentity{}, false, NewDeepReviewError(
			"deepreview git identity env override is incomplete; set both DEEPREVIEW_GIT_USER_NAME and DEEPREVIEW_GIT_USER_EMAIL",
		)
	}
	return CommitIdentity{Name: name, Email: email}, true, nil
}

func gitConfigIdentityGet(gitBin string, extraArgs []string, nameKey, emailKey string) (CommitIdentity, bool, error) {
	name, err := gitConfigGetWithArgs(gitBin, extraArgs, "", nameKey)
	if err != nil {
		return CommitIdentity{}, false, err
	}
	email, err := gitConfigGetWithArgs(gitBin, extraArgs, "", emailKey)
	if err != nil {
		return CommitIdentity{}, false, err
	}
	if strings.TrimSpace(name) == "" && strings.TrimSpace(email) == "" {
		return CommitIdentity{}, false, nil
	}
	if strings.TrimSpace(name) == "" || strings.TrimSpace(email) == "" {
		return CommitIdentity{}, false, NewDeepReviewError(
			"deepreview git identity config is incomplete; configure both name and email before running deepreview",
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

func isLocalGitRepo(path string) bool {
	if !isLocalDirectory(path) {
		return false
	}
	_, err := os.Stat(filepath.Join(path, ".git"))
	return err == nil
}
