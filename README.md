# deepreview

deepreview is a local CLI for deep branch reviews.
It runs parallel Codex reviews, consolidates them, executes fixes, verifies outcomes, and keeps looping until the execute phase makes no code changes.

<img width="1258" height="622" alt="Screenshot 2026-02-25 at 23 07 17" src="https://github.com/user-attachments/assets/f8441f56-159f-410f-a3fc-0b0bd9c30504" />

## What this project is trying to achieve

Give you a reliable review loop that finds issues, applies fixes safely, and delivers one final result to GitHub.

## What you experience as a user

1. You run deepreview for a repository and source branch.
2. It launches several independent review workers in parallel.
3. Each worker writes a review markdown report.
4. The execute stage combines reports, plans changes, applies fixes, and verifies results.
5. It runs cleanup, summary, and commit steps in an isolated managed workspace.
6. If execute changed code, deepreview runs another review round.
7. If execute made no code changes, deepreview stops the loop.
8. In default mode, it opens one pull request back into your source branch.
9. In default mode, it then runs one short post-delivery Codex pass to prepend a human summary at the top of the PR description while keeping full detailed artifacts below.
10. In yolo mode, it pushes directly to your source branch.

## Safety and isolation

- Review and execute work happen under `~/deepreview`, not in your local checkout.
- Default mode works on a delivery branch and opens a pull request.
- Your current branch and working directory stay untouched in default mode.
- yolo mode is available, and it is off by default.
- Internal `.deepreview/*` artifacts are blocked from delivery commits and pull requests.

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
2. acquire per-repo run lock
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
    cd /Users/oc/repos/me/deepreview || return 1
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
- `deepreview --help`
- `deepreview review --help`
- `deepreview doctor --help`
- `deepreview dry-run --help`

Common options.

- `--concurrency <n>`
- `--max-rounds <n>`
- `--mode <pr|yolo>`
- `--yolo`
- `--no-tui`

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
