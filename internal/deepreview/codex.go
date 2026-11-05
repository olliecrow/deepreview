package deepreview

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const envRequireMulticodex = "DEEPREVIEW_REQUIRE_MULTICODEX"
const envMulticodexSelectedProfilePath = "MULTICODEX_SELECTED_PROFILE_PATH"
const maxCodexEventBytes = 1 << 20

var openPromptLogFile = func(path string) (io.WriteCloser, error) {
	return os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
}

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
	runCtx, cancelRun := context.WithCancel(parentCtx)
	defer cancelRun()
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
	stdoutFile, err := openPromptLogFile(stdoutPath)
	if err != nil {
		return CodexRunResult{}, err
	}
	defer stdoutFile.Close()
	stderrFile, err := openPromptLogFile(stderrPath)
	if err != nil {
		return CodexRunResult{}, err
	}
	defer stderrFile.Close()

	parser := &codexEventParser{}
	if ctx != nil {
		parser.threadID = ctx.ThreadID
	}
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
		cancelRun()
	}
	writeStream := func(file io.Writer, chunk []byte) error {
		if len(chunk) == 0 {
			return nil
		}
		_, err := file.Write(chunk)
		if err != nil {
			return NewDeepReviewError("codex log write failed: %v", err)
		}
		return nil
	}
	streamCallbacks := &CommandStreamCallbacks{
		OnStdoutChunk: func(chunk []byte) {
			copied := append([]byte(nil), chunk...)
			streamMu.Lock()
			err := writeStream(stdoutFile, copied)
			if err == nil {
				err = parser.push(copied)
			}
			streamMu.Unlock()
			setStreamErr(err)
			if hooks != nil && hooks.OnStdoutChunk != nil {
				hooks.OnStdoutChunk(copied)
			}
		},
		OnStderrChunk: func(chunk []byte) {
			copied := append([]byte(nil), chunk...)
			streamMu.Lock()
			err := writeStream(stderrFile, copied)
			streamMu.Unlock()
			setStreamErr(err)
			if hooks != nil && hooks.OnStderrChunk != nil {
				hooks.OnStderrChunk(copied)
			}
		},
	}
	commandEnv := withEnvOverride(os.Environ(), envMulticodexSelectedProfilePath, "")
	if launcher.Display == "multicodex" && (ctx == nil || strings.TrimSpace(ctx.MulticodexProfile) == "") {
		commandEnv = withEnvOverride(commandEnv, envMulticodexSelectedProfilePath, selectedProfilePath)
	}
	completed, err := RunCommandContextWithEnvAndCallbacks(runCtx, command, cwd, commandEnv, prompt, true, c.Timeout, streamCallbacks)
	streamMu.Lock()
	if streamErr == nil {
		streamErr = parser.finish()
	}
	capturedStreamErr := streamErr
	threadID := parser.threadID
	agentMessages := append([]string(nil), parser.messages...)
	streamMu.Unlock()
	if capturedStreamErr != nil {
		return CodexRunResult{}, capturedStreamErr
	}
	if err != nil {
		return CodexRunResult{}, err
	}

	discoveredProfile := ""
	if ctx != nil {
		discoveredProfile = strings.TrimSpace(ctx.MulticodexProfile)
	}
	if launcher.Display == "multicodex" && discoveredProfile == "" {
		discoveredProfile = readSelectedMulticodexProfile(selectedProfilePath)
	}
	if threadID == "" {
		return CodexRunResult{}, NewDeepReviewError("codex run did not emit a thread id")
	}

	return CodexRunResult{
		ThreadID:          threadID,
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

type codexEventParser struct {
	pending  []byte
	threadID string
	messages []string
}

func (p *codexEventParser) push(chunk []byte) error {
	if len(chunk) == 0 {
		return nil
	}
	p.pending = append(p.pending, chunk...)
	for {
		idx := bytes.IndexByte(p.pending, '\n')
		if idx < 0 {
			if len(p.pending) > maxCodexEventBytes {
				p.pending = nil
				return NewDeepReviewError("codex event exceeds %d bytes", maxCodexEventBytes)
			}
			return nil
		}
		line := p.pending[:idx]
		p.pending = append([]byte(nil), p.pending[idx+1:]...)
		if len(line) > maxCodexEventBytes {
			return NewDeepReviewError("codex event exceeds %d bytes", maxCodexEventBytes)
		}
		if err := p.consumeLine(line); err != nil {
			return err
		}
	}
}

func (p *codexEventParser) finish() error {
	if len(bytes.TrimSpace(p.pending)) == 0 {
		p.pending = nil
		return nil
	}
	if len(p.pending) > maxCodexEventBytes {
		p.pending = nil
		return NewDeepReviewError("codex event exceeds %d bytes", maxCodexEventBytes)
	}
	line := p.pending
	p.pending = nil
	return p.consumeLine(line)
}

func (p *codexEventParser) consumeLine(line []byte) error {
	trimmed := bytes.TrimSpace(line)
	if len(trimmed) == 0 {
		return nil
	}
	var event map[string]any
	if err := json.Unmarshal(trimmed, &event); err != nil {
		return NewDeepReviewError("failed parsing codex event json: %v", err)
	}
	eventType, _ := event["type"].(string)
	if eventType == "thread.started" {
		if tid, ok := event["thread_id"].(string); ok && tid != "" {
			p.threadID = tid
		}
	}
	if eventType != "item.completed" {
		return nil
	}
	item, ok := event["item"].(map[string]any)
	if !ok {
		return nil
	}
	itemType, _ := item["type"].(string)
	if itemType != "agent_message" {
		return nil
	}
	text, _ := item["text"].(string)
	if text == "" {
		return nil
	}
	p.messages = append(p.messages, text)
	return nil
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
