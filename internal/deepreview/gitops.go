package deepreview

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var sanitizeSegmentRe = regexp.MustCompile(`[^A-Za-z0-9._/-]+`)
var filesystemSafeKeyRe = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

var worktreeOperationalExcludePatterns = []string{
	".deepreview/",
	".tmp/deepreview/",
	".tmp/go-build-cache/",
	".tmp/",
	".codex/",
	".claude/",
	".cache/",
	".pytest_cache/",
	".mypy_cache/",
	".ruff_cache/",
	".tox/",
	".nox/",
}

const (
	operationalExcludeBlockStart = "# deepreview operational artifacts: begin"
	operationalExcludeBlockEnd   = "# deepreview operational artifacts: end"
)

func SanitizeSegment(text string) string {
	sanitized := sanitizeSegmentRe.ReplaceAllString(text, "-")
	sanitized = strings.Trim(sanitized, "-/")
	if sanitized == "" {
		return "value"
	}
	return sanitized
}

func FilesystemSafeKey(text string) string {
	const maxLabelLen = 48

	label := filesystemSafeKeyRe.ReplaceAllString(text, "-")
	label = strings.Trim(label, "-.")
	if label == "" {
		label = "value"
	}
	if len(label) > maxLabelLen {
		label = strings.TrimRight(label[:maxLabelLen], "-.")
		if label == "" {
			label = "value"
		}
	}
	sum := sha256.Sum256([]byte(text))
	return label + "-" + hex.EncodeToString(sum[:8])
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

	for _, candidate := range []string{"main", "master"} {
		exists, err := remoteRefExists(repoPath, gitBin, "refs/remotes/origin/"+candidate)
		if err != nil {
			return "", err
		}
		if exists {
			return candidate, nil
		}
	}
	return "", NewDeepReviewError("unable to resolve default branch from origin/HEAD")
}

func remoteRefExists(repoPath, gitBin, ref string) (bool, error) {
	completed, err := RunCommand([]string{gitBin, "-C", repoPath, "rev-parse", "--verify", "--quiet", ref}, "", "", false, 0)
	if err != nil {
		return false, err
	}
	return completed.ReturnCode == 0 && strings.TrimSpace(completed.Stdout) != "", nil
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
	var removeErr error
	for attempt := 0; attempt < 3; attempt++ {
		_, removeErr = RunCommandContext(
			context.Background(),
			[]string{gitBin, "-C", repoPath, "worktree", "remove", "--force", worktreePath},
			"",
			"",
			true,
			0,
		)
		if statErr := pruneStaleWorktree(repoPath, gitBin, worktreePath); statErr == nil {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	if err := os.RemoveAll(worktreePath); err != nil && !os.IsNotExist(err) {
		if removeErr != nil {
			return removeErr
		}
		return err
	}
	if err := pruneStaleWorktree(repoPath, gitBin, worktreePath); err != nil {
		if removeErr != nil {
			return removeErr
		}
		return err
	}
	return nil
}

func pruneStaleWorktree(repoPath, gitBin, worktreePath string) error {
	_, pruneErr := RunCommandContext(
		context.Background(),
		[]string{gitBin, "-C", repoPath, "worktree", "prune", "--expire", "now"},
		"",
		"",
		true,
		0,
	)
	if pruneErr != nil {
		return pruneErr
	}
	if _, err := os.Stat(worktreePath); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}
	return NewDeepReviewError("worktree path still exists after prune: %s", worktreePath)
}

func EnsureWorktreeOperationalExcludes(repoPath, gitBin string) error {
	excludePath, err := Git(repoPath, gitBin, true, "rev-parse", "--git-path", "info/exclude")
	if err != nil {
		return err
	}
	excludePath = resolveGitPath(repoPath, excludePath)
	existingBytes, err := os.ReadFile(excludePath)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	existing := string(existingBytes)
	patterns, err := managedOperationalExcludePatterns(repoPath, gitBin)
	if err != nil {
		return err
	}
	blockLines := []string{operationalExcludeBlockStart}
	blockLines = append(blockLines, patterns...)
	blockLines = append(blockLines, operationalExcludeBlockEnd)
	block := strings.Join(blockLines, "\n") + "\n"
	updated := upsertManagedExcludeBlock(existing, block)
	if updated == existing {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(excludePath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(excludePath, []byte(updated), 0o644)
}

func resolveGitPath(repoPath, gitPath string) string {
	if filepath.IsAbs(gitPath) {
		return gitPath
	}
	return filepath.Join(repoPath, filepath.FromSlash(gitPath))
}

func managedOperationalExcludePatterns(repoPath, gitBin string) ([]string, error) {
	patterns := make([]string, 0, len(worktreeOperationalExcludePatterns))
	for _, pattern := range worktreeOperationalExcludePatterns {
		relPath := strings.TrimSuffix(pattern, "/")
		if relPath == "" {
			continue
		}
		if relPath == ".deepreview" || relPath == ".tmp/deepreview" {
			patterns = append(patterns, pattern)
			continue
		}
		tracked, err := repoHasTrackedEntries(repoPath, gitBin, relPath)
		if err != nil {
			return nil, err
		}
		if tracked {
			continue
		}
		patterns = append(patterns, pattern)
	}
	return patterns, nil
}

func upsertManagedExcludeBlock(existing, block string) string {
	start := strings.Index(existing, operationalExcludeBlockStart)
	if start >= 0 {
		end := strings.Index(existing[start:], operationalExcludeBlockEnd)
		if end >= 0 {
			end += start + len(operationalExcludeBlockEnd)
			if end < len(existing) && existing[end] == '\n' {
				end++
			}
			existing = existing[:start] + existing[end:]
		}
	}
	existing = strings.TrimRight(existing, "\n")
	if existing == "" {
		return block
	}
	return existing + "\n\n" + block
}

func CleanupUntrackedOperationalArtifacts(repoPath, gitBin string) error {
	for _, pattern := range worktreeOperationalExcludePatterns {
		relPath := strings.TrimSuffix(pattern, "/")
		if relPath == "" {
			continue
		}
		tracked, err := repoHasTrackedEntries(repoPath, gitBin, relPath)
		if err != nil {
			return err
		}
		if tracked {
			continue
		}
		targetPath := filepath.Join(repoPath, filepath.FromSlash(relPath))
		if err := removeAllForceWritable(targetPath); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func removeAllForceWritable(path string) error {
	if err := makeTreeUserWritable(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.RemoveAll(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func makeTreeUserWritable(path string) error {
	return filepath.Walk(path, func(current string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			if os.IsNotExist(walkErr) {
				return nil
			}
			return walkErr
		}
		mode := info.Mode()
		if mode&os.ModeSymlink != 0 {
			return nil
		}
		desired := mode.Perm()
		if info.IsDir() {
			desired |= 0o700
		} else {
			desired |= 0o600
		}
		if mode.Perm() == desired {
			return nil
		}
		return os.Chmod(current, desired)
	})
}

func repoHasTrackedEntries(repoPath, gitBin, relPath string) (bool, error) {
	out, err := Git(repoPath, gitBin, true, "ls-files", "--", relPath)
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) != "", nil
}

func RefHasTrackedEntries(repoPath, gitBin, ref, relPath string) (bool, error) {
	out, err := Git(repoPath, gitBin, true, "ls-tree", "-r", "--name-only", ref, "--", relPath)
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) != "", nil
}

func HasUncommittedChanges(repoPath, gitBin string) (bool, error) {
	status, err := Git(repoPath, gitBin, true, "status", "--porcelain")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(status) != "", nil
}

func CommitAllChanges(repoPath, gitBin, message string, identity CommitIdentity) error {
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
	command := []string{
		gitBin,
		"-C", repoPath,
		"-c", "commit.gpgsign=false",
	}
	if strings.TrimSpace(identity.Name) != "" {
		command = append(command, "-c", "user.name="+identity.Name)
	}
	if strings.TrimSpace(identity.Email) != "" {
		command = append(command, "-c", "user.email="+identity.Email)
	}
	command = append(command, "commit", "--no-gpg-sign", "-m", message)
	_, err = RunCommand(command, "", "", true, 0)
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

func ChangedFileStatusBetweenRefs(repoPath, gitBin, baseRef, headRef, relPath string) (string, error) {
	out, err := Git(
		repoPath,
		gitBin,
		true,
		"diff",
		"--name-status",
		"--find-renames=0",
		baseRef+".."+headRef,
		"--",
		relPath,
	)
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) == 0 {
			continue
		}
		return fields[0], nil
	}
	return "", nil
}

func DiffIsBinaryBetweenRefs(repoPath, gitBin, baseRef, headRef, relPath string) (bool, error) {
	out, err := Git(
		repoPath,
		gitBin,
		true,
		"diff",
		"--numstat",
		"--find-renames=0",
		baseRef+".."+headRef,
		"--",
		relPath,
	)
	if err != nil {
		return false, err
	}
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) < 3 {
			continue
		}
		return fields[0] == "-" && fields[1] == "-", nil
	}
	return false, nil
}

func FileContentAtRef(repoPath, gitBin, ref, relPath string) ([]byte, error) {
	completed, err := RunCommand(
		[]string{gitBin, "-C", repoPath, "show", ref + ":" + relPath},
		"",
		"",
		false,
		0,
	)
	if err != nil {
		return nil, err
	}
	if completed.ReturnCode != 0 {
		return nil, NewDeepReviewError("unable to read %s at %s", relPath, ref)
	}
	return []byte(completed.Stdout), nil
}

func AddedLinesBetweenRefs(repoPath, gitBin, baseRef, headRef, relPath string) ([]string, error) {
	out, err := Git(
		repoPath,
		gitBin,
		true,
		"diff",
		"--no-color",
		"--no-ext-diff",
		"--unified=0",
		baseRef+".."+headRef,
		"--",
		relPath,
	)
	if err != nil {
		return nil, err
	}

	var added []string
	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(line, "+++ "):
			continue
		case strings.HasPrefix(line, "+"):
			added = append(added, strings.TrimPrefix(line, "+"))
		}
	}
	return added, nil
}

func PushRefspec(repoPath, gitBin, refspec string) error {
	_, err := RunCommand([]string{gitBin, "-C", repoPath, "push", "origin", refspec}, "", "", true, 0)
	return err
}

func DryRunPushRefspec(repoPath, gitBin, refspec string) error {
	_, err := RunCommand([]string{gitBin, "-C", repoPath, "push", "--dry-run", "origin", refspec}, "", "", true, 0)
	return err
}
