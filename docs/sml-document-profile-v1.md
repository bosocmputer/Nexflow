# SML Document Profile V1

Status: implementation contract, default runtime mode `off`

## Verified AOY evidence

Read-only inspection on 2026-09-02 compared the manually created SML invoice
`INV26050020` with Nexflow-created `BF-INV26080055`.

The manual invoice has these relations:

| Relation | Manual rows | Stable identity | Profile V1 treatment |
| --- | ---: | --- | --- |
| `ic_trans` | 1 | `(doc_no, trans_flag)` primary key | required, transactional |
| `ic_trans_detail` | 1 current row | semantic `(doc_no, trans_flag, line_number)` | required, transactional |
| `gl_journal_vat_sale` | 1 | semantic `(doc_no, trans_flag, line_number)` | required when VAT applies |
| `ic_trans_shipment` | 1 | `(doc_no, trans_flag)` primary key | required for Marketplace physical-goods profile |
| `logs` | 1 | generated GUID primary key; document identity in row | required, transactional |
| `${tenant}_logs.erp_logs` | 1 create row | semantic `(doc_no, trans_flag, function_code, menu)` | required, reconciled cross-database |
| `ic_trans_detail_lot` | 1 | SML stock-processing output | verified after stock recalculation, not written by profile writer |
| `gl_journal` / `gl_journal_detail` | 1 / 5 | SML accounting/cost posting | observed evidence, not safe to synthesize without SML account/cost authority |

`BF-INV26080055` already has `ic_trans`, two `ic_trans_detail` rows,
`ic_trans_detail_lot`, and a SML `logs` row after later processing. It has no VAT
register or shipment relation. Its BillFlow `erp_logs` row has no `data_new`; a
later SML UI edit produced a second complete ERP log. This proves that core
creation and profile completeness must be separate states and that retry must
reconcile missing relations without recreating the document.

## Manual profile fields

The manual header uses TRANS_FLAG 44, VAT included (`vat_type=1`, rate 7),
`tax_doc_no=doc_no`, `tax_doc_date=doc_date`, `send_date=doc_date`, and
`credit_date=doc_date`. Its detail uses `calc_flag=-1`, the selected warehouse and
location, exact VAT/base amounts, and stable line numbers.

The VAT-sale register has its own semantics and must not copy the header
`vat_type`. For TRANS_FLAG 44, the verified SML register row uses `vat_type=0`
while the VAT-included header remains `vat_type=1`. Profile V1 derives
`vat_effective_period` from the month of `vat_date` and
`vat_effective_year` from its Gregorian year plus 543. The current contract sets
`vat_date=tax_doc_date=doc_date`, so a document dated `2026-09-02` writes period
9 and year 2569. These derived values are written both to
`gl_journal_vat_sale` and `erp_logs.data_new.screenvatsale`; callers do not send
or configure them. The separate nullable `period_number` column in
`gl_journal_vat_sale` remains unset, matching SML-created sale invoices.

The manual `erp_logs.data_new` is UTF-8 JSON with these top-level sections:

- `screentop`
- `screendetail`
- `screenbottom`
- `screenmore`
- `screenshipment`
- `screenvatsale`
- `screengltop`
- `screengldetail`
- `screenpay`
- `screenpaydeposit`
- `screenwithholdingtax`

Profile V1 produces all sections with arrays/objects of the same kind, including
empty sections. It never copies buyer PII into Nexflow telemetry. Shipment PII,
when operationally required, stays inside the SML document/audit domain.

## Applicability rules

- VAT relation is required when `vat_rate > 0` and `vat_type` is 1 or 2. It is
  explicitly not applicable for no-VAT documents. For an applicable sale
  invoice, the route-controlled header VAT mode remains unchanged while the
  sale-register row uses `vat_type=0` and a non-zero derived effective month and
  Buddhist year.
- Shipment is required for Marketplace physical-goods sale invoices. If the
  selected source snapshot lacks the required shipment identity, active mode
  fails before creating the SML core document. Non-shipping/manual/service routes
  mark shipment `not_applicable` instead of inventing data.
- GL accounting rows are not inserted directly by Profile V1. Their account and
  cost values must come from a separately verified SML posting authority; hard-
  coding accounts from one sample would corrupt other tenants.

## Compatibility and safety

- Gateway profile behavior is opt-in with `document_profile_version`.
- Legacy requests retain current behavior.
- Canonical payload hashes exclude volatile timestamps/roworder but include every
  business field and ordered detail line.
- A duplicate identity with a different canonical hash is a conflict.
- Main-database relations commit together. Cross-database `erp_logs` is retried
  by a tenant-scoped Nexflow durable reconciliation job.

## Frozen wire contract

Profile V1 is enabled only by `document_profile_version: "sml-document-v1"`.
Legacy numeric fields stay in the request for old callers. The following decimal
strings are authoritative when Profile V1 is present:

- Header: `total_value_decimal`, `total_discount_decimal`,
  `total_before_vat_decimal`, `total_vat_value_decimal`,
  `total_except_vat_decimal`, `total_after_vat_decimal`, and
  `total_amount_decimal`.
- Detail: `qty_decimal`, `price_decimal`, `discount_amount_decimal`,
  `sum_amount_decimal`, `before_vat_decimal`, and `vat_amount_decimal`.
- System: `exchange_rate_decimal`, fixed to `1` for Profile V1.

Decimal values use base-10 JSON strings, never exponent notation, commas,
leading `+`, or negative zero. Gateway normalizes redundant leading/trailing
zeroes before comparison and rejects a numeric compatibility field that differs
from its decimal authority by more than `0.01` baht.

The canonical hash input is UTF-8 JSON produced from a versioned typed object:

1. Object fields are emitted in the contract order and optional absent values
   are represented consistently; database row IDs, roworder, retry counters,
   correlation IDs, and volatile timestamps are excluded.
2. Detail lines are ordered by `line_number`, then `item_code`, `unit_code`,
   warehouse and location. Duplicate line identities are invalid.
3. Decimal strings are normalized before serialization. Text remains exact
   UTF-8 after validation; the Gateway never trims or truncates business text.
4. SHA-256 of those bytes is returned as lowercase hexadecimal `payload_hash`.
5. Identity is authenticated tenant plus `trans_flag` plus `doc_no`. An existing
   identity with another hash returns HTTP 409 `doc_no_payload_mismatch`.

The request body limit is 2 MiB and detail limit is 500. `remark` and `remark_2`
are each limited to 255 Unicode code points. Invalid UTF-8, control characters,
unknown template tokens, oversize bodies/items/text, and incomplete required
shipment evidence are rejected before the core transaction begins.

Profile checks use stable names: `core`, `vat`, `shipment`, `main_log`, and
`erp_log`. VAT or shipment may be explicitly `not_applicable` under the rules
above. Response statuses are:

- `core_status`: `pending|created|already_exists|terminal_failure`
- `profile_status`: `pending|complete|needs_reconciliation|terminal_failure`
- `required_checks` and `completed_checks`: sorted, duplicate-free check names
- `reconciliation_required`: true only when the core is durable but one or more
  required Profile checks remain incomplete
