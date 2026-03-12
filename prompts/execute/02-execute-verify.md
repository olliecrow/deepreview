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
8. Prefer deleting dead code, removing unnecessary branches, or shrinking scope over adding new machinery when that cleanly resolves the accepted issue.
9. Maintain a high no-regret bar while implementing; if confidence drops materially, stop and document instead of forcing changes.
10. Prefer simplification outcomes when they clearly improve the accepted critical/high fix or remove obvious bloat uncovered during that fix.
11. Run codex-led verification:
   - run relevant tests when available
   - run pre-commit checks when available
   - run locally runnable CI-like checks when available
12. Add quick empirical checks (for changed behavior) when feasible and not long-running.
13. Capture command-level evidence and outcomes.
14. If verification fails, stop and report failures clearly with actionable context.

Rules:
- Do not push.
- Do not open PRs.
- Keep behavior simple; no retry loops.
- Always execute against `{{WORKTREE_PATH}}` and write artifacts to the exact paths provided. If your starting `pwd` is elsewhere, switch to `{{WORKTREE_PATH}}` before making or verifying changes.
- Use the normal inherited local environment. Do not rewrite temp/cache/network settings unless a specific check clearly requires it.
- If a planned verification path proves impractical locally, record the blocker in verification output and continue with the best reliable substitute instead of thrashing on setup.
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
