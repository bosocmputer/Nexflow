import { useMemo, useState, type ReactNode } from 'react'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { Skeleton } from '@/components/ui/skeleton'
import { cn } from '@/lib/utils'

export interface DataTableColumn<T> {
  key: string
  header: ReactNode
  cell: (row: T, index: number) => ReactNode
  className?: string
  headerClassName?: string
  width?: string
}

export interface DataTableProps<T> {
  columns: DataTableColumn<T>[]
  data: T[]
  loading?: boolean
  loadingRows?: number
  empty?: ReactNode
  onRowClick?: (row: T) => void
  getRowKey?: (row: T, index: number) => string
  rowClassName?: string | ((row: T) => string)
  className?: string
  dense?: boolean
  virtualize?: boolean
  virtualizeThreshold?: number
  virtualRowHeight?: number
  virtualMaxHeight?: number
}

export function DataTable<T>({
  columns,
  data,
  loading,
  loadingRows = 8,
  empty,
  onRowClick,
  getRowKey,
  rowClassName,
  className,
  dense,
  virtualize,
  virtualizeThreshold = 100,
  virtualRowHeight = dense ? 64 : 72,
  virtualMaxHeight = 640,
}: DataTableProps<T>) {
  const rowHeight = dense ? 'h-10' : 'h-12'
  const [scrollTop, setScrollTop] = useState(0)
  const useVirtualRows = !!virtualize && !loading && data.length >= virtualizeThreshold
  const overscan = 4
  const virtualWindow = useMemo(() => {
    if (!useVirtualRows) {
      return { rows: data, start: 0, end: data.length, top: 0, bottom: 0 }
    }
    const start = Math.max(0, Math.floor(scrollTop / virtualRowHeight) - overscan)
    const visibleCount = Math.ceil(virtualMaxHeight / virtualRowHeight) + overscan * 2
    const end = Math.min(data.length, start + visibleCount)
    return {
      rows: data.slice(start, end),
      start,
      end,
      top: start * virtualRowHeight,
      bottom: Math.max(0, (data.length - end) * virtualRowHeight),
    }
  }, [data, scrollTop, useVirtualRows, virtualMaxHeight, virtualRowHeight])

  const renderRow = (row: T, actualIndex: number) => {
    const dynClass =
      typeof rowClassName === 'function'
        ? rowClassName(row)
        : rowClassName
    return (
      <TableRow
        key={getRowKey ? getRowKey(row, actualIndex) : actualIndex}
        className={cn(
          rowHeight,
          onRowClick && 'cursor-pointer hover:bg-accent/30',
          dynClass,
        )}
        onClick={onRowClick ? () => onRowClick(row) : undefined}
      >
        {columns.map((col) => (
          <TableCell key={col.key} className={col.className}>
            {col.cell(row, actualIndex)}
          </TableCell>
        ))}
      </TableRow>
    )
  }

  return (
    <div
      className={cn(
        'rounded-xl border border-border/80 bg-card shadow-sm',
        useVirtualRows ? 'overflow-auto' : 'overflow-hidden',
        className,
      )}
      style={useVirtualRows ? { maxHeight: virtualMaxHeight } : undefined}
      onScroll={useVirtualRows ? (e) => setScrollTop(e.currentTarget.scrollTop) : undefined}
    >
      <Table>
        <TableHeader className={useVirtualRows ? 'sticky top-0 z-10' : undefined}>
          <TableRow className="bg-muted/50 hover:bg-muted/50">
            {columns.map((col) => (
              <TableHead
                key={col.key}
                className={cn('text-[11px] font-semibold uppercase tracking-wide text-muted-foreground', col.headerClassName)}
                style={col.width ? { width: col.width } : undefined}
              >
                {col.header}
              </TableHead>
            ))}
          </TableRow>
        </TableHeader>
        <TableBody>
          {loading ? (
            Array.from({ length: loadingRows }).map((_, i) => (
              <TableRow key={`sk-${i}`} className={rowHeight}>
                {columns.map((col) => (
                  <TableCell key={col.key} className={col.className}>
                    <Skeleton className="h-4 w-full max-w-[180px]" />
                  </TableCell>
                ))}
              </TableRow>
            ))
          ) : data.length === 0 ? (
            <TableRow>
              <TableCell
                colSpan={columns.length}
                className="py-12 text-center text-sm text-muted-foreground"
              >
                {empty ?? 'ไม่พบข้อมูล'}
              </TableCell>
            </TableRow>
          ) : (
            <>
              {useVirtualRows && virtualWindow.top > 0 && (
                <TableRow key="virtual-top-spacer" aria-hidden="true">
                  <TableCell colSpan={columns.length} className="p-0" style={{ height: virtualWindow.top }} />
                </TableRow>
              )}
              {virtualWindow.rows.map((row, i) => renderRow(row, virtualWindow.start + i))}
              {useVirtualRows && virtualWindow.bottom > 0 && (
                <TableRow key="virtual-bottom-spacer" aria-hidden="true">
                  <TableCell colSpan={columns.length} className="p-0" style={{ height: virtualWindow.bottom }} />
                </TableRow>
              )}
            </>
          )}
        </TableBody>
      </Table>
    </div>
  )
}
