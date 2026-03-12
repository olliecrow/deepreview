package deepreview

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func runGitTest(t *testing.T, cwd string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = cwd
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git command failed: %v\nargs=%v\noutput=%s", err, args, string(out))
	}
	return strings.TrimSpace(string(out))
}

func TestResolveRepoIdentityLocalPathRequiresOriginRemote(t *testing.T) {
	td := t.TempDir()
	repo := filepath.Join(td, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, td, "init", repo)

	_, err := resolveRepoIdentity(ReviewConfig{GitBin: "git"}, repo)
	if err == nil {
		t.Fatalf("expected error when local repo has no origin remote")
	}
	if !strings.Contains(err.Error(), "remote.origin.url") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestResolveRepoIdentityLocalPathUsesOriginRemoteAsCloneSource(t *testing.T) {
	td := t.TempDir()
	repo := filepath.Join(td, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, td, "init", repo)
	runGitTest(t, td, "-C", repo, "remote", "add", "origin", "https://github.com/example-org/example-repo.git")

	identity, err := resolveRepoIdentity(ReviewConfig{GitBin: "git"}, repo)
	if err != nil {
		t.Fatalf("resolveRepoIdentity failed: %v", err)
	}
	if identity.CloneSource != "https://github.com/example-org/example-repo.git" {
		t.Fatalf("expected clone source to be origin remote, got: %s", identity.CloneSource)
	}
	if identity.Owner != "example-org" || identity.Name != "example-repo" {
		t.Fatalf("unexpected slug: %s/%s", identity.Owner, identity.Name)
	}
}

func TestPromptWatchdogPolicyClampsToSafeValues(t *testing.T) {
	o := &Orchestrator{
		config: ReviewConfig{
			ReviewInactivity:   30 * time.Second,
			ReviewActivityPoll: 90 * time.Second,
			ReviewMaxRestarts:  3,
		},
	}
	policy := o.promptWatchdogPolicy()
	if policy.inactivity != 30*time.Second {
		t.Fatalf("expected inactivity 30s, got %s", policy.inactivity)
	}
	if policy.pollInterval != 30*time.Second {
		t.Fatalf("expected poll interval clamped to inactivity (30s), got %s", policy.pollInterval)
	}
	if policy.maxRestarts != 3 {
		t.Fatalf("expected max restarts 3, got %d", policy.maxRestarts)
	}
}

func TestPromptWatchdogPolicyRejectsNegativeValues(t *testing.T) {
	o := &Orchestrator{
		config: ReviewConfig{
			ReviewInactivity:   -5 * time.Second,
			ReviewActivityPoll: -2 * time.Second,
			ReviewMaxRestarts:  -4,
		},
	}
	policy := o.promptWatchdogPolicy()
	if policy.inactivity != 0 {
		t.Fatalf("expected non-negative inactivity, got %s", policy.inactivity)
	}
	if policy.pollInterval != stageHeartbeatInterval {
		t.Fatalf("expected default poll interval %s, got %s", stageHeartbeatInterval, policy.pollInterval)
	}
	if policy.maxRestarts != 0 {
		t.Fatalf("expected non-negative max restarts, got %d", policy.maxRestarts)
	}
}

func TestFindPromptsRootIgnoresCallerWorkingDirectoryPrompts(t *testing.T) {
	td := t.TempDir()
	callerPrompts := filepath.Join(td, "prompts")
	if err := os.MkdirAll(callerPrompts, 0o755); err != nil {
		t.Fatal(err)
	}

	withWorkingDir(t, td, func() {
		t.Setenv("DEEPREVIEW_PROMPTS_ROOT", "")
		promptsRoot, _, err := findPromptsRoot()
		if err != nil {
			t.Fatalf("findPromptsRoot failed: %v", err)
		}
		if canonicalPath(t, promptsRoot) == canonicalPath(t, callerPrompts) {
			t.Fatalf("expected caller working directory prompts to be ignored, got %s", promptsRoot)
		}
		want := canonicalPath(t, filepath.Join(repoRoot(t), "prompts"))
		if canonicalPath(t, promptsRoot) != want {
			t.Fatalf("expected prompts root %s, got %s", want, promptsRoot)
		}
	})
}

func TestFindPromptsRootHonorsOverride(t *testing.T) {
	td := t.TempDir()
	override := filepath.Join(td, "override-prompts")
	if err := os.MkdirAll(override, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DEEPREVIEW_PROMPTS_ROOT", override)

	promptsRoot, toolRoot, err := findPromptsRoot()
	if err != nil {
		t.Fatalf("findPromptsRoot failed: %v", err)
	}
	if canonicalPath(t, promptsRoot) != canonicalPath(t, override) {
		t.Fatalf("expected prompts root %s, got %s", override, promptsRoot)
	}
	if canonicalPath(t, toolRoot) != canonicalPath(t, filepath.Dir(override)) {
		t.Fatalf("expected tool root %s, got %s", filepath.Dir(override), toolRoot)
	}
}

func TestFindPromptsRootRejectsMissingOverride(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing-prompts")
	t.Setenv("DEEPREVIEW_PROMPTS_ROOT", missing)

	_, _, err := findPromptsRoot()
	if err == nil {
		t.Fatalf("expected missing override to fail")
	}
	if !strings.Contains(err.Error(), "prompts root not found") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBuildReviewPromptScopeUsesCurrentStateAuditForDefaultBranchRuns(t *testing.T) {
	scope := buildReviewPromptScope("main", "main")
	if scope.ModeLabel != "current-state repository audit" {
		t.Fatalf("expected current-state audit mode, got %q", scope.ModeLabel)
	}
	if !strings.Contains(scope.ModeNote, "Treat branch-diff inspection as orientation only") {
		t.Fatalf("expected self-audit guidance in mode note, got: %s", scope.ModeNote)
	}
	if !strings.Contains(scope.ProcessStep1, "current-state repository audit") {
		t.Fatalf("expected self-audit process step, got: %s", scope.ProcessStep1)
	}
}

func TestBuildReviewPromptScopeUsesBranchDiffReviewForFeatureBranches(t *testing.T) {
	scope := buildReviewPromptScope("feature/test", "main")
	if scope.ModeLabel != "source-branch change review" {
		t.Fatalf("expected branch-diff review mode, got %q", scope.ModeLabel)
	}
	if scope.ModeNote != "" {
		t.Fatalf("expected no extra self-audit note, got: %s", scope.ModeNote)
	}
	if scope.ProcessStep1 != "Build a concrete change map from source branch vs default branch." {
		t.Fatalf("unexpected process step: %s", scope.ProcessStep1)
	}
}

func TestExecutePromptLabel(t *testing.T) {
	cases := []struct {
		templateName string
		want         string
	}{
		{templateName: "01-consolidate-plan.md", want: "consolidate and plan"},
		{templateName: "02-execute-verify.md", want: "execute and verify"},
		{templateName: "03-cleanup-summary-commit.md", want: "cleanup, summary, commit"},
		{templateName: "unknown.md", want: "unknown.md"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.templateName, func(t *testing.T) {
			got := executePromptLabel(tc.templateName)
			if got != tc.want {
				t.Fatalf("executePromptLabel(%q) = %q, want %q", tc.templateName, got, tc.want)
			}
		})
	}
}

func TestTriagePolicyViolationsAcceptRequiresCriticalOrHighWithHighConfidence(t *testing.T) {
	markdown := `# Round Triage

### item A
- disposition: accept
- severity: medium
- confidence: high

### item B
- disposition: accept
- severity: high
- confidence: medium

### item C
- disposition: reject
- severity: low
- confidence: low
`
	violations := triagePolicyViolations(markdown)
	if len(violations) != 2 {
		t.Fatalf("expected 2 violations, got %d: %v", len(violations), violations)
	}
	if !strings.Contains(strings.Join(violations, " | "), "item A has disallowed severity \"medium\"") {
		t.Fatalf("expected severity violation for item A, got: %v", violations)
	}
	if !strings.Contains(strings.Join(violations, " | "), "item B has disallowed confidence \"medium\"") {
		t.Fatalf("expected confidence violation for item B, got: %v", violations)
	}
}

func TestTriagePolicyViolationsAllowsAcceptedCriticalHighConfidence(t *testing.T) {
	markdown := `# Round Triage

### fix unsafe branch behavior
- disposition: accept
- severity: critical
- confidence: high
`
	violations := triagePolicyViolations(markdown)
	if len(violations) != 0 {
		t.Fatalf("expected no violations, got: %v", violations)
	}
}

func TestTriagePolicyViolationsRequiresTagsForAcceptedItems(t *testing.T) {
	markdown := `# Round Triage

### missing tags
- disposition: accept
`
	violations := triagePolicyViolations(markdown)
	if len(violations) != 2 {
		t.Fatalf("expected 2 violations for missing tags, got %d: %v", len(violations), violations)
	}
	joined := strings.Join(violations, " | ")
	if !strings.Contains(joined, "missing tags missing severity tag") {
		t.Fatalf("expected missing severity tag violation, got: %v", violations)
	}
	if !strings.Contains(joined, "missing tags missing confidence tag") {
		t.Fatalf("expected missing confidence tag violation, got: %v", violations)
	}
}

func TestBuildReviewSummaryInjectionPrefersVerdictAndIssueHeadings(t *testing.T) {
	td := t.TempDir()
	report := filepath.Join(td, "review-01.md")
	markdown := `# Independent Review 1

## Verdict
- ` + "`critical_flags_found: yes`" + `
- ` + "`merge_readiness: needs_fixes`" + `

## Critical Red Flags / Serious Issues
### [severity: high] sample cache bug
- Location: ` + "`src/cache.py:10`" + `
- Why it matters: stale cache can silently corrupt outputs.
- Evidence: empirical repro showed stale values.
- Recommendation: invalidate cache on upstream change.
- Confidence: high

## Verification ideas
- run pytest
`
	if err := os.WriteFile(report, []byte(markdown), 0o644); err != nil {
		t.Fatal(err)
	}

	summary, err := buildReviewSummaryInjection([]string{report})
	if err != nil {
		t.Fatalf("buildReviewSummaryInjection failed: %v", err)
	}
	for _, want := range []string{
		"## review-01.md",
		"## Verdict",
		"`critical_flags_found: yes`",
		"### [severity: high] sample cache bug",
		"- Why it matters: stale cache can silently corrupt outputs.",
		"- Confidence: high",
	} {
		if !strings.Contains(summary, want) {
			t.Fatalf("expected summary to include %q, got:\n%s", want, summary)
		}
	}
	if strings.Contains(summary, "## Verification ideas") {
		t.Fatalf("summary should omit verification-ideas section, got:\n%s", summary)
	}
}

func TestAcquireRunLockPreventsConcurrentSameRepoBranch(t *testing.T) {
	td := t.TempDir()
	shared := Orchestrator{
		workspaceRoot: td,
		repoIdentity:  RepoIdentity{Owner: "example", Name: "repo"},
		config:        ReviewConfig{RunID: "run-1", SourceBranch: "feature/a"},
	}
	if err := shared.acquireRunLock(); err != nil {
		t.Fatalf("acquire lock failed: %v", err)
	}
	defer shared.releaseRunLock()

	second := Orchestrator{
		workspaceRoot: td,
		repoIdentity:  RepoIdentity{Owner: "example", Name: "repo"},
		config:        ReviewConfig{RunID: "run-2", SourceBranch: "feature/a"},
	}
	err := second.acquireRunLock()
	if err == nil {
		second.releaseRunLock()
		t.Fatalf("expected lock acquisition to fail for concurrent same-repo same-branch run")
	}
	if !strings.Contains(err.Error(), "another deepreview run is active") {
		t.Fatalf("unexpected lock error: %v", err)
	}
}

func TestAcquireRunLockReplacesStaleLock(t *testing.T) {
	td := t.TempDir()
	o := Orchestrator{
		workspaceRoot: td,
		repoIdentity:  RepoIdentity{Owner: "example", Name: "repo"},
		config:        ReviewConfig{RunID: "new-run", SourceBranch: "feature/a"},
	}
	lockPath := o.runLockFilePath()
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
		t.Fatal(err)
	}
	stale := runLockRecord{
		RunID:        "old-run",
		PID:          999999,
		Repo:         "example/repo",
		SourceBranch: "feature/a",
		CreatedAt:    time.Now().Add(-24 * time.Hour).UTC().Format(time.RFC3339),
	}
	payload, err := json.Marshal(stale)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lockPath, payload, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := o.acquireRunLock(); err != nil {
		t.Fatalf("expected stale lock replacement, got error: %v", err)
	}
	defer o.releaseRunLock()

	currentBytes, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("expected fresh lock file, read failed: %v", err)
	}
	var current runLockRecord
	if err := json.Unmarshal(currentBytes, &current); err != nil {
		t.Fatalf("invalid lock json: %v", err)
	}
	if current.RunID != "new-run" {
		t.Fatalf("expected lock run id new-run, got %s", current.RunID)
	}
}

func TestAcquireRunLockAllowsDifferentRepos(t *testing.T) {
	td := t.TempDir()
	first := Orchestrator{
		workspaceRoot: td,
		repoIdentity:  RepoIdentity{Owner: "example", Name: "repo-a"},
		config:        ReviewConfig{RunID: "run-a", SourceBranch: "feature/shared"},
	}
	second := Orchestrator{
		workspaceRoot: td,
		repoIdentity:  RepoIdentity{Owner: "example", Name: "repo-b"},
		config:        ReviewConfig{RunID: "run-b", SourceBranch: "feature/shared"},
	}
	if err := first.acquireRunLock(); err != nil {
		t.Fatalf("first lock failed: %v", err)
	}
	defer first.releaseRunLock()
	if err := second.acquireRunLock(); err != nil {
		t.Fatalf("second lock for different repo should succeed: %v", err)
	}
	defer second.releaseRunLock()
}

func TestAcquireRunLockAllowsDifferentBranchesSameRepo(t *testing.T) {
	td := t.TempDir()
	first := Orchestrator{
		workspaceRoot: td,
		repoIdentity:  RepoIdentity{Owner: "example", Name: "repo"},
		config:        ReviewConfig{RunID: "run-a", SourceBranch: "feature/a"},
	}
	second := Orchestrator{
		workspaceRoot: td,
		repoIdentity:  RepoIdentity{Owner: "example", Name: "repo"},
		config:        ReviewConfig{RunID: "run-b", SourceBranch: "feature/b"},
	}
	if err := first.acquireRunLock(); err != nil {
		t.Fatalf("first lock failed: %v", err)
	}
	defer first.releaseRunLock()
	if err := second.acquireRunLock(); err != nil {
		t.Fatalf("second lock for different branch should succeed: %v", err)
	}
	defer second.releaseRunLock()
}

func TestNewOrchestratorIsolatesManagedRepoPathPerSourceBranch(t *testing.T) {
	td := t.TempDir()
	reporter := &NullProgressReporter{}
	base := ReviewConfig{
		Repo:          "example/repo",
		WorkspaceRoot: td,
		RunID:         "run-1",
		GitBin:        "git",
		CodexBin:      "codex",
		SourceBranch:  "feature/a",
	}

	first, err := NewOrchestrator(base, reporter)
	if err != nil {
		t.Fatalf("NewOrchestrator first failed: %v", err)
	}

	secondCfg := base
	secondCfg.SourceBranch = "feature/b"
	second, err := NewOrchestrator(secondCfg, reporter)
	if err != nil {
		t.Fatalf("NewOrchestrator second failed: %v", err)
	}

	if first.managedRepoPath == second.managedRepoPath {
		t.Fatalf("expected different managed repo paths for different branches, got %s", first.managedRepoPath)
	}
	if !strings.Contains(first.managedRepoPath, string(filepath.Separator)+"branches"+string(filepath.Separator)) {
		t.Fatalf("expected managed repo path to include branch isolation directory, got %s", first.managedRepoPath)
	}
}

func TestCapPRBodyForGitHubFallsBackToCompactBody(t *testing.T) {
	td := t.TempDir()
	roundDir := filepath.Join(td, "round-01")
	if err := os.MkdirAll(roundDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(roundDir, "round-summary.md"), []byte("summary\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(roundDir, "round-status.json"), []byte(`{"decision":"stop","reason":"complete","confidence":0.9}`), 0o644); err != nil {
		t.Fatal(err)
	}

	o := &Orchestrator{config: ReviewConfig{RunID: "run-1"}}
	oversized := strings.Repeat("x", githubPRBodyTargetChars+4096)
	out := o.capPRBodyForGitHub(oversized, []string{filepath.Join(roundDir, "round-summary.md")}, []string{"a.go", "b.go"}, prDeliveryOptions{})
	if utf8.RuneCountInString(out) > githubPRBodyTargetChars {
		t.Fatalf("expected capped pr body <= %d chars, got %d", githubPRBodyTargetChars, utf8.RuneCountInString(out))
	}
	if !strings.Contains(out, "compact body to stay within GitHub PR body limits") {
		t.Fatalf("expected compact-body fallback marker, got:\n%s", out)
	}
}

func TestCapPRBodyForGitHubKeepsNormalBody(t *testing.T) {
	o := &Orchestrator{config: ReviewConfig{RunID: "run-1"}}
	body := "## at a glance\n- short body\n"
	out := o.capPRBodyForGitHub(body, nil, nil, prDeliveryOptions{})
	if out != body {
		t.Fatalf("expected short body to remain unchanged")
	}
}

func TestNormalizePRTitleAddsPrefixAndNormalizesWhitespace(t *testing.T) {
	out := normalizePRTitle("  improve logging clarity \n and test coverage  ", "deepreview: review updates")
	if out != "deepreview: improve logging clarity and test coverage" {
		t.Fatalf("unexpected normalized title: %q", out)
	}
}

func TestNormalizePRTitleFallsBackWhenGeneratedTitleEmpty(t *testing.T) {
	out := normalizePRTitle("   \n", "deepreview: fallback title")
	if out != "deepreview: fallback title" {
		t.Fatalf("expected fallback title, got: %q", out)
	}
}

func TestNormalizePRTitleHandlesDeepreviewPrefixedVariants(t *testing.T) {
	out := normalizePRTitle("DeepReview - tighten retry behavior", "deepreview: fallback")
	if out != "deepreview: tighten retry behavior" {
		t.Fatalf("expected normalized prefixed variant, got: %q", out)
	}
}

func TestNormalizePRTitleCapsLengthForGitHubSafety(t *testing.T) {
	longCore := strings.Repeat("x", githubPRTitleTargetChars+100)
	out := normalizePRTitle(longCore, "deepreview: fallback")
	if utf8.RuneCountInString(out) > githubPRTitleTargetChars {
		t.Fatalf("expected title <= %d chars, got %d", githubPRTitleTargetChars, utf8.RuneCountInString(out))
	}
	if !strings.HasPrefix(strings.ToLower(out), "deepreview:") {
		t.Fatalf("expected deepreview prefix after normalization, got: %q", out)
	}
}

func TestEnsureIncompletePRTitlePrefixPrependsMarker(t *testing.T) {
	out := ensureIncompletePRTitlePrefix("deepreview: tighten retry behavior")
	if out != "[INCOMPLETE] deepreview: tighten retry behavior" {
		t.Fatalf("unexpected incomplete title: %q", out)
	}
}

func TestBuildIncompletePRBodyMentionsIncompleteStatus(t *testing.T) {
	td := t.TempDir()
	roundDir := filepath.Join(td, "round-01")
	if err := os.MkdirAll(roundDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(roundDir, "round-summary.md"), []byte("summary\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(roundDir, "round-status.json"), []byte(`{"decision":"continue","reason":"need one more blocker fix","next_focus":"finish blocker"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	o := &Orchestrator{config: ReviewConfig{RunID: "run-1"}}
	body := o.buildIncompletePRBody([]string{filepath.Join(roundDir, "round-summary.md")}, []string{"internal/foo.go"}, "automatic audit found more work")
	if !strings.Contains(body, "[INCOMPLETE]") {
		t.Fatalf("expected incomplete marker in body, got:\n%s", body)
	}
	if !strings.Contains(body, "do not merge this PR as-is") {
		t.Fatalf("expected merge warning in body, got:\n%s", body)
	}
	if !strings.Contains(body, "latest decision: `continue`") {
		t.Fatalf("expected latest round decision in body, got:\n%s", body)
	}
}

func TestBasePRTitleFromChangesUsesTopAreaAndFileCount(t *testing.T) {
	title := basePRTitleFromChanges([]string{"cmd/a.go", "cmd/b.go", "internal/x.go"})
	if title != "deepreview: cmd updates (3 files)" {
		t.Fatalf("unexpected base title: %q", title)
	}
}

func TestBasePRTitleFromChangesHandlesRootOnlyChanges(t *testing.T) {
	title := basePRTitleFromChanges([]string{"README.md", "go.mod"})
	if title != "deepreview: review updates (2 files)" {
		t.Fatalf("unexpected base title for root-only changes: %q", title)
	}
}

func TestRunPreCommitAllFilesIfConfiguredPasses(t *testing.T) {
	td := t.TempDir()
	repoPath := filepath.Join(td, "repo")
	if err := os.MkdirAll(repoPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoPath, ".pre-commit-config.yaml"), []byte("repos: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	binDir := filepath.Join(td, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	preCommitPath := filepath.Join(binDir, "pre-commit")
	if err := os.WriteFile(preCommitPath, []byte("#!/usr/bin/env bash\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+":"+os.Getenv("PATH"))

	o := &Orchestrator{managedRepoPath: repoPath, reporter: &NullProgressReporter{}}
	if err := o.runPreCommitAllFilesIfConfigured(repoPath); err != nil {
		t.Fatalf("expected pre-commit gate to pass, got: %v", err)
	}
}

func TestRunPreCommitAllFilesIfConfiguredFailsOnHookFailure(t *testing.T) {
	td := t.TempDir()
	repoPath := filepath.Join(td, "repo")
	if err := os.MkdirAll(repoPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoPath, ".pre-commit-config.yaml"), []byte("repos: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	binDir := filepath.Join(td, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	preCommitPath := filepath.Join(binDir, "pre-commit")
	if err := os.WriteFile(preCommitPath, []byte("#!/usr/bin/env bash\necho 'hook failed' >&2\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+":"+os.Getenv("PATH"))

	o := &Orchestrator{managedRepoPath: repoPath, reporter: &NullProgressReporter{}}
	err := o.runPreCommitAllFilesIfConfigured(repoPath)
	if err == nil {
		t.Fatalf("expected pre-commit gate failure")
	}
	if !strings.Contains(err.Error(), "delivery blocked: pre-commit checks failed") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunSetupEnvIfPresentFailsWhenScriptFails(t *testing.T) {
	td := t.TempDir()
	repoPath := filepath.Join(td, "repo")
	if err := os.MkdirAll(repoPath, 0o755); err != nil {
		t.Fatal(err)
	}
	scriptPath := filepath.Join(repoPath, "setup_env.sh")
	if err := os.WriteFile(scriptPath, []byte("#!/usr/bin/env bash\necho 'tests failed' >&2\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	o := &Orchestrator{managedRepoPath: repoPath, reporter: &NullProgressReporter{}}
	err := o.runSetupEnvIfPresent(repoPath)
	if err == nil {
		t.Fatalf("expected setup_env gate failure")
	}
	if !strings.Contains(err.Error(), "delivery blocked: setup_env.sh failed") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunDeliveryQualityChecksRunsAgainstCandidateWorktree(t *testing.T) {
	td := t.TempDir()
	repoPath := filepath.Join(td, "repo")
	if err := os.MkdirAll(repoPath, 0o755); err != nil {
		t.Fatal(err)
	}

	git := func(args ...string) {
		t.Helper()
		cmd := append([]string{"git", "-C", repoPath}, args...)
		if _, err := RunCommand(cmd, "", "", true, 0); err != nil {
			t.Fatalf("git command failed: %v\nargs=%v", err, args)
		}
	}

	git("init", "-b", "main")
	git("config", "user.name", "deepreview-test")
	git("config", "user.email", "deepreview-test@example.com")

	if err := os.WriteFile(filepath.Join(repoPath, ".pre-commit-config.yaml"), []byte("repos: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoPath, "branch-marker.txt"), []byte("main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	setupEnvPath := filepath.Join(repoPath, "setup_env.sh")
	setupScript := "#!/usr/bin/env bash\nset -euo pipefail\nif grep -q '^candidate$' branch-marker.txt; then\n  exit 0\nfi\necho 'setup_env ran on wrong branch content' >&2\nexit 1\n"
	if err := os.WriteFile(setupEnvPath, []byte(setupScript), 0o755); err != nil {
		t.Fatal(err)
	}
	git("add", "-A")
	git("commit", "-m", "main commit")

	git("branch", "candidate")
	git("checkout", "candidate")
	if err := os.WriteFile(filepath.Join(repoPath, "branch-marker.txt"), []byte("candidate\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", "branch-marker.txt")
	git("commit", "-m", "candidate marker")
	git("checkout", "main")

	binDir := filepath.Join(td, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	preCommitPath := filepath.Join(binDir, "pre-commit")
	preCommitScript := "#!/usr/bin/env bash\nset -euo pipefail\nif grep -q '^candidate$' branch-marker.txt; then\n  exit 0\nfi\necho 'pre-commit ran on wrong branch content' >&2\nexit 1\n"
	if err := os.WriteFile(preCommitPath, []byte(preCommitScript), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+":"+os.Getenv("PATH"))

	runRoot := filepath.Join(td, "run")
	if err := os.MkdirAll(runRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	o := &Orchestrator{
		config: ReviewConfig{
			GitBin: "git",
		},
		managedRepoPath: repoPath,
		runRoot:         runRoot,
		reporter:        &NullProgressReporter{},
	}
	if err := o.runDeliveryQualityChecks("candidate"); err != nil {
		t.Fatalf("expected delivery quality checks to pass on candidate worktree, got: %v", err)
	}
	if _, err := os.Stat(filepath.Join(runRoot, "delivery", "quality-worktree")); !os.IsNotExist(err) {
		t.Fatalf("expected delivery quality worktree cleanup, stat err=%v", err)
	}
}

func TestRunDeliveryQualityChecksCleansUpWorktreeOnFailure(t *testing.T) {
	td := t.TempDir()
	repoPath := filepath.Join(td, "repo")
	if err := os.MkdirAll(repoPath, 0o755); err != nil {
		t.Fatal(err)
	}

	git := func(args ...string) {
		t.Helper()
		cmd := append([]string{"git", "-C", repoPath}, args...)
		if _, err := RunCommand(cmd, "", "", true, 0); err != nil {
			t.Fatalf("git command failed: %v\nargs=%v", err, args)
		}
	}

	git("init", "-b", "main")
	git("config", "user.name", "deepreview-test")
	git("config", "user.email", "deepreview-test@example.com")

	if err := os.WriteFile(filepath.Join(repoPath, ".pre-commit-config.yaml"), []byte("repos: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoPath, "branch-marker.txt"), []byte("candidate\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", "-A")
	git("commit", "-m", "candidate commit")

	binDir := filepath.Join(td, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	preCommitPath := filepath.Join(binDir, "pre-commit")
	preCommitScript := "#!/usr/bin/env bash\necho 'intentional failure' >&2\nexit 1\n"
	if err := os.WriteFile(preCommitPath, []byte(preCommitScript), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+":"+os.Getenv("PATH"))

	runRoot := filepath.Join(td, "run")
	if err := os.MkdirAll(runRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	o := &Orchestrator{
		config: ReviewConfig{
			GitBin: "git",
		},
		managedRepoPath: repoPath,
		runRoot:         runRoot,
		reporter:        &NullProgressReporter{},
	}
	err := o.runDeliveryQualityChecks("main")
	if err == nil {
		t.Fatalf("expected delivery quality checks to fail")
	}
	if !strings.Contains(err.Error(), "delivery blocked: pre-commit checks failed") {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(runRoot, "delivery", "quality-worktree")); !os.IsNotExist(statErr) {
		t.Fatalf("expected delivery quality worktree cleanup after failure, stat err=%v", statErr)
	}
}
