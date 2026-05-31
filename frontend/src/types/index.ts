// ─── User ───────────────────────────────────────────────────────────────────
export interface User {
  id: string
  email: string
  name: string
  role: 'admin' | 'staff' | 'viewer'
  created_at: string
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
  score: number
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
  embedding_status: 'pending' | 'done' | 'error'
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
  candidates?: CatalogMatch[] // top-5 catalog matches for needs_review items
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
  status: BillStatus
  document_route?: string
  raw_data?: Record<string, unknown> | null
  sml_doc_no?: string | null
  sml_order_id?: string | null
  sml_payload?: Record<string, unknown> | null
  sml_response?: Record<string, unknown> | null
  ai_confidence?: number
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
  // Only present in single-bill GET (not in list response).
  preview?: BillRoutePreview
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
  source: 'manual' | 'ai_learned'
  usage_count: number
  last_used_at?: string | null
  created_at: string
}

export interface MappingStats {
  total: number
  auto_confirmed: number
  needs_review: number
}

export interface MarketplaceAliasReviewGroup {
  group_key: string
  source: string
  bill_type: string
  source_sku: string
  raw_name: string
  normalized_key: string
  bill_count: number
  item_count: number
  suggested_match?: CatalogMatch | null
  candidates?: CatalogMatch[]
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
}

export interface DailyInsight {
  id: string
  insight: string
  date: string
  created_at: string
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
