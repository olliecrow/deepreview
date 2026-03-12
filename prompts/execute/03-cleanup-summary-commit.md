You are in deepreview execute stage for round `{{ROUND_NUMBER}}`.

This is prompt 3 of 3, in the same Codex chat context as prompts 1-2.

## Inputs
- Triage decisions: `{{ROUND_TRIAGE_PATH}}`
- Plan: `{{ROUND_PLAN_PATH}}`
- Verification report: `{{ROUND_VERIFICATION_PATH}}`
- Round status output path: `{{ROUND_STATUS_PATH}}`
- Round summary output path: `{{ROUND_SUMMARY_PATH}}`

{{ROUND_MODE_NOTE}}

## Task
Finalize this round with cleanup, documentation updates, strict round decision flag output, and final round artifacts.

Process:
1. Work proactively and autonomously; complete all in-scope finalization in this prompt.
2. Clean obvious temporary artifacts and redundant noise in the execute worktree.
3. Ensure docs/notes updates required by implemented changes are present and consistent with current behavior.
4. Ensure durable decision/rationale updates are captured where applicable.
5. Write round summary to `{{ROUND_SUMMARY_PATH}}` with:
- accepted/rejected/deferred triage outcomes
- implemented changes
- verification evidence overview
- residual risks
- strict-scope statement confirming accepted work remained critical/high only
6. Decide round status with high confidence:
- `stop` when quality is sufficient or no further meaningful changes are needed
- `continue` when another round is likely to materially improve quality
7. Write `{{ROUND_STATUS_PATH}}` JSON with strict schema:
```json
{
  "decision": "continue|stop",
  "reason": "non-empty string",
  "confidence": 0.0,
  "next_focus": "optional string"
}
```
8. Do not create local commits in this prompt. The orchestrator handles final commit behavior.
9. Internal deepreview artifacts are operational outputs; never stage/commit/push them.
10. Ensure output artifacts are complete and consistent for orchestrator post-processing.
11. Keep a high bar for additional follow-up work: if confidence is not high, prefer `stop` and document rationale.
12. In summary output, explicitly call out complexity/size impact (reduced/neutral/increased) and why.
13. Round-loop control is change-driven by repository diff after execute; this status decision is not the control signal.

Rules:
- Never push in this prompt.
- Never open PRs in this prompt.
- Keep round decision conservative and evidence-backed.
- Do not expose secrets, tokens, personal information, or sensitive values in outputs.
- Do not recommend speculative robustness work for rare edge cases without clear material impact.
