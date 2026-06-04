# Async SML Bulk Send Jobs

> Updated: 2026-05-21 09:25 +07
> Status: deployed on Nexflow, smoke-tested against real SML purchaseorder and verified through the history page.

## Summary

`ส่ง SML ทั้งหมด` no longer keeps the browser waiting on one long request. The UI creates a DB-backed job, polls progress, and can resume the latest active/recent job after the dialog is closed or the page is refreshed.

This applies to:

- `/bills` → `purchaseorder`
- `/sales-orders` → `saleorder`
- `/sale-invoices` → `saleinvoice`

Single-bill send from Bill Detail still uses `POST /api/bills/:id/retry` and shares the same core SML send function.

## API

| Method | Path | Purpose |
|---|---|---|
| `POST` | `/api/bills/bulk-send-jobs` | Create an async send job from ordered `bill_ids` and dialog config |
| `GET` | `/api/bills/bulk-send-jobs` | List historical jobs with filters and pagination for `/bulk-send-jobs` |
| `GET` | `/api/bills/bulk-send-jobs/active` | Return the current active job for source/bill type/document route/user |
| `GET` | `/api/bills/bulk-send-jobs/:job_id` | Poll progress and item results |
| `POST` | `/api/bills/bulk-send-jobs/:job_id/retry-failed` | Create a new job from failed items only |

Important request behavior:

- `client_request_id` makes create-job idempotent.
- Bills already in an active bulk job are rejected to prevent double submit.
- Bulk is capped at 100 bills per job.
- The original dialog payload is saved as `payload_snapshot`; retry failed reuses it.
- Historical job listing supports `status`, `source`, `bill_type`, `document_route`, `page`, and `per_page` (`max=100`).

## Database

Migration: `backend/internal/database/migrations/044_sml_bulk_jobs.sql`

Tables:

- `sml_bulk_jobs`
  - job header, status, counts, payload/filter snapshots, creator, timestamps
  - statuses: `queued`, `running`, `completed`, `completed_with_errors`, `failed`
- `sml_bulk_job_items`
  - ordered bill IDs, per-row status, attempts, attempted/final `doc_no`, error, timestamps
  - statuses: `queued`, `running`, `sent`, `failed`, `skipped`

Startup safety:

- On backend startup, stale queued/running jobs/items are marked failed with `server interrupted`.
- Users can retry failed safely after a restart.

## Worker Behavior

- Sends serially with concurrency `1`.
- Re-fetches each bill before sending.
- Skips bills that are already sent or no longer sendable.
- Uses the same SML send core as single-bill retry.
- Suppresses per-bill LINE admin spam during bulk job.
- Writes audit details with:
  - `via=bulk_job`
  - `bulk_job_id`
  - `bulk_job_item_id`
  - `bulk_item_sequence`

## Frontend Behavior

- Bulk dialog still loads and validates candidate bills before job creation.
- While running, config fields are disabled.
- Progress is polled every second.
- User can close the dialog without cancelling the job.
- Reopening the dialog restores active/recent progress when available.
- Result summary shows sent/failed/skipped/remaining counts.
- Failed rows can be copied as an error summary.
- `Retry failed` starts a new job for failed bills only.
- `/bulk-send-jobs` is a read-only history page for admin/staff:
  - reachable from the sidebar as `ประวัติส่ง SML` and from command palette
  - filters by route and job status
  - shows total jobs, page-level sent/failed/skipped counts, progress bars, actor email, and created time
  - opens a detail dialog with per-bill result rows and links back to the source bill
  - intentionally does not expose retry actions; retry failed remains in the active bulk dialog to avoid accidental SML sends from an audit/history view.

## Latest Smoke Test

Date: 2026-05-21

- Job: `128ceffe-5055-4863-8944-c6ce52301d26`
- Bill: `20275aed-fe5f-402f-9160-a93a3f5b2ccb`
- Source: `shopee_shipped`
- Route: `purchaseorder`
- Result: `completed`
- Counts: sent `1`, failed `0`, skipped `0`
- SML document: `BF-PO26050001`
- Post-test verification:
  - Bill status became `sent`.
  - Audit log recorded `sml_sent` with `via=bulk_job`.
  - Active-job endpoint returned 404 after completion, as expected.
  - History endpoint `GET /api/bills/bulk-send-jobs?page=1&per_page=20` returned the completed job.
  - Invalid status filter returned HTTP 400 with `invalid status`.
  - Browser QA on `/bulk-send-jobs` showed the job list and detail dialog with SML doc `BF-PO26050001`.
  - `scripts/preflight-main.sh` passed after the live send.

## QA Checklist

Before sending a large batch:

- Run `go test ./...`.
- Run `npm --prefix frontend run build`.
- Run `scripts/preflight-main.sh` after deploy.
- Test 1 bill first if SML config changed.
- Test 5-10 bills before using the 100-bill cap.
- Confirm `/logs` shows the actor and `via=bulk_job`.
- Confirm failed rows can be retried without resending successful rows.

## Operational Notes

- v1 does not include cancel. Cancelling mid-SML-send can leave external SML state ambiguous.
- If the server restarts mid-job, users should retry failed rows from the completed/failed summary.
- SML doc numbers are still reserved by backend send logic at actual send time; frontend expected numbers are preview only.
- Do not manually edit `sml_bulk_job_items` unless doing controlled production recovery.
