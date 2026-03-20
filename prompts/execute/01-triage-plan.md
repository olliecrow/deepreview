You are in deepreview execute stage for round `{{ROUND_NUMBER}}`.

This is prompt 1 of 2. Stay in this same Codex chat context for prompt 2.

## Inputs
- Repository: `{{REPO_SLUG}}`
- Source branch: `{{SOURCE_BRANCH}}`
- Default branch: `{{DEFAULT_BRANCH}}`
- Round: `{{ROUND_NUMBER}}` / max `{{MAX_ROUNDS}}`
- Execute worktree: `{{WORKTREE_PATH}}`
- Independent review files:
{{REVIEW_REPORT_PATHS}}
- Review input manifest:
{{REVIEW_INPUT_MANIFEST}}
- Triage output path: `{{ROUND_TRIAGE_PATH}}`
- Plan output path: `{{ROUND_PLAN_PATH}}`

{{ROUND_MODE_NOTE}}

## Mandatory setup
1. Inspect the locally available Codex skills and use any relevant ones if they exist.
2. Work deeply, proactively, and autonomously; do not wait for follow-up prompts.
3. Always anchor repo inspection and artifact writes to `{{WORKTREE_PATH}}` and the absolute output/report paths below. If your starting `pwd` is elsewhere, switch to `{{WORKTREE_PATH}}` before investigating.

## Task: triage and plan
First decide what is real and worth acting on. Then turn only the accepted work into a concrete implementation and verification plan.

Process:
1. Build the candidate worklist from the reviewer reports, but treat reviewer output as input signals, not gospel.
2. Investigate every candidate item individually, one by one, before deciding whether to accept any implementation work.
3. Open the on-disk review files directly when you need detail; do not rely only on the manifest.
4. For each candidate item, validate it directly in code and surrounding context before moving to the next item.
5. For each candidate item, explicitly determine whether the proposed change is real, desirable, and worth making now.
6. Do not batch-accept a group of related findings without individually confirming each accepted change.
7. Merge duplicates and common themes across reviewers only after confirming that the underlying issue is real.
8. Label each item as `accept`, `reject`, or `defer` only after its individual investigation is complete.
9. Only `accept` items that are both `impact: material` and `confidence: high`.
10. Material accepted items may be bug fixes, security/safety fixes, significant simplifications, meaningful deletions, high-value refactors, meaningful cleanup, or documentation alignment.
11. Reject or defer anything speculative, low-confidence, low-payoff, style-only, or minor.
12. Prefer a small number of no-regret changes over many tiny edits.
13. Prefer simplification, deletion, or scope reduction when that cleanly resolves the accepted item.
14. If no items are accepted, produce a no-op plan and say so explicitly.

Rules:
- Do not modify repository code or docs in this prompt.
- Do not commit or push.
- Keep scope tightly limited to accepted material items.
- Do not move on to implementation planning for an item until you have individually confirmed that the change is high-confidence and worth making.
- Do not expose secrets, tokens, personal information, or sensitive values in outputs.
- You may inspect git history, PR comments, issues, and other GitHub context if useful.

Output requirements:
- Write triage decisions to `{{ROUND_TRIAGE_PATH}}`.
- Write the execution plan to `{{ROUND_PLAN_PATH}}`.

Required triage content:
- For each item: source reviewers, commonality count, disposition (`accept|reject|defer`), evidence summary, rationale.
- Show enough item-specific evidence to make clear that each accepted change was investigated and confirmed on its own merits.
- Accepted items must include explicit `impact: material` and `confidence: high` tags.
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
