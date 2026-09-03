# Production Plan: Nexflow AOY -> SML Document Profile V1

## Outcome

ส่งเอกสารจาก Nexflow เข้า SML ให้มี header/detail/VAT/shipment/audit relationships
เทียบเท่าบิลที่บันทึกจาก SML ERP โดยคง retry แบบ idempotent แยกสถานะ core document,
document profile และ stock recalculation และเปิดใช้ AOY แบบ `off -> shadow -> active`
ก่อน tenant อื่น

## Production Invariants

1. Idempotency identity คือ `tenant + trans_flag + doc_no`; payload เดิม retry ได้
   แต่ payload ต่างกันต้อง `409 doc_no_payload_mismatch`.
2. การ timeout หลัง SML commit ห้ามทำให้ Nexflow สร้างเอกสารซ้ำ.
3. Immutable SML attempt มาก่อน external write และ retry ใช้ payload เดิม byte-stable.
4. Channel configuration ใช้ optimistic `config_version`; route change pause Auto SML.
5. Profile V1 เป็น additive opt-in; legacy Gateway callers ต้องไม่เปลี่ยน behavior.
6. Core/profile/stock มีสถานะแยกกัน; profile repair ห้าม resend core document.
7. Tenant มาจาก authenticated runtime context เท่านั้นและห้าม copy config ข้ามร้าน.
8. Financial authority ใช้ normalized decimal strings; legacy numeric JSON คงไว้เพื่อ
   compatibility เท่านั้น.
9. User input ถูก validate ฝั่ง server, bounded และไม่ truncate เงียบ ๆ.
10. Logs/metrics ไม่เก็บ secret หรือ unredacted buyer PII.

## Contract

- Mode: `SML_DOCUMENT_PROFILE_MODE=off|shadow|active`, default `off`.
- Profile: `document_profile_version=sml-document-v1`.
- Channel defaults use literal free-text `remark` and `remark_2`, plus `config_version` and
  `expected_config_version` on update.
- Preview is read-only and returns literal remarks, system fields, missing
  prerequisites, route signature and mode. The normal UI shows only the
  document/customer/warehouse/VAT readiness summary; technical contract fields
  remain internal.
- Gateway response adds `payload_hash`, `core_status`, `profile_status`,
  `required_checks`, `completed_checks`, and `reconciliation_required`.
- Profile status: `pending|complete|needs_reconciliation|terminal_failure`.
- System fields: `BILLFLOW` creator/cashier, `NEXFLOW` user request, THB/rate 1,
  Asia/Bangkok date/time, and `NEXFLOW|<channel>|<order-or-bill>` trace marker.
- AOY Shopee sale/stock scope: `AB-1 / 001` after controlled configuration change.

## Delivery Strategy

Implement T00-T14 from `tasks/sml-document-profile-todo.md` in small tested
increments. Gateway capability deploys before opt-in client behavior. Nexflow
migrations/code deploy with mode off, then AOY shadow and controlled active UAT.
Demo, Lanboon and Ploy remain off.

## Rollback

Pause Auto SML and set profile mode off before rolling code back. Additive
migrations and immutable attempts/jobs stay in place. Never delete an SML document
automatically as rollback.

## Approved Extension: Sales Document Profile Completion (T15-T24)

The 2026-09-03 extension completes Profile V1 for the five verified SML sales
routes while preserving the existing Sale Invoice canonical hash and every
legacy request/response:

| Route | Canonical API | SML trans flag | Initial rollout mode |
| --- | --- | ---: | --- |
| `saleorder` | `POST /api/v1/ic/sale-orders` | 36 | `shadow` |
| `saleinvoice` | `POST /api/v1/ic/sale-invoices` | 44 | existing global mode |
| `saleordercancel` | `POST /api/v1/ic/sale-orders/:doc_no/void` | 37 | `shadow` |
| `saleinvoicecancel` | `POST /api/v1/ic/sale-invoices/:doc_no/void` | 45 | `shadow` |
| `creditnote` | `POST /api/v1/ic/sale-invoices/:doc_no/cancel` | 48 | `shadow` |

### Extension invariants

1. Gateway capability revision, supported routes and limits are checked before
   Preview and Enable. Unknown/duplicate route modes fail startup.
2. Shopee stores route enums only; the backend maps them to canonical paths and
   rejects absolute URLs.
3. The main and cancellation routes are previewed and saved atomically with two
   optimistic config versions and a signed, tenant-bound token valid for ten
   minutes. Saving pauses automation; enabling is a separate action.
4. All cancellation types take the same source-scoped PostgreSQL advisory
   transaction lock. A competing request returns `409 document_busy` within
   three seconds.
5. A manually created SML document is evidence of an external action, not a
   Nexflow document. Nexflow never adds BILLFLOW/NEXFLOW ownership markers to it.
6. Cancellation is a full-document reversal only. Input and expanded output are
   both capped at 500 items and 2 MiB before any core write.
7. Profile repair never resends the core document or stock job. Cross-database
   ERP-log repair uses a tenant-scoped durable job with fencing and bounded
   retries.
8. Profile code never inserts or updates `gl_journal` or `gl_journal_detail`.
   SML Daily Processing remains the accounting authority.

### Delivery checkpoints

- **Checkpoint A (T15-T17):** fixtures/contract, capability handshake, strict
  route modes and VAT/decimal correctness; no new production write behavior.
- **Checkpoint B (T18-T21):** Sale Order, SSC, SIC and CN Profile writers plus
  durable cancellation reconciliation; route modes remain safe by default.
- **Checkpoint C (T22-T23):** atomic route bundle, guarded operator UI,
  performance/security/telemetry budgets.
- **T24:** full verification, backups and staged rollout. Gateway deploys first;
  all four Nexflow instances use one commit. New routes stay `shadow` outside
  controlled AOY parity, and no historical document is backfilled.
