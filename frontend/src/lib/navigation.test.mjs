import assert from 'node:assert/strict'
import test from 'node:test'

import { createServer } from 'vite'

const vite = await createServer({
  appType: 'custom',
  logLevel: 'error',
  server: { middlewareMode: true },
})

const { NAV_GROUPS, isNavItemActive, permissionNavGroups, visibleNavGroups } = await vite.ssrLoadModule('/src/lib/navigation.tsx')

test.after(async () => {
  await vite.close()
})

test('adds one cancellation shortcut that reuses the Shopee Operations permission', () => {
  const items = NAV_GROUPS.flatMap((group) => group.items)
  const shortcut = items.find((item) => item.label === 'เอกสารยกเลิก/รับคืน Shopee')

  assert.ok(shortcut)
  assert.equal(shortcut.menuKey, 'shopee_operations')
  assert.equal(shortcut.to, '/shopee-operations?status_group=cancelled')
})

test('activates only one Shopee sidebar entry for the cancelled filter', () => {
  const items = NAV_GROUPS.flatMap((group) => group.items)
  const orders = items.find((item) => item.label === 'คำสั่งซื้อ Shopee')
  const cancellations = items.find((item) => item.label === 'เอกสารยกเลิก/รับคืน Shopee')

  assert.equal(isNavItemActive(orders, '/shopee-operations', ''), true)
  assert.equal(isNavItemActive(cancellations, '/shopee-operations', ''), false)
  assert.equal(isNavItemActive(orders, '/shopee-operations', '?status_group=cancelled'), false)
  assert.equal(isNavItemActive(cancellations, '/shopee-operations', '?status_group=cancelled'), true)
})

test('places TikTok Shop orders in the orders group with its own permission', () => {
  const group = NAV_GROUPS.find((item) => item.label === 'คำสั่งซื้อ')
  const orders = group?.items.find((item) => item.label === 'คำสั่งซื้อ TikTok Shop')

  assert.ok(orders)
  assert.equal(orders.menuKey, 'tiktok_shop_operations')
  assert.equal(orders.to, '/tiktok-shop-operations')
  assert.match(orders.hint, /Webhook/)
})

test('groups daily work before settings and excludes retired pages', () => {
  assert.deepEqual(NAV_GROUPS.map((group) => group.label), [
    'ภาพรวม', 'คำสั่งซื้อ', 'เอกสารและรับชำระ', 'นำเข้าข้อมูล',
    'สินค้าและสต๊อก', 'ตั้งค่าช่องทาง', 'ดูแลระบบ',
  ])
  const items = NAV_GROUPS.flatMap((group) => group.items)
  for (const retired of ['line_myshop', 'setup', 'instance_settings', 'old_data']) {
    assert.equal(items.some((item) => item.menuKey === retired), false)
  }
  assert.ok(items.find((item) => item.menuKey === 'line_notifications'))
  assert.equal(items.find((item) => item.menuKey === 'channel_defaults')?.adminOnly, true)
})

test('permission groups contain unique actionable scopes and no admin-only staff rows', () => {
  const capabilities = { sales_orders_configured: true }
  const adminItems = permissionNavGroups('admin', capabilities).flatMap((group) => group.items)
  const staffItems = permissionNavGroups('staff', capabilities).flatMap((group) => group.items)
  assert.equal(new Set(adminItems.map((item) => item.menuKey)).size, adminItems.length)
  assert.ok(adminItems.filter((item) => item.menuKey === 'shopee_operations').length <= 1)
  assert.ok(adminItems.filter((item) => item.menuKey === 'tiktok_shop_operations').length <= 1)
  assert.equal(staffItems.some((item) => item.adminOnly), false)
  assert.ok(staffItems.some((item) => item.menuKey === 'logs'))
})

test('hides the SO queue and its permission scope when the tenant has no SO route', () => {
  const user = {
    role: 'admin',
    menu_permissions: [{ menu_key: 'sales_orders', can_view: true }],
    navigation_capabilities: { sales_orders_configured: false },
  }
  const visibleItems = visibleNavGroups(user).flatMap((group) => group.items)
  const permissionItems = permissionNavGroups('admin', user.navigation_capabilities).flatMap((group) => group.items)

  assert.equal(visibleItems.some((item) => item.menuKey === 'sales_orders'), false)
  assert.equal(permissionItems.some((item) => item.menuKey === 'sales_orders'), false)
})

test('adds a TikTok cancellation shortcut that reuses the TikTok operations permission', () => {
  const items = NAV_GROUPS.flatMap((group) => group.items)
  const shortcut = items.find((item) => item.label === 'เอกสารยกเลิก TikTok Shop')

  assert.ok(shortcut)
  assert.equal(shortcut.menuKey, 'tiktok_shop_operations')
  assert.equal(shortcut.to, '/tiktok-shop-operations?status_group=cancelled')
  assert.doesNotMatch(shortcut.label, /รับคืน/)
})

test('activates only one TikTok sidebar entry for the cancelled filter', () => {
  const items = NAV_GROUPS.flatMap((group) => group.items)
  const orders = items.find((item) => item.label === 'คำสั่งซื้อ TikTok Shop')
  const cancellations = items.find((item) => item.label === 'เอกสารยกเลิก TikTok Shop')

  assert.equal(isNavItemActive(orders, '/tiktok-shop-operations', ''), true)
  assert.equal(isNavItemActive(cancellations, '/tiktok-shop-operations', ''), false)
  assert.equal(isNavItemActive(orders, '/tiktok-shop-operations', '?status_group=cancelled'), false)
  assert.equal(isNavItemActive(cancellations, '/tiktok-shop-operations', '?status_group=cancelled'), true)
})

test('keeps TikTok Shop stock beside Shopee stock with its own permission and feature gate', () => {
  const group = NAV_GROUPS.find((item) => item.label === 'สินค้าและสต๊อก')
  const stock = group?.items.find((item) => item.label === 'ซิงก์สต๊อก TikTok Shop')

  assert.ok(stock)
  assert.equal(stock.menuKey, 'tiktok_shop_stock')
  assert.equal(stock.to, '/settings/tiktok-shop-stock')
  assert.match(stock.hint, /Product Catalog/)
})
