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
- no secrets or confidential data may be committed.
- deepreview operates only in managed workspace paths under `~/deepreview`.
- deepreview must not operate in the user's own active checkout.
- if repo/source-branch are omitted, deepreview may infer them from current local GitHub repo context.
- inferred source branch requires local readiness checks: no tracked local changes and exact local/upstream synchronization.
- v1 keeps orchestration simple: no automatic retry/backoff/self-healing loops for failed stages.
- codex prompt executions use a fixed timeout of 3600 seconds per prompt.
- round loop runs up to `--max-rounds` (default `5`) and may stop early.
- independent reviews run in independent worktrees.
- independent review concurrency defaults to `4` and is configurable.
- each independent-review worker must emit one markdown review report.
- independent-review reports prioritize critical/high issues first; they may include a small optional section of obvious non-blocking improvements only when high-confidence, low-risk, and non-behavior-changing.
- independent review completion waits for all workers in that round.
- each execute pass runs in a fresh worktree.
- independent-review workers use one shared independent-review prompt template in v1.
- each execute pass runs an ordered multi-prompt queue in one Codex chat context.
- execute prompt-1 (consolidate reviews) treats independent reviews as inputs, not gospel, and only accepts high-conviction items after independent validation.
- execute prompt-2 (plan) must produce an end-to-end, execution-ready plan and defer low-confidence items.
- execute prompt-3 (execute/verify) must run end-to-end implementation plus minimum local verification gates (tests, pre-commit checks, locally runnable CI-like checks when available), with evidence output.
- execute prompt-4 (cleanup/summary/commit) must include docs/notes/decision upkeep and ensure changed work is committed locally.
- round progression is determined by repository changes produced in execute stage.
- if an execute round produces repository changes, deepreview must run at least one additional review round (subject to `--max-rounds`).
- if an execute round produces no repository changes, deepreview stops the round loop.
- local commits are encouraged throughout rounds; pushes remain forbidden until final delivery.
- deepreview must not push during intermediate rounds; only one final push is allowed per full run.
- final delivery push is allowed only after round execution completes and no blocking verification failures are reported.
- default delivery mode is `pr` and must not push source branch directly.
- in `pr` mode, deepreview creates the PR with deterministic artifact-backed body content, then runs one fresh codex prompt to generate a top summary and updates the PR description by prepending that summary.
- `yolo` mode is optional opt-in for direct push to source branch.
- when `yolo` targets the default branch, deepreview runs a push-permission dry-run preflight before round execution.
- managed repo checkout is replaced with a fresh clone each run to avoid stale state.
- Codex auth should rely on local Codex CLI session/subscription, not repository-stored API keys.
- all Codex prompt executions (new and resumed threads, including post-delivery prompts) must use `--model gpt-5.3-codex` and `model_reasoning_effort="xhigh"`.

## Runtime contract
- command entrypoint: `deepreview`
- primary command: `deepreview review`
- minimum inputs:
  - none when running inside a valid local GitHub repo context
  - otherwise provide enough explicit context (`<repo>` and/or `--source-branch`) to resolve target repo + source branch
- core options:
  - `--concurrency <n>` default `4`
  - `--max-rounds <n>` default `5`
  - `--mode <pr|yolo>` default `pr` (case-insensitive value parsing)
  - `--yolo` alias for `--mode yolo` (legacy `--YOLO` accepted)
  - `--tui` enable full-screen terminal UI (opt-in)
  - `--no-tui` force structured text progress logs

## Round status artifact contract
- Status file path: `~/deepreview/runs/<run-id>/round-<round>/round-status.json`
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

## Artifact contract
Each run must produce:
- run metadata and final summary
- per-round review/execute logs while active
- `review-<worker-id>.md` independent review outputs for each active round
- per-round execute outputs (triage decisions, change plan, verification report, round summary, round status flag)
- delivery outcome metadata
- per-round local commits for changed work (one or more commits allowed; no empty commits)

Cleanup policy:
- aggressively remove review/execute worktrees and transient round artifacts once they are no longer needed.
- keep only minimal artifacts required for final summary and diagnostics.

## Safety contract
- never commit tokens, credentials, or private keys.
- treat committed docs/artifacts as potentially public.
- run secret-hygiene checks before final delivery actions.
- fail fast on verification failures.

## Failure-handling contract
- if any independent-review worker fails or required report is missing in a round, fail the run (no automatic retries).
- if execute verification fails, fail the run and do not deliver.
- if `pr` mode delivery fails after final round succeeds, emit remediation guidance and do not perform fallback pushes.
- in `yolo` mode, do not push when verification fails.
- verification strategy is codex-led: codex should attempt repo tests, pre-commit checks, and locally runnable CI-like checks when available, then report what ran and outcomes.

## PR body contract (default PR mode)
PR bodies should include these sections:
- codex-generated top summary (high-level narrative of what happened, why it mattered, and final status)
- round summary
- key fixes
- verification evidence
- residual risks
- detailed per-round review and execute artifacts

## Prompt-template contract
- Prompt templates are file-based and unversioned in v1.
- Prompt root directory is `prompts/`.
- Independent review stage uses one shared template: `prompts/review/independent-review.md`.
- Execute stage uses an ordered queue listed in `prompts/execute/queue.txt`.
- PR mode uses one post-delivery description-enhancement template: `prompts/delivery/pr-description-summary.md`.
- Default execute queue order:
  - `01-consolidate-reviews.md`
  - `02-plan.md`
  - `03-execute-verify.md`
  - `04-cleanup-summary-commit.md`
- Execute queue prompts must run sequentially in one Codex chat context for the round.
- Execute prompts must receive injected independent review content in prompt context (not only file paths).
- Prompt rendering must support deterministic template variables (for example `{{ROUND_NUMBER}}`) for repo/branch metadata, worktree paths, round metadata, artifact paths, and commit message templates.
- Any unresolved template variable at render time fails the run immediately.

## Codex autonomy contract
- codex is the primary reasoning engine for review/execute decisions.
- codex is allowed to inspect git history and recent commits/PR context when useful.
- deepreview should avoid over-hardcoding repo-specific heuristics in v1.

## Related docs
- pipeline and stage flow details: [architecture.md](architecture.md)
- execution and notes routing conventions: [workflows.md](workflows.md)
- durable rationale and policy decisions: [decisions.md](decisions.md)
- requirement traceability baseline: [alignment.md](alignment.md)
- prompt templates and queue layout: [../prompts/README.md](../prompts/README.md)
