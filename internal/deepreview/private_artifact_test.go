package deepreview

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func withUmask022(t *testing.T) {
	t.Helper()
	old := syscall.Umask(0o022)
	t.Cleanup(func() { syscall.Umask(old) })
}

func assertPerm(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", filepath.Base(path), err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("%s is a symlink", filepath.Base(path))
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("%s mode = %o, want %o", filepath.Base(path), got, want)
	}
}

func TestPrivateArtifactsIgnoreUmaskAndRejectSymlinks(t *testing.T) {
	withUmask022(t)
	root := t.TempDir()

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(cwd)
	if err != nil {
		t.Fatal(err)
	}
	if err := mkdirPrivate("."); err == nil {
		t.Fatal("expected current directory to be refused")
	}
	after, err := os.Stat(cwd)
	if err != nil {
		t.Fatal(err)
	}
	if before.Mode().Perm() != after.Mode().Perm() {
		t.Fatal("refused private directory changed the current directory mode")
	}

	existingDir := filepath.Join(root, "existing")
	if err := os.Mkdir(existingDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := mkdirPrivate(existingDir); err != nil {
		t.Fatal(err)
	}
	assertPerm(t, existingDir, privateDirMode)

	freshDir := filepath.Join(root, "fresh")
	if err := mkdirPrivate(freshDir); err != nil {
		t.Fatal(err)
	}
	assertPerm(t, freshDir, privateDirMode)

	linkDir := filepath.Join(root, "link-dir")
	if err := os.Symlink(existingDir, linkDir); err != nil {
		t.Fatal(err)
	}
	if err := mkdirPrivate(linkDir); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("symlink directory error = %v", err)
	}

	existingFile := filepath.Join(freshDir, "note.txt")
	if err := os.WriteFile(existingFile, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writePrivateFile(existingFile, []byte("new")); err != nil {
		t.Fatal(err)
	}
	assertPerm(t, existingFile, privateFileMode)
	body, err := os.ReadFile(existingFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "new" {
		t.Fatalf("file body = %q", body)
	}

	target := filepath.Join(freshDir, "target.txt")
	if err := os.WriteFile(target, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	linkFile := filepath.Join(freshDir, "link-file")
	if err := os.Symlink(target, linkFile); err != nil {
		t.Fatal(err)
	}
	if err := writePrivateFile(linkFile, []byte("changed")); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("symlink file error = %v", err)
	}
	unchanged, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(unchanged) != "original" {
		t.Fatalf("symlink write changed the target: %q", unchanged)
	}
}

func TestRunArtifactWritersUsePrivateModes(t *testing.T) {
	withUmask022(t)
	root := t.TempDir()
	loose := filepath.Join(root, "locks", "github", "example", "repo")
	if err := os.MkdirAll(loose, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(loose, 0o755); err != nil {
		t.Fatal(err)
	}

	orchestrator := &Orchestrator{
		workspaceRoot: root,
		runRoot:       filepath.Join(root, "runs", "run-1"),
		repoIdentity:  RepoIdentity{SourceType: RepoSourceGitHub, Owner: "example", Name: "repo"},
		config: ReviewConfig{
			RunID:         "run-1",
			SourceBranch:  "feature/a",
			Mode:          ModePR,
			RetainRawLogs: true,
		},
	}
	if err := orchestrator.acquireRunLock(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(orchestrator.releaseRunLock)
	lockPath := orchestrator.runLockFilePath()
	assertPerm(t, filepath.Dir(lockPath), privateDirMode)
	assertPerm(t, lockPath, privateFileMode)
	assertPerm(t, lockPath+".guard", privateFileMode)

	if err := orchestrator.writeRunConfig(); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(orchestrator.runRoot, "run-config.json")
	assertPerm(t, configPath, privateFileMode)
	configBody, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(configBody), `"retain_raw_logs": true`) {
		t.Fatalf("run config = %s", configBody)
	}

	recordPath := filepath.Join(orchestrator.runRoot, "round-01", "round.json")
	if err := writeRoundRecord(recordPath, RoundRecord{
		Round:   1,
		Summary: "round-summary.md",
		Status:  RoundStatus{Decision: "stop", Reason: "done"},
	}); err != nil {
		t.Fatal(err)
	}
	assertPerm(t, recordPath, privateFileMode)

	base := t.TempDir()
	artifactDir := filepath.Join(base, "artifacts")
	if err := os.MkdirAll(artifactDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(artifactDir, "keep.txt"), []byte("kept\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	snapshot, err := snapshotPaths(base, []string{artifactDir})
	if err != nil {
		t.Fatal(err)
	}
	restored := t.TempDir()
	if err := restoreSnapshot(restored, snapshot); err != nil {
		t.Fatal(err)
	}
	assertPerm(t, filepath.Join(restored, "artifacts"), privateDirMode)
	assertPerm(t, filepath.Join(restored, "artifacts", "keep.txt"), privateFileMode)
}

func TestWriteRunHealthArtifactsShareSanitizedContract(t *testing.T) {
	withUmask022(t)
	root := t.TempDir()
	email := sensitiveEmailFixture()
	privatePath := strings.Join([]string{"", "home", "alice", "run.log"}, "/")
	report := runHealthReport{
		RunID:       "run-1",
		Mode:        ModePR,
		GeneratedAt: "2026-11-06T00:00:00Z",
		FinalStatus: &RoundStatus{
			Decision: "stop",
			Reason:   "see " + email,
		},
		Stderr: runHealthStderrSummary{
			NonEmptyFiles: 1,
			NonEmptyLogSet: []runHealthStderrFile{{
				Path:  privatePath,
				Bytes: 4,
				Lines: 1,
			}},
		},
	}
	if err := writeRunHealthArtifacts(root, report); err != nil {
		t.Fatal(err)
	}
	jsonPath := filepath.Join(root, runHealthJSONName)
	mdPath := filepath.Join(root, runHealthMDName)
	assertPerm(t, jsonPath, privateFileMode)
	assertPerm(t, mdPath, privateFileMode)
	jsonBody, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatal(err)
	}
	mdBody, err := os.ReadFile(mdPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{string(jsonBody), string(mdBody)} {
		if strings.Contains(body, email) || strings.Contains(body, privatePath) {
			t.Fatal("run health export kept sensitive text")
		}
		if !strings.Contains(body, "[redacted-email]") || !strings.Contains(body, "[redacted-path]") {
			t.Fatal("run health export missing redacted markers")
		}
		if !strings.Contains(body, "see [redacted-email]") {
			t.Fatal("run health export reason mismatch")
		}
	}
}
