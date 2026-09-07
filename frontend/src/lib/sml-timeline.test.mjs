import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'

import { createServer } from 'vite'

const vite = await createServer({
  appType: 'custom',
  logLevel: 'error',
  server: { middlewareMode: true },
})

const { groupSMLTimelineEvents } = await vite.ssrLoadModule('/src/lib/sml-timeline.ts')
const fixture = JSON.parse(await readFile(
  new URL('./testdata/bill-sml-history-17.json', import.meta.url),
  'utf8',
))

test.after(async () => {
  await vite.close()
})

test('groups the controlled 17-event history as one resolved SML attempt', () => {
  const timeline = groupSMLTimelineEvents(fixture)
  const attempt = timeline.find((item) => item.kind === 'sml_attempt')

  assert.ok(attempt)
  assert.equal(attempt.attempt_id, 'attempt-001')
  assert.equal(attempt.events.length, 16)
  assert.equal(attempt.failure_count, 6)
  assert.equal(attempt.resolution_status, 'resolved')
  assert.equal(attempt.can_retry, false)
  assert.equal(attempt.summary, 'ส่ง SML อัตโนมัติสำเร็จหลังลองใหม่ 6 ครั้ง')
})

test('does not merge different attempt IDs even when doc number and route match', () => {
  const events = fixture.concat({
    ...fixture[2],
    id: 'other-attempt',
    trace_id: 'trace-sml-002',
    detail: {
      ...fixture[2].detail,
      attempt_id: 'attempt-002',
    },
  })
  const groups = groupSMLTimelineEvents(events).filter((item) => item.kind === 'sml_attempt')

  assert.equal(groups.length, 2)
  assert.deepEqual(groups.map((group) => group.attempt_id).sort(), ['attempt-001', 'attempt-002'])
})

test('leaves an event ungrouped when legacy evidence is ambiguous', () => {
  const legacy = {
    ...fixture[16],
    id: 'ambiguous-success',
    trace_id: '',
  }
  const attemptTwo = {
    ...fixture[2],
    id: 'attempt-two',
    trace_id: 'trace-sml-002',
    detail: { ...fixture[2].detail, attempt_id: 'attempt-002' },
  }
  const events = fixture.slice(0, 16).concat(attemptTwo, legacy)
  const timeline = groupSMLTimelineEvents(events)

  assert.ok(timeline.some((item) => item.kind === 'event' && item.event.id === 'ambiguous-success'))
})
