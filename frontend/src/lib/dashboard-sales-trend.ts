export type DashboardSalesTrendMode = 'daily' | 'cumulative'

export type DashboardSalesTrendPoint = {
  date: string
  previous_date: string
  shopee_amount: number
  previous_shopee_amount: number
  lazada_amount: number
  previous_lazada_amount: number
  tiktok_amount: number
  previous_tiktok_amount: number
  nextstep_amount: number
  previous_nextstep_amount: number
  current_total: number
  previous_total: number
}

export function salesTrendDataForMode(
  data: DashboardSalesTrendPoint[],
  mode: DashboardSalesTrendMode,
): DashboardSalesTrendPoint[] {
  if (mode === 'daily') return data

  let shopeeAmount = 0
  let previousShopeeAmount = 0
  let lazadaAmount = 0
  let previousLazadaAmount = 0
  let tiktokAmount = 0
  let previousTikTokAmount = 0
  let nextStepAmount = 0
  let previousNextStepAmount = 0

  return data.map((point) => {
    shopeeAmount += finiteAmount(point.shopee_amount)
    previousShopeeAmount += finiteAmount(point.previous_shopee_amount)
    lazadaAmount += finiteAmount(point.lazada_amount)
    previousLazadaAmount += finiteAmount(point.previous_lazada_amount)
    tiktokAmount += finiteAmount(point.tiktok_amount)
    previousTikTokAmount += finiteAmount(point.previous_tiktok_amount)
    nextStepAmount += finiteAmount(point.nextstep_amount)
    previousNextStepAmount += finiteAmount(point.previous_nextstep_amount)

    return {
      ...point,
      shopee_amount: shopeeAmount,
      previous_shopee_amount: previousShopeeAmount,
      lazada_amount: lazadaAmount,
      previous_lazada_amount: previousLazadaAmount,
      tiktok_amount: tiktokAmount,
      previous_tiktok_amount: previousTikTokAmount,
      nextstep_amount: nextStepAmount,
      previous_nextstep_amount: previousNextStepAmount,
      current_total: shopeeAmount + lazadaAmount + tiktokAmount + nextStepAmount,
      previous_total:
        previousShopeeAmount +
        previousLazadaAmount +
        previousTikTokAmount +
        previousNextStepAmount,
    }
  })
}

function finiteAmount(value: number): number {
  const amount = Number(value || 0)
  return Number.isFinite(amount) ? amount : 0
}
