package deepreview

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRestrictedPolicyEnvironDropsCredentials(t *testing.T) {
	env := restrictedPolicyEnviron([]string{
		"PATH=/usr/bin",
		"HOME=/tmp/home",
		"TMPDIR=/tmp/scratch",
		"USER=operator",
		"LANG=C",
		"LC_ALL=C",
		"SSH_AUTH_SOCK=/tmp/agent.sock",
		"GH_TOKEN=drop-token",
		"AWS_SECRET_ACCESS_KEY=drop-secret",
		"API_KEY=drop-api",
		"MY_PASSWORD=drop-pass",
		"RANDOM_CONFIG=keep-me-not",
	})
	text := strings.Join(env, "\n")
	for _, banned := range []string{
		"drop-token",
		"drop-secret",
		"drop-api",
		"drop-pass",
		"SSH_AUTH_SOCK",
		"RANDOM_CONFIG",
		"keep-me-not",
	} {
		if strings.Contains(text, banned) {
			t.Fatalf("restricted env contains %s:\n%s", banned, text)
		}
	}
	for _, want := range []string{
		"PATH=/usr/bin",
		"HOME=/tmp/home",
		"TMPDIR=/tmp/scratch",
		"USER=operator",
		"LANG=C",
		"LC_ALL=C",
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_TERMINAL_PROMPT=0",
		"credential.helper",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("restricted env missing %s:\n%s", want, text)
		}
	}
}

func TestRunRefusesUntrustedRepositoryExecution(t *testing.T) {
	orchestrator := &Orchestrator{config: ReviewConfig{}}
	err := orchestrator.Run()
	if err == nil {
		t.Fatal("expected review to refuse execution without --trust-repo")
	}
	if !strings.Contains(err.Error(), "--trust-repo") || !strings.Contains(err.Error(), "not a sandbox") {
		t.Fatalf("expected trust refusal, got %v", err)
	}
}

func TestManagedPreparationSkipsInheritedHooksAndFilters(t *testing.T) {
	td := t.TempDir()
	remote := filepath.Join(td, "remote.git")
	seed := filepath.Join(td, "seed")
	managed := filepath.Join(td, "managed")
	hooks := filepath.Join(td, "hooks")
	hookMarker := filepath.Join(td, "hook-marker")
	smudgeMarker := filepath.Join(td, "smudge-marker")
	if err := os.MkdirAll(hooks, 0o755); err != nil {
		t.Fatal(err)
	}
	writeExecutable(t, filepath.Join(hooks, "post-checkout"), "#!/bin/sh\nprintf 'hooked\\n' > '"+hookMarker+"'\n")
	writeExecutable(t, filepath.Join(td, "smudge.sh"), "#!/bin/sh\nprintf 'smudged\\n' > '"+smudgeMarker+"'\ncat\n")
	if err := os.WriteFile(filepath.Join(td, "gitconfig"), []byte(
		"[core]\n\thooksPath = "+hooks+"\n[filter \"lfs\"]\n\tsmudge = "+filepath.Join(td, "smudge.sh")+"\n\tclean = "+filepath.Join(td, "smudge.sh")+"\n\tprocess = "+filepath.Join(td, "smudge.sh")+"\n\trequired = true\n",
	), 0o644); err != nil {
		t.Fatal(err)
	}

	runGitCommand(t, td, "init", "--bare", remote)
	runGitCommand(t, td, "clone", remote, seed)
	runGitCommand(t, seed, "config", "user.email", testPlaceholderEmail("test"))
	runGitCommand(t, seed, "config", "user.name", "Test User")
	runGitCommand(t, seed, "checkout", "-b", "main")
	if err := os.WriteFile(filepath.Join(seed, ".gitattributes"), []byte("*.dat filter=lfs\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(seed, "data.dat"), []byte("payload\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitCommand(t, seed, "-c", "filter.lfs.required=false", "-c", "filter.lfs.smudge=", "-c", "filter.lfs.clean=", "-c", "filter.lfs.process=", "add", ".gitattributes", "data.dat")
	runGitCommand(t, seed, "-c", "filter.lfs.required=false", "-c", "filter.lfs.smudge=", "-c", "filter.lfs.clean=", "-c", "filter.lfs.process=", "commit", "-m", "seed")
	runGitCommand(t, seed, "push", "-u", "origin", "main")
	runGitCommand(t, td, "--git-dir", remote, "symbolic-ref", "HEAD", "refs/heads/main")

	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(td, "gitconfig"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	if err := CloneFresh(managed, remote, "git"); err != nil {
		t.Fatalf("CloneFresh failed: %v", err)
	}
	assertMarkerAbsent(t, hookMarker)
	assertMarkerAbsent(t, smudgeMarker)
	body, err := os.ReadFile(filepath.Join(managed, "data.dat"))
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "payload\n" {
		t.Fatalf("expected stored payload, got %q", string(body))
	}

	worktree := filepath.Join(td, "worktree")
	if err := AddDetachedWorktree(managed, "git", worktree, "HEAD"); err != nil {
		t.Fatalf("AddDetachedWorktree failed: %v", err)
	}
	assertMarkerAbsent(t, hookMarker)
	assertMarkerAbsent(t, smudgeMarker)
}

func TestPolicyScriptUsesRestrictedEnvAndRecordsBlob(t *testing.T) {
	managed := initPolicyRepo(t, "#!/bin/sh\nenv > \"$TMPDIR/policy-env.txt\"\nexit 0\n")
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	t.Setenv("GH_TOKEN", "drop-token")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "drop-secret")
	t.Setenv("SSH_AUTH_SOCK", filepath.Join(tmp, "agent.sock"))
	orchestrator := &Orchestrator{
		managedRepoPath: managed,
		runRoot:         filepath.Join(t.TempDir(), "run"),
		config:          ReviewConfig{GitBin: "git", TrustRepo: true},
	}
	if err := orchestrator.deliveryPushRangePolicyScanBetween("HEAD", "HEAD", "HEAD"); err != nil {
		t.Fatalf("policy scan failed: %v", err)
	}
	captured, err := os.ReadFile(filepath.Join(tmp, "policy-env.txt"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(captured)
	for _, banned := range []string{"drop-token", "drop-secret", "SSH_AUTH_SOCK"} {
		if strings.Contains(text, banned) {
			t.Fatalf("policy environment contains %s:\n%s", banned, text)
		}
	}
	if !strings.Contains(text, "GIT_CONFIG_GLOBAL=/dev/null") {
		t.Fatalf("policy environment missing git config isolation:\n%s", text)
	}
	record := readPolicyScriptRecord(t, orchestrator.runRoot)
	if !record.Present || record.BlobSHA == "" {
		t.Fatalf("expected recorded policy blob, got %+v", record)
	}
	blob := strings.TrimSpace(runGitCommand(t, managed, "rev-parse", "HEAD:scripts/security/check-push-range.sh"))
	if record.BlobSHA != blob {
		t.Fatalf("expected blob %s, got %s", blob, record.BlobSHA)
	}
}

func TestPolicyScriptTimeout(t *testing.T) {
	managed := initPolicyRepo(t, "#!/bin/sh\nsleep 30\n")
	previous := policyScriptTimeout
	policyScriptTimeout = 200 * time.Millisecond
	t.Cleanup(func() { policyScriptTimeout = previous })
	orchestrator := &Orchestrator{
		managedRepoPath: managed,
		runRoot:         filepath.Join(t.TempDir(), "run"),
		config:          ReviewConfig{GitBin: "git", TrustRepo: true},
	}
	err := orchestrator.deliveryPushRangePolicyScanBetween("HEAD", "HEAD", "HEAD")
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("expected policy timeout, got %v", err)
	}
}

func initPolicyRepo(t *testing.T, script string) string {
	t.Helper()
	td := t.TempDir()
	managed := filepath.Join(td, "repo")
	runGitCommand(t, td, "init", "-b", "main", managed)
	runGitCommand(t, managed, "config", "user.email", testPlaceholderEmail("test"))
	runGitCommand(t, managed, "config", "user.name", "Test User")
	scriptDir := filepath.Join(managed, "scripts", "security")
	if err := os.MkdirAll(scriptDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(scriptDir, "check-push-range.sh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	runGitCommand(t, managed, "add", "scripts/security/check-push-range.sh")
	runGitCommand(t, managed, "commit", "-m", "add policy")
	return managed
}

type policyScriptRecord struct {
	Present bool   `json:"present"`
	BlobSHA string `json:"blob_sha"`
}

func readPolicyScriptRecord(t *testing.T, runRoot string) policyScriptRecord {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(runRoot, "policy-script.json"))
	if err != nil {
		t.Fatal(err)
	}
	var record policyScriptRecord
	if err := json.Unmarshal(body, &record); err != nil {
		t.Fatal(err)
	}
	return record
}

func assertMarkerAbsent(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected %s to stay absent, stat err=%v", path, err)
	}
}
