export function shopeeStockMutationRawName(itemName: string, modelName: string) {
  return modelName.trim() || itemName.trim()
}

export function shopeeStockMutationSourceSKU(itemSKU: string, modelSKU: string) {
  return modelSKU.trim() || itemSKU.trim()
}
