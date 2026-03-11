package deepreview

import (
	"os"
	"path/filepath"
	"strings"
)

func ResolveCommitIdentity(gitBin, repo string) (CommitIdentity, error) {
	identity := CommitIdentity{}
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
			"git commit identity is not configured; set user.name and user.email in your local git config before running deepreview",
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

func gitConfigGet(gitBin, repoPath, key string) (string, error) {
	command := []string{gitBin}
	if strings.TrimSpace(repoPath) != "" {
		command = append(command, "-C", repoPath)
	}
	command = append(command, "config", "--get", key)
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
