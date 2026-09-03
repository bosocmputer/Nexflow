# SML Document Profile V1 Operations Runbook

## Scope and safety boundary

Document Profile V1 applies only when `SML_DOCUMENT_PROFILE_MODE=active` and
the immutable SaleInvoice payload opts in with `sml-document-v1`. `off` keeps
the legacy wire contract. `shadow` resolves and validates the candidate profile
but omits the opt-in field, so no supplemental SML rows are written.

For the sales-route completion rollout, use
`SML_DOCUMENT_PROFILE_ROUTE_MODES`. If it is absent, only `saleinvoice`
inherits the global mode and `saleorder`, `saleordercancel`,
`saleinvoicecancel`, and `creditnote` remain `off`. Each configured entry must
be one unique allowlisted `route:off|shadow|active` pair; invalid input stops the
backend at startup. Preview and Enable must first load
`GET /api/v1/capabilities` and require contract revision
`sml-sales-document-profile-v2-20260903`.

Never delete an SML document as recovery. Once Core is `created` or
`already_exists`, operators may retry only the profile reconciliation job.

For VAT sale invoices, verify the register row before daily processing:
`gl_journal_vat_sale.vat_effective_period` must equal the month of `vat_date`,
`vat_effective_year` must equal the Gregorian year plus 543, and the verified
TRANS_FLAG 44 register `vat_type` is 0 even when `ic_trans.vat_type` is 1. Do
not set the separate `gl_journal_vat_sale.period_number`; SML daily processing
owns the accounting periods in `gl_journal` and `gl_journal_detail`.

## Status model

| Layer | Success | Recoverable | Terminal |
| --- | --- | --- | --- |
| Core document | `created`, `already_exists` | unknown transport result retries the same immutable bytes | `existing_document_conflict` |
| Document Profile | `complete` | `pending`, `needs_reconciliation` | `terminal_failure` |
| Stock recalculation | `completed` | `queued`, `running` | `manual_reconciliation` |

Stock recalculation may start after Core and stock-relevant SML rows commit. A
failed `{tenant}_logs.erp_logs` write keeps the Profile incomplete but must not
resend Core or block a valid stock job.

## Operator checks

1. Open the bill and confirm the SML document number and Core status.
2. Read the Profile error, correlation ID, retry counter, and completed checks.
3. Check the Gateway and the tenant logs database using the correlation ID;
   never paste a complete payload or buyer data into application logs.
4. After the dependency is healthy, an Admin may choose **Retry เฉพาะ Profile**.
5. Verify `profile_complete` and the stock result independently.

`POST /api/bills/:id/sml-document-profile/retry` is Admin-only and requeues the
saved immutable payload. It cannot call the normal Core-send path.

## Metrics and alerts

The Admin-only endpoint `GET /api/metrics/sml-document-profile` exposes bounded
request series plus queue depth, oldest age, p95 age, retries, terminal count,
and payload-mismatch count. Metric labels are limited to tenant, route, profile,
and status; Order SN, document number, buyer data, and correlation IDs are not
labels.

The worker emits `sml_profile_alert` at most once per one-minute check while a
condition remains true:

- `payload_mismatch`: stop automatic sends and compare the immutable payload
  with the existing SML document.
- `terminal_failure`: Auto SML for the exact Shopee shop is paused; repair the
  dependency before an Admin retry.
- `profile_consecutive_failures`: the latest three distinct Profile jobs for
  the exact shop are all failing; one successful job breaks this streak.
- `queue_oldest_over_10m`: inspect Gateway/logs connectivity and worker health.
- `gateway_p95_over_2s`: compare the 50-line budget and inspect SML database
  latency. The hard client timeout is 20 seconds.

Structured audit events are `profile_requested`, `core_committed`,
`reconcile_queued`, `profile_complete`, `profile_terminal_failure`, and
`profile_retry_requested`.

## Production rollout and rollback

Back up the AOY Nexflow database/runtime and Central Gateway source/runtime/image
before deployment. Deploy the Gateway compatibility release first, then Nexflow
with mode `off`. Progress the first tenant through `shadow` and one controlled
bill before enabling automatic sends unless the USER explicitly accepts a live
cutover based on the committed parity fixtures. As of 2026-09-03, Document
Profile mode is `active` for Demo, AOY, Lanboon, and Ploy. AOY has Sale Invoice
and Credit Note route modes active; SO/SSC/SIC remain shadow. Shop `264993963`
is enabled and unpaused with trigger `PROCESSED`, config version 6 and cutoff
`2026-09-03 18:11:52 Asia/Bangkok`. The activation did not backfill or create an
SML document. Demo, Lanboon and Ploy retain only Sale Invoice active, and a newly
connected tenant must still pass its own route preview and shop-level enable
confirmation.

To roll back, pause Auto SML and set Profile mode to `off` before rolling code
back. Keep additive migrations, immutable attempts, reconciliation jobs, and
all SML documents.
