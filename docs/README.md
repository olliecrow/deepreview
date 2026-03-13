# Docs Directory

This directory holds long-term, agent-focused documentation for this repo. It is primarily for agent/runtime guidance, but can also be useful to contributors. It is committed to git.

Principles:
- Keep content evergreen and aligned with the codebase.
- Avoid time- or date-dependent language.
- Prefer updating existing docs when they have a clear home, but create focused docs/subdirectories when it improves organization and findability.
- Use docs for cross-cutting context or rationale that does not belong in code comments or tests.
- Keep entries concise and high-signal.
- Use relative links between related docs and keep indexes current.

Relationship to `/plan/`:
- `/plan/` is short-term, disposable scratch space for agents and is not committed.
- `/plan/handoffs/` is used for staged workflow handoffs when needed.
- Active notes should be routed into `/plan/current/` and promoted into `/docs/` only when they become durable guidance.
- `/docs/` is long-lived; only stable guidance should live here.

## Documentation Map
- [../README.md](../README.md): user-facing project overview, quickstart, CLI usage, and shell ergonomics.
- [spec.md](spec.md): canonical runtime/product contract and required invariants.
- [architecture.md](architecture.md): pipeline shape, isolation model, and delivery modes.
- [workflows.md](workflows.md): execution workflow, note routing, and orchestration conventions.
- [decisions.md](decisions.md): durable decision policy and recorded decisions.
- [alignment.md](alignment.md): requirements traceability baseline from user description to canonical contracts.
- [project-preferences.md](project-preferences.md): durable maintenance and collaboration preferences.
- [untrusted-third-party-repos.md](untrusted-third-party-repos.md): safety policy for external repository snapshots.
- [../prompts/README.md](../prompts/README.md): prompt-template pack and queue layout for review, execute, and delivery stages.

## Source-Of-Truth Layering
- Product/runtime invariants: `spec.md`
- Pipeline/stage structure: `architecture.md`
- Process and routing conventions: `workflows.md`
- Durable rationale/trade-offs: `decisions.md`
- Requirement-to-contract mapping: `alignment.md`
- Prompt-template contracts and review/execute/delivery stage templates: `../prompts/README.md`

## Active Scratch Pointers
- Use `../plan/current/` for active scratch artifacts (for example `notes.md`, `open-questions.md`, or task-specific files).
- Promote resolved, durable guidance from scratch artifacts into docs and delete stale scratch files.
