package deepreview

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type untrustedCheckoutProbe struct {
	repo    string
	markers []string
}

// newUntrustedCheckoutProbe returns a synced checkout whose repository config
// runs a marker script from fsmonitor, a clean filter, a hook, and the
// upload-pack command when Git honours that config.
func newUntrustedCheckoutProbe(t *testing.T) untrustedCheckoutProbe {
	t.Helper()
	repo := createSyncedGitHubLikeRepo(t, "feature/test")
	if err := os.WriteFile(filepath.Join(repo, ".gitattributes"), []byte("*.dat filter=probe\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "data.dat"), []byte("payload\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitCommand(t, repo, "add", ".gitattributes", "data.dat")
	runGitCommand(t, repo, "commit", "-m", "add filtered file")
	runGitCommand(t, repo, "push", "origin", "feature/test")

	probeDir := t.TempDir()
	hooks := filepath.Join(probeDir, "hooks")
	if err := os.MkdirAll(hooks, 0o755); err != nil {
		t.Fatal(err)
	}
	marker := func(name string) string { return filepath.Join(probeDir, name+"-marker") }
	writeExecutable(t, filepath.Join(probeDir, "fsmonitor.sh"), "#!/bin/sh\ntouch '"+marker("fsmonitor")+"'\nexit 1\n")
	writeExecutable(t, filepath.Join(probeDir, "clean.sh"), "#!/bin/sh\ntouch '"+marker("filter")+"'\ncat\n")
	writeExecutable(t, filepath.Join(hooks, "post-index-change"), "#!/bin/sh\ntouch '"+marker("hook")+"'\n")
	writeExecutable(t, filepath.Join(probeDir, "upload-pack.sh"), "#!/bin/sh\ntouch '"+marker("upload-pack")+"'\nexec git upload-pack \"$@\"\n")
	runGitCommand(t, repo, "config", "core.fsmonitor", filepath.Join(probeDir, "fsmonitor.sh"))
	runGitCommand(t, repo, "config", "filter.probe.clean", filepath.Join(probeDir, "clean.sh"))
	runGitCommand(t, repo, "config", "filter.probe.required", "true")
	runGitCommand(t, repo, "config", "core.hooksPath", hooks)
	runGitCommand(t, repo, "config", "remote.origin.uploadpack", filepath.Join(probeDir, "upload-pack.sh"))

	// A new mtime makes the index entry stat-dirty, so a refresh must hash the
	// file through its clean filter.
	later := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(filepath.Join(repo, "data.dat"), later, later); err != nil {
		t.Fatal(err)
	}
	return untrustedCheckoutProbe{
		repo:    repo,
		markers: []string{marker("fsmonitor"), marker("filter"), marker("hook"), marker("upload-pack")},
	}
}

func (p untrustedCheckoutProbe) assertNothingRan(t *testing.T) {
	t.Helper()
	for _, path := range p.markers {
		assertMarkerAbsent(t, path)
	}
}

func TestUntrustedCheckoutProbeRunsUnderPlainGit(t *testing.T) {
	probe := newUntrustedCheckoutProbe(t)
	cmd := exec.Command("git", "-C", probe.repo, "status", "--porcelain")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git status: %v\n%s", err, out)
	}
	cmd = exec.Command("git", "-C", probe.repo, "ls-remote", "origin")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git ls-remote: %v\n%s", err, out)
	}
	for _, path := range probe.markers {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("probe %s did not run under plain git: %v", filepath.Base(path), err)
		}
	}
}

func TestPreTrustCommandsRunNoRepositoryCode(t *testing.T) {
	toolDir := t.TempDir()
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Fatalf("resolve git: %v", err)
	}
	t.Setenv("PATH", toolDir+string(os.PathListSeparator)+filepath.Dir(gitPath))
	t.Setenv("DEEPREVIEW_GIT_BIN", gitPath)
	writeFakeMulticodexTool(t, toolDir)
	t.Setenv("DEEPREVIEW_GH_BIN", writeFakeTool(t, toolDir, "gh"))

	commands := map[string]struct {
		run  func(repo string) int
		want string
	}{
		"review": {
			run: func(repo string) int {
				return runReviewCommand([]string{repo, "--source-branch", "feature/test", "--no-tui"})
			},
			want: "--trust-repo",
		},
		"doctor": {
			run: func(repo string) int {
				return runDoctorCommand([]string{repo, "--source-branch", "feature/test"})
			},
			want: "[ok] local source branch ready for review",
		},
		"dry-run": {
			run: func(repo string) int {
				return runDryRunCommand([]string{repo, "--source-branch", "feature/test"})
			},
			want: "planned order:",
		},
		"inferred dry-run": {
			run:  func(string) int { return runDryRunCommand([]string{}) },
			want: "planned order:",
		},
	}
	for name, command := range commands {
		t.Run(name, func(t *testing.T) {
			probe := newUntrustedCheckoutProbe(t)
			t.Setenv("DEEPREVIEW_WORKSPACE_ROOT", t.TempDir())
			withWorkingDir(t, probe.repo, func() {
				_, stdout, stderr := captureCommandOutput(t, func() int { return command.run(probe.repo) })
				if !strings.Contains(stdout+stderr, command.want) {
					t.Fatalf("expected %q in output\nstdout:\n%s\nstderr:\n%s", command.want, stdout, stderr)
				}
			})
			probe.assertNothingRan(t)
		})
	}
}

func TestReviewRequiresTrustBeforeReadiness(t *testing.T) {
	repo := createSyncedFilesystemRepo(t, "feature/test")
	t.Setenv("DEEPREVIEW_WORKSPACE_ROOT", t.TempDir())
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("dirty\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	withWorkingDir(t, t.TempDir(), func() {
		code, _, stderr := captureCommandOutput(t, func() int {
			return runReviewCommand([]string{repo, "--source-branch", "feature/test", "--no-tui"})
		})
		if code != 1 || !strings.Contains(stderr, "--trust-repo") || strings.Contains(stderr, "local tracked changes") {
			t.Fatalf("expected trust refusal before readiness, code=%d stderr=\n%s", code, stderr)
		}
	})
}

func TestInspectionReadinessStillDetectsChanges(t *testing.T) {
	probe := newUntrustedCheckoutProbe(t)
	state, err := detectLocalRepoState("git", probe.repo)
	if err != nil || state == nil {
		t.Fatalf("detect state: %v %+v", err, state)
	}
	if err := ensureBranchReadyForRemoteReview("git", state, "feature/test"); err != nil {
		t.Fatalf("expected synced checkout to be ready: %v", err)
	}
	if err := os.WriteFile(filepath.Join(probe.repo, "README.md"), []byte("dirty\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err = ensureBranchReadyForRemoteReview("git", state, "feature/test")
	if err == nil || !strings.Contains(err.Error(), "local tracked changes") {
		t.Fatalf("expected tracked-change refusal, got %v", err)
	}
	probe.assertNothingRan(t)
}

func TestInspectionRejectsUnsafeFilterDriverName(t *testing.T) {
	repo := createSyncedFilesystemRepo(t, "feature/test")
	runGitCommand(t, repo, "config", "filter.a=b.clean", "true")
	if _, err := inspectGit("git", repo, 0, "status", "--porcelain"); err == nil {
		t.Fatal("expected an unsafe filter driver name to fail closed")
	}
}

func TestInspectionRemoteRejectsOptionShapedLocator(t *testing.T) {
	if _, err := inspectRemoteRef("git", "--upload-pack=touch", "refs/heads/main", time.Second); err == nil {
		t.Fatal("expected an option-shaped locator to be rejected")
	}
}

func TestInspectionReadinessResolvesRelativeOriginPath(t *testing.T) {
	repo := createSyncedFilesystemRepo(t, "feature/test")
	remote := strings.TrimSpace(runGitCommand(t, repo, "remote", "get-url", "origin"))
	rel, err := filepath.Rel(repo, remote)
	if err != nil {
		t.Fatal(err)
	}
	runGitCommand(t, repo, "remote", "set-url", "origin", rel)
	state, err := detectLocalRepoState("git", repo)
	if err != nil || state == nil {
		t.Fatalf("detect state: %v %+v", err, state)
	}
	if err := ensureBranchReadyForRemoteReview("git", state, "feature/test"); err != nil {
		t.Fatalf("expected a relative origin path to resolve from the checkout: %v", err)
	}
}
