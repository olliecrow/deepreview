You are in deepreview execute stage for round `{{ROUND_NUMBER}}`.

This is prompt 2 of 2, in the same Codex chat context as prompt 1.

## Inputs
- Approved triage: `{{ROUND_TRIAGE_PATH}}`
- Approved plan: `{{ROUND_PLAN_PATH}}`
- Verification output path: `{{ROUND_VERIFICATION_PATH}}`
- Round status output path: `{{ROUND_STATUS_PATH}}`
- Round summary output path: `{{ROUND_SUMMARY_PATH}}`
- Repository/worktree context and triage context.

{{ROUND_MODE_NOTE}}

## Mandatory setup
1. Inspect the locally available Codex skills and use any relevant ones if they exist.
2. Work deeply, proactively, and autonomously; do not wait for follow-up prompts.
3. Always execute against `{{WORKTREE_PATH}}` and write artifacts to the exact paths provided. If your starting `pwd` is elsewhere, switch to `{{WORKTREE_PATH}}` before making or verifying changes.

## Task
Execute the plan end-to-end, verify thoroughly, finalize the round artifacts, and leave the worktree clean with any needed local commit already created.

{{ROUND_EXECUTE_MODE_OVERRIDE}}

Execution requirements:
1. Apply all approved code/doc changes.
2. Keep changes scoped to accepted triage items only.
3. Accepted work may include bug fixes, simplifications, deletions, high-value refactors, meaningful cleanup, or documentation alignment, but only when the triage marked it material and high-confidence.
4. Do not add low-value churn, speculative hardening, or nice-to-have cleanup.
5. Keep implementation simple and pragmatic; avoid speculative over-engineering.
6. Prefer deleting dead code, removing unnecessary branches, or shrinking scope over adding new machinery when that cleanly resolves the accepted item.
7. Maintain a high no-regret bar while implementing; if confidence drops materially, stop and document instead of forcing changes.
8. Run Codex-led verification:
   - run relevant tests when available
   - run pre-commit checks when available
   - run locally runnable CI-like checks when available
9. Add quick empirical checks for changed behavior when feasible and not long-running.
10. Capture command-level evidence and outcomes.
11. Update relevant docs/notes/decision records required by the accepted changes.
12. Write `{{ROUND_VERIFICATION_PATH}}` with:
   - commands attempted
   - pass/fail outcomes
   - checks skipped with reason
   - unresolved failures or blockers
   - residual risks
13. Write `{{ROUND_SUMMARY_PATH}}` with:
   - accepted/rejected/deferred triage outcomes
   - implemented changes
   - verification evidence overview
   - residual risks
   - complexity/size impact
   - strict-scope statement confirming accepted work remained material/high-confidence only
14. Write `{{ROUND_STATUS_PATH}}` JSON with strict schema:
```json
{
  "decision": "continue|stop",
  "reason": "non-empty string",
  "confidence": 0.0,
  "next_focus": "optional string"
}
```
15. Decide round status conservatively:
   - `stop` when quality is sufficient or no further meaningful changes are needed
   - `continue` when another round is likely to materially improve quality
16. Create any needed local commit before finishing this prompt.
17. Internal deepreview artifacts are operational outputs; never stage/commit/push them.
18. Leave the worktree clean when the prompt exits.

Rules:
- Do not push.
- Do not open PRs.
- Use the normal inherited local environment. Do not rewrite temp/cache/network settings unless a specific check clearly requires it.
- If a planned verification path proves impractical locally, record the blocker in verification output and continue with the best reliable substitute instead of thrashing on setup.
- You may use multiple sub-agents or staged execution inside this prompt if useful.
- Do not expose secrets, tokens, personal information, or sensitive values in outputs.
- You may inspect git history, PR comments, issues, and other GitHub context if useful.
- If a planned check cannot run locally, record why and provide the closest reliable substitute run.
- Do not add low-value robustness for extremely rare edge cases unless strongly justified by evidence.
