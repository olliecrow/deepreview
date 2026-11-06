// Package prompts is the editable prompt pack shipped inside the deepreview binary.
package prompts

import "embed"

//go:embed review/* execute/* delivery/*
var Files embed.FS
