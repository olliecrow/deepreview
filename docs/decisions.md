# Decision Capture Policy

This file is the current-state decision log for deepreview.

It is not a changelog and it is not meant to preserve every implementation-era tweak forever. When a decision is superseded, redundant, or fully encoded elsewhere, remove it from this file and rely on the smaller durable source:
- `docs/spec.md` for current behavior contracts
- `docs/architecture.md` for pipeline shape and stage boundaries
- tests/code comments for enforcement details
- git history for the step-by-step change narrative

## When to record
- Any fix for a confirmed bug, regression, or safety issue.
- Any deliberate behavior choice that differs from intuitive defaults.
- Any trade-off decision that affects modeling or behavior.
- Any change that affects external behavior, invariants, or public APIs.

## Where to record
Use the smallest, most local place that makes the decision obvious:
- Code comments near non-obvious behavior.
- Tests whose names/assertions encode the invariant.
- Docs when the decision is cross-cutting.

Prefer updating an existing note over creating a new file.

## What to record
- Decision: what was chosen.
- Context: what problem or risk it addresses.
- Rationale: why this choice was made.
- Trade-offs: what is intentionally not being done.
- Enforcement: which tests or code paths lock it in.
- References: optional file paths or docs.

## Template
```text
Decision:
Context:
Rationale:
Trade-offs:
Enforcement:
References:
```

## Active decisions

Decision:
Use `deepreview` as the canonical project and CLI spelling everywhere.
Context:
User input may contain variants or dictation noise, but stable output is required across docs, CLI text, prompts, and public artifacts.
Rationale:
Consistent naming prevents drift and avoids leaking legacy spellings into committed output.
Trade-offs:
CLI parsing and prompt rendering need to normalize operator input.
Enforcement:
Canonical naming is documented in `AGENTS.md` and `docs/spec.md`, and tests cover normalized CLI behavior.
References:
`AGENTS.md`, `docs/spec.md`, `README.md`, `internal/deepreview/cli_test.go`

Decision:
Treat the repository as open-source-ready and block sensitive material on public-facing surfaces.
Context:
deepreview is intended to become open source, and it generates delivery text that may leave the local machine.
Rationale:
Applying public-ready hygiene early reduces later cleanup risk and keeps runtime behavior aligned with eventual publication.
Trade-offs:
Privacy and secret checks can reject content that would be acceptable in a purely local tool.
Enforcement:
Public-surface sanitization, changed-file/commit-message scans, and security hooks enforce this boundary.
References:
`docs/spec.md`, `docs/project-preferences.md`, `internal/deepreview/orchestrator.go`, `internal/deepreview/privacy_test.go`, `.pre-commit-config.yaml`

Decision:
Run all review and execute work only inside deepreview-managed workspace paths under `~/deepreview`.
Context:
The operator's active checkout must never be mutated or blocked by deepreview execution.
Rationale:
Managed clone/worktree isolation is the core safety boundary for the tool.
Trade-offs:
Fresh clones and isolated worktrees cost extra disk and setup time.
Enforcement:
The orchestrator clones into the managed workspace, creates worktrees there, and never mutates the caller checkout.
References:
`docs/spec.md`, `docs/architecture.md`, `internal/deepreview/orchestrator.go`, `internal/deepreview/gitops.go`

Decision:
Keep orchestration simple and fail-fast, with one narrow self-healing exception: bounded inactivity restarts for stalled Codex workers.
Context:
deepreview is a local operator tool, not a distributed control plane.
Rationale:
Straightforward orchestration is easier to reason about and safer to maintain. Inactivity restarts are worth the limited complexity because a single stalled worker would otherwise deadlock the run.
Trade-offs:
Some transient failures still require a manual rerun, and watchdog logic adds targeted complexity.
Enforcement:
The orchestrator uses explicit restart caps and fails the run outside that bounded policy.
References:
`docs/spec.md`, `docs/architecture.md`, `internal/deepreview/orchestrator.go`, `internal/deepreview/orchestrator_test.go`, `internal/deepreview/integration_test.go`

Decision:
Use concurrent independent reviews with full-worker completion before execute can proceed.
Context:
The value of deepreview comes from independent perspectives, not a single review pass.
Rationale:
Requiring full successful worker coverage keeps the execute stage grounded in the complete configured review set.
Trade-offs:
Wall-clock time and local compute usage increase with concurrency, and one failing worker fails the stage.
Enforcement:
Review-stage orchestration requires `concurrency` successful review reports and fails on partial coverage.
References:
`docs/spec.md`, `docs/architecture.md`, `internal/deepreview/orchestrator.go`, `internal/deepreview/integration_test.go`

Decision:
Run each execute round in a fresh worktree from the current candidate branch head, and preserve continuity only within that round's prompt queue.
Context:
deepreview needs iterative local progress across rounds without carrying stale mutable state or stale chat history between rounds.
Rationale:
Fresh worktrees and fresh stage contexts keep each round isolated while still letting the candidate branch accumulate reviewed improvements.
Trade-offs:
Prompt retries and stage boundaries need explicit artifact preservation rules.
Enforcement:
The orchestrator resets mutable retries to a clean baseline and only preserves outputs from successfully completed earlier prompts.
References:
`docs/spec.md`, `docs/architecture.md`, `internal/deepreview/orchestrator.go`, `internal/deepreview/orchestrator_test.go`

Decision:
Default delivery mode is `pr`; direct source-branch publication requires explicit `yolo` mode.
Context:
The safe default should preserve the source branch while still allowing an explicit fast path.
Rationale:
PR-first delivery provides reviewable output and a safer publication boundary.
Trade-offs:
PR mode is slower and depends on GitHub-backed repository identity and local `gh` auth.
Enforcement:
CLI defaults to `pr`, PR delivery rejects filesystem-backed repo identities, and yolo is opt-in only.
References:
`docs/spec.md`, `docs/architecture.md`, `README.md`, `internal/deepreview/cli.go`, `internal/deepreview/orchestrator.go`

Decision:
In `pr` mode, accept only GitHub-backed repository identities.
Context:
PR creation and mergeability validation depend on GitHub semantics.
Rationale:
Rejecting unsupported identities early is simpler and safer than partial PR-mode behavior for filesystem remotes.
Trade-offs:
Local filesystem remotes can only use `yolo` mode.
Enforcement:
Repository identity resolution distinguishes GitHub vs filesystem sources, and orchestrator construction rejects filesystem identities in PR mode.
References:
`docs/spec.md`, `internal/deepreview/orchestrator.go`, `internal/deepreview/orchestrator_test.go`

Decision:
Use the operator's normal local Codex authentication, configuration, and inherited environment by default.
Context:
deepreview should behave like local Codex usage rather than imposing a separate runtime layer.
Rationale:
This keeps prompt behavior aligned with direct operator workflows and avoids deepreview-specific auth or environment assumptions.
Trade-offs:
Prompt behavior depends more directly on the operator's local environment and launcher setup.
Enforcement:
Codex subprocesses inherit the normal environment, and deepreview does not require repository-stored API keys.
References:
`docs/spec.md`, `docs/architecture.md`, `README.md`, `internal/deepreview/codex.go`, `internal/deepreview/codex_test.go`

Decision:
Prefer `multicodex` when available, but pin resumed multicodex-backed contexts to the profile that created the thread.
Context:
Fresh prompt families benefit from dynamic launcher selection, while resumed threads need profile continuity.
Rationale:
Per-thread pinning preserves continuity without sacrificing fresh-context balancing across the rest of the run.
Trade-offs:
Resumed threads cannot migrate to a different profile mid-thread, and deepreview depends on multicodex exposing selected-profile metadata.
Enforcement:
Launcher resolution prefers `multicodex`, resume contexts carry `MulticodexProfile`, and execute-stage resumes fail fast if resumable multicodex metadata is missing.
References:
`docs/spec.md`, `docs/architecture.md`, `README.md`, `internal/deepreview/codex.go`, `internal/deepreview/codex_test.go`, `internal/deepreview/orchestrator.go`

Decision:
Treat review findings as inputs, not gospel, and only act on high-confidence material improvements.
Context:
Independent reviewers can be useful and still be wrong or over-broad.
Rationale:
deepreview should prefer a small number of no-regret changes over noisy suggestion intake.
Trade-offs:
Some real but low-confidence opportunities are intentionally deferred.
Enforcement:
Review and execute prompts require item-by-item validation, triage policy rejects non-material/non-high-confidence accepts, and execute remains scoped to accepted items only.
References:
`docs/spec.md`, `prompts/review/independent-review.md`, `prompts/execute/01-triage-plan.md`, `prompts/execute/02-implement-verify-finalize.md`, `internal/deepreview/orchestrator.go`

Decision:
Bias accepted work toward simplification, deletion, and scope reduction when those are the cleanest fixes.
Context:
LLM-driven maintenance tends to drift toward additive fixes unless the tool explicitly counters that bias.
Rationale:
deepreview should reduce unnecessary complexity whenever that is the safest high-confidence path.
Trade-offs:
The bar remains high, so not every plausible cleanup belongs in scope.
Enforcement:
Prompts explicitly call out simplification/deletion as first-class options, and execute scope remains material-only.
References:
`prompts/review/independent-review.md`, `prompts/execute/01-triage-plan.md`, `prompts/execute/02-implement-verify-finalize.md`, `prompts/delivery/01-deliver.md`

Decision:
Use iterative round control based on explicit execute decisions: `continue` always continues, first `stop` forces a confirmation round, second consecutive `stop` ends the loop even if that round changed code.
Context:
deepreview needs bounded iteration without overfitting to raw file-change signals alone.
Rationale:
The explicit confirmation-round rule keeps the loop simple and predictable while still giving Codex a final verification pass before delivery.
Trade-offs:
Runs can still stop after a confirming round that changed code if Codex judged the result ready.
Enforcement:
Round-loop control is centralized in orchestrator logic and documented as the canonical policy in spec and architecture docs.
References:
`docs/spec.md`, `docs/architecture.md`, `internal/deepreview/orchestrator.go`, `internal/deepreview/orchestrator_test.go`

Decision:
When source branch equals default branch, treat review as a current-state repository audit rather than a zero-diff no-op.
Context:
Reviewing the default branch is still useful even when branch diff is empty.
Rationale:
This preserves review value for self-audits and maintenance passes.
Trade-offs:
Default-branch runs can be broader and longer than ordinary branch-diff reviews.
Enforcement:
Review prompt rendering injects self-audit guidance, and tests cover this mode.
References:
`docs/spec.md`, `docs/architecture.md`, `prompts/review/independent-review.md`, `internal/deepreview/orchestrator.go`, `internal/deepreview/integration_test.go`

Decision:
Keep delivery read-only for tracked repository content and publish only the reviewed candidate branch.
Context:
Allowing delivery-time tracked edits or alternate publish branches would create a trust gap between what deepreview reviewed and what it shipped.
Rationale:
The published branch must be the reviewed branch.
Trade-offs:
Tracked-content or history blockers must be repaired through the normal candidate-branch path, not patched ad hoc during delivery.
Enforcement:
Delivery prompt forbids tracked-content mutation, orchestrator rejects candidate-branch mutation during delivery, and publication validates the reviewed candidate ref.
References:
`docs/spec.md`, `docs/architecture.md`, `prompts/delivery/01-deliver.md`, `internal/deepreview/orchestrator.go`

Decision:
If the reviewed candidate branch is not publishable, use one bounded candidate-branch recovery cycle before final delivery.
Context:
Some delivery blockers are real publishability issues that still belong on the candidate branch.
Rationale:
Routing repair through the normal reviewed branch keeps trust intact while still allowing one bounded autonomous recovery path.
Trade-offs:
Recovery adds one repair round and one confirmation round, but avoids delivery-time branch divergence.
Enforcement:
The orchestrator runs at most one bounded delivery-recovery cycle and then re-validates publishability against the candidate branch.
References:
`docs/spec.md`, `docs/architecture.md`, `internal/deepreview/orchestrator.go`, `internal/deepreview/integration_test.go`

Decision:
Split privacy policy by surface: public delivery artifacts are sanitized and scanned, local terminal output remains literal.
Context:
Operators need precise local diagnostics, but public-facing artifacts must not leak secrets, personal data, or private paths.
Rationale:
Separate treatment by surface preserves both safety and operability.
Trade-offs:
Local terminal transcripts can still contain private local details and should be treated as local artifacts.
Enforcement:
PR/final-summary text is sanitized and validated; local CLI/TUI reporters preserve literal output.
References:
`docs/spec.md`, `docs/architecture.md`, `internal/deepreview/orchestrator.go`, `internal/deepreview/cli.go`, `internal/deepreview/privacy_test.go`

Decision:
Keep durable docs and scratch notes sharply separated.
Context:
Agents consult both `docs/` and `plan/current/`, but they serve different purposes.
Rationale:
`docs/` should stay current and evergreen; `plan/current/` should stay disposable and be pruned aggressively once it stops matching the active task.
Trade-offs:
This requires explicit cleanup instead of passive accumulation.
Enforcement:
`docs/README.md` and `docs/workflows.md` define the routing boundary, and `plan/` is git-ignored so it stays operational scratch rather than durable history.
References:
`docs/README.md`, `docs/workflows.md`, `.gitignore`

Decision:
Track user-level requirement alignment explicitly.
Context:
This project has many operator-facing contracts and evolving orchestration behavior.
Rationale:
Requirement IDs and evidence states make drift visible and help tie docs, code, and verification together.
Trade-offs:
The alignment layer adds maintenance overhead and should stay concise.
Enforcement:
`docs/alignment.md` defines the requirement set and `docs/workflows.md` routes active evidence tracking into `plan/current/`.
References:
`docs/alignment.md`, `docs/workflows.md`

Decision:
Publish canonical run-health artifacts for every completed run.
Context:
Operators need a compact view of artifact coverage and stderr signal without replaying raw logs.
Rationale:
Run-health artifacts make post-run inspection faster and provide a stable debugging surface.
Trade-offs:
Adds another artifact surface that must stay aligned with the canonical run outputs.
Enforcement:
Completed runs write `run-health.json` and `run-health.md`, and final-summary backfill logic recreates them when needed.
References:
`docs/spec.md`, `internal/deepreview/run_health.go`, `internal/deepreview/orchestrator.go`, `internal/deepreview/cli.go`
