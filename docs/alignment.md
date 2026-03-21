# deepreview alignment baseline

This document maps the user-provided project description to canonical requirements and anchors them to durable docs.

## Usage
- Use requirement IDs (`R-01`, `R-02`, ...) in plan updates, implementation PRs, run summaries, and verification reports.
- Do not close a phase as complete unless affected requirements have implementation and verification evidence.
- Keep unresolved or in-progress evidence tracking in `plan/current/alignment-status.md`.

## Canonical requirements from description

| id | requirement | canonical durable anchors |
| --- | --- | --- |
| R-01 | Project/tool name is always `deepreview` (lowercase, one word). | `docs/spec.md`, `docs/decisions.md` |
| R-02 | Treat project as open-source-ready; never commit secrets/sensitive/confidential/auth data. | `docs/spec.md`, `docs/decisions.md` |
| R-03 | Primary interface is CLI command `deepreview`. | `docs/spec.md` |
| R-04 | Work only in deepreview-managed workspace (`~/deepreview`), never mutate user checkout. | `docs/spec.md`, `docs/architecture.md`, `docs/decisions.md` |
| R-05 | Review target is a specified remote source branch compared relative to default branch. | `docs/spec.md`, `docs/architecture.md` |
| R-06 | Use latest remote source-branch state for round-1 baseline, then use latest local candidate branch state for subsequent rounds. | `docs/spec.md`, `docs/architecture.md` |
| R-07 | Independent review concurrency is configurable; default is 4. | `docs/spec.md`, `docs/architecture.md` |
| R-08 | Independent reviews run independently in separate worktrees and separate fresh contexts. | `docs/spec.md`, `docs/architecture.md` |
| R-09 | Run Codex independent reviews concurrently, one run per worktree. | `docs/spec.md`, `docs/architecture.md` |
| R-10 | Each successful independent-review worker must emit one markdown review report. | `docs/spec.md`, `docs/architecture.md` |
| R-11 | Independent review requires full worker coverage; deepreview monitors worker activity and performs bounded inactivity restarts before failing the stage, then cleans up independent-review worktrees. Mutable execute/delivery retries must reset their git worktrees to a clean baseline before rerun. | `docs/spec.md`, `docs/architecture.md` |
| R-12 | Each execute pass uses a fresh worktree from current candidate head, and mutable retries must rerun from a clean candidate-branch baseline instead of reusing abandoned attempt state. | `docs/spec.md`, `docs/architecture.md` |
| R-13 | Execute ingests independent review reports, applies selected changes, and verifies heavily. | `docs/spec.md`, `docs/architecture.md` |
| R-14 | Default delivery opens PR from a new branch into the original source branch. | `docs/spec.md`, `docs/architecture.md`, `docs/decisions.md` |
| R-15 | Optional `yolo` mode may commit/push directly to original source branch. | `docs/spec.md`, `docs/architecture.md` |
| R-16 | Use local Codex CLI session/subscription; do not require repository-stored API keys for core flow. | `docs/spec.md`, `docs/architecture.md`, `docs/decisions.md` |
| R-17 | Reuse compatible patterns internally but do not reference external inspiration projects in committed deepreview artifacts. | `docs/spec.md`, `AGENTS.md`, `docs/decisions.md` |
| R-18 | Keep orchestration simple: avoid unbounded retries; allow only bounded inactivity restarts with explicit caps. | `docs/spec.md`, `docs/architecture.md`, `docs/decisions.md` |
| R-19 | Run iterative deepreview loops with configurable max rounds (default 5). | `docs/spec.md`, `docs/architecture.md` |
| R-20 | Round status flag files are required execute artifacts (`continue|stop`) for traceability, and round-loop control combines consecutive status decisions with repository change detection. | `docs/spec.md`, `docs/architecture.md`, `docs/decisions.md` |
| R-21 | Do not push in intermediate rounds; allow delivery-stage publication only after rounds are complete. | `docs/spec.md`, `docs/architecture.md`, `docs/decisions.md` |
| R-22 | Each round/stage must use fresh isolated worktrees to minimize stale code/context carryover. | `docs/spec.md`, `docs/architecture.md` |
| R-23 | Aggressive cleanup: remove obsolete worktrees and transient artifacts as soon as they are no longer needed. | `docs/spec.md`, `docs/architecture.md`, `docs/decisions.md` |
| R-24 | Codex may inspect repo history, recent commits, PR context, and local skill availability if useful. | `docs/spec.md`, `docs/architecture.md`, `prompts/README.md` |
| R-25 | Mode interface supports `--mode yolo` and `--yolo` alias; default remains PR mode. | `docs/spec.md` |
| R-26 | Delivery naming conventions use `deepreview/` branch prefix and `deepreview:` PR title prefix. | `docs/spec.md`, `docs/decisions.md` |
| R-27 | Round-loop stop policy is two consecutive `stop` decisions: `continue` always forces another round, first `stop` forces one confirmation round, second consecutive `stop` ends the loop even if that round changed the repo. | `docs/spec.md`, `docs/architecture.md`, `docs/decisions.md` |
| R-28 | Encourage local commits throughout execution; ensure changed round work is committed locally (no empty commits) before round completion. | `docs/spec.md`, `docs/architecture.md`, `docs/decisions.md` |
| R-29 | Local candidate branch naming uses `deepreview/candidate/<source-branch>/<run-id>`. | `docs/spec.md`, `docs/architecture.md`, `docs/decisions.md` |
| R-30 | Default final PR body uses canonical sections: summary, what changed and why, round outcomes, verification, risks and follow-ups, and final status. | `docs/spec.md`, `docs/decisions.md`, `prompts/delivery/01-deliver.md` |
| R-31 | Verification execution is Codex-led; Codex should run available tests/pre-commit/local CI checks and report outcomes. | `docs/spec.md`, `docs/architecture.md`, `docs/decisions.md` |
| R-32 | Round stop-flag file uses a strict schema with enum decision values and deterministic run-artifact path. | `docs/spec.md`, `docs/architecture.md`, `docs/decisions.md` |
| R-33 | Prompt templates are file-based and unversioned. | `docs/spec.md`, `docs/decisions.md` |
| R-34 | Independent review stage uses one shared prompt template for all independent reviewers. | `docs/spec.md`, `docs/architecture.md`, `prompts/review/independent-review.md` |
| R-35 | Execute stage runs an ordered two-prompt queue sequentially in one fresh Codex chat context, with review artifact paths and a compact manifest provided as prompt context; inactivity retries restart fresh and rely on preserved round artifacts rather than the stalled thread. | `docs/spec.md`, `docs/architecture.md`, `prompts/execute/queue.txt`, `prompts/execute/01-triage-plan.md`, `prompts/execute/02-implement-verify-finalize.md`, `internal/deepreview/orchestrator.go` |
| R-36 | Triage-and-plan treats independent reviews as inputs (not gospel), performs independent validation, and only advances high-confidence material items. | `docs/spec.md`, `docs/architecture.md`, `docs/decisions.md`, `prompts/execute/01-triage-plan.md` |
| R-37 | Triage-and-plan must produce an end-to-end executable plan, defer low-confidence items, and include explicit docs/decision updates and verification matrix. | `docs/spec.md`, `prompts/execute/01-triage-plan.md`, `prompts/README.md` |
| R-38 | Implement/verify/finalize must run end-to-end with minimum local verification gates (tests, pre-commit, CI-like checks where available) plus evidence capture, doc updates, round summary/status writing, and local commit responsibility. | `docs/spec.md`, `docs/architecture.md`, `prompts/execute/02-implement-verify-finalize.md` |
| R-39 | Deepreview should favor a small number of no-regret material changes over many tiny edits, and should explicitly allow simplifications, cleanup, refactors, and documentation fixes when material and high-confidence. | `docs/spec.md`, `docs/decisions.md`, `prompts/review/independent-review.md`, `prompts/execute/01-triage-plan.md`, `prompts/execute/02-implement-verify-finalize.md` |
| R-40 | `README.md` is the user-facing guide and should focus on project purpose, setup, usage, and operational ergonomics rather than internal posture details. | `README.md`, `docs/decisions.md`, `docs/workflows.md` |
| R-41 | `README.md` should include optional shell alias guidance for frequent CLI usage. | `README.md`, `docs/decisions.md`, `docs/workflows.md` |
| R-42 | `<repo>` and `--source-branch` are optional when current directory is a valid local GitHub repo; deepreview infers missing values from local context. | `README.md`, `docs/spec.md`, `docs/architecture.md`, `internal/deepreview/cli.go`, `internal/deepreview/local_context.go` |
| R-43 | When source branch is inferred from local context, or when an explicit source branch matches the current branch in a supported local repo context, deepreview requires no tracked local changes and exact local/upstream sync before run start, using a refreshed upstream ref for the comparison. | `README.md`, `docs/spec.md`, `docs/architecture.md`, `internal/deepreview/local_context.go`, `internal/deepreview/local_context_test.go`, `internal/deepreview/cli_test.go` |
| R-44 | Managed repo clone in workspace is branch-scoped and replaced with a fresh clone each run to avoid stale state/worktree leftovers. | `docs/spec.md`, `docs/architecture.md`, `docs/decisions.md`, `internal/deepreview/gitops.go`, `internal/deepreview/gitops_test.go` |
| R-45 | In `yolo` mode targeting default branch, deepreview must preflight push permission using dry-run and fail fast if disallowed. | `docs/spec.md`, `docs/architecture.md`, `internal/deepreview/orchestrator.go` |
| R-46 | YOLO mode ergonomics should accept `--mode YOLO` and legacy `--YOLO` while preserving default PR mode behavior. | `docs/spec.md`, `README.md`, `internal/deepreview/cli.go`, `internal/deepreview/cli_test.go` |
| R-47 | Execute prompt wiring should use review-stage terminology (`REVIEW_*` placeholders) only. | `internal/deepreview/orchestrator.go`, `docs/decisions.md`, `prompts/execute/01-triage-plan.md` |
| R-48 | Review and execute consolidation must reject low-value churn; accepted items must be explicitly marked material/high-confidence rather than severity-only bug labels. | `docs/spec.md`, `docs/decisions.md`, `prompts/review/independent-review.md`, `prompts/execute/01-triage-plan.md`, `prompts/README.md` |
| R-49 | Delivery runs in one fresh Codex context for final local branch preparation; deepreview then validates, publishes, and performs bounded post-create mergeability checks before classifying the result. | `internal/deepreview/orchestrator.go`, `prompts/delivery/01-deliver.md`, `docs/spec.md`, `docs/architecture.md`, `docs/decisions.md` |
| R-50 | All Codex prompt calls (new/resume, including delivery) must use the operator's normal local Codex configuration unless deepreview adds a documented override for a specific reason. | `internal/deepreview/codex.go`, `internal/deepreview/orchestrator.go`, `internal/deepreview/cli.go`, `docs/spec.md`, `docs/decisions.md` |
| R-51 | Deepreview must provide non-mutating helper commands: `doctor` for preflight checks and `dry-run` for ordered execution preview. | `internal/deepreview/cli.go`, `internal/deepreview/cli_test.go`, `README.md`, `docs/spec.md`, `docs/architecture.md`, `docs/decisions.md` |
| R-52 | Privacy boundary is split by surface: public/delivery surfaces must be redacted/guarded, while local terminal output remains literal for operator debugging. | `docs/spec.md`, `docs/architecture.md`, `docs/decisions.md`, `internal/deepreview/orchestrator.go`, `internal/deepreview/cli.go`, `internal/deepreview/progress.go`, `internal/deepreview/text_reporter.go` |
| R-53 | In TUI mode, completion must not block on keypress; deepreview exits UI automatically, clears terminal, and then prints text completion summary. | `internal/deepreview/tui.go`, `internal/deepreview/cli.go`, `internal/deepreview/tui_test.go`, `internal/deepreview/cli_test.go`, `docs/spec.md`, `docs/architecture.md`, `docs/decisions.md`, `README.md` |
| R-54 | The orchestrator should stay thin and operational: workspace/worktree isolation, stage launching/resume, fresh-context policy, activity monitoring, artifact validation, and final run classification are hardcoded; repo-specific mutation work is delegated to Codex where practical. | `docs/spec.md`, `docs/architecture.md`, `docs/decisions.md`, `internal/deepreview/orchestrator.go` |
| R-55 | Prompt templates for machine-validated artifacts include explicit output schemas and examples to improve formatting reliability. | `prompts/execute/01-triage-plan.md`, `prompts/execute/02-implement-verify-finalize.md`, `prompts/review/independent-review.md`, `prompts/README.md`, `docs/decisions.md` |
| R-56 | On user interrupt (`Ctrl+C`), deepreview must terminate active worker commands immediately, emit an interrupt failure summary, scrub lingering transient worktrees, and exit with cancel status. | `internal/deepreview/cli.go`, `internal/deepreview/process.go`, `internal/deepreview/process_unix.go`, `internal/deepreview/orchestrator.go`, `internal/deepreview/gitops.go`, `internal/deepreview/cli_test.go`, `internal/deepreview/integration_test.go`, `docs/spec.md`, `docs/architecture.md`, `docs/decisions.md` |
| R-57 | Supported host operating systems are macOS and Linux only; Windows support is intentionally out of scope and should not be maintained in code or docs. | `README.md`, `docs/spec.md`, `docs/decisions.md`, `docs/project-preferences.md`, `internal/deepreview/process_unix.go` |
| R-58 | All Codex prompt executions use the operator's normal local Codex configuration and inherited local environment by default; deepreview does not force a separate execution layer, model, reasoning profile, or temp/cache override layer. | `internal/deepreview/codex.go`, `internal/deepreview/codex_test.go`, `internal/deepreview/integration_test.go`, `docs/spec.md`, `docs/architecture.md`, `docs/decisions.md`, `README.md` |
| R-59 | When source branch equals default branch, independent review must continue as a current-state repository audit instead of stopping at an empty branch diff. | `internal/deepreview/orchestrator.go`, `prompts/review/independent-review.md`, `internal/deepreview/orchestrator_test.go`, `internal/deepreview/integration_test.go`, `docs/spec.md`, `docs/architecture.md`, `docs/decisions.md` |
| R-60 | Concurrent runs are allowed for different source branches of the same repository, but the exact same repo+source-branch pair must remain serialized. | `docs/spec.md`, `docs/architecture.md`, `docs/decisions.md`, `internal/deepreview/orchestrator.go`, `internal/deepreview/orchestrator_test.go`, `internal/deepreview/integration_test.go` |
| R-61 | Deepreview-managed commits must use the operator's resolved Git identity from source-repo Git config first, then global Git config, and must not fail because of host GPG signing configuration. | `docs/spec.md`, `docs/architecture.md`, `docs/decisions.md`, `internal/deepreview/git_identity.go`, `internal/deepreview/gitops.go`, `internal/deepreview/gitops_test.go` |
| R-62 | Final completion reporting must count only successful rounds with authoritative `round.json` records; failed execute attempts must not be reported as completed or final rounds. | `docs/spec.md`, `docs/architecture.md`, `docs/decisions.md`, `internal/deepreview/orchestrator.go`, `internal/deepreview/cli.go`, `internal/deepreview/cli_test.go` |
| R-63 | Clean review runs with no deliverable repository changes must complete successfully with final summary artifacts but without push or PR delivery. | `docs/spec.md`, `internal/deepreview/orchestrator.go`, `internal/deepreview/cli.go`, `internal/deepreview/integration_test.go` |
| R-64 | Deepreview should prefer canonical markdown/json artifacts by default and treat large raw worker logs as optional/debug-oriented retention. | `docs/spec.md`, `docs/decisions.md`, `internal/deepreview/orchestrator.go` |
| R-65 | Deepreview should instruct Codex to inspect available local skills and use relevant ones when useful, without assuming a fixed skill inventory. | `prompts/review/independent-review.md`, `prompts/execute/01-triage-plan.md`, `prompts/execute/02-implement-verify-finalize.md`, `prompts/delivery/01-deliver.md`, `docs/spec.md`, `prompts/README.md` |
| R-66 | The delivery prompt result contract should stay local-only: it reports local readiness state (mode and incomplete status/reason), keeps `delivery_branch` unset, and leaves push refspecs, PR creation, and remote publication metadata to deepreview. | `docs/spec.md`, `docs/architecture.md`, `docs/decisions.md`, `prompts/delivery/01-deliver.md`, `internal/deepreview/orchestrator.go`, `internal/deepreview/orchestrator_test.go` |

## Alignment gate
For each requirement touched by a change, evidence must be captured for:
- planned: plan item references requirement IDs.
- implemented: code/docs paths or commits that satisfy behavior.
- executed: run evidence (commands/run logs/artifacts).
- verified: tests/checks that validate behavior.

Keep active evidence in `plan/current/alignment-status.md`, and promote stable process rules into `docs/workflows.md` or `docs/decisions.md`.
