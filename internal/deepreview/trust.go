package deepreview

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"time"
)

const repositoryTrustRequiredMessage = "repository execution requires --trust-repo: this workflow runs repository instructions, tests, hooks, filters, and policy scripts with the operator's user privileges and is not a sandbox"

var policyScriptTimeout = 2 * time.Minute

func repositoryTrustStatus(trusted bool) string {
	if trusted {
		return "granted"
	}
	return "not granted"
}

func repositoryTrustDoctorDetail(trusted bool) string {
	if trusted {
		return "granted; review may run repository instructions, tests, hooks, filters, and policy scripts with operator privileges. This workflow is not a sandbox. doctor does not execute those scripts"
	}
	return "not granted; review requires --trust-repo before repository instructions, tests, hooks, filters, and policy scripts run with operator privileges. This workflow is not a sandbox. doctor does not execute those scripts"
}

func restrictedPolicyEnviron(base []string) []string {
	allowed := map[string]struct{}{
		"PATH":   {},
		"HOME":   {},
		"TMPDIR": {},
		"LANG":   {},
		"USER":   {},
	}
	filtered := make([]string, 0, len(base)+8)
	for _, entry := range base {
		key, _, ok := strings.Cut(entry, "=")
		if !ok || key == "" {
			continue
		}
		upper := strings.ToUpper(key)
		if strings.Contains(upper, "TOKEN") ||
			strings.Contains(upper, "SECRET") ||
			strings.Contains(upper, "PASSWORD") ||
			strings.Contains(upper, "API_KEY") ||
			strings.Contains(upper, "APIKEY") ||
			key == "SSH_AUTH_SOCK" {
			continue
		}
		if _, ok := allowed[key]; ok || strings.HasPrefix(key, "LC_") {
			filtered = append(filtered, entry)
		}
	}
	return append(filtered,
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_TERMINAL_PROMPT=0",
		"GIT_CONFIG_COUNT=1",
		"GIT_CONFIG_KEY_0=credential.helper",
		"GIT_CONFIG_VALUE_0=",
	)
}

func (o *Orchestrator) writePolicyScriptRecord(ref string, present bool, blobSHA string) error {
	record := map[string]any{
		"path":     "scripts/security/check-push-range.sh",
		"ref":      strings.TrimSpace(ref),
		"present":  present,
		"blob_sha": strings.TrimSpace(blobSHA),
	}
	body, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	return writePrivateFile(filepath.Join(o.runRoot, "policy-script.json"), append(body, '\n'))
}
