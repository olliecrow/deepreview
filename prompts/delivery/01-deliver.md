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
- Output PR title path: `{{OUTPUT_PR_TITLE_PATH}}`
- Output PR body path: `{{OUTPUT_PR_BODY_PATH}}`

## Mandatory setup
1. Inspect the locally available Codex skills and use any relevant ones if they exist.
2. Work proactively and autonomously. Do not wait for follow-up prompts.
3. Always anchor repo work to `{{WORKTREE_PATH}}` and run-artifact inspection to `{{RUN_ROOT}}`.

## Goal
Get the branch into a state where the user can approve and merge immediately if they choose.

## Delivery requirements
1. Inspect the current candidate diff, local history, and the round artifacts under `{{RUN_ROOT}}`.
2. Run any final local merge-readiness checks still needed:
   - relevant tests
   - pre-commit checks
   - locally runnable CI-like checks
3. Keep changes high-confidence, material, and tightly scoped. Do not introduce low-value churn.
4. Prefer simplification or deletion over additive complexity when that cleanly resolves a real blocker.
5. Maintain or improve concise, high-signal PR metadata throughout delivery.
6. In `pr` mode:
   - ensure the delivery branch is up to date
   - push the delivery branch
   - create or update the PR
   - keep the PR title/body concise, concrete, and current
   - wait for required remote checks to finish
   - if remote checks or mergeability fail because of a high-confidence fixable issue, make the fix, push again, update PR text if needed, and continue
   - stop only when the PR is merge-ready or when an explicit blocker cannot be resolved autonomously with high confidence
7. In `yolo` mode:
   - push the source branch only after local verification is satisfactory
8. Never stage or commit `.deepreview` operational artifacts.
9. Leave the worktree clean before finishing.
10. Do not expose secrets, tokens, personal information, or private local paths in PR title/body or result artifacts.

## Output
Write `{{OUTPUT_RESULT_PATH}}` JSON with this schema:
```json
{
  "mode": "pr|yolo",
  "delivery_branch": "optional remote delivery branch name",
  "pushed_refspec": "non-empty string",
  "pr_url": "required in pr mode",
  "incomplete": false,
  "incomplete_reason": "optional string"
}
```

Additional output requirements:
- In `pr` mode, write the final PR title to `{{OUTPUT_PR_TITLE_PATH}}` as plain text (single line).
- In `pr` mode, write the final PR body to `{{OUTPUT_PR_BODY_PATH}}` in markdown using this structure:

```markdown
## summary
- ...

## what changed and why
- ...

## round outcomes
- ...

## verification
- ...

## risks and follow-ups
- ...

## final status
- ...
```
