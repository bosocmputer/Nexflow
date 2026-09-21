import assert from 'node:assert/strict'
import test from 'node:test'

import { createServer } from 'vite'

const vite = await createServer({
  appType: 'custom',
  logLevel: 'error',
  server: { middlewareMode: true },
})

const { marketplaceErrorMessage } = await vite.ssrLoadModule('/src/lib/marketplace-error.ts')

test.after(async () => {
  await vite.close()
})

test('renders a nested API error message as text instead of a React child object', () => {
  assert.equal(
    marketplaceErrorMessage(
      { response: { data: { error: { code: 'feature_disabled', message: 'Tenant นี้ยังไม่ได้เปิด Product Catalog ของ TikTok Shop' } } } },
      'เกิดข้อผิดพลาด',
    ),
    'Tenant นี้ยังไม่ได้เปิด Product Catalog ของ TikTok Shop',
  )
})
