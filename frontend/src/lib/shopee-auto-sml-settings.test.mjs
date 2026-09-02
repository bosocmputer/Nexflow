import assert from 'node:assert/strict'
import test from 'node:test'

import { createServer } from 'vite'

const vite = await createServer({
  appType: 'custom',
  logLevel: 'error',
  server: { middlewareMode: true },
})

const {
  autoSMLTriggerLabel,
  autoSMLTriggerDescription,
  normalizeAutoSMLTriggerStatus,
  requiredAutoSMLConfirmation,
} = await vite.ssrLoadModule('/src/lib/shopee-auto-sml-settings.ts')

test.after(async () => {
  await vite.close()
})

test('normalizes only supported Auto SML trigger statuses', () => {
  assert.equal(normalizeAutoSMLTriggerStatus(' processed '), 'PROCESSED')
  assert.equal(normalizeAutoSMLTriggerStatus('READY_TO_SHIP'), 'READY_TO_SHIP')
  assert.equal(normalizeAutoSMLTriggerStatus('SHIPPED'), 'READY_TO_SHIP')
})

test('provides user-facing Thai labels and explanations', () => {
  assert.equal(autoSMLTriggerLabel('READY_TO_SHIP'), 'รอจัดส่ง (READY_TO_SHIP)')
  assert.equal(autoSMLTriggerLabel('PROCESSED'), 'เตรียมจัดส่งแล้ว (PROCESSED)')
  assert.match(autoSMLTriggerDescription('PROCESSED'), /รอร้านเตรียมจัดส่ง/)
})

test('uses a distinct confirmation when an enabled shop changes trigger', () => {
  assert.equal(requiredAutoSMLConfirmation(false, 'READY_TO_SHIP', true, 'PROCESSED'), 'ENABLE_AUTO_SML')
  assert.equal(requiredAutoSMLConfirmation(true, 'READY_TO_SHIP', true, 'PROCESSED'), 'UPDATE_AUTO_SML_TRIGGER')
  assert.equal(requiredAutoSMLConfirmation(true, 'PROCESSED', false, 'PROCESSED'), '')
  assert.equal(requiredAutoSMLConfirmation(true, 'READY_TO_SHIP', true, 'READY_TO_SHIP', 'profile_terminal_failure'), 'RESUME_AUTO_SML')
})
