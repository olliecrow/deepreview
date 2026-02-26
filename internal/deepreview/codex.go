package deepreview

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type CodexRunner struct {
	CodexBin   string
	CodexModel string
	Reasoning  string
	Timeout    time.Duration
}

func (c *CodexRunner) buildCommand(threadID *string) []string {
	// Hard-pin model and reasoning for every Codex invocation.
	reasoningConfig := fmt.Sprintf("model_reasoning_effort=%q", forcedCodexReasoningEffort)
	command := []string{c.CodexBin, "exec"}
	if threadID != nil && *threadID != "" {
		command = append(command, "resume", *threadID)
	}
	command = append(command,
		"--model", forcedCodexModel,
		"-c", reasoningConfig,
		"--full-auto", "--json", "-",
	)
	return command
}

func (c *CodexRunner) RunPrompt(cwd, prompt string, threadID *string, logPrefixPath string) (CodexRunResult, error) {
	if strings.TrimSpace(c.CodexBin) == "" {
		return CodexRunResult{}, NewDeepReviewError("codex binary must be configured")
	}
	command := c.buildCommand(threadID)

	completed, err := RunCommand(command, cwd, prompt, true, c.Timeout)
	if err != nil {
		return CodexRunResult{}, err
	}

	stdoutPath := logPrefixPath + ".stdout.jsonl"
	stderrPath := logPrefixPath + ".stderr.log"
	if err := os.MkdirAll(filepath.Dir(stdoutPath), 0o755); err != nil {
		return CodexRunResult{}, err
	}
	if err := os.WriteFile(stdoutPath, []byte(completed.Stdout), 0o644); err != nil {
		return CodexRunResult{}, err
	}
	if err := os.WriteFile(stderrPath, []byte(completed.Stderr), 0o644); err != nil {
		return CodexRunResult{}, err
	}

	discoveredThreadID := ""
	if threadID != nil {
		discoveredThreadID = *threadID
	}
	agentMessages := make([]string, 0)

	for _, raw := range strings.Split(completed.Stdout, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		var event map[string]any
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			return CodexRunResult{}, NewDeepReviewError("failed parsing codex event json: %v", err)
		}

		eventType, _ := event["type"].(string)
		if eventType == "thread.started" {
			if tid, ok := event["thread_id"].(string); ok && tid != "" {
				discoveredThreadID = tid
			}
		}
		if eventType == "item.completed" {
			item, ok := event["item"].(map[string]any)
			if !ok {
				continue
			}
			itype, _ := item["type"].(string)
			if itype != "agent_message" {
				continue
			}
			if text, ok := item["text"].(string); ok && text != "" {
				agentMessages = append(agentMessages, text)
			}
		}
	}

	if discoveredThreadID == "" {
		return CodexRunResult{}, NewDeepReviewError("codex run did not emit a thread id")
	}

	return CodexRunResult{
		ThreadID:      discoveredThreadID,
		AgentMessages: agentMessages,
		Stdout:        completed.Stdout,
		Stderr:        completed.Stderr,
	}, nil
}
