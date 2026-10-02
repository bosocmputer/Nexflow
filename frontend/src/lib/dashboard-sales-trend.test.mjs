import assert from 'node:assert/strict'
import test from 'node:test'

import { salesTrendDataForMode } from './dashboard-sales-trend.ts'

const points = [
  {
    date: '2026-10-01',
    previous_date: '2026-09-01',
    shopee_amount: 100,
    previous_shopee_amount: 40,
    lazada_amount: 20,
    previous_lazada_amount: 10,
    tiktok_amount: 30,
    previous_tiktok_amount: 20,
    nextstep_amount: 5,
    previous_nextstep_amount: 0,
    current_total: 155,
    previous_total: 70,
  },
  {
    date: '2026-10-02',
    previous_date: '2026-09-02',
    shopee_amount: 50,
    previous_shopee_amount: 60,
    lazada_amount: 10,
    previous_lazada_amount: 15,
    tiktok_amount: 40,
    previous_tiktok_amount: 25,
    nextstep_amount: 0,
    previous_nextstep_amount: 5,
    current_total: 100,
    previous_total: 105,
  },
]

test('daily trend mode preserves the original daily points', () => {
  assert.equal(salesTrendDataForMode(points, 'daily'), points)
})

test('cumulative trend mode adds each platform independently for both periods', () => {
  const result = salesTrendDataForMode(points, 'cumulative')

  assert.notEqual(result, points)
  assert.deepEqual(result[0], points[0])
  assert.deepEqual(result[1], {
    ...points[1],
    shopee_amount: 150,
    previous_shopee_amount: 100,
    lazada_amount: 30,
    previous_lazada_amount: 25,
    tiktok_amount: 70,
    previous_tiktok_amount: 45,
    nextstep_amount: 5,
    previous_nextstep_amount: 5,
    current_total: 255,
    previous_total: 175,
  })
  assert.equal(points[1].shopee_amount, 50, 'input must remain immutable')
})
