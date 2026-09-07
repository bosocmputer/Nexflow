# Todo: Bill Diagnostics and LINE OA Usage

## Handoff

- Last completed: D11 four-tenant production deployment and browser QA. Feature
  commits are D09 `4db13a8`, D08/D07 `63f9021`, D06 `1436e4f`, D04/D05
  `7363e76`, D03 `a9c729b`, D02 `5fae167`, and D01 `bbfc64d`; final safety fixes
  are `2a6c229`, `00a9c74`, and `fd1270e`. Release evidence was frozen in
  `175cbb1` before deployment.
- Active task: passive post-release observation of the first real SML outbound
  call; this is not a rollout blocker because the locked scope forbids creating
  a synthetic SML document or sending a test LINE message.
- Branch/release: `codex/marketplace-units-conversion` /
  `175cbb11296dd426a66787bbd02523c785bfbce4`.
- Preserved user work: modified `AGENTS.md`, `docs/current-state.md`,
  `docs/nextstep-server-deploy-flow.md`; untracked `.serena/`, `artifacts/`,
  `scripts/__pycache__/`, `tasks/plan.md`, and `tasks/todo.md`.
- Migration: additive/idempotent 094 is committed. Local PostgreSQL 14 replay
  passed from an empty database and from a migration-093 snapshot, including a
  second 094 replay. Production PostgreSQL 16.14 applied it on all four tenants;
  `bill_sml_attempt_exchanges` exists with all three indexes and zero initial
  rows, as expected before the first new SML outbound call.
- Tests: `go test ./...`, `go test -race ./...`, `go vet ./...`, all 59 frontend
  Node tests, TypeScript/build, sales-only guard, and ESLint (0 errors, 35
  pre-existing warnings) pass. Focused post-review race/tests also pass for
  cache invalidation fencing, LINE 5xx/429/401/timeout/oversize, partial-OA
  failure, redaction bounds, immutable retry, and diagnostic isolation.
- Feature/tenant state: Demo, AOY, Lanboon, and Ploy run the exact release SHA.
  The deployment did not change Shopee connections, SML routes, Auto SML,
  LINE recipients, or tenant feature modes. SML API BY BOS was not changed.
- Production evidence: all tenant and edge health probes returned HTTP 200; DB
  authentication, frontend assets, migration state, shared Shopee Gateway
  connectivity, and recent severe-error scans passed. AOY showed real LINE
  quota `18 / 300`, independently loaded from LINE, with the overview request
  not waiting on it. `BF-INV26090002` showed one resolved group, six transient
  failures, final success, no Core resend action, and an Admin diagnostics call
  completed in 53 ms. Desktop and 390x844 QA passed; the accessibility tree
  exposed native buttons/labels and Chrome Console reported zero messages,
  errors, or warnings on the AOY pages.
- Blocker: none.
- Next action: when the first normal production SML send occurs, verify exactly
  one exchange row per outbound call and continue monitoring user feedback;
  do not create a synthetic bill solely for this evidence.

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
- [x] Bound local diagnostic waits to 20 ms per write / 40 ms worst-case by
  contract test; production percentiles remain a D11 observation gate.

## D04 — Safe historical resolution

- [x] Compute resolution and retry eligibility on the backend.
- [x] Mark failures from a subsequently sent attempt as resolved.
- [x] Permit core retry only for the unresolved current attempt before core success.
- [x] Remove/hide retry for resolved rows in `/logs` and never add it to timeline.
- [x] Keep profile-only and stock-only recovery separate from core resend.

## Checkpoint A — Core safety

- [x] Diagnostic failures cannot change SML outcomes or trigger a resend.
- [x] Concurrent retry cannot duplicate exchange sequence.
- [x] Timeout after commit remains unknown until reconciliation.
- [x] Legacy hash/idempotency/Profile tests pass.
- [x] Stored/API/copied diagnostics contain no secret or unredacted buyer PII.

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
- [x] Verify keyboard semantics/screen-reader labels, reduced-motion-safe
  controls, desktop, and 390x844 production layout with no horizontal overflow.

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

- [x] Timeline is compact and all 17 raw events remain inspectable in component
  and grouping regression tests; production visual QA remains D11.
- [x] Staff package is sanitized; admin evidence contains no credentials.
- [x] Multiple LINE OAs remain independent.
- [x] LINE failure does not block settings or message delivery.
- [x] Initial rendering does not wait for quota.
- [x] Diagnostics query stayed within the 300 ms release budget in focused
  tests and the live AOY request completed in 53 ms; long-running production
  percentile monitoring continues after release.

## D10 — Full verification

- [x] `go test ./...`, `go test -race ./...`, and `go vet ./...`.
- [x] Focused frontend tests, all Node tests, lint, and production build.
- [x] `scripts/check_sales_only_runtime.sh`.
- [x] Migration replay from empty and migration-093 snapshots locally.
- [x] Security, fault injection, concurrency, and response-boundary tests.
- [x] Browser QA desktop/390px, keyboard semantics, accessibility tree, console,
  and network; AOY Console had zero messages/errors/warnings.
- [x] Enforce the local SML evidence overhead ceiling in a regression test;
  compare live percentiles after deploy without creating a test document.

## D11 — Four-tenant deployment

- [x] Back up runtime and database for Demo, AOY, Lanboon, and Ploy.
- [x] Pass canary build, PostgreSQL 16 migration, health, and preflight checks.
- [x] Deploy the same commit/migration without changing tenant configuration.
- [x] Verify health, DB auth, frontend assets, and recent severe errors.
- [x] Verify AOY historical grouping and live LINE quota freshness.
- [ ] Verify one future SML send records exchanges without duplicates.
- [x] Record exact backups, release commit, evidence, and rollback target below.

### D11 production record — 2026-09-07

- Release SHA: `175cbb11296dd426a66787bbd02523c785bfbce4`.
- Demo backup:
  `/mnt/data/nextstep-node-2/nexflow-backups/demo/pre-deploy-20260907-074737.sql.gz`.
- AOY backup:
  `/mnt/data/nextstep-node-2/nexflow-backups/aoy/pre-deploy-20260907-074924.sql.gz`.
- Lanboon backup:
  `/mnt/data/nextstep-node-2/nexflow-backups/lanboon/pre-deploy-20260907-075107.sql.gz`.
- Ploy backup:
  `/mnt/data/nextstep-node-2/nexflow-backups/ploy/pre-deploy-20260907-075205.sql.gz`.
- Rollback: restore the prior tenant container/runtime while retaining additive
  migration 094 and all audit evidence; never delete SML documents or exchange
  rows during rollback.
