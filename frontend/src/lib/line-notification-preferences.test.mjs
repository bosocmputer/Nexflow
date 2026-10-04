import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import test from 'node:test'

const page = readFileSync(
  fileURLToPath(new URL('../pages/LineNotifications.tsx', import.meta.url)),
  'utf8',
)
const preferences = readFileSync(
  fileURLToPath(new URL('../components/line-notifications/LineNotificationPreferences.tsx', import.meta.url)),
  'utf8',
)
const source = `${page}\n${preferences}`

test('LINE recipients edit stable event subscriptions with explicit presets', () => {
  assert.match(source, /event_catalog/)
  assert.match(source, /event_keys/)
  assert.match(source, /งานออเดอร์/)
  assert.match(source, /เฉพาะที่ต้องดำเนินการ/)
  assert.match(source, /รับทั้งหมด/)
  assert.match(source, /<Checkbox/)
})

test('LINE sample dialog renders real Flex payload without sending it', () => {
  assert.match(source, /line-notifications\/samples/)
  assert.match(source, /function FlexMessagePreview/)
  assert.match(source, /ตัวอย่างจาก Flex payload จริง/)
  assert.match(source, /การเปิดหน้าต่างนี้ไม่ส่งข้อความ LINE/)
})

test('LINE delivery history exposes event identity and suppressed work', () => {
  assert.match(source, /event_key/)
  assert.match(source, /suppressed/)
  assert.match(source, /ระงับแล้ว/)
})
