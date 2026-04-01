# Repository Guidelines

## Project Overview (deepreview)
- `deepreview` is a local CLI tool that performs deep code review runs against a remote Git branch.
- Canonical project/tool spelling is always `deepreview` (all lowercase, one word).
- User input may contain variants/typos; normalize intent to `deepreview` in code/docs/output.
- The tool runs Codex-powered review workflows in isolated worktrees and produces actionable review artifacts.

## Repository Ownership
- This repository belongs under the personal GitHub account `olliecrow`.
- Do not move it to a GitHub organization or a different personal account unless Ollie explicitly asks for that change.
- When docs, remotes, automation, releases, or publishing steps need the owning GitHub account, use `olliecrow`.

## Open-Source Transition Posture
- Treat this repository as open-source-ready now, even while private.
- Never commit secrets, credentials, tokens, private keys, sensitive data, or confidential internal details.
- Keep auth material in local environment/secret stores only.
- Assume docs, logs, and generated artifacts may become public; redact sensitive details by default.

## Product Constraints
- deepreview may adopt compatible patterns from prior internal experience, but committed deepreview artifacts must remain project-local and must not reference external inspiration project names.
- Managed workspace root is `~/deepreview`.
- Never operate in or mutate the user's own working checkout of a target repo.
- Orchestration should stay simple and fail-fast; do not add automatic retry/backoff loops by default.
- Review execution is iterative by rounds (`--max-rounds`, default `5`) with change-driven round progression.
- Do not push during intermediate rounds; perform exactly one final push at delivery.
- Cleanup should be aggressive for stale worktrees/transient artifacts once no longer needed.
- Detailed behavior/runtime constraints are canonical in `docs/spec.md`.
- End-to-end stage flow and isolation model are canonical in `docs/architecture.md`.

## Docs, Plans, and Decisions (agent usage)
- `docs/` is long-lived, agent-focused, committed, and evergreen.
- `plan/` is short-lived scratch space and is not committed.
- Decision capture policy lives in `docs/decisions.md`.
- Workflow conventions live in `docs/workflows.md`.
- Canonical behavior/runtime contract lives in `docs/spec.md`.
- Pipeline architecture details live in `docs/architecture.md`.
- Requirement traceability baseline lives in `docs/alignment.md`.

## README and Instructions Maintenance
- Keep `README.md` as the user/operator entrypoint.
- Keep `docs/spec.md` aligned with actual behavior and CLI contracts.
- Keep `docs/alignment.md` aligned with user-level requirement IDs and mappings.
- Keep `docs/workflows.md` aligned with execution and note-routing conventions.
- Keep non-obvious durable rationale in `docs/decisions.md`.

## Note Routing (agent usage)
- Active notes: `plan/current/notes.md`
- Workstream index: `plan/current/notes-index.md`
- Orchestration status: `plan/current/orchestrator-status.md`
- Sequential handoffs: `plan/handoffs/`

## Plan Directory Structure (agent usage)
- `plan/current/`
- `plan/backlog/`
- `plan/complete/`
- `plan/experiments/`
- `plan/artifacts/`
- `plan/scratch/`
- `plan/handoffs/`

## Operating Principles
- Prioritize correctness, clarity, pragmatism, and rigor.
- Think before coding: state assumptions, identify risks, and clarify ambiguity early.
- Keep solutions simple and surgical; avoid overengineering and hacky workarounds.
- Stay tightly in scope: change only what is required for the task.
- Be proactive and autonomous when confidence is high.
- Verify behavior with tests/checks, not assumptions.
- Capture durable rationale in the most local durable place.
- Commit in small logical increments when checks are green and repo policy permits.

## Git and Safety
- Never use destructive git operations unless explicitly requested.
- Do not rewrite history unless explicitly requested.
- Do not revert unrelated user changes.
- Keep worktree isolation strict when running review fanout.

## Dictation-Aware Input Handling
- The user often dictates prompts, so minor transcription errors and homophone substitutions are expected.
- Infer intent from local context and repository state; ask a concise clarification only when ambiguity changes execution risk.
- Keep explicit typo dictionaries at workspace level (do not duplicate repo-local typo maps).

## Third-Party Dependency Trust Policy
- Prefer official packages, libraries, SDKs, frameworks, and services from authoritative sources.
- Prefer options that are reputable, well-maintained, popular, and well-supported.
- Before adopting or upgrading third-party dependencies, verify ownership/publisher authenticity, maintenance activity, security history, license fit, and ecosystem adoption.
- Avoid low-trust, obscure, or weakly maintained dependencies when a stronger alternative exists.
- Pin versions and keep lockfiles current for reproducibility and supply-chain safety.
- If trust signals are unclear, do not adopt the dependency until explicitly approved.

<!-- third-party-policy:start -->
## Third-Party Repository Handling
- External repositories may be cloned for static analysis only.
- Clone them only into ephemeral `plan/` locations such as `plan/scratch/upstream/` or `plan/artifacts/external/`.
- Immediately sanitize clone metadata: prefer `rm -rf .git`; if `.git` is temporarily needed, remove all remotes first and then remove `.git`.
- Never execute third-party code (no scripts, tests, builds, package installs, binaries, or containers).
- Persistent remotes in this repo must reference only `github.com/olliecrow/*`.
<!-- third-party-policy:end -->

## Plain English Default
- Use plain English in chat, session replies, docs, notes, comments, reports, commit messages, issue text, and review text.
- Prefer short words, short sentences, and direct statements.
- If a technical term is needed for correctness, explain it in simple words the first time.
- In code, prefer clear descriptive names for files, folders, flags, config keys, functions, classes, types, variables, tests, and examples.
- Avoid vague names, short cryptic names, and cute internal code names unless an old established name is already clearer than changing it.
- When touching old code, rename confusing names if the change is low risk and clearly improves readability.
- Keep the durable why for this rule in `docs/decisions.md`.
