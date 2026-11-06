package deepreview

import (
	"errors"
	"strings"
	"testing"
)

func TestReviewExitCodeSeparatesIncompleteFromFailure(t *testing.T) {
	if reviewExitCode(nil, false) != 0 {
		t.Fatal("expected exit 0 for a complete run")
	}
	if reviewExitCode(newIncompleteRunError(errors.New("still blocked")), false) != exitCodeIncomplete {
		t.Fatal("expected exit 2 for an incomplete run")
	}
	if reviewExitCode(errors.New("push failed"), false) != 1 {
		t.Fatal("expected exit 1 for a failed run")
	}
	if reviewExitCode(newIncompleteRunError(errors.New("still blocked")), true) != 130 {
		t.Fatal("expected cancellation to outrank an incomplete result")
	}
}

func TestPublicationStateHidesURLUntilMutation(t *testing.T) {
	hidden := DeliveryResult{
		CommitsURL:  "https://example.com/commits/main",
		PRURL:       "https://example.com/pull/1",
		Publication: publicationNotAttempted,
	}
	if hidden.showCommitsURL() || hidden.showPRURL() {
		t.Fatal("expected no remote URL before publication")
	}
	pushed := DeliveryResult{
		CommitsURL:  "https://example.com/commits/main",
		PRURL:       "https://example.com/pull/1",
		Publication: publicationPushed,
	}
	if !pushed.showCommitsURL() || pushed.showPRURL() {
		t.Fatal("expected commits URL only after push")
	}
	created := DeliveryResult{
		CommitsURL:  "https://example.com/commits/main",
		PRURL:       "https://example.com/pull/1",
		Publication: publicationPRCreated,
	}
	if !created.showCommitsURL() || !created.showPRURL() {
		t.Fatal("expected both URLs after pull request creation")
	}
}

func TestSummaryWriteFailureKeepsPublishedOutcome(t *testing.T) {
	previous := writeFinalSummaryFile
	writeFinalSummaryFile = func(string, []byte) error {
		return errors.New("summary write failed")
	}
	t.Cleanup(func() { writeFinalSummaryFile = previous })

	orchestrator := &Orchestrator{
		config:    ReviewConfig{RunID: "run-1"},
		runRoot:   t.TempDir(),
		pushCount: 1,
	}
	delivery := DeliveryResult{
		Mode:          ModePR,
		PushedRefspec: "abc:refs/heads/deepreview/main/run-1",
		PRURL:         "https://example.com/pull/1",
		CommitsURL:    "https://example.com/commits/deepreview/main/run-1",
		Publication:   publicationPRCreated,
	}
	if err := orchestrator.acceptDeliverySummary(delivery, nil); err != nil {
		t.Fatalf("published delivery became a failure: %v", err)
	}
	if orchestrator.pushCount != 1 {
		t.Fatalf("push count changed: %d", orchestrator.pushCount)
	}
	if orchestrator.SummaryDiagnostic() == nil || !strings.Contains(orchestrator.SummaryDiagnostic().Error(), "summary write failed") {
		t.Fatalf("expected summary diagnostic, got %v", orchestrator.SummaryDiagnostic())
	}
	if orchestrator.LastDelivery() == nil || orchestrator.LastDelivery().publicationState() != publicationPRCreated {
		t.Fatal("expected the created pull request to stay recorded")
	}
	if reviewExitCode(nil, false) != 0 {
		t.Fatal("summary diagnostic must not change a complete exit code")
	}
}

func TestSummaryWriteFailureKeepsIncompleteOutcome(t *testing.T) {
	previous := writeFinalSummaryFile
	writeFinalSummaryFile = func(string, []byte) error {
		return errors.New("summary write failed")
	}
	t.Cleanup(func() { writeFinalSummaryFile = previous })

	orchestrator := &Orchestrator{
		config:    ReviewConfig{RunID: "run-1"},
		runRoot:   t.TempDir(),
		pushCount: 1,
	}
	delivery := DeliveryResult{
		Mode:             ModePR,
		PushedRefspec:    "abc:refs/heads/deepreview/main/run-1",
		PRURL:            "https://example.com/pull/1",
		CommitsURL:       "https://example.com/commits/deepreview/main/run-1",
		Incomplete:       true,
		IncompleteReason: "round 1/1 requires another review round",
		Publication:      publicationPRCreated,
	}
	err := orchestrator.acceptDeliverySummary(delivery, nil)
	if !isIncompleteRunError(err) {
		t.Fatalf("expected incomplete result, got %v", err)
	}
	if !strings.Contains(err.Error(), "requires another review round") {
		t.Fatalf("original blocker was dropped: %v", err)
	}
	if orchestrator.pushCount != 1 {
		t.Fatalf("push count changed: %d", orchestrator.pushCount)
	}
	if reviewExitCode(err, false) != exitCodeIncomplete {
		t.Fatal("expected exit 2")
	}
	if orchestrator.LastDelivery() == nil || !orchestrator.LastDelivery().showPRURL() {
		t.Fatal("expected the draft pull request URL to stay visible")
	}
}

func TestUnpublishedSummaryWriteFailureStaysAFailure(t *testing.T) {
	previous := writeFinalSummaryFile
	writeFinalSummaryFile = func(string, []byte) error {
		return errors.New("summary write failed")
	}
	t.Cleanup(func() { writeFinalSummaryFile = previous })

	orchestrator := &Orchestrator{
		config:  ReviewConfig{RunID: "run-1"},
		runRoot: t.TempDir(),
	}
	delivery := DeliveryResult{
		Mode:       ModeYolo,
		Skipped:    true,
		SkipReason: "no deliverable repository changes were produced",
	}
	err := orchestrator.acceptDeliverySummary(delivery, nil)
	if err == nil || isIncompleteRunError(err) {
		t.Fatalf("expected a normal failure, got %v", err)
	}
	if orchestrator.pushCount != 0 {
		t.Fatalf("push count changed: %d", orchestrator.pushCount)
	}
	if reviewExitCode(err, false) != 1 {
		t.Fatal("expected exit 1")
	}
}
