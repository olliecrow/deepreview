package deepreview

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func pushRangeExit(t *testing.T, repo, from, to string, extraEnv ...string) (int, string) {
	t.Helper()
	env := append([]string{"PRE_COMMIT_FROM_REF=" + from, "PRE_COMMIT_TO_REF=" + to, "PRE_COMMIT_REMOTE_NAME="}, extraEnv...)
	return runScriptExit(t, repo, env, "scripts/security/check-push-range.sh")
}

func commitFile(t *testing.T, repo, rel, content, message string) string {
	t.Helper()
	writeRepoFile(t, repo, rel, content)
	runCmd(t, repo, nil, "git", "add", "--", rel)
	runCmd(t, repo, nil, "git", "commit", "-q", "-m", message)
	return runCmd(t, repo, nil, "git", "rev-parse", "HEAD")
}

func TestCheckPushRangeRejectsHiddenAddedContent(t *testing.T) {
	secret := fixtureHomePath("alice") + "/notes"
	cases := map[string]func(t *testing.T, repo string){
		"trailing NUL": func(t *testing.T, repo string) {
			commitFile(t, repo, "blob.txt", "path "+secret+"\n\x00", "add blob")
		},
		"-diff attribute": func(t *testing.T, repo string) {
			commitFile(t, repo, ".gitattributes", "*.txt -diff\n", "mark text as binary")
			commitFile(t, repo, "hidden.txt", "path "+secret+"\n", "add hidden")
		},
		"line starting with ++": func(t *testing.T, repo string) {
			commitFile(t, repo, "plus.txt", "++ "+secret+"\n", "add plus line")
		},
		"merge resolution": func(t *testing.T, repo string) {
			runCmd(t, repo, nil, "git", "checkout", "-q", "-b", "side")
			commitFile(t, repo, "side.txt", "side\n", "side work")
			runCmd(t, repo, nil, "git", "checkout", "-q", "-")
			commitFile(t, repo, "main.txt", "main\n", "main work")
			runCmd(t, repo, nil, "git", "merge", "-q", "--no-ff", "--no-commit", "side")
			writeRepoFile(t, repo, "merged.txt", "path "+secret+"\n")
			runCmd(t, repo, nil, "git", "add", "merged.txt")
			runCmd(t, repo, nil, "git", "commit", "-q", "-m", "merge side")
		},
	}
	for name, add := range cases {
		t.Run(name, func(t *testing.T) {
			repo := initSecurityScriptRepo(t)
			base := commitFile(t, repo, "README.md", "seed\n", "seed")
			add(t, repo)
			code, out := pushRangeExit(t, repo, base, "HEAD")
			if code == 0 || !strings.Contains(out, "local-path") {
				t.Fatalf("expected a local-path block, exit=%d\n%s", code, out)
			}
			if strings.Contains(out, secret) {
				t.Fatal("block printed the matched value")
			}
		})
	}
}

func TestCheckPushRangeFailsClosedOnBadRanges(t *testing.T) {
	repo := initSecurityScriptRepo(t)
	commitFile(t, repo, "README.md", "seed\n", "seed")
	for name, from := range map[string]string{
		"unknown ref":       "bad-ref",
		"missing commit":    "0123456789abcdef0123456789abcdef01234567",
		"option-shaped ref": "--all",
	} {
		if code, out := pushRangeExit(t, repo, from, "HEAD"); code == 0 {
			t.Fatalf("%s: expected failure, got exit 0\n%s", name, out)
		}
	}
	if code, out := pushRangeExit(t, repo, "", "bad-ref"); code == 0 {
		t.Fatalf("bad target: expected failure\n%s", out)
	}
}

func TestCheckPushRangeZeroBaseScansEveryNewCommit(t *testing.T) {
	repo := initSecurityScriptRepo(t)
	commitFile(t, repo, "README.md", "seed\n", "seed")
	commitFile(t, repo, "notes.txt", fixtureHomePath("alice")+"\n", "add path")
	commitFile(t, repo, "later.txt", "clean\n", "later clean commit")
	// The bad commit already sits on another remote. Only the destination
	// remote's refs may be subtracted.
	runCmd(t, repo, nil, "git", "update-ref", "refs/remotes/backup/main", "HEAD")
	if code, out := pushRangeExit(t, repo, zeroSHA, "HEAD"); code == 0 {
		t.Fatalf("zero base without a remote name passed\n%s", out)
	}
	if code, out := pushRangeExit(t, repo, zeroSHA, "HEAD", "PRE_COMMIT_REMOTE_NAME=origin"); code == 0 {
		t.Fatalf("zero base for origin subtracted another remote\n%s", out)
	}
	if code, out := pushRangeExit(t, repo, zeroSHA, "HEAD", "PRE_COMMIT_REMOTE_NAME=backup"); code != 0 {
		t.Fatalf("commits already on the destination remote should be skipped, exit=%d\n%s", code, out)
	}
}

func TestCheckPushRangeAllowsDeletionOnlyRange(t *testing.T) {
	repo := initSecurityScriptRepo(t)
	commitFile(t, repo, "README.md", "seed\n", "seed")
	base := commitFile(t, repo, "old.txt", "old\n", "add old")
	runCmd(t, repo, nil, "git", "rm", "-q", "old.txt")
	runCmd(t, repo, nil, "git", "commit", "-q", "-m", "remove old")
	if code, out := pushRangeExit(t, repo, base, "HEAD"); code != 0 {
		t.Fatalf("deletion-only range failed, exit=%d\n%s", code, out)
	}
}

func TestResolveScanRangeFallsBackToReviewedBase(t *testing.T) {
	repo := initSecurityScriptRepo(t)
	first := commitFile(t, repo, "README.md", "seed\n", "seed")
	reviewed := commitFile(t, repo, "notes.txt", "one\n", "one")
	head := commitFile(t, repo, "notes.txt", "two\n", "two")
	resolve := func(base string) string {
		code, out := runScriptExit(t, repo, nil, "scripts/security/resolve-scan-range.sh", head, base)
		if code != 0 {
			t.Fatalf("resolve %q: exit %d\n%s", base, code, out)
		}
		return strings.TrimSpace(out)
	}
	if got := resolve(first); got != first {
		t.Fatalf("existing base: got %q", got)
	}
	missing := "0123456789abcdef0123456789abcdef01234567"
	for _, base := range []string{"", zeroSHA, missing} {
		if got := resolve(base); got != "" {
			t.Fatalf("base %q without a reviewed base: got %q, want all history", base, got)
		}
	}
	writeRepoFile(t, repo, "scripts/security/reviewed-base", reviewed+"\n")
	for _, base := range []string{"", zeroSHA, missing} {
		if got := resolve(base); got != reviewed {
			t.Fatalf("base %q: got %q, want reviewed base %s", base, got, reviewed)
		}
	}
	runCmd(t, repo, nil, "git", "checkout", "-q", "--orphan", "rewritten")
	other := commitFile(t, repo, "README.md", "rewritten\n", "rewritten root")
	code, out := runScriptExit(t, repo, nil, "scripts/security/resolve-scan-range.sh", other, zeroSHA)
	if code != 0 || strings.TrimSpace(out) != "" {
		t.Fatalf("a reviewed base that is not an ancestor must give all history, exit=%d out=%q", code, out)
	}
}

func initGitleaksRepo(t *testing.T) string {
	t.Helper()
	repo := initSecurityScriptRepo(t)
	copySecurityScript(t, repo, "scripts/security/gitleaks-range.sh")
	copySecurityScript(t, repo, ".gitleaks.toml")
	return repo
}

func gitleaksRangeExit(t *testing.T, repo string, env []string, from, to string) (int, string) {
	t.Helper()
	args := []string{"scripts/security/gitleaks-range.sh", "--to", to}
	if from != "" {
		args = append(args, "--from", from)
	}
	return runScriptExit(t, repo, env, args...)
}

func TestGitleaksRangeScansHiddenContentAndFailsClosed(t *testing.T) {
	requireGitleaks(t)
	secret := fixtureHomePath("alice") + "/notes"

	repo := initGitleaksRepo(t)
	base := commitFile(t, repo, "README.md", "seed\n", "seed")
	head := commitFile(t, repo, "clean.txt", "clean\n", "clean")
	if code, out := gitleaksRangeExit(t, repo, nil, base, head); code != 0 {
		t.Fatalf("clean range: exit %d\n%s", code, out)
	}
	if code, out := gitleaksRangeExit(t, repo, nil, "", head); code != 0 {
		t.Fatalf("full history: exit %d\n%s", code, out)
	}
	if code, out := gitleaksRangeExit(t, repo, nil, head, head); code != 0 || !strings.Contains(out, "no commits to scan") {
		t.Fatalf("empty range: exit %d\n%s", code, out)
	}
	runCmd(t, repo, nil, "git", "rm", "-q", "clean.txt")
	runCmd(t, repo, nil, "git", "commit", "-q", "-m", "delete only")
	deleted := runCmd(t, repo, nil, "git", "rev-parse", "HEAD")
	if code, out := gitleaksRangeExit(t, repo, nil, head, deleted); code != 0 {
		t.Fatalf("deletion-only range: exit %d\n%s", code, out)
	}
	for name, from := range map[string]string{"short": base[:12], "unknown": "0123456789abcdef0123456789abcdef01234567", "spaced": base + " "} {
		if code, out := gitleaksRangeExit(t, repo, nil, from, deleted); code != 2 {
			t.Fatalf("%s base: want exit 2, got %d\n%s", name, code, out)
		}
	}

	nul := initGitleaksRepo(t)
	nulBase := commitFile(t, nul, "README.md", "seed\n", "seed")
	nulHead := commitFile(t, nul, "blob.txt", "path "+secret+"\n\x00", "add blob")
	if code, out := gitleaksRangeExit(t, nul, nil, nulBase, nulHead); code != 1 {
		t.Fatalf("NUL file: want leak exit 1, got %d\n%s", code, out)
	}

	merge := initGitleaksRepo(t)
	commitFile(t, merge, "README.md", "seed\n", "seed")
	runCmd(t, merge, nil, "git", "checkout", "-q", "-b", "side")
	commitFile(t, merge, "side.txt", "side\n", "side")
	runCmd(t, merge, nil, "git", "checkout", "-q", "-")
	parent := commitFile(t, merge, "main.txt", "main\n", "main")
	runCmd(t, merge, nil, "git", "merge", "-q", "--no-ff", "--no-commit", "side")
	writeRepoFile(t, merge, "merged.txt", "path "+secret+"\n")
	runCmd(t, merge, nil, "git", "add", "merged.txt")
	runCmd(t, merge, nil, "git", "commit", "-q", "-m", "merge")
	mergeHead := runCmd(t, merge, nil, "git", "rev-parse", "HEAD")
	if code, out := gitleaksRangeExit(t, merge, nil, parent, mergeHead); code != 1 {
		t.Fatalf("merge-added path: want leak exit 1, got %d\n%s", code, out)
	}

	message := initGitleaksRepo(t)
	messageBase := commitFile(t, message, "README.md", "seed\n", "seed")
	writeRepoFile(t, message, "ok.txt", "ok\n")
	runCmd(t, message, nil, "git", "add", "ok.txt")
	runCmd(t, message, nil, "git", "commit", "-q", "-m", "see "+secret)
	messageHead := runCmd(t, message, nil, "git", "rev-parse", "HEAD")
	code, out := gitleaksRangeExit(t, message, nil, messageBase, messageHead)
	if code != 1 || !strings.Contains(out, messageHead) {
		t.Fatalf("commit message leak: want exit 1 naming the commit, got %d\n%s", code, out)
	}
	if strings.Contains(out, secret) {
		t.Fatal("gitleaks output was not redacted")
	}
}

func TestGitleaksRangeRejectsSilentGitleaksFailures(t *testing.T) {
	repo := initGitleaksRepo(t)
	base := commitFile(t, repo, "README.md", "seed\n", "seed")
	head := commitFile(t, repo, "added.txt", "added\n", "add")
	stubs := map[string]string{
		"logged error":    "echo 'INF 1 commits scanned.'\necho 'ERR [git] fatal: bad revision' >&2\nexit 0\n",
		"zero commits":    "echo 'INF 0 commits scanned.'\necho 'INF no leaks found'\nexit 0\n",
		"no count":        "echo 'INF no leaks found'\nexit 0\n",
		"unexpected exit": "echo 'INF 1 commits scanned.'\nexit 3\n",
	}
	for name, body := range stubs {
		bin := t.TempDir()
		writeExecutable(t, filepath.Join(bin, "gitleaks"), "#!/bin/sh\n"+body)
		env := []string{"PATH=" + bin + string(os.PathListSeparator) + os.Getenv("PATH")}
		if code, out := gitleaksRangeExit(t, repo, env, base, head); code != 2 {
			t.Fatalf("%s: want exit 2, got %d\n%s", name, code, out)
		}
	}
}
