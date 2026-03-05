# Decision Capture Policy

This document defines how to record fixes and important decisions so future work does not re-litigate the same questions. It is written to stay accurate over time; avoid time-specific language.

## When to record
- Any fix for a confirmed bug, regression, or safety issue.
- Any deliberate behavior choice that differs from intuitive defaults.
- Any trade-off decision that affects modeling or behavior.
- Any change that affects external behavior, invariants, or public APIs.

## Where to record
Use the smallest, most local place that makes the decision obvious:
- **Code comments** near the behavior when the rationale is not obvious.
- **Tests** with names/assertions that encode the invariant.
- **Docs** (this file or another focused doc) when the decision is cross-cutting.

Prefer updating an existing note over creating a new file.

## What to record
Keep entries short and focused:
- **Decision**: what was chosen.
- **Context**: what problem or risk it addresses.
- **Rationale**: why this choice was made.
- **Trade-offs**: what we are not doing.
- **Enforcement**: which tests or code paths lock it in.
- **References** (optional): file paths, tests, or PRs that embody the decision.

## Template
```
Decision:
Context:
Rationale:
Trade-offs:
Enforcement:
References:
```

## Recorded decisions

Decision:
Use `deepreview` as the canonical project and CLI spelling (lowercase, one word) in code/docs/output.
Context:
User input may contain variants while dictating, but canonical output must be stable.
Rationale:
Consistent naming prevents drift across docs, CLI contracts, and future public artifacts.
Trade-offs:
Requires normalization logic in CLI parsing and prompt rendering.
Enforcement:
Naming rule documented in `AGENTS.md` and `docs/spec.md`; future tests should validate normalized output.
References:
`AGENTS.md`, `docs/spec.md`, `README.md`

Decision:
Adopt open-source-ready security hygiene immediately, despite current private repo visibility.
Context:
The project is intended to become open source.
Rationale:
Early hygiene avoids future secret scrubbing and reduces leakage risk.
Trade-offs:
Adds friction for examples/logging because sensitive data must be redacted or excluded.
Enforcement:
Security posture documented in README/AGENTS/spec; future pre-commit and runtime checks should scan for secrets.
References:
`README.md`, `AGENTS.md`, `docs/spec.md`

Decision:
Run reviews only in deepreview-managed checkouts under `~/deepreview`, never in user working copies.
Context:
User explicitly requires no interruption/mutation of their local project checkout.
Rationale:
Hard isolation reduces accidental data loss and workflow interference.
Trade-offs:
Requires managed clone/worktree lifecycle logic and extra disk usage.
Enforcement:
Workspace invariant documented in `AGENTS.md`, `docs/spec.md`, and `docs/architecture.md`; implementation must route all git operations through managed paths.
References:
`AGENTS.md`, `docs/spec.md`, `docs/architecture.md`

Decision:
Use concurrent independent reviews (default 4, configurable) with full-worker completion, plus bounded inactivity restarts to recover stalled workers.
Context:
deepreview quality depends on independent concurrent perspectives.
Rationale:
Parallel independent reviews increase coverage and reduce single-run blind spots. Bounded inactivity restarts prevent single-worker stalls from deadlocking the pipeline while preserving full coverage.
Trade-offs:
Higher local compute usage and more orchestration complexity due to activity monitoring and restart handling.
Enforcement:
Independent-review contract documented in spec/architecture/README; implementation and tests must assert configurable concurrency, inactivity policy, bounded restarts, and one artifact per worker.
References:
`README.md`, `docs/spec.md`, `docs/architecture.md`

Decision:
After independent review completion, run execute in a fresh worktree from the current candidate branch head (round-1 from latest remote source head, subsequent rounds from latest local candidate commit), then apply and verify selected changes.
Context:
Findings must be consolidated before acting; execute must not use stale branch state and multi-round execution needs local candidate continuity.
Rationale:
Fresh execute per round minimizes drift while preserving iterative local progress between rounds.
Trade-offs:
Adds another Codex phase per round and longer total wall-clock time.
Enforcement:
Execute stage specified in architecture/spec; implementation must start round-1 from latest remote source head and subsequent rounds from latest local candidate head.
References:
`docs/spec.md`, `docs/architecture.md`

Decision:
Default delivery mode opens a PR into the original source branch from a new branch; direct push is opt-in via `yolo` mode.
Context:
User wants safe default behavior with optional fast-path override.
Rationale:
PR default protects source branch while enabling explicit high-speed operation when chosen.
Trade-offs:
PR mode is slower than direct push.
Enforcement:
Mode contract documented in README/spec/AGENTS/architecture; implementation must forbid default direct commits to source branch.
References:
`README.md`, `AGENTS.md`, `docs/spec.md`, `docs/architecture.md`

Decision:
Use local Codex CLI authentication/subscription as the primary auth model; do not require repo-stored API keys.
Context:
User requires using existing local Codex session.
Rationale:
Reduces secret-management burden and aligns with operator workflow.
Trade-offs:
Runtime depends on local machine Codex CLI session health.
Enforcement:
Auth model documented in README/spec/AGENTS/architecture; implementation should fail clearly if local Codex session is unavailable.
References:
`README.md`, `AGENTS.md`, `docs/spec.md`, `docs/architecture.md`

Decision:
Keep durable contracts in `docs/spec.md` and `docs/architecture.md`; keep unresolved implementation questions in `plan/current/` scratch artifacts.
Context:
Open design questions and iterative choices evolve quickly and create churn when stored in durable docs.
Rationale:
Separating durable vs ephemeral content keeps docs stable and keeps planning churn in disposable notes.
Trade-offs:
Requires deliberate promotion of finalized decisions from `plan/` into `docs/`.
Enforcement:
`docs/spec.md` contains only invariant/runtime contracts and references architecture; unresolved questions are tracked in `plan/current/open-questions.md`.
References:
`docs/spec.md`, `docs/architecture.md`, `plan/current/open-questions.md`

Decision:
Adopt compatible workflow patterns without cross-project references in deepreview artifacts.
Context:
The project may borrow principles from prior work, but repositories are intentionally not linked.
Rationale:
Avoids coupling, branding confusion, and accidental leakage of irrelevant project context into open-source-facing artifacts.
Trade-offs:
Requires rephrasing inherited concepts into deepreview-native terminology.
Enforcement:
AGENTS/spec explicitly prohibit external inspiration project references in committed deepreview code/docs.
References:
`AGENTS.md`, `docs/spec.md`

Decision:
Track alignment to user-level requirements using requirement IDs plus lifecycle evidence states (`planned`, `implemented`, `executed`, `verified`).
Context:
The project requires continuous proof that planning, implementation, execution, and verification remain aligned with the original description.
Rationale:
An explicit traceability baseline in durable docs plus a live scratch evidence table reduces drift and makes gaps visible before phase completion.
Trade-offs:
Adds lightweight maintenance overhead to keep requirement mappings and evidence rows current.
Enforcement:
`docs/alignment.md` defines canonical requirement IDs and mappings; `docs/workflows.md` requires updating `plan/current/alignment-status.md` before closing phases.
References:
`docs/alignment.md`, `docs/workflows.md`, `plan/current/alignment-status.md`

Decision:
Keep orchestration simple and fail-fast, with one narrow self-healing exception: bounded inactivity restarts for stalled Codex workers.
Context:
The initial user/operator model is small-scale and prioritizes clarity over production hardening.
Rationale:
A straightforward control flow reduces complexity and makes behavior easier to reason about while still protecting runs from single-worker stalls.
Trade-offs:
Transient non-stall failures still require manual reruns; stall recovery adds targeted orchestration complexity.
Enforcement:
Spec and architecture define bounded inactivity restart behavior with explicit caps; run failures outside that scope terminate with clear diagnostics.
References:
`docs/spec.md`, `docs/architecture.md`

Decision:
Run iterative deepreview rounds with default `max_rounds=5`, using change-driven progression and a required round-status artifact for traceability.
Context:
One review/execute pass may miss issues; iterative passes improve confidence before final delivery.
Rationale:
Bounded rounds provide extra review depth while preventing unbounded loops, while change-driven progression keeps control flow deterministic.
Trade-offs:
Longer wall-clock runtime compared with single-pass flows.
Enforcement:
Runtime contract includes `--max-rounds`; architecture/spec require change-driven round-loop control and a validated round-status artifact per execute pass.
References:
`docs/spec.md`, `docs/architecture.md`

Decision:
Push exactly once at final delivery, regardless of mode; never push intermediate-round commits, and only deliver after codex final green-light.
Context:
User requires that intermediate iteration remains local until final confidence is reached.
Rationale:
Single final push reduces remote churn and keeps iterative experimentation private until finalized; codex final green-light ensures delivery only happens after the run is judged complete/successful.
Trade-offs:
Remote visibility of intermediate progress is intentionally reduced.
Enforcement:
Spec/architecture require one final push point, forbid intermediate pushes, and gate final delivery on codex final stop/success signal.
References:
`docs/spec.md`, `docs/architecture.md`

Decision:
Aggressively clean stale worktrees and transient round artifacts as soon as they are no longer needed.
Context:
Round-based review/execute creates many temporary resources that should not accumulate.
Rationale:
Aggressive cleanup keeps workspace state simple and reduces stale-context risks.
Trade-offs:
Less retained transient context for postmortems unless captured in final summaries.
Enforcement:
Spec cleanup policy and architecture round stages require immediate post-stage cleanup.
References:
`docs/spec.md`, `docs/architecture.md`

Decision:
Support both `--mode yolo` and `--yolo` as equivalent yolo-mode selectors.
Context:
User requested one explicit mode concept without extra confirmation flags.
Rationale:
Alias support keeps CLI simple while preserving explicit intent.
Trade-offs:
Slightly larger CLI parser surface.
Enforcement:
Runtime contract documents both forms; parser tests must assert equivalent behavior.
References:
`docs/spec.md`, `plan/current/spec.md`

Decision:
Use delivery naming prefixes `deepreview/` for branch names and `deepreview:` for PR titles.
Context:
Consistent naming was requested for delivery artifacts.
Rationale:
Predictable prefixes simplify discovery/filtering of deepreview-generated outputs.
Trade-offs:
Reduced flexibility for ad hoc naming styles.
Enforcement:
Delivery naming contract in spec; delivery-mode tests must assert prefixes.
References:
`docs/spec.md`, `plan/current/spec.md`

Decision:
If an execute round produces no changes, stop additional rounds.
Context:
Round loops should stay bounded and purposeful; no-change rounds often indicate convergence.
Rationale:
Stopping on no-change reduces unnecessary cycles once the candidate branch converges.
Trade-offs:
May stop earlier than a human might prefer in edge cases where another round could still discover changes.
Enforcement:
Round loop logic checks candidate-branch diffs before/after execute; no-change outcome ends the loop.
References:
`docs/spec.md`, `docs/architecture.md`

Decision:
When TUI mode is enabled, deepreview exits immediately on completion, clears the terminal, and then prints the normal completion summary in terminal text.
Context:
Holding the completed frame required an extra keypress and caused confusing overlap artifacts between the final TUI frame and summary output in some terminals.
Rationale:
Immediate exit removes unnecessary interaction, and clearing the terminal ensures the summary starts in a clean, readable state without visual overlap from prior TUI repaint output.
Trade-offs:
Users no longer pause on a static final frame in TUI mode; completion context now relies on the text summary and artifacts.
Enforcement:
TUI update loop exits on worker completion without waiting for keypress; CLI clears terminal before printing completion summary after TUI runs; tests assert worker-completion auto-quit and done-state hint text.
References:
`internal/deepreview/tui.go`, `internal/deepreview/cli.go`, `internal/deepreview/tui_test.go`, `internal/deepreview/cli_test.go`, `docs/spec.md`, `docs/architecture.md`

Decision:
Apply local readiness checks to explicit `--source-branch` runs when the explicit branch matches the current local branch context.
Context:
deepreview reviews remote branch state; if local branch is dirty or diverged and the same branch is targeted explicitly, reviews can miss newest local work.
Rationale:
Explicit branch selection should not bypass local safety checks for the same active local branch.
Trade-offs:
Adds preflight strictness for explicit-branch invocations; explicit branches that do not match the current local branch are not blocked by current-branch readiness.
Enforcement:
`inferRepoAndBranch` now runs local tracked-change and local/upstream sync checks for explicit matching-branch context; tests cover explicit tracked-change and ahead-of-remote rejection.
References:
`internal/deepreview/local_context.go`, `internal/deepreview/local_context_test.go`, `docs/spec.md`

Decision:
Execute prompts must apply a high-conviction consolidation workflow: reviews are inputs, not gospel; only independently validated items move forward.
Context:
Independent review workers can disagree or contain false positives; execution quality depends on careful consolidation before changes.
Rationale:
Treating review artifacts as signals (not instructions) reduces low-confidence churn and keeps implementation focused on serious, evidence-backed issues.
Trade-offs:
Adds upfront consolidation effort per round and may defer some plausible-but-uncertain items.
Enforcement:
Prompt templates require per-item accept/reject/defer with evidence, commonality tracking across reviewers, and explicit deferral of low-confidence items.
References:
`prompts/execute/01-consolidate-reviews.md`, `prompts/execute/02-plan.md`, `docs/spec.md`, `docs/architecture.md`

Decision:
Independent review and execute consolidation are strict: only high-confidence `critical|high` merge-relevant issues are in scope; low/medium severity or optional improvements are out of scope for this workflow.
Context:
Long runs can accrue non-blocking cleanup/perf suggestions that still produce file changes and force additional review rounds without materially improving merge safety.
Rationale:
Keeping scope strictly on critical/high, high-confidence items preserves review rigor, limits churn, and reduces unnecessary rounds while maintaining strong isolation and verification standards.
Trade-offs:
Some useful but non-critical cleanups/perf improvements are deferred to separate scoped runs.
Enforcement:
Independent-review template excludes optional/non-blocking sections; execute triage accepts only `critical|high` items with high confidence and rejects/defers low/medium severity work.
References:
`prompts/review/independent-review.md`, `prompts/execute/01-consolidate-reviews.md`, `docs/spec.md`, `prompts/README.md`

Decision:
Validate execute triage artifacts in orchestrator: any `accept` disposition must carry severity `critical|high` and confidence `high`, or the round fails before commit/delivery.
Context:
Prompt contracts can drift in output shape/quality; without runtime guards, low/medium or low-confidence accepts can still pass through and create unnecessary churn.
Rationale:
A lightweight validator preserves Codex discretion on what to accept while enforcing the policy boundary that accepted work must be critical/high and high-confidence.
Trade-offs:
If triage output is malformed or omits tags, runs fail fast and require prompt/output correction.
Enforcement:
Execute stage validates canonical `round-triage.md` before round commit/status handling and fails with explicit diagnostics on violations.
References:
`internal/deepreview/orchestrator.go`, `internal/deepreview/orchestrator_test.go`, `docs/spec.md`, `prompts/execute/01-consolidate-reviews.md`

Decision:
Prompt templates for machine-validated artifacts should include explicit output schemas and concrete examples.
Context:
Codex output quality improves when formatting constraints are concrete; vague "include X" guidance leads to occasional drift.
Rationale:
Providing strict shape examples increases consistency, reduces parser/validator failures, and keeps runs autonomous.
Trade-offs:
Slightly longer prompt templates and tighter formatting expectations.
Enforcement:
Execute and review prompts include explicit markdown/json shape examples for triage, verification, and summary artifacts.
References:
`prompts/execute/01-consolidate-reviews.md`, `prompts/execute/03-execute-verify.md`, `prompts/execute/04-cleanup-summary-commit.md`, `prompts/review/independent-review.md`, `prompts/README.md`

Decision:
Encourage local commits throughout execution; require changed work to be committed locally before round completion, with no empty commits.
Context:
Round-based progression should preserve progress safely while avoiding remote churn until final delivery.
Rationale:
Allowing local checkpoint commits improves recoverability and encourages incremental progress while still keeping pushes constrained to the final delivery step.
Trade-offs:
Potentially noisier local history than strict single-commit-per-round policy.
Enforcement:
Spec/architecture/prompt contracts allow local commits during rounds and require no empty commits.
References:
`docs/spec.md`, `docs/architecture.md`

Decision:
Prefer full-viewport TUI usage with explicit overflow signaling.
Context:
The progress TUI previously reserved wide/right and bottom gutters and silently dropped/truncated some rendered elements.
Rationale:
Using available viewport space improves readability, while explicit `+N more` and truncation markers reduce ambiguity about hidden content.
Trade-offs:
Slightly denser rendering at large terminal sizes; one-column anti-wrap safety margin is still retained.
Enforcement:
- TUI width/height targeting minimizes reserved gutter space and fills the available frame height.
- Header chip overflow renders an explicit hidden-count hint.
- ANSI-aware width clamping uses visible truncation markers when width permits.
References:
`internal/deepreview/tui.go`, `internal/deepreview/tui_test.go`

Decision:
Use local candidate branch naming `deepreview/candidate/<source-branch>/<run-id>`.
Context:
Multi-round local progression needs deterministic branch naming distinct from delivery branches.
Rationale:
Explicit candidate naming clarifies that the branch is intermediate and local to one run lifecycle.
Trade-offs:
Longer branch names.
Enforcement:
Workspace/runtime branch creation uses this prefix template for candidate branches.
References:
`docs/spec.md`, `docs/architecture.md`

Decision:
Final PR metadata must be Codex-generated and human-readable: concise PR title plus structured PR description sections (summary, what changed and why, round outcomes, verification, risks/follow-ups, final status).
Context:
Final PR output should consistently communicate what changed, why it changed, what was verified, and what risks remain without requiring readers to parse raw artifact dumps.
Rationale:
A fixed title/body contract improves readability and keeps reporting quality stable across runs.
Trade-offs:
Relies on Codex quality and may require prompt tuning if title/body quality drifts.
Enforcement:
The delivery prompt template defines required title/body outputs and section structure; integration tests assert title artifacts and key body section presence.
References:
`docs/spec.md`, `prompts/delivery/pr-description-summary.md`, `internal/deepreview/integration_test.go`

Decision:
In PR mode, run one fresh post-delivery Codex call to generate final PR title + description body and replace both via `gh pr edit`.
Context:
Large deterministic artifact-heavy PR bodies can exceed GitHub limits and cause `gh pr create` failures; users also need clearer human-readable PR metadata than static generic titles.
Rationale:
Using one Codex-generated final title/body pair keeps PRs readable, improves scannability, reduces size pressure, and avoids exposing unnecessary internal artifact detail.
Trade-offs:
Raw artifact detail is not embedded in final PR body and must be read from run artifacts when needed.
Enforcement:
Delivery flow creates PR with base title/body, runs dedicated delivery metadata template in a fresh Codex context, provides path-level context (run root + managed repo path) without injected digest blocks, writes final `pr-title.txt`/`pr-body.md` from generated output, and updates PR title/body via `gh pr edit`.
References:
`internal/deepreview/orchestrator.go`, `prompts/delivery/pr-description-summary.md`, `docs/spec.md`, `docs/architecture.md`

Decision:
Use codex-led verification by default: codex should attempt available tests, pre-commit checks, and locally runnable CI-like checks, then report what ran and outcomes.
Context:
The project favors codex autonomy and a minimal CLI surface, while still requiring explicit verification evidence.
Rationale:
Codex-driven verification keeps orchestration simple and adaptable across repositories with different local check setups.
Trade-offs:
Verification breadth can vary by repository and codex judgment quality.
Enforcement:
Spec/architecture require codex-led verification attempts and explicit verification evidence in final summaries/PR bodies.
References:
`docs/spec.md`, `docs/architecture.md`

Decision:
Use strict round stop-flag schema with enum decision values at deterministic run-artifact paths.
Context:
Codex early-stop control should be robust but simple, with low chance of malformed control signals.
Rationale:
A small required schema with enum decisions (`continue|stop`) is easy to validate and reduces orchestration ambiguity.
Trade-offs:
Slightly more strict parsing/validation logic.
Enforcement:
Spec defines path/schema and requires failure on invalid/missing required fields.
References:
`docs/spec.md`, `docs/architecture.md`

Decision:
Keep CLI parameter surface minimal and rely on strong defaults, while leaving verification strategy primarily to codex.
Context:
The project targets a small operator set and values simplicity over broad configurability.
Rationale:
Fewer knobs reduce complexity and make the tool easier to run consistently.
Trade-offs:
Reduced manual tuning flexibility.
Enforcement:
Runtime contract documents a small option set; verification guidance is codex-led with evidence reporting requirements.
References:
`docs/spec.md`, `docs/architecture.md`

Decision:
Use file-based unversioned prompt templates.
Context:
Prompt iteration is expected to be fast during early project development.
Rationale:
Unversioned file templates minimize process overhead while keeping prompts editable and explicit.
Trade-offs:
Less explicit historical prompt version tracking.
Enforcement:
Prompt-template contract in spec and phase-5 plan tasks require file-based template loading.
References:
`docs/spec.md`, `plan/current/spec.md`

Decision:
Treat prompt `{{...}}` markers as strict template variables and fail fast if any remain unresolved at render time.
Context:
Prompt templates include runtime fields (for example round id, artifact paths, and injected review content) that must be concrete before execution.
Rationale:
Fail-fast unresolved-variable handling avoids ambiguous prompt execution and makes wiring mistakes obvious.
Trade-offs:
Runs can fail earlier when template wiring is incomplete.
Enforcement:
Spec prompt-template contract requires deterministic template-variable rendering and immediate failure on unresolved variables.
References:
`docs/spec.md`, `prompts/README.md`

Decision:
Use one shared independent-review prompt template for all independent reviewers.
Context:
The independent review stage should stay simple while still running concurrent independent review passes.
Rationale:
A single shared independent review prompt reduces prompt-management overhead and keeps reviewer instructions consistent across workers.
Trade-offs:
Less per-worker prompt specialization.
Enforcement:
Spec/architecture require one shared independent-review template; prompt artifacts define a single independent-review template file.
References:
`docs/spec.md`, `docs/architecture.md`, `prompts/review/independent-review.md`

Decision:
Run execute as an ordered prompt queue in one Codex chat context per round, with independent review content injected into prompt context.
Context:
Execute work needs staged reasoning (triage, planning, execution/verification, wrap-up) while preserving intra-round continuity.
Rationale:
Sequential prompts in one context preserve reasoning continuity and still enforce explicit stage boundaries.
Trade-offs:
Longer single-session context may become large on very big changesets.
Enforcement:
Spec/architecture define same-context ordered execute queue; queue and stage templates are committed in `prompts/execute/`.
References:
`docs/spec.md`, `docs/architecture.md`, `prompts/execute/queue.txt`, `prompts/execute/01-consolidate-reviews.md`, `prompts/execute/02-plan.md`, `prompts/execute/03-execute-verify.md`, `prompts/execute/04-cleanup-summary-commit.md`

Decision:
Use Go as the primary implementation language for the deepreview runtime and TUI.
Context:
The tool is CLI/TUI-heavy, runs concurrent subprocess orchestration, and should be easy to distribute as a single binary.
Rationale:
Go provides strong fit for this shape: simple static binaries, reliable concurrency primitives, and mature terminal UI libraries.
Trade-offs:
Initial rewrite/migration cost and short-term delivery slowdown while stabilizing the Go implementation.
Enforcement:
Primary entrypoint is implemented in `cmd/deepreview` and runtime code is in `internal/deepreview`; Go tests cover parser, template, status validation, and integration flows.
References:
`cmd/deepreview/main.go`, `internal/deepreview/`, `internal/deepreview/integration_test.go`

Decision:
Keep `README.md` explicitly user-facing: purpose, requirements, quickstart, CLI usage/help, managed directories, and practical operator ergonomics.
Context:
The project needs one clear onboarding and usage entrypoint for operators; internal architecture/policy details are better kept in `/docs`.
Rationale:
A focused README improves discoverability and reduces confusion between user instructions and internal implementation guidance.
Trade-offs:
Some internal rationale must be duplicated minimally as links/pointers instead of full detail in README.
Enforcement:
README updates should prioritize user actions and outcomes; cross-cutting rationale and contracts stay in `docs/spec.md`, `docs/architecture.md`, and `docs/decisions.md`.
References:
`README.md`, `docs/README.md`, `docs/workflows.md`

Decision:
Include optional shell alias guidance in `README.md` for frequent deepreview CLI usage.
Context:
Operators who run deepreview repeatedly benefit from a shorter command path.
Rationale:
Providing a small optional alias pattern (`dr`) improves ergonomics without changing runtime behavior or CLI contracts.
Trade-offs:
Adds a small amount of shell-specific onboarding text to README.
Enforcement:
README includes optional alias setup guidance with an example invocation using the alias.
References:
`README.md`, `docs/alignment.md`

Decision:
Allow omitted `<repo>` and `--source-branch` by inferring from current local GitHub repo context.
Context:
Operators often run deepreview from the repository they intend to review and prefer minimal CLI input.
Rationale:
Inference reduces friction while preserving explicit override behavior for non-default workflows.
Trade-offs:
Inference can fail in ambiguous contexts (non-GitHub remotes, detached HEAD, mismatched repo context), requiring explicit flags.
Enforcement:
CLI parser infers missing repo/branch from local repo context only when confidence is high and errors otherwise.
References:
`internal/deepreview/cli.go`, `internal/deepreview/local_context.go`, `README.md`

Decision:
When wrappers launch deepreview from the source checkout (for example via `cd ... && go run`), treat caller repo context as authoritative for implicit repo/branch inference.
Context:
Shell wrappers that changed directory into the deepreview repo caused omitted `<repo>` inference to silently target `olliecrow/deepreview`, producing cross-repo lock collisions and runs against the wrong repository.
Rationale:
Inference must track operator intent, not launcher implementation details. Supporting explicit caller context (`DEEPREVIEW_CALLER_CWD`) and guarded `OLDPWD` fallback preserves default ergonomics while preventing wrong-repo runs in common wrapper setups.
Trade-offs:
Adds inference precedence logic and one wrapper-specific fallback path.
Enforcement:
`inferRepoAndBranch` now resolves implicit repo context using `DEEPREVIEW_CALLER_CWD` first, then `OLDPWD` only when current repo matches deepreview source root; targeted tests assert wrapper fallback, env override precedence, and non-source-root stability.
References:
`internal/deepreview/local_context.go`, `internal/deepreview/local_context_test.go`, `internal/deepreview/cli.go`, `README.md`

Decision:
In PR mode, pre-delivery privacy handling uses a bounded remediation loop (maximum 3 attempts) and proceeds with PR delivery after bounded attempts.
Context:
Hard-failing delivery on first privacy scan miss caused repeated runs to complete review/execute work but fail at the final gate, creating avoidable delivery dead-ends.
Rationale:
Treating privacy as a bounded fix loop keeps privacy hygiene proactive while preserving delivery momentum; Codex can stop early when it judges remediation complete.
Trade-offs:
Residual privacy findings may still exist when bounded attempts are exhausted; this approach prioritizes bounded autonomy and delivery continuity over hard-stop guarantees at this gate.
Enforcement:
PR-mode delivery runs a Codex-guided privacy remediation attempt loop (`max=3`) before push/PR actions; attempts may stop early on Codex `stop`, and delivery proceeds by policy after bounded attempts.
References:
`internal/deepreview/orchestrator.go`, `prompts/delivery/privacy-fix.md`, `internal/deepreview/integration_test.go`, `docs/spec.md`, `docs/architecture.md`, `README.md`

Decision:
Treat Windows local-path matching as conservative to avoid false positives on shell/path-expansion fragments in changed files.
Context:
The original Windows path regex (`[A-Za-z]:\\\S+`) could match non-path shell fragments such as `.../bin:\${PATH}`, triggering unnecessary privacy remediation attempts.
Rationale:
Restricting Windows-path matches to backslash-prefixed path segments that begin with an expected path token keeps privacy detection useful while avoiding obvious false positives in shell scripts.
Trade-offs:
This matcher remains heuristic and may miss unusual Windows path encodings that start with symbols; coverage is focused on common absolute path shapes.
Enforcement:
Privacy path matcher requires a drive-letter path segment with an alphanumeric/path-like token after the first backslash; regression tests assert shell expansion fragments are allowed while canonical Windows absolute paths are still blocked.
References:
`internal/deepreview/orchestrator.go`, `internal/deepreview/privacy_test.go`

Decision:
Treat user interrupt (`Ctrl+C`) as immediate worker termination plus cleanup: hard-stop active worker commands immediately, then cleanup worktrees/locks and exit.
Context:
Long-running review runs need a predictable operator escape hatch that does not continue spending Codex tokens after user cancellation. Pure abrupt process termination can leave stale worktrees/locks and block subsequent runs.
Rationale:
Immediate hard-stop preserves operator control/token budget while still maintaining workspace hygiene and lock correctness via cleanup.
Trade-offs:
Adds interrupt orchestration and aggressive process teardown behavior.
Enforcement:
Review command captures interrupts, cancels run context, and force-terminates active command/process trees immediately (`SIGKILL` on unix, `Kill` on windows), then returns exit code `130` after cleanup; tests verify cancellation classification, command teardown behavior, and interrupt-triggered cleanup/source-branch non-mutation.
References:
`internal/deepreview/cli.go`, `internal/deepreview/process.go`, `internal/deepreview/tui.go`, `internal/deepreview/integration_test.go`, `internal/deepreview/gitops.go`

Decision:
When source branch is inferred, require local branch readiness: no tracked local changes and exact local/upstream synchronization.
Context:
deepreview reviews remote branch state; inferred local context should match the remote state to avoid reviewing stale or partial work.
Rationale:
Failing fast on unsynced local context prevents accidental reviews of outdated remote state.
Trade-offs:
Adds strict pre-run checks that may require operator prep (`commit/push/pull`) before review can start.
Enforcement:
Inference path validates tracked-working-tree cleanliness and local/upstream SHA equality before run start.
References:
`internal/deepreview/local_context.go`, `internal/deepreview/local_context_test.go`, `README.md`

Decision:
Replace managed workspace clone with a fresh clone each run.
Context:
Interrupted or abandoned previous runs can leave stale checkout/worktree state under `~/deepreview/repos/...`.
Rationale:
Fresh clone replacement is simpler and more reliable than trying to recover unknown stale state.
Trade-offs:
Slightly higher clone/fetch cost per run.
Enforcement:
Managed repo path is removed and recloned during prepare stage before fetching refs.
References:
`internal/deepreview/gitops.go`, `internal/deepreview/gitops_test.go`, `docs/architecture.md`

Decision:
In `yolo` mode, when source branch equals default branch, preflight direct-push permission using `git push --dry-run`.
Context:
Some repositories block direct pushes to default branch via protection rules.
Rationale:
Failing early avoids wasting full review cycles when final delivery is guaranteed to fail.
Trade-offs:
Adds one remote preflight check before rounds in this specific mode/branch case.
Enforcement:
Prepare stage runs yolo default-branch dry-run push preflight and fails fast on rejection.
References:
`internal/deepreview/orchestrator.go`, `internal/deepreview/gitops.go`, `docs/spec.md`

Decision:
Normalize yolo mode parsing for operator ergonomics: accept case-insensitive `--mode` values and support legacy `--YOLO`.
Context:
Operators may type uppercase mode values while dictating or using older habits.
Rationale:
Small compatibility parsing reduces avoidable CLI friction without adding complexity.
Trade-offs:
Very small additional argument-normalization logic in CLI parsing.
Enforcement:
CLI argument parsing lowercases mode values, normalizes `--YOLO` to `--yolo`, and tests cover both forms.
References:
`internal/deepreview/cli.go`, `internal/deepreview/cli_test.go`, `docs/spec.md`, `README.md`

Decision:
Standardize execute prompt variable naming on `REVIEW_*` placeholders while keeping fanout placeholders as backward-compatible aliases.
Context:
Stage terminology was renamed to independent review/execute, but template variable names still referenced fanout.
Rationale:
Aligned variable naming improves readability and keeps prompt terminology consistent with runtime stage names.
Trade-offs:
Temporary duplication of variable keys until legacy templates are fully retired.
Enforcement:
Execute prompt templates use `REVIEW_REPORT_*` placeholders and orchestrator injects both new and legacy keys.
References:
`prompts/execute/01-consolidate-reviews.md`, `internal/deepreview/orchestrator.go`

Decision:
All Codex prompt-generated artifacts must be written inside the active worktree first, then copied to canonical run artifact paths.
Context:
Codex runs with filesystem sandboxing that may block writes outside its working tree. Prompt templates previously pointed outputs (review reports, round triage/plan/verification/status/summary) directly to `~/deepreview/runs/...`, which can fail even when Codex completed useful work.
Rationale:
Writing in-worktree is sandbox-compatible and deterministic. Copying to canonical run paths preserves the external artifact contract used by orchestrator logic and user-facing run directories.
Trade-offs:
Slightly more orchestration logic for artifact materialization and fallback probing.
Enforcement:
Independent review stage writes to per-worker worktree paths and materializes canonical `round-XX/review-YY.md`; execute stage writes triage/plan/verification/status/summary under execute worktree `.deepreview/artifacts/` and then materializes canonical round artifacts.
References:
`internal/deepreview/orchestrator.go`, `internal/deepreview/integration_test.go`

Decision:
Internal deepreview operational artifacts must never be delivered to source repositories (no commit/push/PR diff entries under `.deepreview/`).
Context:
Execute-stage prompts require intermediate files (triage/plan/verification/status/summary) for orchestration, but these are control-plane artifacts, not user repository content.
Rationale:
Preventing operational artifact delivery keeps PRs clean, avoids leaking internal review machinery, and aligns output with user expectations (only meaningful repository changes should be delivered).
Trade-offs:
Adds delivery validation and execute-stage cleanup/auto-commit logic to separate operational files from real repository changes.
Enforcement:
Execute stage removes internal `.deepreview` worktree artifacts before final commit checks, auto-commits remaining repository changes when needed, validates no internal artifact paths exist in candidate commit range, and blocks delivery if branch diff contains `.deepreview/`.
References:
`internal/deepreview/orchestrator.go`, `internal/deepreview/gitops.go`, `prompts/execute/04-cleanup-summary-commit.md`, `internal/deepreview/integration_test.go`

Decision:
After run completion, CLI must print an explicit terminal summary with delivery outcome and clickable URL where applicable.
Context:
Full-screen TUI exits back to shell; without post-run summary users can miss final result details (PR created, pushed commits, or no-op delivery).
Rationale:
A concise completion line with direct URL improves UX and reduces ambiguity immediately after command return.
Trade-offs:
Adds a small amount of additional stdout output after successful runs.
Enforcement:
CLI prints run completion summary, including PR URL in PR mode, commits URL in yolo mode, or explicit skipped-delivery reason for no-op runs.
References:
`internal/deepreview/cli.go`, `internal/deepreview/integration_test.go`

Decision:
Allow concurrent deepreview runs across different repositories, but enforce a per-repository run lock to prevent concurrent runs on the same repository.
Context:
Managed repository cloning/worktree operations mutate shared per-repo workspace paths (`~/deepreview/repos/<owner>/<repo>`), which can race if two runs target the same repo at once.
Rationale:
Cross-repo concurrency is desirable for user throughput, while same-repo serialization prevents destructive races and stale state corruption.
Trade-offs:
Operators cannot run two same-repo deepreview sessions at the exact same time; they must wait for the active run to complete (or stale lock recovery to occur).
Enforcement:
Run startup acquires a repo-scoped lock file under `~/deepreview/locks/<owner>/<repo>.lock`; lock creation fails with a clear error if another active run holds it; stale locks are reclaimed.
References:
`internal/deepreview/orchestrator.go`, `internal/deepreview/orchestrator_test.go`, `README.md`

Decision:
Pin deepreview Codex execution to `gpt-5.3-codex` with `model_reasoning_effort=xhigh` for all prompt executions and resume turns.
Context:
Users want consistent deep-review quality and deterministic model behavior across runs.
Rationale:
Hard-pinning model and reasoning removes drift from local CLI defaults/profile changes and ensures every stage runs with the intended capability level.
Trade-offs:
Reduced flexibility to switch model/effort at runtime from deepreview CLI flags.
Enforcement:
Codex command construction always injects `--model gpt-5.3-codex` and `-c model_reasoning_effort=\"xhigh\"` for new and resumed exec turns; parser defaults and help text reflect the pin.
References:
`internal/deepreview/codex.go`, `internal/deepreview/codex_test.go`, `internal/deepreview/cli.go`, `README.md`

Decision:
If execute phase produces repository changes in a round, deepreview must run at least one additional review round before any final delivery.
Context:
Without a mandatory post-change review pass, changed code can be delivered without being independently re-reviewed in the updated state.
Rationale:
Forcing a fresh independent review after modifications increases confidence in final delivery quality while keeping flow simple.
Trade-offs:
Runs can require additional rounds; if `--max-rounds` is too low to allow the required post-change review round, the run fails and operator must rerun with a higher max round setting.
Enforcement:
Round loop checks candidate branch HEAD before/after execute stage; when changed, it forces another round regardless of status decision. If that would exceed `--max-rounds`, run exits with a clear error, prints a self-serve failure summary (completed progress + artifact/log/review paths), and performs no delivery.
References:
`internal/deepreview/orchestrator.go`, `internal/deepreview/integration_test.go`, `README.md`

Decision:
TUI rendering must reserve a right-edge gutter and initialize model viewport size from CLI terminal dimensions.
Context:
Repeated terminal scrolling was observed during live runs when TUI frame lines hit terminal width exactly or when first paints occurred before Bubble Tea window-size messages arrived.
Rationale:
Terminals can auto-wrap exact-width lines into an extra row before newline, which causes logical-line cursor rewinds to drift and append frames. Seeding initial viewport dimensions and keeping one free column avoids this class of renderer drift.
Trade-offs:
Rendered content uses a small right gutter (a few fewer visible columns); some layouts switch to compact mode slightly earlier on narrow terminals.
Enforcement:
`RunCLI` enables full-screen TUI by default when terminal checks pass, with `--no-tui` as explicit opt-out to force structured text logs. When active, `RunCLI` passes measured terminal width/height into TUI model initialization, `effectiveContentWidth` subtracts a conservative right-edge gutter, timeline rendering applies an additional width safety gutter, and the top region is kept compact: inline progress in the header, one `RUN CONTEXT` panel, and one stage timeline panel (status/live-summary boxes removed). Row budgeting uses wrap-aware rendered-row accounting. Final rendering is stabilized into a fixed viewport frame: each line is ANSI-safe truncated and padded to one column below terminal width, and output is padded/clamped to viewport height. Ultra-narrow pathological viewports (`width<=1`) use a blank-frame fallback to avoid unavoidable auto-wrap drift. TUI heartbeat refresh runs at 1s cadence to reduce repaint churn. Regression tests validate width safety, height safety, fixed-frame shape invariants, and absence of border-collision artifacts.
References:
`internal/deepreview/cli.go`, `internal/deepreview/tui.go`, `internal/deepreview/tui_test.go`

Decision:
Expose explicit `doctor` and `dry-run` commands alongside `review`.
Context:
Operators need quick non-mutating checks and a deterministic preview before launching full multi-round runs.
Rationale:
Dedicated helper commands improve onboarding and troubleshooting without changing review execution semantics.
Trade-offs:
CLI help and spec docs require additional maintenance as command behavior evolves.
Enforcement:
CLI dispatch supports `doctor` and `dry-run`; help text documents both commands; tests cover help/dispatch paths and output expectations.
References:
`internal/deepreview/cli.go`, `internal/deepreview/cli_test.go`, `README.md`, `docs/spec.md`

Decision:
Apply strict privacy guardrails across outward-facing deepreview surfaces, with bounded pre-delivery remediation in PR mode.
Context:
Runs can generate or relay text into PR titles/descriptions, summaries, logs, and commit messages across both public and private repositories; these surfaces must never leak personal information or secrets.
Rationale:
Centralized sanitization plus pre-delivery scans provide consistent protection while preserving normal execution flow when content is safe.
Trade-offs:
Conservative pattern-based blocking can reject some edge-case content that resembles sensitive data, while bounded PR-mode remediation can still allow residual findings after max attempts.
Enforcement:
Runtime sanitizes PR/summary delivery content, validates generated public text before delivery writes, and in PR mode runs bounded scans/remediation over changed files and delivery commit messages before push/PR creation. Local CLI/TUI/text progress output remains literal for operator visibility and debugging.
References:
`internal/deepreview/orchestrator.go`, `internal/deepreview/privacy_test.go`, `docs/spec.md`

Decision:
Keep unsupported-command CLI error output literal to preserve local operator context.
Context:
User input for the top-level command token is echoed on CLI errors; sanitizing this path degraded local usability and made diagnosis harder.
Rationale:
Local terminal output should be maximally useful for the operator. Privacy policy is enforced on delivery/public surfaces, not local stderr/stdout.
Trade-offs:
Machine-local paths can appear in local terminal output; users should treat terminal transcripts as local artifacts unless intentionally shared.
Enforcement:
`RunCLI` prints the original unsupported token, with regression coverage asserting `/Users/...` is preserved locally.
References:
`internal/deepreview/cli.go`, `internal/deepreview/cli_test.go`

Decision:
Provide first-class shell completion output and document command help for every top-level command.
Context:
deepreview is operated as a daily CLI and now has multiple top-level commands; discoverability and typing safety matter in high-frequency use.
Rationale:
Adding `completion [bash|zsh]` and explicit help paths for it keeps the CLI self-service without changing review execution logic.
Trade-offs:
Completion templates require periodic maintenance as command flags evolve.
Enforcement:
- `RunCLI` dispatches `completion`, with dedicated help text and strict shell validation.
- Main help text lists completion command and completion-specific help entrypoint.
- Tests cover completion help output, shell script generation, and unsupported-shell errors.
References:
`internal/deepreview/cli.go`, `internal/deepreview/cli_test.go`, `README.md`

Decision:
Keep PR descriptions size-safe and privacy-safe by using a detailed Codex-generated final body and excluding raw per-worker review reports/full execute artifact dumps.
Context:
Large multi-round runs can produce PR descriptions that exceed GitHub's body limit (`65536` chars), causing delivery failures at `gh pr create`.
Rationale:
A structured narrative final body preserves actionable signal while avoiding oversized payloads and reducing accidental leakage risk.
Trade-offs:
Some deep artifact detail is no longer directly embedded in PR body and must be read from run artifacts when needed.
Enforcement:
Final PR body is generated in post-delivery Codex stage, validated with privacy checks, and capped with compact fallback when body size approaches GitHub limits.
References:
`internal/deepreview/orchestrator.go`, `internal/deepreview/integration_test.go`, `internal/deepreview/orchestrator_test.go`, `docs/spec.md`

Decision:
Emit periodic stage heartbeat progress updates while long-running worker/prompt execution is in flight.
Context:
Some independent-review and execute steps can run for extended periods with no stdout updates, which looks stalled in the TUI even when progress is healthy.
Rationale:
Heartbeat updates keep operator confidence high and reduce false "hung run" assumptions without changing execution semantics.
Trade-offs:
Adds periodic progress-message noise (bounded cadence) in stage logs.
Enforcement:
Orchestrator emits stage progress heartbeats on a fixed interval during worker fanout waits and long prompt runs; tests cover stage-progress state updates so completed stages are updated in-place instead of reopening duplicate rows.
References:
`internal/deepreview/orchestrator.go`, `internal/deepreview/progress.go`, `internal/deepreview/progress_test.go`, `internal/deepreview/tui.go`

Decision:
Block delivery unless repository quality gates pass, and keep docs local-path remediation as a built-in step within PR-mode privacy remediation attempts.
Context:
Runs could reach delivery with failing repository pre-commit hooks, and privacy scans could fail late on machine-local absolute paths inside changed docs files even when remediation was straightforward.
Rationale:
Hard-gating delivery on repo-local quality checks prevents deepreview from opening PRs that are already known-bad on mandatory local checks. Auto-remediating docs local-path violations keeps deterministic redaction built in while reducing unnecessary manual reruns.
Trade-offs:
Delivery can take longer when `setup_env.sh` exists, and some runs now fail earlier on quality-gate violations that were previously deferred to PR CI. Auto-remediation is intentionally narrow (docs text formats only) to avoid mutating code paths.
Enforcement:
Delivery stage resolves the candidate branch HEAD and creates a detached `delivery/quality-worktree` snapshot from that commit, then runs `pre-commit run --all-files` when `.pre-commit-config.yaml` exists and `./setup_env.sh` when present inside that snapshot; non-zero exit blocks delivery. The quality worktree is removed after checks (success or failure). In PR mode, privacy remediation attempts include a built-in docs-only local-path redaction pass (`/path/to/project` placeholder) before optional Codex remediation within each bounded attempt.
References:
`internal/deepreview/orchestrator.go`, `internal/deepreview/orchestrator_test.go`, `internal/deepreview/privacy_test.go`, `docs/spec.md`, `README.md`

Decision:
Introduce bounded inactivity watchdog and restart handling for all Codex workers (independent review, execute prompts, and post-delivery prompt).
Context:
Operators observed runs where one worker could stall indefinitely and block the entire deepreview pipeline.
Rationale:
Activity-based monitoring plus bounded restarts preserves full review coverage while preventing single-worker stalls from holding the run hostage.
Trade-offs:
Adds monitoring/restart complexity and can restart workers that are genuinely quiet for too long; this is mitigated by generous timeout defaults and configurable knobs.
Enforcement:
- Review policy defaults:
  - `DEEPREVIEW_REVIEW_INACTIVITY_SECONDS=300` (5 minutes)
  - `DEEPREVIEW_REVIEW_ACTIVITY_POLL_SECONDS=15`
  - `DEEPREVIEW_REVIEW_MAX_RESTARTS=1`
- Worker activity evidence includes stdout/stderr output and filesystem/git-change signals.
- On inactivity timeout, deepreview cancels and restarts the worker up to configured max restarts.
- Independent review stage requires full worker coverage; stage fails if any worker cannot complete within bounded restart policy.
- Run config snapshot records effective review policy settings.
- Integration and unit tests assert inactivity restart behavior and policy clamping behavior; restart-path integration assertions should prefer deterministic log evidence over strict wall-time thresholds to reduce flake risk.
References:
`internal/deepreview/orchestrator.go`, `internal/deepreview/cli.go`, `internal/deepreview/integration_test.go`, `internal/deepreview/orchestrator_test.go`, `docs/spec.md`, `docs/architecture.md`
