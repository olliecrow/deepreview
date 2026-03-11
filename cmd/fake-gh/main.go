package main

import (
	"fmt"
	"os"
	"strings"
)

func main() {
	args := os.Args[1:]
	if capturePath := os.Getenv("FAKE_GH_CAPTURE_ARGS_PATH"); capturePath != "" {
		_ = os.WriteFile(capturePath, []byte(strings.Join(args, "\n")+"\n"), 0o644)
	}
	if len(args) >= 2 && args[0] == "pr" && args[1] == "create" {
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
		if os.Getenv("FAKE_GH_PR_CREATE_SILENT") != "" {
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
	os.Exit(1)
}
