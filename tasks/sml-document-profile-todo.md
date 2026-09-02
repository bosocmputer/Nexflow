# Todo: Nexflow AOY -> SML Document Profile V1

## Handoff

- Last completed: AOY off-mode compatibility deploy, route safety gate and shadow preview
- Active task: T14 controlled active-document parity and production monitoring
- Nexflow deployed application: `codex/marketplace-units-conversion` / `2ae9cbd`;
  durable production handoff continues on the same branch
- Gateway branch/HEAD: `codex/include-sml-unit-use-status-one` / `4b5a3f3`
- Feature mode: AOY is `shadow`; Demo, Lanboon and Ploy remain `off`. AOY
  Shopee main route is `AB-1 / 001`, config version 2. Shop `264993963`
  remains enabled but is safely paused with `route_changed`; shop `1029622928`
  remains disabled. No Profile reconciliation job or backfill was created.
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
- Browser evidence: local desktop Channel dialog plus production AOY off/shadow
  previews passed. Production shadow preview displayed `sml-document-v1`,
  `AB-1 / 001`, VAT 7%, the deterministic `remark_5`, and a clean console.
  Profile recovery-card and true 390px verification still require a controlled
  Profile attempt because the in-app browser keeps a 1280px viewport.
- Production evidence: Central Gateway `4b5a3f3` and AOY Nexflow `2ae9cbd` are
  deployed. Gateway health passed for demo/aoy/lbk63/ploy_test and its capability
  endpoint advertises `sml-document-v1`. AOY migrations 091-092 are applied,
  health and Gateway connectivity pass, and the recent severe-error scan is
  clean. A production preview null-array bug was found, fixed in `2ae9cbd`,
  redeployed and reverified without an SML write.
- Preserved user work: modified `AGENTS.md`, `docs/current-state.md`,
  `docs/nextstep-server-deploy-flow.md`; existing `tasks/plan.md` and
  `tasks/todo.md`; untracked `.serena/` and `scripts/__pycache__/`
- Backups: Gateway source/runtime/image at
  `/mnt/data/nextstep-node-2/deploy-backups/sml-document-profile-20260902-141000`;
  AOY DB at `pre-deploy-20260902-141500.sql.gz` and
  `pre-deploy-20260902-142246.sql.gz`; AOY runtime before shadow at
  `.env.pre-sml-profile-shadow-20260902-213500`.
- Blocker: activating Profile writes and proving parity requires user confirmation
  of the controlled production order. Read-only inspection found exactly one
  current non-cancelled AOY Shopee snapshot without a Nexflow bill:
  `26090216HNM1GJ` (`READY_TO_SHIP`, one item, 308.00 THB). No production
  accounting document will be selected by guesswork.
- Next action: confirm whether `26090216HNM1GJ` is the controlled AOY order,
  switch AOY temporarily to `active`, create/send it manually, and compare SML
  header/detail/VAT/shipment/logs before reconfirming Auto SML with a fresh cutoff.

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

- [x] Backup AOY application DB/runtime and Central SML Gateway
- [x] Deploy Gateway compatibility first
- [x] Deploy Nexflow with profile mode off
- [x] Shadow preview with AOY `AB-1 / 001`
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

### AOY release checkpoint: capability -> off -> shadow

- Nexflow commits: release baseline `174485a`; production preview stability fix
  `2ae9cbd`
- Gateway commit: `4b5a3f3`
- Scope: Central Gateway capability deployed first; only AOY Nexflow was
  migrated/recreated. Demo, Lanboon and Ploy Profile modes remain `off`.
- Backups: Central Gateway source/runtime/container/image under
  `/mnt/data/nextstep-node-2/deploy-backups/sml-document-profile-20260902-141000`;
  AOY database `pre-deploy-20260902-141500.sql.gz` and
  `pre-deploy-20260902-142246.sql.gz`; AOY runtime
  `.env.pre-sml-profile-shadow-20260902-213500`.
- Compatibility evidence: Gateway readiness passed for all four SML tenants;
  capability returned HTTP 200 with Profile V1 and the 2 MiB/500-item limits.
  AOY off-mode health, Gateway DNS/connectivity, migrations 091-092, empty
  Profile queue and clean severe-error scan passed.
- Safety evidence: production Preview initially exposed a `null` empty-array UI
  boundary and made the page white. No save or SML write happened. `2ae9cbd`
  makes backend preview arrays non-null and keeps a frontend compatibility guard;
  focused Go/frontend tests, lint and build passed before redeploy.
- Route/config evidence: off-mode preview passed and the main Shopee route was
  saved as `AB-1 / 001`, incrementing Channel config 1 -> 2. The enabled shop
  was automatically paused as `route_changed` and its Auto SML config incremented
  1 -> 2. The second shop remained disabled; no queue/backfill appeared.
- Shadow evidence: AOY runtime reports `sml_document_profile_mode=shadow`;
  health/Gateway connectivity and error scan pass. Production UI preview shows
  `shadow · sml-document-v1`, `AB-1 / 001`, VAT type 1/rate 7,
  `/api/v1/ic/sale-invoices`, and
  `NEXFLOW|shopee_realtime|ORDER-PREVIEW`, with a clean console.
- Feature mode after checkpoint: AOY `shadow`; Auto SML remains paused. No SML
  sale/profile write occurred.
- Residual verification: controlled active bill, SML parity, recovery card,
  true 390px, deployed percentile evidence, ten-document monitoring and Auto SML
  reconfirm/cutoff remain open.
- Candidate discovered read-only: order `26090216HNM1GJ` is the only current
  non-cancelled Shopee snapshot without a Nexflow bill (`READY_TO_SHIP`, one
  item, 308.00 THB). It remains untouched while awaiting explicit confirmation.
- Next action: confirm that candidate for the active write and parity gate.

For every completed task, update Handoff with:

1. Exact commit(s) or explicit reason no commit was made
2. Files changed
3. Focused and broad validation commands with result
4. Feature mode and tenant scope
5. Production evidence or “not deployed”
6. Known residual risk and the single next action
