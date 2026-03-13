package deepreview

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const envRequireMulticodex = "DEEPREVIEW_REQUIRE_MULTICODEX"
const envMulticodexSelectedProfilePath = "MULTICODEX_SELECTED_PROFILE_PATH"

type CodexRunner struct {
	CodexBin string
	Timeout  time.Duration
}

type codexLauncher struct {
	Command string
	Args    []string
	Display string
}

func (l codexLauncher) withArgs(args ...string) []string {
	command := make([]string, 0, 1+len(l.Args)+len(args))
	command = append(command, l.Command)
	command = append(command, l.Args...)
	command = append(command, args...)
	return command
}

type CodexRunHooks struct {
	Context       context.Context
	OnStdoutChunk func(chunk []byte)
	OnStderrChunk func(chunk []byte)
}

func (c *CodexRunner) buildCommand(ctx *CodexContext) []string {
	return c.buildCommandWithLauncher(codexLauncher{Command: c.CodexBin, Display: "codex"}, ctx)
}

func (c *CodexRunner) buildCommandWithLauncher(launcher codexLauncher, ctx *CodexContext) []string {
	command := launcher.withArgs("exec")
	if ctx != nil && strings.TrimSpace(ctx.ThreadID) != "" {
		command = append(command, "resume", ctx.ThreadID)
	}
	command = append(command,
		// deepreview may run codex from non-repo run directories during delivery.
		"--skip-git-repo-check",
		"--json", "-",
	)
	return command
}

func (c *CodexRunner) resolveLauncher() (codexLauncher, error) {
	if multicodexPath, err := exec.LookPath("multicodex"); err == nil {
		return codexLauncher{Command: multicodexPath, Display: "multicodex"}, nil
	}
	if requireMulticodex() {
		return codexLauncher{}, NewDeepReviewError("%s is set but multicodex is not available on PATH", envRequireMulticodex)
	}

	codexBin := strings.TrimSpace(c.CodexBin)
	if codexBin == "" {
		codexBin = "codex"
	}
	codexPath, err := exec.LookPath(codexBin)
	if err != nil {
		return codexLauncher{}, NewDeepReviewError("resolve codex executable: %v", err)
	}
	return codexLauncher{Command: codexPath, Display: "codex"}, nil
}

func (c *CodexRunner) resolveLauncherForContext(ctx *CodexContext) (codexLauncher, error) {
	if ctx != nil && strings.TrimSpace(ctx.MulticodexProfile) != "" {
		multicodexPath, err := exec.LookPath("multicodex")
		if err != nil {
			return codexLauncher{}, NewDeepReviewError(
				"resume requires multicodex profile %q but multicodex is not available on PATH",
				ctx.MulticodexProfile,
			)
		}
		return codexLauncher{
			Command: multicodexPath,
			Args:    []string{"run", ctx.MulticodexProfile, "--", "codex"},
			Display: "multicodex",
		}, nil
	}
	return c.resolveLauncher()
}

func requireMulticodex() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(envRequireMulticodex))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func (c *CodexRunner) RunPrompt(cwd, prompt string, ctx *CodexContext, logPrefixPath string) (CodexRunResult, error) {
	return c.RunPromptWithHooks(cwd, prompt, ctx, logPrefixPath, nil)
}

func (c *CodexRunner) RunPromptWithHooks(cwd, prompt string, ctx *CodexContext, logPrefixPath string, hooks *CodexRunHooks) (CodexRunResult, error) {
	if strings.TrimSpace(c.CodexBin) == "" {
		return CodexRunResult{}, NewDeepReviewError("codex binary must be configured")
	}
	parentCtx := context.Background()
	if hooks != nil && hooks.Context != nil {
		parentCtx = hooks.Context
	}
	launcher, err := c.resolveLauncherForContext(ctx)
	if err != nil {
		return CodexRunResult{}, err
	}
	command := c.buildCommandWithLauncher(launcher, ctx)

	stdoutPath := logPrefixPath + ".stdout.jsonl"
	stderrPath := logPrefixPath + ".stderr.log"
	selectedProfilePath := logPrefixPath + ".multicodex-profile.json"
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
	commandEnv := withEnvOverride(os.Environ(), envMulticodexSelectedProfilePath, "")
	if launcher.Display == "multicodex" && (ctx == nil || strings.TrimSpace(ctx.MulticodexProfile) == "") {
		commandEnv = withEnvOverride(commandEnv, envMulticodexSelectedProfilePath, selectedProfilePath)
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
	if ctx != nil {
		discoveredThreadID = ctx.ThreadID
	}
	discoveredProfile := ""
	if ctx != nil {
		discoveredProfile = strings.TrimSpace(ctx.MulticodexProfile)
	}
	agentMessages := make([]string, 0)
	if launcher.Display == "multicodex" && discoveredProfile == "" {
		discoveredProfile = readSelectedMulticodexProfile(selectedProfilePath)
	}

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
		ThreadID:          discoveredThreadID,
		MulticodexProfile: discoveredProfile,
		UsedMulticodex:    launcher.Display == "multicodex",
		AgentMessages:     agentMessages,
		Stdout:            completed.Stdout,
		Stderr:            completed.Stderr,
	}, nil
}

func withEnvOverride(base []string, key, value string) []string {
	key = strings.TrimSpace(key)
	if key == "" {
		return append([]string{}, base...)
	}
	prefix := key + "="
	env := make([]string, 0, len(base)+1)
	for _, kv := range base {
		if strings.HasPrefix(kv, prefix) {
			continue
		}
		env = append(env, kv)
	}
	if value != "" {
		env = append(env, prefix+value)
	}
	return env
}

func readSelectedMulticodexProfile(path string) string {
	payload, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var metadata struct {
		Profile string `json:"profile"`
	}
	if err := json.Unmarshal(payload, &metadata); err != nil {
		return ""
	}
	return strings.TrimSpace(metadata.Profile)
}
