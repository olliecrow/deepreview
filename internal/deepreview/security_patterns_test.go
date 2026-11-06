package deepreview

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Fixture strings are built at run time so this file never holds the text
// that the repository's own scanners block.

const zeroSHA = "0000000000000000000000000000000000000000"

func fixtureHomePath(name string) string { return "/" + "home" + "/" + name }

func fixtureMacPath(name string) string { return "/" + "Users" + "/" + name }

func fixtureWindowsPath(drive, users, name string) string {
	return drive + `:\` + users + `\` + name
}

func fixtureJWT() string {
	return "ey" + "JhbGciOiJIUzI1NiJ9.ey" + "JzdWIiOiIxMjM0NTY3ODkwIn0." + strings.Repeat("Q", 32)
}

func runScriptExit(t *testing.T, repo string, env []string, args ...string) (int, string) {
	t.Helper()
	cmd := exec.Command("bash", args...)
	cmd.Dir = repo
	cmd.Env = append(os.Environ(), env...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	var exitErr *exec.ExitError
	switch {
	case err == nil:
		return 0, out.String()
	case errors.As(err, &exitErr):
		return exitErr.ExitCode(), out.String()
	default:
		t.Fatalf("run %v: %v", args, err)
		return -1, ""
	}
}

func TestCheckSensitiveTextTokenAndPathBoundaries(t *testing.T) {
	tokenBody := strings.Repeat("Ab3", 12)
	blocked := map[string]string{
		"anthropic api":            "sk-" + "ant-api03-" + tokenBody,
		"anthropic oauth":          "sk-" + "ant-oat01-" + tokenBody,
		"openai project":           "sk-" + "proj-" + tokenBody,
		"github classic":           "gh" + "p_" + tokenBody,
		"github fine-grained":      "github" + "_pat_" + tokenBody,
		"jwt":                      fixtureJWT(),
		"home path":                fixtureHomePath("alice"),
		"home placeholder prefix":  fixtureHomePath("userbob"),
		"mac placeholder prefix":   fixtureMacPath("YOUNG"),
		"windows lowercase":        fixtureWindowsPath("c", "users", "alice"),
		"placeholder beside real":  fixtureMacPath("YOU") + " and " + fixtureHomePath("alice"),
		"email placeholder suffix": "a@" + "example.com.attacker.dev",
	}
	allowed := map[string]string{
		"task id":             "task-" + strings.Repeat("list", 10),
		"risk id":             "risk-" + strings.Repeat("a1", 16),
		"hyphenated words":    "task-list-item-checkbox risk-assessment-for-new-users",
		"home placeholder":    fixtureHomePath("user") + "/project",
		"mac placeholder":     fixtureMacPath("YOU") + "/project",
		"windows placeholder": fixtureWindowsPath("C", "Users", "USERNAME"),
		"placeholder email":   "dev@" + "example.com",
	}
	for _, withoutRipgrep := range []bool{false, true} {
		var env []string
		if withoutRipgrep {
			env = []string{"PATH=/usr/bin:/bin"}
		}
		repo := initSecurityScriptRepo(t)
		for name, text := range blocked {
			target := writeRepoFile(t, repo, "case.txt", "x\nvalue "+text+"\n")
			code, out := runScriptExit(t, repo, env, "scripts/security/check-sensitive-text.sh", target)
			if code != 1 || !strings.Contains(out, "case.txt:2") {
				t.Fatalf("%s (no rg=%v): expected a block on line 2, exit=%d\n%s", name, withoutRipgrep, code, out)
			}
			if strings.Contains(out, text) {
				t.Fatalf("%s: block printed the value", name)
			}
		}
		for name, text := range allowed {
			target := writeRepoFile(t, repo, "case.txt", "value "+text+"\n")
			if code, out := runScriptExit(t, repo, env, "scripts/security/check-sensitive-text.sh", target); code != 0 {
				t.Fatalf("%s (no rg=%v): expected no block, exit=%d\n%s", name, withoutRipgrep, code, out)
			}
		}
	}
}

func requireGitleaks(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("gitleaks"); err != nil {
		if os.Getenv("DEEPREVIEW_REQUIRE_GITLEAKS") == "1" {
			t.Fatal("gitleaks is required but not on PATH")
		}
		t.Skip("gitleaks not on PATH")
	}
}

func TestGitleaksConfigReportsHomePathsWithoutPlaceholderPrefixes(t *testing.T) {
	requireGitleaks(t)
	config := filepath.Join(repoRoot(t), ".gitleaks.toml")
	blocked := []string{
		fixtureHomePath("alice") + "/x",
		fixtureHomePath("alice"),
		fixtureHomePath("userbob"),
		fixtureMacPath("alice"),
		fixtureMacPath("YOUNG"),
		fixtureWindowsPath("C", "Users", "alice"),
		fixtureWindowsPath("c", "users", "alice"),
		fixtureHomePath("user") + " " + fixtureHomePath("alice"),
		fixtureMacPath("YOU") + " " + fixtureMacPath("alice"),
	}
	allowed := []string{
		fixtureHomePath("user") + "/x",
		fixtureMacPath("YOU") + "/x",
		fixtureWindowsPath("C", "Users", "YOU"),
		fixtureWindowsPath("D", "users", "username"),
	}
	scan := func(text string) int {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "case.txt"), []byte("value "+text+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command("gitleaks", "dir", "--no-banner", "--redact", "--config", config, dir)
		err := cmd.Run()
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return exitErr.ExitCode()
		}
		if err != nil {
			t.Fatal(err)
		}
		return 0
	}
	for i, text := range blocked {
		if code := scan(text); code != 1 {
			t.Fatalf("blocked case %d: gitleaks exit %d", i, code)
		}
	}
	for i, text := range allowed {
		if code := scan(text); code != 0 {
			t.Fatalf("allowed case %d: gitleaks exit %d", i, code)
		}
	}
}
