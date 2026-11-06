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

func TestSnapshotRoundTripsPreservedTree(t *testing.T) {
	src := t.TempDir()
	if err := os.MkdirAll(filepath.Join(src, ".deepreview", "inputs", "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		".deepreview/inputs/review-01.md":     "one\n",
		".deepreview/inputs/nested/notes.txt": "two\n",
		".deepreview/plan.md":                 "plan\n",
	}
	for rel, body := range files {
		if err := os.WriteFile(filepath.Join(src, rel), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	snapshot, err := snapshotPaths(src, []string{
		filepath.Join(src, ".deepreview", "inputs"),
		filepath.Join(src, ".deepreview", "plan.md"),
		filepath.Join(src, ".deepreview", "missing.md"),
	})
	if err != nil {
		t.Fatal(err)
	}
	dst := t.TempDir()
	if err := restoreSnapshot(dst, snapshot); err != nil {
		t.Fatal(err)
	}
	for rel, want := range files {
		got, err := os.ReadFile(filepath.Join(dst, rel))
		if err != nil || string(got) != want {
			t.Fatalf("%s = %q, %v; want %q", rel, got, err, want)
		}
	}
}

func TestSnapshotRejectsSymlinkedParent(t *testing.T) {
	root := t.TempDir()
	base := filepath.Join(root, "work")
	outside := filepath.Join(root, "outside")
	for _, dir := range []string{base, outside} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(outside, "review.md"), []byte("outside\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(base, ".deepreview")); err != nil {
		t.Fatal(err)
	}
	if _, err := snapshotPaths(base, []string{filepath.Join(base, ".deepreview", "review.md")}); err == nil {
		t.Fatal("expected a symlinked parent to be rejected")
	}
	if err := os.Symlink(filepath.Join(outside, "review.md"), filepath.Join(base, "link.md")); err != nil {
		t.Fatal(err)
	}
	if _, err := snapshotPaths(base, []string{filepath.Join(base, "link.md")}); err == nil {
		t.Fatal("expected a symlinked file to be rejected")
	}
}

func TestSnapshotRejectsOversizedAndTooMuchContent(t *testing.T) {
	base := t.TempDir()
	inputs := filepath.Join(base, "inputs")
	if err := os.MkdirAll(inputs, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(inputs, "big.md"), make([]byte, maxArtifactBytes+1), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := snapshotPaths(base, []string{inputs}); err == nil {
		t.Fatal("expected a nested file over the artifact limit to be rejected")
	}
	if err := os.Remove(filepath.Join(inputs, "big.md")); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"a.md", "b.md", "c.md"} {
		if err := os.WriteFile(filepath.Join(inputs, name), make([]byte, 40), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	previousBytes, previousEntries := maxSnapshotBytes, maxSnapshotEntries
	t.Cleanup(func() { maxSnapshotBytes, maxSnapshotEntries = previousBytes, previousEntries })
	maxSnapshotBytes = 100
	if _, err := snapshotPaths(base, []string{inputs}); err == nil {
		t.Fatal("expected the total byte limit to be enforced")
	}
	maxSnapshotBytes, maxSnapshotEntries = previousBytes, 3
	if _, err := snapshotPaths(base, []string{inputs}); err == nil {
		t.Fatal("expected the entry limit to be enforced")
	}
}

func TestSnapshotRejectsFIFOWithoutBlocking(t *testing.T) {
	base := t.TempDir()
	inputs := filepath.Join(base, "inputs")
	if err := os.MkdirAll(inputs, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(filepath.Join(inputs, "pipe"), 0o644); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := snapshotPaths(base, []string{inputs})
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected a FIFO to be rejected")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("snapshot blocked on a FIFO")
	}
}

func TestRestoreSnapshotRejectsSymlinkedDestination(t *testing.T) {
	root := t.TempDir()
	dst := filepath.Join(root, "dst")
	outside := filepath.Join(root, "outside")
	for _, dir := range []string{dst, outside} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(outside, filepath.Join(dst, ".deepreview")); err != nil {
		t.Fatal(err)
	}
	snapshot := []snapshotEntry{{rel: filepath.Join(".deepreview", "plan.md"), body: []byte("plan\n")}}
	if err := restoreSnapshot(dst, snapshot); err == nil {
		t.Fatal("expected restore through a symlinked parent to fail")
	}
	assertMarkerAbsent(t, filepath.Join(outside, "plan.md"))
}

func TestReadRoundRecordRejectsSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "elsewhere.json")
	body := []byte("{\"round\":1,\"summary\":\"round-summary.md\",\"status\":{\"decision\":\"stop\",\"reason\":\"ok\"}}\n")
	if err := os.WriteFile(target, body, 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "round.json")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if _, err := readRoundRecord(link); err == nil {
		t.Fatal("expected a symlinked round record to be rejected")
	}
}
