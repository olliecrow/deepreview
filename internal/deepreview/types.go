package deepreview

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
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
	GhBin               string         `json:"gh_bin"`
	CodexTimeoutSeconds int            `json:"codex_timeout_seconds"`
	CodexTimeout        time.Duration  `json:"-"`
	ReviewInactivity    time.Duration  `json:"-"`
	ReviewActivityPoll  time.Duration  `json:"-"`
	CommitIdentity      CommitIdentity `json:"-"`
}

type RepoSourceType string

const (
	RepoSourceGitHub     RepoSourceType = "github"
	RepoSourceFilesystem RepoSourceType = "filesystem"
)

type RepoIdentity struct {
	SourceType  RepoSourceType
	Owner       string
	Name        string
	CloneSource string
}

func (r RepoIdentity) normalizedSourceType() RepoSourceType {
	switch r.SourceType {
	case RepoSourceFilesystem:
		return RepoSourceFilesystem
	case RepoSourceGitHub:
		return RepoSourceGitHub
	default:
		return RepoSourceGitHub
	}
}

func (r RepoIdentity) Slug() string {
	if r.normalizedSourceType() == RepoSourceFilesystem {
		name := strings.TrimSpace(r.Name)
		if name == "" {
			name = FilesystemSafeKey(strings.TrimSpace(r.CloneSource))
		}
		return string(RepoSourceFilesystem) + "/" + name
	}
	return r.Owner + "/" + r.Name
}

func (r RepoIdentity) SupportsPRDelivery() bool {
	return r.normalizedSourceType() == RepoSourceGitHub &&
		strings.TrimSpace(r.Owner) != "" &&
		strings.TrimSpace(r.Name) != ""
}

func (r RepoIdentity) NamespaceSegments() []string {
	if r.normalizedSourceType() == RepoSourceFilesystem {
		key := strings.TrimSpace(r.CloneSource)
		if key == "" {
			key = strings.TrimSpace(r.Name)
		}
		return []string{string(RepoSourceFilesystem), FilesystemSafeKey(key)}
	}
	return []string{
		string(RepoSourceGitHub),
		SanitizeSegment(r.Owner),
		SanitizeSegment(r.Name),
	}
}

type RoundStatus struct {
	Decision   string   `json:"decision"`
	Reason     string   `json:"reason"`
	Confidence *float64 `json:"confidence,omitempty"`
	NextFocus  *string  `json:"next_focus,omitempty"`
}

type RoundRecord struct {
	Round   int         `json:"round"`
	Status  RoundStatus `json:"status"`
	Summary string      `json:"summary"`
}

type CodexContext struct {
	ThreadID          string `json:"thread_id,omitempty"`
	MulticodexProfile string `json:"multicodex_profile,omitempty"`
}

type CodexRunResult struct {
	ThreadID          string
	MulticodexProfile string
	UsedMulticodex    bool
	AgentMessages     []string
	Stdout            string
	Stderr            string
}

type DeliveryResult struct {
	Mode             string
	PushedRefspec    string
	PRURL            string
	CommitsURL       string
	Skipped          bool
	SkipReason       string
	Incomplete       bool
	IncompleteReason string
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
