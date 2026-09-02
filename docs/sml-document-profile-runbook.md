# SML Document Profile V1 Operations Runbook

## Scope and safety boundary

Document Profile V1 applies only when `SML_DOCUMENT_PROFILE_MODE=active` and
the immutable SaleInvoice payload opts in with `sml-document-v1`. `off` keeps
the legacy wire contract. `shadow` resolves and validates the candidate profile
but omits the opt-in field, so no supplemental SML rows are written.

Never delete an SML document as recovery. Once Core is `created` or
`already_exists`, operators may retry only the profile reconciliation job.

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

## AOY rollout and rollback

Back up the AOY Nexflow database/runtime and Central Gateway source/runtime/image
before deployment. Deploy the Gateway compatibility release first, then Nexflow
with mode `off`. Progress AOY through `shadow` and one controlled manual bill at
`AB-1 / 001` before `active` Auto SML. Demo, Lanboon, and Ploy remain `off`.

To roll back, pause Auto SML and set Profile mode to `off` before rolling code
back. Keep additive migrations, immutable attempts, reconciliation jobs, and
all SML documents.
