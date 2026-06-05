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
  Bell,
  ReceiptText,
  RadioTower,
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
  ENABLE_SHOPEE_REALTIME_OPS,
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
  | 'shopee_realtime'

export interface NavItem {
  to: string
  label: string
  icon: LucideIcon
  end?: boolean
  hasBadge?: NavBadgeKey
  hint?: string
  minPhase?: number
  enabled?: boolean
  adminOnly?: boolean
}

export interface NavGroup {
  label: string
  items: NavItem[]
}

// Ordered by operator workflow. Sidebar and command palette consume this same
// source so new pages do not silently disappear from quick navigation.
export const NAV_GROUPS: NavGroup[] = [
  {
    label: 'ภาพรวม',
    items: [
      { to: '/dashboard', label: 'ภาพรวมงานวันนี้', icon: LayoutDashboard, hint: 'สถานะงานขายและสิ่งที่ต้องทำ' },
      { to: '/setup', label: 'สถานะพร้อมใช้งาน', icon: ClipboardCheck, hint: 'ตรวจความพร้อมร้าน' },
    ],
  },
  {
    label: 'ช่องทางรับข้อมูล',
    items: [
      { to: '/shopee-operations', label: 'คำสั่งซื้อ Shopee', icon: RadioTower, hasBadge: 'shopee_realtime', hint: 'คิวงานประจำวันจาก Shopee Push/Sync', enabled: ENABLE_SHOPEE_REALTIME_OPS },
      { to: '/import/shopee', label: 'นำเข้า Shopee ย้อนหลัง', icon: Upload, hint: 'ดึงย้อนหลัง, ซ่อมรายการตกหล่น, หรือใช้ Excel fallback', enabled: ENABLE_SHOPEE_EXCEL },
      { to: '/import/lazada', label: 'นำเข้า Lazada', icon: Upload, hint: 'นำเข้าจาก Lazada Excel', enabled: ENABLE_LAZADA_EXCEL && ENABLE_SALES_ORDERS },
      { to: '/import/tiktok', label: 'นำเข้า TikTok', icon: Upload, hint: 'นำเข้าจาก TikTok Excel/CSV', enabled: ENABLE_TIKTOK_EXCEL && ENABLE_SALES_ORDERS },
      { to: '/settings/email', label: 'กล่องอีเมลรับบิล', icon: Mail, hint: 'ตั้งค่ากล่องเมลสำหรับใบสั่งซื้อ' },
    ],
  },
  {
    label: 'งานฝั่งขาย',
    items: [
      { to: '/sale-invoices', label: 'ขายสินค้าและบริการ', icon: ShoppingBag, hasBadge: 'saleinvoice', hint: 'คิวบิลขายหลัก ส่งเข้า SML', enabled: ENABLE_SALES_ORDERS },
      { to: '/sales-orders', label: 'ใบสั่งขาย (SO)', icon: ShoppingBag, hasBadge: 'saleorder', hint: 'คิวใบสั่งขายที่ยังเปิดใช้งาน', enabled: ENABLE_SALES_ORDERS },
      { to: '/shopee-settlements', label: 'รับชำระ Shopee', icon: ReceiptText, hint: 'รอบถอนเงินและรับชำระ', enabled: ENABLE_SHOPEE_EXCEL && ENABLE_SALES_ORDERS },
      { to: '/bulk-send-jobs', label: 'งานส่งเข้า SML', icon: Send, hint: 'ติดตามงานส่งจำนวนมาก' },
    ],
  },
  {
    label: 'งานฝั่งซื้อ',
    items: [
      { to: '/bills', label: 'ใบสั่งซื้อ', icon: FileText, hasBadge: 'purchase', hint: 'คิวบิลซื้อจากอีเมล' },
    ],
  },
  {
    label: 'ข้อมูลหลัก',
    items: [
      { to: '/marketplace-aliases', label: 'สินค้ารอยืนยัน', icon: Tags, hasBadge: 'marketplace_aliases', hint: 'ยืนยันครั้งเดียว ระบบจำให้บิลถัดไป', enabled: ENABLE_SALES_ORDERS },
      { to: '/mappings', label: 'ตารางจับคู่สินค้า', icon: Workflow, hint: 'Item Mapping (raw_name → SML code)' },
      { to: '/settings/catalog', label: 'สินค้าใน SML', icon: Database, hint: 'SML Catalog' },
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
    label: 'ตั้งค่าระบบ',
    items: [
      { to: '/settings/channels', label: 'เส้นทางเอกสาร SML', icon: Building2, hint: 'Document Routing' },
      { to: '/settings/instance', label: 'การเชื่อมต่อระบบ', icon: Settings2, hint: 'SML / OpenRouter / ร้านนี้' },
      { to: '/settings/line-notifications', label: 'LINE แจ้งเตือน', icon: Bell, hint: 'แจ้งออเดอร์ใหม่จากคำสั่งซื้อ Shopee', adminOnly: true },
      { to: '/settings/users', label: 'ผู้ใช้ระบบ', icon: UsersRound, hint: 'Roles and access', adminOnly: true },
      { to: '/logs', label: 'ประวัติการทำงาน', icon: ScrollText, hint: 'ใครทำอะไรและผลลัพธ์' },
      { to: '/settings/ai-usage', label: 'การใช้งาน AI', icon: Bot, hint: 'ค่าใช้จ่าย / รุ่น AI' },
      { to: '/settings/old-data', label: 'จัดการข้อมูลเก่า', icon: Archive, hint: 'เก็บบิล / ลบถาวร' },
    ],
  },
]

export function isNavItemVisible(item: NavItem, role?: string | null): boolean {
  return item.enabled !== false && (!item.minPhase || PHASE >= item.minPhase) && (!item.adminOnly || role === 'admin')
}

export function visibleNavGroups(role?: string | null): NavGroup[] {
  return NAV_GROUPS
    .map((group) => ({
      ...group,
      items: group.items.filter((item) => isNavItemVisible(item, role)),
    }))
    .filter((group) => group.items.length > 0)
}

export function visibleNavItems(role?: string | null): NavItem[] {
  return visibleNavGroups(role).flatMap((group) => group.items)
}
