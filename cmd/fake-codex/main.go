package main

import (
	"crypto/rand"
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

func handlePrompt(prompt string) (string, error) {
	if strings.Contains(prompt, "independent deepreview reviewer in the independent review stage") {
		outPath := regexGet("Output report path: `([^`]+)`", prompt)
		if err := requirePromptOutputWithinScope(prompt, outPath, "review output path"); err != nil {
			return "", err
		}
		if outPath != "" {
			if err := writeText(outPath, "# Independent Review\n\n## Findings\n- no actionable findings\n"); err != nil {
				return "", err
			}
		}
		return "review complete", nil
	}

	if strings.Contains(prompt, "prompt 1 of 3") {
		triage := regexGet("Write triage decisions to `([^`]+)`", prompt)
		if triage == "" {
			triage = regexGet("Triage output path: `([^`]+)`", prompt)
		}
		if err := requirePromptOutputWithinScope(prompt, triage, "triage output path"); err != nil {
			return "", err
		}
		if triage != "" {
			if err := writeText(triage, "# Triage\n\n- accept: sample change\n"); err != nil {
				return "", err
			}
		}
		plan := regexGet("Write the plan to `([^`]+)`", prompt)
		if plan == "" {
			plan = regexGet("Plan output path: `([^`]+)`", prompt)
		}
		if err := requirePromptOutputWithinScope(prompt, plan, "plan output path"); err != nil {
			return "", err
		}
		if plan != "" {
			if err := writeText(plan, "# Plan\n\n- apply sample change\n"); err != nil {
				return "", err
			}
		}
		return "triage and plan complete", nil
	}

	if strings.Contains(prompt, "prompt 2 of 3") {
		roundNumber := roundNumberFromPrompt(prompt)
		verification := regexGet("Write verification evidence to `([^`]+)`", prompt)
		if err := requirePromptOutputWithinScope(prompt, verification, "verification output path"); err != nil {
			return "", err
		}
		if verification != "" {
			if err := writeText(verification, "# Verification\n\n- fake checks passed\n"); err != nil {
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
				changeContent = "key " + "AKIA" + "ABCDEFGHIJKLMNOP" + "\n"
			}
			if strings.TrimSpace(os.Getenv("FAKE_CODEX_WRITE_BINARY_SECRET_PATTERN_CHANGE")) != "" {
				changePath = filepath.Join(".", "secret.bin")
				changeContent = "prefix\x00" + "AKIA" + "ABCDEFGHIJKLMNOP" + "\x00suffix"
			}
			if strings.TrimSpace(os.Getenv("FAKE_CODEX_WRITE_DOC_LOCAL_PATH_CHANGE")) != "" {
				changePath = filepath.Join(".", "docs", "generated.md")
				changeContent = "path /" + strings.Join([]string{"Users", "fake-user", "private", "project"}, "/") + "\n"
			}
			if err := writeText(changePath, changeContent); err != nil {
				return "", err
			}
			commitMessage := strings.TrimSpace(os.Getenv("FAKE_CODEX_CHANGE_COMMIT_MESSAGE"))
			if commitMessage == "" {
				commitMessage = "deepreview: fake execute change"
			}
			if err := gitCommitIfPossible(commitMessage); err != nil {
				return "", err
			}
		}
		return "execute complete", nil
	}

	if strings.Contains(prompt, "prompt 3 of 3") {
		roundNumber := roundNumberFromPrompt(prompt)
		summary := regexGet("Write round summary to `([^`]+)`", prompt)
		if err := requirePromptOutputWithinScope(prompt, summary, "summary output path"); err != nil {
			return "", err
		}
		if summary != "" {
			if err := writeText(summary, "# Round Summary\n\n- complete\n"); err != nil {
				return "", err
			}
		}

		statusPath := regexGet("Write `([^`]+)` JSON", prompt)
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
		return "finalize complete", nil
	}

	if strings.Contains(prompt, "pre-delivery PR preparation stage") {
		if strings.TrimSpace(os.Getenv("FAKE_CODEX_PR_PREP_WRITE_FILE")) != "" {
			if err := writeText(filepath.Join(".", "pr-prepare.txt"), "prepared\n"); err != nil {
				return "", err
			}
		}
		if strings.TrimSpace(os.Getenv("FAKE_CODEX_PR_PREP_DELETE_ROUND_FILE")) != "" {
			if err := os.Remove(filepath.Join(".", "deepreview_test_round.txt")); err != nil && !os.IsNotExist(err) {
				return "", err
			}
		}
		if strings.TrimSpace(os.Getenv("FAKE_CODEX_PR_PREP_STAGE_ALL")) != "" {
			if err := gitCommitIfPossible("deepreview: prepare delivery branch"); err != nil {
				return "", err
			}
		}
		return "pr preparation complete", nil
	}

	if strings.Contains(prompt, "post-delivery PR description enhancement stage") {
		titlePath := regexGet("Output title path: `([^`]+)`", prompt)
		if titlePath != "" {
			title := "deepreview: tighten diagnostics and summarize round outcomes clearly"
			if err := writeText(titlePath, title+"\n"); err != nil {
				return "", err
			}
		}
		outPath := regexGet("Output summary path: `([^`]+)`", prompt)
		if outPath != "" {
			content := "## summary\n- top summary generated by fake codex\n\n## what changed and why\n- execute outputs were reviewed and summarized\n\n## round outcomes\n- round-01: stop, no additional high-conviction issues remained\n\n## verification\n- fake checks passed in execute stage\n\n## risks and follow-ups\n- no material follow-ups identified in this fake flow\n\n## final status\n- pr body enhancement complete\n"
			if err := writeText(outPath, content); err != nil {
				return "", err
			}
		}
		return "pr description summary complete", nil
	}

	if strings.Contains(prompt, "pre-delivery privacy remediation stage") {
		statusPath := regexGet("Output status path: `([^`]+)`", prompt)
		if strings.TrimSpace(os.Getenv("FAKE_CODEX_PRIVACY_WRITE_UNCOMMITTED_FILE")) != "" {
			if err := writeText(filepath.Join(".", "privacy-fix-dirty.txt"), "dirty remediation\n"); err != nil {
				return "", err
			}
		}
		if strings.TrimSpace(os.Getenv("FAKE_CODEX_PRIVACY_SANITIZE_BINARY_UNCOMMITTED")) != "" {
			if err := writeText(filepath.Join(".", "secret.bin"), "prefix\x00clean\x00suffix"); err != nil {
				return "", err
			}
		}
		if statusPath != "" {
			if strings.TrimSpace(os.Getenv("FAKE_CODEX_REQUIRE_PRIVACY_STATUS_WITHIN_CWD")) != "" {
				cwd, err := os.Getwd()
				if err != nil {
					return "", err
				}
				if !pathWithinBase(cwd, statusPath) {
					return "", fmt.Errorf("privacy status path must stay within cwd: %s", statusPath)
				}
			}
			decision := strings.TrimSpace(os.Getenv("FAKE_CODEX_PRIVACY_DECISION"))
			if decision == "" {
				decision = "continue"
			}
			payload := map[string]any{
				"decision":   decision,
				"reason":     "fake privacy remediation status",
				"confidence": 0.9,
			}
			b, _ := json.MarshalIndent(payload, "", "  ")
			if err := writeText(statusPath, string(b)+"\n"); err != nil {
				return "", err
			}
		}
		if strings.TrimSpace(os.Getenv("FAKE_CODEX_PRIVACY_STAGE_ALL")) != "" {
			if err := gitCommitIfPossible("deepreview: privacy remediation attempt"); err != nil {
				return "", err
			}
		}
		return "privacy remediation complete", nil
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
		if maybeSleepOnceByMarker(filepath.Join(".deepreview", "fake-codex-stall-once-match.marker"), matchSleepRaw) {
			return
		}
	}

	workerID := workerIDFromPrompt(prompt)
	if workerID > 0 {
		stallKey := fmt.Sprintf("FAKE_CODEX_STALL_ONCE_MS_WORKER_%d", workerID)
		stallMarker := filepath.Join(".deepreview", fmt.Sprintf("fake-codex-stall-once-worker-%02d.marker", workerID))
		if maybeSleepOnceByMarker(stallMarker, os.Getenv(stallKey)) {
			return
		}
		key := fmt.Sprintf("FAKE_CODEX_SLEEP_MS_WORKER_%d", workerID)
		if sleepFromEnv(os.Getenv(key)) {
			return
		}
	}
	if maybeSleepOnceByMarker(filepath.Join(".deepreview", "fake-codex-stall-once-global.marker"), os.Getenv("FAKE_CODEX_STALL_ONCE_MS")) {
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
