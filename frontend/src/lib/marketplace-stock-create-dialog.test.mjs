import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import test from 'node:test'

const pageSource = readFileSync(
  fileURLToPath(new URL('../pages/MarketplaceStock.tsx', import.meta.url)),
  'utf8',
)

function autocompleteSource() {
  const start = pageSource.indexOf('function SMLGroupAutocomplete')
  const end = pageSource.indexOf('function selectedGroupLabel')
  assert.ok(start >= 0 && end > start, 'SMLGroupAutocomplete source must exist')
  return pageSource.slice(start, end)
}

test('Marketplace stock creation uses a controlled combobox that closes after selecting an SML group', () => {
  const autocomplete = autocompleteSource()

  assert.match(autocomplete, /const \[open, setOpen\] = useState\(false\)/)
  assert.match(autocomplete, /<Popover modal open=\{open\} onOpenChange=\{setOpen\}>/)
  assert.match(autocomplete, /const selectGroup = \(key: string\) => \{ onChange\(key\); setQuery\(''\); setOpen\(false\) \}/)
})

test('Marketplace stock creation explains mapping eligibility and preserves the next step in the dialog', () => {
  const autocomplete = autocompleteSource()
  const dialogStart = pageSource.indexOf('function CreatePoolDialog')
  const dialogEnd = pageSource.indexOf('function poolMemberKey')
  const dialog = pageSource.slice(dialogStart, dialogEnd)

  assert.match(autocomplete, /เลือกสินค้าที่จับคู่แล้ว/)
  assert.match(autocomplete, /จับคู่สินค้า Marketplace ก่อน/)
  assert.match(autocomplete, /SKU Marketplace พร้อมเพิ่มในกลุ่ม/)
  assert.match(dialog, /\{groupKey \? <>/)
})
