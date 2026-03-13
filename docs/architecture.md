# deepreview architecture

This document captures the stable architecture of deepreview.

## Primary objective
Run deepreview workflows against a remote source branch using isolated worktrees, iterative round-based execution, and one final Codex-owned delivery stage.

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
- max rounds (default `5`) for total execute rounds
- UI mode (full-screen UI by default when terminal capabilities are valid; optional `--no-tui` force-off for structured text logs)
- resolve Codex launcher: use `multicodex` whenever it is available on `PATH`, otherwise use `codex`; if `DEEPREVIEW_REQUIRE_MULTICODEX=1`, fail instead of falling back
- multicodex routing is dynamic only for fresh prompt contexts; once deepreview needs to resume a multicodex-backed Codex thread, it reuses the profile that created that thread
- if source branch is inferred from local repo context, require local readiness:
  - no tracked local changes
  - local branch exactly synchronized with upstream remote branch after refreshing the tracked upstream ref

2. Prepare managed workspace under `~/deepreview`:
- replace stale managed checkout for the source branch with a fresh clone
- fetch latest remote refs
- resolve source-branch head SHA
- resolve the operator's Git identity from the source repository Git config first, then global Git config, and apply it to the managed clone with local signing disabled for deepreview-owned commits
- initialize candidate head to latest remote source branch
- initialize or reuse local candidate branch `deepreview/candidate/<source-branch>/<run-id>`
- in `yolo` mode when source branch is default branch, run push dry-run preflight before rounds

3. Round loop (`round = 1..max_rounds`):
- create `N` independent review worktrees from current candidate head
- run `N` concurrent Codex independent reviews using one shared independent-review prompt template
- each review worker starts in a fresh isolated context and writes one canonical review artifact
- when source branch equals default branch, treat branch diff as orientation only and run independent review as a current-state repository audit
- require all review workers to complete successfully and emit one markdown review artifact each
- monitor worker activity (stdout/stderr + filesystem/git-change evidence); cancel and restart inactive workers with bounded retries
- collect report artifacts needed for execute prompts by copying worker-written review files from worktree-local paths into the canonical run directory
- pass execute prompt 1 review artifact paths plus a compact manifest; let Codex open full review files directly instead of injecting large review-summary blocks
- aggressively remove independent-review worktrees
- create fresh execute worktree from current candidate head
- start a fresh Codex context for the execute stage
- run ordered execute prompt queue in one Codex chat context:
  - prompt 1: triage and plan (reviews are inputs, not gospel; accept only high-confidence material items and produce the round plan)
  - prompt 2: implement, verify, finalize, update docs/decisions, write round artifacts, and create any needed local commit
- when a mutable execute or delivery worktree is retried after inactivity, reset it to the immutable last clean candidate-branch SHA captured before that attempt before rerunning so abandoned edits/commits from the stalled attempt do not survive into later history
- execute retries preserve only artifacts from earlier successful prompts in the same queue; final round status/summary artifacts must be rewritten by the successful prompt-2 attempt
- execute prompts stage their output files inside reserved worktree-local `.deepreview/artifacts/` paths; after prompt queue completion, the orchestrator validates those staged files, persists canonical copies into the run directory, and then writes the authoritative `round.json` completion record for that round
- apply the same inactivity watchdog/restart policy to execute and delivery Codex workers
- Codex workers run with the operator's normal local Codex configuration and inherited local environment; deepreview does not add a separate execution/temp-cache layer, except that resumed multicodex-backed threads stay pinned to the profile that created the thread
- allow local checkpoint commits throughout execution; never push during rounds
- Codex writes the round status file inside the execute worktree, and the orchestrator persists the canonical copy at `~/deepreview/runs/<run-id>/round-<round>/round-status.json` with enum decision (`continue|stop`) and rationale
- the orchestrator writes `~/deepreview/runs/<run-id>/round-<round>/round.json` after successful execute-stage completion; final completion reporting counts only rounds with that authoritative record
- aggressively remove execute worktree and transient per-round artifacts
- if execute status is `continue`, reset the stop streak and continue
- if execute status is the first consecutive `stop`, keep the candidate head and run one confirmation round
- if execute status is the second consecutive `stop`, stop the round loop even if that round also changed the repository
- if another round is still required at the configured max, fail and require rerun with a higher `--max-rounds`

4. Final delivery (single delivery stage):
- require completed round execution and no blocking execute-stage verification failures
- create a fresh delivery worktree and a fresh Codex context
- run one Codex delivery prompt that owns final local repo mutation work:
  - inspect candidate diff/history and prior verification evidence
  - run any remaining local merge-readiness checks
  - optionally move work onto the delivery branch locally
  - report whether local delivery preparation is complete or incomplete
  - write only local-readiness result fields for the orchestrator (mode, optional prepared delivery branch, incomplete status/reason), not push refspecs or PR metadata
- the orchestrator validates the prepared ref, pushes it, creates the PR in `pr` mode, and performs bounded post-create mergeability validation before classifying final success/failure
- when delivery is blocked by PR-range/history state outside the current prepared tip, deepreview reports the blocker precisely and stops; it does not perform history rewrite/rebuild recovery automatically
- in `yolo` mode, the orchestrator pushes the prepared source-branch ref instead of creating a PR
- the orchestrator still stays out of repo-specific local mutation logic except for worktree lifecycle, prompt launching/resume, artifact validation, remote publication, and terminal classification

5. Finalization:
- if TUI mode was active, exit TUI immediately on completion and clear terminal screen before summary output
- emit final summary and alignment evidence pointers; successful terminal states backfill the root `final-summary.md` if an earlier path failed to write it
- ensure no stale transient worktrees/artifacts remain

## Fresh-context model
- Independent reviewers never share chat history with one another.
- Each round execute stage starts from a fresh context and keeps continuity only within that stage's prompt queue.
- Delivery starts from a fresh context and never inherits execute chat history.
- A new round always means a new execute context.
- Any retry after inactivity on a mutable stage resets both:
  - the worktree
  - the Codex context

## Exact stage tree

```text
deepreview
├── 0. preflight
│   ├── resolve repo + branch + mode
│   ├── acquire repo+branch run lock
│   ├── prepare managed clone/workspace
│   └── validate prompt assets + required tools
├── 1. review round N
│   ├── create independent review worktrees
│   ├── run K reviewers in fresh isolated contexts
│   ├── collect canonical review artifacts
│   └── delete review worktrees
├── 2. execute round N
│   ├── create fresh execute worktree
│   ├── start fresh execute context
│   ├── prompt 1: triage + plan
│   ├── prompt 2: implement + verify + docs + summary + status + commit
│   ├── validate canonical artifacts
│   ├── write round.json
│   └── delete execute worktree
├── 3. round control
│   ├── continue => next round
│   ├── first stop => confirmation round
│   └── second consecutive stop => exit round loop
├── 4. delivery
│   ├── create fresh delivery worktree
│   ├── start fresh delivery context
│   ├── Codex finalizes local branch state for publication
│   ├── deepreview pushes and creates PR (or yolo push)
│   ├── deepreview waits briefly for mergeability to settle
│   ├── validate delivery outcome
│   └── delete delivery worktree
└── 5. finalize
    ├── write final-summary.md
    ├── print terminal outcome
    └── clean transient leftovers
```

## Isolation model
- deepreview never edits the user's own active checkout.
- deepreview isolates managed clone state per repo plus source branch; concurrent same-repo runs are allowed only when source branches differ.
- each independent review runs in a separate worktree and separate fresh context.
- each execute pass runs in a separate fresh worktree and fresh context.
- each delivery stage runs in a separate fresh worktree and fresh context.
- each round uses fresh worktrees to minimize stale code/context carryover.

## Safety model
- `pr` mode is the default and must not push source branch directly.
- `yolo` mode is explicit opt-in.
- no pushes occur during intermediate rounds.
- PR mode performs bounded post-create mergeability validation before it reports terminal delivery success.
- public delivery surfaces remain privacy-guarded (PR title/body and delivery summaries).
- local terminal progress/error output is intentionally literal and unredacted for operator debugging.
- verification execution is Codex-led (tests, pre-commit checks, locally runnable CI-like checks when available) with explicit evidence in round/final summaries.
- delivery must leave the branch/PR in a state where the operator can approve and merge without extra manual cleanup when autonomous completion succeeds.

## Simplicity model
- no unbounded orchestrator retry/backoff/self-healing loops; only bounded inactivity restarts with explicit per-worker caps.
- fail fast on failed stages, then report clearly.
- prefer straightforward control flow over production-hardening complexity.
- keep CLI controls minimal; prefer sensible defaults over large option surfaces.
- keep the orchestrator thin and deterministic; let Codex own repo mutation and repo-specific reasoning whenever practical.

## Authentication model
- Git auth uses local `git`/`gh` session.
- Codex auth uses the local launcher-selected Codex CLI session/subscription.
- when routed through `multicodex`, deepreview first validates `multicodex exec` itself and then relies on `multicodex status` to confirm that at least one logged-in profile is available. Fresh prompt contexts use normal multicodex selection; resumed contexts pin the creating profile.
- core workflows must not require repository-stored API keys.
