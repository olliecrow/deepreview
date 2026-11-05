//go:build darwin || linux

package deepreview

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestReadFileFromRootRejectsSymlinkFile(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("outside-secret\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "secret.txt"), filepath.Join(root, "report.md")); err != nil {
		t.Fatal(err)
	}
	if _, err := readFileFromRoot(root, "report.md"); err == nil {
		t.Fatal("expected symlink file to be rejected")
	}
}

func TestReadFileFromRootRejectsSymlinkDirectory(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("outside-secret\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "linked")); err != nil {
		t.Fatal(err)
	}
	if _, err := readFileFromRoot(root, filepath.Join("linked", "secret.txt")); err == nil {
		t.Fatal("expected symlink directory to be rejected")
	}
}

func TestReadFileFromRootRejectsFIFOWithoutBlocking(t *testing.T) {
	root := t.TempDir()
	if err := syscall.Mkfifo(filepath.Join(root, "pipe"), 0o644); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := readFileFromRoot(root, "pipe")
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected fifo to be rejected")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("fifo read blocked")
	}
}

func TestReadFileFromRootRejectsOversizeFile(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "big.md")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(path, maxArtifactBytes+1); err != nil {
		t.Fatal(err)
	}
	if _, err := readFileFromRoot(root, "big.md"); err == nil {
		t.Fatal("expected oversize artifact to be rejected")
	}
}

func TestCopyBoundedArtifactReplacesSymlinkWithoutFollowingIt(t *testing.T) {
	srcRoot := t.TempDir()
	dstRoot := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(srcRoot, "report.md"), []byte("safe\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	outsidePath := filepath.Join(outside, "target.md")
	if err := os.WriteFile(outsidePath, []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outsidePath, filepath.Join(dstRoot, "report.md")); err != nil {
		t.Fatal(err)
	}
	if err := copyBoundedArtifact(dstRoot, "report.md", srcRoot, "report.md"); err != nil {
		t.Fatal(err)
	}
	got, err := readFileFromRoot(dstRoot, "report.md")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "safe\n" {
		t.Fatalf("canonical artifact = %q", string(got))
	}
	outsideBody, err := os.ReadFile(outsidePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(outsideBody) != "old\n" {
		t.Fatalf("symlink target changed: %q", string(outsideBody))
	}
}

func TestReadFileFromRootRejectsParentTraversal(t *testing.T) {
	root := t.TempDir()
	if _, err := readFileFromRoot(root, filepath.Join("..", "secret.md")); err == nil {
		t.Fatal("expected traversal path to be rejected")
	}
}

func TestNewOrchestratorRejectsUnsafeRunID(t *testing.T) {
	for _, id := range []string{"", ".", "..", "../escape", "a/b", "a..b"} {
		_, err := NewOrchestrator(ReviewConfig{
			RunID:         id,
			WorkspaceRoot: t.TempDir(),
			Repo:          t.TempDir(),
		}, nil)
		if err == nil || !strings.Contains(err.Error(), "run id") {
			t.Fatalf("run id %q returned %v", id, err)
		}
	}
}

func TestReadRoundRecordRejectsNonBasenameSummary(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "round.json")
	body := []byte("{\"round\":1,\"summary\":\"../secret.md\",\"status\":{\"decision\":\"stop\",\"reason\":\"ok\"}}\n")
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := readRoundRecord(path); err == nil {
		t.Fatal("expected traversal summary to be rejected")
	}
}

func TestReadQueueRejectsEscapeEntry(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "queue.txt")
	if err := os.WriteFile(path, []byte("../secret.md\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadQueue(path); err == nil {
		t.Fatal("expected queue escape to be rejected")
	}
}

func TestSnapshotPathsStayUnderRequestedParent(t *testing.T) {
	base := t.TempDir()
	parent := filepath.Join(base, "tmp")
	kept := filepath.Join(base, "keep.txt")
	if err := os.WriteFile(kept, []byte("kept\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	snapshotDir, err := snapshotPaths(base, []string{kept}, parent)
	if err != nil {
		t.Fatal(err)
	}
	rel, err := filepath.Rel(parent, snapshotDir)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		t.Fatalf("snapshot %s escaped parent %s", snapshotDir, parent)
	}
	if _, err := os.Stat(filepath.Join(snapshotDir, "keep.txt")); err != nil {
		t.Fatal(err)
	}
}
