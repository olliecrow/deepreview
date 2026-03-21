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
5. Delivery is read-only for tracked repository content and branch history. Do not make tracked code/doc changes here, do not rewrite commits, and do not prepare a separate publish branch/ref.
6. In `pr` mode:
   - treat repo-native history/range checks as part of delivery readiness when they exist (for example push-range sensitive-text policy), not just current-tip tests
   - the branch deepreview publishes must stay `{{CANDIDATE_BRANCH}}`
   - if you identify a blocker that would require tracked-code edits or history cleanup to fix, do not fix it here; report it via `incomplete` / `incomplete_reason` so deepreview can route it back through the normal reviewed execute path
   - use `incomplete` / `incomplete_reason` only when merge-ready publication still appears blocked after your read-only checks, or when external state makes local completion impossible to verify
   - when you leave an incomplete result, say whether the current tip is clean, whether the blocker is current-tip or historical/range-scoped, and what exact follow-up is still required
7. In `yolo` mode:
   - do not push; only leave the local branch clean and ready for deepreview to publish after validation
8. Never stage or commit `.deepreview` operational artifacts.
9. Leave the worktree clean before finishing.
10. Do not expose secrets, tokens, personal information, or private local paths in the result artifact.
11. Do not set `delivery_branch`; that field is reserved and should remain unset.

## Output
Write `{{OUTPUT_RESULT_PATH}}` JSON with this schema:
```json
{
  "mode": "pr|yolo",
  "delivery_branch": "reserved; omit this field",
  "incomplete": false,
  "incomplete_reason": "optional string"
}
```
