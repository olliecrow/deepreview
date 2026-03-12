# deepreview

deepreview is a local CLI for deep branch reviews.
It runs parallel Codex reviews, consolidates them, executes fixes, verifies outcomes, and keeps looping until the execute phase makes no code changes.

<img width="1209" height="594" alt="image" src="https://github.com/user-attachments/assets/0fc6b1f5-28e2-4d4e-b061-cc24202e6515" />

## Current status

This project is actively maintained and intended for real repository review loops.

## What this project is trying to achieve

Give you a reliable review loop that finds issues, applies fixes safely, and delivers one final result to GitHub.

## What you experience as a user

1. You run deepreview for a repository and source branch.
2. It launches several independent review workers in parallel.
3. Each independent review worker must complete and write its review markdown report; deepreview monitors all Codex workers for activity and restarts stalled workers with bounded retries to avoid pipeline stalls.
4. The execute stage runs three prompts in one Codex thread: consolidate and plan, execute and verify, cleanup/summary/commit.
5. Round artifacts, logs, temp directories, and caches are written under the run directory in `~/deepreview/runs/<run-id>/`, not into the repo worktree.
6. If execute changed code, deepreview runs another review round.
7. If the last allowed execute round changed code, deepreview automatically adds one final audit round with the same review bar and no repository edits.
8. If execute made no code changes, deepreview stops the loop.
9. In default mode, it opens one pull request back into your source branch; if the run made tangible repository changes but did not finish cleanly, it still opens a draft PR marked `[INCOMPLETE]`.
10. In default mode, it then runs one post-delivery Codex pass to generate the final PR title and description for complete PR deliveries.
11. The final PR title/body are Codex-generated, human-readable summaries with clear change motivation, round outcomes, and verification highlights, while excluding raw worker/artifact dumps for privacy and size safety.
12. In yolo mode, it pushes directly to your source branch.
13. At completion, TUI mode exits automatically, clears terminal output, and prints a plain-text completion summary with final status and artifact paths.
14. Before delivery, deepreview resolves candidate branch HEAD and runs repository quality gates in a detached worktree snapshot of that candidate (`pre-commit run --all-files` when configured, plus `./setup_env.sh` when present).
15. In PR mode, deepreview runs a bounded privacy remediation loop (up to 3 Codex-guided attempts) in a candidate-branch worktree immediately before push/PR delivery, and then proceeds with PR delivery by policy after the bounded attempts.

## Requirements

- supported host operating systems: macOS and Linux
- `git`
- `codex`
- `gh` for default pull request mode
- authenticated local sessions for required tools

## Safety and isolation

- Review and execute work happen under `~/deepreview`, not in your local checkout.
- Run-scoped logs, artifacts, temp directories, and caches live under `~/deepreview/runs/<run-id>/`.
- Managed repository state is isolated per repo and source branch, so different branches of the same repo can run concurrently without sharing a checkout.
- deepreview blocks concurrent runs only when both the repo and source branch match.
- Default mode works on a delivery branch and opens a pull request.
- Your current branch and working directory stay untouched in default mode.
- yolo mode is available, and it is off by default.
- Internal `.deepreview/*` artifacts are blocked from delivery commits and pull requests.
- Public delivery surfaces are privacy-guarded (PR title/body and delivery summaries are redacted/guarded; PR mode also runs bounded pre-delivery privacy remediation attempts over delivery commit messages and changed files).
- Local terminal output is intentionally unredacted so operators can see literal paths and command errors while running deepreview.
- You can cancel at any time with `Ctrl+C`; deepreview performs lock/worktree cleanup before exit.

## Known limitations

- Windows is unsupported.
- Requires local `git` and `codex`; default pull request mode also requires `gh`.
- Review quality depends on Codex outputs and repository test coverage.
- Deep runs can take significant time on large repositories or high `--max-rounds`.
- Execute prompt 1 receives compact review summaries plus file paths to the full on-disk reviews, so Codex can read more detail when it chooses without forcing all review text into the prompt.

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
[ok] tool available: codex
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

If your launcher changes directories before invoking deepreview (for example, wrapping `go run` in the deepreview source repo), pass the original caller directory so repo inference stays correct:

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
