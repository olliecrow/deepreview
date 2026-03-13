package main

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"testing"
)

func TestSourceDoesNotEmbedContiguousAKIAFixtureLiteral(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	sourcePath := filepath.Join(filepath.Dir(file), "main.go")
	body, err := os.ReadFile(sourcePath)
	if err != nil {
		t.Fatalf("read %s: %v", sourcePath, err)
	}
	if match := regexp.MustCompile(`AKIA[0-9A-Z]{16}`).FindString(string(body)); match != "" {
		t.Fatalf("main.go should build secret fixtures at runtime, found contiguous literal %q", match)
	}
}
