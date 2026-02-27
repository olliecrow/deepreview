You are in the deepreview post-delivery PR description enhancement stage.

## Scope
- This stage runs only after a PR has been created.
- Work in a fresh Codex context (do not resume prior threads).
- Do not modify repository code, docs, configs, commits, branches, or run artifacts.
- Only produce high-quality final PR title/body text and write to the output paths.

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
- Existing base PR title path: `{{BASE_PR_TITLE_PATH}}`
- Existing base PR body path: `{{BASE_PR_BODY_PATH}}`
- Output title path: `{{OUTPUT_TITLE_PATH}}`
- Output summary path: `{{OUTPUT_SUMMARY_PATH}}`

## Task
Generate a human-readable final PR title and PR description.

Context access:
- You are running inside the run root (`{{RUN_ROOT}}`) where all round artifacts and logs are available.
- Inspect round folders, artifacts, and logs directly to build your understanding.
- Use `{{MANAGED_REPO_PATH}}` to inspect repository diffs/history as needed.
- Do not rely on pre-digested injected summaries.

Requirements:
1. Keep tone direct and human. Prefer clear concise language over generic boilerplate.
2. Be concrete: describe what changed, why it changed, and what issue/problem motivated the changes.
3. Include verification evidence and explicit outcomes, not vague statements.
4. Make key risks/trade-offs explicit when relevant.
5. Do not include secrets, personal information, or private local machine paths.
6. Include detailed round-by-round outcomes and verification highlights, but do not dump raw worker reports or full artifact logs.
7. Keep outputs self-contained so humans can understand the PR without opening deepreview internals.
8. Avoid filler/hype. Every bullet should add real signal.

Output:
- Write plain text title (single line, no markdown) to `{{OUTPUT_TITLE_PATH}}`.
- Write markdown body to `{{OUTPUT_SUMMARY_PATH}}`.
- Body must use this structure:

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
