# deepreview spec

This document defines the canonical runtime and product contract for `deepreview`. Keep it evergreen and consistent with implementation.

## Definitions
- **deepreview**: the CLI orchestrator in this repository.
- **target repo**: the repository being reviewed.
- **source branch**: the branch chosen for the deepreview run.
- **default branch**: repository default branch (for example `main` or `master`).
- **independent review**: one independent Codex review run in one isolated worktree.
- **execute stage**: the per-round Codex stage that triages review findings, applies selected changes, verifies them, and records round artifacts in a fresh execute worktree.
- **delivery stage**: the final read-only Codex assessment of local merge readiness before deepreview performs remote delivery actions.
- **round**: one independent review stage plus one execute stage, ending in local commit/no-push unless it is final delivery.
- **material improvement**: a high-confidence change that clearly improves correctness, security, maintainability, simplicity, documentation accuracy, or delivery readiness in a meaningful way.

## Required invariants
- deepreview documentation and code must not reference external inspiration project names; patterns may be reused without cross-project coupling in artifacts.
- project/tool naming is always `deepreview` (lowercase, one word).
- this repository is public, and tracked content, outgoing history, automation, and delivery surfaces must remain publication-safe.
- no secrets, confidential data, or personal information may be committed.
- supported host operating systems are macOS and Linux only; Windows compatibility is out of scope and should be removed rather than maintained.
- deepreview operates only in managed workspace paths under `~/deepreview`.
- deepreview must not operate in the user's own active checkout.
- deepreview must isolate managed repository clones and run locks by repo plus source branch, so different branches of the same repo can run concurrently while same-branch runs remain serialized.
- deepreview-managed commits must use the operator's resolved Git identity from the source repository Git config when present, otherwise the operator's global Git config, and must not depend on host GPG signing configuration.
- if repo/source-branch are omitted, deepreview may infer them from current local GitHub repo context.
- when launched via wrappers that `cd` before execution, repo and branch inference uses the current checkout, or `DEEPREVIEW_CALLER_CWD` when that variable is set. An invalid non-empty caller CWD fails fast. deepreview does not read `OLDPWD`.
- resolved repo identity must model GitHub-backed and filesystem-local sources explicitly; GitHub repos keep their stable `owner/repo` slug while filesystem-local repos use a deterministic filesystem identity derived from the canonicalized clone source.
- in `pr` mode, the resolved repo identity must be GitHub-backed; local filesystem origin remotes are rejected before round execution.
- source branch resolution requires local readiness checks when it targets the current local branch context (inferred branch, or explicit `--source-branch` matching the current branch in a supported local repo context): no tracked local changes, and the local HEAD matches the remote branch. The check uses `git ls-remote` from an empty directory. It does not update local refs or `FETCH_HEAD`. Repo inference and readiness checks never run repository code: they disable fsmonitor, hooks, filter drivers, lazy fetches, and transports, and repository transport config does not apply to `ls-remote`. `review` checks `--trust-repo` before readiness.
- deepreview keeps orchestration simple with bounded self-healing only: inactivity-based worker restarts are allowed with explicit per-worker restart caps.
- deepreview resolves the Codex prompt launcher by name instead of by hardcoded local repo path: use `multicodex` whenever it is available on `PATH`; otherwise fall back to `codex` unless `DEEPREVIEW_REQUIRE_MULTICODEX` is set.
- fresh multicodex-backed prompt contexts use normal `multicodex exec` profile selection, but once a prompt creates a resumable Codex thread, later deepreview resumes for that logical context must stay on the same selected multicodex profile.
- codex prompt executions use a fixed timeout of 3600 seconds per prompt.
- deepreview runs must be interruptible via `Ctrl+C` at any point; on interrupt, active worker commands are terminated immediately, deepreview emits a failure summary for the interrupted run, then lock/worktree cleanup and final transient-worktree scrubbing run before process exit.
- round loop runs up to `--max-rounds` (default `5`) total execute rounds and may stop early. A requested value outside `1` to `100` is rejected. deepreview does not reduce it.
- independent reviews run in independent worktrees.
- independent review concurrency defaults to `4`. A requested value outside `1` to `64` is rejected. deepreview does not reduce it.
- each successful independent-review worker must emit one markdown review report.
- independent review rounds require full worker coverage: required successful workers = `concurrency`.
- independent-review reports must stay focused on high-confidence, material issues or opportunities. Accepted change types may include bug fixes, security/safety fixes, substantial simplifications, high-value refactors, meaningful cleanup, and documentation alignment. Low-value polish, speculative hardening, and style churn are out of scope.
- independent-review and execute/delivery Codex workers are monitored for activity signals (stdout/stderr output plus filesystem/git-change evidence).
- Codex and Git subprocesses run in a new process group. Cancellation or a timeout kills that group. A pipe held open after the process exits or the context ends stops the wait after 2 seconds. That bound makes the call return. It does not by itself prove that a detached descendant has exited.
- Worktree teardown uses a 15-second cleanup context that is not the canceled run context. Each git cleanup command stops after 5 seconds. deepreview deletes a path only when its gitdir is inside that repository's `.git/worktrees` directory and points back at that same worktree path after parent symlinks are resolved. A missing directory still prunes its registration. Cleanup errors name any residual path and join the stage error. The operator checkout is never a cleanup target.
- Codex stdout is parsed while the process runs. An event line over 1 MiB, malformed JSON, or a failed log write cancels the process. Memory keeps the thread id, the profile, agent messages, and at most 256 KiB from each stream. When a log prefix is set, the full stream is written there. Attempt 1 uses that prefix. Later attempts write `<prefix>.attempt-<n>` and leave the earlier logs in place.
- if a worker is inactive for the configured timeout, deepreview cancels and restarts that worker up to the configured restart cap.
- before retrying a prompt after inactivity, deepreview discards the detached attempt worktree and recreates it from the immutable baseline SHA; abandoned attempt commits never move the candidate branch.
- each execute stage runs in a fresh worktree and one fresh Codex context.
- each delivery stage runs in a fresh worktree and one fresh Codex context.
- independent-review workers always use fresh contexts isolated from one another.
- execute/delivery history must not carry across rounds or across stages.
- independent-review workers use one shared independent-review prompt template.
- when the source branch equals the default branch, independent review treats branch diff as orientation only and continues as a current-state repository audit.
- each execute stage runs an ordered multi-prompt queue in one Codex chat context.
- normal execute queue continuity applies only within a healthy queue attempt; if an execute prompt is retried after inactivity, that retry must restart in a fresh Codex context rather than resuming the stalled thread.
- execute prompt 1 receives review artifact paths plus a compact manifest; avoid large injected review-summary blocks and let Codex inspect full reports directly when needed.
- execute prompt 1 (triage and plan) treats independent reviews as inputs, not gospel, investigates candidate items individually before acceptance, accepts only independently validated, high-confidence, material items, and produces the round plan for the round.
- execute prompt 1 writes human triage notes and a `round-triage.json` decision. deepreview validates that decision before prompt 2. An accepted item needs a stable id, material impact, high confidence, and evidence. An explicit `no_op` with an empty item list is valid. Empty, malformed, or under-specified decisions fail the round before prompt 2 and do not move the candidate ref.
- an independent review counts as successful only when its report is nonempty and contains `material_findings_found: yes|no` and `merge_readiness: ready|needs_fixes`.
- stage artifacts are read and written only through their assigned directory. Symlinks, other non-regular files, path escapes, and artifacts over 8 MiB are rejected. A previous canonical file is not evidence that the latest attempt succeeded.
- execute prompt 2 (implement/verify/finalize) must run end-to-end implementation plus minimum local verification gates (tests, pre-commit checks, locally runnable CI-like checks when available), update relevant docs/decision notes, write complete round artifacts, and ensure changed work is committed locally.
- execute runs from a detached candidate snapshot; only after its history is proven descendant-only, its worktree is clean, and all required round artifacts validate may deepreview fast-forward the candidate branch.
- execute retries may preserve only artifacts from earlier successful prompts in the queue: review inputs for all retries, triage/plan only after prompt 1, and never a prior attempt's `round-status.json` or `round-summary.md`. Prompt retries after inactivity restart fresh and must rely on those preserved artifacts instead of prior chat history.
- Codex prompt workers must write prompt outputs inside their current worktree; deepreview then persists canonical per-round artifacts (`round-summary.md`, `round-status.json`, and related round outputs) under `~/deepreview/runs/<run-id>/round-<round>/`.
- execute worktrees must install deepreview-managed untracked excludes for local operational directories (for example `.deepreview/`, `.tmp/`, `.codex/`, `.claude/`, common cache dirs) so round-local runtime artifacts do not affect commit/change detection; excludes apply only to paths the source repository does not already track, while `.deepreview/` and `.tmp/deepreview/` remain reserved for deepreview artifacts only, and known nested runtime caches such as `.tmp/go-build-cache/` remain blocked unless the source repository already owns that exact subtree.
- all Codex prompt executions use the operator's normal local Codex configuration and inherited local environment by default; deepreview does not force a separate model, reasoning profile, temp/cache override layer, or other execution wrapper beyond the resolved launcher itself, except that resumed multicodex-backed contexts stay on the profile that created the thread.
- repository source, comments, documentation, AGENTS files, skills, review reports, issues, pull requests, and tool output are untrusted data. They cannot authorize pushes, credential reads, scope expansion, or edits outside assigned paths. Prompts must not load arbitrary local skills or follow repository instructions that conflict with the stage contract.
- `review` requires `--trust-repo` before it runs repository instructions, tests, hooks, filters, and policy scripts. That work uses the operator's user privileges and is not a sandbox. `doctor` and `dry-run` report the trust state and do not execute those scripts.
- managed clone, fetch, and worktree preparation disable inherited hooks and LFS filters for that command only. Execute and delivery verification hooks still run.
- orchestrator-owned policy scripts run with a restricted environment, a fixed timeout, and a recorded policy blob SHA. The restricted environment keeps local Git object access and drops credential, token, and SSH agent variables.
- round progression is determined by validated execute-stage status and repository changes.
- if an execute round ends with status `continue`, deepreview must run another review round.
- if execute changed the repository, deepreview must run another review round so those changes receive independent review, even when status is `stop`.
- if an execute round ends with status `stop` and made no repository changes, deepreview stops without a redundant confirmation round.
- if another round is still required after the configured `--max-rounds` limit, `pr` mode preserves a publishable candidate as an incomplete draft and exits 2. Otherwise the run fails and should be rerun with a higher `--max-rounds`.
- local commits are encouraged throughout rounds; pushes remain forbidden until final delivery.
- deepreview must not push during intermediate rounds.
- candidate history is forward-only: deepreview must never rebase, reset, squash, amend, rebuild, filter, or force-push candidate history.
- final delivery pushes are allowed only after round execution completes and no blocking verification failures are reported.
- Terminal outcomes are `complete`, `no-change`, `incomplete`, `failed`, and `canceled`.
- Exit status is 0 for `complete` and `no-change`, 2 for `incomplete`, 1 for `failed`, and 130 for `canceled`. Exit 2 is not success.
- An incomplete result can still preserve a draft pull request. The pull request URL and the original blocker stay visible.
- Publication state is `not-attempted`, `pushed`, or `pr-created`. A commits URL is shown only after a successful push. A pull request URL is shown only after the pull request is created.
- A recorded push or pull request stays recorded when a later summary or run-health write fails. Those writes are diagnostic and do not hide the remote mutation.
- Run-health counts stderr in bounded chunks. A long line or a missing log is diagnostic. It does not fail a published run.
- default delivery mode is `pr` and must not push source branch directly.
- in delivery, Codex owns final local merge-readiness assessment inside the worktree, while deepreview owns final pre-publication validation, remote push / PR creation, and bounded post-create mergeability validation.
- in `pr` mode, run one fresh Codex delivery prompt in a candidate-branch worktree. That prompt must:
  - inspect the candidate diff and prior round artifacts
  - run any final local merge-readiness checks still needed
  - validate final local merge-readiness without mutating tracked repository content or branch history
  - keep the publish target fixed to the reviewed candidate branch
  - if it detects a blocker that would require tracked-code edits or history cleanup, report that blocker instead of repairing it in delivery
  - report whether local delivery preparation is complete or incomplete
  - write only local-readiness result fields needed by the orchestrator (mode and incomplete status/reason); `delivery_branch` remains reserved and must stay unset
- before and after the delivery prompt, deepreview must validate the reviewed candidate branch itself before any push or PR creation occurs.
- post-prompt delivery validation must inspect the exact candidate ref that deepreview will publish, not a stale candidate-branch diff or mutable post-push remote-tracking ref.
- post-prompt delivery validation must also enforce repo-native outbound history policies against the candidate publish range when the repository defines them.
- final publishability validation must inspect every outgoing commit, including sensitive content and blocked operational artifacts introduced and removed before the current tip.
- publication pushes that reviewed commit SHA to `refs/heads/<target>` and confirms the remote ref matches it. A candidate branch that moved after review is not published.
- publication accepts only the prepared GitHub repository, over HTTPS or `git` SSH, or the prepared local origin path. A token, unexpected port, option-shaped locator, changed push URL, or a source or default branch that moved after preparation stops publication.
- publication scans every changed path, including rename sources and unusual filenames, and accepts only placeholder or GitHub noreply author and committer identities. It does not rewrite commits.
- sensitive-text policy failures report the rule, file, and line only. They do not print the matched text. A missing, unreadable, or unscannable input fails closed.
- a tracked-content or outgoing-history publication blocker fails delivery before push; deepreview does not rewrite candidate history to repair it.
- after PR creation in `pr` mode, deepreview may poll mergeability briefly to let transient GitHub states settle before reporting terminal success or failure.
- in `pr` mode, if the run exits before normal completion after producing deliverable repository changes, deepreview should publish a draft PR to preserve the candidate branch state only when the candidate still passes final publishability validation; if publishability remains blocked, fail without push or PR creation.
- incomplete draft publication may preserve only changes from completed execute rounds with valid required artifacts; commits from failed or invalid detached attempts never advance the candidate branch and are not published.
- incomplete draft PR titles must start with `[INCOMPLETE] ` before the normal `deepreview:` title.
- incomplete draft PR bodies must explicitly state that the PR is incomplete, why delivery did not finish cleanly, and what remains to be done before merge.
- incomplete delivery/reporting should distinguish current-tip blockers from PR-range/history-only blockers.
- `yolo` mode is optional opt-in for direct push to source branch.
- when `yolo` targets the default branch, deepreview runs a push-permission dry-run preflight before round execution.
- managed repo checkout is replaced with a fresh clone each run to avoid stale state.
- managed repo checkout paths are branch-scoped under the workspace so fresh-clone setup for one source branch cannot race another source branch of the same repo.
- Codex auth should rely on local Codex CLI session/subscription, not repository-stored API keys.
- `DEEPREVIEW_REQUIRE_MULTICODEX=1` requires `multicodex exec` to be available on `PATH`; when unset, deepreview falls back to `codex exec` only if `multicodex` is unavailable.
- all Codex prompt executions (new and resumed threads, including delivery) must use the operator's normal local Codex configuration unless an explicit deepreview override is added in code for a documented reason. Resumed multicodex-backed contexts are one such documented override boundary: they may pin the creating multicodex profile to preserve thread continuity.

## Runtime contract
- command entrypoint: `deepreview`
- primary command: `deepreview review`
- helper command: `deepreview doctor`
- helper command: `deepreview dry-run`
- minimum inputs:
  - none when running inside a valid local GitHub repo context
  - otherwise provide enough explicit context (`<repo>` and/or `--source-branch`) to resolve target repo + source branch
- optional inference override:
  - `DEEPREVIEW_CALLER_CWD` can be set by launch wrappers as an explicit caller-context override when the wrapper changes directories before invoking deepreview; if it is non-empty but does not resolve to a valid local repo context, deepreview must fail fast.
- optional launcher requirement:
- `DEEPREVIEW_REQUIRE_MULTICODEX=1` disables fallback to `codex exec` and fails preflight/doctor if `multicodex exec` is unavailable.
- `DEEPREVIEW_CODEX_BIN` overrides only the codex fallback path used when `multicodex` is unavailable.
- core options:
  - `--concurrency <n>` default `4`, maximum `64`
  - `--max-rounds <n>` default `5`, maximum `100`
  - `--mode <pr|yolo>` default `pr` (case-insensitive value parsing)
  - `--yolo` alias for `--mode yolo`
  - full-screen terminal UI is enabled by default when terminal capabilities are valid
  - when TUI is enabled, deepreview exits the UI automatically on completion and prints the text summary immediately
  - Ctrl+C requests cancellation as an asynchronous UI command. The UI shows CANCELING immediately. If terminal startup fails, or the UI exits while its worker is still running, deepreview cancels that worker and waits up to 30 seconds for it to exit. A worker that has already finished is not canceled again.
  - The timeline keeps the active stage visible, including on a small terminal. It shows the current round out of `--max-rounds` and, during independent review, reviewers completed out of reviewers required. It does not show a percent complete, because later rounds are conditional.
  - before printing the completion summary after a TUI run, deepreview clears the terminal and prints summary text from the top-left cursor position
  - `--no-tui` force structured text progress logs
- worker-activity resilience env knobs (applies to all Codex workers):
  - `DEEPREVIEW_REVIEW_INACTIVITY_SECONDS` default `300` (5 minutes; `0` disables inactivity restarts)
  - `DEEPREVIEW_REVIEW_ACTIVITY_POLL_SECONDS` default `15`
  - `DEEPREVIEW_REVIEW_MAX_RESTARTS` default `1`

Helper command behavior:
- `doctor` runs non-mutating preflight checks for local tools, auth state, prompt assets, and remote source-branch reachability. It does not create a run directory.
- `doctor` validates the actual prompt launcher that deepreview would use (`multicodex exec --help` when routed through multicodex, otherwise `codex exec --help`) and then checks matching auth state (`multicodex status` or `codex login status`).
- `doctor` and `dry-run` reuse the same repo/branch inference and local-current-branch readiness validation rules as `review`, so they fail early on dirty or unsynchronized local source-branch state when that local context is authoritative.
- `dry-run` prints resolved run context and stage order without running Codex, mutating git state, or creating a run directory.

## Round artifact contract
- Authoritative round record path: `~/deepreview/runs/<run-id>/round-<round>/round.json`
- Required round-record schema:
  - `round`: positive integer
  - `summary`: non-empty string naming the round summary artifact
  - `status`: valid round-status object
- A round counts as completed for final reporting only when `round.json` exists and parses successfully.
- Invalid or missing `round.json` means the round did not complete successfully for reporting purposes.
- Incomplete-draft publication and final reporting must derive completed-round counts and latest-decision claims only from valid `round.json` records; if no valid round records exist, report zero completed rounds and omit latest-decision claims.

## Round status artifact contract
- Canonical status file path: `~/deepreview/runs/<run-id>/round-<round>/round-status.json`
- Required schema:
  - `decision`: enum `continue|stop`
  - `reason`: non-empty string
- Optional fields:
  - `confidence`: number in `[0.0, 1.0]`
  - `next_focus`: string
- This file is an execute-stage artifact for traceability; round-loop control combines its decision with repository change detection.
- Invalid or missing required fields fail the round.

## Delivery naming contract
- delivery branch prefix: `deepreview/`
- local candidate branch prefix: `deepreview/candidate/`
- PR title prefix: `deepreview:`
- final PR title should be concise, human-readable, and explain what changed at a glance.

## Artifact contract
Each run must produce:
- run metadata and final summary
- run-health summary artifacts (`run-health.json` and `run-health.md`) that describe canonical artifact coverage plus observational stderr/log noise metrics without changing delivery classification
- per-round review/execute logs while active
- `review-<worker-id>.md` independent review outputs for each active round
- per-round execute outputs (triage decisions, change plan, verification report, round summary, round status flag, authoritative round record)
- delivery outcome metadata
- per-round local commits for changed work (one or more commits allowed; no empty commits)
- final PR title/body artifacts when PR delivery runs
- successful terminal runs must always leave a root `final-summary.md`, including incomplete-draft outcomes

Cleanup policy:
- aggressively remove review/execute/delivery worktrees and transient round artifacts once they are no longer needed.
- keep canonical markdown/json artifacts by default.
- workspace `runs/` directories, each run directory, and lock directories are mode 0700. Private artifacts, including canonical reports, metadata, and locks, are mode 0600.
- raw Codex logs (`*.stdout.jsonl` and stderr captures) are off unless `--retain-raw-logs`. When retained, they stay private inside the run directory and can contain source, credentials, and prompts. Delete that run's directory under `runs/` when you no longer need them.
- `run-health.json` and `run-health.md` use the same sanitized public text.

## Safety contract
- never commit tokens, credentials, or private keys.
- never emit personal information in public delivery surfaces (PR title/body, commit messages, delivery summaries, comments, or committed code/docs).
- treat committed docs/artifacts as potentially public.
- in PR mode, delivery/public text and deliverable diffs must pass privacy-hygiene checks before final PR completion.
- publication scans cover the full outgoing commit range, not only the aggregate current-tip diff.
- keep local terminal progress/error output literal for operator debugging; privacy redaction is enforced at delivery/public surfaces.
- fail fast on blocking verification failures.

## Failure-handling contract
- if any independent-review worker does not complete successfully after bounded inactivity restarts, fail the run.
- deepreview does not continue with partial independent-review coverage; all configured workers must succeed.
- if execute verification fails, fail the run and do not deliver.
- if `pr` mode delivery fails after final round succeeds, fail the run and do not perform fallback pushes.
- in `yolo` mode, do not push when verification fails.
- if another round is still required after `--max-rounds`, `pr` mode should publish an incomplete draft PR only when deliverable repository changes exist and final publishability validation passes; that preserved draft exits 2 and keeps the original blocker. Otherwise it fails without push/PR. `yolo` mode still fails with guidance to rerun deepreview using a higher `--max-rounds`.
- verification strategy is codex-led: Codex should attempt repo tests, pre-commit checks, and locally runnable CI-like checks when available, then report what ran and outcomes.

## PR body contract (default PR mode)
PR bodies should include these sections in the final generated output:
- `## summary`
- `## what changed and why`
- `## round outcomes`
- `## verification`
- `## risks and follow-ups`
- `## final status`
- incomplete draft PR bodies must also include an explicit warning not to merge yet plus the blocking reason and latest round state
- do not embed individual independent-review reports or full execute artifact dumps in PR description
- final PR body text must pass privacy checks (no personal information, secrets, or private local machine paths)
- if generated PR text exceeds GitHub PR body limits, deepreview must fall back to a compact body automatically

## PR title contract (default PR mode)
- final PR title is generated during delivery and applied when deepreview creates the PR.
- final PR title must remain prefixed with `deepreview:`.
- final PR title must be concise, concrete, and human-readable (not generic boilerplate).
- incomplete draft PR titles must be prefixed with `[INCOMPLETE] ` ahead of the normal `deepreview:` prefix.
- final PR title text must pass privacy checks (no personal information, secrets, or private local machine paths).

## Prompt-template contract
- Prompt templates are file-based and unversioned in the repository. The binary embeds them at build time.
- Prompt source directory is `prompts/`.
- With `DEEPREVIEW_PROMPTS_ROOT` unset, deepreview reads the embedded pack. When that variable is set, it must name a directory and that directory replaces the pack. Executable-adjacent directories and a target repository `prompts/` directory are never selected automatically.
- Independent review stage uses one shared template: `prompts/review/independent-review.md`.
- Execute stage uses an ordered queue listed in `prompts/execute/queue.txt`.
- Delivery stage uses one shared template: `prompts/delivery/01-deliver.md`.
- Default execute queue order:
  - `01-triage-plan.md`
  - `02-implement-verify-finalize.md`
- Execute queue prompts must run sequentially in one Codex chat context for the round.
- Execute prompts must receive review artifact paths plus a compact manifest in prompt context.
- Prompt rendering must support deterministic template variables (for example `{{ROUND_NUMBER}}`) for repo/branch metadata, worktree paths, round metadata, artifact paths, and commit message templates.
- Any unresolved template variable at render time fails the run immediately.

## Codex autonomy contract
- Codex is the primary reasoning engine for review, execute, and delivery decisions.
- Codex is allowed to inspect git history and recent commits/PR context when useful.
- Deepreview should avoid over-hardcoding repo-specific heuristics.
- The orchestrator should stay thin and operational: workspace/worktree management, run locking, stage launching/resume, context retry policy, activity monitoring, artifact validation, and final run classification.
- Repo mutation steps beyond that boundary should be Codex-owned whenever practical.

## Related docs
- pipeline and stage flow details: [architecture.md](architecture.md)
- durable rationale and policy decisions: [decisions.md](decisions.md)
- prompt templates and queue layout: [../prompts/README.md](../prompts/README.md)
