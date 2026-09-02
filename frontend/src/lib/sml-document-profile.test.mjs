import assert from 'node:assert/strict'
import test from 'node:test'

import { validateProfileText } from './smlDocumentProfile.js'

test('treats remark as literal free text, including braces that look like old tokens', () => {
  assert.equal(validateProfileText('{{channel}} | {{order_ref}} | {{bill_no}}'), '')
  assert.equal(validateProfileText('{{buyer_name}}'), '')
})

test('rejects control characters and more than 255 unicode characters', () => {
  assert.match(validateProfileText('safe\nunsafe'), /อักขระควบคุม/)
  assert.match(validateProfileText('ก'.repeat(256)), /255/)
  assert.equal(validateProfileText('ก'.repeat(255)), '')
})
