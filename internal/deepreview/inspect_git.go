package deepreview

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Doctor, dry-run, repo inference, and review readiness read the operator's
// checkout before --trust-repo is checked. Repository config must not run code
// there, so local queries disable fsmonitor, hooks, filter drivers, lazy
// fetches, and transports, and remote queries run outside the checkout so its
// config does not apply.

// inspectionEnv keeps Git from writing the index, prompting, or fetching
// missing objects. GIT_NO_LAZY_FETCH needs Git 2.45; protocol.allow=never
// below blocks the lazy fetch transport on older Git.
func inspectionEnv(extra ...string) []string {
	env := append(os.Environ(), "GIT_OPTIONAL_LOCKS=0", "GIT_NO_LAZY_FETCH=1", "GIT_TERMINAL_PROMPT=0")
	return append(env, extra...)
}

// inspectionConfigOverrides returns -c values that win over every configured
// command-running key Git reads for local inspection.
func inspectionConfigOverrides(gitBin, repoPath string) ([]string, error) {
	overrides := []string{
		"core.fsmonitor=false",
		"core.hooksPath=" + os.DevNull,
		"protocol.allow=never",
	}
	listed, err := RunCommandContextWithEnvAndCallbacks(
		currentRunCommandContext(),
		[]string{gitBin, "-C", repoPath, "config", "--null", "--name-only", "--get-regexp", `^(filter|protocol)\.`},
		"", inspectionEnv(), "", false, 0, nil,
	)
	if err != nil {
		return nil, err
	}
	switch listed.ReturnCode {
	case 0:
	case 1:
		return overrides, nil
	default:
		return nil, NewDeepReviewError("unable to read repository config safely: %s", firstOutputLine(listed.Stdout, listed.Stderr))
	}
	seen := map[string]bool{}
	for _, key := range strings.Split(listed.Stdout, "\x00") {
		section, rest, ok := strings.Cut(key, ".")
		if !ok {
			continue
		}
		dot := strings.LastIndex(rest, ".")
		if dot <= 0 {
			continue
		}
		name := rest[:dot]
		if strings.ContainsAny(name, "=\n") {
			return nil, NewDeepReviewError("unable to inspect repository safely: config has an unsupported %s name", section)
		}
		id := section + "." + name
		if seen[id] {
			continue
		}
		seen[id] = true
		switch section {
		case "filter":
			overrides = append(overrides, id+".clean=", id+".smudge=", id+".process=", id+".required=false")
		case "protocol":
			overrides = append(overrides, id+".allow=never")
		}
	}
	return overrides, nil
}

// inspectGit runs a read-only Git query in an untrusted checkout.
func inspectGit(gitBin, repoPath string, timeout time.Duration, args ...string) (CompletedProcess, error) {
	overrides, err := inspectionConfigOverrides(gitBin, repoPath)
	if err != nil {
		return CompletedProcess{}, err
	}
	command := []string{gitBin, "-C", repoPath, "--no-optional-locks"}
	for _, override := range overrides {
		command = append(command, "-c", override)
	}
	command = append(command, args...)
	return RunCommandContextWithEnvAndCallbacks(currentRunCommandContext(), command, "", inspectionEnv(), "", false, timeout, nil)
}

// inspectGitOutput runs inspectGit and returns trimmed stdout, failing on a
// non-zero exit.
func inspectGitOutput(gitBin, repoPath string, args ...string) (string, error) {
	completed, err := inspectGit(gitBin, repoPath, 0, args...)
	if err != nil {
		return "", err
	}
	if completed.ReturnCode != 0 {
		return "", NewDeepReviewError("git %s failed: %s", strings.Join(args, " "), firstOutputLine(completed.Stdout, completed.Stderr))
	}
	return strings.TrimSpace(completed.Stdout), nil
}

// inspectRemote runs `git ls-remote --exit-code <locator> <ref>` from an
// empty directory, so no repository config (sshCommand, uploadpack, insteadOf,
// credential helpers, protocol policy) applies.
func inspectRemote(gitBin, locator, ref string, timeout time.Duration) (CompletedProcess, error) {
	if strings.HasPrefix(strings.TrimSpace(locator), "-") {
		return CompletedProcess{}, NewDeepReviewError("refusing option-shaped remote locator")
	}
	neutral, err := os.MkdirTemp("", "deepreview-inspect-")
	if err != nil {
		return CompletedProcess{}, err
	}
	defer os.RemoveAll(neutral)
	env := inspectionEnv("GIT_CEILING_DIRECTORIES=" + filepath.Dir(neutral))
	command := []string{gitBin, "ls-remote", "--exit-code", locator, ref}
	return RunCommandContextWithEnvAndCallbacks(currentRunCommandContext(), command, neutral, env, "", false, timeout, nil)
}

// inspectRemoteRef returns the SHA that the remote advertises for ref.
func inspectRemoteRef(gitBin, locator, ref string, timeout time.Duration) (string, error) {
	completed, err := inspectRemote(gitBin, locator, ref, timeout)
	if err != nil {
		return "", err
	}
	if completed.ReturnCode != 0 {
		return "", NewDeepReviewError("remote did not advertise %s: %s", ref, firstOutputLine(completed.Stdout, completed.Stderr))
	}
	fields := strings.Fields(completed.Stdout)
	if len(fields) == 0 {
		return "", NewDeepReviewError("remote did not advertise %s", ref)
	}
	return fields[0], nil
}
