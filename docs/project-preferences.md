# Project Preferences (Going Forward)

These preferences define how `deepreview` should be maintained as an open-source-ready project.

## Quality and Scope

- Keep behavior deterministic and bounded across review rounds.
- Prefer clear, testable pipeline rules over implicit behavior.
- Keep changes small and easy to reason about, especially around delivery logic.
- Support only macOS and Linux hosts; do not preserve Windows compatibility code or documentation.

## Security and Confidentiality

- Never commit secrets, credentials, tokens, API keys, or private key material.
- Never commit private/sensitive machine paths; use generic placeholders such as `/path/to/project`.
- Keep local runtime state untracked (`.env*`, `.claude/`, `.codex/`, run artifacts, temp files).
- If sensitive data is found in history, rotate credentials and scrub history before publication.

## Documentation Expectations

- Keep `README.md`, `AGENTS.md`, and `docs/` aligned with real behavior.
- Keep prompt/runtime docs synchronized with execution stages and delivery semantics.

## Verification Expectations

- Validate touched stages with focused checks before merge.
- Run `deepreview doctor` and `deepreview dry-run` as first-line checks before full review runs.
- Include clear verification evidence in PRs/issues when practical.

## Collaboration Preferences

- Preserve accurate author/committer attribution for each contributor.
- Prefer commit author identities tied to genuine human GitHub accounts, not fabricated bot names/emails.
- Avoid destructive history rewrites unless required for secret/confidentiality remediation.
