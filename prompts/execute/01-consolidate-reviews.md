You are in deepreview execute stage for round `{{ROUND_NUMBER}}`.

This is prompt 1 of 4. Stay in this same Codex chat context for prompts 2-4.

## Inputs
- Repository: `{{REPO_SLUG}}`
- Source branch: `{{SOURCE_BRANCH}}`
- Default branch: `{{DEFAULT_BRANCH}}`
- Round: `{{ROUND_NUMBER}}` / max `{{MAX_ROUNDS}}`
- Execute worktree: `{{WORKTREE_PATH}}`
- Independent review files:
{{REVIEW_REPORT_PATHS}}

Injected review content:

{{REVIEW_REPORTS_MARKDOWN}}

## Task: consolidate reviews
Investigate every reported item and decide what should actually be acted on.

Process:
1. Work deeply, proactively, and autonomously; do not wait for follow-up prompts.
2. Enumerate all findings across independent reviews.
3. Step back and treat reviews as input signals, not gospel; do an independent reassessment.
4. Merge duplicates/common themes across reviewers and note where multiple reviewers independently flagged the same item.
5. Prioritize severity first (critical/high merge blockers first).
6. Validate each finding by inspecting code and relevant context, including boundary/integration code.
7. Investigate candidate items one by one and perform a "no-regret" check before accepting.
8. Label each item as `accept`, `reject`, or `defer`.
9. For each `accept`, define intended outcome, constraints, and why this is high confidence and high conviction.
10. For each `reject`/`defer`, capture concise reason with evidence.
11. Keep only high-conviction items that are worth implementation effort this round.
12. Use a high acceptance threshold: if confidence is not clearly high, reject/defer.
13. Explicitly look for high-value simplification opportunities (remove code, remove duplication, reduce complexity, improve maintainability/perf/memory).
14. Do not accept speculative robustness work for rare edge cases unless impact is clearly material.

Rules:
- Do not modify code in this prompt.
- Do not commit or push.
- Be conservative about false positives.
- Prefer clear evidence over speculation.
- You may use multiple sub-agents or staged analysis inside this prompt if useful.
- Do not expose secrets, tokens, personal information, or sensitive values in outputs.
- You may inspect git history, PR comments, issues, and other GitHub context if useful.
- If an item remains low-confidence after investigation, reject or defer it rather than forcing it into execution.

Output:
- Write triage decisions to `{{ROUND_TRIAGE_PATH}}`.
- For each item, include: source reviewers, commonality count, disposition (`accept|reject|defer`), evidence summary, and rationale.
- End with a compact, prioritized list of accepted items that should drive the implementation plan.
- If there are no accepted items, state explicitly: `No execute items selected for this round.`
- For accepted items, include expected net effect on code complexity/size (`reduce`, `neutral`, or `increase`) with justification.
