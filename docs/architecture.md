# deepreview architecture

This document captures the stable architecture of deepreview.

## Primary objective
Run deepreview workflows against a remote source branch using isolated worktrees, iterative round-based execute passes, and one final delivery push.

## CLI command surface
- `deepreview review` runs the full multi-round orchestration workflow.
- `deepreview doctor` runs non-mutating preflight checks and exits with pass/fail status.
- `deepreview dry-run` prints resolved run context and planned stage order without running Codex or mutating git state.

## Core pipeline
1. Resolve input and source context:
- target repo (explicit or inferred from current local GitHub repo context)
- source branch (explicit or inferred current branch from local repo context)
- default branch
- mode (`pr` or `yolo`)
- concurrency (default `4`)
- max rounds (default `5`)
- UI mode (`--tui` opt-in full-screen UI, default structured text progress logs, optional `--no-tui` force-off)
- if source branch is inferred from local repo context, require local readiness:
  - no tracked local changes
  - local branch exactly synchronized with upstream remote branch

2. Prepare managed workspace under `~/deepreview`:
- replace stale managed checkout with a fresh clone
- fetch latest remote refs
- resolve source-branch head SHA
- initialize candidate head to latest remote source branch
- initialize or reuse local candidate branch `deepreview/candidate/<source-branch>/<run-id>`
- in `yolo` mode when source branch is default branch, run push dry-run preflight before rounds

3. Round loop (`round = 1..max_rounds`):
- create `N` independent review worktrees from current candidate head
- run `N` concurrent Codex independent reviews using one shared independent-review prompt template
- require one markdown review artifact per worker
- wait for all independent-review workers
- collect report artifacts needed for execute prompts
- aggressively remove independent-review worktrees
- create fresh execute worktree from current candidate head
- run ordered execute prompt queue in one Codex chat context:
  - prompt 1: consolidate reviews (reviews are inputs, not gospel; decisioning only, no code edits)
  - prompt 2: plan (high-conviction, end-to-end implementation and verification plan; no code edits)
  - prompt 3: execute/verify (apply approved changes and run codex-led verification with evidence output)
  - prompt 4: cleanup/summary/commit (docs/decision upkeep, round status flag write, ensure changed work is committed locally)
- allow local checkpoint commits throughout execution; never push during rounds
- Codex writes round status file at `~/deepreview/runs/<run-id>/round-<round>/round-status.json` with enum decision (`continue|stop`) and rationale
- aggressively remove execute worktree and transient per-round artifacts
- if execute produced changes, update candidate head to latest local committed state and continue
- if execute produced no changes, stop the round loop early

4. Final delivery (single push point):
- require completed round execution and no blocking verification failures
- `pr` mode (default): create/push delivery branch, open PR into source branch with deterministic detailed artifact body, then run one fresh Codex post-delivery summary prompt and prepend that summary to the PR description via PR edit
- `yolo` mode: push committed candidate state directly to source branch

5. Finalization:
- emit final summary and alignment evidence pointers
- ensure no stale transient worktrees/artifacts remain

## Isolation model
- deepreview never edits the user's own active checkout.
- each independent review runs in a separate worktree.
- each execute pass runs in a separate fresh worktree.
- each round uses fresh worktrees to minimize cross-round context carryover.

## Safety model
- default mode is `pr` and must not push source branch directly.
- `yolo` mode is explicit opt-in.
- no pushes occur during intermediate rounds.
- verification and secret-hygiene checks gate final delivery.
- verification execution is codex-led (tests, pre-commit checks, locally runnable CI-like checks when available) with explicit evidence in round/final summaries.

## Simplicity model (v1)
- no automatic retry/backoff/self-healing loops.
- fail fast on failed stages, then report clearly.
- prioritize straightforward control flow over production-hardening complexity.
- keep CLI controls minimal; prefer sensible defaults over large option surfaces.

## Authentication model
- Git auth uses local `git`/`gh` session.
- Codex auth uses local Codex CLI session/subscription.
- core workflows must not require repository-stored API keys.
