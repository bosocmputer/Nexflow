import { useEffect, useMemo, useState } from 'react'
import { Link } from 'react-router-dom'
import {
  ChevronDown,
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
import { ENABLE_CHAT, ENABLE_LAZADA_EXCEL, ENABLE_SALES_ORDERS, ENABLE_SHOPEE_EXCEL, ENABLE_TIKTOK_EXCEL } from '@/lib/featureFlags'
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

function EndpointCell({ row }: { row: ChannelDefaultRow }) {
  const [open, setOpen] = useState(false)
  const destination = destinationFor(
    row.channel as ChannelKey,
    row.bill_type,
    row.endpoint ?? '',
    row.doc_format_code ?? '',
  )

  return (
    <div className="min-w-[220px] space-y-1">
      <div className="flex items-center gap-1.5">
        <span className="text-xs font-medium text-foreground">
          {destination?.label ?? 'ยังไม่ตั้งปลายทาง'}
        </span>
      </div>
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
  const [open, setOpen] = useState(true)
  const phase1 = PHASE < 2
  const enabledSourceLabels = [
    ENABLE_CHAT ? 'LINE' : '',
    'Shopee',
    ENABLE_LAZADA_EXCEL && ENABLE_SALES_ORDERS ? 'Lazada' : '',
    ENABLE_TIKTOK_EXCEL && ENABLE_SALES_ORDERS ? 'TikTok' : '',
  ].filter(Boolean).join(' / ')
  return (
    <Card className="border-info/30 bg-info/5">
      <Collapsible open={open} onOpenChange={setOpen}>
        <CollapsibleTrigger className="flex w-full items-center gap-2 px-4 py-3 text-left text-sm font-medium text-foreground hover:bg-info/10">
          <Info className="h-4 w-4 text-info" />
          <span>{phase1 ? 'ตั้งค่าเส้นทางเอกสาร SML สำหรับ Phase 1' : 'หน้านี้ใช้ทำอะไร — อ่านก่อนตั้งค่า'}</span>
          <ChevronDown
            className={cn(
              'ml-auto h-4 w-4 text-muted-foreground transition-transform',
              open && 'rotate-180',
            )}
          />
        </CollapsibleTrigger>
        <CollapsibleContent>
          <CardContent className="space-y-3 border-t border-info/20 px-4 pt-3 text-sm">
            {phase1 ? (
              <p className="text-muted-foreground">
                ใน Phase 1 หน้านี้เปิดเฉพาะเส้นทางที่พร้อมใช้งานก่อน ได้แก่{' '}
                <b>Email Shopee → บิลซื้อ</b>{ENABLE_SHOPEE_EXCEL && ENABLE_SALES_ORDERS ? <> และ <b>Shopee → บิลขาย</b></> : null}{ENABLE_LAZADA_EXCEL && ENABLE_SALES_ORDERS ? <> และ <b>Lazada Excel → บิลขาย</b></> : null}{ENABLE_TIKTOK_EXCEL && ENABLE_SALES_ORDERS ? <> และ <b>TikTok Excel → บิลขาย</b></> : null}.
                ใช้กำหนดปลายทางที่จะส่งเอกสารเข้า SML ได้แก่ เมนู SML รหัสประเภทเอกสาร
                และรูปแบบเลขเอกสาร. ส่วนคู่ค้า คลัง พื้นที่เก็บ และภาษี จะเลือกในขั้นตอนส่งบิล.
              </p>
            ) : (
              <p className="text-muted-foreground">
                บิลทุกใบที่ระบบรับเข้ามา ({enabledSourceLabels}) สุดท้ายต้องส่งเข้า{' '}
                <b>SML ERP</b> เพื่อบันทึก. หน้านี้กำหนดว่า <b>"แต่ละช่องทาง"</b> จะ:
              </p>
            )}
            <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
              <div className="rounded-md border border-border bg-card p-3">
                <div className="mb-1 text-sm font-semibold">1. ส่งเข้าเมนูไหนใน SML</div>
                <p className="text-xs text-muted-foreground">
                  {phase1
                    ? ENABLE_SHOPEE_EXCEL && ENABLE_SALES_ORDERS
                      ? <>Phase 1 เปิดปลายทาง {'ซื้อ -> ใบสั่งซื้อ'}, {'ขาย -> ใบสั่งขาย'} และ {'ขาย -> ขายสินค้าและบริการ'} เพื่อให้เอกสารถูกบันทึกในเมนูที่สัมพันธ์กับประเภทบิล</>
                      : <>Phase 1 เปิดปลายทาง {'ซื้อ -> ใบสั่งซื้อ'} สำหรับบิลซื้อจากอีเมลก่อน</>
                    : <>เลือกปลายทางของบิล เช่น {'ซื้อ -> ใบสั่งซื้อ'} หรือ {'ขาย -> ขายสินค้าและบริการ'} เพื่อให้เอกสารถูกบันทึกในเมนูที่ถูกต้อง</>}
                </p>
              </div>
              <div className="rounded-md border border-border bg-card p-3">
                <div className="mb-1 text-sm font-semibold">2. รหัสประเภทเอกสาร</div>
                <p className="text-xs text-muted-foreground">
                รหัสที่ SML ใช้แยกชนิดเอกสาร เช่น <code>PO</code> สำหรับใบสั่งซื้อ หรือ <code>SR</code> สำหรับใบสั่งขาย.
                  ตั้งให้ตรงกับระบบ SML
                </p>
              </div>
              <div className="rounded-md border border-border bg-card p-3">
                <div className="mb-1 text-sm font-semibold">3. เลขเอกสาร</div>
                <p className="text-xs text-muted-foreground">
                  {phase1
                    ? 'ตั้ง prefix และรูปแบบเลขรัน doc_no ของช่องทางนี้ ค่าประจำบิลอื่นจะอยู่ใน dialog ก่อนส่ง SML'
                    : 'ตั้ง prefix และรูปแบบเลขรัน doc_no ของช่องทางนี้ ค่าประจำบิลอื่นจะเลือกตอนส่ง SML'}
                </p>
              </div>
            </div>
            {phase1 ? (
              <p className="rounded-md bg-info/10 px-3 py-2 text-xs text-info">
                หน้าเส้นทางเอกสาร SML เก็บเฉพาะปลายทาง SML, doc_format_code และ doc_no.
                ตอนส่งบิลจริงระบบจะใช้ค่าที่สัมพันธ์กับประเภทบิล และเปิด dialog ให้กรอกค่าที่ต้องเลือกเฉพาะบิลนั้น.
              </p>
            ) : (
              <p className="rounded-md bg-warning/10 px-3 py-2 text-xs text-warning">
                <b>ถ้าตั้งปลายทางหรือเลขเอกสารไม่ครบ:</b> ระบบจะยังไม่รู้ว่าจะส่งเอกสารเข้าเมนู SML ไหน
                หรือควรออกเลขเอกสารรูปแบบใด. ค่าประจำบิลจะเลือกใน dialog ตอนส่ง SML.
              </p>
            )}
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
              ? 'เลือกปลายทาง SML, doc_format_code และรูปแบบเลขเอกสารสำหรับบิลซื้อ Shopee และบิลขาย Marketplace Excel'
              : 'เลือกปลายทาง SML, doc_format_code และรูปแบบเลขเอกสารสำหรับบิลซื้อ Shopee'
            : 'กำหนดว่าแต่ละช่องทางจะส่งบิลเข้าเมนูไหนใน SML พร้อมรหัสเอกสารและรูปแบบเลขเอกสาร'
        }
      />

      <HelpBanner />

      <DataTable<ChannelDefaultRow>
        data={tableRows}
        loading={loading}
        empty="ยังไม่มี channel ที่ตั้งค่า"
        columns={[
          {
            key: 'channel',
            header: 'ช่องทาง',
            cell: (r) => (
              <span className="font-medium">
                {displayChannelLabel(r)}
              </span>
            ),
          },
          {
            key: 'bill_type',
            header: 'ประเภท',
            cell: (r) => (
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
                {r.bill_type === 'purchase' ? 'บิลซื้อ' : r.bill_type === 'ar_receipt' ? 'ลูกหนี้' : 'บิลขาย'}
              </Badge>
            ),
          },
          {
            key: 'endpoint',
            header: 'ส่งเข้า SML',
            cell: (r) => <EndpointCell row={r} />,
          },
          {
            key: 'work_menu',
            header: 'เมนูงาน',
            cell: (r) => {
              const menu = workMenuFor(r)
              if (!menu) return <span className="text-xs text-muted-foreground">—</span>
              return (
                <Link to={menu.to} className="text-xs font-medium text-primary hover:underline">
                  {menu.label}
                </Link>
              )
            },
          },
          {
            key: 'doc_format',
            header: 'รหัสเอกสาร',
            cell: (r) => {
              return r.doc_format_code ? (
                <span className="font-mono text-xs font-medium text-foreground">
                  {r.doc_format_code}
                </span>
              ) : (
                <span className="text-xs text-warning">ยังไม่ตั้ง</span>
              )
            },
          },
          {
            key: 'config_summary',
            header: 'ค่าใช้งาน',
            cell: (r) => configSummary(r),
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
