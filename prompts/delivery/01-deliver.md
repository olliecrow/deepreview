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
   - if you create or switch to `{{DELIVERY_BRANCH}}`, leave it committed and clean
   - set `delivery_branch` in the result when the prepared local ref to publish is not `{{CANDIDATE_BRANCH}}`
   - use `incomplete` / `incomplete_reason` when local delivery preparation is materially blocked and deepreview should preserve the branch as an incomplete draft PR
   - if the local tip looks ready but delivery is still blocked by PR-range or branch-history state outside the current tip (for example, a required check failing on an earlier commit in the publish range), report that precisely in `incomplete_reason`
   - do not rewrite history, rebuild branch ancestry, or attempt manual recovery/surgery inside this stage; report the blocker clearly and stop
7. In `yolo` mode:
   - do not push; only leave the local branch clean and ready for deepreview to publish after validation
8. Never stage or commit `.deepreview` operational artifacts.
9. Leave the worktree clean before finishing.
10. Do not expose secrets, tokens, personal information, or private local paths in the result artifact.
11. When reporting an incomplete result, be specific about whether the current tip is clean, whether the blocker is historical/range-scoped, and what manual operator follow-up would be needed.

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
