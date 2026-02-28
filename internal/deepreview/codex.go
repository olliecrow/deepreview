package deepreview

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type CodexRunner struct {
	CodexBin   string
	CodexModel string
	Reasoning  string
	Timeout    time.Duration
}

type CodexRunHooks struct {
	Context       context.Context
	OnStdoutChunk func(chunk []byte)
	OnStderrChunk func(chunk []byte)
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
	return c.RunPromptWithHooks(cwd, prompt, threadID, logPrefixPath, nil)
}

func (c *CodexRunner) RunPromptWithHooks(cwd, prompt string, threadID *string, logPrefixPath string, hooks *CodexRunHooks) (CodexRunResult, error) {
	if strings.TrimSpace(c.CodexBin) == "" {
		return CodexRunResult{}, NewDeepReviewError("codex binary must be configured")
	}
	command := c.buildCommand(threadID)

	stdoutPath := logPrefixPath + ".stdout.jsonl"
	stderrPath := logPrefixPath + ".stderr.log"
	if err := os.MkdirAll(filepath.Dir(stdoutPath), 0o755); err != nil {
		return CodexRunResult{}, err
	}
	stdoutFile, err := os.Create(stdoutPath)
	if err != nil {
		return CodexRunResult{}, err
	}
	defer stdoutFile.Close()
	stderrFile, err := os.Create(stderrPath)
	if err != nil {
		return CodexRunResult{}, err
	}
	defer stderrFile.Close()

	var streamMu sync.Mutex
	var streamErr error
	setStreamErr := func(err error) {
		if err == nil {
			return
		}
		streamMu.Lock()
		if streamErr == nil {
			streamErr = err
		}
		streamMu.Unlock()
	}
	streamCallbacks := &CommandStreamCallbacks{
		OnStdoutChunk: func(chunk []byte) {
			streamMu.Lock()
			_, err := stdoutFile.Write(chunk)
			streamMu.Unlock()
			setStreamErr(err)
			if hooks != nil && hooks.OnStdoutChunk != nil {
				hooks.OnStdoutChunk(chunk)
			}
		},
		OnStderrChunk: func(chunk []byte) {
			streamMu.Lock()
			_, err := stderrFile.Write(chunk)
			streamMu.Unlock()
			setStreamErr(err)
			if hooks != nil && hooks.OnStderrChunk != nil {
				hooks.OnStderrChunk(chunk)
			}
		},
	}
	parentCtx := context.Background()
	if hooks != nil && hooks.Context != nil {
		parentCtx = hooks.Context
	}
	completed, err := RunCommandContextWithCallbacks(parentCtx, command, cwd, prompt, true, c.Timeout, streamCallbacks)
	if err != nil {
		return CodexRunResult{}, err
	}
	streamMu.Lock()
	capturedStreamErr := streamErr
	streamMu.Unlock()
	if capturedStreamErr != nil {
		return CodexRunResult{}, capturedStreamErr
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
