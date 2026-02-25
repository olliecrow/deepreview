# deepreview

deepreview is a local CLI that performs deep branch reviews using parallel Codex independent reviews and iterative execute rounds, then delivers one final result to GitHub.

## What deepreview does
- Reviews a specific source branch against the repository default branch context.
- Runs independent reviews concurrently in isolated worktrees.
- Consolidates findings in execute rounds and applies verified fixes locally.
- Runs a staged execute prompt queue (consolidate reviews, plan, execute/verify, cleanup/summary/finalize) in one Codex context per round.
- Repeats review/execute for bounded rounds (default `5`) with change-driven early stop.
- Round progression rule: if execute phase makes repository changes, deepreview must run at least one additional review round before delivery.
- Stop rule: if execute phase produces no repository changes, deepreview stops the round loop.
- If `--max-rounds` is too low to allow that post-change review round, deepreview fails with a clear error.
- Pushes exactly once at final delivery:
  - default: opens a PR into the original source branch
  - `yolo` mode: pushes directly to the original source branch

## Key capabilities
- Managed workspace isolation (`~/deepreview`) so user checkouts are untouched.
- Optional repo/branch inference from current directory when run inside a valid GitHub repo.
- Local branch-readiness validation for inferred source branch:
  - no tracked local changes
  - local branch synchronized with upstream remote branch
- Fresh worktrees per independent review/execute stage to reduce stale context carryover.
- Fresh managed clone replacement each run to avoid stale repository/worktree state.
- Configurable independent review concurrency (default `4`).
- Configurable max rounds (default `5`).
- Auto-launches a full-screen terminal UI when running in an interactive terminal.
  - TUI is non-interactive and passively streams live run state.
  - TUI uses structured panels (`run context`, `live summary`, `stage timeline`, `status`) for clearer live progress.
  - On wide terminals, context and summary are shown side-by-side.
  - Stage marker legend is shown in-footer (`> active`, `+ done`, `x failed`, `~ running`).
  - TUI has a compact mode for very small terminal sizes.
- Non-interactive/`--no-tui` runs stream structured text progress logs with elapsed timings.
- Prints an explicit completion summary after run exit (including PR URL in `pr` mode or commits URL in `yolo` mode).
- Aggressive cleanup of stale worktrees/transient artifacts.
- Codex-first decision flow with codex-led verification.
- In `yolo` mode, default-branch direct-push permission is preflight-checked before rounds.
- Codex execution is pinned to `gpt-5.3-codex` with `model_reasoning_effort=xhigh` for all review/execute prompts.
- Internal deepreview operational artifacts are never delivered into repository diffs (`.deepreview/*` is blocked from commit/push/PR delivery).
- PR body is generated from per-round review/execute artifacts with sanitization for local system paths and secret-like patterns.
- Parallel runs are supported across different repositories.
- Same-repo runs are serialized with a per-repo run lock (a second concurrent run for the same repo is rejected).

## Requirements
- Go 1.26+
- `git`
- `codex` CLI authenticated locally
- `gh` CLI authenticated locally (required for default PR mode)

## Managed directories
- Workspace root: `~/deepreview`
- Managed repo clones: `~/deepreview/repos/<owner>/<repo>/`
- Run artifacts/logs: `~/deepreview/runs/<run-id>/`

## Quickstart
1. Ensure local tools are available and authenticated (`git`, `codex`, `gh`).
2. Build deepreview:
```bash
go build -o ./bin/deepreview ./cmd/deepreview
```
3. Run deepreview:
```bash
./bin/deepreview review
```
   - If run from a valid GitHub repo checkout, deepreview infers `<repo>` and `--source-branch` from current context.
   - If inferred source branch is used, deepreview requires:
     - no tracked local changes
     - local branch synchronized with upstream remote branch
4. Optional explicit target repo + source branch:
```bash
./bin/deepreview review <repo> --source-branch <branch>
```
   - If `<repo>` is a local path, it must have `remote.origin.url` configured.
   - deepreview always clones/fetches into its managed workspace and does not run review work in your local repo directory.
5. Optional controls:
```bash
./bin/deepreview review <repo> --source-branch <branch> --concurrency 4 --max-rounds 5
```
6. Optional direct-push mode:
```bash
./bin/deepreview review <repo> --source-branch <branch> --mode yolo
# equivalent:
./bin/deepreview review <repo> --source-branch <branch> --yolo
```
7. Optional non-interactive/plain mode:
```bash
./bin/deepreview review <repo> --source-branch <branch> --no-tui
```

## Optional shell alias
If you run deepreview often, adding a shell alias can speed up usage.

Example alias (short command `dr`):
```bash
alias dr="/absolute/path/to/deepreview/bin/deepreview"
```

Add it permanently:
- `zsh`:
```bash
echo 'alias dr="/absolute/path/to/deepreview/bin/deepreview"' >> ~/.zshrc
source ~/.zshrc
```
- `bash`:
```bash
echo 'alias dr="/absolute/path/to/deepreview/bin/deepreview"' >> ~/.bashrc
source ~/.bashrc
```

After that, run:
```bash
dr review
```

## Command summary (v1 contract)
- `deepreview review [<repo>] [--source-branch <branch>]`
- `deepreview --help` (top-level help)
- `deepreview review --help` (detailed command help, flags, env overrides, troubleshooting)
- Inference defaults:
  - if `<repo>` is omitted and cwd is a valid GitHub repo, deepreview uses cwd repo
  - if `--source-branch` is omitted, deepreview uses current branch from local repo context
- Common options:
  - `--concurrency <n>`
  - `--max-rounds <n>`
  - `--mode <pr|yolo>` (case-insensitive values)
  - `--yolo` (legacy `--YOLO` also accepted)
  - `--no-tui`

## Delivery conventions
- Delivery branch prefix: `deepreview/`
- PR title prefix: `deepreview:`

## Documentation Map
- [AGENTS.md](AGENTS.md): repository operating instructions and agent constraints
- [docs/spec.md](docs/spec.md): canonical runtime/product contract
- [docs/architecture.md](docs/architecture.md): pipeline and isolation model
- [docs/workflows.md](docs/workflows.md): execution and note-routing conventions
- [docs/decisions.md](docs/decisions.md): durable decision rationale
- [docs/alignment.md](docs/alignment.md): requirement traceability baseline
- [prompts/README.md](prompts/README.md): prompt template pack and execute queue
- [docs/project-preferences.md](docs/project-preferences.md): durable project maintenance preferences
