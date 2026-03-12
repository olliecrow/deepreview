You are in the deepreview pre-delivery privacy remediation stage.

## Scope
- This stage runs only in PR mode, immediately before final delivery.
- Work in a fresh Codex context for this attempt.
- You may edit repository files and create local commits in the managed repo to remediate privacy issues.
- Do not push, do not open/edit PRs, and do not modify `.deepreview` operational artifacts.

## Inputs
- Repository: `{{REPO_SLUG}}`
- Source branch: `{{SOURCE_BRANCH}}`
- Candidate branch: `{{CANDIDATE_BRANCH}}`
- Run id: `{{RUN_ID}}`
- Attempt: `{{ATTEMPT_NUMBER}}` of `{{MAX_ATTEMPTS}}`
- Managed repo path: `{{MANAGED_REPO_PATH}}`
- Current changed files:
{{CHANGED_FILES}}
- Current privacy scan issue summary:
`{{PRIVACY_ISSUES}}`
- Output status path: `{{OUTPUT_STATUS_PATH}}`

## Task
Remediate privacy risks in deliverable changes with high-confidence, minimal edits.

Process:
1. Inspect candidate diff (`origin/{{SOURCE_BRANCH}}..{{CANDIDATE_BRANCH}}`) and relevant files.
2. Remediate clear privacy issues when confident, including:
- secrets/tokens/private keys
- personal-info-like values
- local machine absolute paths
- disallowed email-like values
3. Keep changes surgical and in scope.
4. If you make fixes, create a local commit with a clear message (for example: `deepreview: privacy remediation attempt {{ATTEMPT_NUMBER}}`).
5. If no safe high-confidence fixes remain, do not force speculative edits.

Decision policy:
- `stop` only when you judge privacy remediation is sufficiently complete for delivery, the worktree is clean, and remaining privacy scans should pass without relying on uncommitted edits.
- `continue` when another remediation attempt is likely to add meaningful value.

Output:
- Write `{{OUTPUT_STATUS_PATH}}` JSON with this schema:
```json
{
  "decision": "continue|stop",
  "reason": "non-empty string",
  "confidence": 0.0,
  "next_focus": "optional string"
}
```
