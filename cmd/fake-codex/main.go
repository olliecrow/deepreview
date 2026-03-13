package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	envMulticodexActiveProfile       = "MULTICODEX_ACTIVE_PROFILE"
	envMulticodexSelectedProfilePath = "MULTICODEX_SELECTED_PROFILE_PATH"
)

func randomID() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "thread-fallback"
	}
	return hex.EncodeToString(buf)
}

func emitThread(threadID, message string) error {
	events := []map[string]any{
		{"type": "thread.started", "thread_id": threadID},
		{"type": "turn.started"},
		{
			"type": "item.completed",
			"item": map[string]any{
				"id":   "item_0",
				"type": "agent_message",
				"text": message,
			},
		},
		{"type": "turn.completed", "usage": map[string]any{"input_tokens": 1, "output_tokens": 1}},
	}
	enc := json.NewEncoder(os.Stdout)
	for _, e := range events {
		if err := enc.Encode(e); err != nil {
			return err
		}
	}
	return nil
}

func maybeWriteSelectedProfile(profile string) error {
	path := strings.TrimSpace(os.Getenv(envMulticodexSelectedProfilePath))
	if path == "" {
		return nil
	}
	payload := map[string]string{"profile": strings.TrimSpace(profile)}
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o600)
}

// Keep fake multicodex profile handoff identical for exec and run wrappers.
func activateFakeMulticodexProfile(profile string) error {
	if err := maybeWriteSelectedProfile(profile); err != nil {
		return err
	}
	return os.Setenv(envMulticodexActiveProfile, profile)
}

func normalizeFakeLauncherInvocation(args []string) ([]string, error) {
	if !shouldNormalizeFakeLauncherInvocation(args) {
		return args, nil
	}
	if len(args) == 0 {
		return args, nil
	}
	switch args[0] {
	case "status":
		fmt.Println("logged-in profile")
		os.Exit(0)
		return nil, nil
	case "exec":
		profile := strings.TrimSpace(os.Getenv("FAKE_MULTICODEX_SELECTED_PROFILE"))
		if profile == "" {
			profile = "fake-profile"
		}
		if err := activateFakeMulticodexProfile(profile); err != nil {
			return nil, err
		}
		return args[1:], nil
	case "run":
		if len(args) < 5 || args[2] != "--" || args[3] != "codex" {
			return nil, fmt.Errorf("unsupported fake multicodex run invocation: %s", strings.Join(args, " "))
		}
		profile := strings.TrimSpace(args[1])
		if profile == "" {
			return nil, fmt.Errorf("fake multicodex run missing profile")
		}
		if err := activateFakeMulticodexProfile(profile); err != nil {
			return nil, err
		}
		return args[4:], nil
	default:
		return nil, fmt.Errorf("unsupported fake multicodex command: %s", strings.Join(args, " "))
	}
}

func shouldNormalizeFakeLauncherInvocation(args []string) bool {
	if filepath.Base(os.Args[0]) == "multicodex" {
		return true
	}
	if len(args) == 0 {
		return false
	}
	if args[0] == "exec" && strings.TrimSpace(os.Getenv(envMulticodexSelectedProfilePath)) != "" {
		return true
	}
	return args[0] == "run" && len(args) >= 5 && args[2] == "--" && args[3] == "codex"
}

func requirePathWithinCWD(path, label string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	if !pathWithinBase(cwd, path) {
		return fmt.Errorf("%s must stay within cwd: %s", label, path)
	}
	return nil
}

func requirePromptOutputWithinCWD(path, label string) error {
	if strings.TrimSpace(os.Getenv("FAKE_CODEX_REQUIRE_PROMPT_OUTPUTS_WITHIN_CWD")) == "" || path == "" {
		return nil
	}
	return requirePathWithinCWD(path, label)
}

func promptWorktreePath(prompt string) string {
	worktree := regexGet("Worktree path: `([^`]+)`", prompt)
	if worktree != "" {
		return worktree
	}
	return regexGet("Execute worktree: `([^`]+)`", prompt)
}

func requirePromptOutputWithinScope(prompt, path, label string) error {
	if strings.TrimSpace(os.Getenv("FAKE_CODEX_REQUIRE_PROMPT_OUTPUTS_WITHIN_CWD")) == "" || path == "" {
		return nil
	}
	if filepath.IsAbs(path) {
		return nil
	}
	if err := requirePathWithinCWD(path, label); err == nil {
		return nil
	}
	worktree := strings.TrimSpace(promptWorktreePath(prompt))
	if worktree == "" || !pathWithinBase(worktree, path) {
		return fmt.Errorf("%s must stay within cwd or declared worktree: %s", label, path)
	}
	return nil
}

func writeText(path, text string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(text), 0o644)
}

func regexGet(pattern, prompt string) string {
	re := regexp.MustCompile(pattern)
	m := re.FindStringSubmatch(prompt)
	if len(m) < 2 {
		return ""
	}
	return m[1]
}

func roundNumberFromPrompt(prompt string) int {
	raw := regexGet("round `([0-9]+)`", prompt)
	if raw == "" {
		raw = regexGet("Round: `([0-9]+)` / max", prompt)
	}
	if raw == "" {
		return 0
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 1 {
		return 0
	}
	return value
}

func csvSequenceValue(raw string, index int) string {
	if index < 1 {
		return ""
	}
	parts := strings.Split(raw, ",")
	if index > len(parts) {
		return ""
	}
	return strings.TrimSpace(parts[index-1])
}

func csvContainsRound(raw string, round int) bool {
	if round < 1 {
		return false
	}
	for _, item := range strings.Split(raw, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		value, err := strconv.Atoi(item)
		if err != nil {
			continue
		}
		if value == round {
			return true
		}
	}
	return false
}

func runGit(args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func gitCommitIfPossible(message string) error {
	if _, err := runGit("add", "-A"); err != nil {
		return err
	}
	statusOut, err := runGit("status", "--porcelain")
	if err != nil {
		return err
	}
	if strings.TrimSpace(statusOut) == "" {
		return nil
	}
	if _, err := runGit("commit", "-m", message); err != nil {
		return err
	}
	return nil
}

func gitPushRefspecIfPossible(refspec string) error {
	if strings.TrimSpace(refspec) == "" {
		return fmt.Errorf("empty refspec")
	}
	_, err := runGit("push", "origin", refspec)
	return err
}

func ghBin() string {
	if candidate := strings.TrimSpace(os.Getenv("DEEPREVIEW_GH_BIN")); candidate != "" {
		return candidate
	}
	return "gh"
}

func fakeAWSAccessKey() string {
	return "AKIA" + strings.Repeat("A", 8) + strings.Repeat("B", 8)
}

func handlePrompt(prompt string) (string, error) {
	if strings.Contains(prompt, "independent deepreview reviewer in the independent review stage") {
		outPath := regexGet("Output report path: `([^`]+)`", prompt)
		if err := requirePromptOutputWithinScope(prompt, outPath, "review output path"); err != nil {
			return "", err
		}
		if outPath != "" {
			report := "# Independent Review 1\n\n## Verdict\n- `material_findings_found: no`\n- `merge_readiness: ready`\n- `No high-confidence material findings were found.`\n\n## Material Findings\n\n## Verification ideas\n- no additional checks suggested by fake codex\n"
			if err := writeText(outPath, report); err != nil {
				return "", err
			}
		}
		return "review complete", nil
	}

	if strings.Contains(prompt, "prompt 1 of 2") || strings.Contains(prompt, "prompt 1 of 3") {
		triage := regexGet("Write triage decisions to `([^`]+)`", prompt)
		if triage == "" {
			triage = regexGet("Triage output path: `([^`]+)`", prompt)
		}
		if triage == "" {
			triage = filepath.Join(".", ".deepreview", "artifacts", "round-triage.md")
		}
		if err := requirePromptOutputWithinScope(prompt, triage, "triage output path"); err != nil {
			return "", err
		}
		if triage != "" {
			triageText := "# Round Triage\n\n### sample accepted change\n- source reviewers: 1\n- commonality count: 1\n- disposition: accept\n- impact: material\n- confidence: high\n- evidence summary: fake codex validated a sample change path\n- rationale: used by integration tests\n\n## accepted items\n- sample accepted change\n"
			if strings.TrimSpace(os.Getenv("FAKE_CODEX_SKIP_ACCEPTED_TRIAGE")) != "" {
				triageText = "# Round Triage\n\nNo execute items selected for this round.\n"
			}
			if err := writeText(triage, triageText); err != nil {
				return "", err
			}
		}
		plan := regexGet("Write the plan to `([^`]+)`", prompt)
		if plan == "" {
			plan = regexGet("Plan output path: `([^`]+)`", prompt)
		}
		if plan == "" {
			plan = filepath.Join(".", ".deepreview", "artifacts", "round-plan.md")
		}
		if err := requirePromptOutputWithinScope(prompt, plan, "plan output path"); err != nil {
			return "", err
		}
		if plan != "" {
			if err := writeText(plan, "# Plan\n\n## scope\n- fake execute scope\n\n## task list\n- apply sample change\n\n## complexity/size impact\n- neutral\n\n## verification matrix\n- fake checks\n\n## docs/notes/decision updates\n- none\n\n## risks and mitigations\n- none\n\n## stop conditions\n- stop when fake checks pass\n"); err != nil {
				return "", err
			}
		}
		return "triage and plan complete", nil
	}

	if strings.Contains(prompt, "prompt 2 of 2") || strings.Contains(prompt, "prompt 2 of 3") {
		roundNumber := roundNumberFromPrompt(prompt)
		verification := regexGet("Write verification evidence to `([^`]+)`", prompt)
		if verification == "" {
			verification = filepath.Join(".", ".deepreview", "artifacts", "round-verification.md")
		}
		if err := requirePromptOutputWithinScope(prompt, verification, "verification output path"); err != nil {
			return "", err
		}
		if verification != "" {
			if err := writeText(verification, "# Verification\n\n- commands attempted: fake checks\n- pass/fail outcomes: passed\n- checks skipped with reason: none\n- unresolved failures or blockers: none\n- residual risks: none\n"); err != nil {
				return "", err
			}
		}
		if strings.TrimSpace(os.Getenv("FAKE_CODEX_WRITE_OPERATIONAL_TMP")) != "" {
			if err := writeText(filepath.Join(".", ".tmp", "go-build-cache", "synthetic-cache.txt"), "cache\n"); err != nil {
				return "", err
			}
		}
		if strings.TrimSpace(os.Getenv("FAKE_CODEX_FORCE_ADD_OPERATIONAL_TMP")) != "" {
			target := filepath.Join(".", ".tmp", "go-build-cache", "forced-cache.txt")
			if err := writeText(target, "forced cache\n"); err != nil {
				return "", err
			}
			if _, err := runGit("add", "-f", filepath.ToSlash(target)); err != nil {
				return "", err
			}
			if _, err := runGit("commit", "-m", "deepreview: force add operational tmp"); err != nil {
				return "", err
			}
		}
		if strings.TrimSpace(os.Getenv("FAKE_CODEX_ADD_REPO_TMP_FILE")) != "" {
			if err := writeText(filepath.Join(".", ".tmp", "repo-added.txt"), "repo tmp file\n"); err != nil {
				return "", err
			}
		}
		if strings.TrimSpace(os.Getenv("FAKE_CODEX_RUN_GO_TEST_WITH_INHERITED_ENV")) != "" {
			if err := runGo("test", "./..."); err != nil {
				return "", err
			}
		}
		auditOnly := strings.Contains(prompt, "automatic final audit round")
		if auditOnly {
			if strings.TrimSpace(os.Getenv("FAKE_CODEX_AUDIT_WRITE_FILE_CHANGE")) != "" {
				if err := writeText(filepath.Join(".", "audit_round_change.txt"), "audit change\n"); err != nil {
					return "", err
				}
			}
			if strings.TrimSpace(os.Getenv("FAKE_CODEX_AUDIT_ALLOW_EMPTY_COMMIT")) != "" {
				if _, err := runGit("commit", "--allow-empty", "-m", "audit empty commit"); err != nil {
					return "", err
				}
			}
		}
		skipCodeChange := strings.TrimSpace(os.Getenv("FAKE_CODEX_SKIP_CODE_CHANGE")) != ""
		if !skipCodeChange && roundNumber > 0 {
			skipCodeChange = csvContainsRound(os.Getenv("FAKE_CODEX_SKIP_CODE_CHANGE_ROUNDS"), roundNumber)
		}
		if !skipCodeChange && !auditOnly {
			changeContent := "round change\n"
			changePath := filepath.Join(".", "deepreview_test_round.txt")
			if strings.TrimSpace(os.Getenv("FAKE_CODEX_CHANGE_CONTENT_BY_ROUND")) != "" && roundNumber > 0 {
				changeContent = fmt.Sprintf("round %d change\n", roundNumber)
			}
			if strings.TrimSpace(os.Getenv("FAKE_CODEX_WRITE_LOCAL_PATH_CHANGE")) != "" {
				changeContent = "path /" + strings.Join([]string{"Users", "fake-user", "private", "project"}, "/") + "\n"
			}
			if strings.TrimSpace(os.Getenv("FAKE_CODEX_WRITE_SECRET_PATTERN_CHANGE")) != "" {
				changeContent = "key " + fakeAWSAccessKey() + "\n"
			}
			if strings.TrimSpace(os.Getenv("FAKE_CODEX_WRITE_BINARY_SECRET_PATTERN_CHANGE")) != "" {
				changePath = filepath.Join(".", "secret.bin")
				changeContent = "prefix\x00" + fakeAWSAccessKey() + "\x00suffix"
			}
			if strings.TrimSpace(os.Getenv("FAKE_CODEX_WRITE_DOC_LOCAL_PATH_CHANGE")) != "" {
				changePath = filepath.Join(".", "docs", "generated.md")
				changeContent = "path /" + strings.Join([]string{"Users", "fake-user", "private", "project"}, "/") + "\n"
			}
			if err := writeText(changePath, changeContent); err != nil {
				return "", err
			}
		}
		summary := regexGet("Write round summary to `([^`]+)`", prompt)
		if summary == "" {
			summary = regexGet("Round summary output path: `([^`]+)`", prompt)
		}
		if summary == "" {
			summary = filepath.Join(".", ".deepreview", "artifacts", "round-summary.md")
		}
		if err := requirePromptOutputWithinScope(prompt, summary, "summary output path"); err != nil {
			return "", err
		}
		if summary != "" {
			if err := writeText(summary, "# Round Summary\n\n- accepted/rejected/deferred triage outcomes: fake accept\n- implemented changes: fake execute update\n- verification evidence overview: fake checks passed\n- residual risks: none\n- complexity/size impact: neutral\n- strict-scope statement: accepted work remained material/high-confidence only\n"); err != nil {
				return "", err
			}
		}

		statusPath := regexGet("Write `([^`]+)` JSON", prompt)
		if statusPath == "" {
			statusPath = regexGet("Round status output path: `([^`]+)`", prompt)
		}
		if statusPath == "" {
			statusPath = filepath.Join(".", ".deepreview", "artifacts", "round-status.json")
		}
		if err := requirePromptOutputWithinScope(prompt, statusPath, "status output path"); err != nil {
			return "", err
		}
		if statusPath != "" {
			decision := strings.TrimSpace(csvSequenceValue(os.Getenv("FAKE_CODEX_DECISION_SEQUENCE"), roundNumber))
			if decision == "" {
				decision = os.Getenv("FAKE_CODEX_DECISION")
			}
			if decision == "" {
				decision = "stop"
			}
			statusText := ""
			if strings.TrimSpace(os.Getenv("FAKE_CODEX_WRITE_INVALID_ROUND_STATUS")) != "" {
				statusText = "{invalid json}\n"
			} else {
				payload := map[string]any{"decision": decision, "reason": "ready"}
				b, _ := json.MarshalIndent(payload, "", "  ")
				statusText = string(b) + "\n"
			}
			if err := writeText(statusPath, statusText); err != nil {
				return "", err
			}
		}
		commitMessage := strings.TrimSpace(os.Getenv("FAKE_CODEX_CHANGE_COMMIT_MESSAGE"))
		if commitMessage == "" {
			commitMessage = "deepreview: fake execute change"
		}
		if err := gitCommitIfPossible(commitMessage); err != nil {
			return "", err
		}
		return "implement, verify, finalize complete", nil
	}

	if strings.Contains(prompt, "deepreview final delivery stage") {
		mode := regexGet("Mode: `([^`]+)`", prompt)
		deliveryBranch := regexGet("Delivery branch: `([^`]+)`", prompt)
		resultPath := regexGet("Output result path: `([^`]+)`", prompt)
		if err := requirePromptOutputWithinScope(prompt, resultPath, "delivery result path"); err != nil {
			return "", err
		}
		if strings.TrimSpace(os.Getenv("FAKE_CODEX_DELIVERY_WRITE_FILE")) != "" || strings.TrimSpace(os.Getenv("FAKE_CODEX_PR_PREP_WRITE_FILE")) != "" {
			if err := writeText(filepath.Join(".", "delivery-ready.txt"), "delivery\n"); err != nil {
				return "", err
			}
		}
		if strings.TrimSpace(os.Getenv("FAKE_CODEX_WRITE_DOC_LOCAL_PATH_CHANGE")) != "" {
			docPath := filepath.Join(".", "docs", "generated.md")
			content, err := os.ReadFile(docPath)
			if err == nil {
				sourcePath := filepath.ToSlash(filepath.Join(string(os.PathSeparator)+"Users", "fake-user", "private", "project"))
				sanitized := strings.ReplaceAll(string(content), sourcePath, "/path/to/project")
				if err := writeText(docPath, sanitized); err != nil {
					return "", err
				}
			}
		}
		if strings.TrimSpace(os.Getenv("FAKE_CODEX_PR_PREP_DELETE_ROUND_FILE")) != "" {
			if err := os.Remove(filepath.Join(".", "deepreview_test_round.txt")); err != nil && !os.IsNotExist(err) {
				return "", err
			}
		}
		preparedBranch := ""
		if mode == "pr" && strings.TrimSpace(os.Getenv("FAKE_CODEX_DELIVERY_CREATE_BRANCH")) != "" {
			if _, err := runGit("checkout", "-B", deliveryBranch); err != nil {
				return "", err
			}
			preparedBranch = deliveryBranch
		}
		if mode == "pr" && strings.TrimSpace(os.Getenv("FAKE_CODEX_DELIVERY_BRANCH_SECRET")) != "" {
			if preparedBranch == "" {
				if _, err := runGit("checkout", "-B", deliveryBranch); err != nil {
					return "", err
				}
				preparedBranch = deliveryBranch
			}
			if err := writeText(filepath.Join(".", "delivery_branch_secret.txt"), "key "+fakeAWSAccessKey()+"\n"); err != nil {
				return "", err
			}
		}
		if err := gitCommitIfPossible("deepreview: prepare delivery branch"); err != nil {
			return "", err
		}
		if mode == "pr" {
			payload := map[string]any{
				"mode":       mode,
				"incomplete": false,
			}
			if preparedBranch != "" {
				payload["delivery_branch"] = preparedBranch
			}
			if strings.TrimSpace(os.Getenv("FAKE_CODEX_DELIVERY_INCOMPLETE")) != "" {
				payload["incomplete"] = true
				reason := strings.TrimSpace(os.Getenv("FAKE_CODEX_DELIVERY_INCOMPLETE_REASON"))
				if reason == "" {
					reason = "fake delivery blocked on mergeability"
				}
				payload["incomplete_reason"] = reason
			}
			b, _ := json.MarshalIndent(payload, "", "  ")
			if err := writeText(resultPath, string(b)+"\n"); err != nil {
				return "", err
			}
			return "delivery complete", nil
		}
		payload := map[string]any{
			"mode":       mode,
			"incomplete": false,
		}
		b, _ := json.MarshalIndent(payload, "", "  ")
		if err := writeText(resultPath, string(b)+"\n"); err != nil {
			return "", err
		}
		return "delivery complete", nil
	}

	return "ok", nil
}

func workerIDFromPrompt(prompt string) int {
	raw := regexGet("Worker id: `([0-9]+)`", prompt)
	if raw == "" {
		return 0
	}
	id, err := strconv.Atoi(raw)
	if err != nil || id < 1 {
		return 0
	}
	return id
}

func pathWithinBase(basePath, targetPath string) bool {
	baseAbs, err := filepath.Abs(basePath)
	if err != nil {
		return false
	}
	if resolved, err := filepath.EvalSymlinks(baseAbs); err == nil {
		baseAbs = resolved
	}
	targetAbs, err := filepath.Abs(targetPath)
	if err != nil {
		return false
	}
	if resolved, err := filepath.EvalSymlinks(targetAbs); err == nil {
		targetAbs = resolved
	}
	rel, err := filepath.Rel(baseAbs, targetAbs)
	if err != nil {
		return false
	}
	return rel == "." || (!strings.HasPrefix(rel, ".."+string(os.PathSeparator)) && rel != "..")
}

func sleepFromEnv(raw string) bool {
	if strings.TrimSpace(raw) == "" {
		return false
	}
	ms, err := strconv.Atoi(raw)
	if err != nil || ms <= 0 {
		return false
	}
	time.Sleep(time.Duration(ms) * time.Millisecond)
	return true
}

func maybeSleepOnceByMarker(markerPath, sleepRaw string) bool {
	if !sleepFromEnvValueEnabled(sleepRaw) {
		return false
	}
	if _, err := os.Stat(markerPath); err == nil {
		return false
	}
	if err := os.MkdirAll(filepath.Dir(markerPath), 0o755); err != nil {
		return false
	}
	if err := os.WriteFile(markerPath, []byte(time.Now().UTC().Format(time.RFC3339Nano)+"\n"), 0o644); err != nil {
		return false
	}
	return sleepFromEnv(sleepRaw)
}

func stableMarkerPath(name string) string {
	root := strings.TrimSpace(os.Getenv("FAKE_CODEX_MARKER_ROOT"))
	if root == "" {
		cwd, err := os.Getwd()
		if err != nil {
			root = filepath.Join(os.TempDir(), "fake-codex-markers", "unknown")
		} else {
			sum := sha256.Sum256([]byte(cwd))
			root = filepath.Join(os.TempDir(), "fake-codex-markers", hex.EncodeToString(sum[:8]))
		}
	}
	return filepath.Join(root, name)
}

func sleepFromEnvValueEnabled(raw string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return false
	}
	ms, err := strconv.Atoi(raw)
	return err == nil && ms > 0
}

func sleepForPrompt(prompt string) {
	if contains := strings.TrimSpace(os.Getenv("FAKE_CODEX_STALL_ONCE_CONTAINS")); contains != "" && strings.Contains(prompt, contains) {
		matchSleepRaw := strings.TrimSpace(os.Getenv("FAKE_CODEX_STALL_ONCE_MS_MATCH"))
		if matchSleepRaw == "" {
			matchSleepRaw = os.Getenv("FAKE_CODEX_STALL_ONCE_MS")
		}
		if maybeSleepOnceByMarker(stableMarkerPath("fake-codex-stall-once-match.marker"), matchSleepRaw) {
			return
		}
	}

	workerID := workerIDFromPrompt(prompt)
	if workerID > 0 {
		stallKey := fmt.Sprintf("FAKE_CODEX_STALL_ONCE_MS_WORKER_%d", workerID)
		stallMarker := stableMarkerPath(fmt.Sprintf("fake-codex-stall-once-worker-%02d.marker", workerID))
		if maybeSleepOnceByMarker(stallMarker, os.Getenv(stallKey)) {
			return
		}
		key := fmt.Sprintf("FAKE_CODEX_SLEEP_MS_WORKER_%d", workerID)
		if sleepFromEnv(os.Getenv(key)) {
			return
		}
	}
	if maybeSleepOnceByMarker(stableMarkerPath("fake-codex-stall-once-global.marker"), os.Getenv("FAKE_CODEX_STALL_ONCE_MS")) {
		return
	}
	if sleepFromEnv(os.Getenv("FAKE_CODEX_SLEEP_MS")) {
		return
	}
}

func hasArg(args []string, target string) bool {
	for _, arg := range args {
		if arg == target {
			return true
		}
	}
	return false
}

func runGo(args ...string) error {
	cmd := exec.Command("go", args...)
	// Keep stdout reserved for Codex JSON events so inherited go-tool output
	// cannot corrupt the event stream seen by the orchestrator.
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()
	return cmd.Run()
}

func requireSelfAuditReviewPrompt(prompt string) error {
	sourceBranch := regexGet("Source branch: `([^`]+)`", prompt)
	defaultBranch := regexGet("Default branch: `([^`]+)`", prompt)
	if sourceBranch == "" || defaultBranch == "" || sourceBranch != defaultBranch {
		return nil
	}
	if !strings.Contains(prompt, "Review mode: `current-state repository audit`") {
		return fmt.Errorf("self-audit review prompt missing current-state review mode")
	}
	if !strings.Contains(prompt, "Treat branch-diff inspection as orientation only") {
		return fmt.Errorf("self-audit review prompt missing branch-diff orientation note")
	}
	return nil
}

func main() {
	promptBytes, err := io.ReadAll(os.Stdin)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	prompt := string(promptBytes)
	sleepForPrompt(prompt)

	threadID := randomID()
	args := os.Args[1:]
	args, err = normalizeFakeLauncherInvocation(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if strings.TrimSpace(os.Getenv("FAKE_CODEX_REQUIRE_SKIP_GIT_REPO_CHECK")) != "" && !hasArg(args, "--skip-git-repo-check") {
		fmt.Fprintln(os.Stderr, "missing required --skip-git-repo-check")
		os.Exit(1)
	}
	if strings.TrimSpace(os.Getenv("FAKE_CODEX_REQUIRE_SELF_AUDIT_REVIEW_PROMPT")) != "" && strings.Contains(prompt, "independent deepreview reviewer in the independent review stage") {
		if err := requireSelfAuditReviewPrompt(prompt); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
	if len(args) >= 3 && args[0] == "exec" && args[1] == "resume" {
		threadID = args[2]
	}

	message, err := handlePrompt(prompt)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := emitThread(threadID, message); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
