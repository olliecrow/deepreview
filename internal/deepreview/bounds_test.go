package deepreview

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRunCommandFailsWhenRetainedOutputExceedsCap(t *testing.T) {
	previous := maxRetainedCommandOutput
	maxRetainedCommandOutput = 1024
	t.Cleanup(func() { maxRetainedCommandOutput = previous })
	completed, err := RunCommand([]string{"sh", "-c", "head -c 4096 /dev/zero"}, "", "", false, 10*time.Second)
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("expected an output cap error, got %v", err)
	}
	if len(completed.Stdout) != 0 {
		t.Fatalf("truncated output was returned: %d bytes", len(completed.Stdout))
	}
	completed, err = RunCommand([]string{"sh", "-c", "head -c 512 /dev/zero"}, "", "", true, 10*time.Second)
	if err != nil || len(completed.Stdout) != 512 {
		t.Fatalf("output under the cap: %d bytes, %v", len(completed.Stdout), err)
	}
}

func TestRunCommandAppliesDefaultTimeout(t *testing.T) {
	previous := defaultCommandTimeout
	defaultCommandTimeout = 200 * time.Millisecond
	t.Cleanup(func() { defaultCommandTimeout = previous })
	started := time.Now()
	_, err := RunCommand([]string{"sleep", "10"}, "", "", true, 0)
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("expected a default timeout, got %v", err)
	}
	if elapsed := time.Since(started); elapsed > 5*time.Second {
		t.Fatalf("default timeout took %s", elapsed)
	}
}

func TestValidateMergeReadyPRBoundsAStuckGhCall(t *testing.T) {
	previous := mergeReadyPRValidationWindow
	mergeReadyPRValidationWindow = 300 * time.Millisecond
	t.Cleanup(func() { mergeReadyPRValidationWindow = previous })
	gh := filepath.Join(t.TempDir(), "gh")
	writeExecutable(t, gh, "#!/bin/sh\nexec sleep 30\n")
	o := &Orchestrator{
		config:          ReviewConfig{GhBin: gh},
		repoIdentity:    RepoIdentity{SourceType: RepoSourceGitHub, Owner: "example", Name: "repo"},
		managedRepoPath: t.TempDir(),
		reporter:        &NullProgressReporter{},
	}
	started := time.Now()
	err := o.validateMergeReadyPR("https://github.com/example/repo/pull/1")
	if err == nil {
		t.Fatal("expected a stuck gh pr view to fail")
	}
	if elapsed := time.Since(started); elapsed > 10*time.Second {
		t.Fatalf("mergeability check took %s", elapsed)
	}
}

func TestRemoveTreeContextStopsWhenCanceledAndRemovesReadOnlyTrees(t *testing.T) {
	root := filepath.Join(t.TempDir(), "tree")
	if err := os.MkdirAll(filepath.Join(root, "a", "b"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "a", "b", "f.txt"), []byte("x"), 0o444); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(root, "a", "b"), 0o500); err != nil {
		t.Fatal(err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := removeTreeContext(canceled, root); err == nil {
		t.Fatal("expected a canceled context to stop removal")
	}
	if _, err := os.Stat(filepath.Join(root, "a", "b", "f.txt")); err != nil {
		t.Fatalf("canceled removal deleted content: %v", err)
	}
	if err := removeTreeContext(context.Background(), root); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(root); !os.IsNotExist(err) {
		t.Fatalf("tree still exists: %v", err)
	}
}
