# Prompt Templates

This directory contains file-based, unversioned prompt templates for deepreview v1.

## Goals
- Keep prompt behavior explicit, editable, and deterministic.
- Use one shared independent-review template for all independent reviewers.
- Use an ordered execute prompt queue that runs in a single Codex chat context per round.
- Keep prompts self-contained so they work on machines without local skill packs.
- Emphasize severity-first review quality: critical red flags and serious issues first.
- Allow only optional, obvious, low-risk non-blocking improvement notes as a secondary review output.
- Emphasize proactive/autonomous execution and evidence-backed decisions.
- Treat independent reviews as strong inputs, not gospel, and require independent consolidation.
- Keep execution no-regret and high-conviction: defer low-confidence items.
- Require end-to-end plan and execute behavior, including local verification and docs/decision upkeep.

## Layout
- `prompts/review/independent-review.md`: template used by every independent-review worker.
- `prompts/execute/queue.txt`: ordered list of execute prompt templates.
- `prompts/execute/01-consolidate-reviews.md`
- `prompts/execute/02-plan.md`
- `prompts/execute/03-execute-verify.md`
- `prompts/execute/04-cleanup-summary-commit.md`

## Rendering notes
- Templates are rendered with run- and round-specific template variables (for example `{{ROUND_NUMBER}}`).
- If any template variable is unresolved at render time, fail fast.
- Execute prompts run sequentially in the same Codex chat context for that execute worktree.
- Independent review markdown content is injected into execute prompts using template variables.
- Legacy empty prompt directories (`fanout`, `synthesis`) are intentionally removed.
