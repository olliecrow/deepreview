package deepreview

import (
	"os"
	"regexp"
	"sort"
	"strings"
)

var templateVarRe = regexp.MustCompile(`\{\{([A-Z0-9_]+)\}\}`)

func ReadTemplate(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", NewDeepReviewError("template file not found: %s", path)
	}
	return string(b), nil
}

func ReadQueue(path string) ([]string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, NewDeepReviewError("execute queue file not found: %s", path)
	}
	var entries []string
	for _, raw := range strings.Split(string(b), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		entries = append(entries, line)
	}
	if len(entries) == 0 {
		return nil, NewDeepReviewError("execute queue is empty: %s", path)
	}
	return entries, nil
}

func RenderTemplate(text string, variables map[string]string) (string, error) {
	required := map[string]struct{}{}
	for _, m := range templateVarRe.FindAllStringSubmatch(text, -1) {
		required[m[1]] = struct{}{}
	}
	var missing []string
	for key := range required {
		if _, ok := variables[key]; !ok {
			missing = append(missing, key)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return "", NewDeepReviewError("missing template variables: %s", strings.Join(missing, ", "))
	}

	rendered := text
	for key, value := range variables {
		rendered = strings.ReplaceAll(rendered, "{{"+key+"}}", value)
	}

	unresolvedSet := map[string]struct{}{}
	for _, m := range templateVarRe.FindAllStringSubmatch(rendered, -1) {
		unresolvedSet[m[1]] = struct{}{}
	}
	if len(unresolvedSet) > 0 {
		unresolved := make([]string, 0, len(unresolvedSet))
		for key := range unresolvedSet {
			unresolved = append(unresolved, key)
		}
		sort.Strings(unresolved)
		return "", NewDeepReviewError("unresolved template variables after rendering: %s", strings.Join(unresolved, ", "))
	}

	return rendered, nil
}
