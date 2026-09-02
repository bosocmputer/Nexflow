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
