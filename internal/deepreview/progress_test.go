package deepreview

import "testing"

func TestStageProgressDoesNotReopenCompletedStage(t *testing.T) {
	state := NewSharedProgressState()
	reporter := NewTUIProgressReporter(state)
	round := 2

	reporter.StageStarted("execute stage", &round, "running execute workflow")
	reporter.StageFinished("execute stage", &round, true, "round status recorded (decision=continue)")
	reporter.StageProgress("execute stage", "round produced 3 repository change(s); scheduling next review round", &round)

	snapshot := state.Snapshot()
	if len(snapshot.Stages) != 1 {
		t.Fatalf("expected exactly one stage row, got %d", len(snapshot.Stages))
	}
	stage := snapshot.Stages[0]
	if stage.Status != "done" {
		t.Fatalf("expected stage to remain done, got %q", stage.Status)
	}
	if stage.Message != "round produced 3 repository change(s); scheduling next review round" {
		t.Fatalf("expected latest progress message to be retained, got %q", stage.Message)
	}
}

func TestStageProgressCreatesStageWhenMissing(t *testing.T) {
	state := NewSharedProgressState()
	reporter := NewTUIProgressReporter(state)
	round := 1

	reporter.StageProgress("execute stage", "bootstrapping", &round)

	snapshot := state.Snapshot()
	if len(snapshot.Stages) != 1 {
		t.Fatalf("expected stage row to be created, got %d", len(snapshot.Stages))
	}
	stage := snapshot.Stages[0]
	if stage.Status != "running" {
		t.Fatalf("expected created stage status running, got %q", stage.Status)
	}
	if stage.Message != "bootstrapping" {
		t.Fatalf("unexpected stage message %q", stage.Message)
	}
}
