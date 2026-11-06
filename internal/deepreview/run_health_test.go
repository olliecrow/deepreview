package deepreview

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSummarizeStderrLogCountsOversizedLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "worker.stderr.log")
	payload := bytes.Repeat([]byte("A"), 2<<20)
	payload = append(payload, '\n')
	payload = append(payload, []byte("ERROR sample\n")...)
	if err := os.WriteFile(path, payload, 0o644); err != nil {
		t.Fatal(err)
	}

	summary := summarizeStderrLog("worker.stderr.log", path)
	if summary.Unreadable {
		t.Fatal("expected a readable stderr log")
	}
	if summary.Lines != 2 || summary.LongLines != 1 || summary.ErrorLines != 1 || summary.WarningLines != 0 {
		t.Fatalf("unexpected summary: %+v", summary)
	}

	report, err := collectRunHealth(dir, "run-1", ModePR)
	if err != nil {
		t.Fatalf("long stderr line failed run health: %v", err)
	}
	if report.Stderr.LongLines != 1 || report.Stderr.ErrorLines != 1 || report.Stderr.TotalLines != 2 {
		t.Fatalf("unexpected health stderr: %+v", report.Stderr)
	}

	orchestrator := &Orchestrator{
		config:    ReviewConfig{RunID: "run-1"},
		runRoot:   dir,
		pushCount: 1,
	}
	delivery := DeliveryResult{
		Mode:          ModePR,
		PushedRefspec: "abc:refs/heads/deepreview/main/run-1",
		PRURL:         "https://example.com/pull/1",
		Publication:   publicationPRCreated,
	}
	if err := orchestrator.writeFinalSummary(delivery, nil); err != nil {
		t.Fatalf("long stderr line failed the final summary: %v", err)
	}
	body, err := os.ReadFile(filepath.Join(dir, "final-summary.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "long_lines=`1`") {
		t.Fatalf("expected long-line count in summary, got:\n%s", string(body))
	}
}

func TestCollectRunHealthReportsUnreadableStderrLog(t *testing.T) {
	dir := t.TempDir()
	if err := os.Symlink(filepath.Join(dir, "missing.log"), filepath.Join(dir, "worker.stderr.log")); err != nil {
		t.Fatal(err)
	}
	report, err := collectRunHealth(dir, "run-1", ModePR)
	if err != nil {
		t.Fatalf("missing stderr log failed run health: %v", err)
	}
	found := false
	for _, item := range report.Diagnostics {
		if strings.Contains(item, "could not be read") {
			found = true
		}
		if strings.Contains(item, dir) {
			t.Fatalf("diagnostic included a local path: %s", item)
		}
	}
	if !found {
		t.Fatalf("expected an unreadable-log diagnostic, got %v", report.Diagnostics)
	}
	if report.Stderr.NonEmptyFiles != 0 {
		t.Fatalf("unreadable log was treated as content: %+v", report.Stderr)
	}
}
