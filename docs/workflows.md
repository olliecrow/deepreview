# Operating Workflow

This document defines how work is tracked so progress compounds without context bloat.

## Core mode
- Keep active, disposable notes in `/plan/current/`.
- Promote durable guidance into `/docs/`.
- Capture important rationale in the smallest durable place (code comments, tests, or docs).
- Keep the workflow spartan: short notes, clear routing, minimal ceremony.

## Note routing
- `/plan/current/notes.md`: running task notes, key findings, and next actions.
- `/plan/current/notes-index.md`: compact index of active workstreams and pointers to detailed notes.
- `/plan/current/orchestrator-status.md`: packet/status board for parallel or subagent work.
- `/plan/handoffs/`: sequential handoff summaries for staged automation workflows.

## deepreview delivery loop
- For non-trivial changes, run: `investigate -> plan -> implement -> verify -> battletest -> organise-docs -> git-commit -> re-review`.
- Prefer repeated small loops over large one-shot drops.
- Keep defaults safe: avoid direct push behavior unless explicitly in `yolo` mode.
- Before widening scope, ensure existing plan items are verified and documented.

## Round-loop operating rules
- Execute deepreview as bounded rounds (`max_rounds`, default `5`) with round-loop control driven by Codex status decisions plus repository change detection.
- Use fresh review/execute/delivery worktrees each round/stage; do not reuse stale mutable worktrees.
- Use one shared independent-review prompt template, one two-prompt execute queue in a shared execute context, and one fresh delivery prompt context.
- Independent reviewers must never share chat history.
- Reset both the worktree and Codex context on mutable-stage retries.
- Keep orchestration simple: avoid unbounded retries; only bounded inactivity restarts are allowed.
- Encourage local commits throughout rounds; never push during intermediate rounds.
- Pushes remain forbidden during intermediate rounds. Delivery may push multiple times if the merge-ready loop needs a high-confidence fix-and-retry cycle.
- In PR mode, let the delivery prompt own final branch/PR readiness, including local checks, push/PR actions, remote-check waiting, and concise PR title/body upkeep.
- Aggressively clean stale worktrees/transient artifacts after each round.

## Parallel and subagent workflows
- Use isolated worktrees or dedicated working directories when streams are independent.
- Track each stream with owner, scope, status, blocker, and last update.
- Require each stream to produce a concise handoff summary before merge.

## Durable vs ephemeral routing
- Durable contracts and architecture belong in `/docs/` (`spec.md`, `architecture.md`, `decisions.md`, `workflows.md`).
- Unresolved design questions and iterative planning belong in `/plan/current/` (`open-questions.md`, `notes.md`, and task-specific scratch files).

## Doc update routing
- Update `README.md` for user-facing onboarding and CLI ergonomics (for example quickstart, help usage, optional shell alias guidance).
- Update `docs/spec.md` when runtime/product invariants change.
- Update `docs/architecture.md` when stage flow, workspace model, or delivery flow changes.
- Update `docs/decisions.md` when a non-obvious trade-off is decided.
- Update `docs/alignment.md` when user-level requirements change or new requirement IDs are introduced.
- Update `plan/current/open-questions.md` for unresolved items; do not place unresolved decisions in durable docs.

## Alignment tracking
- Use `docs/alignment.md` requirement IDs in planning and implementation artifacts.
- Maintain `plan/current/alignment-status.md` with evidence for planned/implemented/executed/verified states.
- Use `plan/current/alignment-checklist.md` at each milestone and before phase-close decisions.
- Before marking a phase complete, confirm touched requirement IDs have corresponding evidence updates.

## Promotion cycle
- During execution: write concise notes to `/plan/current/`.
- At meaningful milestones: consolidate and de-duplicate active notes.
- Before finishing: promote durable learnings to `/docs/` and trim stale `/plan/` artifacts.

## Checkpoint cadence
- Run docs consolidation frequently during long tasks.
- Create small logical commits at verified milestones.
- Do not let large uncommitted diffs accumulate.

## Stop conditions
- Stop when acceptance checks pass, risks are documented, and no unresolved blockers remain.
- If no new evidence appears, avoid repeating the same loop; report completion instead.
