# Todo: Nexflow AOY -> SML Document Profile V1

## Handoff

- Last completed: T07 Gateway existing-document reconciliation implementation
- Active task: T08 shared Nexflow payload propagation
- Nexflow branch/HEAD: `codex/marketplace-units-conversion` / `d162b00`
- Gateway branch/HEAD: `codex/include-sml-unit-use-status-one` / `a04b500`
- Feature mode: implemented in code with default `off`; production unchanged
- Tests: both repositories `go test ./...` pass; Gateway race/vet pass; frontend focused tests/build pass; lint has 0 errors and 35 pre-existing warnings
- Production evidence: read-only AOY parity inspection only; no production mutation performed
- Preserved user work: modified `AGENTS.md`, `docs/current-state.md`,
  `docs/nextstep-server-deploy-flow.md`; existing `tasks/plan.md` and
  `tasks/todo.md`; untracked `.serena/` and `scripts/__pycache__/`
- Blocker: none; production writes remain unavailable because Nexflow send paths do not opt in yet and runtime mode defaults to `off`
- Next action: implement one immutable resolver/snapshot for manual, bulk, Auto SML, retry and cancellation send paths

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

- [ ] **T08 — Shared payload resolver for manual/bulk/Auto/retry/cancel**
- [ ] **T09 — Durable profile reconciliation jobs with leases/fencing**
- [ ] **T10 — Logs, timeline and profile-only recovery UX**

## Phase E — Production verification

- [ ] **T11 — Measured performance baseline and budgets**
- [ ] **T12 — Security/abuse tests and dependency audit triage**
- [ ] **T13 — Correlation IDs, structured events, metrics and runbook alerts**
- [ ] **T14 — Full tests, browser QA and failure injection**

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

For every completed task, update Handoff with:

1. Exact commit(s) or explicit reason no commit was made
2. Files changed
3. Focused and broad validation commands with result
4. Feature mode and tenant scope
5. Production evidence or “not deployed”
6. Known residual risk and the single next action
