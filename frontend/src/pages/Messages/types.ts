// Shared types between the Messages page and its child components.
// Mirrors backend models in backend/internal/models/chat.go.

export type ChatStatus = 'open' | 'resolved' | 'archived'

export interface ChatConversation {
  line_user_id: string
  // Multi-OA: which LINE OA owns this conversation. line_oa_name is the
  // display label (joined from line_oa_accounts.name) for the inbox badge.
  line_oa_id?: string | null
  line_oa_name?: string
  display_name: string
  picture_url: string
  phone: string
  status: ChatStatus
  last_message_at: string
  last_inbound_at?: string | null
  last_admin_reply_at?: string | null
  unread_admin_count: number
  created_at: string
}

// Phase 4.8 internal admin notes
export interface ChatNote {
  id: string
  line_user_id: string
  body: string
  created_by?: string | null
  created_at: string
  updated_at: string
}

// Phase 4.9 tags
export interface ChatTag {
  id: string
  label: string
  color: string
  created_at: string
}

export type ChatDirection = 'incoming' | 'outgoing' | 'system'
export type ChatKind = 'text' | 'image' | 'file' | 'audio' | 'system'
export type ChatDelivery = 'sent' | 'failed' | 'pending'
// Reply = LINE's free Reply API (used cached replyToken from inbound).
// Push  = LINE Push API — counts toward 200/month free OA quota.
export type ChatDeliveryMethod = 'reply' | 'push'

export interface ChatMedia {
  id: string
  message_id: string
  filename: string
  content_type: string
  size_bytes: number
  sha256: string
  storage_path: string
  created_at: string
}

export interface ChatMessage {
  id: string
  line_user_id: string
  direction: ChatDirection
  kind: ChatKind
  text_content: string
  line_message_id?: string
  line_event_ts?: number
  sender_admin_id?: string
  delivery_status: ChatDelivery
  delivery_method?: ChatDeliveryMethod
  delivery_error?: string
  created_at: string
  media?: ChatMedia | null
}

// AI extracted preview from a media message — mirrors ai.ExtractedBill in Go.
export interface ExtractedItem {
  raw_name: string
  qty: number
  unit?: string
  price?: number | null
}

export interface ExtractedBill {
  doc_type?: 'sale' | 'purchase'
  customer_name?: string
  customer_phone?: string | null
  items: ExtractedItem[]
  total_amount?: number | null
  note?: string | null
  confidence?: number
}

// Catalog search result (reused from existing /api/catalog/search endpoint).
export interface CatalogMatch {
  item_code: string
  item_name: string
  item_name2?: string
  unit_code?: string
  price?: number | null
  score?: number
}
