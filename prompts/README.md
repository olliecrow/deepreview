# Prompt Templates

This directory contains file-based, unversioned prompt templates for deepreview.

## Goals
- Keep prompt behavior explicit, editable, and deterministic.
- Use one shared independent-review template for all independent reviewers.
- Use an ordered execute prompt queue that runs in a single Codex chat context per round.
- Keep prompts self-contained so they work on machines without local skill packs.
- Emphasize severity-first review quality: critical red flags and serious issues first.
- Keep review output strictly focused on high-confidence `critical|high` merge-relevant issues.
- Prefer explicit output schemas with concrete examples so Codex formatting is consistent and machine-validated contracts are reliably satisfied.
- Emphasize proactive/autonomous execution and evidence-backed decisions.
- Treat independent reviews as strong inputs, not gospel, and require independent consolidation.
- Keep execution no-regret and high-conviction: defer low-confidence items and reject low/medium severity work in this workflow.
- Require end-to-end plan and execute behavior, including local verification and docs/decision upkeep.
- In PR mode, generate a detailed post-delivery Codex PR title and PR description body, then apply both as final PR metadata.
- Post-delivery PR metadata quality should come from Codex reading run artifacts/logs/repo context directly, not injected pre-digested summary blocks.

## Layout
- `prompts/review/independent-review.md`: template used by every independent-review worker.
- `prompts/execute/queue.txt`: ordered list of execute prompt templates.
- `prompts/execute/01-consolidate-reviews.md`
- `prompts/execute/02-plan.md`
- `prompts/execute/03-execute-verify.md`
- `prompts/execute/04-cleanup-summary-commit.md`
- `prompts/delivery/pr-description-summary.md`: post-PR template used to generate the final PR title and description body.

## Rendering notes
- Templates are rendered with run- and round-specific template variables (for example `{{ROUND_NUMBER}}`).
- If any template variable is unresolved at render time, fail fast.
- Execute prompts run sequentially in the same Codex chat context for that execute worktree.
- Independent review markdown content is injected into execute prompts using template variables.
- Legacy empty prompt directories (`fanout`, `synthesis`) are intentionally removed.
