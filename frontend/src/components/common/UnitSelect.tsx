import { useEffect, useMemo, useState } from 'react'
import api from '@/api/client'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { cn } from '@/lib/utils'
import type { UnitOption } from '@/types'

interface UnitSelectProps {
  value: string
  onValueChange: (value: string) => void
  productCode?: string
  disabled?: boolean
  placeholder?: string
  className?: string
  limit?: number
  autoSelectSingle?: boolean
}

function unitLabel(unit: UnitOption) {
  const name = (unit.name_1 || unit.name_2 || unit.code).trim()
  if (!name || name === unit.code) return unit.code
  return `${unit.code} · ${name}`
}

function normalizeUnits(units: UnitOption[]) {
  const seen = new Set<string>()
  const normalized: UnitOption[] = []
  for (const unit of units) {
    const code = (unit.code || '').trim()
    if (!code || seen.has(code)) continue
    seen.add(code)
    normalized.push({
      ...unit,
      code,
      name_1: (unit.name_1 || '').trim() || code,
      name_2: (unit.name_2 || '').trim(),
    })
  }
  return normalized
}

export function UnitSelect({
  value,
  onValueChange,
  productCode,
  disabled = false,
  placeholder = 'เลือกหน่วย',
  className,
  limit = 200,
  autoSelectSingle = false,
}: UnitSelectProps) {
  const [units, setUnits] = useState<UnitOption[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const code = productCode?.trim()
  const productScoped = productCode !== undefined

  useEffect(() => {
    if (productScoped && !code) {
      setUnits([])
      setError('')
      return
    }

    const controller = new AbortController()
    setLoading(true)
    setError('')

    const endpoint = productScoped
      ? `/api/catalog/${encodeURIComponent(code ?? '')}/units`
      : '/api/sml/units'
    const params = productScoped ? undefined : { limit }

    api
      .get<{ units?: UnitOption[] }>(endpoint, { params, signal: controller.signal })
      .then((res) => setUnits(normalizeUnits(res.data.units ?? [])))
      .catch((err: unknown) => {
        if ((err as { code?: string })?.code === 'ERR_CANCELED') return
        setUnits([])
        setError(productScoped ? 'โหลดหน่วยของสินค้านี้ไม่ได้ ใช้ค่าปัจจุบันได้' : 'โหลดหน่วยจาก SML ไม่ได้')
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false)
      })

    return () => controller.abort()
  }, [code, limit, productScoped])

  const options = useMemo(() => {
    const current = value.trim()
    if (!current || units.some((unit) => unit.code === current)) {
      return units
    }
    return [
      ...units,
      {
        code: current,
        name_1: current,
      },
    ]
  }, [units, value])

  useEffect(() => {
    if (!autoSelectSingle || value.trim() || options.length !== 1) return
    onValueChange(options[0].code)
  }, [autoSelectSingle, onValueChange, options, value])

  const selectDisabled = disabled || loading || (productScoped && !code)
  const emptyMessage = productScoped && !code
    ? 'เลือกสินค้า SML ก่อน'
    : loading
      ? 'กำลังโหลดหน่วย...'
      : 'ไม่พบหน่วย'
  const selectPlaceholder =
    loading || (productScoped && !code) || options.length === 0 ? emptyMessage : placeholder

  return (
    <div className={cn('space-y-1', className)}>
      <Select
        value={value.trim() || undefined}
        onValueChange={onValueChange}
        disabled={selectDisabled}
      >
        <SelectTrigger className="h-10">
          <SelectValue placeholder={selectPlaceholder} />
        </SelectTrigger>
        <SelectContent>
          {options.length === 0 ? (
            <SelectItem value="__empty" disabled>
              {emptyMessage}
            </SelectItem>
          ) : (
            options.map((unit) => (
              <SelectItem key={unit.code} value={unit.code}>
                <span className="flex w-full items-center justify-between gap-3">
                  <span>{unitLabel(unit)}</span>
                  {unit.is_default && (
                    <span className="text-[11px] text-muted-foreground">หลัก</span>
                  )}
                </span>
              </SelectItem>
            ))
          )}
        </SelectContent>
      </Select>
      {error && <p className="text-[11px] leading-4 text-warning">{error}</p>}
      {!error && productScoped && !code && (
        <p className="text-[11px] leading-4 text-muted-foreground">เลือกสินค้า SML ก่อนจึงจะเลือกหน่วยได้</p>
      )}
    </div>
  )
}
