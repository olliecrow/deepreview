package deepreview

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
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
		// deepreview may run codex from non-repo run directories during delivery.
		"--skip-git-repo-check",
		"--full-auto", "--json", "-",
	)
	return command
}

func (c *CodexRunner) buildEnvironment(cwd string) ([]string, error) {
	sandboxRoot, err := filepath.Abs(filepath.Join(cwd, ".deepreview", "runtime"))
	if err != nil {
		return nil, err
	}
	goCache := filepath.Join(sandboxRoot, "go-build-cache")
	goModCache := filepath.Join(sandboxRoot, "go-mod-cache")
	goTmpDir := filepath.Join(sandboxRoot, "go-tmp")
	tmpDir := filepath.Join(sandboxRoot, "tmp")
	for _, dir := range []string{sandboxRoot, goCache, goModCache, goTmpDir, tmpDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
	}

	return mergeCommandEnv(os.Environ(), map[string]string{
		"GOCACHE":    goCache,
		"GOMODCACHE": goModCache,
		"GOTMPDIR":   goTmpDir,
		"TMPDIR":     tmpDir,
		"TMP":        tmpDir,
		"TEMP":       tmpDir,
	}), nil
}

func mergeCommandEnv(base []string, overrides map[string]string) []string {
	env := make(map[string]string, len(base)+len(overrides))
	order := make([]string, 0, len(base)+len(overrides))
	for _, entry := range base {
		key, value, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		if _, seen := env[key]; !seen {
			order = append(order, key)
		}
		env[key] = value
	}
	overrideKeys := make([]string, 0, len(overrides))
	for key := range overrides {
		overrideKeys = append(overrideKeys, key)
		if _, seen := env[key]; !seen {
			order = append(order, key)
		}
	}
	sort.Strings(overrideKeys)
	for _, key := range overrideKeys {
		env[key] = overrides[key]
	}

	merged := make([]string, 0, len(order))
	for _, key := range order {
		merged = append(merged, key+"="+env[key])
	}
	return merged
}

func (c *CodexRunner) RunPrompt(cwd, prompt string, threadID *string, logPrefixPath string) (CodexRunResult, error) {
	return c.RunPromptWithHooks(cwd, prompt, threadID, logPrefixPath, nil)
}

func (c *CodexRunner) RunPromptWithHooks(cwd, prompt string, threadID *string, logPrefixPath string, hooks *CodexRunHooks) (CodexRunResult, error) {
	if strings.TrimSpace(c.CodexBin) == "" {
		return CodexRunResult{}, NewDeepReviewError("codex binary must be configured")
	}
	command := c.buildCommand(threadID)
	commandEnv, err := c.buildEnvironment(cwd)
	if err != nil {
		return CodexRunResult{}, err
	}

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
	completed, err := RunCommandContextWithEnvAndCallbacks(parentCtx, command, cwd, commandEnv, prompt, true, c.Timeout, streamCallbacks)
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
