# Production Plan: Bill Diagnostics and LINE OA Usage

## Outcome

Make a bill's SML history understandable and supportable without risking a
duplicate document, and show each configured LINE OA's real monthly quota and
consumption from LINE.

## Production invariants

1. Diagnostic persistence is best-effort and never changes SML core/profile/
   stock outcomes or causes a core resend.
2. One immutable SML attempt may have multiple HTTP exchanges. Each exchange is
   recorded before the outbound request and finalized independently.
3. A timeout is `unknown`, not proof that SML did not create the document.
4. Resolved historical failures are not retryable. Only the unresolved current
   core attempt may expose a core retry action.
5. Staff receive a bounded, sanitized summary. Admins may inspect business
   payload/response, but credentials and authentication headers are never
   returned to any role.
6. SML responses are read up to 2 MiB; stored diagnostic JSON is recursively
   sanitized and capped at 64 KiB. Invalid/non-JSON bodies store metadata and
   hash only.
7. LINE quota values come from LINE Messaging API, per OA. They are never
   derived from Nexflow delivery counts or hardcoded plan limits.
8. LINE dependency failure degrades only the quota cell. It must not block the
   notification settings page or message delivery.
9. Tenant data and runtime feature configuration remain isolated. All four
   instances receive the same code and additive migration only.

## Contracts

- Add the next free migration (currently 094) for
  `bill_sml_attempt_exchanges` with one row per outbound exchange.
- `GET /api/bills/:id` adds a server-generated `sml_summary`; raw bill/SML
  fields are admin-only.
- `GET /api/bills/:id/timeline` remains compatible and adds optional diagnostic
  resolution fields while applying role-aware sanitization.
- `GET /api/bills/:id/sml-diagnostics` is admin-only and returns the immutable
  attempt plus bounded exchange history.
- `GET /api/settings/line-notifications/quota` is admin-only and returns one
  independent result per configured OA.
- Copied/downloaded support data uses `diagnostic_package_version: 1` and is
  always sanitized.

## Delivery sequence

Implement D00-D11 from `tasks/bill-diagnostics-line-quota-todo.md` in small,
tested commits. Complete Checkpoint A before UI work and Checkpoint B before
release verification. Do not deploy a partial slice.

## Rollback

Deploy the previous application commit and leave the additive exchange table in
place. Never delete an SML document, audit log, exchange row, LINE setting, or
LINE message as part of rollback.
