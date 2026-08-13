// ─── User ───────────────────────────────────────────────────────────────────
export interface User {
  id: string
  email: string
  name: string
  role: 'admin' | 'staff' | 'viewer'
  created_at: string
  menu_permissions?: UserMenuPermission[]
}

export interface UserMenuPermission {
  menu_key: string
  can_view: boolean
  can_create: boolean
  can_update: boolean
  can_delete: boolean
}

// Live SML tenant DB readiness from sml-api-bybos /health/ready.
export interface SMLReadiness {
  configured: boolean
  ready: boolean
  status: string
  tenant?: string
  message: string
  http_status?: number
  checked_at: string
  cached?: boolean
}

// ─── Catalog ─────────────────────────────────────────────────────────────────
export interface CatalogSetComponent {
  line_number: number
  row_order: number
  item_code: string
  item_name: string
  item_type: number
  unit_code: string
  qty: number
  price: number
  sum_amount: number
  price_ratio: number
  unit_factor: number
  active: boolean
  unit_valid: boolean
}

export interface CatalogMatch {
  item_code: string
  item_name: string
  item_name2?: string
  unit_code: string
  wh_code?: string
  shelf_code?: string
  price?: number
  image_count?: number
  primary_image_roworder?: number
  primary_image_guid?: string
  primary_image_bytes?: number
  image_url?: string
  has_hidden_chars?: boolean
  clean_item_code?: string
  hidden_char_kinds?: string[]
  item_type?: number
  set_component_count?: number
  set_definition_hash?: string
  set_document_valid?: boolean
  set_warning_codes?: string[]
  set_components?: CatalogSetComponent[]
  score: number
  method?: 'database'
  match_type?: 'exact_code' | 'code_prefix' | 'code_contains' | 'product_name'
}

export interface CatalogImage {
  roworder: number
  image_order?: number
  guid?: string
  bytes?: number
  image_url: string
}

export interface UnitOption {
  code: string
  name_1?: string
  name_2?: string
  stand_value?: number
  divide_value?: number
  is_default?: boolean
}

export interface CatalogItem {
  item_code: string
  item_name: string
  item_name2?: string
  unit_code: string
  price?: number | null
  sale_price?: number | null
  embedding_status: 'disabled' | 'pending' | 'done' | 'error'
  embedded_at?: string | null
  image_count?: number
  primary_image_roworder?: number
  primary_image_guid?: string
  primary_image_bytes?: number
  image_synced_at?: string | null
  image_url?: string
  has_hidden_chars?: boolean
  clean_item_code?: string
  hidden_char_kinds?: string[]
  item_type?: number
  set_component_count?: number
  set_definition_hash?: string
  set_document_valid?: boolean
  set_stock_valid?: boolean
  set_warning_codes?: string[]
  set_components?: CatalogSetComponent[]
  synced_at?: string | null
  last_seen_at?: string | null
  is_active?: boolean
  missing_at?: string | null
}

// ─── Bill ────────────────────────────────────────────────────────────────────
// Only the 5 statuses the backend actually sets. Migration 002 + 004 keep
// these values in sync with the bills_status_check CHECK constraint.
export type BillStatus =
  | 'pending'
  | 'needs_review'
  | 'sent'
  | 'failed'
  | 'skipped'

export interface BillItem {
  id: string
  bill_id: string
  raw_name: string
  source_sku?: string
  source_item_id?: string
  source_variant_id?: string
  marketplace_alias_id?: string | null
  source_image_url?: string
  item_code?: string | null
  has_hidden_chars?: boolean
  clean_item_code?: string
  qty: number
  unit_code?: string | null
  price?: number | null
  discount_amount?: number
  mapped: boolean
  mapping_id?: string | null
}

// Preview of which SML route + endpoint + doc_no pattern this bill would
// hit on retry. Resolved server-side so the BillDetail UI can show a chip
// "→ saleorder · SML 248 · doc_no NX-SO-#####" before admin clicks Send,
// catching channel-misconfig errors at the cheapest point.
export interface BillRoutePreview {
  channel: string
  bill_type: string
  route?: string             // sale_reserve / saleorder / saleinvoice / purchaseorder
  endpoint?: string          // tested SML destination path from /settings/channels
  doc_no?: string            // existing doc_no or SML-latest next preview (not reserved)
  doc_format?: string        // e.g. "NX-SO" + "YYMM####"
  doc_format_code?: string   // e.g. "SR", "INV", "PO"
  party_code?: string        // legacy channel value; purchase flow now selects seller in the send dialog
  party_name?: string
  sml_defaults?: {
    party_code?: string
    party_name?: string
    branch_code?: string
    sale_code?: string
    wh_code?: string
    shelf_code?: string
    unit_code?: string
    vat_type?: number
    vat_rate?: number
    inquiry_type?: number
    remark_2?: string
    doc_time?: string
    doc_format?: string
    database?: string
    base_url?: string
  }
  // Set when there's no channel_default row yet → preview can't compute
  // route. Frontend shows a hint linking to /settings/channels.
  error?: string
}

export interface BillEmailGroup {
  message_id: string
  group_key: string
  subject?: string
  from?: string
  order_count: number
  has_printable_email?: boolean
  print_count?: number
  last_printed_at?: string | null
  last_printed_by_email?: string
  last_printed_by_name?: string
  related_bills?: BillEmailRelatedBill[]
  print_events?: EmailPrintEvent[]
}

export interface BillEmailRelatedBill {
  id: string
  order_id?: string
  party_name?: string
  source: string
  bill_type: string
  document_route?: string
  status: BillStatus
  sml_doc_no?: string
  total_amount?: number
  created_at: string
  is_current?: boolean
}

export interface EmailPrintEvent {
  id: string
  bill_id: string
  artifact_id?: string
  email_message_id: string
  email_group_key: string
  subject?: string
  from?: string
  requested_by?: string
  requested_by_email?: string
  requested_by_name?: string
  created_at: string
}

export interface Bill {
  id: string
  bill_type: string
  source: string
  source_account_key?: string
  status: BillStatus
  document_route?: string
  raw_data?: Record<string, unknown> | null
  sml_doc_no?: string | null
  sml_order_id?: string | null
  sml_payload?: Record<string, unknown> | null
  sml_response?: Record<string, unknown> | null
  anomalies?: Anomaly[]
  error_msg?: string | null
  items?: BillItem[]
  created_at: string
  sent_at?: string | null
  archived_at?: string | null
  archived_by?: string | null
  archive_reason?: string
  // computed in list view
  total_amount?: number | null
  preview?: BillRoutePreview
  // Present when this bill is linked to a Shopee Realtime order snapshot.
  // Used to hide destructive delete and guide users to route-change flow.
  shopee_realtime_linked?: boolean
  remark?: string
  shopee_status?: ShopeeOrderEvent | null
  shopee_events?: ShopeeOrderEvent[]
  email_group?: BillEmailGroup | null
}

export interface ShopeeOrderEvent {
  id: string
  bill_id?: string | null
  order_id: string
  event_type: string
  status_label: string
  subject: string
  from_addr: string
  message_id: string
  email_date?: string | null
  raw_data?: Record<string, unknown> | null
  created_at: string
}

export interface BillListResponse {
  data: Bill[]
  total?: number
  page: number
  per_page: number
  page_size?: number
  limit?: number
  has_more?: boolean
  next_cursor?: string
}

// ─── Mapping ─────────────────────────────────────────────────────────────────
export interface Mapping {
  id: string
  raw_name: string
  item_code: string
  unit_code: string
  confidence: number
  source: 'manual' | 'verified'
  usage_count: number
  last_used_at?: string | null
  created_at: string
  updated_at: string
  item_name?: string
  confirmed_name?: string
  product_active: boolean
  open_item_count: number
}

export interface MappingStats {
  total: number
  auto_confirmed: number
  needs_review: number
}

export interface MarketplaceAliasReviewGroup {
  group_key: string
  source: string
  account_key: string
  account_name?: string
  external_item_id: string
  external_variant_id: string
  bill_type: string
  source_sku: string
  raw_name: string
  normalized_key: string
  bill_count: number
  item_count: number
}

export interface MarketplaceItemAlias {
  id: string
  source: string
  account_key: string
  account_name?: string
  external_item_id: string
  external_variant_id: string
  source_sku: string
  raw_name: string
  normalized_key: string
  item_code: string
  unit_code: string
  confirmed_by?: string | null
  confirmed_name?: string
  item_name?: string
  usage_count: number
  last_used_at?: string | null
  created_at: string
  updated_at: string
  is_active: boolean
  match_method: 'exact_sku' | 'manual_identity' | 'manual_sku' | 'manual_name' | 'legacy'
  scope_confirmed: boolean
  product_active: boolean
  open_item_count: number
  stock_mapping_count: number
}

export interface MarketplaceAliasImpact {
  open_items: number
  open_bills: number
  stock_mappings: number
  stock_conflicts: number
  dry_run_required: boolean
}

// ─── Dashboard ───────────────────────────────────────────────────────────────
export interface DashboardStats {
  total_bills: number
  pending: number
  needs_review: number
  confirmed: number
  sml_success: number
  sml_failed: number
  total_amount: number
  today_bills: number
  pilot_30d_total?: number
  pilot_30d_needs_review?: number
  pilot_30d_pending?: number
  pilot_30d_sent?: number
  pilot_30d_failed?: number
  pilot_30d_remaining?: number
  pilot_30d_success_rate?: number
  pilot_30d_estimated_minutes_saved?: number
  pilot_30d_estimated_hours_saved?: number
  purchase_total?: number
  purchase_pending?: number
  purchase_needs_review?: number
  purchase_sent?: number
  purchase_failed?: number
  sales_total?: number
  sales_pending?: number
  sales_needs_review?: number
  sales_sent?: number
  sales_failed?: number
  unread_messages?: number
  email_inbox_errors?: number
  sales_today_total?: number
  sales_mtd_total?: number
  sales_previous_total?: number
  sales_change_pct?: number | null
  sales_mtd_order_count?: number
  platform_sales?: PlatformSalesStat[]
  platform_sales_trend?: PlatformSalesTrendPoint[]
  sales_comparison_trend?: SalesComparisonTrendPoint[]
  platform_sales_meta?: PlatformSalesMeta
  nextstep_marketplace?: NextStepMarketplaceState
}

export type PlatformKey = 'shopee' | 'lazada' | 'tiktok'

export interface PlatformSalesStat {
  platform: PlatformKey
  label: string
  total_amount: number
  today_amount: number
  previous_total_amount?: number
  change_pct?: number | null
  order_count: number
  sent_count: number
  pending_count: number
  needs_review_count: number
  failed_count: number
  share_pct: number
}

export interface PlatformSalesTrendPoint {
  date: string
  shopee_amount: number
  lazada_amount: number
  tiktok_amount: number
  nextstep_amount?: number
}

export interface SalesComparisonTrendPoint {
  date: string
  previous_date: string
  current_total: number
  previous_total: number
  previous_shopee_amount?: number
  previous_lazada_amount?: number
  previous_tiktok_amount?: number
}

export interface PlatformSalesMeta {
  timezone: string
  from_date: string
  to_date: string
  previous_from_date?: string
  previous_to_date?: string
  definition: string
}

export interface NextStepMarketplaceState {
  configured: boolean
  available: boolean
  error?: string
  message?: string
  summary?: NextStepMarketplaceSummary
  previous_summary?: NextStepMarketplaceSummary
  orders?: NextStepMarketplaceOrder[]
  trend?: NextStepMarketplaceTrendPoint[]
  previous_trend?: NextStepMarketplaceTrendPoint[]
  meta?: NextStepMarketplaceMeta
  previous_meta?: NextStepMarketplaceMeta
  previous_available?: boolean
  previous_error?: string
  previous_message?: string
  change_pct?: number | null
}

export interface NextStepMarketplaceSummary {
  total_orders: number
  total_amount: number
  cn_total_amount: number
  total_except_vat?: number
  total_after_vat?: number
  total_vat_value?: number
  status_counts: Record<string, number>
  pending_count?: number
  packing_count?: number
  payment_count?: number
  success_count?: number
  cancel_count?: number
}

export interface NextStepMarketplaceOrder {
  remark_5?: string
  inv_doc_no?: string
  inv_doc_date?: string
  wallet_amount?: number
  remark_qt?: string
  remark_cancel?: string
  remark_inv?: string
  doc_no: string
  doc_date: string
  doc_time?: string
  cust_code?: string
  send_type?: number
  emp_code?: string
  emp_name?: string
  total_amount: number
  cn_total_amount?: number
  total_except_vat?: number
  total_after_vat?: number
  total_vat_value?: number
  balance?: number
  status: 'pending' | 'packing' | 'payment' | 'success' | 'cancel' | string
}

export interface NextStepMarketplaceTrendPoint {
  date: string
  total_amount: number
}

export interface NextStepMarketplaceMeta {
  tenant: string
  cust_code?: string
  doc_prefix?: string
  doc_prefixes?: string[]
  date_from: string
  date_to: string
  date_basis: string
  source: string
  search?: string
  status?: string
  page: number
  size: number
  total: number
}

// ─── Anomaly ─────────────────────────────────────────────────────────────────
export interface Anomaly {
  type: 'qty_zero' | 'price_zero' | 'price_too_high' | 'price_too_low' | 'qty_suspicious' | 'new_item'
  message: string
  severity: 'error' | 'warning'
}

// ─── API Generic ─────────────────────────────────────────────────────────────
export interface APIError {
  error: string
}

// ─── Import (Phase 4) ────────────────────────────────────────────────────────
export interface BillPreview {
  bill_id: string
  order_id: string
  customer_name: string
  item_count: number
  mapped_count: number
  total_amount: number
  anomalies: Array<{ code: string; severity: 'block' | 'warn'; message: string }>
  has_block: boolean
}

export interface ImportUploadResponse {
  platform: string
  bill_type: string
  total: number
  bills: BillPreview[]
}

export interface ImportConfirmResponse {
  success: number
  failed: number
  errors: Array<{ bill_id: string; reason: string }>
}

export interface PlatformColumnMapping {
  id?: string
  platform: string
  field_name: string
  column_name: string
  updated_at?: string
}
