import { useEffect, useMemo, useState } from 'react'
import { Link } from 'react-router-dom'
import {
  CheckCircle2,
  ChevronDown,
  CircleAlert,
  Info,
  Pencil,
} from 'lucide-react'
import dayjs from 'dayjs'
import { toast } from 'sonner'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from '@/components/ui/collapsible'
import { DataTable } from '@/components/common/DataTable'
import { PageHeader } from '@/components/common/PageHeader'
import client from '@/api/client'
import { ENABLE_CHAT, ENABLE_LAZADA_EXCEL, ENABLE_SALES_ORDERS, ENABLE_SHOPEE_EXCEL, ENABLE_SHOPEE_REALTIME_OPS, ENABLE_TIKTOK_EXCEL } from '@/lib/featureFlags'
import { cn } from '@/lib/utils'

import { EditDialog } from './ChannelDefaults/EditDialog'
import {
  CHANNEL_LABELS,
  destinationFor,
  type ChannelBillType,
  type ChannelDefaultRow,
  type ChannelKey,
} from './ChannelDefaults/labels'

const PHASE = Number(import.meta.env.VITE_PHASE ?? 99)

const PHASE1_CHANNEL_SLOTS: Array<{
  channel: ChannelKey
  bill_type: ChannelBillType
}> = [
  { channel: 'shopee_shipped', bill_type: 'purchase' },
]

const PHASE_PLUS_CHANNEL_SLOTS: Array<{
  channel: ChannelKey
  bill_type: ChannelBillType
}> = [
  ...(ENABLE_CHAT
    ? [{ channel: 'line' as ChannelKey, bill_type: 'sale' as const }]
    : []),
  { channel: 'shopee_shipped', bill_type: 'purchase' },
  ...(ENABLE_SHOPEE_EXCEL && ENABLE_SALES_ORDERS
    ? [{ channel: 'shopee' as ChannelKey, bill_type: 'sale' as const }]
    : []),
  ...(ENABLE_SHOPEE_REALTIME_OPS && ENABLE_SALES_ORDERS
    ? [{ channel: 'shopee_realtime' as ChannelKey, bill_type: 'sale' as const }]
    : []),
  ...(ENABLE_SHOPEE_REALTIME_OPS && ENABLE_SALES_ORDERS
    ? [{ channel: 'shopee_realtime_cancel' as ChannelKey, bill_type: 'sale' as const }]
    : []),
  ...(ENABLE_LAZADA_EXCEL && ENABLE_SALES_ORDERS
    ? [{ channel: 'lazada' as ChannelKey, bill_type: 'sale' as const }]
    : []),
  ...(ENABLE_TIKTOK_EXCEL && ENABLE_SALES_ORDERS
    ? [{ channel: 'tiktok' as ChannelKey, bill_type: 'sale' as const }]
    : []),
  ...(ENABLE_SHOPEE_EXCEL && ENABLE_SALES_ORDERS
    ? [{ channel: 'shopee_settlement' as ChannelKey, bill_type: 'ar_receipt' as const }]
    : []),
]

function visibleChannelSlots() {
  return PHASE < 2 ? PHASE1_CHANNEL_SLOTS : PHASE_PLUS_CHANNEL_SLOTS
}

function displayChannelLabel(row: Pick<ChannelDefaultRow, 'channel' | 'bill_type'>) {
  if (row.channel === 'shopee_shipped' && row.bill_type === 'purchase') {
    return 'Email บิลซื้อ Shopee'
  }
  return CHANNEL_LABELS[row.channel as ChannelKey] ?? row.channel
}

function workMenuFor(row: Pick<ChannelDefaultRow, 'channel' | 'bill_type' | 'endpoint' | 'doc_format_code'>) {
  if (row.channel === 'shopee_settlement' && row.bill_type === 'ar_receipt') {
    return { label: 'รับชำระหนี้', to: '/shopee-settlements' }
  }
  if (row.channel === 'shopee_realtime' && row.bill_type === 'sale') {
    return { label: 'คำสั่งซื้อ Shopee', to: '/shopee-operations' }
  }
  if (row.channel === 'shopee_realtime_cancel' && row.bill_type === 'sale') {
    return { label: 'คำสั่งซื้อ Shopee ที่ยกเลิก', to: '/shopee-operations?status_group=cancelled' }
  }
  if (row.channel === 'shopee' && row.bill_type === 'sale') {
    return { label: 'นำเข้า Shopee ย้อนหลัง', to: '/import/shopee' }
  }
  if (ENABLE_SALES_ORDERS && (row.channel === 'shopee' || row.channel === 'lazada' || row.channel === 'tiktok') && row.bill_type === 'sale') {
    const route = `${row.endpoint ?? ''} ${row.doc_format_code ?? ''}`.toLowerCase()
    if (route.includes('saleinvoice') || route.includes('sale-invoices') || row.doc_format_code?.toUpperCase() === 'SI') {
      return { label: 'ขายสินค้าและบริการ', to: '/sale-invoices' }
    }
    return { label: 'ใบสั่งขาย', to: '/sales-orders' }
  }
  if (row.channel === 'shopee_shipped' && row.bill_type === 'purchase') {
    return { label: 'ใบสั่งซื้อ', to: '/bills' }
  }
  return null
}

function channelPurpose(row: Pick<ChannelDefaultRow, 'channel' | 'bill_type'>) {
  if (row.channel === 'shopee_realtime' && row.bill_type === 'sale') {
    return 'งานหลักสำหรับ order ใหม่: รับจาก Push/Sync แล้วกดสร้างเอกสารใน Nexflow'
  }
  if (row.channel === 'shopee_realtime_cancel' && row.bill_type === 'sale') {
    return 'งานยกเลิกหลังส่ง SML: สร้างเอกสารขาย -> ยกเลิกขายสินค้าและบริการ'
  }
  if (row.channel === 'shopee' && row.bill_type === 'sale') {
    return 'งานสำรองเท่านั้น: ดึงย้อนหลัง ซ่อม order ตกหล่น หรือ Excel/API fallback'
  }
  if (row.channel === 'shopee_settlement' && row.bill_type === 'ar_receipt') {
    return 'รอบถอนเงิน Shopee สำหรับรับชำระหนี้'
  }
  if (row.channel === 'shopee_shipped' && row.bill_type === 'purchase') {
    return 'อีเมล Shopee Shipped สำหรับสร้างใบสั่งซื้อ'
  }
  return ''
}

function channelModeBadge(row: Pick<ChannelDefaultRow, 'channel' | 'bill_type'>) {
  if (row.channel === 'shopee_realtime' && row.bill_type === 'sale') {
    return { label: 'งานหลัก', className: 'border-success/30 bg-success/10 text-success' }
  }
  if (row.channel === 'shopee_realtime_cancel' && row.bill_type === 'sale') {
    return { label: 'หลังยกเลิก', className: 'border-destructive/30 bg-destructive/10 text-destructive' }
  }
  if (row.channel === 'shopee' && row.bill_type === 'sale') {
    return { label: 'สำรอง/ย้อนหลัง', className: 'border-warning/30 bg-warning/10 text-warning' }
  }
  return null
}

function EndpointCell({ row }: { row: ChannelDefaultRow }) {
  const [open, setOpen] = useState(false)
  const destination = destinationFor(
    row.channel as ChannelKey,
    row.bill_type,
    row.endpoint ?? '',
    row.doc_format_code ?? '',
  )
  const context =
    row.channel === 'shopee_realtime'
      ? 'ใช้เมื่อกดสร้างเอกสารจากคำสั่งซื้อ Shopee'
      : row.channel === 'shopee_realtime_cancel'
        ? 'ใช้เมื่อ Shopee ยกเลิก order หลังส่งใบขายเข้า SML แล้ว'
      : row.channel === 'shopee'
        ? 'ใช้เฉพาะเมื่อนำเข้าย้อนหลังหรือซ่อม order ตกหล่น'
        : ''

  return (
    <div className="min-w-[220px] space-y-1">
      <div className="flex items-center gap-1.5">
        <span className="text-xs font-medium text-foreground">
          {destination?.label ?? 'ยังไม่ตั้งปลายทาง'}
        </span>
      </div>
      {context && (
        <div className="max-w-[260px] text-[11px] leading-4 text-muted-foreground">
          {context}
        </div>
      )}
      <button
        type="button"
        onClick={(e) => {
          e.stopPropagation()
          setOpen((v) => !v)
        }}
        className="inline-flex items-center gap-1 text-[11px] text-muted-foreground hover:text-foreground"
      >
        <ChevronDown className={cn('h-3 w-3 transition-transform', open && 'rotate-180')} />
        {open ? 'ซ่อนรายละเอียดขั้นสูง' : 'รายละเอียดขั้นสูง'}
      </button>
      {open && destination && (
        <div className="rounded-md border border-border/70 bg-muted/35 px-2 py-1.5">
          <div className="text-[10px] font-medium uppercase text-muted-foreground">
            API path
          </div>
          <code className="mt-0.5 block break-all text-[10px] leading-4 text-muted-foreground">
            {destination.apiPath}
          </code>
        </div>
      )}
    </div>
  )
}

function HelpBanner() {
  const [open, setOpen] = useState(false)
  return (
    <Card className="border-border/70 bg-muted/20 shadow-none">
      <Collapsible open={open} onOpenChange={setOpen}>
        <CollapsibleTrigger className="flex w-full items-center gap-2 px-4 py-2.5 text-left text-sm font-medium text-foreground hover:bg-muted/45">
          <Info className="h-4 w-4 text-accent-strong" />
          <span>รายละเอียดสำหรับแอดมิน</span>
          <span className="hidden text-xs font-normal text-muted-foreground sm:inline">
            endpoint, doc format และเลขเอกสาร SML
          </span>
          <ChevronDown
            className={cn(
              'ml-auto h-4 w-4 text-muted-foreground transition-transform',
              open && 'rotate-180',
            )}
          />
        </CollapsibleTrigger>
        <CollapsibleContent>
          <CardContent className="grid gap-2 border-t border-border px-4 py-3 text-xs leading-relaxed text-muted-foreground md:grid-cols-3">
            <div>
              <span className="font-medium text-foreground">ปลายทาง SML:</span> เลือกเมนูที่เอกสารจะถูกส่งเข้า เช่น ขายสินค้าและบริการ หรือใบสั่งซื้อ
            </div>
            <div>
              <span className="font-medium text-foreground">รหัสเอกสาร:</span> ใช้ doc_format_code จาก SML และใช้เลขรันแบบที่ SML แสดงเอกสารได้จริง
            </div>
            <div>
              <span className="font-medium text-foreground">ค่าเริ่มต้น:</span> ลูกค้า คลัง VAT และบัญชีรับเงินจะถูกเติมใน dialog ก่อนส่งจริง
            </div>
          </CardContent>
        </CollapsibleContent>
      </Collapsible>
    </Card>
  )
}

export default function ChannelDefaults() {
  const [rows, setRows] = useState<ChannelDefaultRow[]>([])
  const [loading, setLoading] = useState(true)
  const [editing, setEditing] = useState<ChannelDefaultRow | null>(null)
  const [editOpen, setEditOpen] = useState(false)

  const load = async () => {
    setLoading(true)
    try {
      const r = await client.get<{ data: ChannelDefaultRow[] }>(
        '/api/settings/channel-defaults',
      )
      setRows(r.data.data ?? [])
    } catch (e: any) {
      toast.error('โหลดข้อมูลไม่สำเร็จ: ' + (e?.message ?? 'unknown'))
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    load()
  }, [])

  // Merge DB rows with the static slot list so unset channels show up too
  // (instead of "no data" — we need to nudge admin to set them).
  const tableRows = useMemo(() => {
    const slots = visibleChannelSlots()
    const byKey = new Map<string, ChannelDefaultRow>()
    for (const r of rows) {
      byKey.set(`${r.channel}/${r.bill_type}`, r)
    }
    return slots.map((slot) => {
      const existing = byKey.get(`${slot.channel}/${slot.bill_type}`)
      if (existing) return existing
      return {
        channel: slot.channel,
        bill_type: slot.bill_type,
        party_code: '',
        party_name: '',
        party_phone: '',
        party_address: '',
        party_tax_id: '',
        doc_format_code: '',
        endpoint: '',
        doc_prefix: '',
        doc_running_format: '',
        branch_code: '',
        sale_code: '',
        unit_code: '',
        doc_time: '',
        shipping_item_enabled: false,
        shipping_item_code: '',
        shipping_item_unit_code: '',
        passbook_code: '',
        passbook_name: '',
        bank_code: '',
        bank_branch: '',
        expense_code: '',
        expense_name: '',
        wh_code: '',
        shelf_code: '',
        vat_type: -1,
        vat_rate: -1,
        inquiry_type: -1,
      } satisfies ChannelDefaultRow
    })
  }, [rows])

  const isRouteUnset = (r: ChannelDefaultRow) => {
    if (r.channel === 'shopee_settlement' && r.bill_type === 'ar_receipt') {
      return !r.endpoint || !r.doc_format_code || !r.passbook_code
    }
    return !r.endpoint || !r.doc_format_code || !r.doc_prefix || !r.doc_running_format
  }

  const unsetRoutes = tableRows.filter(isRouteUnset)
  const saleInvoiceRoute = tableRows.find((r) => (
    r.bill_type === 'sale' &&
    (r.channel === 'shopee' || r.channel === 'shopee_realtime' || r.channel === 'lazada' || r.channel === 'tiktok') &&
    `${r.endpoint ?? ''} ${r.doc_format_code ?? ''}`.toLowerCase().includes('saleinvoice')
  ))
  const settlementRoute = tableRows.find((r) => r.channel === 'shopee_settlement' && r.bill_type === 'ar_receipt')

  const configSummary = (r: ChannelDefaultRow) => {
    if (r.channel === 'shopee_settlement' && r.bill_type === 'ar_receipt') {
      return (
        <div className="space-y-0.5 text-xs">
          <div className={r.passbook_code ? 'text-foreground' : 'text-warning'}>
            บัญชีรับเงิน: {r.passbook_code ? `${r.passbook_code}${r.passbook_name ? ` · ${r.passbook_name}` : ''}` : 'ยังไม่ตั้งค่า'}
          </div>
          <div className={r.expense_code ? 'text-muted-foreground' : 'text-muted-foreground'}>
            ส่วนต่าง Shopee: {r.expense_code ? `${r.expense_code}${r.expense_name ? ` · ${r.expense_name}` : ''}` : 'ยังไม่ตั้งค่า'}
          </div>
        </div>
      )
    }
    return (
      <span className="text-xs text-muted-foreground">
        ค่า wh/shelf/VAT เลือกใน dialog ส่ง SML
      </span>
    )
  }

  return (
    <div className="space-y-5">
      <PageHeader
        title="เส้นทางเอกสาร SML"
        description={
          PHASE < 2
            ? ENABLE_SHOPEE_EXCEL && ENABLE_SALES_ORDERS
              ? 'ตรวจความพร้อมเส้นทางขายหลักและบิลซื้อก่อนส่งเอกสารเข้า SML'
              : 'ตรวจความพร้อมเส้นทางบิลซื้อ Shopee ก่อนส่งเข้า SML'
            : 'ตรวจว่าแต่ละช่องทางพร้อมส่งเข้าเมนู SML ที่ถูกต้องหรือยัง'
        }
      />

      <HelpBanner />

      <Card className="border-border/70 bg-card shadow-none">
        <CardContent className="space-y-3 p-4">
          <div className="flex flex-col gap-3 md:flex-row md:items-start md:justify-between">
            <div className="flex min-w-0 items-start gap-3">
            {unsetRoutes.length > 0 ? (
              <CircleAlert className="mt-0.5 h-4 w-4 shrink-0 text-warning" />
            ) : (
              <CheckCircle2 className="mt-0.5 h-4 w-4 shrink-0 text-success" />
            )}
              <div className="min-w-0">
                <p className="text-sm font-semibold text-foreground">
                  {unsetRoutes.length > 0 ? 'ยังมีเส้นทางที่ต้องตั้งค่าก่อนใช้งานจริง' : 'เส้นทางเอกสารพร้อมใช้งาน'}
                </p>
                <p className="mt-1 text-xs leading-relaxed text-muted-foreground">
                  งานขายหลักควรชี้ไปขายสินค้าและบริการ / SI ก่อนเริ่ม import หรือส่งเข้า SML
                </p>
              </div>
            </div>
            <div className="flex flex-wrap gap-1.5">
              <Badge variant="outline" className="bg-background text-xs">
                พร้อม {tableRows.length - unsetRoutes.length}/{tableRows.length}
              </Badge>
              {unsetRoutes.slice(0, 3).map((route) => (
                <Badge key={`${route.channel}/${route.bill_type}`} variant="secondary" className="text-xs">
                  ต้องตั้งค่า: {displayChannelLabel(route)}
                </Badge>
              ))}
            </div>
          </div>
          <div className="grid gap-2 text-xs md:grid-cols-2">
            <div className={cn(
              'rounded-md border px-3 py-2',
              saleInvoiceRoute ? 'border-success/25 bg-success/[0.04]' : 'border-warning/30 bg-warning/[0.06]',
            )}>
              <div className="font-medium text-foreground">เส้นทางขายหลัก</div>
              <div className="mt-0.5 text-muted-foreground">
                Marketplace / Shopee → ขายสินค้าและบริการ
                <span className="ml-1 font-mono text-foreground">
                  {saleInvoiceRoute?.doc_format_code || 'SI'}
                </span>
              </div>
            </div>
            <div className={cn(
              'rounded-md border px-3 py-2',
              settlementRoute && !isRouteUnset(settlementRoute) ? 'border-success/25 bg-success/[0.04]' : 'border-warning/30 bg-warning/[0.06]',
            )}>
              <div className="font-medium text-foreground">รับชำระ Shopee</div>
              <div className="mt-0.5 text-muted-foreground">
                ลูกหนี้ → รับชำระหนี้
                <span className="ml-1 font-mono text-foreground">
                  {settlementRoute?.doc_format_code || 'RC'}
                </span>
              </div>
            </div>
          </div>
        </CardContent>
      </Card>

      <DataTable<ChannelDefaultRow>
        data={tableRows}
        loading={loading}
        empty="ยังไม่มี channel ที่ตั้งค่า"
        columns={[
          {
            key: 'channel',
            header: 'ช่องทาง / เมนูที่ใช้',
            cell: (r) => {
              const menu = workMenuFor(r)
              const purpose = channelPurpose(r)
              const mode = channelModeBadge(r)
              return (
                <div className="min-w-[190px] space-y-1">
                  <div className="flex flex-wrap items-center gap-1.5">
                    <span className="font-medium text-foreground">
                      {displayChannelLabel(r)}
                    </span>
                    <Badge
                      variant="secondary"
                      className={cn(
                        'h-5 px-1.5 text-[10px] font-medium',
                        r.bill_type === 'purchase'
                          ? 'bg-warning/15 text-warning hover:bg-warning/20'
                          : r.bill_type === 'ar_receipt'
                            ? 'bg-success/15 text-success hover:bg-success/20'
                        : 'bg-info/15 text-info hover:bg-info/20',
                      )}
                    >
                      {r.bill_type === 'purchase' ? 'บิลซื้อ' : r.bill_type === 'ar_receipt' ? 'รับชำระ' : 'บิลขาย'}
                    </Badge>
                    {mode && (
                      <Badge variant="outline" className={cn('h-5 px-1.5 text-[10px] font-medium', mode.className)}>
                        {mode.label}
                      </Badge>
                    )}
                  </div>
                  {purpose && (
                    <div className="max-w-[260px] text-xs leading-5 text-muted-foreground">
                      {purpose}
                    </div>
                  )}
                  {menu ? (
                    <Link to={menu.to} className="text-xs font-medium text-link hover:underline">
                      ไปหน้า {menu.label}
                    </Link>
                  ) : (
                    <span className="text-xs text-muted-foreground">ไม่มีคิวงานประจำ</span>
                  )}
                </div>
              )
            },
          },
          {
            key: 'endpoint',
            header: 'เอกสารที่จะสร้างใน SML',
            cell: (r) => (
              <div className="min-w-[240px] space-y-2">
                <EndpointCell row={r} />
                <div className="flex flex-wrap items-center gap-1.5 text-xs">
                  <span className="text-muted-foreground">รหัสเอกสาร</span>
                  {r.doc_format_code ? (
                    <span className="rounded bg-muted px-1.5 py-0.5 font-mono font-medium text-foreground">
                      {r.doc_format_code}
                    </span>
                  ) : (
                    <span className="text-warning">ยังไม่ตั้ง</span>
                  )}
                </div>
              </div>
            ),
          },
          {
            key: 'readiness',
            header: 'ความพร้อม',
            cell: (r) => {
              const unset = isRouteUnset(r)
              return (
                <div className="min-w-[190px] space-y-1">
                  <Badge
                    variant={unset ? 'secondary' : 'outline'}
                    className={cn(
                      'h-6 px-2 text-xs',
                      unset
                        ? 'bg-warning/15 text-warning hover:bg-warning/20'
                        : 'border-success/30 bg-success/[0.04] text-success',
                    )}
                  >
                    {unset ? 'ต้องตั้งค่าก่อนใช้จริง' : 'พร้อมใช้'}
                  </Badge>
                  <div>{configSummary(r)}</div>
                </div>
              )
            },
          },
          {
            key: 'updated',
            header: 'อัปเดต',
            cell: (r) =>
              r.updated_at ? (
                <span className="text-xs tabular-nums text-muted-foreground">
                  {dayjs(r.updated_at).format('DD/MM/YY HH:mm')}
                </span>
              ) : (
                <span className="text-xs text-muted-foreground">—</span>
              ),
          },
          {
            key: 'actions',
            header: '',
            headerClassName: 'text-right',
            className: 'text-right',
            cell: (r) => (
              <div className="flex items-center justify-end gap-1.5">
                <Button
                  variant="outline"
                  size="sm"
                  className="h-7 gap-1 px-2.5 text-xs"
                  onClick={(e) => {
                    e.stopPropagation()
                    setEditing(r)
                    setEditOpen(true)
                  }}
                  title="แก้ไขปลายทาง SML, รหัสเอกสาร, prefix และเลขรัน"
                >
                  <Pencil className="h-3.5 w-3.5" />
                  {isRouteUnset(r) ? 'ตั้งค่า' : 'แก้ไข'}
                </Button>
              </div>
            ),
          },
        ]}
      />

      <EditDialog
        open={editOpen}
        onOpenChange={setEditOpen}
        row={editing}
        onSaved={load}
      />
    </div>
  )
}
