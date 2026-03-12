You are in deepreview execute stage for round `{{ROUND_NUMBER}}`.

This is prompt 2 of 3, in the same Codex chat context as prompt 1.

## Inputs
- Approved plan: `{{ROUND_PLAN_PATH}}`
- Repository/worktree context and triage context.

{{ROUND_MODE_NOTE}}

## Task
Execute the plan end-to-end and verify thoroughly.

{{ROUND_EXECUTE_MODE_OVERRIDE}}

Execution requirements:
1. Work deeply, proactively, and autonomously; do not wait for follow-up prompts.
2. Execute from start to finish in this prompt without waiting for more prompts.
3. Apply all approved code/doc changes.
4. Keep changes scoped to accepted triage items.
5. Prioritize fixes for critical/high-severity accepted items first.
6. Execute only accepted `critical|high` items; do not add low/medium severity cleanup or optional improvements.
7. Keep implementation simple and pragmatic; avoid speculative over-engineering.
8. Maintain a high no-regret bar while implementing; if confidence drops materially, stop and document instead of forcing changes.
9. Prefer simplification outcomes only when they are directly required for accepted critical/high fixes.
10. Run codex-led verification:
   - run relevant tests when available
   - run pre-commit checks when available
   - run locally runnable CI-like checks when available
11. Add quick empirical checks (for changed behavior) when feasible and not long-running.
12. Capture command-level evidence and outcomes.
13. If verification fails, stop and report failures clearly with actionable context.

Rules:
- Do not push.
- Do not open PRs.
- Keep behavior simple; no retry loops.
- The inherited environment already points temp/cache paths at run-scoped writable directories outside the repo worktree; use that environment instead of host cache paths.
- You may use multiple sub-agents or staged execution inside this prompt if useful.
- Do not expose secrets, tokens, personal information, or sensitive values in outputs.
- You may inspect git history, PR comments, issues, and other GitHub context if useful.
- If a planned check cannot run locally, record why and provide the closest reliable substitute run.
- Do not add low-value robustness for extremely rare edge cases unless strongly justified by evidence.

Output:
- Write verification evidence to `{{ROUND_VERIFICATION_PATH}}` with:
  - commands attempted
  - pass/fail outcomes
  - checks skipped with reason
  - unresolved failures or blockers
  - residual risks
