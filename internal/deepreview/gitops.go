package deepreview

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var sanitizeSegmentRe = regexp.MustCompile(`[^A-Za-z0-9._/-]+`)
var filesystemSafeKeyRe = regexp.MustCompile(`[^A-Za-z0-9._-]+`)
var fullGitSHARe = regexp.MustCompile(`^[0-9a-f]{40}$`)

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
	worktreeCleanupBudget        = 15 * time.Second
	worktreeGitCommandTimeout    = 5 * time.Second
)

// worktreeNotOwnedError means the path is not a linked worktree of the
// repository. Cleanup must not delete it and must not retry.
type worktreeNotOwnedError struct {
	message string
}

func (e *worktreeNotOwnedError) Error() string {
	return e.message
}

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

func managedPreparationArgs(gitBin string, args ...string) []string {
	command := []string{
		gitBin,
		"-c", "core.hooksPath=/dev/null",
		"-c", "filter.lfs.required=false",
		"-c", "filter.lfs.smudge=",
		"-c", "filter.lfs.clean=",
		"-c", "filter.lfs.process=",
	}
	return append(command, args...)
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
	if _, err := RunCommand(managedPreparationArgs(gitBin, "clone", cloneSource, repoPath), "", "", true, 0); err != nil {
		return err
	}

	_, err := RunCommand(managedPreparationArgs(gitBin, "-C", repoPath, "fetch", "--all", "--prune"), "", "", true, 0)
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

func CreateBranchAtRef(repoPath, gitBin, branch, ref string) error {
	_, err := RunCommand([]string{gitBin, "-C", repoPath, "branch", branch, ref}, "", "", true, 0)
	return err
}

func FastForwardBranch(repoPath, gitBin, branch, ref string) error {
	current, err := RevParse(repoPath, gitBin, branch)
	if err != nil {
		return err
	}
	forward, err := RefContainsCommit(repoPath, gitBin, current, ref)
	if err != nil {
		return err
	}
	if !forward {
		return NewDeepReviewError("branch %s cannot advance to non-descendant ref %s", branch, ref)
	}
	_, err = RunCommand(
		[]string{gitBin, "-C", repoPath, "update-ref", "refs/heads/" + branch, ref, current},
		"",
		"",
		true,
		0,
	)
	return err
}

func RevParse(repoPath, gitBin, ref string) (string, error) {
	return Git(repoPath, gitBin, true, "rev-parse", "--verify", ref)
}

func RefContainsCommit(repoPath, gitBin, ancestorRef, descendantRef string) (bool, error) {
	ancestorSHA := strings.TrimSpace(ancestorRef)
	if parsed, err := RevParse(repoPath, gitBin, ancestorRef); err == nil {
		ancestorSHA = parsed
	}
	mergeBase, err := Git(repoPath, gitBin, true, "merge-base", ancestorSHA, descendantRef)
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(mergeBase) == strings.TrimSpace(ancestorSHA), nil
}

func RefTree(repoPath, gitBin, ref string) (string, error) {
	trimmedRef := strings.TrimSpace(ref)
	if trimmedRef == "" {
		return "", NewDeepReviewError("tree ref is required")
	}
	return Git(repoPath, gitBin, true, "rev-parse", "--verify", trimmedRef+"^{tree}")
}

func RefsHaveSameTree(repoPath, gitBin, leftRef, rightRef string) (bool, error) {
	leftTree, err := RefTree(repoPath, gitBin, leftRef)
	if err != nil {
		return false, err
	}
	rightTree, err := RefTree(repoPath, gitBin, rightRef)
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(leftTree) == strings.TrimSpace(rightTree), nil
}

func AddDetachedWorktree(repoPath, gitBin, worktreePath, ref string) error {
	if err := os.MkdirAll(filepath.Dir(worktreePath), 0o755); err != nil {
		return err
	}
	_, err := RunCommand(managedPreparationArgs(gitBin, "-C", repoPath, "worktree", "add", "--detach", worktreePath, ref), "", "", true, 0)
	if err != nil {
		return errors.Join(err, removeFailedWorktreeAdd(repoPath, gitBin, worktreePath))
	}
	return nil
}

func removeFailedWorktreeAdd(repoPath, gitBin, worktreePath string) error {
	info, err := os.Lstat(worktreePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return NewDeepReviewError("refusing to remove symlink left by worktree add: %s", worktreePath)
	}
	if ownedErr := assertOwnedLinkedWorktree(repoPath, worktreePath); ownedErr == nil {
		return RemoveWorktree(repoPath, gitBin, worktreePath)
	}
	if !info.IsDir() {
		return nil
	}
	entries, readErr := os.ReadDir(worktreePath)
	if readErr != nil {
		return readErr
	}
	if len(entries) == 0 {
		return os.Remove(worktreePath)
	}
	return nil
}

// RemoveWorktree deletes one linked worktree of repoPath.
// It uses its own 15-second context, not the canceled run context.
// Each git command stops after 5 seconds. A path is deleted only when its
// gitdir is inside that repository's .git/worktrees directory and points
// back at the same path. A missing directory still prunes the registration.
func RemoveWorktree(repoPath, gitBin, worktreePath string) error {
	repoPath = filepath.Clean(repoPath)
	worktreePath = filepath.Clean(worktreePath)
	if err := refuseUnsafeCleanupPath(repoPath, worktreePath); err != nil {
		return err
	}
	gitDir, err := repositoryGitDirectory(repoPath)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), worktreeCleanupBudget)
	defer cancel()
	return removeLinkedWorktree(ctx, repoPath, gitDir, gitBin, worktreePath)
}

func removeWorktrees(repoPath, gitBin string, paths []string) error {
	var joined error
	for _, path := range paths {
		joined = errors.Join(joined, RemoveWorktree(repoPath, gitBin, path))
	}
	return joined
}

func removeLinkedWorktree(ctx context.Context, repoPath, gitDir, gitBin, worktreePath string) error {
	var lastErr error
	for {
		if err := ctx.Err(); err != nil {
			return cleanupFailure(worktreePath, lastErr, err)
		}
		info, statErr := os.Lstat(worktreePath)
		missing := false
		if statErr != nil {
			if !os.IsNotExist(statErr) {
				lastErr = statErr
				if !waitForCleanupRetry(ctx) {
					return cleanupFailure(worktreePath, lastErr, ctx.Err())
				}
				continue
			}
			missing = true
		} else if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return &worktreeNotOwnedError{message: "refusing to clean a path that is not a worktree directory: " + worktreePath}
		} else if err := assertOwnedLinkedWorktree(repoPath, worktreePath); err != nil {
			return err
		} else {
			removeErr := runBoundedGit(ctx, managedPreparationArgs(gitBin, "--git-dir", gitDir, "worktree", "remove", "--force", worktreePath)...)
			if removeErr != nil {
				lastErr = removeErr
			}
			if err := forceRemoveOwnedPath(worktreePath); err != nil {
				lastErr = err
			}
		}
		pruneErr := runBoundedGit(ctx, managedPreparationArgs(gitBin, "--git-dir", gitDir, "worktree", "prune", "--expire", "now")...)
		if pruneErr != nil {
			lastErr = pruneErr
		}
		if _, err := os.Lstat(worktreePath); err == nil {
			lastErr = NewDeepReviewError("worktree path still exists after cleanup: %s", worktreePath)
		} else if !os.IsNotExist(err) {
			lastErr = err
		} else if registered, regErr := worktreeRegistrationExists(ctx, gitDir, gitBin, worktreePath); regErr != nil {
			lastErr = regErr
		} else if registered {
			lastErr = NewDeepReviewError("worktree registration still exists after cleanup: %s", worktreePath)
		} else if missing || lastErr == nil || pruneErr == nil {
			return nil
		}
		if !waitForCleanupRetry(ctx) {
			return cleanupFailure(worktreePath, lastErr, ctx.Err())
		}
	}
}

func cleanupFailure(worktreePath string, lastErr, ctxErr error) error {
	residual := NewDeepReviewError("worktree cleanup failed for %s", worktreePath)
	if ctxErr != nil {
		residual = NewDeepReviewError("worktree cleanup timed out for %s", worktreePath)
	}
	return errors.Join(residual, lastErr)
}

func waitForCleanupRetry(ctx context.Context) bool {
	timer := time.NewTimer(100 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func runBoundedGit(ctx context.Context, command ...string) error {
	timeout := worktreeGitCommandTimeout
	if deadline, ok := ctx.Deadline(); ok {
		remaining := time.Until(deadline)
		if remaining < timeout {
			timeout = remaining
		}
	}
	if timeout <= 0 {
		return NewDeepReviewError("worktree cleanup timed out: %s", strings.Join(command, " "))
	}
	_, err := RunCommandContext(ctx, command, "", "", true, timeout)
	return err
}

func worktreeRegistrationExists(ctx context.Context, gitDir, gitBin, worktreePath string) (bool, error) {
	timeout := worktreeGitCommandTimeout
	if deadline, ok := ctx.Deadline(); ok {
		remaining := time.Until(deadline)
		if remaining < timeout {
			timeout = remaining
		}
	}
	if timeout <= 0 {
		return false, NewDeepReviewError("worktree cleanup timed out before registration check: %s", worktreePath)
	}
	completed, err := RunCommandContext(ctx, managedPreparationArgs(gitBin, "--git-dir", gitDir, "worktree", "list", "--porcelain"), "", "", true, timeout)
	if err != nil {
		return false, err
	}
	want := filepath.Clean(worktreePath)
	for _, line := range strings.Split(completed.Stdout, "\n") {
		if !strings.HasPrefix(line, "worktree ") {
			continue
		}
		if samePathAfterSymlinks(strings.TrimPrefix(line, "worktree "), want) {
			return true, nil
		}
	}
	return false, nil
}

func refuseUnsafeCleanupPath(repoPath, worktreePath string) error {
	if worktreePath == "" || worktreePath == "." || worktreePath == string(filepath.Separator) {
		return &worktreeNotOwnedError{message: "refusing to clean an unsafe worktree path"}
	}
	if repoPath == "" || repoPath == "." || repoPath == string(filepath.Separator) {
		return &worktreeNotOwnedError{message: "refusing to clean a worktree without a repository path"}
	}
	if worktreePath == repoPath || pathIsWithinDir(worktreePath, repoPath) || pathIsWithinDir(repoPath, worktreePath) {
		return &worktreeNotOwnedError{message: "refusing to clean the repository checkout: " + worktreePath}
	}
	return nil
}

func repositoryGitDirectory(repoPath string) (string, error) {
	gitPath := filepath.Join(repoPath, ".git")
	info, err := os.Lstat(gitPath)
	if err != nil {
		return "", err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return "", &worktreeNotOwnedError{message: "refusing to follow a symlink .git entry: " + gitPath}
	}
	if info.IsDir() {
		return gitPath, nil
	}
	if !info.Mode().IsRegular() {
		return "", &worktreeNotOwnedError{message: "unsupported .git entry: " + gitPath}
	}
	raw, err := os.ReadFile(gitPath)
	if err != nil {
		return "", err
	}
	pointer, err := parseGitdirPointer(string(raw))
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(pointer) {
		pointer = filepath.Join(repoPath, pointer)
	}
	return filepath.Clean(pointer), nil
}

func assertOwnedLinkedWorktree(repoPath, worktreePath string) error {
	if err := refuseUnsafeCleanupPath(repoPath, worktreePath); err != nil {
		return err
	}
	repoGit, err := repositoryGitDirectory(repoPath)
	if err != nil {
		return err
	}
	meta, err := linkedWorktreeMetadataDir(worktreePath)
	if err != nil {
		return err
	}
	worktreesRoot := filepath.Join(repoGit, "worktrees")
	resolvedMeta, err := filepath.EvalSymlinks(meta)
	if err != nil {
		return err
	}
	resolvedRoot, err := filepath.EvalSymlinks(worktreesRoot)
	if err != nil {
		return err
	}
	if !pathIsWithinDir(resolvedRoot, resolvedMeta) {
		return &worktreeNotOwnedError{message: "worktree gitdir resolves outside the repository worktrees directory: " + worktreePath}
	}
	backRaw, err := os.ReadFile(filepath.Join(meta, "gitdir"))
	if err != nil {
		return &worktreeNotOwnedError{message: "worktree metadata is missing its gitdir back-pointer: " + worktreePath}
	}
	backPath := strings.TrimSpace(string(backRaw))
	if !filepath.IsAbs(backPath) {
		backPath = filepath.Join(meta, backPath)
	}
	if !samePathAfterSymlinks(backPath, filepath.Join(worktreePath, ".git")) {
		return &worktreeNotOwnedError{message: "worktree gitdir does not point back at the worktree: " + worktreePath}
	}
	commonRaw, err := os.ReadFile(filepath.Join(meta, "commondir"))
	if err != nil {
		return &worktreeNotOwnedError{message: "worktree metadata is missing commondir: " + worktreePath}
	}
	common := strings.TrimSpace(string(commonRaw))
	if !filepath.IsAbs(common) {
		common = filepath.Join(meta, common)
	}
	common = filepath.Clean(common)
	if common != filepath.Clean(repoGit) {
		resolvedCommon, commonErr := filepath.EvalSymlinks(common)
		resolvedRepo, repoErr := filepath.EvalSymlinks(repoGit)
		if commonErr != nil || repoErr != nil || resolvedCommon != resolvedRepo {
			return &worktreeNotOwnedError{message: "worktree commondir does not match the repository: " + worktreePath}
		}
	}
	return nil
}

func linkedWorktreeMetadataDir(worktreePath string) (string, error) {
	info, err := os.Lstat(worktreePath)
	if err != nil {
		return "", err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return "", &worktreeNotOwnedError{message: "refusing to clean a path that is not a worktree directory: " + worktreePath}
	}
	gitFile := filepath.Join(worktreePath, ".git")
	gitInfo, err := os.Lstat(gitFile)
	if err != nil {
		return "", &worktreeNotOwnedError{message: "path is not a linked worktree: " + worktreePath}
	}
	if gitInfo.Mode()&os.ModeSymlink != 0 || !gitInfo.Mode().IsRegular() {
		return "", &worktreeNotOwnedError{message: "refusing to clean a worktree whose .git entry is not a regular file: " + worktreePath}
	}
	raw, err := os.ReadFile(gitFile)
	if err != nil {
		return "", err
	}
	pointer, err := parseGitdirPointer(string(raw))
	if err != nil {
		return "", &worktreeNotOwnedError{message: "path is not a linked worktree: " + worktreePath}
	}
	if !filepath.IsAbs(pointer) {
		pointer = filepath.Join(worktreePath, pointer)
	}
	return filepath.Clean(pointer), nil
}

func parseGitdirPointer(text string) (string, error) {
	line := strings.TrimSpace(text)
	if index := strings.IndexByte(line, '\n'); index >= 0 {
		line = strings.TrimSpace(line[:index])
	}
	const prefix = "gitdir:"
	if !strings.HasPrefix(line, prefix) {
		return "", NewDeepReviewError("missing gitdir pointer")
	}
	pointer := strings.TrimSpace(strings.TrimPrefix(line, prefix))
	if pointer == "" {
		return "", NewDeepReviewError("empty gitdir pointer")
	}
	return pointer, nil
}

func discoverWorktreeRepository(worktreePath string) (string, error) {
	meta, err := linkedWorktreeMetadataDir(worktreePath)
	if err != nil {
		return "", err
	}
	worktreesDir := filepath.Dir(meta)
	if filepath.Base(worktreesDir) != "worktrees" {
		return "", &worktreeNotOwnedError{message: "worktree gitdir is not inside a worktrees directory: " + worktreePath}
	}
	gitDir := filepath.Dir(worktreesDir)
	if filepath.Base(gitDir) != ".git" {
		return "", &worktreeNotOwnedError{message: "worktree gitdir is not inside a repository .git directory: " + worktreePath}
	}
	repoPath := filepath.Dir(gitDir)
	if err := assertOwnedLinkedWorktree(repoPath, worktreePath); err != nil {
		return "", err
	}
	return repoPath, nil
}

func samePathAfterSymlinks(left, right string) bool {
	left = filepath.Clean(left)
	right = filepath.Clean(right)
	if left == right {
		return true
	}
	resolvedLeft, leftErr := resolvePathWithParentSymlinks(left)
	resolvedRight, rightErr := resolvePathWithParentSymlinks(right)
	return leftErr == nil && rightErr == nil && resolvedLeft == resolvedRight
}

func resolvePathWithParentSymlinks(path string) (string, error) {
	resolved, err := filepath.EvalSymlinks(path)
	if err == nil {
		return resolved, nil
	}
	parent := filepath.Dir(path)
	if parent == path {
		return "", err
	}
	resolvedParent, parentErr := filepath.EvalSymlinks(parent)
	if parentErr != nil {
		return "", err
	}
	return filepath.Join(resolvedParent, filepath.Base(path)), nil
}

func pathIsWithinDir(root, target string) bool {
	root = filepath.Clean(root)
	target = filepath.Clean(target)
	if root == "" || target == "" {
		return false
	}
	rel, err := filepath.Rel(root, target)
	if err != nil || rel == "." || rel == ".." {
		return false
	}
	return !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func forceRemoveOwnedPath(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return NewDeepReviewError("refusing to remove symlink worktree path: %s", path)
	}
	_ = makeTreeWritable(path)
	return os.RemoveAll(path)
}

func makeTreeWritable(root string) error {
	info, err := os.Lstat(root)
	if err != nil || info.Mode()&os.ModeSymlink != 0 {
		return err
	}
	if err := os.Chmod(root, info.Mode().Perm()|0o700); err != nil {
		return err
	}
	return filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil || path == root {
			return walkErr
		}
		entryInfo, err := entry.Info()
		if err != nil {
			return err
		}
		if entryInfo.Mode()&os.ModeSymlink != 0 {
			return nil
		}
		perm := entryInfo.Mode().Perm() | 0o200
		if entry.IsDir() {
			perm |= 0o700
		}
		return os.Chmod(path, perm)
	})
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

func PublicationRefspec(gitBin, commitSHA, branch string) (string, error) {
	sha := strings.ToLower(strings.TrimSpace(commitSHA))
	if !fullGitSHARe.MatchString(sha) {
		return "", NewDeepReviewError("publication commit must be a full 40-character SHA")
	}
	branch = strings.TrimSpace(branch)
	if branch == "" || strings.HasPrefix(branch, "-") || strings.Contains(branch, ":") {
		return "", NewDeepReviewError("publication branch is not a valid ref")
	}
	ref := "refs/heads/" + branch
	if _, err := RunCommand([]string{gitBin, "check-ref-format", ref}, "", "", true, 0); err != nil {
		return "", NewDeepReviewError("publication branch is not a valid ref")
	}
	return sha + ":" + ref, nil
}

func PushVerifiedRef(repoPath, gitBin, commitSHA, branch string) (string, error) {
	sha := strings.ToLower(strings.TrimSpace(commitSHA))
	refspec, err := PublicationRefspec(gitBin, sha, branch)
	if err != nil {
		return "", err
	}
	if err := PushRefspec(repoPath, gitBin, refspec); err != nil {
		return "", err
	}
	if err := requireRemoteBranchSHA(repoPath, gitBin, branch, sha); err != nil {
		return "", err
	}
	return refspec, nil
}

func requireRemoteBranchSHA(repoPath, gitBin, branch, want string) error {
	got, err := remoteBranchSHA(repoPath, gitBin, branch)
	if err != nil {
		return err
	}
	if got != strings.ToLower(strings.TrimSpace(want)) {
		return NewDeepReviewError("remote ref %s does not match the reviewed commit after publication", "refs/heads/"+branch)
	}
	return nil
}

func remoteBranchSHA(repoPath, gitBin, branch string) (string, error) {
	ref := "refs/heads/" + branch
	completed, err := RunCommand([]string{gitBin, "-C", repoPath, "ls-remote", "--exit-code", "origin", ref}, "", "", true, 0)
	if err != nil {
		return "", NewDeepReviewError("publication could not confirm remote ref %s", ref)
	}
	for _, line := range strings.Split(completed.Stdout, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[1] == ref {
			return strings.ToLower(fields[0]), nil
		}
	}
	return "", NewDeepReviewError("publication could not confirm remote ref %s", ref)
}

func DryRunPushRefspec(repoPath, gitBin, refspec string) error {
	_, err := RunCommand([]string{gitBin, "-C", repoPath, "push", "--dry-run", "origin", refspec}, "", "", true, 0)
	return err
}
