import { useEffect, useMemo, useRef, useState } from 'react'
import { Check, ChevronsUpDown, Search, X } from 'lucide-react'

import client from '@/api/client'
import { Button } from '@/components/ui/button'
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from '@/components/ui/popover'
import { humanizeSMLConnectionError } from '@/lib/sml-readiness'
import { cn } from '@/lib/utils'

type MasterKind = 'branch' | 'sale'

type SMLMasterOption = {
  code: string
  name_1?: string
}

interface SMLMasterCodePickerProps {
  kind: MasterKind
  value: string
  onChange: (code: string) => void
  disabled?: boolean
}

const MASTER_CONFIG: Record<MasterKind, {
  endpoint: string
  placeholder: string
  searchPlaceholder: string
  emptyText: string
  missingText: string
}> = {
  branch: {
    endpoint: '/api/sml/branches',
    placeholder: 'ไม่ระบุสาขา',
    searchPlaceholder: 'ค้นหารหัส / ชื่อสาขา…',
    emptyText: 'ไม่พบสาขาใน SML',
    missingText: 'รหัสสาขาเดิมไม่พบใน SML',
  },
  sale: {
    endpoint: '/api/sml/sales',
    placeholder: 'ไม่ระบุพนักงานขาย',
    searchPlaceholder: 'ค้นหารหัส / ชื่อพนักงานขาย…',
    emptyText: 'ไม่พบพนักงานขายใน SML',
    missingText: 'รหัสพนักงานขายเดิมไม่พบใน SML',
  },
}

export function SMLMasterCodePicker({ kind, value, onChange, disabled }: SMLMasterCodePickerProps) {
  const config = MASTER_CONFIG[kind]
  const [open, setOpen] = useState(false)
  const [query, setQuery] = useState('')
  const [results, setResults] = useState<SMLMasterOption[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [total, setTotal] = useState(0)
  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  const selected = useMemo(
    () => results.find((row) => row.code === value.trim()),
    [results, value],
  )
  const hasUnknownValue = value.trim() !== '' && !selected && !loading

  const fetchResults = useMemo(
    () => async (q: string) => {
      setLoading(true)
      setError('')
      try {
        const r = await client.get<{ data: SMLMasterOption[]; total?: number }>(
          `${config.endpoint}?search=${encodeURIComponent(q)}&limit=30`,
        )
        setResults(r.data.data ?? [])
        setTotal(r.data.total ?? 0)
      } catch (err: any) {
        setResults([])
        setTotal(0)
        setError(humanizeSMLConnectionError(err?.response?.data?.error ?? err?.message ?? ''))
      } finally {
        setLoading(false)
      }
    },
    [config.endpoint],
  )

  useEffect(() => {
    if (!open) return
    fetchResults('')
  }, [fetchResults, open])

  useEffect(() => {
    if (!open) return
    if (debounceRef.current) clearTimeout(debounceRef.current)
    debounceRef.current = setTimeout(() => fetchResults(query), 250)
    return () => {
      if (debounceRef.current) clearTimeout(debounceRef.current)
    }
  }, [fetchResults, open, query])

  return (
    <Popover modal open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button
          type="button"
          variant="outline"
          role="combobox"
          aria-expanded={open}
          className="w-full justify-between font-normal"
          disabled={disabled}
        >
          {value.trim() ? (
            <span className="flex min-w-0 items-center gap-2 truncate text-left">
              <span className="font-mono text-xs text-muted-foreground">{value.trim()}</span>
              {selected?.name_1 && <span className="truncate">{selected.name_1}</span>}
            </span>
          ) : (
            <span className="text-muted-foreground">{config.placeholder}</span>
          )}
          <ChevronsUpDown className="ml-2 h-4 w-4 shrink-0 opacity-50" />
        </Button>
      </PopoverTrigger>
      <PopoverContent className="w-[min(420px,calc(100vw-2rem))] p-0" align="start">
        <div className="relative border-b border-border">
          <Search className="pointer-events-none absolute left-3 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" />
          <input
            autoFocus
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder={config.searchPlaceholder}
            className="h-10 w-full bg-transparent px-9 text-sm placeholder:text-muted-foreground focus:outline-none"
          />
          {loading && (
            <div className="absolute right-3 top-1/2 h-3.5 w-3.5 -translate-y-1/2 animate-spin rounded-full border-2 border-muted-foreground/30 border-t-muted-foreground" />
          )}
        </div>
        <div
          className="overflow-y-auto py-1"
          style={{ maxHeight: 'min(300px, var(--radix-popover-content-available-height, 300px))' }}
        >
          <ClearRow
            selected={!value.trim()}
            onClick={() => {
              onChange('')
              setOpen(false)
            }}
          />
          {error ? (
            <div className="px-3 py-6 text-center text-sm text-destructive">{error}</div>
          ) : results.length === 0 && !loading ? (
            <div className="px-3 py-6 text-center text-sm text-muted-foreground">
              {query ? `${config.emptyText}จากคำค้นนี้` : config.emptyText}
              {hasUnknownValue && (
                <div className="mt-2 text-xs text-warning">
                  {config.missingText} ({value.trim()}) เลือกใหม่หรือล้างค่าได้
                </div>
              )}
            </div>
          ) : (
            results.map((row) => (
              <MasterRow
                key={row.code}
                option={row}
                selected={value.trim() === row.code}
                onClick={() => {
                  onChange(row.code)
                  setOpen(false)
                }}
              />
            ))
          )}
        </div>
        <div className="border-t border-border bg-muted/30 px-3 py-2 text-xs text-muted-foreground">
          {total.toLocaleString('th-TH')} รายการจาก SML
        </div>
      </PopoverContent>
    </Popover>
  )
}

function ClearRow({ selected, onClick }: { selected: boolean; onClick: () => void }) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={cn(
        'flex w-full items-center gap-3 px-3 py-2 text-left text-sm hover:bg-accent',
        selected && 'bg-accent',
      )}
    >
      <Check className={cn('h-4 w-4 shrink-0', selected ? 'opacity-100' : 'opacity-0')} />
      <X className="h-3.5 w-3.5 text-muted-foreground" />
      <span className="text-muted-foreground">ไม่ระบุ</span>
    </button>
  )
}

function MasterRow({
  option,
  selected,
  onClick,
}: {
  option: SMLMasterOption
  selected: boolean
  onClick: () => void
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={cn(
        'flex w-full items-start gap-3 px-3 py-2 text-left text-sm hover:bg-accent',
        selected && 'bg-accent',
      )}
    >
      <Check className={cn('mt-1 h-4 w-4 shrink-0', selected ? 'opacity-100' : 'opacity-0')} />
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-2">
          <span className="font-mono text-xs text-muted-foreground">{option.code}</span>
          <span className="truncate font-medium">{option.name_1 || '-'}</span>
        </div>
      </div>
    </button>
  )
}
