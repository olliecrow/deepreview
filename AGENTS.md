# deepreview repository guidance

## Purpose and layout

- `deepreview` is a Go CLI that reviews a remote branch with independent Codex workers, applies validated material fixes, and delivers one final result.
- `cmd/deepreview/` contains the CLI, `internal/deepreview/` contains orchestration and tests, and `prompts/` contains runtime prompt templates.
- `README.md` is the operator guide. `docs/spec.md` is the behavior contract, `docs/architecture.md` defines pipeline and isolation boundaries, and `docs/decisions.md` records durable cross-cutting rationale.

## Product invariants

- Run review and mutation only in branch-scoped workspaces under the configured deepreview root; never mutate the operator's checkout.
- Keep review workers independent and require the configured worker coverage before execute begins.
- Treat findings as evidence. Execute only independently validated, high-confidence, material work.
- A `continue` decision requires another round. A `stop` with repository changes requires those changes to be reviewed in another round. A `stop` with no repository changes ends the loop.
- Never push during review or execute rounds. Delivery publishes the reviewed candidate branch once: PR mode by default, direct source-branch push only in explicit yolo mode.
- Delivery may not mutate tracked content or branch history. Keep public delivery text privacy-guarded while retaining literal local diagnostics.
- Keep candidate history forward-only. Never rebase, reset, squash, amend, rebuild, filter, or force-push it; unsafe outgoing history blocks delivery.
- Preserve bounded worker inactivity recovery, interrupt cleanup, synthetic test fixtures, and the macOS/Linux-only support boundary.

## Development

- Format Go changes with `gofmt`.
- Run focused tests while iterating, then `go test ./...`, `go test -race ./...`, and `go vet ./...` for material orchestration changes.
- Update prompts, `docs/spec.md`, `docs/architecture.md`, CLI help, and README together when their shared runtime contract changes.
- Keep temporary plans, logs, and generated review artifacts in ignored `plan/` or the configured runtime workspace.
