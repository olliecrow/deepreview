You are in the deepreview final delivery stage.

## Scope
- Mode: `{{MODE}}`
- Repository: `{{REPO_SLUG}}`
- Source branch: `{{SOURCE_BRANCH}}`
- Default branch: `{{DEFAULT_BRANCH}}`
- Candidate branch: `{{CANDIDATE_BRANCH}}`
- Delivery branch: `{{DELIVERY_BRANCH}}`
- Run id: `{{RUN_ID}}`
- Worktree path: `{{WORKTREE_PATH}}`
- Run root: `{{RUN_ROOT}}`
- Changed files:
{{CHANGED_FILES}}
- Round summaries:
{{ROUND_SUMMARY_PATHS}}
- Output result path: `{{OUTPUT_RESULT_PATH}}`

## Mandatory setup
1. Inspect the locally available Codex skills and use any relevant ones if they exist.
2. Work proactively and autonomously. Do not wait for follow-up prompts.
3. Always anchor repo work to `{{WORKTREE_PATH}}` and run-artifact inspection to `{{RUN_ROOT}}`.

## Goal
Get local branch state and delivery intent into a state where deepreview can publish safely after it runs the final orchestrator-owned delivery validation.

## Delivery requirements
1. Inspect the current candidate diff, local history, and the round artifacts under `{{RUN_ROOT}}`.
2. Run any final local merge-readiness checks still needed:
   - relevant tests
   - pre-commit checks
   - locally runnable CI-like checks
3. Keep changes high-confidence, material, and tightly scoped. Do not introduce low-value churn.
4. Prefer simplification or deletion over additive complexity when that cleanly resolves a real blocker.
5. In `pr` mode, prepare the delivery branch locally if needed, but do not push and do not open/edit the PR yourself.
6. In `pr` mode:
   - treat repo-native history/range checks as part of delivery readiness when they exist (for example push-range sensitive-text policy), not just current-tip tests
   - if you create or switch to `{{DELIVERY_BRANCH}}`, leave it committed and clean
   - set `delivery_branch` in the result when the prepared local ref to publish is not `{{CANDIDATE_BRANCH}}`
   - if the current tip is good but publication is blocked by PR-range or branch-history state outside the current tip, first try to repair that locally instead of defaulting to `incomplete`
   - the preferred repair path for history-scoped blockers is: keep `{{CANDIDATE_BRANCH}}` as the reviewed branch, create `{{DELIVERY_BRANCH}}` from `origin/{{DEFAULT_BRANCH}}` (or the relevant source-branch base), replay the final reviewed candidate tree into clean commit history there, rerun the relevant local checks, and publish that rebuilt branch by setting `delivery_branch`
   - do not rewrite `{{CANDIDATE_BRANCH}}` ancestry in place to repair history-scoped blockers; preserve the reviewed candidate branch and do history cleanup only on `{{DELIVERY_BRANCH}}`
   - use `incomplete` / `incomplete_reason` only when local delivery preparation or clean-history repair is materially blocked, unverifiable, or would require unsafe/speculative surgery
   - when you leave an incomplete result, say whether the current tip is clean, whether the blocker is current-tip or historical/range-scoped, and what exact follow-up is still required
7. In `yolo` mode:
   - do not push; only leave the local branch clean and ready for deepreview to publish after validation
8. Never stage or commit `.deepreview` operational artifacts.
9. Leave the worktree clean before finishing.
10. Do not expose secrets, tokens, personal information, or private local paths in the result artifact.
11. When history-scoped repair succeeds, prefer publishing the repaired delivery branch normally instead of asking the operator to salvage it by hand.

## Output
Write `{{OUTPUT_RESULT_PATH}}` JSON with this schema:
```json
{
  "mode": "pr|yolo",
  "delivery_branch": "optional prepared local branch/ref to publish",
  "incomplete": false,
  "incomplete_reason": "optional string"
}
```
