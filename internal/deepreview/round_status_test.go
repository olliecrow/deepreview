package deepreview

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadRoundStatusInvalidDecisionFails(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "status.json")
	if err := os.WriteFile(path, []byte(`{"decision":"halt","reason":"x"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := readRoundStatus(path); err == nil {
		t.Fatal("expected error for invalid decision")
	}
}

func TestReadRoundStatusValidParses(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "status.json")
	if err := os.WriteFile(path, []byte(`{"decision":"stop","reason":"done","confidence":0.9,"next_focus":"none"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	status, err := readRoundStatus(path)
	if err != nil {
		t.Fatalf("readRoundStatus failed: %v", err)
	}
	if status.Decision != "stop" {
		t.Fatalf("expected decision stop, got %s", status.Decision)
	}
	if status.Reason != "done" {
		t.Fatalf("expected reason done, got %s", status.Reason)
	}
	if status.Confidence == nil || *status.Confidence != 0.9 {
		t.Fatalf("expected confidence 0.9, got %+v", status.Confidence)
	}
}

func TestReadRoundStatusReasonTypeFails(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "status.json")
	if err := os.WriteFile(path, []byte(`{"decision":"stop","reason":42}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := readRoundStatus(path); err == nil {
		t.Fatal("expected error for non-string reason")
	}
}

func TestReadRoundStatusConfidenceTypeFails(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "status.json")
	if err := os.WriteFile(path, []byte(`{"decision":"stop","reason":"done","confidence":"high"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := readRoundStatus(path); err == nil {
		t.Fatal("expected error for non-numeric confidence")
	}
}

func TestReadRoundStatusNextFocusTypeFails(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "status.json")
	if err := os.WriteFile(path, []byte(`{"decision":"stop","reason":"done","next_focus":false}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := readRoundStatus(path); err == nil {
		t.Fatal("expected error for non-string next_focus")
	}
}
