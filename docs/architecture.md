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
- when source branch equals default branch, treat branch diff as orientation only and run independent review as a current-state repository audit
- require all review workers to complete successfully and emit one markdown review artifact each
- monitor worker activity (stdout/stderr + filesystem/git-change evidence); cancel and restart inactive workers with bounded retries
- collect report artifacts needed for execute prompts by copying worker-written review files from worktree-local paths into the canonical run directory
- inject compact review summaries into execute prompt 1 and also provide on-disk review paths so Codex can inspect full reports directly when needed
- aggressively remove independent-review worktrees
- create fresh execute worktree from current candidate head
- run ordered execute prompt queue in one Codex chat context:
  - prompt 1: consolidate and plan (reviews are inputs, not gospel; accept only high-conviction items and produce the round plan)
  - prompt 2: execute/verify (apply approved changes, prefer simplification/removal when it cleanly resolves accepted issues, and run codex-led verification with evidence output)
  - prompt 3: cleanup/summary/commit (docs/decision upkeep, round status flag write, and complete round artifacts)
- execute prompts stage their output files inside reserved worktree-local `.deepreview/artifacts/` paths; after prompt queue completion, the orchestrator validates those staged files, persists canonical copies into the run directory, performs execute-stage post-processing (artifact validation, hygiene checks, and local auto-commit when changes exist), and then writes the authoritative `round.json` completion record for that round
- apply the same inactivity watchdog/restart policy to execute and post-delivery Codex workers
- Codex workers run with the operator's normal local Codex configuration and inherited local environment; deepreview does not add a separate execution/temp-cache layer
- allow local checkpoint commits throughout execution; never push during rounds
- Codex writes the round status file inside the execute worktree, and the orchestrator persists the canonical copy at `~/deepreview/runs/<run-id>/round-<round>/round-status.json` with enum decision (`continue|stop`) and rationale
- the orchestrator writes `~/deepreview/runs/<run-id>/round-<round>/round.json` after successful execute-stage completion; final completion reporting counts only rounds with that authoritative record
- aggressively remove execute worktree and transient per-round artifacts
- if execute status is `continue`, reset the stop streak and continue
- if execute status is the first consecutive `stop`, keep the candidate head and run one confirmation round
- if execute status is the second consecutive `stop`, stop the round loop even if that round also changed the repository
- if another round is still required at the configured max, fail and require rerun with a higher `--max-rounds`

4. Final delivery (single push point):
- require completed round execution and no blocking execute-stage verification failures
- `pr` mode (default): run one Codex PR-preparation pass in a candidate-branch worktree; then run bounded pre-delivery privacy remediation attempts (up to 3 Codex-guided passes) in a candidate-branch worktree; early stop is allowed only after clean post-attempt scans and a clean remediation worktree after deepreview auto-commits any simple residual edits; then create/push delivery branch, open PR into source branch, then run one fresh Codex post-delivery prompt to generate a clear final PR title + description and update both via `gh pr edit`; terminal outcomes are complete PR, incomplete draft PR, or failure
- `yolo` mode: push committed candidate state directly to source branch

5. Finalization:
- if TUI mode was active, exit TUI immediately on completion and clear terminal screen before summary output
- emit final summary and alignment evidence pointers
- ensure no stale transient worktrees/artifacts remain

## Isolation model
- deepreview never edits the user's own active checkout.
- deepreview isolates managed clone state per repo plus source branch; concurrent same-repo runs are allowed only when source branches differ.
- each independent review runs in a separate worktree.
- each execute pass runs in a separate fresh worktree.
- each round uses fresh worktrees to minimize cross-round context carryover.

## Safety model
- default mode is `pr` and must not push source branch directly.
- `yolo` mode is explicit opt-in.
- no pushes occur during intermediate rounds.
- in PR mode, a Codex PR-preparation pass can make optional final tidy/history changes before privacy remediation; if no prep changes are needed it should leave the branch untouched.
- in PR mode, privacy remediation runs as a bounded pre-delivery Codex loop (up to 3 attempts) over delivery commit messages and changed files; early stop requires clean post-attempt scans and a clean remediation worktree after deepreview auto-commits any simple residual edits, then delivery proceeds by policy if bounded attempts are exhausted.
- privacy guardrails remain enforced on delivery/public text surfaces (PR title/body and delivery summaries).
- local terminal progress/error output is intentionally literal and unredacted for operator debugging.
- verification execution is codex-led (tests, pre-commit checks, locally runnable CI-like checks when available) with explicit evidence in round/final summaries.

## Simplicity model
- no unbounded retry/backoff/self-healing loops; only bounded inactivity restarts with explicit per-worker caps.
- fail fast on failed stages, then report clearly.
- prioritize straightforward control flow over production-hardening complexity.
- keep CLI controls minimal; prefer sensible defaults over large option surfaces.

## Authentication model
- Git auth uses local `git`/`gh` session.
- Codex auth uses the local launcher-selected Codex CLI session/subscription.
- When routed through `multicodex`, deepreview first validates `multicodex exec` itself and then relies on `multicodex status` to confirm that at least one logged-in profile is available. Fresh prompt contexts use normal multicodex selection; resumed contexts pin the creating profile.
- core workflows must not require repository-stored API keys.
