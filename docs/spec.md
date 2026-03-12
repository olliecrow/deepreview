# deepreview spec

This document defines the canonical runtime and product contract for `deepreview`. Keep it evergreen and consistent with implementation.

## Definitions
- **deepreview**: the CLI orchestrator in this repository.
- **target repo**: the repository being reviewed.
- **source branch**: the branch chosen for the deepreview run.
- **default branch**: repository default branch (for example `main` or `master`).
- **independent review**: one independent Codex review run in one isolated worktree.
- **execute pass**: per-round consolidation run that applies selected changes in a fresh execute worktree.
- **round**: one independent review stage plus one execute stage, ending in local commit/no-push unless it is final delivery.

## Required invariants
- deepreview documentation and code must not reference external inspiration project names; patterns may be reused without cross-project coupling in artifacts.
- project/tool naming is always `deepreview` (lowercase, one word).
- repository posture is open-source-ready even while private.
- no secrets, confidential data, or personal information may be committed.
- supported host operating systems are macOS and Linux only; Windows compatibility is out of scope and should be removed rather than maintained.
- deepreview operates only in managed workspace paths under `~/deepreview`.
- deepreview must not operate in the user's own active checkout.
- deepreview must isolate managed repository clones and run locks by repo plus source branch, so different branches of the same repo can run concurrently while same-branch runs remain serialized.
- deepreview-managed commits must use the operator's resolved Git identity from the source repository Git config when present, otherwise the operator's global Git config, and must not depend on host GPG signing configuration.
- if repo/source-branch are omitted, deepreview may infer them from current local GitHub repo context.
- when launched from the deepreview source repo via wrappers that `cd` before execution, repo inference may fall back to caller context (`DEEPREVIEW_CALLER_CWD` first, then `OLDPWD`) to avoid silently targeting the tool repo.
- source branch resolution requires local readiness checks when it targets the current local branch context (inferred branch, or explicit `--source-branch` matching current local branch): no tracked local changes and exact local/upstream synchronization.
- deepreview keeps orchestration simple with bounded self-healing only: inactivity-based worker restarts are allowed with explicit per-worker restart caps.
- codex prompt executions use a fixed timeout of 3600 seconds per prompt.
- deepreview runs must be interruptible via `Ctrl+C` at any point; on interrupt, active worker commands are terminated immediately, then lock/worktree cleanup runs before process exit.
- round loop runs up to `--max-rounds` (default `5`) code-changing execute rounds and may stop early.
- independent reviews run in independent worktrees.
- independent review concurrency defaults to `4` and is configurable.
- each successful independent-review worker must emit one markdown review report.
- independent review rounds require full worker coverage: required successful workers = `concurrency`.
- independent-review reports are strictly severity-first and include only high-confidence `critical|high` merge-relevant issues.
- independent-review and execute/delivery Codex workers are monitored for activity signals (stdout/stderr output plus filesystem/git-change evidence).
- if a worker is inactive for the configured timeout, deepreview cancels and restarts that worker up to the configured restart cap.
- each execute pass runs in a fresh worktree.
- independent-review workers use one shared independent-review prompt template.
- when the source branch equals the default branch, independent review treats branch diff as orientation only and continues as a current-state repository audit.
- each execute pass runs an ordered multi-prompt queue in one Codex chat context.
- execute prompt-1 receives compact injected review summaries plus on-disk review report paths so Codex can inspect full reports directly when needed.
- execute prompt-1 (consolidate and plan) treats independent reviews as inputs, not gospel, accepts only independently-validated, high-confidence `critical|high` items, and produces the execution plan for the round.
- execute stage validates `round-triage.md` and fails the round if any `accept` item is missing severity/confidence tags or does not satisfy `severity in {critical, high}` and `confidence=high`.
- execute prompt-2 (execute/verify) must run end-to-end implementation plus minimum local verification gates (tests, pre-commit checks, locally runnable CI-like checks when available), with evidence output.
- execute prompt-3 (cleanup/summary/commit) must include docs/notes/decision upkeep, write complete round artifacts, and ensure changed work is committed locally.
- Codex prompt workers must write prompt outputs inside their current worktree sandbox; deepreview then persists canonical per-round artifacts (`round-summary.md`, `round-status.json`, and related round outputs) under `~/deepreview/runs/<run-id>/round-<round>/`.
- execute worktrees must install deepreview-managed untracked excludes for local operational directories (for example `.deepreview/`, `.tmp/`, `.codex/`, `.claude/`, common cache dirs) so round-local runtime artifacts do not affect commit/change detection; excludes apply only to paths the source repository does not already track, while `.deepreview/` remains reserved for deepreview artifacts only, and known nested runtime caches such as `.tmp/go-build-cache/` remain blocked unless the source repository already owns that exact subtree.
- all Codex prompt executions must receive writable run-scoped temp/cache defaults for tool execution, including Go cache/temp envs (`TMPDIR`, `GOCACHE`, `GOMODCACHE`, `GOTMPDIR`), rooted under the run's log/runtime directory rather than the repo worktree, so verification commands do not fall back to host-local caches outside the sandbox.
- round progression is determined by repository changes produced in execute stage.
- if an execute round produces repository changes, deepreview must run at least one additional review round.
- if the last allowed execute round produces repository changes, deepreview must schedule one automatic final audit round with the same review strictness and no repository edits.
- automatic final audit rounds must remain read-only: after execute artifact extraction, the execute worktree must be clean, the candidate branch HEAD must be identical before and after the round, and deepreview must not auto-commit audit-round changes.
- automatic final audit rounds must end with round status `stop` before delivery can proceed.
- if an execute round produces no repository changes, deepreview stops the round loop.
- local commits are encouraged throughout rounds; pushes remain forbidden until final delivery.
- deepreview must not push during intermediate rounds; only one final push is allowed per full run.
- final delivery push is allowed only after round execution completes and no blocking verification failures are reported.
- PR mode has exactly four terminal outcomes: success with complete PR, success with incomplete draft PR, success with no deliverable repository changes (no push/PR), or failure.
- before delivery, deepreview must run repository quality gates and block delivery on failures:
  - run `pre-commit run --all-files` when `.pre-commit-config.yaml` exists
  - run `./setup_env.sh` when `setup_env.sh` exists
  - run both gates in an isolated detached worktree created at the candidate branch HEAD to match the exact content being delivered
- default delivery mode is `pr` and must not push source branch directly.
- in `pr` mode, run a bounded pre-delivery privacy remediation loop (up to 3 Codex-guided attempts) in a candidate-branch worktree so changed-file scans and auto-remediation inspect the exact candidate content rather than the managed repo's default checked-out branch.
- in `pr` mode, privacy remediation attempts may stop early when Codex reports `stop`; otherwise proceed automatically after the configured max attempts.
- in `pr` mode, privacy remediation is a fix gate (attempted remediation + scan feedback), not a hard terminal blocker after max attempts.
- in `pr` mode, deepreview creates the PR, then runs one fresh codex prompt to generate a clear final PR title + description body and updates both via `gh pr edit`.
- in `pr` mode, if the run exits before normal completion after producing deliverable repository changes, deepreview must still publish a draft PR to preserve the candidate branch state.
- incomplete draft PR titles must start with `[INCOMPLETE] ` before the normal `deepreview:` title.
- incomplete draft PR bodies must explicitly state that the PR is incomplete, why delivery did not finish cleanly, and what remains to be done before merge.
- `yolo` mode is optional opt-in for direct push to source branch.
- when `yolo` targets the default branch, deepreview runs a push-permission dry-run preflight before round execution.
- managed repo checkout is replaced with a fresh clone each run to avoid stale state.
- managed repo checkout paths are branch-scoped under the workspace so fresh-clone setup for one source branch cannot race another source branch of the same repo.
- Codex auth should rely on local Codex CLI session/subscription, not repository-stored API keys.
- all Codex prompt executions (new and resumed threads, including post-delivery prompts) must use `--model gpt-5.4` and `model_reasoning_effort="high"`.

## Runtime contract
- command entrypoint: `deepreview`
- primary command: `deepreview review`
- helper command: `deepreview doctor`
- helper command: `deepreview dry-run`
- minimum inputs:
  - none when running inside a valid local GitHub repo context
  - otherwise provide enough explicit context (`<repo>` and/or `--source-branch`) to resolve target repo + source branch
- optional inference override:
  - `DEEPREVIEW_CALLER_CWD` can be set by launch wrappers to preserve caller repo inference when the wrapper changes directories before invoking deepreview.
- core options:
  - `--concurrency <n>` default `4`
  - `--max-rounds <n>` default `5`
  - `--mode <pr|yolo>` default `pr` (case-insensitive value parsing)
  - `--yolo` alias for `--mode yolo` (legacy `--YOLO` accepted)
  - full-screen terminal UI is enabled by default when terminal capabilities are valid
  - when TUI is enabled, deepreview exits the UI automatically on completion and prints the text summary immediately
  - before printing the completion summary after a TUI run, deepreview clears the terminal and prints summary text from the top-left cursor position
  - `--no-tui` force structured text progress logs
- worker-activity resilience env knobs (applies to all Codex workers):
  - `DEEPREVIEW_REVIEW_INACTIVITY_SECONDS` default `300` (5 minutes; `0` disables inactivity restarts)
  - `DEEPREVIEW_REVIEW_ACTIVITY_POLL_SECONDS` default `15`
  - `DEEPREVIEW_REVIEW_MAX_RESTARTS` default `1`

Helper command behavior:
- `doctor` runs non-mutating preflight checks for local tools, auth state, prompt assets, and remote source-branch reachability.
- `dry-run` prints resolved run context and stage order without running Codex or mutating git state.

## Round artifact contract
- Authoritative round record path: `~/deepreview/runs/<run-id>/round-<round>/round.json`
- Required round-record schema:
  - `round`: positive integer
  - `summary`: non-empty string naming the round summary artifact
  - `status`: valid round-status object
- A round counts as completed for final reporting only when `round.json` exists and parses successfully.
- Invalid or missing `round.json` means the round did not complete successfully for reporting purposes.
- Incomplete-draft recovery and final reporting must derive completed-round counts and latest-decision claims only from valid `round.json` records; if no valid round records exist, report zero completed rounds and omit latest-decision claims.

## Round status artifact contract
- Canonical status file path: `~/deepreview/runs/<run-id>/round-<round>/round-status.json`
- Required schema:
  - `decision`: enum `continue|stop`
  - `reason`: non-empty string
- Optional fields:
  - `confidence`: number in `[0.0, 1.0]`
  - `next_focus`: string
- This file is an execute-stage artifact for traceability; round-loop control is change-driven, not decision-driven.
- Invalid or missing required fields fail the round.

## Delivery naming contract
- delivery branch prefix: `deepreview/`
- local candidate branch prefix: `deepreview/candidate/`
- PR title prefix: `deepreview:`
- final PR title should be concise, human-readable, and explain what changed at a glance.

## Artifact contract
Each run must produce:
- run metadata and final summary
- per-round review/execute logs while active
- `review-<worker-id>.md` independent review outputs for each active round
- per-round execute outputs (triage decisions, change plan, verification report, round summary, round status flag, authoritative round record)
- delivery outcome metadata
- per-round local commits for changed work (one or more commits allowed; no empty commits)

Cleanup policy:
- aggressively remove review/execute worktrees and transient round artifacts once they are no longer needed.
- keep only minimal artifacts required for final summary and diagnostics.

## Safety contract
- never commit tokens, credentials, or private keys.
- never emit personal information in public delivery surfaces (PR title/body, commit messages, delivery summaries, comments, or committed code/docs).
- treat committed docs/artifacts as potentially public.
- in PR mode, run privacy-hygiene scans and Codex remediation attempts before final delivery actions, including changed-file scans and delivery commit-message scans.
- keep local terminal progress/error output literal for operator debugging; privacy redaction is enforced at delivery/public surfaces.
- fail fast on verification failures.

## Failure-handling contract
- if any independent-review worker does not complete successfully after bounded inactivity restarts, fail the run.
- deepreview does not continue with partial independent-review coverage; all configured workers must succeed.
- if execute verification fails, fail the run and do not deliver.
- if `pr` mode delivery fails after final round succeeds, fail the run and do not perform fallback pushes.
- in `yolo` mode, do not push when verification fails.
- in PR mode, deepreview should run at most 3 bounded privacy remediation attempts before delivery; each attempt can apply built-in local-path doc sanitization and/or Codex-guided fixes, then re-scan.
- in PR mode, after bounded privacy attempts complete, delivery proceeds by policy (privacy findings no longer hard-block delivery).
- if an automatic final audit round reports `continue` or produces repository changes, `pr` mode should publish an incomplete draft PR when deliverable repository changes exist; `yolo` mode still fails with guidance to rerun deepreview using a higher `--max-rounds`.
- verification strategy is codex-led: codex should attempt repo tests, pre-commit checks, and locally runnable CI-like checks when available, then report what ran and outcomes.

## PR body contract (default PR mode)
PR bodies should include these sections in the final Codex-generated output:
- `## summary`
- `## what changed and why`
- `## round outcomes`
- `## verification`
- `## risks and follow-ups`
- `## final status`
- incomplete draft PR bodies must also include an explicit warning not to merge yet plus the blocking reason and latest round state
- do not embed individual independent-review reports or full execute artifact dumps in PR description
- final PR body text must pass privacy checks (no personal information, secrets, or private local machine paths)
- if generated PR text exceeds GitHub PR body limits, deepreview must fall back to a compact body automatically

## PR title contract (default PR mode)
- final PR title is Codex-generated in post-delivery stage and then applied via `gh pr edit`.
- final PR title must remain prefixed with `deepreview:`.
- final PR title must be concise, concrete, and human-readable (not generic boilerplate).
- incomplete draft PR titles must be prefixed with `[INCOMPLETE] ` ahead of the normal `deepreview:` prefix.
- final PR title text must pass privacy checks (no personal information, secrets, or private local machine paths).

## Prompt-template contract
- Prompt templates are file-based and unversioned.
- Prompt root directory is `prompts/`.
- Independent review stage uses one shared template: `prompts/review/independent-review.md`.
- Execute stage uses an ordered queue listed in `prompts/execute/queue.txt`.
- PR mode uses one pre-delivery privacy remediation template: `prompts/delivery/privacy-fix.md`.
- PR mode uses one post-delivery description-enhancement template: `prompts/delivery/pr-description-summary.md`.
- Post-delivery PR enhancement prompt should provide path-level context and let Codex inspect run artifacts/logs/repo directly; avoid injecting pre-digested round/file summary blocks.
- Default execute queue order:
  - `01-consolidate-plan.md`
  - `02-execute-verify.md`
  - `03-cleanup-summary-commit.md`
- Execute queue prompts must run sequentially in one Codex chat context for the round.
- Execute prompts must receive injected independent review content in prompt context (not only file paths).
- Prompt rendering must support deterministic template variables (for example `{{ROUND_NUMBER}}`) for repo/branch metadata, worktree paths, round metadata, artifact paths, and commit message templates.
- Any unresolved template variable at render time fails the run immediately.

## Codex autonomy contract
- codex is the primary reasoning engine for review/execute decisions.
- codex is allowed to inspect git history and recent commits/PR context when useful.
- deepreview should avoid over-hardcoding repo-specific heuristics.

## Related docs
- pipeline and stage flow details: [architecture.md](architecture.md)
- execution and notes routing conventions: [workflows.md](workflows.md)
- durable rationale and policy decisions: [decisions.md](decisions.md)
- requirement traceability baseline: [alignment.md](alignment.md)
- prompt templates and queue layout: [../prompts/README.md](../prompts/README.md)
