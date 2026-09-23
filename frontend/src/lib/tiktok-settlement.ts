type SettlementShop = {
  shop_id: string
  disabled?: boolean
}

export function resolveTikTokSettlementShopID(
  selectedShopID: string,
  shops: SettlementShop[],
): string {
  if (selectedShopID) return selectedShopID

  const activeShops = shops.filter((shop) => !shop.disabled && shop.shop_id)
  return activeShops.length === 1 ? activeShops[0].shop_id : ''
}
