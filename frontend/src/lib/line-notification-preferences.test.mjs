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

test('LINE settings prioritizes recipients and separates connection setup', () => {
  assert.match(page, /useState<'recipients' \| 'connection'>\('recipients'\)/)
  assert.match(page, /<TabsTrigger value="recipients"[\s\S]*?ผู้รับแจ้งเตือน\s*<\/TabsTrigger>/)
  assert.match(page, /<TabsTrigger value="connection"[\s\S]*?การเชื่อมต่อ LINE OA\s*<\/TabsTrigger>/)
  assert.match(page, /เพิ่มผู้รับ/)
  assert.match(page, /<RecipientSheet/)
  assert.match(page, /<CandidatePickerSheet/)
})

test('LINE quota is lazy and connection onboarding disappears after readiness', () => {
  assert.doesNotMatch(page, /void load\(\)\s*\n\s*void loadQuota\(\)/)
  assert.match(page, /activeSection === 'connection'/)
  assert.match(page, /quotaRequestedRef/)
  assert.match(page, /!ready &&/)
})

test('recipient test flow lets the operator choose one subscribed Flex event', () => {
  assert.match(page, /function RecipientTestDialog/)
  assert.match(page, /เลือกประเภท Flex ที่ต้องการส่งทดสอบ/)
  assert.match(page, /recipient\?\.event_keys\.includes\(event\.key\)/)
})
