import assert from 'node:assert/strict'
import test from 'node:test'

import { createServer } from 'vite'

const vite = await createServer({
  appType: 'custom',
  logLevel: 'error',
  server: { middlewareMode: true },
})

const { NAV_GROUPS, isNavItemActive } = await vite.ssrLoadModule('/src/lib/navigation.tsx')

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
