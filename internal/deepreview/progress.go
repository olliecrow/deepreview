package deepreview

import (
	"sync"
	"time"
)

type StageState struct {
	RoundNumber *int
	Name        string
	Status      string
	Message     string
	StartedAt   time.Time
	EndedAt     *time.Time
}

func (s StageState) Elapsed(now time.Time) time.Duration {
	if s.EndedAt != nil {
		return s.EndedAt.Sub(s.StartedAt)
	}
	return now.Sub(s.StartedAt)
}

type ProgressReporter interface {
	RunStarted(runID, repo, sourceBranch, mode, runRoot string)
	StageStarted(stageName string, roundNumber *int, message string)
	StageProgress(stageName, message string, roundNumber *int)
	StageFinished(stageName string, roundNumber *int, success bool, message string)
	RunFinished(success bool, message string)
}

type MaxRoundsAwareProgressReporter interface {
	SetMaxRounds(maxRounds int)
}

type NullProgressReporter struct{}

func (n *NullProgressReporter) RunStarted(string, string, string, string, string) {}
func (n *NullProgressReporter) StageStarted(string, *int, string)                 {}
func (n *NullProgressReporter) StageProgress(string, string, *int)                {}
func (n *NullProgressReporter) StageFinished(string, *int, bool, string)          {}
func (n *NullProgressReporter) RunFinished(bool, string)                          {}

type ProgressSnapshot struct {
	RunID         string
	Repo          string
	SourceBranch  string
	Mode          string
	MaxRounds     int
	RunRoot       string
	RunStartedAt  time.Time
	RunFinishedAt *time.Time
	Success       *bool
	FinalMessage  string
	Stages        []StageSnapshot
}

type StageSnapshot struct {
	RoundNumber *int
	Name        string
	Status      string
	Message     string
	Elapsed     time.Duration
}

type SharedProgressState struct {
	mu            sync.Mutex
	runID         string
	repo          string
	sourceBranch  string
	mode          string
	maxRounds     int
	runRoot       string
	runStartedAt  time.Time
	runFinishedAt *time.Time
	success       *bool
	finalMessage  string
	stages        []StageState
}

func NewSharedProgressState() *SharedProgressState {
	return &SharedProgressState{runStartedAt: time.Now()}
}

func (s *SharedProgressState) Snapshot() ProgressSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	stages := make([]StageSnapshot, 0, len(s.stages))
	for _, stage := range s.stages {
		var rn *int
		if stage.RoundNumber != nil {
			v := *stage.RoundNumber
			rn = &v
		}
		stages = append(stages, StageSnapshot{
			RoundNumber: rn,
			Name:        stage.Name,
			Status:      stage.Status,
			Message:     stage.Message,
			Elapsed:     stage.Elapsed(now),
		})
	}

	var finished *time.Time
	if s.runFinishedAt != nil {
		t := *s.runFinishedAt
		finished = &t
	}
	var success *bool
	if s.success != nil {
		v := *s.success
		success = &v
	}

	return ProgressSnapshot{
		RunID:         s.runID,
		Repo:          s.repo,
		SourceBranch:  s.sourceBranch,
		Mode:          s.mode,
		MaxRounds:     s.maxRounds,
		RunRoot:       s.runRoot,
		RunStartedAt:  s.runStartedAt,
		RunFinishedAt: finished,
		Success:       success,
		FinalMessage:  s.finalMessage,
		Stages:        stages,
	}
}

type TUIProgressReporter struct {
	state *SharedProgressState
}

func NewTUIProgressReporter(state *SharedProgressState) *TUIProgressReporter {
	return &TUIProgressReporter{state: state}
}

func (r *TUIProgressReporter) SetMaxRounds(maxRounds int) {
	r.state.mu.Lock()
	defer r.state.mu.Unlock()
	if maxRounds < 0 {
		maxRounds = 0
	}
	r.state.maxRounds = maxRounds
}

func (r *TUIProgressReporter) RunStarted(runID, repo, sourceBranch, mode, runRoot string) {
	r.state.mu.Lock()
	defer r.state.mu.Unlock()
	r.state.runID = runID
	r.state.repo = repo
	r.state.sourceBranch = sourceBranch
	r.state.mode = mode
	r.state.runRoot = runRoot
	r.state.runStartedAt = time.Now()
}

func (r *TUIProgressReporter) StageStarted(stageName string, roundNumber *int, message string) {
	r.state.mu.Lock()
	defer r.state.mu.Unlock()
	var rn *int
	if roundNumber != nil {
		v := *roundNumber
		rn = &v
	}
	r.state.stages = append(r.state.stages, StageState{
		RoundNumber: rn,
		Name:        stageName,
		Status:      "running",
		Message:     message,
		StartedAt:   time.Now(),
	})
}

func roundEqual(a, b *int) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}

func (r *TUIProgressReporter) StageProgress(stageName, message string, roundNumber *int) {
	r.state.mu.Lock()
	defer r.state.mu.Unlock()
	for i := len(r.state.stages) - 1; i >= 0; i-- {
		stage := &r.state.stages[i]
		if stage.Name == stageName && roundEqual(stage.RoundNumber, roundNumber) && stage.Status == "running" {
			stage.Message = message
			return
		}
	}
	var rn *int
	if roundNumber != nil {
		v := *roundNumber
		rn = &v
	}
	r.state.stages = append(r.state.stages, StageState{
		RoundNumber: rn,
		Name:        stageName,
		Status:      "running",
		Message:     message,
		StartedAt:   time.Now(),
	})
}

func (r *TUIProgressReporter) StageFinished(stageName string, roundNumber *int, success bool, message string) {
	r.state.mu.Lock()
	defer r.state.mu.Unlock()
	now := time.Now()
	for i := len(r.state.stages) - 1; i >= 0; i-- {
		stage := &r.state.stages[i]
		if stage.Name == stageName && roundEqual(stage.RoundNumber, roundNumber) && stage.Status == "running" {
			if success {
				stage.Status = "done"
			} else {
				stage.Status = "failed"
			}
			if message != "" {
				stage.Message = message
			}
			stage.EndedAt = &now
			return
		}
	}
	var rn *int
	if roundNumber != nil {
		v := *roundNumber
		rn = &v
	}
	status := "done"
	if !success {
		status = "failed"
	}
	r.state.stages = append(r.state.stages, StageState{
		RoundNumber: rn,
		Name:        stageName,
		Status:      status,
		Message:     message,
		StartedAt:   now,
		EndedAt:     &now,
	})
}

func (r *TUIProgressReporter) RunFinished(success bool, message string) {
	r.state.mu.Lock()
	defer r.state.mu.Unlock()
	now := time.Now()
	r.state.success = &success
	r.state.finalMessage = message
	r.state.runFinishedAt = &now
}
