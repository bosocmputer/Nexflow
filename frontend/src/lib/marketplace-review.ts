type MarketplacePendingCounts = {
  catalog_product?: boolean
  discovery_source?: string
  order_count?: number
  item_count: number
  bill_count: number
}

export function marketplacePendingSummary(row: MarketplacePendingCounts) {
  if (row.discovery_source === 'tiktok_order_snapshot') {
    return {
      primary: 'รอจับคู่',
      secondary: `พบใน ${(row.order_count ?? 0).toLocaleString('th-TH')} ออเดอร์ TikTok Shop`,
    }
  }
  if (row.discovery_source === 'tiktok_product_catalog') {
    return { primary: 'รอจับคู่', secondary: 'จากรายการสินค้า TikTok Shop' }
  }
  if (row.catalog_product && row.bill_count === 0) {
    return { primary: 'รอจับคู่', secondary: 'จากรายการสินค้า Shopee' }
  }
  return {
    primary: `${row.item_count.toLocaleString('th-TH')} รายการ`,
    secondary: `${row.bill_count.toLocaleString('th-TH')} บิล`,
  }
}

export function buildTikTokOrderMappingReviewPath(accountKey: string, orderID: string) {
  const shopID = accountKey.startsWith('shop:') ? accountKey.slice('shop:'.length) : ''
  const encodedShopID = encodeURIComponent(shopID)
  const encodedOrderID = encodeURIComponent(orderID)
  return `/tiktok-shop-operations?shop_id=${encodedShopID}&order_id=${encodedOrderID}&review_order_id=${encodedOrderID}`
}
