import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import test from 'node:test'

const dashboardSource = readFileSync(
  fileURLToPath(new URL('../pages/Dashboard.tsx', import.meta.url)),
  'utf8',
)

function salesOverviewSource() {
  const start = dashboardSource.indexOf('function PlatformSalesOverview')
  const end = dashboardSource.indexOf('function DashboardDateFilter')
  assert.ok(start >= 0 && end > start, 'PlatformSalesOverview source must exist')
  return dashboardSource.slice(start, end)
}

test('dashboard report header uses one compact report identity and metric strip', () => {
  const overview = salesOverviewSource()

  assert.match(overview, /data-slot="dashboard-report-header"/)
  assert.match(overview, /data-slot="dashboard-metric-strip"/)
  assert.match(overview, /<h1[^>]*>ยอดขาย Nexflow<\/h1>/)
  assert.match(overview, /ยอดขายรวม/)
  assert.match(overview, /ยอดขายวันที่/)
  assert.match(overview, /ต้องตรวจ/)
})

test('dashboard report header removes repeated system wording and exposes source details on demand', () => {
  const overview = salesOverviewSource()

  assert.doesNotMatch(overview, /ยอดขายใน Nexflow ตามช่วงวันที่/)
  assert.doesNotMatch(overview, /ยอดวันสิ้นสุด/)
  assert.doesNotMatch(overview, /รายการเสี่ยง/)
  assert.match(overview, /ที่มาของยอดขาย/)
  assert.match(overview, /PopoverTrigger/)
  assert.equal((overview.match(/<DashboardDateFilter/g) ?? []).length, 1)
})
