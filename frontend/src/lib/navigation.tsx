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
  ShieldCheck,
  ShoppingBag,
  Store,
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
  ENABLE_LINE_MYSHOP,
  ENABLE_SALES_ORDERS,
  ENABLE_SHOPEE_EXCEL,
  ENABLE_SHOPEE_REALTIME_OPS,
  ENABLE_TIKTOK_EXCEL,
} from '@/lib/featureFlags'
import type { User, UserMenuPermission } from '@/types'

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
  | 'nextstep_marketplace'

export interface NavItem {
  menuKey: string
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

const STAFF_DEFAULT_MENU_KEYS = new Set([
  'dashboard',
  'nextstep_marketplace',
  'shopee_operations',
  'sale_invoices',
  'sales_orders',
  'purchase_orders',
  'marketplace_aliases',
  'bulk_send_jobs',
  'import_shopee',
  'import_lazada',
  'import_tiktok',
  'shopee_settlements',
  'mappings',
  'catalog',
  'messages',
  'logs',
])

const VIEWER_DEFAULT_MENU_KEYS = new Set([
  'dashboard',
  'sale_invoices',
  'sales_orders',
  'purchase_orders',
  'mappings',
  'catalog',
])

// Ordered as an online platform operations console: overview, orders,
// import/payment repair, product data, customer channels, connections, audit.
// Sidebar and command palette consume this same source so new pages do not
// silently disappear from quick navigation.
export const NAV_GROUPS: NavGroup[] = [
  {
    label: 'ภาพรวมแพลตฟอร์ม',
    items: [
      { menuKey: 'dashboard', to: '/dashboard', label: 'ยอดขายตามแพลตฟอร์ม', icon: LayoutDashboard, hint: 'ยอดเอกสารขาย Shopee, Lazada, TikTok และ NextStep Marketplace' },
      { menuKey: 'nextstep_marketplace', to: '/nextstep-marketplace', label: 'NextStep Marketplace', icon: Store, hasBadge: 'nextstep_marketplace', hint: 'ออเดอร์ MQT จาก SML marketplace' },
    ],
  },
  {
    label: 'ออเดอร์และเอกสาร',
    items: [
      { menuKey: 'shopee_operations', to: '/shopee-operations', label: 'คำสั่งซื้อ Shopee', icon: RadioTower, hasBadge: 'shopee_realtime', hint: 'คิวงานประจำวันจาก Shopee Push/Sync', enabled: ENABLE_SHOPEE_REALTIME_OPS },
      { menuKey: 'sale_invoices', to: '/sale-invoices', label: 'ขายสินค้าและบริการ', icon: ShoppingBag, hasBadge: 'saleinvoice', hint: 'คิวบิลขายหลัก ส่งเข้า SML', enabled: ENABLE_SALES_ORDERS },
      { menuKey: 'sales_orders', to: '/sales-orders', label: 'ใบสั่งขาย (SO)', icon: ShoppingBag, hasBadge: 'saleorder', hint: 'คิวใบสั่งขายที่ยังเปิดใช้งาน', enabled: ENABLE_SALES_ORDERS },
      { menuKey: 'purchase_orders', to: '/bills', label: 'ใบสั่งซื้อ', icon: FileText, hasBadge: 'purchase', hint: 'คิวบิลซื้อจากอีเมล' },
      { menuKey: 'marketplace_aliases', to: '/marketplace-aliases', label: 'สินค้ารอยืนยัน', icon: Tags, hasBadge: 'marketplace_aliases', hint: 'ยืนยันครั้งเดียว ระบบจำให้บิลถัดไป', enabled: ENABLE_SALES_ORDERS },
      { menuKey: 'bulk_send_jobs', to: '/bulk-send-jobs', label: 'งานส่งเข้า SML', icon: Send, hint: 'ติดตามงานส่งจำนวนมาก' },
    ],
  },
  {
    label: 'นำเข้าและรับชำระ',
    items: [
      { menuKey: 'import_shopee', to: '/import/shopee', label: 'นำเข้า Shopee', icon: Upload, hint: 'นำเข้าจาก Shopee Excel สำหรับงานย้อนหลังหรือรายการตกหล่น', enabled: ENABLE_SHOPEE_EXCEL },
      { menuKey: 'import_lazada', to: '/import/lazada', label: 'นำเข้า Lazada', icon: Upload, hint: 'นำเข้าจาก Lazada Excel', enabled: ENABLE_LAZADA_EXCEL && ENABLE_SALES_ORDERS },
      { menuKey: 'import_tiktok', to: '/import/tiktok', label: 'นำเข้า TikTok', icon: Upload, hint: 'นำเข้าจาก TikTok Excel/CSV', enabled: ENABLE_TIKTOK_EXCEL && ENABLE_SALES_ORDERS },
      { menuKey: 'shopee_settlements', to: '/shopee-settlements', label: 'รับชำระ Shopee', icon: ReceiptText, hint: 'รอบถอนเงินและรับชำระ', enabled: ENABLE_SHOPEE_EXCEL && ENABLE_SALES_ORDERS },
    ],
  },
  {
    label: 'สินค้าและการจับคู่',
    items: [
      { menuKey: 'mappings', to: '/mappings', label: 'ตารางจับคู่สินค้า', icon: Workflow, hint: 'Item Mapping (raw_name → SML code)' },
      { menuKey: 'catalog', to: '/settings/catalog', label: 'สินค้าใน SML', icon: Database, hint: 'SML Catalog' },
    ],
  },
  {
    label: 'ลูกค้าและ LINE',
    items: [
      { menuKey: 'messages', to: '/messages', label: 'ข้อความลูกค้า', icon: MessageSquare, hasBadge: 'messages', hint: 'Inbox รวมทุก OA', minPhase: 2, enabled: ENABLE_CHAT },
      { menuKey: 'line_notifications', to: '/settings/line-notifications', label: 'LINE แจ้งเตือน', icon: Bell, hint: 'แจ้งออเดอร์ใหม่จากคำสั่งซื้อ Shopee', adminOnly: true },
      { menuKey: 'line_oa', to: '/settings/line-oa', label: 'บัญชี LINE OA', icon: MessageSquare, end: true, hint: 'LINE OA Accounts', minPhase: 2, enabled: ENABLE_CHAT },
      { menuKey: 'line_myshop', to: '/settings/line-myshop', label: 'LINE MyShop', icon: ShoppingBag, hint: 'บัญชี OA Plus และ webhook orders', enabled: ENABLE_LINE_MYSHOP, adminOnly: true },
      { menuKey: 'quick_replies', to: '/settings/quick-replies', label: 'ข้อความสำเร็จรูป', icon: MessageSquareQuote, end: true, hint: 'Quick Replies', minPhase: 2, enabled: ENABLE_CHAT },
      { menuKey: 'chat_tags', to: '/settings/chat-tags', label: 'ป้ายลูกค้า', icon: Tag, end: true, hint: 'Chat Tags', minPhase: 2, enabled: ENABLE_CHAT },
    ],
  },
  {
    label: 'เชื่อมต่อแพลตฟอร์ม',
    items: [
      { menuKey: 'setup', to: '/setup', label: 'สถานะพร้อมใช้งาน', icon: ClipboardCheck, hint: 'ตรวจความพร้อมร้าน' },
      { menuKey: 'channel_defaults', to: '/settings/channels', label: 'เส้นทางเอกสาร SML', icon: Building2, hint: 'Document Routing' },
      { menuKey: 'email_accounts', to: '/settings/email', label: 'กล่องอีเมลรับบิล', icon: Mail, hint: 'ตั้งค่ากล่องเมลสำหรับใบสั่งซื้อ' },
      { menuKey: 'shopee_connections', to: '/settings/shopee-connections', label: 'ร้าน Shopee', icon: Store, hint: 'OAuth / token / shop connection', adminOnly: true, enabled: ENABLE_SHOPEE_EXCEL },
      { menuKey: 'instance_settings', to: '/settings/instance', label: 'การเชื่อมต่อระบบ', icon: Settings2, hint: 'SML / OpenRouter / ร้านนี้' },
    ],
  },
  {
    label: 'ดูแลระบบ',
    items: [
      { menuKey: 'settings_users', to: '/settings/users', label: 'ผู้ใช้ระบบ', icon: UsersRound, hint: 'Roles and access', adminOnly: true },
      { menuKey: 'settings_menu_permissions', to: '/settings/menu-permissions', label: 'สิทธิ์เมนู', icon: ShieldCheck, hint: 'กำหนดเมนูที่ผู้ใช้เห็น', adminOnly: true },
      { menuKey: 'logs', to: '/logs', label: 'ประวัติการทำงาน', icon: ScrollText, hint: 'ใครทำอะไรและผลลัพธ์' },
      { menuKey: 'ai_usage', to: '/settings/ai-usage', label: 'การใช้งาน AI', icon: Bot, hint: 'ค่าใช้จ่าย / รุ่น AI' },
      { menuKey: 'old_data', to: '/settings/old-data', label: 'จัดการข้อมูลเก่า', icon: Archive, hint: 'เก็บบิล / ลบถาวร' },
    ],
  },
]

export function isNavItemVisible(item: NavItem, userOrRole?: User | string | null): boolean {
  const role = typeof userOrRole === 'string' ? userOrRole : userOrRole?.role
  return (
    item.enabled !== false &&
    (!item.minPhase || PHASE >= item.minPhase) &&
    (!item.adminOnly || role === 'admin') &&
    canViewMenu(userOrRole, item.menuKey)
  )
}

export function visibleNavGroups(userOrRole?: User | string | null): NavGroup[] {
  return NAV_GROUPS
    .map((group) => ({
      ...group,
      items: group.items.filter((item) => isNavItemVisible(item, userOrRole)),
    }))
    .filter((group) => group.items.length > 0)
}

export function visibleNavItems(userOrRole?: User | string | null): NavItem[] {
  return visibleNavGroups(userOrRole).flatMap((group) => group.items)
}

export function firstVisibleNavPath(userOrRole?: User | string | null): string {
  return visibleNavItems(userOrRole)[0]?.to ?? '/dashboard'
}

export function canViewMenu(userOrRole: User | string | null | undefined, menuKey: string): boolean {
  const role = typeof userOrRole === 'string' ? userOrRole : userOrRole?.role
  if (role === 'admin' && menuKey === 'settings_users') return true
  if (role === 'admin' && menuKey === 'settings_menu_permissions') return true

  const permissions = typeof userOrRole === 'string' ? undefined : userOrRole?.menu_permissions
  if (permissions && permissions.length > 0) {
    const permission = permissions.find((item) => item.menu_key === menuKey)
    if (permission) return permission.can_view
  }
  return roleDefaultCanView(role, menuKey)
}

export function permissionForMenu(user: User | null | undefined, menuKey: string): UserMenuPermission | null {
  const explicit = user?.menu_permissions?.find((item) => item.menu_key === menuKey)
  if (explicit) return explicit
  if (!user?.role) return null
  return {
    menu_key: menuKey,
    can_view: roleDefaultCanView(user.role, menuKey),
    can_create: user.role === 'admin',
    can_update: user.role === 'admin',
    can_delete: user.role === 'admin',
  }
}

function roleDefaultCanView(role: string | null | undefined, menuKey: string): boolean {
  if (role === 'admin') return true
  if (role === 'staff') return STAFF_DEFAULT_MENU_KEYS.has(menuKey)
  if (role === 'viewer') return VIEWER_DEFAULT_MENU_KEYS.has(menuKey)
  return false
}
