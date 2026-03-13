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
	if identity.SourceType != RepoSourceGitHub {
		t.Fatalf("expected GitHub repo source type, got: %s", identity.SourceType)
	}
	if identity.Owner != "example-org" || identity.Name != "example-repo" {
		t.Fatalf("unexpected slug: %s/%s", identity.Owner, identity.Name)
	}
}

func TestResolveRepoIdentityLocalPathAcceptsFilesystemOriginRemote(t *testing.T) {
	td := t.TempDir()
	remote := filepath.Join(td, "remote.git")
	repo := filepath.Join(td, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, td, "init", "--bare", remote)
	runGitTest(t, td, "init", repo)
	runGitTest(t, td, "-C", repo, "remote", "add", "origin", remote)

	identity, err := resolveRepoIdentity(ReviewConfig{GitBin: "git"}, repo)
	if err != nil {
		t.Fatalf("resolveRepoIdentity failed: %v", err)
	}
	if canonicalPath(t, identity.CloneSource) != canonicalPath(t, remote) {
		t.Fatalf("expected clone source to stay local origin path, got: %s", identity.CloneSource)
	}
	if identity.SourceType != RepoSourceFilesystem {
		t.Fatalf("expected filesystem repo source type, got: %s", identity.SourceType)
	}
	if identity.Name != "remote" {
		t.Fatalf("expected filesystem repo name derived from clone source, got: %s", identity.Name)
	}
}

func TestResolveRepoIdentityCanonicalizesRelativeFilesystemOriginRemote(t *testing.T) {
	td := t.TempDir()
	remote := filepath.Join(td, "remote.git")
	repo := filepath.Join(td, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, td, "init", "--bare", remote)
	runGitTest(t, td, "init", repo)
	runGitTest(t, td, "-C", repo, "remote", "add", "origin", "../remote.git")

	identity, err := resolveRepoIdentity(ReviewConfig{GitBin: "git"}, repo)
	if err != nil {
		t.Fatalf("resolveRepoIdentity failed: %v", err)
	}
	if identity.SourceType != RepoSourceFilesystem {
		t.Fatalf("expected filesystem repo source type, got: %s", identity.SourceType)
	}
	if canonicalPath(t, identity.CloneSource) != canonicalPath(t, remote) {
		t.Fatalf("expected canonical relative clone source %s, got %s", canonicalPath(t, remote), identity.CloneSource)
	}
	if identity.Name != "remote" {
		t.Fatalf("expected filesystem repo display name remote, got %s", identity.Name)
	}
}

func TestNewOrchestratorRejectsPRModeForFilesystemOriginRemote(t *testing.T) {
	td := t.TempDir()
	remote := filepath.Join(td, "remote.git")
	repo := filepath.Join(td, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, td, "init", "--bare", remote)
	runGitTest(t, td, "init", repo)
	runGitTest(t, td, "-C", repo, "remote", "add", "origin", remote)

	_, err := NewOrchestrator(ReviewConfig{
		Repo:          repo,
		SourceBranch:  "main",
		WorkspaceRoot: t.TempDir(),
		RunID:         "run-1",
		GitBin:        "git",
		CodexBin:      "codex",
		GhBin:         "gh",
		Mode:          ModePR,
	}, &NullProgressReporter{})
	if err == nil {
		t.Fatalf("expected PR mode to reject filesystem origin remote")
	}
	if !strings.Contains(err.Error(), "--mode pr requires a GitHub-backed repo identity") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNewOrchestratorAllowsYoloModeForFilesystemOriginRemote(t *testing.T) {
	td := t.TempDir()
	remote := filepath.Join(td, "remote.git")
	repo := filepath.Join(td, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, td, "init", "--bare", remote)
	runGitTest(t, td, "init", repo)
	runGitTest(t, td, "-C", repo, "remote", "add", "origin", remote)

	o, err := NewOrchestrator(ReviewConfig{
		Repo:          repo,
		SourceBranch:  "main",
		WorkspaceRoot: t.TempDir(),
		RunID:         "run-1",
		GitBin:        "git",
		CodexBin:      "codex",
		GhBin:         "gh",
		Mode:          ModeYolo,
	}, &NullProgressReporter{})
	if err != nil {
		t.Fatalf("expected yolo mode to allow filesystem origin remote, got: %v", err)
	}
	if o.repoIdentity.SourceType != RepoSourceFilesystem {
		t.Fatalf("expected filesystem repo identity, got: %s", o.repoIdentity.SourceType)
	}
}

func TestNewOrchestratorCanonicalizesRelativeFilesystemOriginForDoctorAndClone(t *testing.T) {
	td := t.TempDir()
	remote := filepath.Join(td, "remote.git")
	seed := filepath.Join(td, "seed")
	repo := filepath.Join(td, "repo")
	workspace := filepath.Join(td, "workspace")

	runGitTest(t, td, "init", "--bare", remote)
	runGitTest(t, td, "init", "-b", "main", seed)
	runGitTest(t, td, "-C", seed, "config", "user.email", testPlaceholderEmail("seed"))
	runGitTest(t, td, "-C", seed, "config", "user.name", "Seed User")
	if err := os.WriteFile(filepath.Join(seed, "README.md"), []byte("seed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, td, "-C", seed, "add", "README.md")
	runGitTest(t, td, "-C", seed, "commit", "-m", "seed")
	runGitTest(t, td, "-C", seed, "remote", "add", "origin", remote)
	runGitTest(t, td, "-C", seed, "push", "-u", "origin", "main")

	runGitTest(t, td, "clone", remote, repo)
	runGitTest(t, td, "-C", repo, "remote", "set-url", "origin", "../remote.git")

	var orchestrator *Orchestrator
	withWorkingDir(t, t.TempDir(), func() {
		var err error
		orchestrator, err = NewOrchestrator(ReviewConfig{
			Repo:          repo,
			SourceBranch:  "main",
			WorkspaceRoot: workspace,
			RunID:         "run-1",
			GitBin:        "git",
			CodexBin:      "codex",
			GhBin:         "gh",
			Mode:          ModeYolo,
		}, &NullProgressReporter{})
		if err != nil {
			t.Fatalf("NewOrchestrator failed: %v", err)
		}
		if err := CloneOrFetch(orchestrator.managedRepoPath, orchestrator.repoIdentity.CloneSource, "git"); err != nil {
			t.Fatalf("CloneOrFetch failed with canonicalized local origin: %v", err)
		}
	})

	if canonicalPath(t, orchestrator.repoIdentity.CloneSource) != canonicalPath(t, remote) {
		t.Fatalf("expected canonical clone source %s, got %s", canonicalPath(t, remote), orchestrator.repoIdentity.CloneSource)
	}
	checks := buildDoctorChecks(orchestrator)
	branchCheck := doctorCheck{}
	for _, check := range checks {
		if check.Name == "remote source branch reachable" {
			branchCheck = check
			break
		}
	}
	if !branchCheck.Passed {
		t.Fatalf("expected doctor remote branch check to pass, got detail: %s", branchCheck.Detail)
	}
	if !strings.Contains(branchCheck.Detail, orchestrator.repoIdentity.CloneSource) {
		t.Fatalf("expected doctor detail to mention canonical clone source, got: %s", branchCheck.Detail)
	}
	if _, err := os.Stat(filepath.Join(orchestrator.managedRepoPath, ".git")); err != nil {
		t.Fatalf("expected managed repo clone to exist, got %v", err)
	}
}

func TestResolveRepoIdentityLocalPathRejectsNonGitHubOriginRemote(t *testing.T) {
	td := t.TempDir()
	repo := filepath.Join(td, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, td, "init", repo)
	runGitTest(t, td, "-C", repo, "remote", "add", "origin", "ssh://mirror.local/github.com/example-org/example-repo.git")

	_, err := resolveRepoIdentity(ReviewConfig{GitBin: "git"}, repo)
	if err == nil {
		t.Fatalf("expected non-GitHub origin remote to be rejected")
	}
	if !strings.Contains(err.Error(), "GitHub or local filesystem origin remote") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestResolveRepoIdentityPreservesExplicitSSHGitHubURL(t *testing.T) {
	repo := githubSSHCloneURL("example-org", "example-repo")
	identity, err := resolveRepoIdentity(ReviewConfig{GitBin: "git"}, repo)
	if err != nil {
		t.Fatalf("resolveRepoIdentity failed: %v", err)
	}
	if identity.CloneSource != repo {
		t.Fatalf("expected clone source %q, got %q", repo, identity.CloneSource)
	}
}

func TestResolveRepoIdentityTreatsGitHubLocalOwnerAsGitHub(t *testing.T) {
	testCases := []struct {
		name  string
		input string
	}{
		{name: "slug", input: "local/repo"},
		{name: "https", input: "https://github.com/local/repo.git"},
		{name: "scp", input: githubSCPLikeCloneURL("local", "repo")},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			identity, err := resolveRepoIdentity(ReviewConfig{GitBin: "git"}, tc.input)
			if err != nil {
				t.Fatalf("resolveRepoIdentity failed: %v", err)
			}
			if identity.SourceType != RepoSourceGitHub {
				t.Fatalf("expected GitHub repo source type, got: %s", identity.SourceType)
			}
			if identity.Owner != "local" || identity.Name != "repo" {
				t.Fatalf("unexpected slug: %s/%s", identity.Owner, identity.Name)
			}
			if !identity.SupportsPRDelivery() {
				t.Fatalf("expected GitHub local/repo identity to support PR delivery")
			}
		})
	}
}

func TestParseOwnerRepoAcceptsGitHubLocatorForms(t *testing.T) {
	cases := []struct {
		input string
		owner string
		name  string
	}{
		{input: "example-org/example-repo", owner: "example-org", name: "example-repo"},
		{input: "https://github.com/example-org/example-repo.git", owner: "example-org", name: "example-repo"},
		{input: githubSSHCloneURL("example-org", "example-repo"), owner: "example-org", name: "example-repo"},
		{input: githubSCPLikeCloneURL("example-org", "example-repo"), owner: "example-org", name: "example-repo"},
	}

	for _, tc := range cases {
		owner, name, ok := parseOwnerRepo(tc.input)
		if !ok {
			t.Fatalf("expected parseOwnerRepo to accept %q", tc.input)
		}
		if owner != tc.owner || name != tc.name {
			t.Fatalf("parseOwnerRepo(%q) = %s/%s, want %s/%s", tc.input, owner, name, tc.owner, tc.name)
		}
	}
}

func TestParseOwnerRepoRejectsSuffixOnlyNonGitHubRemotes(t *testing.T) {
	cases := []string{
		"ssh://mirror.local/github.com/example-org/example-repo.git",
		"file:///tmp/github.com/example-org/example-repo.git",
	}

	for _, input := range cases {
		if owner, name, ok := parseOwnerRepo(input); ok {
			t.Fatalf("expected parseOwnerRepo to reject %q, got %s/%s", input, owner, name)
		}
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

func TestFindPromptsRootIgnoresExecutableAdjacentPromptsByDefault(t *testing.T) {
	exePath, err := os.Executable()
	if err != nil {
		t.Fatalf("resolve executable: %v", err)
	}
	exePrompts := filepath.Join(filepath.Dir(exePath), "prompts")
	if err := os.MkdirAll(exePrompts, 0o755); err != nil {
		t.Fatalf("create executable-adjacent prompts: %v", err)
	}
	t.Cleanup(func() {
		_ = os.RemoveAll(exePrompts)
	})

	t.Setenv("DEEPREVIEW_PROMPTS_ROOT", "")
	promptsRoot, _, err := findPromptsRoot()
	if err != nil {
		t.Fatalf("findPromptsRoot failed: %v", err)
	}
	if canonicalPath(t, promptsRoot) == canonicalPath(t, exePrompts) {
		t.Fatalf("expected executable-adjacent prompts to be ignored, got %s", promptsRoot)
	}
	want := canonicalPath(t, filepath.Join(repoRoot(t), "prompts"))
	if canonicalPath(t, promptsRoot) != want {
		t.Fatalf("expected prompts root %s, got %s", want, promptsRoot)
	}
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
		repoIdentity:  RepoIdentity{SourceType: RepoSourceGitHub, Owner: "example", Name: "repo"},
		config:        ReviewConfig{RunID: "run-1", SourceBranch: "feature/a"},
	}
	if err := shared.acquireRunLock(); err != nil {
		t.Fatalf("acquire lock failed: %v", err)
	}
	defer shared.releaseRunLock()

	second := Orchestrator{
		workspaceRoot: td,
		repoIdentity:  RepoIdentity{SourceType: RepoSourceGitHub, Owner: "example", Name: "repo"},
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
		repoIdentity:  RepoIdentity{SourceType: RepoSourceGitHub, Owner: "example", Name: "repo"},
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
		repoIdentity:  RepoIdentity{SourceType: RepoSourceGitHub, Owner: "example", Name: "repo-a"},
		config:        ReviewConfig{RunID: "run-a", SourceBranch: "feature/shared"},
	}
	second := Orchestrator{
		workspaceRoot: td,
		repoIdentity:  RepoIdentity{SourceType: RepoSourceGitHub, Owner: "example", Name: "repo-b"},
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
		repoIdentity:  RepoIdentity{SourceType: RepoSourceGitHub, Owner: "example", Name: "repo"},
		config:        ReviewConfig{RunID: "run-a", SourceBranch: "feature/a"},
	}
	second := Orchestrator{
		workspaceRoot: td,
		repoIdentity:  RepoIdentity{SourceType: RepoSourceGitHub, Owner: "example", Name: "repo"},
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

func TestNewOrchestratorIsolatesGitHubAndFilesystemNamespacePaths(t *testing.T) {
	td := t.TempDir()
	reporter := &NullProgressReporter{}

	remote := filepath.Join(td, "repo.git")
	localRepo := filepath.Join(td, "clone")
	runGitTest(t, td, "init", "--bare", remote)
	runGitTest(t, td, "init", localRepo)
	runGitTest(t, td, "-C", localRepo, "remote", "add", "origin", remote)

	githubConfig := ReviewConfig{
		Repo:          "local/repo",
		WorkspaceRoot: td,
		RunID:         "run-github",
		GitBin:        "git",
		CodexBin:      "codex",
		SourceBranch:  "feature/test",
	}
	filesystemConfig := ReviewConfig{
		Repo:          localRepo,
		WorkspaceRoot: td,
		RunID:         "run-filesystem",
		GitBin:        "git",
		CodexBin:      "codex",
		SourceBranch:  "feature/test",
	}

	githubOrchestrator, err := NewOrchestrator(githubConfig, reporter)
	if err != nil {
		t.Fatalf("NewOrchestrator for GitHub repo failed: %v", err)
	}
	filesystemOrchestrator, err := NewOrchestrator(filesystemConfig, reporter)
	if err != nil {
		t.Fatalf("NewOrchestrator for filesystem repo failed: %v", err)
	}

	if githubOrchestrator.managedRepoPath == filesystemOrchestrator.managedRepoPath {
		t.Fatalf("expected distinct managed repo paths, got %s", githubOrchestrator.managedRepoPath)
	}
	if githubOrchestrator.runLockFilePath() == filesystemOrchestrator.runLockFilePath() {
		t.Fatalf("expected distinct run lock paths, got %s", githubOrchestrator.runLockFilePath())
	}
}

func TestPreflightUsesResolvedMulticodexWithoutCodexOnPath(t *testing.T) {
	repo := createSyncedGitHubLikeRepo(t, "feature/test")
	workspace := t.TempDir()
	toolDir := t.TempDir()
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Fatalf("resolve git: %v", err)
	}
	writeExecutable(t, filepath.Join(toolDir, "multicodex"), "#!/bin/sh\nexit 0\n")
	ghPath := writeFakeTool(t, toolDir, "gh")
	t.Setenv("PATH", toolDir)
	t.Setenv("DEEPREVIEW_PROMPTS_ROOT", filepath.Join(repoRoot(t), "prompts"))
	t.Setenv("SHELL", "")

	o, err := NewOrchestrator(ReviewConfig{
		Repo:          repo,
		SourceBranch:  "feature/test",
		WorkspaceRoot: workspace,
		RunID:         "run-1",
		GitBin:        gitPath,
		CodexBin:      "codex",
		GhBin:         ghPath,
		Mode:          ModePR,
	}, &NullProgressReporter{})
	if err != nil {
		t.Fatalf("NewOrchestrator failed: %v", err)
	}
	if err := o.preflight(); err != nil {
		t.Fatalf("expected preflight to succeed with multicodex-only PATH, got: %v", err)
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

func TestEvaluateRoundLoopControlContinueAlwaysContinues(t *testing.T) {
	control := evaluateRoundLoopControl(1, RoundStatus{Decision: "continue", Reason: "more work"}, false, 0)
	if !control.shouldContinue {
		t.Fatal("expected continue decision to force another round")
	}
	if control.nextStopStreak != 0 {
		t.Fatalf("expected continue decision to reset stop streak, got %d", control.nextStopStreak)
	}
}

func TestEvaluateRoundLoopControlFirstStopStillContinues(t *testing.T) {
	control := evaluateRoundLoopControl(0, RoundStatus{Decision: "stop", Reason: "looks good"}, true, 3)
	if !control.shouldContinue {
		t.Fatal("expected first stop decision to require a confirmation round")
	}
	if control.nextStopStreak != 1 {
		t.Fatalf("expected stop streak 1, got %d", control.nextStopStreak)
	}
	if !strings.Contains(control.message, "first stop decision") {
		t.Fatalf("expected first-stop message, got %q", control.message)
	}
}

func TestEvaluateRoundLoopControlSecondStopTerminatesEvenWithChanges(t *testing.T) {
	control := evaluateRoundLoopControl(1, RoundStatus{Decision: "stop", Reason: "still good"}, true, 2)
	if control.shouldContinue {
		t.Fatal("expected second consecutive stop decision to end the loop")
	}
	if control.nextStopStreak != 2 {
		t.Fatalf("expected stop streak 2, got %d", control.nextStopStreak)
	}
	if !strings.Contains(control.message, "despite 2 repository change(s)") {
		t.Fatalf("expected second-stop-with-changes message, got %q", control.message)
	}
}

func TestAutoCommitWorktreeChangesIfNeededCommitsDirtyWorktree(t *testing.T) {
	td := t.TempDir()
	repoPath := filepath.Join(td, "repo")
	if err := os.MkdirAll(repoPath, 0o755); err != nil {
		t.Fatal(err)
	}

	runGitTest(t, td, "init", "-b", "main", repoPath)
	runGitTest(t, td, "-C", repoPath, "config", "user.name", "deepreview-test")
	runGitTest(t, td, "-C", repoPath, "config", "user.email", testPlaceholderEmail("deepreview-test"))
	if err := os.WriteFile(filepath.Join(repoPath, "tracked.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, td, "-C", repoPath, "add", "tracked.txt")
	runGitTest(t, td, "-C", repoPath, "commit", "-m", "seed")
	if err := os.WriteFile(filepath.Join(repoPath, "tracked.txt"), []byte("updated\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	o := &Orchestrator{
		config: ReviewConfig{GitBin: "git"},
		commitIdentity: CommitIdentity{
			Name:  "deepreview-test",
			Email: testPlaceholderEmail("deepreview-test"),
		},
	}
	if err := o.autoCommitWorktreeChangesIfNeeded(repoPath, "deepreview: auto commit"); err != nil {
		t.Fatalf("expected dirty worktree to auto-commit, got: %v", err)
	}
	dirty, err := HasUncommittedChanges(repoPath, "git")
	if err != nil {
		t.Fatalf("status check failed: %v", err)
	}
	if dirty {
		t.Fatal("expected clean worktree after auto-commit")
	}
	log := runGitTest(t, td, "-C", repoPath, "log", "--format=%s", "-1")
	if log != "deepreview: auto commit" {
		t.Fatalf("unexpected commit message: %s", log)
	}
}
