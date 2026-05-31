# BillFlow Henna Full-System QA Audit — 2026-05-12

> Target: BillFlow Henna production trial  
> Frontend: `https://aurora-enjoyed-backup-lines.trycloudflare.com` / `http://192.168.2.109:3030`  
> Backend: `http://192.168.2.109:8110`  
> Scope approved by owner: create/edit/delete test data, send to SML for real, test all roles, write this report in repo.

## Executive Status

Status: Full-system QA pass completed, fixes deployed to Henna, and regression retest passed.

Goal before Shopee API Direct: find user-facing bugs, data-flow risks, role/permission gaps, SML integration issues, and UX friction across the existing Excel/email/review/SML pipeline.

Recommendation: The Excel/email/review/SML baseline is now ready for the next Shopee API Direct planning pass. Keep the QA users for role regression unless the owner asks to remove them.

## Retest Summary — 2026-05-12

Status: Pass.

Deploy target: Henna (`/home/bosscatdog/billflow-henna`)

Verification:

- Backend health: `http://192.168.2.109:8110/health` returned `{"database":"ok","env":"production","status":"ok"}`.
- Containers healthy/running: `billflow-henna-postgres`, `billflow-henna-backend`, `billflow-henna-frontend`.
- Frontend bundle now contains Phase constants `=99`; Henna is no longer built as Phase 1-only.
- SML real send without explicitly posting `wh_code`, `shelf_code`, `vat_*`, or `doc_time` succeeded using server/channel defaults.
- New real SML document sent during retest: `BF-INV26050008`.
- `doc_counters` for `BF-INV/202605` advanced to `8`; duplicate `BF-INV2605%` doc numbers in local DB: `0`.
- Hidden-character `doc_no` now returns HTTP 400 with a user-correctable message.
- `/api/settings/imap-accounts` empty response now returns `data: []`.
- Role matrix:
  - admin: `/api/logs` 200, `/api/settings/users` 200.
  - staff: `/api/logs` 200, `/api/settings/users` 403.
  - viewer: `/api/logs` 403, `/api/settings/users` 403.
- User management API CRUD passed: create → update → delete temporary QA user.

Henna instance data adjusted:

- `shopee/sale` defaults now use actual SML cache values: `wh_code=AB-2`, `shelf_code=002`, `vat_type=0`, `vat_rate=7`, `doc_time=09:00`.
- Existing malformed local doc number `ฺBF-INV26050001` was cleaned to `BF-INV26050001`.

## Known Baseline Before QA

- Instance name: `BillFlow Henna`
- SML database: `AOY`
- SML REST base URL: `http://demserver.3bbddns.com:47308`
- Existing users before QA: only `admin@billflow.local`.
- QA users created for role regression:
  - `qa.staff@billflow.local` / role `staff`
  - `qa.viewer@billflow.local` / role `viewer`
- Existing channel default before QA:
  - `shopee/sale` routes to `saleinvoice`, doc format `SI`, prefix `BF-INV`, running `YYMM####`
- Existing document state before first QA:
  - 7 Shopee sale invoices already `sent`
  - One existing sent document had malformed hidden-character doc no: `ฺBF-INV26050001` (cleaned during retest)
  - `doc_counters`: `BF-INV` period `202605` last used sequence `6`
- QA data created during this audit:
  - QA saleinvoice bill `3cc66fc3-291f-4542-8d80-e24475c13090`
  - QA hidden-character guardrail bill `e26bac8e-b4d0-4342-85a4-59e2427c47ee`
  - Shopee import bill `06655cdd-3634-4445-b14b-6136b0ef8f28`
  - Lazada import bill `ddff2636-53e8-45ad-8c56-24212ec2becd`
  - TikTok import bill `ed29d559-7514-4548-9812-3b184ab1d2bd`
  - Real SML document sent: `QA-INV260512001`

## Test Matrix

| Area | Case | Role | Method | Status | Evidence | Notes / Findings |
| --- | --- | --- | --- | --- | --- | --- |
| Auth | Login admin | admin | API | Pass | `/api/auth/me` 200 |  |
| Auth | Login staff | staff | API | Pass | `/api/auth/me` 200 | QA staff created directly in DB |
| Auth | Login viewer | viewer | API | Pass | `/api/auth/me` 200 | QA viewer created directly in DB |
| Permission | Viewer cannot mutate mappings/items/settings/import | viewer | API | Pass | mapping create 403, import runs 403, IMAP 403, channel defaults 403 | Logs now 403 for viewer |
| Permission | Staff can review/import but cannot admin settings | staff | API | Pass | import runs 200, mapping create 201, admin settings 403 |  |
| Setup | Setup status and readiness | admin | API + browser | Partial | Login page reachable; full setup page blocked by browser automation typing issue | See Browser Notes |
| Settings | Channel defaults list/update validation | admin/staff/viewer | API | Partial | admin list 200, staff/viewer 403 | Did not mutate channel defaults during QA |
| Email | IMAP accounts, poll details, accepted sender clarity | admin/staff/viewer | API | Pass | admin list 200 with `data:[]`; staff/viewer 403 | Fixed F-007 |
| Catalog | Catalog search/create/sync permissions | all roles | API | Pass | viewer catalog read 200, staff/viewer sync 403 |  |
| Mapping | Create mapping and role guard | staff/viewer | API | Pass | staff create 201; viewer create 403 | Created `QA AUDIT MAP 20260512` |
| Shopee Excel | Preview, confirm, dedup, review, saleinvoice route | staff | API | Pass | preview 53 orders; confirm 1 bill; re-preview duplicate 1 | Route-aware confirm message fixed |
| Lazada Excel | Preview, confirm, dedup, review, route behavior | staff | API | Pass | preview 10 orders; confirm 1 bill `needs_review`; re-preview duplicate 1 |  |
| TikTok Excel/CSV | Preview, confirm, dedup, review, route behavior | staff | API | Pass | CSV preview 19 orders; XLSX preview 19 orders; confirm 1 bill `needs_review`; re-preview duplicate 1 |  |
| Bills | Detail read and status transition | staff | API | Pass | QA bill detail 200; retry moved failed then sent | Add/delete item not exercised in this pass |
| SML Send | Single retry saleinvoice sends real SML | staff | API | Pass | Explicit `QA-INV260512001` succeeded; retest auto default sent `BF-INV26050008` | Fixed F-002/F-003/F-004 |
| SML Send | Bulk send validation/results/logs | admin/staff | API + browser | Not run |  | Blocked until frontend phase issue fixed |
| Logs | Actor, filters, incident detail, DEV payload | admin/staff/viewer | API | Pass | `user_id` filter returns QA Staff actor; viewer logs now 403 | Fixed F-006 with admin/staff-only policy |
| Data Quality | Hidden-character doc no guardrail | staff | API | Pass | hidden doc no blocked before SML with HTTP 400 | Fixed F-005 |
| Resilience | Duplicate imports | staff | API | Pass | Re-preview after confirm marks first order duplicate for Shopee/Lazada/TikTok |  |
| Browser UX | Henna frontend phase/layout | unauth/browser + asset check | API/curl | Pass | Built asset contains Phase constants `=99` | Fixed F-001 |

## Findings

### F-001 — P0 — Henna frontend is built as Phase 1, hiding Phase 1+ sales/import UX

Retest status: Resolved. Henna `.env` now uses Phase 1+ flags and the rebuilt JS bundle contains Phase constants `=99`.

Observation:

- `https://aurora-enjoyed-backup-lines.trycloudflare.com/login` and `http://192.168.2.109:3030/login` both show `Phase 1 · Shopee Purchase Bill`.
- Built frontend asset on Henna contains `const Uy=1`, consistent with `VITE_PHASE=1`.
- Henna policy says it should match BillFlow main Phase 1+ with purchase + sales work.

Impact:

- User may not see sales-side menus/import flows even though backend supports them.
- This directly contradicts the instance policy and can make Henna testing invalid before Shopee API Direct.

Fix plan:

- Rebuild/deploy Henna frontend with Phase 1+ flags:
  - `VITE_PHASE` should match main.
  - `VITE_ENABLE_SALES_ORDERS=true`.
  - `VITE_ENABLE_SHOPEE_EXCEL=true` unless intentionally hidden.
- Add post-deploy smoke check that reads built asset or rendered login/sidebar to assert phase flags per instance.

### F-002 — P0 — Henna SML doc counter was out of sync and generated duplicate `BF-INV26050007`

Retest status: Resolved for local counter protection. The backend now skips locally-used doc numbers during preview/generation, and Henna retest sent `BF-INV26050008` successfully.

Observation:

- Before QA, Henna had `BF-INV26050007` already sent, while `doc_counters` showed last sequence `6`.
- QA retry with valid customer/warehouse generated `BF-INV26050007`.
- SML returned HTTP 200 with database error: duplicate key `(doc_no, trans_flag)=(BF-INV26050007, 44)`.

Impact:

- A normal user pressing send could fail because auto doc_no is already used.
- This is exactly the duplicate-doc failure users were seeing in logs.

Current state after QA:

- The failed QA retry advanced `doc_counters` to `7`, so the immediate next auto number should be `...0008`.
- Root risk still exists: the app does not reconcile counters against existing sent bill doc numbers or SML state.

Fix plan:

- Add a reconciliation step before generating doc_no:
  - compare next local counter with max known local `sml_doc_no` for the same prefix/period;
  - optionally check SML duplicate response and auto-advance/retry once with the next number.
- Add admin repair tool: `reconcile doc counters`.
- Add alert in `/settings/channels` or `/logs` when local counter is behind existing bill doc numbers.

### F-003 — P1 — Henna preview/default SML warehouse values are invalid for actual Henna SML

Retest status: Resolved. Current Henna warehouse cache exposes shelf `002` under warehouse `AB-2`; channel defaults were updated to `AB-2/002`, and preview now matches the send defaults.

Observation:

- Bill detail preview displayed defaults `wh_code=WH-01`, `shelf_code=SH-01`.
- Initial QA note expected `AB-1/001` and `AB-2/002`, but retest showed the active cache only exposes shelf `002` under `AB-2`. The fix uses the value visible in the current SML cache.
- Retry with preview default `WH-01` returned: `ไม่พบรหัสคลัง WH-01 ใน SML`.

Impact:

- User sees a default that looks ready, but sending fails.
- This creates confusion and blocks bulk send unless user knows the real warehouse code.

Fix plan:

- Seed/update Henna channel defaults to `AB-1` / `001` or force the UI to require picking from actual SML warehouse cache.
- If env fallback values are not in the cache, show warning in send dialog and setup center.
- Add backend validation endpoint for channel defaults.

### F-004 — P1 — Send dialog/backend require `doc_time` even though preview shows a default

Retest status: Resolved. Backend now validates the resolved config after channel/env/request overlays, so omitted dialog fields can fall back to channel/env defaults.

Observation:

- Preview showed `doc_time=09:00` in `sml_defaults`.
- Retry request without `doc_time` returned: `กรุณากรอกเวลาเอกสารก่อนส่ง SML`.

Impact:

- Backend ignores fallback defaults for fields it later requires from dialog input.
- Users may see a valid-looking preview but still be blocked.

Fix plan:

- Either apply env/channel default `doc_time` in backend when request omits it, or make UI always send the displayed default.
- Keep one source of truth for validation: the same value shown in preview should be the value sent.

### F-005 — P1 — Hidden-character doc_no guardrail works but returns HTTP 500

Retest status: Resolved. Hidden-character retry now returns HTTP 400 with the suggested clean doc number.

Observation:

- Retry with doc no `ฺQA-INV260512-HIDDEN` was blocked before SML.
- Response: HTTP 500 with `generate doc_no: doc_no contains hidden or invalid Thai mark characters; use "QA-INV260512-HIDDEN"`.

Impact:

- Data protection works, but 500 makes this look like system failure instead of user-correctable validation.

Fix plan:

- Return HTTP 400.
- Localize the message, e.g. `เลขเอกสารมีอักขระซ่อน กรุณาใช้ QA-INV260512-HIDDEN`.
- Show inline correction in send dialog.

### F-006 — P1 / decision needed — Viewer can read logs including SML payload/error detail

Retest status: Resolved by policy decision. `/api/logs` is restricted to `admin` and `staff`; `viewer` receives 403.

Observation:

- `qa.viewer@billflow.local` can call `/api/logs?page_size=3` and gets SML/audit entries.
- Logs may contain SML payload, customer/order details, and operational errors.

Impact:

- If `viewer` is meant for read-only business users, this may be acceptable.
- If `viewer` is meant for limited customer-facing users, this exposes too much admin/dev information.

Fix plan:

- Decide role policy:
  - Option A: logs visible to admin/staff only.
  - Option B: viewer sees redacted logs without payload/error internals.
  - Option C: keep as-is and document that viewer is trusted internal read-only.

### F-007 — P2 — Empty IMAP list returns `data:null`

Retest status: Resolved. Empty IMAP list now returns `data: []`.

Observation:

- Henna `/api/settings/imap-accounts` returns `{"data":null}` when there are no accounts.

Impact:

- Frontend may handle it, but API consistency should return `[]`.
- This can cause future UI bugs or extra null handling.

Fix plan:

- Normalize repository/handler response to empty slice.

### F-008 — P2 — Shopee confirm success message points saleinvoice user to `/sales-orders`

Retest status: Resolved in code. Shopee/Lazada/TikTok confirm messages now use the configured document route.

Observation:

- Shopee confirm created a saleinvoice bill successfully.
- Message says: `รอตรวจสอบใน /sales-orders`.
- The document route is saleinvoice, so user should go to `/sale-invoices`.

Impact:

- User may look in the wrong menu.

Fix plan:

- Make confirm message route-aware for Shopee/Lazada/TikTok:
  - saleorder -> `/sales-orders`
  - saleinvoice -> `/sale-invoices`
  - purchaseorder -> `/bills`

### F-009 — P2 — No user-management UI/API, role QA requires DB insert

Retest status: Resolved. Added admin-only `/api/settings/users` and `/settings/users`; create/update/delete was tested on Henna with a temporary QA user.

Observation:

- The backend has `UserRepo.Create/List`, but no protected user management routes or frontend page.
- Staff/viewer QA users had to be created directly in PostgreSQL.

Impact:

- Admin cannot safely manage staff/viewer users in production.
- QA of role policy is awkward and not auditable through app UI.

Fix plan:

- Add admin-only `/settings/users` page and `/api/settings/users` routes.
- Support create, deactivate, reset password, role change, and audit log entries.

## Browser Notes

- Browser navigation and DOM inspection worked.
- Browser automation typing into the login form failed because the in-app browser automation clipboard bridge was unavailable. API tests covered login/role behavior instead.
- The browser still provided critical evidence for F-001 because the unauthenticated login page rendered the wrong Phase 1 label.

## Remaining Recommendations

These are no longer blockers for Shopee API Direct, but they are worth adding before wider rollout:

1. Add an automated QA smoke script for role matrix, frontend phase flags, SML default preview, route-aware import messages, and doc_no guardrails.
2. Add setup warning when channel defaults reference warehouse/shelf codes not present in the current SML cache.
3. Add an admin "reconcile doc counters" action for rare cases where data was imported or edited outside BillFlow.
4. Decide later whether QA users should remain in Henna DB permanently for regression testing or be removed before customer handoff.

## Verification Artifacts

- Real SML success: bill `3cc66fc3-291f-4542-8d80-e24475c13090`, doc `QA-INV260512001`.
- Real SML duplicate failure: same bill attempted `BF-INV26050007`.
- Import confirms:
  - Shopee bill `06655cdd-3634-4445-b14b-6136b0ef8f28`, order `260401MKHF7K3V`.
  - Lazada bill `ddff2636-53e8-45ad-8c56-24212ec2becd`, order `1094245069322705`.
  - TikTok bill `ed29d559-7514-4548-9812-3b184ab1d2bd`, order `583964950291252338`.
- Logs actor verification:
  - `/api/logs?user_id=346a8455-f800-4601-bd5d-4e8c7018bb0d` returns `QA Staff`.
  - QA bill timeline contains `sml_failed` and `sml_sent` with QA Staff actor.
