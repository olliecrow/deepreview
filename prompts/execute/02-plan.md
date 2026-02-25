You are in deepreview execute stage for round `{{ROUND_NUMBER}}`.

This is prompt 2 of 4, in the same Codex chat context as prompt 1.

## Inputs
- Triage decisions: `{{ROUND_TRIAGE_PATH}}`
- Repository/worktree context from prior prompt.

## Task: plan
Create a high-confidence implementation and verification plan for accepted items.

Process:
1. Work deeply, proactively, and autonomously; do not wait for follow-up prompts.
2. Convert accepted triage items into concrete change tasks.
3. Order tasks to minimize regression risk and implementation regret.
4. Ensure this is a full end-to-end plan (implementation, verification, cleanup/docs, and commit outcomes) with no missing decision points.
5. Define per-task verification actions with concrete commands/checks.
6. Include minimum verification gates: relevant tests, pre-commit checks, and locally runnable CI-like checks when available.
7. Add short empirical checks or standalone validation scripts where feasible and quick.
8. Identify required docs/notes/decision updates.
9. Call out risks and mitigations before execution.
10. Ensure each planned task is justified by evidence and expected impact.
11. Prefer plans that simplify the codebase where possible (remove redundancy, reduce complexity, improve clarity and resource efficiency).
12. Avoid over-engineering; do not add low-value robustness for rare edge cases in greenfield/fast-iteration contexts.
13. If confidence/conviction for a task is not high, do not include it in the executable plan.

Rules:
- No code edits in this prompt.
- Keep the plan specific enough to execute directly.
- Scope tightly to accepted items; avoid speculative extra work.
- You may use multiple sub-agents or staged analysis inside this prompt if useful.
- Do not expose secrets, tokens, or sensitive values in outputs.
- You may inspect git history, PR comments, issues, and other GitHub context if useful.
- If confidence for a plan item is not high, exclude it from execution and mark it deferred with rationale.

Output:
- Write the plan to `{{ROUND_PLAN_PATH}}` with sections:
  - scope
  - task list
  - complexity/size impact
  - verification matrix
  - docs/notes/decision updates
  - risks and mitigations
  - stop conditions
- If accepted items are empty, produce a no-op plan and explicitly state there is no execute work for this round.
