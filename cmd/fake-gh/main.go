package main

import (
	"fmt"
	"os"
)

func main() {
	args := os.Args[1:]
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
