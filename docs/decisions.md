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
Resolve the Codex launcher dynamically by name, always using `multicodex` when it is available and falling back to `codex` only when `multicodex` is unavailable and fallback is allowed.
Context:
Some machines expose a live `multicodex` wrapper on `PATH` that should always be used for prompt runs, but deepreview should remain portable to systems that only have plain `codex`.
Rationale:
Name-based resolution avoids hardcoded workstation paths while still letting operators enforce strict `multicodex` usage with `DEEPREVIEW_REQUIRE_MULTICODEX=1`. Treating `multicodex` as authoritative whenever it exists keeps deepreview aligned with the operator's intended routing policy. Requiring a real `multicodex` command on `PATH` keeps execution simple and avoids fragile interactive-shell wrapper behavior while still allowing the `PATH` command itself to manage rebuilding or dispatching to the latest multicodex implementation.
Trade-offs:
Launcher behavior now depends on `PATH` setup, and operators who previously relied on shell-only functions or aliases must expose `multicodex` as a real command instead.
Enforcement:
`CodexRunner.resolveLauncher` selects `PATH` `multicodex` whenever it exists, `DEEPREVIEW_REQUIRE_MULTICODEX` fails fast when unavailable, `DEEPREVIEW_CODEX_BIN` only affects the codex fallback path, and doctor validates the selected launcher with matching auth checks.
References:
`internal/deepreview/codex.go`, `internal/deepreview/cli.go`, `internal/deepreview/codex_test.go`, `internal/deepreview/cli_test.go`, `docs/spec.md`, `docs/architecture.md`, `README.md`

Decision:
Bias deepreview review and execute prompts toward simplification, deletion, and scope reduction when those are the cleanest high-confidence fixes.
Context:
Repeated deepreview runs can drift toward additive fixes because prompts emphasize implementation and verification pressure more than removal pressure.
Rationale:
Explicitly naming deletion and simplification as preferred outcomes when they cleanly solve accepted issues counterbalances additive bias without broadening scope into speculative refactoring.
Trade-offs:
Prompt guidance still requires high confidence, so some worthwhile cleanup will remain out of scope when it is not tightly tied to accepted critical/high work.
Enforcement:
Independent review, execute planning, execute/verify, and PR-preparation prompts tell Codex not to bias toward additive fixes and to treat high-confidence removals or simplifications as first-class options.
References:
`prompts/review/independent-review.md`, `prompts/execute/01-consolidate-plan.md`, `prompts/execute/02-execute-verify.md`, `prompts/delivery/pr-prepare.md`, `docs/architecture.md`

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
Run iterative deepreview rounds with default `max_rounds=5`, using two-signal progression (Codex status decisions plus repository change detection) and a required round-status artifact for traceability.
Context:
One review/execute pass may miss issues; iterative passes improve confidence before final delivery.
Rationale:
Bounded rounds provide extra review depth while preventing unbounded loops, while using Codex's explicit `continue|stop` judgment keeps the loop aligned with review quality instead of relying only on whether files changed. The second-consecutive-stop rule preserves one confirmation round without needing a separate audit mode.
Trade-offs:
Longer wall-clock runtime compared with single-pass flows.
Enforcement:
Runtime contract includes `--max-rounds`; architecture/spec require validated round-status artifacts plus orchestrator logic where `continue` always continues, first `stop` forces a confirmation round, and second consecutive `stop` ends the loop even if that round changed the repository.
References:
`docs/spec.md`, `docs/architecture.md`

Decision:
Do not use a special automatic final audit round; use the normal round loop and fail only when another round is still required at `--max-rounds`.
Context:
deepreview requires confirmation before delivery, but a dedicated audit-only branch of orchestration added extra policy surface, extra tests, and a second round mode to reason about.
Rationale:
The first-stop/second-stop policy already gives a built-in confirmation pass. Removing the audit-only mode simplifies the orchestrator and keeps every round on one consistent execute path.
Trade-offs:
Runs that still need another round at the configured limit now fail/incomplete directly instead of getting one extra implicit audit round for free.
Enforcement:
Orchestrator round control has no audit-only branch. When another round is still required at `--max-rounds`, `pr` mode publishes an incomplete draft PR when deliverable changes exist and `yolo` mode fails with guidance to rerun using a higher limit.
References:
`internal/deepreview/orchestrator.go`, `docs/spec.md`, `docs/architecture.md`

Decision:
Track PR delivery branch push state separately from PR creation success so incomplete-draft recovery can reuse an already-pushed branch.
Context:
In `pr` mode, a run can successfully push the delivery branch and then fail before `gh pr create` returns a PR URL. Treating "push happened" as equivalent to "delivery completed" blocks the documented incomplete-draft recovery path and can leave a remote branch with no PR.
Rationale:
Separating `branch pushed`, `refspec`, and `prURL established` preserves the one-push invariant while allowing recovery to create a draft PR from the existing delivery branch after post-push failures.
Trade-offs:
Adds a small amount of explicit delivery state to the orchestrator instead of relying on a coarse push counter alone.
Enforcement:
`deliverPR` reuses an existing pushed delivery branch, incomplete-draft recovery is gated on missing PR URL rather than push count, and integration coverage expects a draft recovery PR when `gh pr create` initially returns no URL after push.
References:
`internal/deepreview/orchestrator.go`, `internal/deepreview/integration_test.go`, `docs/spec.md`

Decision:
Inject compact review summaries into execute prompt 1 and provide on-disk review file paths for deeper inspection.
Context:
Execute prompt 1 needs reviewer signal quickly, but fully inlining every review body increases prompt size and latency.
Rationale:
Compact summaries keep the most relevant review signal in prompt context while still trusting Codex to open full review files on disk when it wants more detail.
Trade-offs:
The injected summaries are lossy compared with raw review text, so the prompt must explicitly point Codex at the review files when deeper inspection is needed.
Enforcement:
The orchestrator builds structured review summaries for prompt injection, and prompt 1 tells Codex to use those summaries for orientation and read the on-disk reports directly when useful.
References:
`internal/deepreview/orchestrator.go`, `prompts/execute/01-consolidate-plan.md`, `docs/spec.md`

Decision:
Push exactly once at final delivery, regardless of mode; never push intermediate-round commits, and only deliver after round execution and delivery gates pass.
Context:
User requires that intermediate iteration remains local until final confidence is reached.
Rationale:
Single final push reduces remote churn and keeps iterative experimentation private until finalized; gating delivery on completed rounds plus delivery checks ensures publishing only happens after successful run completion.
Trade-offs:
Remote visibility of intermediate progress is intentionally reduced.
Enforcement:
Spec/architecture require one final push point, forbid intermediate pushes, and gate final delivery on completed round execution plus delivery quality/privacy checks.
References:
`docs/spec.md`, `docs/architecture.md`

Decision:
Deepreview-managed commits use the operator's local Git identity and explicitly disable signing in managed clones/worktrees.
Context:
Deepreview creates internal automation commits in managed repositories and worktrees. Depending on host-level GPG signing config can make otherwise-valid runs fail for operator-environment reasons.
Rationale:
Resolving identity from the source repository first, then falling back to global Git config, keeps authorship aligned with the operator's normal Git setup without introducing deepreview-specific identity layers. Disabling signing for deepreview-owned commits removes an unnecessary dependency on external signer setup.
Trade-offs:
Automation commits created by deepreview are intentionally unsigned even when the operator normally signs interactive commits.
Enforcement:
Managed-clone setup writes the resolved Git identity plus `commit.gpgsign=false`, and commit helpers pass the resolved identity with no-sign flags on each deepreview-owned commit. Resolution uses source-repo `user.name` / `user.email` first, then global Git `user.name` / `user.email`.
References:
`internal/deepreview/git_identity.go`, `internal/deepreview/gitops.go`, `internal/deepreview/gitops_test.go`, `docs/spec.md`, `docs/architecture.md`

Decision:
Only successful rounds with authoritative `round.json` records count as completed rounds in final reporting.
Context:
Execute prompts can produce round artifacts before orchestrator post-processing finishes. If a later execute-stage validation or commit step fails, those artifacts can misrepresent an uncommitted round as completed.
Rationale:
Completion summaries and delivery surfaces must reflect durable round state rather than transient execute output.
Trade-offs:
Failed execute attempts may require reading diagnostic sub-artifacts instead of the top-level round summary paths.
Enforcement:
Execute-stage artifacts are validated first; after execute-stage success and any required local commit, the orchestrator writes `round.json`, and completion reporting keys off that authoritative record rather than raw summary/status-file presence.
References:
`internal/deepreview/orchestrator.go`, `internal/deepreview/cli.go`, `internal/deepreview/cli_test.go`, `docs/spec.md`, `docs/architecture.md`

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
`docs/spec.md`, `internal/deepreview/cli.go`, `internal/deepreview/cli_test.go`

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
`docs/spec.md`, `docs/architecture.md`, `internal/deepreview/orchestrator.go`, `internal/deepreview/orchestrator_test.go`

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
`prompts/execute/01-consolidate-plan.md`, `docs/spec.md`, `docs/architecture.md`

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
`prompts/review/independent-review.md`, `prompts/execute/01-consolidate-plan.md`, `docs/spec.md`, `prompts/README.md`

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
`internal/deepreview/orchestrator.go`, `internal/deepreview/orchestrator_test.go`, `docs/spec.md`, `prompts/execute/01-consolidate-plan.md`

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
`prompts/execute/01-consolidate-plan.md`, `prompts/execute/02-execute-verify.md`, `prompts/execute/03-cleanup-summary-commit.md`, `prompts/review/independent-review.md`, `prompts/README.md`

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
`docs/spec.md`, `prompts/README.md`, `internal/deepreview/templates.go`, `internal/deepreview/template_test.go`, `internal/deepreview/orchestrator.go`

Decision:
Trust prompt discovery only from explicit override or deepreview-owned prompt roots.
Context:
deepreview is routinely launched from inside the target repository being reviewed. Trusting a caller checkout's own `./prompts` directory lets an untrusted repo override deepreview's review, execute, and delivery instructions.
Rationale:
Restricting default prompt discovery to explicit override or deepreview-owned locations preserves prompt editability for operators while keeping the prompt control plane out of the reviewed repository.
Trade-offs:
Ad hoc local prompt experimentation from arbitrary working directories now requires setting `DEEPREVIEW_PROMPTS_ROOT` explicitly.
Enforcement:
Default prompt discovery ignores caller-CWD `./prompts` and resolves only `DEEPREVIEW_PROMPTS_ROOT` or deepreview-owned executable/source-relative prompt directories; regression tests cover the caller-CWD hijack case.
References:
`internal/deepreview/orchestrator.go`, `internal/deepreview/orchestrator_test.go`, `docs/spec.md`

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
`docs/spec.md`, `docs/architecture.md`, `prompts/execute/queue.txt`, `prompts/execute/01-consolidate-plan.md`, `prompts/execute/02-execute-verify.md`, `prompts/execute/03-cleanup-summary-commit.md`

Decision:
Use Go as the primary implementation language for the deepreview runtime and TUI.
Context:
The tool is CLI/TUI-heavy, runs concurrent subprocess orchestration, and should be easy to distribute as a single binary.
Rationale:
Go provides strong fit for this shape: simple static binaries, reliable concurrency primitives, and mature terminal UI libraries.
Trade-offs:
Initial rewrite/migration cost and short-term delivery slowdown while stabilizing the Go implementation.
Enforcement:
Primary CLI/runtime code lives in Go under `cmd/` and `internal/deepreview/`; architecture/spec describe the shipped CLI behavior.
References:
`cmd/deepreview/main.go`, `internal/deepreview/`, `docs/spec.md`, `docs/architecture.md`

Decision:
When `pr` mode has already produced deliverable repository changes but the run cannot finish cleanly, publish a draft `[INCOMPLETE]` PR instead of dropping the candidate branch.
Context:
Multi-round runs can spend significant time implementing and verifying high-severity fixes, then still stop short of a normal terminal `stop` state or later delivery gates.
Rationale:
Preserving tangible work in a visible draft PR keeps hard-won fixes reviewable and recoverable, while the explicit `[INCOMPLETE]` marker prevents the branch from being mistaken for merge-ready output.
Trade-offs:
Draft PRs may surface partially complete work on GitHub and add some branch/PR churn compared with the previous fail-without-PR behavior.
Enforcement:
PR-mode failure recovery should publish a draft PR with explicit incomplete title/body markers whenever deliverable repo changes exist and public-surface hygiene checks pass.
References:
`internal/deepreview/orchestrator.go`, `internal/deepreview/cli.go`, `docs/spec.md`, `docs/architecture.md`

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
Inference must track operator intent, not launcher implementation details. Treating `DEEPREVIEW_CALLER_CWD` as an unconditional explicit override preserves wrapper intent even when the wrapper launches from another repo or a non-repo directory, while keeping `OLDPWD` guarded to deepreview-source-root launches prevents accidental fallback in unrelated shells.
Trade-offs:
Adds inference precedence logic and one wrapper-specific fallback path.
Enforcement:
`inferRepoAndBranch`, local readiness validation, and commit-identity resolution now check `DEEPREVIEW_CALLER_CWD` first, then `OLDPWD` only when current repo matches deepreview source root; targeted tests assert wrapper fallback, explicit override precedence, and non-source-root/non-repo behavior.
References:
`internal/deepreview/local_context.go`, `internal/deepreview/local_context_test.go`, `internal/deepreview/cli.go`, `README.md`

Decision:
Represent repo source type explicitly and canonicalize filesystem-local clone sources during identity resolution.
Context:
Using synthetic owner `local` to represent filesystem remotes collided with valid GitHub namespaces such as `local/repo`, broke `--mode pr` eligibility checks, and made relative local origins depend on the caller's working directory.
Rationale:
An explicit source-type field keeps GitHub-vs-filesystem behavior honest across PR gating, display slugs, managed repo paths, and lock paths. Canonicalizing relative filesystem remotes once at identity resolution ensures clone/fetch and doctor checks use the same stable source path.
Trade-offs:
Filesystem-local repos now use a deterministic internal namespace derived from the canonical clone source instead of the older synthetic `local/*` convention.
Enforcement:
`RepoIdentity` carries an explicit source type, `SupportsPRDelivery` keys off that source type, local-path identity resolution canonicalizes filesystem remotes before GitHub slug parsing, and regression tests cover relative local remotes plus GitHub `local/repo` inputs.
References:
`internal/deepreview/types.go`, `internal/deepreview/orchestrator.go`, `internal/deepreview/orchestrator_test.go`, `docs/spec.md`

Decision:
In PR mode, pre-delivery privacy handling uses a bounded remediation loop (maximum 3 attempts) and proceeds with PR delivery after bounded attempts.
Context:
Hard-failing delivery on first privacy scan miss caused repeated runs to complete review/execute work but fail at the final gate, creating avoidable delivery dead-ends.
Rationale:
Treating privacy as a bounded fix loop keeps privacy hygiene proactive while preserving delivery momentum, while requiring clean post-attempt scans and a clean remediation worktree after deepreview auto-commits simple residual edits prevents a single inaccurate `stop` from bypassing the gate without forcing Codex to manage every last `git commit` detail itself.
Trade-offs:
Residual privacy findings may still exist when bounded attempts are exhausted; this approach prioritizes bounded autonomy and delivery continuity over hard-stop guarantees at this gate.
Enforcement:
PR-mode delivery runs one Codex branch-preparation pass and then a Codex-guided privacy remediation attempt loop (`max=3`) in candidate-branch worktrees before push/PR actions; attempts may stop early only after post-attempt commit-message and changed-file scans pass and the remediation worktree is clean after deepreview auto-commits simple residual edits, and delivery proceeds by policy after bounded attempts. Built-in docs-only local-path sanitization runs against that candidate worktree so non-default source branches cannot be remediated against the wrong checked-out branch.
References:
`internal/deepreview/orchestrator.go`, `prompts/delivery/privacy-fix.md`, `internal/deepreview/integration_test.go`, `docs/spec.md`, `docs/architecture.md`, `README.md`

Decision:
Support only macOS and Linux host operating systems.
Context:
`deepreview` is used on macOS and Linux only. Carrying Windows-specific code paths and heuristics increased maintenance surface without serving an in-scope runtime.
Rationale:
Removing Windows-specific compatibility code keeps process handling, privacy scanning, and documentation aligned with actual operating scope.
Trade-offs:
Windows builds and runtime behavior are unsupported. Reintroducing Windows support would require deliberate implementation work rather than relying on stale compatibility shims.
Enforcement:
Host-specific process management is implemented only for `darwin` and `linux`; privacy path matching scans only supported macOS/Linux home-directory prefixes; docs and requirements declare Windows unsupported.
References:
`internal/deepreview/process.go`, `internal/deepreview/process_unix.go`, `internal/deepreview/orchestrator.go`, `internal/deepreview/privacy_test.go`, `README.md`, `docs/spec.md`

Decision:
Treat user interrupt (`Ctrl+C`) as immediate worker termination plus cleanup: hard-stop active worker commands immediately, then cleanup worktrees/locks and exit.
Context:
Long-running review runs need a predictable operator escape hatch that does not continue spending Codex tokens after user cancellation. Pure abrupt process termination can leave stale worktrees/locks and block subsequent runs.
Rationale:
Immediate hard-stop preserves operator control/token budget while still maintaining workspace hygiene and lock correctness via cleanup.
Trade-offs:
Adds interrupt orchestration and aggressive process teardown behavior.
Enforcement:
Review command captures interrupts, cancels run context, and force-terminates active command/process trees immediately with `SIGKILL`, then returns exit code `130` after cleanup; tests verify cancellation classification, command teardown behavior, and interrupt-triggered cleanup/source-branch non-mutation.
References:
`internal/deepreview/cli.go`, `internal/deepreview/process.go`, `internal/deepreview/tui.go`, `internal/deepreview/integration_test.go`, `internal/deepreview/gitops.go`

Decision:
Write privacy remediation status inside the remediation worktree, then persist it into run artifacts after the worker exits.
Context:
Deepreview keeps prompt-written artifacts worktree-local during execution. Writing `privacy-status.json` directly into the run artifact directory couples prompt behavior to orchestrator-owned paths and makes delivery staging less consistent.
Rationale:
Keeping the worker-written status file inside the worktree preserves one simple rule for prompt outputs while still preserving the status artifact under the run directory for diagnostics and tests. The worktree path must also remain reserved deepreview space so repo-owned `.tmp/` content cannot accidentally deliver worker status files.
Trade-offs:
This adds one post-run copy step for the status artifact and reserves `.tmp/deepreview/` from repository delivery even when a repo tracks `.tmp/`.
Enforcement:
Privacy remediation prompts receive a worktree-local `OUTPUT_STATUS_PATH`; orchestrator copies the resulting JSON into the attempt artifact directory before consuming it; `.tmp/deepreview/` is treated as internal deepreview artifact space for excludes and delivery validation; and integration coverage requires the worker path to stay within the remediation worktree without becoming deliverable.
References:
`internal/deepreview/orchestrator.go`, `internal/deepreview/integration_test.go`, `cmd/fake-codex/main.go`

Decision:
Evaluate outbound push diffs against added lines only.
Context:
The push-range sensitive-text hook previously scanned full patches, including deleted lines. That caused privacy cleanups to be blocked by the very sensitive text they were removing.
Rationale:
Only newly added lines can leak sensitive content into new history. Ignoring deletions and patch context keeps the hook aligned with its security goal without blocking remediation commits.
Trade-offs:
The hook no longer reports sensitive text that appears only in deleted/context lines within an outbound patch. Commit messages remain scanned separately.
Enforcement:
The push-range hook extracts added patch lines before running sensitive-text checks, and regression tests cover both deleted-sensitive-line cleanup and added-sensitive-line rejection.
References:
`scripts/security/check-push-range.sh`, `scripts/security/check-sensitive-text.sh`, `internal/deepreview/security_scripts_test.go`

Decision:
When source branch is inferred, require local branch readiness: no tracked local changes and exact local/upstream synchronization.
Context:
deepreview reviews remote branch state; inferred local context should match the remote state to avoid reviewing stale or partial work.
Rationale:
Failing fast on unsynced local context prevents accidental reviews of outdated remote state, but helper commands and local argument resolution must remain non-mutating in the caller repo. The readiness check therefore compares local HEAD against a read-only remote query instead of fetching into the operator checkout.
Trade-offs:
Adds strict pre-run checks that may require operator prep (`commit/push/pull`) before review can start, and may omit ahead/behind counts when the remote commit is not already present locally.
Enforcement:
Inference path validates tracked-working-tree cleanliness and local/remote SHA equality via a read-only remote query before run start, without updating caller-repo remote-tracking refs.
References:
`internal/deepreview/local_context.go`, `internal/deepreview/local_context_test.go`, `README.md`

Decision:
Replace the branch-scoped managed workspace clone with a fresh clone each run.
Context:
Interrupted or abandoned previous runs can leave stale checkout/worktree state under `~/deepreview/repos/.../branches/...`.
Rationale:
Fresh clone replacement is simpler and more reliable than trying to recover unknown stale state.
Trade-offs:
Slightly higher clone/fetch cost per run.
Enforcement:
The source-branch managed repo path is removed and recloned during prepare stage before fetching refs.
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
`prompts/execute/01-consolidate-plan.md`, `internal/deepreview/orchestrator.go`

Decision:
Codex prompt workers should stage review and execute artifacts inside their current worktree, while deepreview persists canonical copies under the run directory and records round completion with one authoritative `round.json` per successful round.
Context:
The old artifact flow mixed worktree-local writes, promotion/copy steps, and inferred round completion from multiple files. Keeping prompt outputs worktree-local and promoting canonical copies later keeps prompt IO simple while still giving the orchestrator one stable reporting location.
Rationale:
Worktree-local prompt writes keep artifact ownership straightforward. Keeping canonical artifacts in the run directory still gives one stable place for summaries, diagnostics, and completion reporting, while a single orchestrator-written `round.json` keeps round accounting explicit and reliable.
Trade-offs:
Some copy/promotion logic remains for prompt-written artifacts, and docs/tests must distinguish between transient staged files in worktrees and canonical persisted artifacts in the run directory.
Enforcement:
Independent-review workers write review reports under worktree-local `.deepreview/`; execute prompts write triage/plan/verification/status/summary artifacts under worktree-local `.deepreview/artifacts/`; the orchestrator copies canonical artifacts into `~/deepreview/runs/<run-id>/round-<round>/` before consuming them; after successful execute-stage validation and any required local commit, the orchestrator writes `round.json`. Integration coverage requires fake-codex prompt output paths to stay within the worker cwd.
References:
`internal/deepreview/orchestrator.go`, `internal/deepreview/cli.go`, `internal/deepreview/cli_test.go`, `internal/deepreview/integration_test.go`, `docs/spec.md`, `docs/architecture.md`

Decision:
Internal deepreview operational artifacts must never be delivered to source repositories, and untracked runtime directories created during execute rounds must not affect round change detection or auto-commit decisions.
Context:
Execute-stage prompts require intermediate files (triage/plan/verification/status/summary) for orchestration, and round-local verification often creates temporary caches or helper state (`.tmp/`, `.codex/`, `.claude/`, cache dirs) that are not repository changes.
Rationale:
Preventing operational artifact delivery keeps PRs clean, avoids leaking internal review machinery, and prevents false-positive extra rounds caused by untracked runtime state being mistaken for meaningful repository changes.
Trade-offs:
Adds worktree-local git exclude management plus execute-stage cleanup logic to separate operational files from real repository changes. The excludes are installed only for operational paths the source repository does not already track, so repositories that intentionally version content under paths like `.tmp/` keep those files deliverable; `.deepreview/` remains reserved and blocked; and known nested runtime caches (for example `.tmp/go-build-cache/`) stay protected even inside a repo-owned parent directory unless the source repository already tracks that subtree.
Enforcement:
Execute stage installs deepreview-managed untracked excludes for operational directories that are untracked in the candidate repository, removes round-local operational directories before final commit checks when the repository does not already own those paths, auto-commits remaining repository changes when needed, validates no internal `.deepreview/` artifact paths exist in candidate commit range, and blocks delivery only for newly introduced operational-artifact paths that were absent from the source branch.
References:
`internal/deepreview/orchestrator.go`, `internal/deepreview/gitops.go`, `prompts/execute/03-cleanup-summary-commit.md`, `internal/deepreview/integration_test.go`

Decision:
After run completion, CLI must print an explicit terminal summary with delivery outcome and clickable URL where applicable.
Context:
Full-screen TUI exits back to shell; without post-run summary users can miss final result details (PR created, pushed commits, or no-op delivery).
Rationale:
A concise completion line with direct URL improves UX and reduces ambiguity immediately after command return.
Trade-offs:
Adds a small amount of additional stdout output after successful runs.
Enforcement:
CLI prints run completion summary, including PR URL in PR mode when available, delivery commits URL for delivered runs, commits URL in yolo mode, or explicit skipped-delivery reason for no-op runs. When PR mode completes without a returned PR URL, CLI prints manual-recovery guidance instead of a vague success line.
References:
`internal/deepreview/cli.go`, `internal/deepreview/integration_test.go`

Decision:
Allow concurrent deepreview runs across different repositories and across different source branches of the same repository, but enforce a per-repository+source-branch run lock.
Context:
Users need same-project concurrency for different branches, but deepreview startup replaces its managed clone and creates candidate refs/worktrees. Shared same-branch state would race, while branch-isolated state can safely proceed in parallel.
Rationale:
Cross-repo and cross-branch concurrency improve throughput. Branch-scoped managed clones plus branch-scoped locks keep fresh-clone setup, candidate refs, and worktree cleanup isolated while still blocking duplicate runs against the exact same repo+branch.
Trade-offs:
Branch isolation uses more disk because the workspace now keeps separate managed clones per active source branch. Operators still cannot run two deepreview sessions for the exact same repo+branch at the same time; they must wait for the active run to complete (or stale lock recovery to occur).
Enforcement:
Run startup acquires a repo+branch-scoped lock file under `~/deepreview/locks/<owner>/<repo>/<branch-key>.lock`, and each source branch uses its own managed clone path under `~/deepreview/repos/<owner>/<repo>/branches/<branch-key>`. Lock creation fails with a clear error if another active run holds the same repo+branch lock; stale locks are reclaimed.
References:
`internal/deepreview/orchestrator.go`, `internal/deepreview/orchestrator_test.go`, `README.md`

Decision:
Run deepreview Codex execution with the operator's normal local Codex configuration.
Context:
Deepreview was forcing a pinned model/reasoning pair and a separate deepreview-specific execution style. That diverged from how operators normally run `multicodex exec` or `codex exec` locally.
Rationale:
Using the normal local Codex configuration keeps deepreview aligned with direct terminal usage, removes deepreview-specific execution drift, and simplifies the runner.
Trade-offs:
Prompt execution behavior now depends more directly on the operator's local Codex configuration and machine environment.
Enforcement:
Codex command construction uses only the minimal deepreview orchestration flags, preserves the inherited local environment, and help/spec text describe prompt execution as using the operator's normal local Codex config/profile.
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
`RunCLI` prints the original unsupported token, with regression coverage asserting representative absolute-path input is preserved locally.
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
Let delivery rely on execute-stage verification plus Codex-led PR preparation/privacy remediation; do not run a separate detached delivery quality-gate stage.
Context:
The detached delivery-gate stage duplicated work the execute prompt already tries to do, added more orchestration branches, and blocked delivery for checks that are better handled inside the main Codex verify/tidy workflow.
Rationale:
Keeping final delivery focused on Codex-led prep plus privacy remediation removes a dedicated worktree/check stage, reduces orchestration bloat, and still preserves explicit execute-stage verification and delivery-surface privacy checks.
Trade-offs:
Delivery no longer has an extra detached safety net for `pre-commit` or `setup_env.sh`; repositories that want those checks should have Codex run them during execute verification or PR preparation when appropriate.
Enforcement:
Delivery stage does not create a detached quality-gate worktree. In PR mode it runs Codex branch preparation, then bounded privacy remediation, then push/PR actions. Execute-stage verification and delivery-surface privacy checks remain the enforced gates.
References:
`internal/deepreview/orchestrator.go`, `prompts/delivery/pr-prepare.md`, `prompts/delivery/privacy-fix.md`, `internal/deepreview/privacy_test.go`, `docs/spec.md`, `README.md`

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

Decision:
Run Codex prompts with the operator's normal local Codex configuration and inherited local environment.
Context:
Deepreview previously forced a separate deepreview-specific execution style, pinned model/reasoning settings, and worktree-local temp/cache overrides. That diverged from how operators normally run `multicodex exec` or `codex exec` locally and made verification behavior more complex than necessary.
Rationale:
Using the normal local Codex configuration keeps deepreview behavior aligned with direct terminal usage, simplifies the runner, and avoids deepreview-specific assumptions about network reachability or cache layout.
Trade-offs:
Deepreview no longer forces extra isolation for prompt subprocesses beyond worktree separation, so verification behavior now depends more directly on the operator's local Codex configuration and machine environment.
Enforcement:
Codex runner invokes the resolved launcher with the minimal deepreview orchestration flags only, preserves the inherited local environment, and documentation/prompt guidance treat network/module access as environment-dependent rather than assuming a deepreview-managed offline environment.
References:
`internal/deepreview/codex.go`, `internal/deepreview/process.go`, `internal/deepreview/codex_test.go`, `internal/deepreview/process_test.go`, `internal/deepreview/integration_test.go`, `cmd/fake-codex/main.go`, `prompts/review/independent-review.md`, `prompts/execute/02-execute-verify.md`, `docs/spec.md`

Decision:
Treat `source branch == default branch` runs as current-state repository audits rather than zero-diff branch reviews.
Context:
Self-review runs against `main` can have no branch diff even when the repository still has current-state issues worth auditing. Recent artifacts showed some reviewers stopping at "no diff" and missing repo-level findings unless another worker ignored that framing.
Rationale:
Reframing self-review runs as repo audits preserves reviewer effort for the actual current codebase instead of wasting capacity on proving the diff is empty.
Trade-offs:
Default-branch reviews may inspect more repository surface area and take longer than a strict diff-only pass.
Enforcement:
Independent-review prompt rendering now injects explicit self-audit mode guidance when source and default branch names match, and integration tests require fake-codex to see that prompt mode.
References:
`internal/deepreview/orchestrator.go`, `internal/deepreview/orchestrator_test.go`, `internal/deepreview/integration_test.go`, `prompts/review/independent-review.md`, `docs/spec.md`

Decision:
Reject non-GitHub repo identities in default `pr` mode before round execution begins.
Context:
`deepreview` accepts local repo paths whose `origin` remote is a local filesystem path so managed clones can still be created from local sources. PR delivery, however, is implemented only through GitHub URLs plus `gh pr create`.
Rationale:
Fail-fast validation is simpler and safer than allowing a full multi-round run to proceed and then discovering at final delivery that the repo has no valid PR target.
Trade-offs:
Users reviewing local-only remotes must choose a non-PR flow instead of relying on late delivery failure.
Enforcement:
`NewOrchestrator` rejects `--mode pr` when repo identity resolution produces a non-GitHub/local synthetic repo identity; help text and spec call out the restriction explicitly.
References:
`internal/deepreview/orchestrator.go`, `internal/deepreview/orchestrator_test.go`, `internal/deepreview/cli.go`, `README.md`, `docs/spec.md`
