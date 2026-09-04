type MarketplacePendingCounts = {
  catalog_product?: boolean
  item_count: number
  bill_count: number
}

export function marketplacePendingSummary(row: MarketplacePendingCounts) {
  if (row.catalog_product && row.bill_count === 0) {
    return { primary: 'รอจับคู่', secondary: 'จากรายการสินค้า Shopee' }
  }
  return {
    primary: `${row.item_count.toLocaleString('th-TH')} รายการ`,
    secondary: `${row.bill_count.toLocaleString('th-TH')} บิล`,
  }
}
