package deepreview

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var sanitizeSegmentRe = regexp.MustCompile(`[^A-Za-z0-9._/-]+`)

func SanitizeSegment(text string) string {
	sanitized := sanitizeSegmentRe.ReplaceAllString(text, "-")
	sanitized = strings.Trim(sanitized, "-/")
	if sanitized == "" {
		return "value"
	}
	return sanitized
}

func Git(repoPath, gitBin string, check bool, args ...string) (string, error) {
	command := append([]string{gitBin}, args...)
	completed, err := RunCommand(command, repoPath, "", check, 0)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(completed.Stdout), nil
}

func CloneOrFetch(repoPath, cloneSource, gitBin string) error {
	if _, err := os.Stat(repoPath); err == nil {
		if err := os.RemoveAll(repoPath); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(repoPath), 0o755); err != nil {
		return err
	}
	if _, err := RunCommand([]string{gitBin, "clone", cloneSource, repoPath}, "", "", true, 0); err != nil {
		return err
	}

	_, err := RunCommand([]string{gitBin, "-C", repoPath, "fetch", "--all", "--prune"}, "", "", true, 0)
	return err
}

func ResolveDefaultBranch(repoPath, gitBin string) (string, error) {
	if ref, err := Git(repoPath, gitBin, true, "symbolic-ref", "--short", "refs/remotes/origin/HEAD"); err == nil {
		if strings.HasPrefix(ref, "origin/") {
			parts := strings.SplitN(ref, "/", 2)
			if len(parts) == 2 {
				return parts[1], nil
			}
		}
	}

	remoteBranches, err := Git(repoPath, gitBin, true, "branch", "-r")
	if err != nil {
		return "", err
	}
	for _, candidate := range []string{"main", "master"} {
		if strings.Contains(remoteBranches, "origin/"+candidate) {
			return candidate, nil
		}
	}
	return "", NewDeepReviewError("unable to resolve default branch from origin/HEAD")
}

func RequireRemoteBranch(repoPath, gitBin, branch string) (string, error) {
	ref := "origin/" + branch
	sha, err := Git(repoPath, gitBin, true, "rev-parse", "--verify", ref)
	if err != nil {
		return "", err
	}
	if sha == "" {
		return "", NewDeepReviewError("remote branch not found: %s", ref)
	}
	return sha, nil
}

func SetBranchToRef(repoPath, gitBin, branch, ref string) error {
	_, err := RunCommand([]string{gitBin, "-C", repoPath, "update-ref", "refs/heads/" + branch, ref}, "", "", true, 0)
	return err
}

func RevParse(repoPath, gitBin, ref string) (string, error) {
	return Git(repoPath, gitBin, true, "rev-parse", "--verify", ref)
}

func AddDetachedWorktree(repoPath, gitBin, worktreePath, ref string) error {
	if err := os.MkdirAll(filepath.Dir(worktreePath), 0o755); err != nil {
		return err
	}
	_, err := RunCommand([]string{gitBin, "-C", repoPath, "worktree", "add", "--detach", worktreePath, ref}, "", "", true, 0)
	return err
}

func AddBranchWorktree(repoPath, gitBin, worktreePath, branch, ref string) error {
	if err := os.MkdirAll(filepath.Dir(worktreePath), 0o755); err != nil {
		return err
	}
	_, err := RunCommand([]string{gitBin, "-C", repoPath, "worktree", "add", "-B", branch, worktreePath, ref}, "", "", true, 0)
	return err
}

func RemoveWorktree(repoPath, gitBin, worktreePath string) error {
	if _, err := os.Stat(worktreePath); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	_, err := RunCommand([]string{gitBin, "-C", repoPath, "worktree", "remove", "--force", worktreePath}, "", "", true, 0)
	return err
}

func HasUncommittedChanges(repoPath, gitBin string) (bool, error) {
	status, err := Git(repoPath, gitBin, true, "status", "--porcelain")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(status) != "", nil
}

func CommitAllChanges(repoPath, gitBin, message string) error {
	if _, err := RunCommand([]string{gitBin, "-C", repoPath, "add", "-A"}, "", "", true, 0); err != nil {
		return err
	}
	changed, err := HasUncommittedChanges(repoPath, gitBin)
	if err != nil {
		return err
	}
	if !changed {
		return nil
	}
	_, err = RunCommand([]string{gitBin, "-C", repoPath, "commit", "-m", message}, "", "", true, 0)
	return err
}

func ListChangedFiles(repoPath, gitBin, baseRef, headRef string) ([]string, error) {
	out, err := Git(repoPath, gitBin, true, "diff", "--name-only", baseRef+".."+headRef)
	if err != nil {
		return nil, err
	}
	var files []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			files = append(files, line)
		}
	}
	return files, nil
}

func PushRefspec(repoPath, gitBin, refspec string) error {
	_, err := RunCommand([]string{gitBin, "-C", repoPath, "push", "origin", refspec}, "", "", true, 0)
	return err
}

func DryRunPushRefspec(repoPath, gitBin, refspec string) error {
	_, err := RunCommand([]string{gitBin, "-C", repoPath, "push", "--dry-run", "origin", refspec}, "", "", true, 0)
	return err
}
