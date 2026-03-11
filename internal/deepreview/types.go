package deepreview

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"path/filepath"
	"time"
)

const (
	ModePR   = "pr"
	ModeYolo = "yolo"
)

type ReviewConfig struct {
	Repo                string         `json:"repo"`
	SourceBranch        string         `json:"source_branch"`
	Concurrency         int            `json:"concurrency"`
	MaxRounds           int            `json:"max_rounds"`
	ReviewInactivitySec int            `json:"review_inactivity_seconds"`
	ReviewActivityPollS int            `json:"review_activity_poll_seconds"`
	ReviewMaxRestarts   int            `json:"review_max_restarts"`
	Mode                string         `json:"mode"`
	WorkspaceRoot       string         `json:"workspace_root"`
	RunID               string         `json:"run_id"`
	GitBin              string         `json:"git_bin"`
	CodexBin            string         `json:"codex_bin"`
	CodexModel          string         `json:"codex_model"`
	CodexReasoning      string         `json:"codex_reasoning_effort"`
	GhBin               string         `json:"gh_bin"`
	CodexTimeoutSeconds int            `json:"codex_timeout_seconds"`
	CodexTimeout        time.Duration  `json:"-"`
	ReviewInactivity    time.Duration  `json:"-"`
	ReviewActivityPoll  time.Duration  `json:"-"`
	CommitIdentity      CommitIdentity `json:"-"`
}

type RepoIdentity struct {
	Owner       string
	Name        string
	CloneSource string
}

func (r RepoIdentity) Slug() string {
	return r.Owner + "/" + r.Name
}

type RoundStatus struct {
	Decision   string   `json:"decision"`
	Reason     string   `json:"reason"`
	Confidence *float64 `json:"confidence,omitempty"`
	NextFocus  *string  `json:"next_focus,omitempty"`
}

type CodexRunResult struct {
	ThreadID      string
	AgentMessages []string
	Stdout        string
	Stderr        string
}

type DeliveryResult struct {
	Mode             string
	PushedRefspec    string
	PRURL            string
	CommitsURL       string
	Incomplete       bool
	IncompleteReason string
	Skipped          bool
	SkipReason       string
}

type CommitIdentity struct {
	Name  string
	Email string
}

func BuildRunID(now time.Time) (string, error) {
	buf := make([]byte, 4)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return now.UTC().Format("20060102T150405Z") + "-" + hex.EncodeToString(buf), nil
}

func WorkspaceRootFromEnv() (string, error) {
	if override := os.Getenv("DEEPREVIEW_WORKSPACE_ROOT"); override != "" {
		abs, err := filepath.Abs(override)
		if err != nil {
			return "", err
		}
		return abs, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "deepreview"), nil
}
