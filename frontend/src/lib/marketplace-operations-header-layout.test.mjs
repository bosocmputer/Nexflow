import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import test from 'node:test'

function source(relativePath) {
  return readFileSync(fileURLToPath(new URL(relativePath, import.meta.url)), 'utf8')
}

test('shared Marketplace Operations header separates shop controls from page actions', () => {
  const header = source('../components/marketplace/MarketplaceOperationsHeader.tsx')

  assert.match(header, /scopeControls: ReactNode/)
  assert.match(header, /data-slot="marketplace-operations-toolbar"/)
  assert.match(header, /data-slot="marketplace-operations-scope"/)
  assert.match(header, /data-slot="marketplace-operations-actions"/)
})

test('Shopee and TikTok operation pages use the shared scope-control layout', () => {
  for (const page of ['../pages/ShopeeOperations.tsx', '../pages/TikTokShopOperations.tsx']) {
    const contents = source(page)
    assert.match(contents, /scopeControls=\{<>/)
  }
})

test('TikTok cancelled queue does not repeat the cancelled-queue explanation below the header', () => {
  const contents = source('../pages/TikTokShopOperations.tsx')

  assert.doesNotMatch(contents, /คิวยกเลิก TikTok แยกจากงานคืนสินค้า\/คืนเงิน/)
})
