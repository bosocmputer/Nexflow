# Todo: Nexflow AOY -> SML Document Profile V1

## Handoff

- Last completed: authenticated T24 production browser QA and the responsive
  dialog correction are deployed. Gateway capability V2 is live, migration 093
  is applied to all four Nexflow databases, and every tenant runs the same
  Nexflow commit with only Sale Invoice Profile active; the four new routes
  remain shadow.
- Active task: AOY controlled parity for SO/SSC/SIC/CN. The first organic
  post-hotfix observation, real-user RUM and long-running percentiles remain
  non-blocking monitoring.
- Nexflow deployed application: Demo, AOY, Lanboon and Ploy all run
  `codex/marketplace-units-conversion` / `ed70202`; durable production handoff
  continues on the same branch.
- Nexflow branch/deployed code: `codex/marketplace-units-conversion` /
  `ed70202`; handoff-only documentation commits may be newer than the deployed
  code without changing runtime behavior.
- Gateway branch/HEAD: `codex/include-sml-unit-use-status-one` / `3e2ed0e`
- Feature mode: all four instances use
  `saleinvoice:active,saleorder:shadow,saleordercancel:shadow,saleinvoicecancel:shadow,creditnote:shadow`.
  AOY main/cancellation config remains Sale Invoice/Credit Note at versions 2/1.
  Shop `264993963` retains trigger `PROCESSED` and is deliberately paused as
  `route_changed`, config version 5, until controlled parity and USER reconfirm;
  shop `1029622928` remains disabled. All sale, cancellation, sale-Profile and
  cancellation-Profile live queues are empty. No backfill or SML write occurred
  during this rollout.
- Tests: both repositories pass `go test ./...`, `go test -race ./...`, and
  `go vet ./...`; frontend focused tests, lint (0 errors / 35 pre-existing
  warnings), and production build pass; sales-only release guard passes
- VAT incident regression: Gateway tests prove a `2026-09-02` sale writes
  `vat_effective_period=9`, `vat_effective_year=2569`, and VAT-register
  `vat_type=0` while preserving header `vat_type=1`; a year-boundary test proves
  `2027-01-01 -> 1/2570`. The exact INSERT passed read-only `PREPARE` against
  Demo, AOY, Lanboon `lbk63`, and Ploy `ploy_test` schemas.
- Security evidence: `govulncheck` reports no reachable Go vulnerabilities;
  production npm audit retains two moderate React Router advisories because the
  offered fix is a forced breaking v7 upgrade and was intentionally not applied
- Performance evidence: current Gateway Profile normalization/hash is
  0.119/0.436/1.081 ms for 50/200/500 lines on Apple M1 Pro; the route-bundle
  normalize/hash/sign/verify contract is 0.010 ms. On deployed AOY, 20 sequential
  samples measured route-bundle GET p95 at 3.117 ms and Preview p95 at 1.711 ms;
  both are far below their 300/500 ms budgets. Authenticated DevTools Network
  measured the dialog count/Preview requests at 31-66 ms. At 390px, Event Timing
  measured 3-5 ms handler processing plus 37-64 ms presentation; the CUA driver
  itself held synthetic clicks for 360-371 ms, so its raw 400-440 ms click entry
  is automation input delay rather than application work. Real-user RUM and
  Gateway database-write p95 still require organic/controlled traffic.
- Browser evidence: production AOY renders the back action inline before
  `BF-INV26090002`, followed by `ขาย -> ขายสินค้าและบริการ` and
  `ส่งแล้ว (AUTO)` in one compact header. The duplicate successful-status card
  is absent and the Shopee order time is `02/09/2026 16:43 น.` in
  Asia/Bangkok. The SML summary renders the operator-facing label/value
  `ประเภทรายการ` / `ขายเงินเชื่อ` and both remarks without exposing the raw
  `inquiry_type` key, numeric code or fallback wording. An empty Shopee API
  artifact section is hidden; timelines and real artifacts remain available.
  Desktop 1280x720 and true 390x844 QA pass with no horizontal overflow and
  zero new console warnings/errors. The authenticated route-bundle dialog also
  passes desktop and 390x844: labels/accessible names are present; Tab moves
  from remark 1 to remark 2, shipping and onward controls; Enter runs the
  read-only Preview; Escape closes the dialog. Preview enables Save without
  saving, enabling automation or creating an SML document.
- Production evidence: Central Gateway `3e2ed0e`; Demo, AOY, Lanboon and Ploy
  Nexflow `ed70202` are deployed. Gateway readiness passes for
  demo/aoy/lbk63/ploy_test, all five capability routes resolve at revision
  `sml-sales-document-profile-v2-20260903`, and all four backends have migration
  093 plus the exact route-mode map above. Controlled
  order `26090216HNM1GJ` produced exactly one bill/attempt/document
  `BF-INV26090002`: HTTP 201 in 179 ms, Profile complete with all five checks,
  stock recalculation completed on its first attempt, and no duplicate rows.
  SML has header 1/detail 2/VAT 1/shipment 1/main log 1/erp_log 1; `erp_logs`
  contains all 11 frozen JSON sections. LINE success was delivered once to each
  of the two enabled recipients.
- VAT incident evidence: before the SML user edit, the immutable BILLFLOW audit
  stored effective period/year `0/0` and copied header VAT type 1 into the VAT
  register. SML daily processing omitted output-VAT credit `215500` for 20.15,
  leaving debit 434.96 versus credit 414.81. The SML user edit changed the
  register to period 9/year 2569/type 0; reprocessing added the exact 20.15
  credit and balanced 434.96/434.96. Gateway `53393b6` now derives those values
  from the validated document date for future Profile V1 sales and writes the
  same values into `erp_logs.data_new.screenvatsale`. It does not write
  `gl_journal` or `gl_journal_detail`.
- Preserved user work: modified `AGENTS.md`, `docs/current-state.md`,
  `docs/nextstep-server-deploy-flow.md`; existing `tasks/plan.md` and
  `tasks/todo.md`; untracked `.serena/` and `scripts/__pycache__/`
- Backups: Gateway source/runtime/image at
  `/mnt/data/nextstep-node-2/deploy-backups/sml-sales-profile-v2-20260903-094700`
  with rollback container
  `nexflow-sml-api-bybos-pre-profile-v2-20260903-094700`; four-tenant runtime
  backups are `.env.pre-profile-v2-20260903-100335`; deployment database backups
  are Demo `pre-deploy-20260903-100723.sql.gz`, AOY
  `pre-deploy-20260903-100946.sql.gz`, Lanboon
  `pre-deploy-20260903-101253.sql.gz`, and Ploy
  `pre-deploy-20260903-101422.sql.gz`. AOY also has the pre-pause database
  `pre-profile-v2-20260903-100335.sql.gz`. Earlier backups remain at
  the following locations: Gateway source/runtime/image at
  `/mnt/data/nextstep-node-2/deploy-backups/sml-document-profile-20260902-141000`;
  Gateway VAT/profile hotfix backup at
  `/mnt/data/nextstep-node-2/deploy-backups/sml-profile-vat-hotfix-20260902-215500`;
  AOY DB at `pre-deploy-20260902-141500.sql.gz` and
  `pre-deploy-20260902-142246.sql.gz`, plus UI-fix backup
  `pre-deploy-20260902-154415.sql.gz`, plus plain-UI backups
  `pre-deploy-20260902-170204.sql.gz` and
  `pre-deploy-20260902-170832.sql.gz`, compact-header backup
  `pre-deploy-20260902-172519.sql.gz`, and inquiry-type backup
  `pre-deploy-20260902-173738.sql.gz`, plus simplified-evidence backups
  `pre-deploy-20260902-180737.sql.gz` and
  `pre-deploy-20260902-181122.sql.gz`; alignment backups are Demo
  `pre-deploy-20260902-181634.sql.gz`, Lanboon
  `pre-deploy-20260902-181809.sql.gz` and Ploy
  `pre-deploy-20260902-181838.sql.gz`; four-tenant activation runtime backups
  are Demo `.env.pre-sml-document-profile-active-20260903-024305`, Lanboon
  `.env.pre-sml-document-profile-active-20260903-024355` and Ploy
  `.env.pre-sml-document-profile-active-20260903-024430`; AOY runtime before
  shadow at `.env.pre-sml-profile-shadow-20260902-213500` and before active at
  `.env.pre-sml-profile-active-20260902-220000`; VAT metadata hotfix backup is
  `/mnt/data/nextstep-node-2/deploy-backups/sml-vat-effective-period-20260903-034424`,
  with rollback container
  `nexflow-sml-api-bybos-pre-vat-20260903-034639`.
- Responsive-fix backups: Demo `pre-deploy-20260903-104107.sql.gz`, AOY
  `pre-deploy-20260903-104309.sql.gz`, Lanboon
  `pre-deploy-20260903-104505.sql.gz`, and Ploy
  `pre-deploy-20260903-104626.sql.gz`.
- Controlled order: `26090216HNM1GJ` transitioned to `PROCESSED` at 21:51:10.
  Early writes exposed PostgreSQL 11 parameter inference conflicts and rolled
  back atomically; candidate lookup proved that no core existed. Gateway fixes
  `30d97db` and `16c550d` separated every VAT/shipment/main-log parameter context.
  Retrying the immutable attempt reused the same bill, attempt, payload hash and
  document number, then completed successfully without a duplicate.
- Pre-T17 parity evidence: SML-created sales with header `vat_type=0` (external VAT)
  and non-zero VAT do have `gl_journal_vat_sale` rows whose register `vat_type`
  is 0. Gateway `53393b6` wrote Profile VAT rows only for header types 1/2, so
  external-VAT Profile documents were a proven contract gap. API-created
  `CN26080002` also had VAT 11.69 but no VAT-register, main-log, or `erp_logs`
  row. T17-T20 close these gaps in deployed Gateway `3e2ed0e`.
- External-VAT manual evidence: `INV26090003 -> CN26090002` proves both headers
  use `vat_type=0`, VAT 7%, base 300.00, VAT 21.00 and total amount 321.00; both
  have VAT-register type 0 with period/year 9/2569. The credit note uses
  `ref_amount=ref_diff=total_value=300.00`, while its `ap_ar_trans_detail`
  receivable amount is the VAT-inclusive 321.00. It has no shipment, while the
  source invoice has one. Both full manual `erp_logs.data_new` payloads include
  the same VAT/reference/remark values. Lot `00007` reverses the exact 149.53
  cost from the invoice to the credit note. No GL rows exist yet.
- External-VAT post-processing evidence: `INV26090003` and `CN26090002` each
  produced exactly one journal and five detail rows at 13:07 Asia/Bangkok. Each
  balances 470.53/470.53. The invoice debits AR 321.00 and cost 149.53, then
  credits sales 300.00, output VAT 21.00 and inventory 149.53; the credit note
  reverses those exact entries. The controlled pair may now be deleted.
- Zero-rate pre-processing evidence: manual SML `INV26090004` uses header
  `vat_type=2`, `vat_rate=7`, `total_value=total_amount=300.00`, but deliberately
  keeps `total_before_vat=total_vat_value=total_after_vat=0`. Its detail still
  has `sum_amount_exclude_vat=300.00`. SML writes one zero-base/zero-amount VAT
  register row with period/year 9/2569 and register type 0. Pre-T17 Nexflow
  calculated header before/after VAT as 300.00 for type 2; T17 corrected and
  regression-tested this parity gap.
- Zero-rate credit-note evidence: manual SML `CN26090003` correctly references
  `INV26090004`. Both headers use `vat_type=2`, rate 7%,
  `total_value=total_amount=300.00` and zero before-VAT/VAT/after-VAT totals.
  Both still have one VAT-register row with zero base/amount, period/year 9/2569
  and register type 0. The credit note uses
  `ref_amount=ref_diff=300.00`, has one `ap_ar_trans_detail` row for 300.00,
  carries the exact source line reference, reverses lot `00007` and cost 149.53
  with `calc_flag=1`, has no shipment row, and has complete current-round main
  and `erp_logs` audit rows. Before daily processing neither document has a GL
  journal or GL detail row.
- Zero-rate post-processing evidence: SML daily processing created exactly one
  GL header and four GL detail rows for each of `INV26090004` and `CN26090003`
  at 13:25:42 Asia/Bangkok. Both journals balance exactly at 449.53/449.53.
  The invoice debits receivables 300.00 and cost 149.53, then credits sales
  300.00 and inventory 149.53. The credit note reverses all four accounts
  exactly. There is deliberately no output-VAT GL detail, while each document
  retains one zero-base/zero-amount VAT-register row with period/year 9/2569.
  The detail reference and lot `00007` cost reversal remain intact after
  processing. The controlled pair may now be deleted.
- Next action: promote one AOY route at a time only after its controlled
  Preview/Write matches the committed fixture; USER must reconfirm before AOY
  automation resumes. Separately inspect the first organic AOY VAT Profile
  document created after Gateway `53393b6`; do not manufacture or backfill a
  Shopee order.

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
- [x] Approve AOY production use after the controlled document passed; the
      first-ten observation is not a release blocker
- [x] Activate Document Profile for Demo, Lanboon and Ploy before their future
      Shopee connection, per the user's explicit 2026-09-03 request

## Post-release monitoring (non-blocking)

- [x] Verify controlled AOY document `BF-INV26090002` end to end
- [x] Diagnose its daily-processing VAT/GL incident and deploy the shared
      Gateway correction to all four tenant routes
- [ ] Verify the first organic post-`53393b6` AOY VAT document after normal SML
      daily processing
- [ ] Observe up to nine additional AOY documents only as real customer orders
      naturally arrive; there is no deadline and this does not block production
- [ ] Complete deployed Settings/Gateway/queue/UI percentile evidence when the
      sample size is sufficient

## Endpoint parity extension (evidence gathering)

- [x] Capture manual SML pre-processing structures for sale order, sale invoice,
      header-only void, and credit note
- [x] Capture post-processing GL, output-VAT, cost, stock-direction, reference,
      and audit evidence for the five controlled documents
- [x] Prove `vat_type=0` sales with non-zero VAT require a VAT-register row
- [x] Capture a manual `vat_type=0` invoice/credit-note pre-processing snapshot,
      including VAT-register, AR reference, remarks, shipment and exact lot cost
- [x] Confirm post-processing GL direction and balance for the manual
      `vat_type=0` invoice/credit-note pair
- [x] Capture the manual `vat_type=2` sale pre-processing header/detail/VAT/log
      behavior and prove its header-total parity gap
- [x] Capture a manual `vat_type=2` credit-note pre-processing snapshot
- [x] Capture the `vat_type=2` invoice/credit-note pair's post-processing GL
- [x] Freeze and approve the sale-order/void/credit-note Profile contract before
      implementation

## Sales Document Profile Completion (approved 2026-09-03)

- [x] **T15 — Freeze evidence and contract**
  - [x] Commit PII-free fixtures for SO/SSC, INV/SIC/CN and VAT 0/1/2
  - [x] Record exact columns, relation keys, SML menu and ERP JSON sections
  - [x] Append the approved extension to the durable plan/todo without replacing
        T00-T14 history
  - [x] Record that controlled SML samples may be removed after the committed
        fixture test passes; no test reads the live rows
- [x] **T16 — Capability and route-mode foundation**
  - [x] Add `GET /api/v1/capabilities` with contract revision/routes/limits
  - [x] Add strict Gateway and Nexflow route-mode parsing with safe defaults
  - [x] Add fail-closed Nexflow capability client and legacy compatibility tests
- [x] **Checkpoint A1 — Foundation safety**
  - [x] No production behavior changes; legacy tests and Sale Invoice hash pass
  - [x] Unknown/duplicate mode and capability mismatch fail closed
- [x] **T17 — VAT and exact-decimal correctness**
  - [x] Correct VAT type 0/1/2 header/register applicability and zero-rate totals
  - [x] Reject invalid exact decimals before beginning the core transaction
  - [x] Add controlled-fixture regression tests for all VAT modes
- [x] **T18 — Sale Order Profile vertical slice**
  - [x] Add Profile fields/status/hash and route-correct main/ERP logs
  - [x] Write detail `calc_flag=1`; require shipment for Marketplace goods
  - [x] Test retry, mismatch, 500-item/expanded-item limits and logs DB outage
- [x] **T19 — Sale Order Cancellation vertical slice**
  - [x] Add SSC Preview/Create/doc-number aliases and trans flag 37
  - [x] Add common source lock, source fingerprint and exact detail references
  - [x] Mark source `used_status=1` and queue stock recalculation after core
- [x] **Checkpoint B — SO/SSC parity and concurrency**
  - [x] SO/SSC match fixtures and contain no VAT/AP-AR/GL placeholders
  - [x] Lost response/concurrent cross-kind cancellation creates no duplicate
- [x] **T20 — Invoice Void and Credit Note Profile**
  - [x] Complete SIC main/ERP logs and uniform Profile response
  - [x] Complete CN VAT, AP-AR and exact source references
  - [x] Use `ref_amount/ref_diff=source.total_value` and preserve blank/detail branch
  - [x] Test VAT 0/1/2 and external source-state conflicts
- [x] **T21 — Durable cancellation reconciliation (migration 093)**
  - [x] Add Profile summary columns to `shopee_sml_cancellations`
  - [x] Add fenced reconciliation jobs, unique cancellation/version, max 10 tries
  - [x] Prove Profile retry cannot resend core or stock jobs
- [x] **T22 — Atomic Shopee route bundle API and UI**
  - [x] Add GET/Preview/PUT bundle with two config versions and 10-minute token
  - [x] Save both rows/audit/Auto pause in one transaction; Enable stays separate
  - [x] Reject incompatible route pairs, stale preview and absolute URL injection
  - [x] Build one accessible dialog; production desktop/390px evidence remains T24
- [x] **Checkpoint C — Configuration safety**
  - [x] Config races cannot overwrite newer values
  - [x] Automation cannot bypass Preview, capability or active route mode
- [x] **T23 — Performance, security and observability**
  - [x] Enforce 2 MiB and 500 items before/after expansion without N+1 writes
  - [x] Enforce 20s transaction, 3s lock wait and 2s ERP-log timeouts
  - [x] Verify admin/cross-tenant/PII/logging/SSRF protections
  - [x] Add bounded route/profile metrics and operational alerts
  - [x] Record 50/200/500-item and local settings performance evidence; deployed UI INP remains T24

### T22 completion record

- Completed task: T22 atomic Shopee route bundle and guarded configuration UI
- Nexflow commit: `ddf6669` (`feat: configure Shopee SML routes atomically`)
- Gateway commit: `4d93ebe` (unchanged; T22 consumes the T16 capability contract)
- Tests: focused handler/repository tests, config-race rollback, HMAC binding and
  expiry tests, `go test ./...`, focused race tests, `go vet ./...`, frontend
  lint (0 errors / 35 pre-existing warnings), build and Profile text tests pass
- Feature modes: local code only; no production runtime or route mode changed
- Evidence: Preview performs one capability request; signed claims bind tenant,
  normalized payload, both config versions, capability revision and both route
  modes. The write transaction updates both rows, pauses every enabled Shopee
  shop and writes the redacted audit. The normal UI no longer presents the
  cancellation route as a separate row.
- Remaining verification: live desktop/390px, keyboard and network QA after the
  Gateway/Nexflow staged deploy in T24
- Next action: T23 request/expanded-item limits, batch-write and timeout audit,
  bounded operational signals and 50/200/500 regression evidence

### T23 completion record

- Completed task: T23 performance, security and observability hardening
- Nexflow commits: `48b825f` (`perf: harden SML profile delivery limits`) and
  `e243da3` (`test: benchmark SML route preview contract`)
- Gateway commit: `3e2ed0e` (`perf: harden SML profile document writes`)
- Limits and performance: both callers and Gateway reject requests over 2 MiB
  and more than 500 input lines. Gateway rechecks the resolved payload after
  set-product expansion before the header insert. INV/SO/CN/SSC detail writes
  use one parameterized `pgx.Batch`, while reference validation remains bounded
  set-based SQL. Gateway CPU validation/hash benchmarks are 0.119/0.436/1.081
  ms at 50/200/500 lines; route Preview normalization/signing is 0.010 ms.
- Timeouts: every sales/cancellation transaction uses request context plus
  PostgreSQL `statement_timeout=20s`; cancellation uses a shared source advisory
  lock with `lock_timeout=3s`; the independent ERP log retains its 2s timeout.
- Observability: the admin Profile metrics response now includes the separate
  cancellation queue. Core and reconciliation calls emit only bounded tenant,
  route, profile and status dimensions. Cancellation mismatch, terminal state
  and oldest queue over ten minutes emit periodic structured alerts.
- Security: route allowlist/signed Preview/admin gates and authenticated tenant
  context remain mandatory; oversized JSON returns 413 without echoing parser
  internals. Both Go repositories report zero reachable vulnerabilities.
  Production npm audit reports only the two known moderate React Router issues;
  the offered forced breaking v7 upgrade was not applied.
- Tests: both repositories pass full `go test ./...` and `go vet ./...`; focused
  changed-package race suites and diff checks pass. Gateway limit/expanded-size,
  hard-timeout and 500-line benchmarks pass. Nexflow immutable limit,
  tenant-scoped cancellation metrics and Preview-contract benchmarks pass.
- Feature modes and production evidence: code only; no runtime mode, database or
  production service changed. Live PostgreSQL timings, UI INP and queue-age
  evidence remain part of T24 staged rollout.
- Next action: run T24 full race/build/migration/SQL/fault checks, create backups,
  deploy Gateway capability first, then deploy the same Nexflow commit to all
  four tenants with non-invoice routes kept shadow until controlled AOY parity.
- [ ] **T24 — Full verification and staged rollout**
  - [x] Full Go test/race/vet, frontend tests/lint/build and sales-only guard
  - [x] PostgreSQL 11 statement preparation and PostgreSQL 16 migration replay
  - [x] Fault matrix and source-to-Profile/stock trace proof
  - [x] Backup/deploy Gateway first, then the same Nexflow commit to all four
  - [x] Keep new routes shadow outside controlled AOY parity; no backfill
  - [x] Authenticated production dialog QA at desktop/390px, keyboard,
        accessibility, console/network and UI INP
  - [ ] Controlled AOY parity for each promoted route and USER reconfirm before
        resuming automation

### T24 production baseline record

- Deployed commits: Gateway `3e2ed0e`; Nexflow Demo/AOY/Lanboon/Ploy
  `ed70202`. The stopped Gateway rollback container is
  `nexflow-sml-api-bybos-pre-profile-v2-20260903-094700`.
- Verification: both repositories passed full `go test ./...`,
  `go test -race ./...` and `go vet ./...`. Frontend focused tests, lint with
  zero errors/35 pre-existing warnings, production build and the sales-only
  runtime guard passed. The repeated race fault matrix covers logs-DB outage,
  lost response, duplicate/cross-kind concurrency, hash mismatch, config race,
  capability mismatch, worker lease recovery and cross-tenant rejection.
- Database compatibility: migrations 091-093 replayed inside rollback
  transactions on all four PostgreSQL 16 Nexflow databases. The exact batched
  INV/SO/CN/SSC detail statements prepared successfully against PostgreSQL 11
  schemas `demo`, `aoy`, `lbk63` and `ploy_test`. Migration 093 now exists on
  every deployed Nexflow database.
- Runtime: Central Gateway health/readiness passes all four SML tenants and
  advertises revision `sml-sales-document-profile-v2-20260903`, all five routes,
  2 MiB request/expanded limits and 500 input/expanded items. All four Nexflow
  containers report Sale Invoice active and SO/SSC/SIC/CN shadow. Health,
  database authentication, edge routing, Shopee Gateway connectivity,
  before/after protected counts and severe-log scans passed.
- Configuration safety: the deployed AOY route bundle is Sale Invoice/Credit
  Note, config versions 2/1; capability and pair checks pass while automation
  readiness correctly remains false because Credit Note is shadow. A read-only
  Preview returned a signed token and a ten-minute expiry. Twenty live samples
  measured GET p95 3.117 ms and Preview p95 1.711 ms; unauthenticated GET and
  Preview both returned 401. No PUT, Enable or SML write was invoked.
- AOY automation: shop `264993963` is still enabled at the business-setting
  level but is fail-closed with `paused_reason=route_changed`, trigger
  `PROCESSED`, config version 5. All four live work queues are empty. The second
  shop remains disabled. Resume requires a current Preview and explicit USER
  reconfirm after controlled parity.
- Backups: Gateway source/runtime/image/container backup is under
  `/mnt/data/nextstep-node-2/deploy-backups/sml-sales-profile-v2-20260903-094700`.
  All tenant runtime environments have pre-change snapshots at timestamp
  `20260903-100335`; database backup names are recorded in Handoff above.
- Authenticated browser QA: USER signed in to AOY. The dialog passed desktop and
  true 390x844 checks, accessible labels/focus order, keyboard Preview/Escape,
  clean post-login Console and successful 200 Network responses. QA found the
  three footer buttons exceeded the 390px dialog by 18px; `ed70202` stacks them
  full-width below 640px. Production recheck proves body 390, dialog
  client/scroll width 388/388 and every action within x=17-373. No Save, Enable,
  route change or SML write occurred.
- Responsive-fix validation/deploy: frontend lint passes with zero errors and 35
  pre-existing warnings; production build and sales-only guard pass. Demo, AOY,
  Lanboon and Ploy health/database auth, protected counts, edge/Gateway probes
  and severe-log scans pass after deploying `ed70202`. Backups are Demo
  `pre-deploy-20260903-104107.sql.gz`, AOY
  `pre-deploy-20260903-104309.sql.gz`, Lanboon
  `pre-deploy-20260903-104505.sql.gz`, and Ploy
  `pre-deploy-20260903-104626.sql.gz`.
- Performance note: Preview/count XHRs completed in 31-66 ms. Event Timing shows
  3-5 ms application processing and 37-64 ms presentation. The CUA synthetic
  click adds 360-371 ms of tool input hold to the raw click entry; long-running
  real-user RUM remains monitoring evidence, not a release blocker.
- Next action: promote and write one route at a time only when USER is ready to
  create controlled evidence in SML; compare against committed fixtures before
  changing that route from shadow to active.

### T15 completion record

- Completed task: T15
- Nexflow commit: `e78a99b` (`docs: freeze SML sales profile completion contract`)
- Gateway commit: `304235e` (`test: freeze SML sales profile parity fixtures`)
- Files: `docs/sml-sales-document-profile-evidence.md`, durable plan/todo,
  Gateway PII-free JSON fixture and its contract test
- Tests: Gateway focused fixture test passes; no live database dependency
- Feature modes: no runtime or production behavior change
- Production evidence: prior read-only controlled SML evidence only; not deployed
- Residual risk: none for fixture retention; application behavior is still the
  pre-extension baseline until T16-T24 complete
- Next action: add failing T16 capability/revision and strict route-mode tests

### T16 completion record

- Completed task: T16 and Checkpoint A1
- Nexflow commit: `faaafda` (`feat: add fail-closed SML route capability foundation`)
- Gateway commit: `0ad872c` (`feat: expose versioned SML sales capabilities`)
- Tests: Gateway and Nexflow focused capability/route-mode tests passed; both
  full `go test ./...` suites passed. Existing legacy-response and canonical
  Sale Invoice hash tests remained green.
- Feature mode: no runtime mode or production deployment changed. When the new
  route map is absent, `saleinvoice` alone inherits the global mode and every
  added route remains `off`.
- Production evidence: not applicable; this checkpoint changes no production
  behavior and has not been deployed.
- Next action: T17 failing VAT 0/1/2 and exact-decimal tests, then the narrow
  Gateway/Nexflow corrections required by the frozen fixtures.

### T17 completion record

- Completed task: T17
- Nexflow commit: `020d655` (`fix: match SML zero-rate invoice totals`)
- Gateway commit: `cde4472` (`fix: align sale invoice VAT profile modes`)
- Tests: RED tests proved missing type-0 VAT register, wrong type-2 header totals
  and missing cross-field validation; focused suites then passed GREEN. Full
  `go test ./...` passed in both repositories. The exact pre-change canonical
  Sale Invoice hash is now pinned as
  `21f38aa96983afb7ed038e3290f6b15213d241aca53ac9110d7d5270924b8897`.
- Feature mode: no runtime mode or deployment changed.
- Production evidence: controlled no-PII fixtures from `INV26090003` and
  `INV26090004` are the authority; no production SML write was performed.
- Next action: T18 Sale Order Profile request/response/hash, `calc_flag=1`,
  route-specific logs and ERP JSON, required shipment, retry/mismatch and logs
  database outage tests.

### T18 completion record

- Completed task: T18
- Nexflow commit: `3df46be` (`feat: propagate sale order document profiles`)
- Gateway commit: `f9a20b8` (`feat: complete sale order document profile`)
- Contract evidence: `SO26090001` has detail `calc_flag=1`, one shipment,
  `menu_so_sale_order`, no VAT/AP-AR/GL rows, blank header tax-document fields,
  and only the five verified ERP JSON sections/keys. The implementation encodes
  those route differences instead of copying Invoice-only fields into SO.
- Tests: both repositories pass `go test ./...` and `go vet ./...`; Gateway
  OpenAPI parses with `jq -e`; diff checks pass. Regression coverage includes
  legacy wire bytes, active/shadow payloads, route hash mismatch, logs DB
  failure, route-aware durable retry, and the 500-row post-expansion ceiling.
- Feature mode: no runtime mode or production deployment changed. With the new
  route-mode variable absent, Sale Order Profile remains `off` and Sale Invoice
  continues to inherit the existing global mode.
- Production evidence: frozen PII-free fixture/read-only evidence only; no SML
  write or deployment occurred in T18.
- Residual risk: the Sale Order core still cannot be activated for automation
  until T19 SSC and T22 atomic route bundle/capability checks are complete.
- Next action: T19 failing SSC preview/create/doc-number and source-lock tests,
  then exact flag-37 reversal and stock-recalculation separation.

### T19 completion record

- Completed task: T19
- Nexflow commit: `c448d1e` (`feat: propagate sale order cancellation profiles`)
- Gateway commit: `9abef9a` (`feat: complete sale order cancellation profile`)
- Contract evidence: `SO26090001 -> SSC26090001` is encoded as TRANS_FLAG 37,
  `cancel_type=2`, detail `calc_flag=-1`, exact source line/branch/amount
  references, source `used_status=1`, no VAT/shipment/AP-AR/GL placeholder,
  blank main-menu value and only the four verified ERP JSON sections.
- Safety: all SSC/SIC/CN requests take one 3-second PostgreSQL advisory lock
  keyed by source type/document. SIC and CN also reject the other destination
  after taking the same lock. A Profile retry requires the existing BILLFLOW
  marker and matching canonical hash; a manual SML SSC is returned as
  `409 source_already_cancelled_externally` and is never adopted or relabelled.
- Tests: both repositories pass full `go test ./...`, `go test -race ./...`,
  `go vet ./...` and diff checks. Gateway OpenAPI parses with `jq`; focused
  tests cover SSC contract/hash/log shapes, lock timeout, doc-number aliases
  and request limits. Nexflow tests cover endpoint selection, active Profile
  fields, route-scoped mode/signature and immutable lost-response retry.
- Feature mode: no runtime mode or production deployment changed. The new
  route-mode variable is still absent, so SSC/SIC/CN remain `off`; Sale Invoice
  continues to inherit the deployed global `active` mode.
- Production evidence: committed PII-free fixture and prior read-only
  `SO26090001 -> SSC26090001` evidence only; no production SML write occurred.
- Residual risk: Checkpoint B remains open until the full database fault/
  concurrent test matrix is rerun with T20's sibling cancellation writers.
- Next action: T20 failing SIC/CN Profile parity and manual-ownership tests,
  then complete their transactional audit/VAT/AP-AR reconciliation paths.

### T20 completion record

- Completed task: T20
- Nexflow commit: `8b8f349` (`fix: preserve cancellation shadow compatibility`)
- Gateway commit: `4d93ebe` (`feat: complete invoice cancellation profiles`)
- Contract evidence: SIC is a header-only TRANS_FLAG 45 document that marks the
  source header/detail `last_status=1`. CN uses TRANS_FLAG 48 detail
  `calc_flag=1`, exact source line/branch references, source `total_value` for
  `ref_amount/ref_diff`, VAT-inclusive `total_amount` for the receivable, and a
  zero/non-zero VAT-register row for header VAT modes 0/1/2. Neither route
  inserts or updates `gl_journal` or `gl_journal_detail`.
- Ownership/recovery: a Profile request never adopts a same-kind manual SML
  cancellation without a BILLFLOW marker. Matching owned documents reconcile
  only missing VAT/AP-AR/main-log/ERP-log relationships; sibling SIC/CN intents
  still conflict under the shared source advisory lock.
- Tests: Gateway and Nexflow pass full `go test ./...`, `go test -race ./...`
  and `go vet ./...`; Gateway OpenAPI parses and both diffs pass whitespace
  checks. Regression tests cover VAT 0/1/2, header-only SIC, exact blank detail
  branch, audit section/menu shape, external ownership, and unchanged legacy
  invalid-date fallback behavior.
- Feature mode: no runtime mode or production deployment changed. Active mode
  sends Profile fields; shadow now validates its configured remark but omits all
  Profile extension fields so legacy Gateway writes remain byte-compatible.
- Production evidence: committed PII-free fixtures and prior read-only manual
  SML evidence only; no production SML write occurred in T20.
- Residual risk: core creation and synchronous Profile reconciliation are safe,
  but a logs-database outage still needs the T21 durable profile-only job for
  SIC/CN. Checkpoint B remains open until that worker/fault matrix passes.
- Next action: migration 093, fenced cancellation reconciliation repository,
  worker, and proof that retry cannot invoke core or enqueue stock twice.

### T21 completion record

- Completed task: T21 and local Checkpoint B
- Nexflow commits: `eb53648` (`feat: persist cancellation profile repair jobs`)
  and `9a1f2e1` (`feat: reconcile cancellation profiles safely`)
- Gateway baseline: `4d93ebe` from T20; no additional Gateway change was
  required for the durable Nexflow queue.
- Migration: additive/idempotent 093 adds cancellation core/Profile summary,
  byte-exact immutable request storage, and a unique
  `cancellation_id + profile_version` repair queue with lease owner/token,
  bounded exponential backoff, ten-attempt ceiling and manual-reconciliation
  state.
- Safety: core completion, Profile summary and initial repair job are persisted
  in one Nexflow transaction. A repair claim requires the configured tenant and
  a successful cancellation core row. Completion/failure/manual retry SQL only
  changes Profile/job columns; it cannot change cancellation core status or
  enqueue/reset the separate stock recalculation fields. An inactive route is
  deferred without consuming an attempt. Admin retry is role-restricted.
- Tests: repository tests prove transactional enqueue, tenant-scoped fenced
  leases, ten-attempt terminal state, and Profile-only manual reopening. Handler
  test sends the exact persisted bytes once with correlation ID and expects only
  Profile/job completion SQL. Full Nexflow `go test ./...`,
  `go test -race ./...`, `go vet ./...` and diff checks pass.
- Feature mode: no runtime mode or production deployment changed. The worker
  starts only when at least one cancellation route is `active`; current safe
  defaults keep all new cancellation routes `off`.
- Production evidence: none yet; migration 093 is committed but not applied.
- Residual risk: PostgreSQL 16 migration replay, real logs-DB fault injection
  and deployed worker recovery stay in T24. Configuration must be made atomic
  and capability-bound in T22 before any new route can be activated.
- Next action: implement the atomic main/cancellation route bundle APIs and the
  unified admin dialog, then browser-test desktop and 390px.

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

### AOY release checkpoint: compact bill header and complete SML summary

- Nexflow commits: compact header/detail presentation `dcf9fbd`; immutable sale
  inquiry type and historical fallback `68ec5c9`.
- Behavior: the back action now shares the document-number row; the header owns
  the SML number, sale route and AUTO/manual success state. The redundant success
  card is removed while actionable recovery remains visible. Shopee order time
  is formatted in Asia/Bangkok. The compact summary now exposes
  `inquiry_type`, `remark` and `remark_2` without expanding the admin JSON.
- Data integrity: future sale-invoice and sale-order payloads always serialize
  `inquiry_type` with precedence explicit first-send value -> channel default ->
  legacy-compatible fallback 0. Validation accepts only sale values 0-3; import,
  manual, bulk, Auto SML and retry paths use the same resolved config. The
  existing Gateway already writes this field to SML header and detail, so no
  Gateway deployment or SML document rewrite was required.
- Tests: `go test ./...`, `go test -race ./...`, `go vet ./...`, focused
  frontend presentation/payload tests, production build and sales-only release
  guard pass. Lint has zero errors and 35 pre-existing warnings.
- Production: deployed AOY-only at `68ec5c9`; Demo, Lanboon and Ploy were not
  rebuilt. Backup is `pre-deploy-20260902-173738.sql.gz`. AOY backend/database,
  frontend, edge and Shopee Gateway health pass; before/after protected counts
  are unchanged and the recent severe-error scan is clean.
- Browser: desktop 1280x720 and 390x844 QA pass with no horizontal overflow and
  zero console warnings/errors. No config save, SML write or document resend was
  performed during QA.
- Next action: monitor nine more Profile documents and confirm that the next new
  immutable request contains its selected `inquiry_type`, then capture deployed
  p95/p99/queue-age evidence for T11.

### AOY release checkpoint: plain inquiry type and Shopee API evidence

- Nexflow commits: human-only inquiry type and conditional empty-evidence
  presentation `dcc660a`; removal of the remaining technical field label
  `483160d`.
- Behavior: the SML summary displays `ประเภทรายการ` / `ขายเงินเชื่อ` without a
  raw field name, numeric code or default suffix. Shopee API bills hide the
  original-evidence card only when it has no files; a real artifact still shows,
  and non-API import flows preserve their existing evidence state. The Shopee
  source shop and order remain in the Bill header.
- Tests: bill-detail presentation, SML payload summary, Document Profile and
  timeline focused suites pass; production build passes; lint has zero errors
  and 35 pre-existing warnings.
- Production: deployed AOY-only at `483160d`; Demo, Lanboon and Ploy were not
  rebuilt. Backups are `pre-deploy-20260902-180737.sql.gz` and
  `pre-deploy-20260902-181122.sql.gz`. Backend/database, frontend, edge and
  Shopee Gateway health pass; protected before/after counts are unchanged and
  the recent severe-error scan is clean.
- Browser: exact production bill `BF-INV26090002` passes desktop 1280x720 and
  mobile 390x844 QA. The new label/value are present, technical/default text and
  the empty artifact copy are absent, the page has no horizontal overflow, and
  the console has no warning/error.
- Next action: monitor nine more Profile documents and confirm that the next new
  immutable request contains its selected `inquiry_type`, then capture deployed
  p95/p99/queue-age evidence for T11.

### Four-tenant application alignment and daily closeout

- Runtime commit: Demo, AOY, Lanboon and Ploy all run application commit
  `483160d`. The later branch-only commit `476f05a` contains handoff
  documentation and was not required in runtime images.
- Tenant isolation: databases, credentials, channel routes, shop settings and
  feature flags were preserved. `SML_DOCUMENT_PROFILE_MODE` remains `active`
  only on AOY; its absence on Demo, Lanboon and Ploy resolves to the code's
  fail-closed `off` default. Ploy's pre-existing global Auto SML capability flags
  remain unchanged and no shop was enabled by this deployment.
- Backups: Demo `pre-deploy-20260902-181634.sql.gz`; Lanboon
  `pre-deploy-20260902-181809.sql.gz`; Ploy
  `pre-deploy-20260902-181838.sql.gz`.
- Deploy verification: sales-only guard passed before each deployment; all four
  public `/health` endpoints return HTTP 200 with production/database status OK;
  backend database authentication, frontend status, edge health, Shopee Gateway
  connectivity and recent severe-error scans pass. Protected counts are exactly
  unchanged before/after on each newly deployed tenant.
- Frontend evidence: the deployed Bill bundle for every tenant contains the
  plain `ประเภทรายการ` / `ขายเงินเชื่อ` presentation and does not contain the
  removed technical label. AOY exact-bill desktop/mobile QA remains the
  behavioral proof for the Shopee API empty-evidence rule.
- Dependency evidence: production-only npm audit has zero high/critical and two
  previously recorded moderate React Router advisories. The full build tree also
  reports Vite/esbuild development-server advisories; those tools are not copied
  into the nginx runtime image and no forced breaking upgrade was applied.
- Closeout: no active deployment remains on 2026-09-03. Next action on resume is
  the remaining nine AOY Profile documents plus deployed percentile/queue-age
  evidence for T11.

### Four-tenant Document Profile activation

- Change type: production runtime configuration only; no application/Gateway
  source change and no database migration. All tenants remain on application
  `483160d`, and the Central Gateway remains on `16c550d`.
- User decision: set `SML_DOCUMENT_PROFILE_MODE=active` on Demo, Lanboon and
  Ploy so the Profile path is ready when each customer later connects Shopee.
  AOY was already active and was not restarted.
- Safety proof before activation: each new tenant had zero active Shopee
  connections, zero open Auto SML jobs and zero open Profile reconciliation
  jobs. After activation, Demo/Lanboon/Ploy still have zero active connections,
  zero enabled Auto SML shops and zero open Auto/Profile jobs; no historical
  order was backfilled and no SML document was created.
- Runtime backups: Demo
  `/mnt/data/nextstep-node-2/nexflow/.env.pre-sml-document-profile-active-20260903-024305`;
  Lanboon
  `/mnt/data/nextstep-node-2/nexflow-lanboon/.env.pre-sml-document-profile-active-20260903-024355`;
  Ploy
  `/mnt/data/nextstep-node-2/nexflow-ploy/.env.pre-sml-document-profile-active-20260903-024430`.
- Verification: Docker runtime inspection reports `active` for all four tenants;
  all four public `/health` endpoints report production/database OK, recent
  backend severe-error scans are empty, and open Profile jobs are zero. AOY
  retains one active Shopee connection and one enabled Auto SML shop; the other
  three tenants retain zero of each.
- Rollback: restore the exact tenant `.env` backup above and force-recreate only
  that tenant's backend. Documents, attempts and migrations must remain intact.
- Next action: validate the SML route and signed preview for a newly connected
  tenant before enabling its shop-level Auto SML setting.

### Release-gate reclassification

- User decision: AOY may continue normal production use without waiting for an
  unpredictable number of new Shopee orders.
- Release status: the controlled end-to-end document `BF-INV26090002`,
  no-duplicate proof, stock result, health checks and rollback controls satisfy
  the release gate. No code or runtime configuration changed in this checkpoint.
- Monitoring status: the next nine documents are a passive post-release sample,
  not a deployment gate, deadline or reason to generate artificial orders.
  Evidence is accumulated only when real orders naturally arrive.
- Next action: remain idle until organic AOY traffic provides a new document or
  production telemetry has enough samples for T11.

### Shared Gateway VAT effective-period hotfix

- Incident: SML daily processing of `BF-INV26090002` initially produced five GL
  lines with no output-VAT credit. Debit exceeded credit by 20.15, exactly the
  document VAT. The source header, detail, revenue, shipping, and cost amounts
  were otherwise correct.
- Root cause: Profile V1 omitted
  `gl_journal_vat_sale.vat_effective_period/year`, stored `0/0` in
  `erp_logs.data_new.screenvatsale`, and copied the route-controlled header
  `vat_type=1` into a sale-register field whose verified SML value is 0.
- Gateway fix: `53393b6` derives the effective month and Buddhist year from the
  validated document date, writes them transactionally to the VAT register and
  ERP audit JSON, and keeps header/register VAT types independent. No public
  request field, UI setting, migration, direct GL write, or historical backfill
  was added.
- Tests: focused regression tests first failed with the old 14-argument INSERT,
  zero audit period/year, and register type 1. After the fix, focused and full
  `go test ./...`, `go test -race ./...`, and `go vet ./...` pass. The exact SQL
  passed non-mutating `PREPARE` against all four production SML tenant schemas.
- Deploy: the new image passed a separate canary container, authenticated
  capability check, and readiness for `demo`, `aoy`, `lbk63`, and `ploy_test`
  before the shared production container was switched. All four Nexflow
  backends reach the new Gateway, all public health endpoints return HTTP 200,
  and there are zero open Auto SML/Profile jobs and no fatal/panic/HTTP 5xx log.
- Preservation: user-corrected `BF-INV26090002` remains register period 9/year
  2569/type 0 with VAT credit 20.15 and balanced GL 434.96/434.96. Immutable
  historical audit rows were not rewritten.
- Rollback: backup
  `/mnt/data/nextstep-node-2/deploy-backups/sml-vat-effective-period-20260903-034424`;
  stopped prior container
  `nexflow-sml-api-bybos-pre-vat-20260903-034639`; previous image remains tagged
  inside the backup record. Restore the prior container only if the shared
  Gateway becomes unhealthy; never delete SML documents during rollback.
- Next action: verify the first naturally occurring post-hotfix VAT document
  after the user's normal SML daily processing.

For every completed task, update Handoff with:

1. Exact commit(s) or explicit reason no commit was made
2. Files changed
3. Focused and broad validation commands with result
4. Feature mode and tenant scope
5. Production evidence or “not deployed”
6. Known residual risk and the single next action
