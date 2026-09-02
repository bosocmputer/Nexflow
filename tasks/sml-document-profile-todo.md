# Todo: Nexflow AOY -> SML Document Profile V1

## Handoff

- Last completed: AOY plain-language Channel/Bill UI, literal remarks and true 390px QA
- Active task: T11 deployed percentiles plus the remaining first-ten monitoring
- Nexflow deployed application: `codex/marketplace-units-conversion` / `c51c52b`;
  durable production handoff continues on the same branch
- Gateway branch/HEAD: `codex/include-sml-unit-use-status-one` / `16c550d`
- Feature mode: AOY is `active`; Demo, Lanboon and Ploy remain `off`. AOY
  Shopee main route is `AB-1 / 001`, Channel config version 2. Shop `264993963`
  is enabled and unpaused with trigger `PROCESSED`, Auto SML config version 4
  and cutoff `2026-09-02 21:48:18.064382` Asia/Bangkok. Shop `1029622928`
  remains disabled. The first Profile completed synchronously, so no Profile
  reconciliation job or backfill was required.
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
- Browser evidence: production AOY shows the single result `ส่งเข้า SML แล้ว
  (AUTO)` for `BF-INV26090002`; recovery details appear only when actionable.
  The compact summary shows order reference, Auto/manual source, document format,
  customer, `AB-1 / 001`, configured VAT, two rows and exact 308.00 total. The
  timeline uses plain Thai instead of Profile/Core jargon. Desktop and true
  390x844 QA pass with no horizontal overflow and clean console. The settings
  dialog uses literal free-text remarks and a simple readiness check; its Select
  controlled-state warning was fixed and the final console is clean.
- Production evidence: Central Gateway `16c550d` and AOY Nexflow `aa1ccbb` are
  deployed. Gateway readiness passes for demo/aoy/lbk63/ploy_test. Controlled
  order `26090216HNM1GJ` produced exactly one bill/attempt/document
  `BF-INV26090002`: HTTP 201 in 179 ms, Profile complete with all five checks,
  stock recalculation completed on its first attempt, and no duplicate rows.
  SML has header 1/detail 2/VAT 1/shipment 1/main log 1/erp_log 1; `erp_logs`
  contains all 11 frozen JSON sections. LINE success was delivered once to each
  of the two enabled recipients.
- Preserved user work: modified `AGENTS.md`, `docs/current-state.md`,
  `docs/nextstep-server-deploy-flow.md`; existing `tasks/plan.md` and
  `tasks/todo.md`; untracked `.serena/` and `scripts/__pycache__/`
- Backups: Gateway source/runtime/image at
  `/mnt/data/nextstep-node-2/deploy-backups/sml-document-profile-20260902-141000`;
  Gateway VAT/profile hotfix backup at
  `/mnt/data/nextstep-node-2/deploy-backups/sml-profile-vat-hotfix-20260902-215500`;
  AOY DB at `pre-deploy-20260902-141500.sql.gz` and
  `pre-deploy-20260902-142246.sql.gz`, plus UI-fix backup
  `pre-deploy-20260902-154415.sql.gz`, plus plain-UI backups
  `pre-deploy-20260902-170204.sql.gz` and
  `pre-deploy-20260902-170832.sql.gz`; AOY runtime before shadow at
  `.env.pre-sml-profile-shadow-20260902-213500` and before active at
  `.env.pre-sml-profile-active-20260902-220000`.
- Controlled order: `26090216HNM1GJ` transitioned to `PROCESSED` at 21:51:10.
  Early writes exposed PostgreSQL 11 parameter inference conflicts and rolled
  back atomically; candidate lookup proved that no core existed. Gateway fixes
  `30d97db` and `16c550d` separated every VAT/shipment/main-log parameter context.
  Retrying the immutable attempt reused the same bill, attempt, payload hash and
  document number, then completed successfully without a duplicate.
- Next action: leave AOY active and monitor the next nine Profile documents;
  collect deployed p95/p99/queue-age evidence.

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
  - [x] Literal free-text remark and remark_2 with 255-character/control validation
  - [x] Plain-language readiness check, unsaved guard and new-documents-only warning
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
- [x] **T14 — Full tests, browser QA and failure injection**
  - [x] Go test/race/vet in both repositories
  - [x] Frontend focused tests/lint/build and sales-only guard
  - [x] Automated logs-DB-down, lost-response immutable retry, concurrent
    duplicate, config-race, tenant mismatch and worker lease-reclaim coverage
  - [x] Local desktop Channel dialog and clean-console check
  - [x] Live backend/network, Profile status card and desktop accessibility QA
  - [x] True 390px QA

## AOY Release Gates

- [x] Backup AOY application DB/runtime and Central SML Gateway
- [x] Deploy Gateway compatibility first
- [x] Deploy Nexflow with profile mode off
- [x] Shadow preview with AOY `AB-1 / 001`
- [x] Controlled automatic document parity and no-duplicate proof (the user
      explicitly chose the Auto SML path instead of a separate manual send)
- [x] Enable AOY Auto SML with a new cutoff and no backfill
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

### AOY release checkpoint: active + controlled Auto SML

- User-directed scope: exercise the first Profile V1 write through the real
  Shopee automatic lifecycle rather than a separate manual send.
- Runtime: AOY changed `shadow` -> `active` after creating
  `.env.pre-sml-profile-active-20260902-220000`; backend health, Central Gateway
  readiness, runtime capability log and severe-error scan passed. Other tenants
  remain `off`.
- Initial Auto SML preview: Henna.milkford, `READY_TO_SHIP`, Profile `active`,
  route config version 2, two LINE recipients and no-backfill warning all passed.
  Reconfirmation cleared `route_changed` and created a fresh cutoff without
  enqueuing the historical ready-to-ship order.
- Trigger correction: exact push evidence for `26090216HNM1GJ` proved
  `UNPAID` at 16:43 and `READY_TO_SHIP` at 17:13. Because the user's intended
  Seller Centre action is the later prepare-shipment action, a second signed
  preview changed the trigger to `PROCESSED`, whose UI contract explicitly says
  it waits for the shop to prepare shipment.
- Final setting: shop `264993963` enabled/unpaused, trigger `PROCESSED`, config
  version 4, cutoff `2026-09-02 21:48:18.064382` Asia/Bangkok. Shop
  `1029622928` remains disabled. Existing Auto jobs remain six succeeded;
  Profile reconciliation queue remains empty; no backfill or SML write occurred.
- Browser evidence: active Preview and both Auto SML signed previews passed;
  UI reports Auto SML on and trigger PROCESSED; console is clean.
- Sequence exception: the user explicitly requested the controlled bill itself
  test Auto SML, so Auto SML is enabled before the first Profile parity result.
  Scope is bounded to the selected shop/order and the new cutoff.
- Next action: after the user's Shopee click, monitor and reconcile the first
  automatic Profile V1 document end to end before accepting the release gate.

### AOY release checkpoint: first automatic Profile V1 document

- Trigger evidence: Shopee order `26090216HNM1GJ` reached exact `PROCESSED` at
  21:51:10 Asia/Bangkok after the configured cutoff. Nexflow created one Auto job
  `a2b210c4-d65a-40d1-b5d2-a547d74da046`, one bill
  `513e0afd-1f8d-42c3-8023-f7759803ffab`, and one immutable SML attempt
  `29cfa83f-6983-4c84-8c15-1bc22360fda6` for `BF-INV26090002`.
- Failure containment: PostgreSQL 11 rejected reused parameters across
  `INSERT ... SELECT` and `NOT EXISTS`. Every failed transaction rolled back;
  an authenticated candidate lookup returned 404 before the final retry, proving
  no core document existed. The shop remained enabled because this was one job,
  not three distinct terminal job failures.
- Gateway hotfixes: `30d97db` first separated VAT document identity fields;
  `16c550d` then separated every INSERT and existence-check parameter for VAT,
  shipment and main log. New regression tests failed before each fix. Full
  Gateway `go test ./...`, `go test -race ./...`, and `go vet ./...` pass. The
  three final statements also passed read-only `PREPARE` against AOY's real
  PostgreSQL 11 schema before deployment.
- Final result: immutable retry returned HTTP 201 in 179 ms and reused the same
  bill/attempt/doc number. Attempt state is `sent`, core is `created`, Profile is
  `complete`, reconciliation is false, and required/completed checks are exactly
  core, VAT, shipment, main log and erp_log. The stock job completed on attempt 1
  with both process-stock and balance verification timestamps.
- SML parity: `BF-INV26090002` has one active header, two active details
  (`AH-0007` 290 THB and shipping `AH-0061` 18 THB), one VAT row (base 287.85,
  VAT 20.15, rate 7), one complete shipment row, one Profile main log and one
  `aoy_logs.erp_logs` row. Header/detail totals are 308, details use `AB-1 / 001`,
  the Profile marker hash matches the immutable attempt, and `data_new` contains
  all 11 frozen SML sections. The same header/VAT/shipment/main-log/erp-log
  relation pattern is present on manual fixture `INV26050020`.
- Notifications: the earlier terminal failure and final success each produced
  one delivery per enabled recipient; both success deliveries are sent, and no
  duplicate delivery exists for a recipient/dedupe key.
- UI correction: production verification found the legacy lower payload summary
  ignored Profile V1 `details`. Nexflow `aa1ccbb` adds a legacy-compatible tested
  resolver; production now displays `AB-1 / 001` and `2 รายการ`, alongside the
  complete Core/Profile/Stock status card and five-check timeline. Frontend
  focused tests, lint (0 errors/35 pre-existing warnings), build, sales-only
  guard, AOY backup/deploy health, Gateway connectivity and severe-error scan
  pass. AOY backup is `pre-deploy-20260902-154415.sql.gz`.
- Current scope: AOY remains `active`; Demo, Lanboon and Ploy remain `off`.
- Next action: monitor nine more AOY Profile documents and capture deployed
  percentile/queue-age evidence before closing T11.

### AOY release checkpoint: plain-language operator UI

- Nexflow commits: literal free-text remarks and simplified route preview
  `68d2366`; compact successful/recovery SML presentation `cc249d9`; plain Thai
  timeline and controlled Select fix `c51c52b`.
- Behavior: `remark` and `remark_2` are literal free text end to end; braces are
  no longer expanded as tokens. Both fields reject control characters and more
  than 255 Unicode characters. Preview remains a required read-only safety gate
  but no longer exposes Profile mode/version, system fields, hash or route
  signature to normal users.
- Bill UI: a fully successful automatic document renders one compact
  `ส่งเข้า SML แล้ว (AUTO)` result. Core/profile/stock recovery details are shown
  only for pending/failure states and use Thai operator language. Raw request and
  response JSON remain collapsed and admin-only.
- Auto/manual evidence: Bill detail now derives AUTO only from a succeeded
  `shopee_auto_sml_jobs` row; the existing bill `BF-INV26090002` renders AUTO,
  `AB-1 / 001`, VAT `รวมใน · 7%`, two rows and `฿308.00` correctly.
- Tests: backend `go test ./...`, `go test -race ./...`, and `go vet ./...`
  pass. Frontend Profile/payload/timeline/audit tests, lint (0 errors / 35
  pre-existing warnings), production build and sales-only guard pass.
- Production: deployed AOY-only at `c51c52b`; Demo, Lanboon and Ploy were not
  rebuilt and remain Profile `off`. Backups are
  `pre-deploy-20260902-170204.sql.gz` and
  `pre-deploy-20260902-170832.sql.gz`. AOY/edge/Gateway health, database auth,
  sales-only counts and severe-error scan pass.
- Browser: desktop accessibility snapshots and 390x844 responsive QA pass; no
  horizontal overflow. Final settings and bill tabs have zero console warnings
  or errors. Preview was exercised read-only; no config save or SML write was
  performed during QA.
- Next action: monitor nine more Profile documents and capture deployed
  p95/p99/queue-age evidence for T11.

For every completed task, update Handoff with:

1. Exact commit(s) or explicit reason no commit was made
2. Files changed
3. Focused and broad validation commands with result
4. Feature mode and tenant scope
5. Production evidence or “not deployed”
6. Known residual risk and the single next action
