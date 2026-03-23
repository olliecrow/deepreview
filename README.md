# deepreview

deepreview is a local CLI for deep branch reviews.
It runs parallel Codex reviews, consolidates them, executes fixes, verifies outcomes, and keeps looping until Codex produces two consecutive `stop` decisions.

<img width="1209" height="594" alt="image" src="https://github.com/user-attachments/assets/0fc6b1f5-28e2-4d4e-b061-cc24202e6515" />

## Current status

This project is actively maintained and intended for real repository review loops.

## What this project is trying to achieve

Give you a reliable review loop that finds issues, applies fixes safely, and delivers one final result to GitHub.

## What you experience as a user

1. You run deepreview for a repository and source branch.
2. It launches several independent review workers in parallel.
3. Each independent review worker must complete and write its review markdown report; deepreview monitors all Codex workers for activity and restarts stalled workers with bounded retries to avoid pipeline stalls.
4. The execute stage runs two prompts in one Codex thread: first triage/plan, which investigates proposed changes one by one and accepts only high-confidence material work, then implement/verify/finalize/commit.
5. Canonical round artifacts and logs are kept under `~/deepreview/runs/<run-id>/`; Codex stages prompt outputs inside the active worktree first, and deepreview copies the canonical artifacts back into the run directory.
6. Each completed run also writes a canonical run-health summary under `~/deepreview/runs/<run-id>/run-health.{md,json}` so operators can inspect artifact coverage and stderr noise without replaying raw logs.
7. If execute says `continue`, deepreview always runs another review round.
8. If execute says `stop` once, deepreview still runs one confirmation round.
9. If execute says `stop` for two consecutive rounds, deepreview stops the loop, even if the second stop round also changed code.
10. In `pr` mode (default), it runs one fresh Codex delivery stage to confirm merge-ready local state without mutating tracked repository content. If publication is blocked by tracked content or branch history, deepreview routes that blocker back through one bounded recovery cycle on the candidate branch, then deepreview pushes and opens one pull request back into your source branch.
11. If the run made tangible repository changes but did not finish cleanly, that PR is still opened as a draft marked `[INCOMPLETE]`.
12. The final PR title/body are deepreview-generated, human-readable summaries with clear change motivation, round outcomes, and verification highlights, while excluding raw worker/artifact dumps for privacy and size safety.
13. In yolo mode, it pushes directly to your source branch.
14. At completion, TUI mode exits automatically, clears terminal output, and prints a plain-text completion summary with final status and artifact paths.
15. In PR mode, deepreview performs bounded post-create mergeability validation after PR creation; it does not currently run a remote fix/retry/edit loop after the PR is opened.

## Requirements

- supported host operating systems: macOS and Linux
- `git`
- `codex`
- `gh` for default pull request mode
- authenticated local sessions for required tools
- default pull request mode requires a GitHub-backed repo identity; local filesystem origin remotes are rejected in `pr` mode

Optional launcher:

- `multicodex`
  - when available on `PATH`, deepreview always uses `multicodex exec` for Codex prompt runs
  - when a multicodex-backed prompt creates a resumable Codex thread, deepreview records the selected multicodex profile and reuses that profile for later `resume` calls in the same logical context
  - fresh prompt families still start with normal multicodex lowest-usage selection
  - when unavailable, deepreview falls back to `codex exec`
  - set `DEEPREVIEW_REQUIRE_MULTICODEX=1` to fail fast instead of falling back
  - `DEEPREVIEW_CODEX_BIN` only changes the codex fallback path; it does not override a working `multicodex`

## Safety and isolation

- Review and execute work happen under `~/deepreview`, not in your local checkout.
- Run-scoped logs, canonical artifacts, temp directories, and caches live under `~/deepreview/runs/<run-id>/`.
- Codex workers stage prompt-written artifacts under reserved worktree-local `.deepreview/` paths, which deepreview excludes from delivery and copies back into the run directory for canonical storage.
- Codex prompt runs use your normal local Codex configuration by default; deepreview does not force a separate profile/model/runtime layer beyond pinning resumed multicodex-backed threads to the profile that created them.
- Managed repository state is isolated per repo and source branch, so different branches of the same repo can run concurrently without sharing a checkout.
- deepreview blocks concurrent runs only when both the repo and source branch match.
- `pr` mode (default) reviews and publishes the candidate branch itself, then opens a pull request.
- Default pull request mode requires a GitHub-backed repo identity.
- Your current branch and working directory stay untouched in `pr` mode.
- yolo mode is available, and it is off by default.
- Internal `.deepreview/*` artifacts are blocked from delivery commits and pull requests.
- Public delivery surfaces are privacy-guarded (PR title/body and delivery summaries are redacted/guarded before final delivery is accepted).
- Local terminal output is intentionally unredacted so operators can see literal paths and command errors while running deepreview.
- You can cancel at any time with `Ctrl+C`; deepreview prints an interrupt failure summary, cleans up locks/worktrees, and then exits.

## Known limitations

- Windows is unsupported.
- Requires local `git` and `codex`; default pull request mode also requires `gh`.
- Local filesystem origin remotes are not supported in `pr` mode.
- Review quality depends on Codex outputs and repository test coverage.
- Deep runs can take significant time on large repositories or high `--max-rounds`.
- Execute prompt 1 receives review file paths plus a compact manifest, so Codex can inspect candidate items in detail and confirm them individually without forcing large review-summary blocks into the prompt.

## Quick start

1. Make sure tools are installed and authenticated.

- `git`
- `codex`
- `gh`, required for default pull request mode

2. Build deepreview.

```bash
go build -o ./bin/deepreview ./cmd/deepreview
```

3. Run deepreview from inside a GitHub repo checkout.

```bash
./bin/deepreview review
```

4. Optional quick checks before a full run.

```bash
./bin/deepreview doctor
./bin/deepreview dry-run
```

Show command help.

```bash
./bin/deepreview --help
./bin/deepreview review --help
```

5. Optional explicit target repo and source branch.

```bash
./bin/deepreview review <repo> --source-branch <branch>
./bin/deepreview doctor <repo> --source-branch <branch>
./bin/deepreview dry-run <repo> --source-branch <branch>
```

If you want to require `multicodex` on a machine where it is expected to exist:

```bash
DEEPREVIEW_REQUIRE_MULTICODEX=1 ./bin/deepreview doctor
DEEPREVIEW_REQUIRE_MULTICODEX=1 ./bin/deepreview review
```

## Useful options

```bash
./bin/deepreview review <repo> --source-branch <branch> --concurrency 4 --max-rounds 5
./bin/deepreview review <repo> --source-branch <branch> --mode yolo
./bin/deepreview review <repo> --source-branch <branch> --no-tui
./bin/deepreview doctor <repo> --source-branch <branch> --mode pr
./bin/deepreview dry-run <repo> --source-branch <branch> --mode yolo
```

Install shell tab completion.

```bash
# bash
./bin/deepreview completion bash > ~/.local/share/bash-completion/completions/deepreview

# zsh
mkdir -p ~/.zsh/completions
./bin/deepreview completion zsh > ~/.zsh/completions/_deepreview
```

## Short example output

Doctor:

```text
deepreview doctor
repo: owner/repo
source branch: feature/login
mode: pr

[ok] tool available: git
[ok] codex launcher
[ok] gh auth status
[ok] remote source branch reachable

doctor result: PASS
```

Dry run:

```text
deepreview dry-run
repo: owner/repo
source branch: feature/login
mode: pr

planned order:
1. preflight checks
2. acquire per-repo+branch run lock
3. prepare stage
4. round loop
5. delivery stage
6. final summary
```

## Optional shell shortcut

If you run deepreview often, add a short alias:

```bash
alias dr="/path/to/deepreview/bin/deepreview"
```

Then run:

```bash
dr review
```

If your launcher changes directories before invoking deepreview (for example, wrapping `go run` in the deepreview source repo), pass the original caller directory so repo inference stays correct. `DEEPREVIEW_CALLER_CWD` is an explicit override, so deepreview will use it even if the wrapper launches from another repo or a non-repo directory. If it is set to an invalid path or a non-repo directory, deepreview now fails fast instead of silently falling back to `OLDPWD` or the current working directory:

```bash
deepreview() {
  local caller_cwd="$PWD"
  (
    cd /path/to/deepreview || return 1
    DEEPREVIEW_CALLER_CWD="$caller_cwd" go run ./cmd/deepreview review "$@"
  )
}
```

If you are actively editing deepreview source, rebuild after changes:

```bash
go build -o ./bin/deepreview ./cmd/deepreview
```

If you also keep `multicodex` under active development, expose it as a real command on `PATH` rather than relying on a stale copied binary. deepreview only resolves launcher names (`multicodex` first, then `codex`) and does not hardcode repo-specific launcher paths.

## Command summary

- `deepreview review [<repo>] [--source-branch <branch>]`
- `deepreview doctor [<repo>] [--source-branch <branch>]`
- `deepreview dry-run [<repo>] [--source-branch <branch>]`
- `deepreview completion [bash|zsh]`
- `deepreview --help`
- `deepreview review --help`
- `deepreview doctor --help`
- `deepreview dry-run --help`
- `deepreview completion --help`

Common options.

- `--concurrency <n>`
- `--max-rounds <n>`
- `--mode <pr|yolo>`
- `--yolo`
- `--no-tui` (disable full-screen terminal user interface)

## Delivery conventions

- Delivery branch prefix: `deepreview/`
- Pull request title prefix: `deepreview:`

## Documentation map

- [AGENTS.md](AGENTS.md): repository operating instructions and agent constraints
- [docs/spec.md](docs/spec.md): canonical runtime and product behavior
- [docs/architecture.md](docs/architecture.md): pipeline and isolation model
- [docs/workflows.md](docs/workflows.md): execution and note routing conventions
- [docs/decisions.md](docs/decisions.md): durable decision rationale
- [docs/alignment.md](docs/alignment.md): requirement traceability baseline
- [prompts/README.md](prompts/README.md): prompt template pack and execute queue
- [docs/project-preferences.md](docs/project-preferences.md): durable project maintenance preferences
- [docs/untrusted-third-party-repos.md](docs/untrusted-third-party-repos.md): static-analysis-only policy for third-party snapshots

<!-- third-party-policy:start -->
## Third-Party Code Policy
This repository allows external-code snapshots for static analysis only. External clones must stay in ephemeral `plan/` locations, be sanitized immediately (`rm -rf .git`, or remove all remotes first if `.git` is temporarily retained), and must never be executed.

See `docs/untrusted-third-party-repos.md`.
<!-- third-party-policy:end -->
