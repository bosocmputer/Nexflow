import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import test from 'node:test'

const dialogFiles = [
  '../pages/ChannelDefaults/EditDialog.tsx',
  '../pages/ChannelDefaults/ShopeeSMLRouteBundleDialog.tsx',
]

for (const file of dialogFiles) {
  test(`${file} uses the shared discard confirmation for unsaved settings`, () => {
    const source = readFileSync(fileURLToPath(new URL(file, import.meta.url)), 'utf8')
    assert.doesNotMatch(source, /\b(?:window\.)?(?:alert|confirm|prompt)\s*\(/)
    assert.match(source, /<ConfirmDialog\b/)
    assert.match(source, /onClick=\{\(\) => requestOpenChange\(false\)\}/)
  })
}
