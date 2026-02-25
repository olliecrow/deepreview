package main

import (
	"os"

	"deepreview/internal/deepreview"
)

func main() {
	os.Exit(deepreview.RunCLI(os.Args[1:]))
}
