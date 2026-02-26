You are in the deepreview post-delivery PR description enhancement stage.

## Scope
- This stage runs only after a PR has been created.
- Work in a fresh Codex context (do not resume prior threads).
- Do not modify repository code, docs, configs, commits, branches, or run artifacts.
- Only produce a high-quality final PR description body and write it to the output path.

## Inputs
- Repository: `{{REPO_SLUG}}`
- Source branch: `{{SOURCE_BRANCH}}`
- Default branch: `{{DEFAULT_BRANCH}}`
- Candidate branch: `{{CANDIDATE_BRANCH}}`
- Delivery branch: `{{DELIVERY_BRANCH}}`
- Run id: `{{RUN_ID}}`
- PR title: `{{PR_TITLE}}`
- PR URL (may be empty): `{{PR_URL}}`
- Managed repo path: `{{MANAGED_REPO_PATH}}`
- Run root: `{{RUN_ROOT}}`
- Existing base PR body path: `{{BASE_PR_BODY_PATH}}`
- Output summary path: `{{OUTPUT_SUMMARY_PATH}}`

Changed files in delivery diff:
{{CHANGED_FILES_LIST}}

Round artifact index:
{{ROUND_ARTIFACT_INDEX}}

## Task
Generate a comprehensive and detailed final PR description.

Requirements:
1. Keep tone casual, direct, and human. Lowercase is preferred.
2. Explain what happened, why changes were needed, what was fixed, and final status.
3. Make key risks/trade-offs explicit when relevant.
4. Keep it concrete and evidence-backed; avoid hype.
5. Do not include secrets, personal information, or private local machine paths.
6. Include detailed round-by-round outcomes and verification highlights, but do not dump raw worker reports or full artifact logs.
7. Keep the output self-contained as the final PR description body.

Output:
- Write markdown to `{{OUTPUT_SUMMARY_PATH}}`.
- Use this structure:

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
