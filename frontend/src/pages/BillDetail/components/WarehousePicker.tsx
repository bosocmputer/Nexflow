import { type ReactNode, useEffect, useMemo, useRef, useState } from 'react'
import { Check, ChevronsUpDown, RefreshCw, Search } from 'lucide-react'
import { toast } from 'sonner'
import dayjs from 'dayjs'

import client from '@/api/client'
import { Button } from '@/components/ui/button'
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from '@/components/ui/popover'
import { cn } from '@/lib/utils'

export interface Warehouse {
  code: string
  name: string
  name_1?: string
  name_2?: string
  status?: number
}

export interface Shelf {
  code: string
  name: string
  name_1?: string
  name_2?: string
  whcode: string
  status?: number
}

interface WarehousePickerProps {
  value: string
  onChange: (warehouse: Warehouse) => void
  disabled?: boolean
}

interface ShelfPickerProps {
  warehouseCode: string
  value: string
  onChange: (shelf: Shelf) => void
  disabled?: boolean
}

export function WarehousePicker({ value, onChange, disabled }: WarehousePickerProps) {
  const [open, setOpen] = useState(false)
  const [query, setQuery] = useState('')
  const [results, setResults] = useState<Warehouse[]>([])
  const [loading, setLoading] = useState(false)
  const [refreshing, setRefreshing] = useState(false)
  const [lastSync, setLastSync] = useState<string | null>(null)
  const [total, setTotal] = useState(0)
  const [resolvedName, setResolvedName] = useState('')
  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  useEffect(() => {
    if (!value) { setResolvedName(''); return }
    client
      .get<{ data: Warehouse[] }>(`/api/sml/warehouses?search=${encodeURIComponent(value)}&limit=5`)
      .then((r) => {
        const found = r.data.data?.find((w) => w.code === value)
        setResolvedName(found?.name || found?.name_1 || found?.name_2 || '')
      })
      .catch(() => {})
  }, [value])

  const fetchResults = useMemo(
    () => (q: string) => {
      setLoading(true)
      client
        .get<{ data: Warehouse[]; warehouses: number; last_sync: string }>(
          `/api/sml/warehouses?search=${encodeURIComponent(q)}&limit=20`,
        )
        .then((r) => {
          setResults(r.data.data ?? [])
          setTotal(r.data.warehouses ?? 0)
          setLastSync(r.data.last_sync)
        })
        .catch(() => setResults([]))
        .finally(() => setLoading(false))
    },
    [],
  )

  useEffect(() => {
    if (!open) return
    fetchResults('')
  }, [open, fetchResults])

  useEffect(() => {
    if (!open) return
    if (debounceRef.current) clearTimeout(debounceRef.current)
    debounceRef.current = setTimeout(() => fetchResults(query), 250)
    return () => {
      if (debounceRef.current) clearTimeout(debounceRef.current)
    }
  }, [query, open, fetchResults])

  const handleRefresh = async () => {
    setRefreshing(true)
    try {
      const r = await client.post<{
        warehouses: number
        shelves: number
        last_sync: string
      }>('/api/sml/refresh-warehouses')
      setLastSync(r.data.last_sync)
      toast.success(`ซิงก์เสร็จ — ${r.data.warehouses} คลัง / ${r.data.shelves} พื้นที่เก็บ`)
      fetchResults(query)
    } catch (e: any) {
      toast.error('รีเฟรชคลังไม่สำเร็จ: ' + (e?.response?.data?.error ?? e?.message ?? 'unknown'))
    } finally {
      setRefreshing(false)
    }
  }

  return (
    <PickerShell
      open={open}
      onOpenChange={setOpen}
      value={value}
      valueLabel={value ? { code: value, name: resolvedName } : null}
      placeholder="เลือกคลัง…"
      searchPlaceholder="ค้นหาด้วยรหัส / ชื่อคลัง…"
      disabled={disabled}
      query={query}
      onQueryChange={setQuery}
      loading={loading}
      emptyText={query ? 'ไม่พบคลัง — ลองคำค้นอื่นหรือกดรีเฟรช' : 'ยังไม่มีคลังในแคช — กดรีเฟรช'}
      footer={`${total.toLocaleString()} คลัง${lastSync ? ` · ซิงก์ล่าสุด ${dayjs(lastSync).format('HH:mm')}` : ''}`}
      onRefresh={handleRefresh}
      refreshing={refreshing}
    >
      {results.map((w) => (
        <PickerRow
          key={w.code}
          code={w.code}
          name={w.name || w.name_1 || w.name_2 || ''}
          selected={value === w.code}
          onClick={() => {
            onChange(w)
            setOpen(false)
          }}
        />
      ))}
    </PickerShell>
  )
}

export function ShelfPicker({ warehouseCode, value, onChange, disabled }: ShelfPickerProps) {
  const [open, setOpen] = useState(false)
  const [query, setQuery] = useState('')
  const [results, setResults] = useState<Shelf[]>([])
  const [loading, setLoading] = useState(false)
  const [lastSync, setLastSync] = useState<string | null>(null)
  const [total, setTotal] = useState(0)
  const [resolvedName, setResolvedName] = useState('')
  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  useEffect(() => {
    if (!value || !warehouseCode) { setResolvedName(''); return }
    client
      .get<{ data: Shelf[] }>(
        `/api/sml/warehouses/${encodeURIComponent(warehouseCode)}/shelves?search=${encodeURIComponent(value)}&limit=5`,
      )
      .then((r) => {
        const found = r.data.data?.find((s) => s.code === value)
        setResolvedName(found?.name || found?.name_1 || found?.name_2 || '')
      })
      .catch(() => {})
  }, [value, warehouseCode])

  const fetchResults = useMemo(
    () => (q: string) => {
      if (!warehouseCode) return
      setLoading(true)
      client
        .get<{ data: Shelf[]; total: number; last_sync: string }>(
          `/api/sml/warehouses/${encodeURIComponent(warehouseCode)}/shelves?search=${encodeURIComponent(q)}&limit=50`,
        )
        .then((r) => {
          setResults(r.data.data ?? [])
          setTotal(r.data.total ?? 0)
          setLastSync(r.data.last_sync)
        })
        .catch(() => setResults([]))
        .finally(() => setLoading(false))
    },
    [warehouseCode],
  )

  useEffect(() => {
    if (!open) return
    fetchResults('')
  }, [open, fetchResults])

  useEffect(() => {
    if (!open) return
    if (debounceRef.current) clearTimeout(debounceRef.current)
    debounceRef.current = setTimeout(() => fetchResults(query), 250)
    return () => {
      if (debounceRef.current) clearTimeout(debounceRef.current)
    }
  }, [query, open, fetchResults])

  return (
    <PickerShell
      open={open}
      onOpenChange={setOpen}
      value={value}
      valueLabel={value ? { code: value, name: resolvedName } : null}
      placeholder={warehouseCode ? 'เลือกพื้นที่เก็บ…' : 'เลือกคลังก่อน'}
      searchPlaceholder="ค้นหาด้วยรหัส / ชื่อพื้นที่เก็บ…"
      disabled={disabled || !warehouseCode}
      query={query}
      onQueryChange={setQuery}
      loading={loading}
      emptyText={query ? 'ไม่พบพื้นที่เก็บ — ลองคำค้นอื่น' : 'คลังนี้ยังไม่มีพื้นที่เก็บในแคช'}
      footer={`${total.toLocaleString()} พื้นที่เก็บ${lastSync ? ` · ซิงก์ล่าสุด ${dayjs(lastSync).format('HH:mm')}` : ''}`}
    >
      {results.map((s) => (
        <PickerRow
          key={s.code}
          code={s.code}
          name={s.name || s.name_1 || s.name_2 || ''}
          selected={value === s.code}
          onClick={() => {
            onChange(s)
            setOpen(false)
          }}
        />
      ))}
    </PickerShell>
  )
}

function PickerShell({
  open,
  onOpenChange,
  value,
  valueLabel,
  placeholder,
  searchPlaceholder,
  disabled,
  query,
  onQueryChange,
  loading,
  emptyText,
  footer,
  onRefresh,
  refreshing,
  children,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  value: string
  valueLabel: { code: string; name?: string } | null
  placeholder: string
  searchPlaceholder: string
  disabled?: boolean
  query: string
  onQueryChange: (query: string) => void
  loading: boolean
  emptyText: string
  footer: string
  onRefresh?: () => void
  refreshing?: boolean
  children: ReactNode
}) {
  const hasChildren = Array.isArray(children) ? children.length > 0 : !!children
  return (
    <Popover modal open={open} onOpenChange={onOpenChange}>
      <PopoverTrigger asChild>
        <Button
          variant="outline"
          role="combobox"
          aria-expanded={open}
          className="w-full justify-between font-normal"
          disabled={disabled}
        >
          {valueLabel ? (
            <span className="flex items-center gap-2 truncate text-left">
              <span className="font-mono text-xs text-muted-foreground">{valueLabel.code}</span>
              {valueLabel.name && <span className="truncate">{valueLabel.name}</span>}
            </span>
          ) : value ? (
            <span className="font-mono text-xs">{value}</span>
          ) : (
            <span className="text-muted-foreground">{placeholder}</span>
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
            onChange={(e) => onQueryChange(e.target.value)}
            placeholder={searchPlaceholder}
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
          {!hasChildren && !loading ? (
            <div className="px-3 py-6 text-center text-sm text-muted-foreground">{emptyText}</div>
          ) : (
            children
          )}
        </div>
        <div className="flex items-center justify-between gap-2 border-t border-border bg-muted/30 px-3 py-2 text-xs text-muted-foreground">
          <span>{footer}</span>
          {onRefresh && (
            <Button
              variant="ghost"
              size="sm"
              className="h-7 gap-1.5 px-2 text-xs"
              onClick={onRefresh}
              disabled={refreshing}
            >
              <RefreshCw className={cn('h-3 w-3', refreshing && 'animate-spin')} />
              รีเฟรช
            </Button>
          )}
        </div>
      </PopoverContent>
    </Popover>
  )
}

function PickerRow({
  code,
  name,
  selected,
  onClick,
}: {
  code: string
  name: string
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
          <span className="font-mono text-xs text-muted-foreground">{code}</span>
          <span className="truncate font-medium">{name || '-'}</span>
        </div>
      </div>
    </button>
  )
}
