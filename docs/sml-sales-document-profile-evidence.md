# SML Sales Document Profile Evidence

Status: frozen implementation evidence for T15, captured read-only on
2026-09-03 (Asia/Bangkok).

## Controlled source documents

The mappings below were verified from SML-created documents
`SO26090001 -> SSC26090001`, `INV26090001 -> SIC26090001`,
`INV26090002 -> CN26090001`, `INV26090003 -> CN26090002` (`vat_type=0`),
and `INV26090004 -> CN26090003` (`vat_type=2`). These identifiers are retained
only as bounded operational evidence. The committed executable fixture uses
synthetic document, customer and item identifiers and contains no buyer PII.

The USER ran SML Daily Processing after each sample. The observed GL rows are
verification output only. Profile V1 must never insert or update `gl_journal`
or `gl_journal_detail`.

## Route parity

| Route | Flag | Core relations | Source update | Main log | ERP JSON sections |
| --- | ---: | --- | --- | --- | --- |
| Sale Order | 36 | `ic_trans`, `ic_trans_detail`, `ic_trans_shipment` | none | screen 36, `menu_so_sale_order` | `screentop`, `screendetail`, `screenbottom`, `screenmore`, `screenshipment` |
| Sale Order Cancellation | 37 | `ic_trans`, `ic_trans_detail` | flag 36 `used_status=1` | screen 37, blank menu | `screentop`, `screendetail`, `screenbottom`, `screenmore` |
| Sale Invoice | 44 | `ic_trans`, `ic_trans_detail`, `gl_journal_vat_sale`, `ic_trans_shipment` | none | screen 44, `menu_so_invoice` | all 11 sales sections |
| Invoice Void | 45 | header-only `ic_trans` | flag 44 header/detail `last_status=1` | screen 45, `menu_so_invoice_cancel` | `screentop`, `screendetail`, `screenbottom`, `screenmore` |
| Credit Note | 48 | `ic_trans`, `ic_trans_detail`, `gl_journal_vat_sale`, `ap_ar_trans_detail` | flag 44 `used_status=1` | screen 48, `menu_so_credit_note` | sales sections except shipment and pay-deposit |

All routes also require one current-round `logs` create row and one idempotent
`${tenant}_logs.erp_logs` create row. SML UI can leave zero-valued placeholder
detail slots in ERP JSON; Profile V1 stores only rows with a real semantic
document relationship.

## Exact relation identities and columns

- Header identity: `ic_trans(doc_no, trans_flag)`. Business comparison includes
  document/ref dates and formats, party, branch/sale/warehouse/location,
  inquiry/VAT modes, all exact totals, remarks, system identity and state fields.
- Detail identity: semantic
  `ic_trans_detail(doc_no, trans_flag, line_number)` for active rows. Comparison
  includes item/unit, warehouse/location, quantity/price/discount/VAT totals,
  `calc_flag`, `branch_code`, set-product fields and source references.
- VAT identity: semantic
  `gl_journal_vat_sale(doc_no, trans_flag, line_number)`. Required fields are
  `vat_number`, `vat_date`, base, rate, amount, `vat_type=0`, effective month and
  Buddhist year. `period_number` remains unset.
- Shipment identity: `ic_trans_shipment(doc_no, trans_flag)`. It is required for
  Marketplace physical Sale Order/Sale Invoice documents and absent for all
  cancellation routes.
- Receivable reference identity: semantic
  `ap_ar_trans_detail(doc_no, trans_flag, line_number)`. It exists only for a
  Credit Note in this scope and references the source invoice number/date.
- Main log identity is the document/trans-flag create action in `logs`; ERP log
  identity is `(doc_no, trans_flag, function_code=1, route menu)` in
  `${tenant}_logs.erp_logs`.

## Route-specific rules

- Sale Order detail uses `calc_flag=1`; it has no VAT-register, AP/AR or GL
  relation.
- Sale Order Cancellation uses `cancel_type=2`, detail `calc_flag=-1`,
  `ref_doc_no` and `ref_line_number` from the source, and no VAT/shipment/AP-AR/
  GL relation. The controlled final log's menu is blank, matching SML.
- Sale Invoice and Credit Note use the following VAT matrix. A Credit Note
  copies each detail branch exactly from its source; blank stays blank.

| Header VAT type | Header before/VAT/after | VAT register |
| ---: | --- | --- |
| 0 (external) | base / tax / VAT-inclusive total | required, non-zero base/tax |
| 1 (included) | extracted base / tax / VAT-inclusive total | required, non-zero base/tax |
| 2 (zero-rate) | `0 / 0 / 0` while `total_value=total_amount` | required zero row |

- Credit Note detail uses `calc_flag=1`; `ref_amount` and `ref_diff` equal the
  source `total_value` (before added VAT for type 0). The AP/AR debt values equal
  the VAT-inclusive source `total_amount`, and `sum_before_vat` equals source
  `total_before_vat`.
- Invoice Void is header-only. It does not invent zero detail/VAT/shipment/AP-AR/
  GL rows.
- A source-scoped cancellation lock is shared across SSC/SIC/CN. An existing
  manual SML cancellation is reported as external evidence and never receives
  BILLFLOW/NEXFLOW ownership markers.

## Executable fixture

The PII-free contract fixture is
`internal/handlers/compat/testdata/sml_sales_document_profile_v2_golden.json` in
the Gateway repository. Gateway tests validate its route set, relation matrix,
VAT rules, source state transitions, menus and ERP sections before writer changes
are accepted.
