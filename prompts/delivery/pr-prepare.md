You are in the deepreview pre-delivery PR preparation stage.

## Scope
- This stage runs only in PR mode, before privacy remediation and before any push/PR actions.
- Work in a fresh Codex context for this stage.
- You may inspect and edit repository files, git history, and local commits on the candidate branch to prepare a clean PR.
- Do not push, do not open/edit PRs, and do not modify `.deepreview` operational artifacts.
- Preserve useful commit history. Do not squash commits just to tidy history.
- If earlier commit messages or commits must be corrected for privacy/security or obvious branch hygiene reasons, you may rewrite local candidate history before delivery.

## Inputs
- Repository: `{{REPO_SLUG}}`
- Source branch: `{{SOURCE_BRANCH}}`
- Candidate branch: `{{CANDIDATE_BRANCH}}`
- Run id: `{{RUN_ID}}`
- Managed repo path: `{{MANAGED_REPO_PATH}}`
- Current changed files:
{{CHANGED_FILES}}

## Task
Prepare the candidate branch for PR delivery with high-confidence, minimal changes.

Process:
1. Inspect the candidate diff (`origin/{{SOURCE_BRANCH}}..{{CANDIDATE_BRANCH}}`) and current branch state.
2. Make only no-regret preparation changes that clearly improve delivery readiness, for example:
- remove obviously accidental or oversized files
- fix obviously wrong generated or temporary artifacts
- make small final cleanup edits needed for a clean PR
- correct commit messages/history when privacy/security or obvious delivery hygiene requires it
3. Do not make speculative refactors or broaden scope.
4. If no changes are needed, leave the branch unchanged.
5. Prefer leaving the worktree clean. If you intentionally leave simple uncommitted edits, deepreview may auto-commit them after this stage.

Output requirements:
- Do not write separate summary/status files in this stage.
- Keep terminal/log output concise and factual.
