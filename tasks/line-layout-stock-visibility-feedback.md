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
