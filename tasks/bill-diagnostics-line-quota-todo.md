# Todo: Bill Diagnostics and LINE OA Usage

## Handoff

- Last completed: D09 LINE Notifications UX (commit pending in this checkpoint);
  D07/D08 are `63f9021`, D06 is `1436e4f`, D04/D05 are `7363e76`, D03 is
  `a9c729b`, D02 is `5fae167`, and D01 is `bbfc64d`.
- Active task: D10 full verification and browser QA.
- Branch/HEAD at D10 start: `codex/marketplace-units-conversion` / `63f9021`.
- Preserved user work: modified `AGENTS.md`, `docs/current-state.md`,
  `docs/nextstep-server-deploy-flow.md`; untracked `.serena/`, `artifacts/`,
  `scripts/__pycache__/`, `tasks/plan.md`, and `tasks/todo.md`.
- Migration baseline: 093 is the latest checked-in migration; use 094 only if
  it remains free when D02 starts.
- Tests: focused backend LINE service/handler race tests, frontend quota,
  audit/grouping tests, TypeScript, and focused ESLint pass. Limited/unlimited/
  none, 401/429/timeout, partial-OA failure, cache, singleflight, stale fallback,
  refresh cooldown, safe attempt grouping, and lazy Admin diagnostics are covered.
- Feature/tenant state: no runtime or tenant setting changed; not deployed.
- Blocker: none.
- Next action: run broad backend/frontend verification, migration replay,
  performance/security checks, then browser QA before the four-tenant rollout.

For every completed task update this block with exact commits, files, focused
and broad tests, tenant scope, production evidence, known residual risk, and one
next action.

## D00 — Durable plan, todo, and snapshot

- [x] Create dedicated plan/todo files without overwriting existing tasks.
- [x] Record branch, HEAD, dirty files, and migration baseline.
- [x] Add the durable Handoff block.
- [x] Commit D00 without staging unrelated user files (`265b0fa`).

## D01 — Fixtures and security contract

- [x] Add an anonymized 17-event `BF-INV26090002` regression fixture.
- [x] Prove one immutable attempt, six transient failures, final success.
- [x] Define safe header allowlist and recursive secret/PII redaction limits.
- [x] Define staff/admin diagnostics and package V1 contracts.
- [x] Add failing grouping, redaction, depth, field-count, and size tests.

## D02 — Durable SML exchange repository

- [x] Add the next free additive migration and migration replay test.
- [x] Insert exchange start under the parent-attempt lock with unique sequence.
- [x] Finalize exchange independently with bounded sanitized response evidence.
- [x] Keep repository failures separable from attempt/core transactions.
- [x] Verify parent locking, unique sequence, and old-attempt/no-backfill behavior.

## D03 — Instrument immutable SML sends

- [x] Instrument Sale Invoice and Sale Order immutable attempt paths.
- [x] Preserve legacy client method contracts.
- [x] Capture every retry, transport error, HTTP error, and business failure.
- [x] Add attempt ID and payload hash to new audit success/failure events.
- [x] Bound SML response reads to 2 MiB.
- [ ] Measure local overhead against the pre-change baseline (final D10 gate).

## D04 — Safe historical resolution

- [x] Compute resolution and retry eligibility on the backend.
- [x] Mark failures from a subsequently sent attempt as resolved.
- [x] Permit core retry only for the unresolved current attempt before core success.
- [x] Remove/hide retry for resolved rows in `/logs` and never add it to timeline.
- [x] Keep profile-only and stock-only recovery separate from core resend.

## Checkpoint A — Core safety

- [ ] Diagnostic failures cannot change SML outcomes or trigger a resend.
- [ ] Concurrent retry cannot duplicate exchange sequence.
- [ ] Timeout after commit remains unknown until reconciliation.
- [ ] Legacy hash/idempotency/Profile tests pass.
- [ ] Stored/API/copied diagnostics contain no secret or unredacted buyer PII.

## D05 — Role-safe bill APIs

- [x] Add server-generated `sml_summary` for staff.
- [x] Return raw bill/SML fields only to admins.
- [x] Apply role-aware timeline sanitization with the 200-row cap unchanged.
- [x] Add admin-only SML diagnostics endpoint and authorization tests.
- [x] Restrict `/logs` raw/dev mode to admins.

## D06 — Bill timeline UX

- [x] Group SML events only when attempt/trace/document evidence is unambiguous.
- [x] Render the controlled bill as success after six retries.
- [x] Expand to chronological event details with resolved wording.
- [x] Add sanitized copy with download fallback.
- [x] Explain missing per-exchange evidence on historical records.
- [ ] Verify keyboard, screen reader, reduced motion, desktop, and 390px.

## D07 — LINE quota client

- [x] Call LINE quota and consumption endpoints with response validation.
- [x] Support limited/unlimited/none and clamp remaining to zero.
- [x] Bound concurrency to four, request timeout to 5s, handler to 8s.
- [x] Retry once only for 429/5xx, respecting Retry-After up to 2s.
- [x] Never expose token or raw unsafe LINE error bodies.

## D08 — LINE quota cache and API

- [x] Add a 30-second per-OA cache and concurrent-request coalescing.
- [x] Let manual refresh fetch fresh LINE data with a 10-second cooldown.
- [x] Serve last success for up to 15 minutes with `is_stale=true` on failure.
- [x] Keep failures independent per OA and Overview independent from quota.
- [x] Add structured bounded logs for status and duration.

## D09 — LINE Notifications UX

- [x] Load Overview and quota independently.
- [x] Show used/limit, remaining, freshness, and stale/error states per OA.
- [x] Support warning/full, unlimited, and none without color-only meaning.
- [x] Add safe refresh loading/cooldown behavior.
- [x] Remove user-facing hardcoded monthly quota copy.
- [x] Verify no polling or request waterfall in the implementation; browser
  network verification remains in D10.

## Checkpoint B — UX, performance, and failures

- [ ] Timeline is compact and all 17 raw events remain inspectable.
- [ ] Staff package is sanitized; admin evidence contains no credentials.
- [ ] Multiple LINE OAs remain independent.
- [ ] LINE failure does not block settings or message delivery.
- [ ] Initial rendering does not wait for quota.
- [ ] Diagnostics query p95 is at most 300ms.

## D10 — Full verification

- [ ] `go test ./...`, `go test -race ./...`, and `go vet ./...`.
- [ ] Focused frontend tests, all Node tests, lint, and production build.
- [ ] `scripts/check_sales_only_runtime.sh`.
- [ ] Migration replay from empty and migration-093 snapshots.
- [ ] Security, fault injection, concurrency, and response-boundary tests.
- [ ] Browser QA desktop/390px, keyboard, accessibility, console, and network.
- [ ] Compare SML send latency and revert unjustified overhead.

## D11 — Four-tenant deployment

- [ ] Back up runtime and database for Demo, AOY, Lanboon, and Ploy.
- [ ] Pass canary build, migration, health, and preflight checks.
- [ ] Deploy the same commit/migration without changing tenant configuration.
- [ ] Verify health, DB auth, frontend assets, and recent severe errors.
- [ ] Verify AOY historical grouping and live LINE quota freshness.
- [ ] Verify one future SML send records exchanges without duplicates.
- [ ] Record exact backups, release commit, evidence, and rollback target.
