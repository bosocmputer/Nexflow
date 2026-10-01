import {
  Building2,
  Database,
  LayoutDashboard,
  Bell,
  ReceiptText,
  RadioTower,
  ListOrdered,
  RotateCcw,
  ScrollText,
  Send,
  ShieldCheck,
  ShoppingBag,
  PackageCheck,
  Store,
  Tags,
  Upload,
  UsersRound,
  type LucideIcon,
} from 'lucide-react'

import {
  ENABLE_LAZADA_EXCEL,
  ENABLE_MARKETPLACE_STOCK,
  ENABLE_SALES_ORDERS,
  ENABLE_SHOPEE_EXCEL,
  ENABLE_SHOPEE_REALTIME_OPS,
  ENABLE_TIKTOK_EXCEL,
  ENABLE_TIKTOK_SHOP_API,
  ENABLE_TIKTOK_SHOP_STOCK, ENABLE_TIKTOK_SHOP_FINANCE,
} from '@/lib/featureFlags'
import type { User, UserMenuPermission } from '@/types'

const PHASE = Number(import.meta.env.VITE_PHASE ?? 99)

export type NavBadgeKey =
  | boolean
  | 'bills'
  | 'saleorder'
  | 'saleinvoice'
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
  'tiktok_shop_operations',
  'marketplace_operations',
  'sale_invoices',
  'sales_orders',
  'marketplace_aliases',
  'bulk_send_jobs',
  'import_shopee',
  'import_lazada',
  'import_tiktok',
  'shopee_settlements',
  'tiktok_settlements',
  'catalog',
  'logs',
])

const VIEWER_DEFAULT_MENU_KEYS = new Set([
  'dashboard',
  'sale_invoices',
  'sales_orders',
  'catalog',
])

// Daily work first. Sidebar, command palette, and permission settings share
// this source so a page cannot silently disappear from one navigation surface.
export const NAV_GROUPS: NavGroup[] = [
  {
    label: 'ภาพรวม',
    items: [
      { menuKey: 'dashboard', to: '/dashboard', label: 'ยอดขายตามแพลตฟอร์ม', icon: LayoutDashboard, hint: 'ยอดเอกสารขาย Shopee, Lazada, TikTok และ NextStep Marketplace' },
    ],
  },
  {
    label: 'คำสั่งซื้อ',
    items: [
      { menuKey: 'shopee_operations', to: '/shopee-operations', label: 'คำสั่งซื้อ Shopee', icon: RadioTower, hasBadge: 'shopee_realtime', hint: 'คิวงานประจำวันจาก Shopee Push/Sync', enabled: ENABLE_SHOPEE_REALTIME_OPS },
      { menuKey: 'shopee_operations', to: '/shopee-operations?status_group=cancelled', label: 'เอกสารยกเลิก/รับคืน Shopee', icon: RotateCcw, hint: 'Order ที่ยกเลิกและเอกสาร SML หลังยกเลิก', enabled: ENABLE_SHOPEE_REALTIME_OPS },
      { menuKey: 'tiktok_shop_operations', to: '/tiktok-shop-operations', label: 'คำสั่งซื้อ TikTok Shop', icon: ListOrdered, hint: 'คิวงานแบบเรียลไทม์จาก TikTok Webhook พร้อมซิงก์สำรอง', enabled: ENABLE_TIKTOK_SHOP_API },
      { menuKey: 'tiktok_shop_operations', to: '/tiktok-shop-operations?status_group=cancelled', label: 'เอกสารยกเลิก TikTok Shop', icon: RotateCcw, hint: 'ออเดอร์ TikTok ที่ยกเลิกและสถานะใบขายเดิมใน SML', enabled: ENABLE_TIKTOK_SHOP_API },
      { menuKey: 'nextstep_marketplace', to: '/nextstep-marketplace', label: 'NextStep Marketplace', icon: Store, hasBadge: 'nextstep_marketplace', hint: 'ออเดอร์ MQT จาก SML marketplace' },
    ],
  },
  {
    label: 'เอกสารและรับชำระ',
    items: [
      { menuKey: 'sale_invoices', to: '/sale-invoices', label: 'ขายสินค้าและบริการ', icon: ShoppingBag, hasBadge: 'saleinvoice', hint: 'คิวบิลขายหลัก ส่งเข้า SML', enabled: ENABLE_SALES_ORDERS },
      { menuKey: 'sales_orders', to: '/sales-orders', label: 'ใบสั่งขาย (SO)', icon: ShoppingBag, hasBadge: 'saleorder', hint: 'คิวใบสั่งขายที่ยังเปิดใช้งาน', enabled: ENABLE_SALES_ORDERS },
      { menuKey: 'bulk_send_jobs', to: '/bulk-send-jobs', label: 'งานส่งเข้า SML', icon: Send, hint: 'ติดตามงานส่งจำนวนมาก' },
      { menuKey: 'shopee_settlements', to: '/shopee-settlements', label: 'รับชำระ Shopee', icon: ReceiptText, hint: 'รอบถอนเงินและรับชำระ', enabled: ENABLE_SHOPEE_EXCEL && ENABLE_SALES_ORDERS },
      { menuKey: 'tiktok_settlements', to: '/tiktok-settlements', label: 'รับชำระ TikTok Shop', icon: ReceiptText, hint: 'Statement และรับชำระหนี้ที่ตรวจแล้ว', enabled: ENABLE_TIKTOK_SHOP_FINANCE && ENABLE_SALES_ORDERS },
    ],
  },
  {
    label: 'นำเข้าข้อมูล',
    items: [
      { menuKey: 'import_shopee', to: '/import/shopee', label: 'นำเข้า Shopee', icon: Upload, hint: 'นำเข้าจาก Shopee Excel สำหรับงานย้อนหลังหรือรายการตกหล่น', enabled: ENABLE_SHOPEE_EXCEL },
      { menuKey: 'import_lazada', to: '/import/lazada', label: 'นำเข้า Lazada', icon: Upload, hint: 'นำเข้าจาก Lazada Excel', enabled: ENABLE_LAZADA_EXCEL && ENABLE_SALES_ORDERS },
      { menuKey: 'import_tiktok', to: '/import/tiktok', label: 'นำเข้า TikTok', icon: Upload, hint: 'นำเข้าจาก TikTok Excel/CSV', enabled: ENABLE_TIKTOK_EXCEL && ENABLE_SALES_ORDERS },
    ],
  },
  {
    label: 'สินค้าและสต๊อก',
    items: [
      { menuKey: 'marketplace_aliases', to: '/marketplace-aliases', label: 'จับคู่สินค้า Marketplace', icon: Tags, hasBadge: 'marketplace_aliases', hint: 'จัดการความสัมพันธ์สินค้า Marketplace ไปยัง SML', enabled: ENABLE_SALES_ORDERS },
      { menuKey: 'catalog', to: '/settings/catalog', label: 'รายการสินค้า SML', icon: Database, hint: 'ดู ค้นหา และรีเฟรชสินค้าปลายทางจาก SML' },
      { menuKey: 'marketplace_stock', to: '/settings/marketplace-stock', label: 'ควบคุมสต๊อก Marketplace', icon: PackageCheck, hint: 'กำหนดโควตาหรือสต๊อกร่วมจากยอดพร้อมขายใน SML', enabled: ENABLE_MARKETPLACE_STOCK },
      { menuKey: 'shopee_stock', to: '/settings/shopee-stock', label: 'ซิงก์สต๊อก Shopee', icon: PackageCheck, hint: 'คุมสต๊อก Shopee จากยอดพร้อมขายใน SML', enabled: !ENABLE_MARKETPLACE_STOCK },
      { menuKey: 'tiktok_shop_stock', to: '/settings/tiktok-shop-stock', label: 'ซิงก์สต๊อก TikTok Shop', icon: PackageCheck, hint: 'ตรวจ Product Catalog และเตรียมคุมสต๊อก TikTok Shop จาก SML', enabled: !ENABLE_MARKETPLACE_STOCK && ENABLE_TIKTOK_SHOP_STOCK },
    ],
  },
  {
    label: 'ตั้งค่าช่องทาง',
    items: [
      { menuKey: 'channel_defaults', to: '/settings/channels', label: 'เส้นทางเอกสาร SML', icon: Building2, hint: 'กำหนดเอกสาร SML แยกตามช่องทาง', adminOnly: true },
      { menuKey: 'shopee_connections', to: '/settings/shopee-connections', label: 'ร้าน Shopee', icon: Store, hint: 'เชื่อมต่อและจัดการร้าน Shopee', adminOnly: true, enabled: ENABLE_SHOPEE_EXCEL },
      { menuKey: 'tiktok_shop_connections', to: '/settings/tiktok-shop', label: 'ร้าน TikTok Shop', icon: Store, hint: 'เชื่อมต่อร้านผ่าน TikTok Shop Open API', adminOnly: true, enabled: ENABLE_TIKTOK_SHOP_API },
      { menuKey: 'line_notifications', to: '/settings/line-notifications', label: 'LINE แจ้งเตือน', icon: Bell, hint: 'แจ้งออเดอร์ใหม่จาก Shopee, TikTok Shop และ NextStep Marketplace', adminOnly: true },
    ],
  },
  {
    label: 'ดูแลระบบ',
    items: [
      { menuKey: 'settings_users', to: '/settings/users', label: 'ผู้ใช้ระบบ', icon: UsersRound, hint: 'Roles and access', adminOnly: true },
      { menuKey: 'settings_menu_permissions', to: '/settings/menu-permissions', label: 'สิทธิ์เมนู', icon: ShieldCheck, hint: 'กำหนดเมนูที่ผู้ใช้เห็น', adminOnly: true },
      { menuKey: 'logs', to: '/logs', label: 'ประวัติการทำงาน', icon: ScrollText, hint: 'ใครทำอะไรและผลลัพธ์' },
    ],
  },
]

// Cancellation shortcuts reuse their source permission; show each scope only
// once in the editor. Admin-only pages cannot be granted to staff/viewers.
export function permissionNavGroups(role: User['role']): NavGroup[] {
  const seen = new Set<string>()
  return NAV_GROUPS
    .map((group) => ({
      ...group,
      items: group.items.filter((item) => {
        if (item.enabled === false || (item.minPhase && PHASE < item.minPhase)) return false
        if (item.adminOnly && role !== 'admin') return false
        if (seen.has(item.menuKey)) return false
        seen.add(item.menuKey)
        return true
      }),
    }))
    .filter((group) => group.items.length > 0)
}

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

export function isNavItemActive(item: NavItem | undefined, pathname: string, search: string): boolean {
  if (!item) return false
  const currentParams = new URLSearchParams(search)

  const [itemPath, itemSearch = ''] = item.to.split('?', 2)
  const pathMatches = item.end
    ? pathname === itemPath
    : pathname === itemPath || pathname.startsWith(`${itemPath}/`)
  if (!pathMatches) return false

  const itemStatusGroup = new URLSearchParams(itemSearch).get('status_group')
  const currentStatusGroup = currentParams.get('status_group')
  if (itemStatusGroup) return currentStatusGroup === itemStatusGroup
  if (itemPath === '/shopee-operations' || itemPath === '/tiktok-shop-operations') return currentStatusGroup !== 'cancelled'
  return true
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
