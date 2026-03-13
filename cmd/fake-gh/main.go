package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

func csvSequenceValue(csv string, index int) string {
	parts := strings.Split(csv, ",")
	if len(parts) == 0 {
		return ""
	}
	if index < 0 {
		index = 0
	}
	if index >= len(parts) {
		index = len(parts) - 1
	}
	return strings.TrimSpace(parts[index])
}

func nextSequenceIndex(path string) int {
	if strings.TrimSpace(path) == "" {
		return 0
	}
	idx := 0
	if raw, err := os.ReadFile(path); err == nil {
		if parsed, parseErr := strconv.Atoi(strings.TrimSpace(string(raw))); parseErr == nil && parsed >= 0 {
			idx = parsed
		}
	}
	_ = os.WriteFile(path, []byte(strconv.Itoa(idx+1)), 0o644)
	return idx
}

func sequenceValue(sequenceEnv, singleEnv string, index int, fallback string) string {
	if sequence := strings.TrimSpace(os.Getenv(sequenceEnv)); sequence != "" {
		if value := csvSequenceValue(sequence, index); value != "" {
			return value
		}
	}
	if value := strings.TrimSpace(os.Getenv(singleEnv)); value != "" {
		return value
	}
	return fallback
}

func parseBoolToken(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "t", "true", "y", "yes":
		return true
	default:
		return false
	}
}

func sequenceBoolValue(sequenceEnv, singleEnv string, index int) bool {
	if sequence := strings.TrimSpace(os.Getenv(sequenceEnv)); sequence != "" {
		return parseBoolToken(csvSequenceValue(sequence, index))
	}
	return strings.TrimSpace(os.Getenv(singleEnv)) != ""
}

func main() {
	args := os.Args[1:]
	if capturePath := os.Getenv("FAKE_GH_CAPTURE_ARGS_PATH"); capturePath != "" {
		_ = os.WriteFile(capturePath, []byte(strings.Join(args, "\n")+"\n"), 0o644)
	}
	if len(args) >= 2 && args[0] == "pr" && args[1] == "create" {
		hasTitle := false
		isDraft := false
		for i := 0; i < len(args); i++ {
			if args[i] == "--body-file" {
				if i+1 >= len(args) {
					os.Exit(2)
				}
				if _, err := os.Stat(args[i+1]); err != nil {
					os.Exit(3)
				}
			}
			if args[i] == "--title" {
				if i+1 >= len(args) || args[i+1] == "" {
					os.Exit(4)
				}
				hasTitle = true
			}
			if args[i] == "--draft" {
				isDraft = true
			}
		}
		if !hasTitle {
			os.Exit(4)
		}
		if os.Getenv("FAKE_GH_PR_CREATE_SILENT") != "" && !isDraft {
			return
		}
		if custom := os.Getenv("FAKE_GH_PR_CREATE_STDOUT"); custom != "" {
			fmt.Println(custom)
			return
		}
		fmt.Println("https://example.com/olliecrow/test/pull/123")
		return
	}
	if len(args) >= 2 && args[0] == "pr" && args[1] == "edit" {
		hasTitle := false
		for i := 0; i < len(args); i++ {
			if args[i] == "--body-file" {
				if i+1 >= len(args) {
					os.Exit(2)
				}
				if _, err := os.Stat(args[i+1]); err != nil {
					os.Exit(3)
				}
			}
			if args[i] == "--title" {
				if i+1 >= len(args) || args[i+1] == "" {
					os.Exit(4)
				}
				hasTitle = true
			}
		}
		if !hasTitle {
			os.Exit(4)
		}
		return
	}
	if len(args) >= 2 && args[0] == "pr" && args[1] == "view" {
		index := nextSequenceIndex(os.Getenv("FAKE_GH_PR_VIEW_SEQUENCE_STATE_PATH"))
		state := sequenceValue("FAKE_GH_PR_VIEW_STATE_SEQUENCE", "FAKE_GH_PR_VIEW_STATE", index, "OPEN")
		mergeable := sequenceValue("FAKE_GH_PR_VIEW_MERGEABLE_SEQUENCE", "FAKE_GH_PR_VIEW_MERGEABLE", index, "MERGEABLE")
		mergeStateStatus := sequenceValue("FAKE_GH_PR_VIEW_MERGE_STATE_STATUS_SEQUENCE", "FAKE_GH_PR_VIEW_MERGE_STATE_STATUS", index, "CLEAN")
		isDraft := sequenceBoolValue("FAKE_GH_PR_VIEW_IS_DRAFT_SEQUENCE", "FAKE_GH_PR_VIEW_IS_DRAFT", index)
		fmt.Printf(`{"url":"https://example.com/olliecrow/test/pull/123","state":%q,"isDraft":%t,"mergeable":%q,"mergeStateStatus":%q}`, state, isDraft, mergeable, mergeStateStatus)
		return
	}
	os.Exit(1)
}
