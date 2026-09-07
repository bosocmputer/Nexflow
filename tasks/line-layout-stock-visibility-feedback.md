# LINE layout and AOY stock visibility — 2026-09-07

## Scope / handoff

- User requested full-width LINE sender tables with sample/history icon dialogs, and read-only investigation of missing endpoint/stock evidence on AOY bills.
- UI implemented locally in `frontend/src/pages/LineNotifications.tsx`; not deployed or committed in this checkpoint.
- No backend, migration, SML API BY BOS, runtime flags, SML documents, LINE delivery, or stock jobs changed.
- Preserve pre-existing dirty AGENTS.md, docs/current-state.md, docs/nextstep-server-deploy-flow.md and untracked artifacts/tasks.

## Completed

- Removed persistent 360px sidebar; sender, candidate and recipient tables use full content width.
- Dedicated quota column; sample/history accessible icon controls open a scrollable dialog.
- Dialog returns focus to its initiating icon; opening dialogs uses existing overview data and makes no outbound call.
- TypeScript/build passed; lint passed with 35 existing warnings and no errors.
- Quota/timeline focused tests: 5 passed (existing Vite HMR port warning during tests).
- Local browser with synthetic, network-isolated data: full-width desktop screenshot, quota 18/300, sample dialog, history empty dialog verified. Temporary fixture files removed afterwards.
- Not yet verified: 390px, real tenant browser after deployment, populated history dialog, console/network instrumentation.

## Read-only AOY evidence

Both requested bills have immutable route `/api/v1/ic/sale-invoices`, sent attempts, and completed stock jobs.

| Document | processstock success (Asia/Bangkok) | balance verified | job |
| --- | --- | --- | --- |
| BF-INV26090002 | 2026-09-02 22:31:07.316313 | 22:31:07.375966 | completed, 1 attempt |
| BF-INV26090003 | 2026-09-07 07:35:08.312389 | 07:35:08.388016 | completed |
| BF-INV26090004 | 2026-09-07 09:42:48.214571 | 09:42:48.252696 | completed |
| BF-INV26080064 | 2026-08-27 11:01:36.209500 | 11:01:36.254469 | completed, 9 attempts |

Latest 10 stock jobs queried were completed. This confirms recorded processstock success plus downstream balance verification, not a new SML accounting audit.

`BF-INV26080064` has historical audit `sml_stock_recalc_ok` at 2026-08-27 04:02:09 UTC; its detail contains item AH-0009 and stock_movement evidence. `BF-INV26090002` has no matching stock-success audit row, but its durable job is completed.

Root cause of visibility gap:
- `bills_sml_attempt.go` calls the old audit-producing recalculation path only when reservation ledger is disabled.
- `services/stockrecalc/worker.go` persists processstock success and verifies balances/evidence through durable repository methods without adding that audit milestone.
- Bill timeline reads audit records; `SMLDocumentStatusCard` hides itself when `documentSendPresentation.complete` is true.
- Both requested historical bills have zero HTTP exchange rows; instrumentation deployed later cannot reconstruct old HTTP exchanges. Their immutable route snapshots still prove the configured sale endpoint.

## Next action

- Complete responsive/console QA and release UI through normal backup/canary/deploy workflow when releasing this follow-up.
- Proposed separate visibility fix: expose existing durable stock-job outcome in bill history/summary, explicitly labelled as job evidence (do not fabricate historical HTTP exchanges or audit timestamps).
- Do not resend core or stock: no missing processing was found for the requested/recent bills. User asked to diagnose this part; no business-flow change implemented.

## Authorized AOY follow-up release

- User authorized fixing the visibility gap and deploying AOY only; user will do UI acceptance testing.
- Code commit `0d533b82ee4aa625fee36132d30e3208e9492588` adds a stock summary above the bill timeline, using the existing `sml_stock_job_status` only. It does not add synthetic historical events or timestamps, and missing/unknown status never implies success.
- Verification: 7 bill-presentation tests passed; production build passed; lint 0 errors / 35 pre-existing warnings; sales-only guard and diff whitespace check passed.
- Deployment started with `--target aoy --ref 0d533b8`. No new migration. Demo/Lanboon/Ploy and SML API BY BOS are outside this release.
- Rollback: deploy prior application `175cbb1` to AOY, retaining all data and migration 094.
- Next action: record backup, health, DB authentication and deployment outcome; user performs UI acceptance.

### Release result

- AOY deploy completed successfully at `0d533b8` on 2026-09-07. No other tenant application deployed; existing shared edge remained running.
- Backup: `/mnt/data/nextstep-node-2/nexflow-backups/aoy/pre-deploy-20260907-084250.sql.gz` (1.1 MiB).
- Backend health/database authentication passed, frontend/edge login returned 200, public HTTPS health returned database/status ok; recent backend error scan empty.
- Catalog/alias/mapping counts unchanged at 65/74/43. Gateway health ok; internal gateway API remains externally blocked (404).
- Existing deploy sanitizer removed `PURCHASE_FLOW_ENABLED` (disabled default); no Shopee/SML/LINE business settings changed.
- User acceptance of LINE layout and bill stock summary is pending as requested. No test LINE send, SML resend, or stock recalculation was triggered.

## Info-button follow-up

- User clarified that stock outcome belongs inside “ประวัติระบบ”, and authorized an info button like the existing cancellation popover.
- `e68d085`: moved current stock-job summary under system history, clearly labelled as current state rather than a historical event; added shared info popover there and beside sale SML document numbers in Shopee Operations. Existing cancellation popover remains unchanged.
- Popover loads only on open through the existing authorized bill GET, displays document/method/stock outcome, and has loading/error/unknown states. No per-row request waterfall; no business writes or synthetic audit records.
- Build and 7 presentation tests passed; focused lint 0 errors / 24 existing ShopeeOperations warnings; sales-only deploy guard passed.
- AOY-only deploy initiated; backup `/mnt/data/nextstep-node-2/nexflow-backups/aoy/pre-deploy-20260907-085505.sql.gz`.
- Deploy completed successfully at `e68d085`: DB authentication, backend/public health, frontend/edge login 200 and Gateway health passed; recent backend error scan empty. Counts unchanged. Other tenant applications untouched. User browser acceptance remains next action; no full responsive/popover browser QA claimed for this follow-up.
