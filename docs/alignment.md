# deepreview alignment baseline

This document maps the user-provided project description to canonical requirements and anchors them to durable docs.

## Usage
- Use requirement IDs (`R-01`, `R-02`, ...) in plan updates, implementation PRs, run summaries, and verification reports.
- Do not close a phase as complete unless affected requirements have implementation and verification evidence.
- Keep unresolved or in-progress evidence tracking in `plan/current/alignment-status.md`.

## Canonical requirements from description

| id | requirement | canonical durable anchors |
| --- | --- | --- |
| R-01 | Project/tool name is always `deepreview` (lowercase, one word). | `docs/spec.md` (required invariants), `docs/decisions.md` |
| R-02 | Treat project as open-source-ready; never commit secrets/sensitive/confidential/auth data. | `docs/spec.md` (required invariants + safety), `docs/decisions.md` |
| R-03 | Primary interface is CLI command `deepreview`. | `docs/spec.md` (runtime contract) |
| R-04 | Work only in deepreview-managed workspace (`~/deepreview`), never mutate user checkout. | `docs/spec.md`, `docs/architecture.md`, `docs/decisions.md` |
| R-05 | Review target is a specified remote source branch compared relative to default branch. | `docs/spec.md`, `docs/architecture.md` |
| R-06 | Use latest remote source-branch state for round-1 baseline, then use latest local candidate branch state for subsequent rounds. | `docs/spec.md`, `docs/architecture.md` |
| R-07 | Independent review concurrency is configurable; default is 4. | `docs/spec.md`, `docs/architecture.md` |
| R-08 | Independent reviews run independently in separate worktrees (one worktree per review). | `docs/spec.md`, `docs/architecture.md` |
| R-09 | Run Codex independent reviews concurrently, one run per worktree. | `docs/spec.md`, `docs/architecture.md` |
| R-10 | Each successful independent-review worker must emit one markdown review report. | `docs/spec.md` (artifact contract), `docs/architecture.md` |
| R-11 | Independent review requires full worker coverage; deepreview monitors worker activity and performs bounded inactivity restarts before failing the stage, then cleans up independent-review worktrees. | `docs/spec.md`, `docs/architecture.md` |
| R-12 | Each execute pass uses a fresh worktree from current candidate head. | `docs/spec.md`, `docs/architecture.md` |
| R-13 | Execute ingests independent review reports, applies selected changes, and verifies heavily. | `docs/spec.md`, `docs/architecture.md` |
| R-14 | Default delivery opens PR from a new branch into the original source branch. | `docs/spec.md`, `docs/architecture.md`, `docs/decisions.md` |
| R-15 | Optional `yolo` mode may commit/push directly to original source branch. | `docs/spec.md`, `docs/architecture.md` |
| R-16 | Use local Codex CLI session/subscription; do not require repository-stored API keys for core flow. | `docs/spec.md`, `docs/architecture.md`, `docs/decisions.md` |
| R-17 | Reuse compatible patterns internally but do not reference external inspiration projects in committed deepreview artifacts. | `docs/spec.md`, `AGENTS.md`, `docs/decisions.md` |
| R-18 | Keep orchestration simple: avoid unbounded retries; allow only bounded inactivity restarts with explicit caps. | `docs/spec.md`, `docs/architecture.md`, `docs/decisions.md` |
| R-19 | Run iterative deepreview loops with configurable max rounds (default 5). | `docs/spec.md`, `docs/architecture.md` |
| R-20 | Codex is trusted as the main judge and may stop early via round status flag file. | `docs/spec.md`, `docs/architecture.md`, `docs/decisions.md` |
| R-21 | Do not push in intermediate rounds; perform one push only at final delivery step. | `docs/spec.md`, `docs/architecture.md`, `docs/decisions.md` |
| R-22 | Each round must use fresh isolated worktrees to minimize stale context carryover. | `docs/spec.md`, `docs/architecture.md` |
| R-23 | Aggressive cleanup: remove obsolete worktrees and transient artifacts as soon as they are no longer needed. | `docs/spec.md`, `docs/architecture.md`, `docs/decisions.md` |
| R-24 | Codex may inspect repo history, recent commits, and PR context if useful. | `docs/spec.md`, `docs/architecture.md` |
| R-25 | Mode interface supports `--mode yolo` and `--yolo` alias; default remains PR mode. | `docs/spec.md` |
| R-26 | Delivery naming conventions use `deepreview/` branch prefix and `deepreview:` PR title prefix. | `docs/spec.md`, `docs/decisions.md` |
| R-27 | If an execute round produces no changes, stop by default unless codex explicitly requests continue. | `docs/spec.md`, `docs/architecture.md`, `docs/decisions.md` |
| R-28 | Encourage local commits throughout execution; ensure changed round work is committed locally (no empty commits) before round completion. | `docs/spec.md`, `docs/architecture.md`, `docs/decisions.md` |
| R-29 | Local candidate branch naming uses `deepreview/candidate/<source-branch>/<run-id>`. | `docs/spec.md`, `docs/architecture.md`, `docs/decisions.md` |
| R-30 | Default PR body includes sections: round summary, key fixes, verification evidence, residual risks. | `docs/spec.md`, `docs/decisions.md` |
| R-31 | Verification execution is codex-led; codex should run available tests/pre-commit/local CI checks and report outcomes. | `docs/spec.md`, `docs/architecture.md`, `docs/decisions.md` |
| R-32 | Round stop-flag file uses a strict schema with enum decision values and deterministic run-artifact path. | `docs/spec.md`, `docs/architecture.md`, `docs/decisions.md` |
| R-33 | Prompt templates are file-based and unversioned. | `docs/spec.md`, `docs/decisions.md` |
| R-34 | Independent review stage uses one shared prompt template for all independent reviewers. | `docs/spec.md`, `docs/architecture.md`, `prompts/review/independent-review.md` |
| R-35 | Execute stage runs an ordered prompt queue sequentially in one Codex chat context, with independent review content injected into prompt context. | `docs/spec.md`, `docs/architecture.md`, `prompts/execute/queue.txt`, `prompts/execute/01-consolidate-reviews.md`, `prompts/execute/02-plan.md`, `prompts/execute/03-execute-verify.md`, `prompts/execute/04-cleanup-summary-commit.md` |
| R-36 | Consolidate stage treats independent reviews as inputs (not gospel), performs independent validation, and only advances high-conviction accepted items. | `docs/spec.md`, `docs/architecture.md`, `docs/decisions.md`, `prompts/execute/01-consolidate-reviews.md` |
| R-37 | Plan stage must produce an end-to-end executable plan, defer low-confidence items, and include explicit docs/decision updates and verification matrix. | `docs/spec.md`, `prompts/execute/02-plan.md`, `prompts/README.md` |
| R-38 | Execute/verify stage must run end-to-end with minimum local verification gates (tests, pre-commit, CI-like checks where available) plus evidence capture. | `docs/spec.md`, `docs/architecture.md`, `prompts/execute/03-execute-verify.md` |
| R-39 | Cleanup/summary/commit stage must update docs/notes/decision rationale, write strict round outputs, and ensure local commits without pushing. | `docs/spec.md`, `docs/architecture.md`, `prompts/execute/04-cleanup-summary-commit.md` |
| R-40 | `README.md` is the user-facing guide and should focus on project purpose, setup, usage, and operational ergonomics rather than internal posture details. | `README.md`, `docs/decisions.md`, `docs/workflows.md` |
| R-41 | `README.md` should include optional shell alias guidance for frequent CLI usage. | `README.md`, `docs/decisions.md`, `docs/workflows.md` |
| R-42 | `<repo>` and `--source-branch` are optional when current directory is a valid local GitHub repo; deepreview infers missing values from local context. | `README.md`, `docs/spec.md`, `docs/architecture.md`, `internal/deepreview/cli.go`, `internal/deepreview/local_context.go` |
| R-43 | When source branch is inferred from local context, deepreview requires no tracked local changes and exact local/upstream sync before run start. | `README.md`, `docs/spec.md`, `docs/architecture.md`, `internal/deepreview/local_context.go`, `internal/deepreview/local_context_test.go` |
| R-44 | Managed repo clone in workspace is replaced with a fresh clone each run to avoid stale state/worktree leftovers. | `docs/spec.md`, `docs/architecture.md`, `internal/deepreview/gitops.go`, `internal/deepreview/gitops_test.go` |
| R-45 | In `yolo` mode targeting default branch, deepreview must preflight push permission using dry-run and fail fast if disallowed. | `docs/spec.md`, `docs/architecture.md`, `internal/deepreview/orchestrator.go` |
| R-46 | YOLO mode ergonomics should accept `--mode YOLO` and legacy `--YOLO` while preserving default PR mode behavior. | `docs/spec.md`, `README.md`, `internal/deepreview/cli.go`, `internal/deepreview/cli_test.go` |
| R-47 | Execute prompt wiring should use review-stage terminology (`REVIEW_*` placeholders) while retaining compatibility for legacy placeholder names. | `prompts/execute/01-consolidate-reviews.md`, `internal/deepreview/orchestrator.go`, `docs/decisions.md` |
| R-48 | Independent review and execute consolidation remain strict: only high-confidence `critical|high` merge-relevant accepted items are in scope for implementation in this workflow. | `prompts/review/independent-review.md`, `prompts/execute/01-consolidate-reviews.md`, `docs/spec.md`, `docs/decisions.md`, `prompts/README.md` |
| R-49 | In PR mode, deepreview must run a post-delivery fresh Codex prompt that generates a comprehensive final PR title + description body (including round outcomes and verification highlights) and updates both via PR edit. | `internal/deepreview/orchestrator.go`, `prompts/delivery/pr-description-summary.md`, `docs/spec.md`, `docs/architecture.md`, `docs/decisions.md` |
| R-50 | All Codex prompt calls (new/resume, including post-delivery PR enhancement) must be hard-pinned to `gpt-5.3-codex` with `model_reasoning_effort=xhigh`. | `internal/deepreview/codex.go`, `internal/deepreview/orchestrator.go`, `internal/deepreview/cli.go`, `docs/spec.md`, `docs/decisions.md` |
| R-51 | Deepreview must provide non-mutating helper commands: `doctor` for preflight checks and `dry-run` for ordered execution preview. | `internal/deepreview/cli.go`, `internal/deepreview/cli_test.go`, `README.md`, `docs/spec.md`, `docs/architecture.md`, `docs/decisions.md` |
| R-52 | Privacy boundary is split by surface: public/delivery surfaces must be redacted/guarded, while local terminal output remains literal for operator debugging. | `docs/spec.md`, `docs/architecture.md`, `docs/decisions.md`, `internal/deepreview/orchestrator.go`, `internal/deepreview/cli.go`, `internal/deepreview/progress.go`, `internal/deepreview/text_reporter.go` |
| R-53 | In TUI mode, completion must not block on keypress; deepreview exits UI automatically, clears terminal, and then prints text completion summary. | `internal/deepreview/tui.go`, `internal/deepreview/cli.go`, `internal/deepreview/tui_test.go`, `internal/deepreview/cli_test.go`, `docs/spec.md`, `docs/architecture.md`, `docs/decisions.md`, `README.md` |
| R-54 | Delivery quality gates must execute against a detached worktree snapshot of candidate branch HEAD so pre-delivery checks match the exact deliverable content. | `internal/deepreview/orchestrator.go`, `internal/deepreview/orchestrator_test.go`, `docs/spec.md`, `docs/architecture.md`, `docs/decisions.md`, `README.md` |
| R-55 | Execute triage accepts are policy-gated at runtime: accepted items must be severity `critical|high` and confidence `high`, otherwise the round fails fast. | `internal/deepreview/orchestrator.go`, `internal/deepreview/orchestrator_test.go`, `docs/spec.md`, `docs/decisions.md`, `prompts/execute/01-consolidate-reviews.md` |
| R-56 | Prompt templates for machine-validated artifacts include explicit output schemas and examples to improve formatting reliability. | `prompts/execute/01-consolidate-reviews.md`, `prompts/execute/03-execute-verify.md`, `prompts/execute/04-cleanup-summary-commit.md`, `prompts/review/independent-review.md`, `prompts/README.md`, `docs/decisions.md` |
| R-57 | On user interrupt (`Ctrl+C`), deepreview must terminate active worker commands immediately, then perform cleanup and exit with cancel status. | `internal/deepreview/cli.go`, `internal/deepreview/process.go`, `internal/deepreview/process_unix.go`, `internal/deepreview/process_windows.go`, `internal/deepreview/integration_test.go`, `docs/spec.md`, `docs/decisions.md` |
| R-58 | In PR mode, pre-delivery privacy handling is a bounded Codex-led remediation loop (max 3 attempts) with optional early stop, and delivery proceeds by policy after bounded attempts. | `internal/deepreview/orchestrator.go`, `prompts/delivery/privacy-fix.md`, `internal/deepreview/integration_test.go`, `docs/spec.md`, `docs/architecture.md`, `docs/decisions.md`, `README.md` |

## Alignment gate
For each requirement touched by a change, evidence must be captured for:
- planned: plan item references requirement IDs.
- implemented: code/docs paths or commits that satisfy behavior.
- executed: run evidence (commands/run logs/artifacts).
- verified: tests/checks that validate behavior.

Keep active evidence in `plan/current/alignment-status.md`, and promote stable process rules into `docs/workflows.md` or `docs/decisions.md`.
