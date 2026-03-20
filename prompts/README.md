# Prompt Templates

This directory contains file-based, unversioned prompt templates for deepreview.

## Goals
- Keep prompt behavior explicit, editable, and deterministic.
- Use one shared independent-review template for all independent reviewers.
- Use an ordered two-prompt execute queue that runs in a single Codex chat context per round.
- Keep prompts self-contained so they work on machines without local skill packs.
- Tell Codex in every prompt to inspect available local skills and use any relevant ones if present, without assuming a specific skill exists.
- Keep review output focused on high-confidence, material improvements rather than broad suggestion dumping.
- Prefer explicit output schemas with concrete examples so Codex formatting is consistent and machine-validated contracts are reliably satisfied.
- Emphasize proactive/autonomous execution and evidence-backed decisions.
- Treat independent reviews as strong inputs, not gospel, and require independent consolidation.
- Require execute prompt 1 to confirm proposed changes item by item before accepting implementation work.
- Keep execution no-regret and high-conviction: defer low-confidence items and reject low-value churn.
- Require end-to-end plan and execute behavior, including local verification and docs/decision upkeep.
- In PR mode, let one fresh delivery prompt own final local merge-readiness and branch preparation, while deepreview owns publication, PR creation, and bounded post-create mergeability checks.
- Prefer Codex reading on-disk review artifacts directly instead of receiving large injected summary blocks.

## Layout
- `prompts/review/independent-review.md`: template used by every independent-review worker.
- `prompts/execute/queue.txt`: ordered list of execute prompt templates.
- `prompts/execute/01-triage-plan.md`
- `prompts/execute/02-implement-verify-finalize.md`
- `prompts/delivery/01-deliver.md`: fresh delivery template used for final branch/PR delivery.

## Rendering notes
- Templates are rendered with run- and round-specific template variables (for example `{{ROUND_NUMBER}}`).
- If any template variable is unresolved at render time, fail fast.
- Execute prompts run sequentially in the same Codex chat context for that execute worktree on the normal path; inactivity retries restart fresh and rely on the written round artifacts.
- Execute prompt 1 receives review artifact paths plus a compact manifest.
- Legacy empty prompt directories (`fanout`, `synthesis`) are intentionally removed.
