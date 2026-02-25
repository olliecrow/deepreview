package deepreview

import (
	"fmt"
	"io"
	"os"
	"sync"
	"time"
)

type TextProgressReporter struct {
	mu         sync.Mutex
	out        io.Writer
	runStarted time.Time
	stageStart map[string]time.Time
}

func NewTextProgressReporter(out io.Writer) *TextProgressReporter {
	if out == nil {
		out = os.Stderr
	}
	return &TextProgressReporter{
		out:        out,
		stageStart: make(map[string]time.Time),
	}
}

func (r *TextProgressReporter) RunStarted(runID, repo, sourceBranch, mode, runRoot string) {
	r.mu.Lock()
	r.runStarted = time.Now()
	r.mu.Unlock()
	r.printf("[run-start] id=%s repo=%s branch=%s mode=%s", runID, repo, sourceBranch, mode)
	r.printf("[run-path] %s", runRoot)
}

func (r *TextProgressReporter) StageStarted(stageName string, roundNumber *int, message string) {
	r.mu.Lock()
	r.stageStart[stageKey(stageName, roundNumber)] = time.Now()
	r.mu.Unlock()
	r.printf("[stage-start] %s %s | %s", formatRound(roundNumber), stageName, message)
}

func (r *TextProgressReporter) StageProgress(stageName, message string, roundNumber *int) {
	r.printf("[stage-progress] %s %s | %s", formatRound(roundNumber), stageName, message)
}

func (r *TextProgressReporter) StageFinished(stageName string, roundNumber *int, success bool, message string) {
	status := "ok"
	if !success {
		status = "failed"
	}
	key := stageKey(stageName, roundNumber)
	elapsed := ""
	r.mu.Lock()
	if started, ok := r.stageStart[key]; ok {
		elapsed = " elapsed=" + fmtDuration(time.Since(started))
		delete(r.stageStart, key)
	}
	r.mu.Unlock()
	r.printf("[stage-%s] %s %s | %s%s", status, formatRound(roundNumber), stageName, message, elapsed)
}

func (r *TextProgressReporter) RunFinished(success bool, message string) {
	status := "ok"
	if !success {
		status = "failed"
	}
	duration := ""
	r.mu.Lock()
	if !r.runStarted.IsZero() {
		duration = " elapsed=" + fmtDuration(time.Since(r.runStarted))
	}
	r.mu.Unlock()
	r.printf("[run-%s] %s%s", status, message, duration)
}

func formatRound(roundNumber *int) string {
	if roundNumber == nil {
		return "global"
	}
	return fmt.Sprintf("round=%d", *roundNumber)
}

func stageKey(stageName string, roundNumber *int) string {
	return formatRound(roundNumber) + "|" + stageName
}

func (r *TextProgressReporter) printf(format string, args ...any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	ts := time.Now().Format("15:04:05")
	_, _ = fmt.Fprintf(r.out, "[%s] %s\n", ts, fmt.Sprintf(format, args...))
}
