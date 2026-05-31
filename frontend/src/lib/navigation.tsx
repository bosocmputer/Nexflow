import {
  Archive,
  Bot,
  Building2,
  ClipboardCheck,
  Database,
  FileText,
  LayoutDashboard,
  Mail,
  MessageSquare,
  MessageSquareQuote,
  ReceiptText,
  ScrollText,
  Send,
  Settings2,
  ShoppingBag,
  Tag,
  Tags,
  Upload,
  UsersRound,
  Workflow,
  type LucideIcon,
} from 'lucide-react'

import {
  ENABLE_CHAT,
  ENABLE_LAZADA_EXCEL,
  ENABLE_SALES_ORDERS,
  ENABLE_SHOPEE_EXCEL,
  ENABLE_TIKTOK_EXCEL,
} from '@/lib/featureFlags'

const PHASE = Number(import.meta.env.VITE_PHASE ?? 99)

export type NavBadgeKey =
  | boolean
  | 'bills'
  | 'purchase'
  | 'saleorder'
  | 'saleinvoice'
  | 'messages'
  | 'marketplace_aliases'

export interface NavItem {
  to: string
  label: string
  icon: LucideIcon
  end?: boolean
  hasBadge?: NavBadgeKey
  hint?: string
  minPhase?: number
  enabled?: boolean
}

export interface NavGroup {
  label: string
  items: NavItem[]
}

// Ordered by daily-frequency. Sidebar and command palette consume this same
// source so new pages do not silently disappear from quick navigation.
export const NAV_GROUPS: NavGroup[] = [
  {
    label: 'ภาพรวม',
    items: [
      { to: '/setup', label: 'เริ่มต้นใช้งาน', icon: ClipboardCheck, hint: 'ตรวจความพร้อมร้าน' },
      { to: '/dashboard', label: 'ภาพรวม', icon: LayoutDashboard, hint: 'งานวันนี้' },
      { to: '/logs', label: 'ประวัติการทำงาน', icon: ScrollText, hint: 'Activity Log' },
      { to: '/bulk-send-jobs', label: 'ประวัติส่ง SML', icon: Send, hint: 'Bulk Send Jobs' },
    ],
  },
  {
    label: 'งานฝั่งซื้อ',
    items: [
      { to: '/bills', label: 'ใบสั่งซื้อ', icon: FileText, hasBadge: 'purchase', hint: 'Email → ซื้อ -> ใบสั่งซื้อ' },
    ],
  },
  {
    label: 'งานฝั่งขาย',
    items: [
      { to: '/sales-orders', label: 'ใบสั่งขาย', icon: ShoppingBag, hasBadge: 'saleorder', hint: 'Marketplace Excel → ขาย -> ใบสั่งขาย', enabled: ENABLE_SALES_ORDERS },
      { to: '/sale-invoices', label: 'ขายสินค้าและบริการ', icon: ShoppingBag, hasBadge: 'saleinvoice', hint: 'Marketplace Excel → ขาย -> ขายสินค้าและบริการ', enabled: ENABLE_SALES_ORDERS },
      { to: '/shopee-settlements', label: 'รับชำระ Shopee', icon: ReceiptText, hint: 'Shopee payout -> SML รับชำระ', enabled: ENABLE_SHOPEE_EXCEL && ENABLE_SALES_ORDERS },
    ],
  },
  {
    label: 'งานที่ต้องตรวจ',
    items: [
      { to: '/marketplace-aliases', label: 'สินค้ารอยืนยัน', icon: Tags, hasBadge: 'marketplace_aliases', hint: 'ยืนยันครั้งเดียว ระบบจำให้บิลถัดไป', enabled: ENABLE_SALES_ORDERS },
    ],
  },
  {
    label: 'ช่องทางรับข้อมูล',
    items: [
      { to: '/settings/email', label: 'กล่องอีเมลรับบิล', icon: Mail, hint: 'Email → ใบสั่งซื้อ' },
      { to: '/import/shopee', label: 'Shopee', icon: Upload, hint: 'API + Excel จาก Shopee', enabled: ENABLE_SHOPEE_EXCEL },
      { to: '/import/lazada', label: 'Lazada Excel', icon: Upload, hint: 'Excel จาก Lazada', enabled: ENABLE_LAZADA_EXCEL && ENABLE_SALES_ORDERS },
      { to: '/import/tiktok', label: 'TikTok Excel', icon: Upload, hint: 'Excel/CSV จาก TikTok', enabled: ENABLE_TIKTOK_EXCEL && ENABLE_SALES_ORDERS },
    ],
  },
  {
    label: 'แชทลูกค้า',
    items: [
      { to: '/messages', label: 'ข้อความลูกค้า', icon: MessageSquare, hasBadge: 'messages', hint: 'Inbox รวมทุก OA', minPhase: 2, enabled: ENABLE_CHAT },
      { to: '/settings/line-oa', label: 'บัญชี LINE OA', icon: MessageSquare, end: true, hint: 'LINE OA Accounts', minPhase: 2, enabled: ENABLE_CHAT },
      { to: '/settings/quick-replies', label: 'ข้อความสำเร็จรูป', icon: MessageSquareQuote, end: true, hint: 'Quick Replies', minPhase: 2, enabled: ENABLE_CHAT },
      { to: '/settings/chat-tags', label: 'ป้ายลูกค้า', icon: Tag, end: true, hint: 'Chat Tags', minPhase: 2, enabled: ENABLE_CHAT },
    ],
  },
  {
    label: 'ข้อมูลหลัก',
    items: [
      { to: '/mappings', label: 'ตารางจับคู่สินค้า', icon: Workflow, hint: 'Item Mapping (raw_name → SML code)' },
      { to: '/settings/catalog', label: 'สินค้าใน SML', icon: Database, hint: 'SML Catalog' },
    ],
  },
  {
    label: 'ตั้งค่าระบบ',
    items: [
      { to: '/settings/channels', label: 'เส้นทางเอกสาร SML', icon: Building2, hint: 'Document Routing' },
      { to: '/settings/old-data', label: 'จัดการข้อมูลเก่า', icon: Archive, hint: 'เก็บบิล / ลบถาวร' },
      { to: '/settings/ai-usage', label: 'การใช้งาน AI', icon: Bot, hint: 'ค่าใช้จ่าย / รุ่น AI' },
      { to: '/settings/users', label: 'ผู้ใช้ระบบ', icon: UsersRound, hint: 'Roles and access' },
      { to: '/settings/instance', label: 'การเชื่อมต่อระบบ', icon: Settings2, hint: 'SML / OpenRouter / ร้านนี้' },
    ],
  },
]

export function isNavItemVisible(item: NavItem): boolean {
  return item.enabled !== false && (!item.minPhase || PHASE >= item.minPhase)
}

export function visibleNavGroups(): NavGroup[] {
  return NAV_GROUPS
    .map((group) => ({
      ...group,
      items: group.items.filter(isNavItemVisible),
    }))
    .filter((group) => group.items.length > 0)
}

export function visibleNavItems(): NavItem[] {
  return visibleNavGroups().flatMap((group) => group.items)
}
