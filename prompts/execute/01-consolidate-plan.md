You are in deepreview execute stage for round `{{ROUND_NUMBER}}`.

This is prompt 1 of 3. Stay in this same Codex chat context for prompts 2-3.

## Inputs
- Repository: `{{REPO_SLUG}}`
- Source branch: `{{SOURCE_BRANCH}}`
- Default branch: `{{DEFAULT_BRANCH}}`
- Round: `{{ROUND_NUMBER}}` / max `{{MAX_ROUNDS}}`
- Execute worktree: `{{WORKTREE_PATH}}`
- Independent review files:
{{REVIEW_REPORT_PATHS}}
- Triage output path: `{{ROUND_TRIAGE_PATH}}`
- Plan output path: `{{ROUND_PLAN_PATH}}`

{{ROUND_MODE_NOTE}}

Injected review summaries:

{{REVIEW_SUMMARIES_MARKDOWN}}

## Task: consolidate and plan
First decide what is real and worth acting on. Then turn only the accepted work into a concrete implementation and verification plan.

Process:
1. Work deeply, proactively, and autonomously; do not wait for follow-up prompts.
2. Investigate every reported item and treat reviewer output as input signals, not gospel.
3. Merge duplicates/common themes across reviewers and note where multiple reviewers independently flagged the same item.
4. Validate each candidate item directly in code and surrounding context.
5. Label each item as `accept`, `reject`, or `defer`.
6. Only `accept` items that are both severity `critical|high` and confidence `high`.
7. Reject or defer anything speculative, low-confidence, or non-blocking.
8. After triage, convert only accepted items into a concrete end-to-end execution plan.
9. The plan must include implementation, verification, cleanup/docs work, and stop conditions.
10. Prefer solutions that simplify the codebase when that directly helps resolve accepted items.
11. Treat high-confidence removals, deletions, and scope reductions as first-class fix options when they solve accepted items cleanly.
12. If no items are accepted, produce a no-op plan and say so explicitly.
13. Always anchor repo inspection and artifact writes to `{{WORKTREE_PATH}}` and the absolute output/report paths below. If your starting `pwd` is elsewhere, switch to `{{WORKTREE_PATH}}` before investigating.

Rules:
- Do not modify code in this prompt.
- Do not commit or push.
- Keep scope tightly limited to accepted `critical|high` items.
- Do not expose secrets, tokens, personal information, or sensitive values in outputs.
- You may inspect git history, PR comments, issues, and other GitHub context if useful.

Output requirements:
- Write triage decisions to `{{ROUND_TRIAGE_PATH}}`.
- Write the execution plan to `{{ROUND_PLAN_PATH}}`.

Required triage content:
- For each item: source reviewers, commonality count, disposition (`accept|reject|defer`), evidence summary, rationale.
- Accepted items must include explicit severity (`critical|high`) and confidence (`high`) tags.
- End with a compact prioritized list of accepted items.
- If none are accepted, state explicitly: `No execute items selected for this round.`

Required plan sections:
- scope
- task list
- complexity/size impact
- verification matrix
- docs/notes/decision updates
- risks and mitigations
- stop conditions
