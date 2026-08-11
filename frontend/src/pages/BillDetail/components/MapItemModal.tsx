import { MarketplaceMappingDrawer } from '@/components/marketplace/MarketplaceMappingDrawer'
import type { CatalogMatch } from '@/types'

interface Props {
  open: boolean
  rawName: string
  currentCode: string
  currentUnit: string
  currentPrice: number
  sourceImageUrl?: string
  rawNameLabel?: string
  onPick: (code: string, unitCode: string, picked?: CatalogMatch) => void
  onClose: () => void
}

export function MapItemModal({ open, rawName, currentCode, currentUnit, sourceImageUrl, rawNameLabel, onPick, onClose }: Props) {
  return (
    <MarketplaceMappingDrawer
      open={open}
      rawName={rawName}
      currentCode={currentCode}
      currentUnit={currentUnit}
      sourceImageUrl={sourceImageUrl}
      rawNameLabel={rawNameLabel}
      onPick={onPick}
      onOpenChange={(value) => !value && onClose()}
    />
  )
}
