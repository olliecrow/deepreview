package deepreview

import (
	"bytes"
	"strings"
	"testing"
)

func TestTextProgressReporterEmitsExpectedLines(t *testing.T) {
	var buf bytes.Buffer
	reporter := NewTextProgressReporter(&buf)

	round := 1
	reporter.RunStarted("run-1", "owner/repo", "feature/x", "pr", "/tmp/deepreview/runs/run-1")
	reporter.StageStarted("prepare", nil, "start")
	reporter.StageProgress("independent review stage", "2/4", &round)
	reporter.StageFinished("independent review stage", &round, true, "done")
	reporter.RunFinished(true, "completed successfully")

	output := buf.String()
	expect := []string{
		"[run-start] id=run-1 repo=owner/repo branch=feature/x mode=pr",
		"[run-path] /tmp/deepreview/runs/run-1",
		"[stage-start] global prepare | start",
		"[stage-progress] round=1 independent review stage | 2/4",
		"[stage-ok] round=1 independent review stage | done",
		"[run-ok] completed successfully",
	}
	for _, token := range expect {
		if !strings.Contains(output, token) {
			t.Fatalf("expected output to contain %q, got:\n%s", token, output)
		}
	}
	if !strings.Contains(output, "elapsed=") {
		t.Fatalf("expected elapsed timing in output, got:\n%s", output)
	}
}
