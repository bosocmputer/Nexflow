import assert from 'node:assert/strict'
import test from 'node:test'

import { createServer } from 'vite'

const vite = await createServer({
  appType: 'custom',
  logLevel: 'error',
  server: { middlewareMode: true },
})

const {
  normalizeTikTokAutoSMLTriggerStatus,
  tiktokAutoSMLTriggerDescription,
  tiktokAutoSMLTriggerLabel,
} = await vite.ssrLoadModule('/src/lib/tiktok-auto-sml-settings.ts')

test.after(async () => vite.close())

test('normalizes only supported TikTok Auto SML trigger statuses', () => {
  assert.equal(normalizeTikTokAutoSMLTriggerStatus(' awaiting_shipment '), 'AWAITING_SHIPMENT')
  assert.equal(normalizeTikTokAutoSMLTriggerStatus('AWAITING_COLLECTION'), 'AWAITING_COLLECTION')
  assert.equal(normalizeTikTokAutoSMLTriggerStatus('IN_TRANSIT'), 'IN_TRANSIT')
  assert.equal(normalizeTikTokAutoSMLTriggerStatus('COMPLETED'), 'COMPLETED')
  assert.equal(normalizeTikTokAutoSMLTriggerStatus('UNPAID'), 'AWAITING_COLLECTION')
})

test('provides Thai labels and descriptions for every selectable status', () => {
  assert.equal(tiktokAutoSMLTriggerLabel('AWAITING_SHIPMENT'), 'รอจัดส่ง')
  assert.equal(tiktokAutoSMLTriggerLabel('AWAITING_COLLECTION'), 'รอรับพัสดุ')
  assert.equal(tiktokAutoSMLTriggerLabel('IN_TRANSIT'), 'กำลังขนส่ง')
  assert.equal(tiktokAutoSMLTriggerLabel('COMPLETED'), 'สำเร็จ')
  assert.match(tiktokAutoSMLTriggerDescription('COMPLETED'), /เสร็จสมบูรณ์/)
})
