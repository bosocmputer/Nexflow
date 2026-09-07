import assert from 'node:assert/strict'
import test from 'node:test'

import { createServer } from 'vite'

const vite = await createServer({
  appType: 'custom',
  logLevel: 'error',
  server: { middlewareMode: true },
})

const { presentLineQuota, lineQuotaErrorLabel } = await vite.ssrLoadModule('/src/lib/line-quota.ts')

test.after(async () => {
  await vite.close()
})

test('presents limited quota and warning/full thresholds without negative remaining', () => {
  const base = {
    line_oa_id: 'oa-1',
    name: 'OA',
    quota_type: 'limited',
    limit: 300,
    status: 'ok',
    checked_at: '2026-09-07T10:00:00Z',
    is_stale: false,
  }
  const normal = presentLineQuota({ ...base, used: 18, remaining: 282 })
  assert.equal(normal.headline, 'ใช้แล้ว 18 / 300 ข้อความ')
  assert.equal(normal.severity, 'normal')
  assert.equal(presentLineQuota({ ...base, used: 240, remaining: 60 }).severity, 'warning')
  const full = presentLineQuota({ ...base, used: 318, remaining: 0 })
  assert.equal(full.severity, 'full')
  assert.equal(full.percentage, 100)
  assert.equal(full.detail, 'เหลือประมาณ 0 ข้อความ')
})

test('presents unlimited, none, and safe token error states', () => {
  const base = {
    line_oa_id: 'oa-1',
    name: 'OA',
    limit: null,
    remaining: null,
    status: 'ok',
    checked_at: '2026-09-07T10:00:00Z',
    is_stale: false,
    used: 12,
  }
  assert.equal(presentLineQuota({ ...base, quota_type: 'unlimited' }).headline, 'ไม่จำกัด')
  assert.equal(presentLineQuota({ ...base, quota_type: 'none' }).headline, 'ไม่มีโควตาส่งแบบ Push')
  assert.match(lineQuotaErrorLabel('line_token_invalid'), /token/)
  assert.doesNotMatch(lineQuotaErrorLabel('line_token_invalid'), /secret/i)
})
