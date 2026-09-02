# Todo: Nexflow AOY -> SML Document Profile V1

## Handoff

- Last completed: T13 observability, security hardening, recovery UX and durable worker
- Active task: T14 production release verification and AOY controlled rollout
- Nexflow branch/HEAD: `codex/marketplace-units-conversion` / `d685345`
- Gateway branch/HEAD: `codex/include-sml-unit-use-status-one` / `4b5a3f3`
- Feature mode: complete in code with default `off`; production runtime and tenant settings unchanged
- Tests: both repositories pass `go test ./...`, `go test -race ./...`, and
  `go vet ./...`; frontend focused tests, lint (0 errors / 35 pre-existing
  warnings), and production build pass; sales-only release guard passes
- Security evidence: `govulncheck` reports no reachable Go vulnerabilities;
  production npm audit retains two moderate React Router advisories because the
  offered fix is a forced breaking v7 upgrade and was intentionally not applied
- Performance evidence: resolver benchmark 1/10/50/200 items =
  1.539/7.854/35.891/140.596 microseconds; Gateway normalization/hash =
  5.572/25.762/110.357/482.279 microseconds. HTTP/database p95 budgets still
  require deployed telemetry.
- Browser evidence: local desktop Channel dialog, preview states and unsaved
  guard passed with a clean console. The local backend was intentionally absent;
  live data, Profile status recovery, and true 390px verification remain release
  checks because the in-app browser kept a 1280px viewport.
- Production evidence: read-only AOY parity inspection only; no deployment,
  runtime change, migration, Auto SML setting change, or SML write was performed
- Preserved user work: modified `AGENTS.md`, `docs/current-state.md`,
  `docs/nextstep-server-deploy-flow.md`; existing `tasks/plan.md` and
  `tasks/todo.md`; untracked `.serena/` and `scripts/__pycache__/`
- Blocker: controlled `active` parity and first ten-document monitoring require
  the AOY release gates; they cannot be claimed from local tests
- Next action: back up AOY and Central Gateway, deploy Gateway capability first,
  then deploy Nexflow migration/code with `SML_DOCUMENT_PROFILE_MODE=off`

## Phase A — Proof and contract

- [x] **T00 — Durable plan/todo and repository snapshot**
  - [x] Record dirty files and both repository heads
  - [x] Create uniquely named plan/todo without overwriting existing task files
  - [x] Run baseline focused tests after contract inspection
- [x] **T01 — Freeze parity from AOY `INV26050020`**
  - [x] Record exact header/detail/VAT/shipment/main-log/erp-log tables and keys
  - [x] Create synthetic, PII-free golden fixture
  - [x] Stop Gateway profile writes if any required relationship remains unproven
- [x] **T02 — Freeze Profile V1 contract**
  - [x] Decimal-string authority, canonical hash and compatibility rules
  - [x] VAT/shipment applicable vs not-applicable rules
  - [x] 2 MiB, 500-item and 255-character boundary rules

## Phase B — Channel configuration

- [x] **T03 — Migration and backend Channel config**
  - [x] Add remark/free remark_2/config_version additively
  - [x] Add optimistic update and safe audit
  - [x] Add read-only preview endpoint
- [x] **T04 — Channel dialog and user-error protection**
  - [x] Remark template preview and free-text remark_2
  - [x] Read-only system fields, unsaved guard and new-documents-only warning
  - [x] Separate save from Auto SML enable/reconfirm

## Phase C — Gateway Document Profile

- [x] **T05 — Capability and legacy compatibility**
- [x] **T06 — Transactional header/detail/VAT/shipment/main-log writer**
- [x] **T07 — Existing-document profile reconciliation**

## Phase D — Nexflow send paths and durable jobs

- [x] **T08 — Shared payload resolver for manual/bulk/Auto/retry/cancel**
- [x] **T09 — Durable profile reconciliation jobs with leases/fencing**
- [x] **T10 — Logs, timeline and profile-only recovery UX**

## Phase E — Production verification

- [ ] **T11 — Measured performance baseline and budgets**
  - [x] Local 1/10/50/200-item resolver and Gateway canonical-hash benchmarks
  - [x] Bounded request-duration metrics and queue age/depth/p95 metrics
  - [ ] Deployed Settings/Gateway/queue/UI percentile evidence against budgets
- [x] **T12 — Security/abuse tests and dependency audit triage**
- [x] **T13 — Correlation IDs, structured events, metrics and runbook alerts**
- [ ] **T14 — Full tests, browser QA and failure injection**
  - [x] Go test/race/vet in both repositories
  - [x] Frontend focused tests/lint/build and sales-only guard
  - [x] Automated logs-DB-down, lost-response immutable retry, concurrent
    duplicate, config-race, tenant mismatch and worker lease-reclaim coverage
  - [x] Local desktop Channel dialog and clean-console check
  - [ ] Live backend/network, Profile recovery card, accessibility and true 390px QA

## AOY Release Gates

- [ ] Backup AOY application DB/runtime and Central SML Gateway
- [ ] Deploy Gateway compatibility first
- [ ] Deploy Nexflow with profile mode off
- [ ] Shadow preview with AOY `AB-1 / 001`
- [ ] Controlled manual document parity and no-duplicate proof
- [ ] Enable AOY Auto SML with a new cutoff and no backfill
- [ ] Verify the first 10 documents; keep all other tenants off

## Per-Increment Completion Record

### T00-T04

- Nexflow commit: `d162b00`
- Gateway commit: not applicable for this increment
- Tests: Nexflow `go test ./...`; frontend `npm run test:sml-document-profile`,
  `npm run lint` (0 errors, 35 unrelated warnings), and `npm run build`
- Feature mode: default `off`; no tenant runtime changed
- Production evidence: T01 used read-only AOY/SML inspection; no write or deploy
- Next action at completion: T05 Gateway capability and compatibility

### T05-T07

- Gateway commit: `a04b500`
- Nexflow commit: `d162b00` remains the caller-side baseline
- Tests: Gateway `go test ./...`, `go test -race ./...`, `go vet ./...`,
  OpenAPI tests and focused Document Profile contract tests
- Feature mode: Gateway is opt-in by `document_profile_version`; every legacy
  request/response remains on its old path
- Production evidence: no deploy and no SML mutation; only the synthetic golden
  fixture and read-only T01 evidence were used
- Residual verification: live crash-after-commit and logs-DB fault injection stay
  in T14; the retry/reconcile path and hash-mismatch contract are covered in code
- Next action at completion: T08 shared payload resolver and immutable attempt
  propagation

### T08-T10

- Nexflow commit: `d685345`
- Gateway commits: `a04b500` plus tracing/recovery hardening `4b5a3f3`
- Files: migration 092; immutable Profile resolver/attempt propagation; durable
  reconciliation repository/worker; admin retry/metrics endpoints; Core/Profile/
  Stock bill UI; Auto SML signed preview/reconfirm; cancellation route snapshots
- Tests: repository/handler/service tests cover fencing, bounded retry, exact-byte
  replay after a lost response, nested Gateway payload-mismatch errors, cross-
  tenant rejection, config race, and three-distinct-job Auto SML pause
- Feature mode: worker and Gateway Profile writes run only in `active`; `shadow`
  validates without the opt-in wire field; `off` preserves legacy behavior
- Production evidence: not deployed; no database migration or external write
- Next action at completion: T11-T14 verification and release evidence

### T11-T13

- Nexflow commit: `d685345`
- Gateway commit: `4b5a3f3`
- Tests: both Go repositories pass regular/race/vet; `govulncheck` finds zero
  reachable vulnerabilities; frontend focused tests/lint/build and release guard
  pass; abuse tests cover size/item/text/control-character/HTML/cross-tenant cases
- Feature mode: unchanged and default `off`; no tenant runtime changed
- Performance: local benchmark numbers are recorded in Handoff; deployed p95/p99,
  queue-age and UI INP remain T11 release evidence
- Observability: validated correlation ID, bounded metric labels, queue metrics,
  structured events and alerts for mismatch, terminal failure, oldest queue,
  Gateway p95 and three consecutive distinct failed jobs
- Residual risk: two moderate React Router production advisories require a forced
  major upgrade; keep as a separate tested migration rather than forcing it here
- Production evidence: not deployed
- Next action at completion: AOY backup and off-mode compatibility deployment

For every completed task, update Handoff with:

1. Exact commit(s) or explicit reason no commit was made
2. Files changed
3. Focused and broad validation commands with result
4. Feature mode and tenant scope
5. Production evidence or “not deployed”
6. Known residual risk and the single next action
