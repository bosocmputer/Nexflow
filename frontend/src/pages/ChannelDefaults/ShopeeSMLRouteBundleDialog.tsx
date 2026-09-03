import { useCallback, useEffect, useMemo, useState } from 'react'
import { AlertCircle, CheckCircle2, PackageSearch, RefreshCw } from 'lucide-react'
import { toast } from 'sonner'

import client from '@/api/client'
import { UnitSelect } from '@/components/common/UnitSelect'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import { validateProfileText } from '@/lib/smlDocumentProfile.js'
import type { CatalogMatch } from '@/types'
import { MapItemModal } from '../BillDetail/components/MapItemModal'
import { SMLMasterCodePicker } from '../BillDetail/components/SMLMasterCodePicker'
import { ShelfPicker, WarehousePicker } from '../BillDetail/components/WarehousePicker'
import {
  SML_DESTINATION_OPTIONS,
  type ChannelDefaultRow,
  type EndpointKind,
  type SmlDestinationOption,
} from './labels'
import { PartyPicker, type Party } from './PartyPicker'

type MainRoute = 'saleinvoice' | 'saleorder'
type CancellationRoute = 'saleinvoicecancel' | 'creditnote' | 'saleordercancel'

interface SmlDocFormat {
  code: string
  name_1: string
  format: string
}

interface BundleReadiness {
  configured: boolean
  pair_compatible: boolean
  capability_compatible: boolean
  automation_ready: boolean
}

interface BundleCapability {
  compatible: boolean
  revision: string
  message: string
}

interface BundleResponse {
  main_route: MainRoute | ''
  cancellation_route: CancellationRoute | ''
  main: ChannelDefaultRow | null
  cancellation: ChannelDefaultRow | null
  main_config_version: number
  cancel_config_version: number
  route_modes: Record<string, string>
  capability: BundleCapability
  readiness: BundleReadiness
}

interface BundlePreview {
  preview_token: string
  expires_at: string
  main_route: MainRoute
  cancellation_route: CancellationRoute
  route_modes: Record<string, string>
  readiness: BundleReadiness
  warnings: string[]
}

interface Props {
  open: boolean
  onOpenChange: (open: boolean) => void
  onSaved: () => void
}

const mainOptions = SML_DESTINATION_OPTIONS.filter((option) =>
  option.value === 'saleinvoice' || option.value === 'saleorder',
)

function routeOption(route: EndpointKind): SmlDestinationOption {
  const option = SML_DESTINATION_OPTIONS.find((item) => item.value === route)
  if (!option) throw new Error(`missing SML route option: ${route}`)
  return option
}

function emptyRoute(option: SmlDestinationOption, channel: string): ChannelDefaultRow {
  return {
    channel,
    bill_type: 'sale',
    party_code: '',
    party_name: '',
    party_phone: '',
    party_address: '',
    party_tax_id: '',
    doc_format_code: option.docFormatCode,
    endpoint: option.apiPath,
    doc_prefix: option.docPrefix,
    doc_running_format: option.docRunningFormat,
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
    inquiry_type: 0,
    remark: '',
    remark_2: '',
    config_version: 0,
  }
}

function modeLabel(mode: string) {
  if (mode === 'active') return 'พร้อมใช้งานจริง'
  if (mode === 'shadow') return 'ตรวจสอบก่อนเปิดจริง'
  return 'ปิดอยู่'
}

function runningFormat(format: string) {
  return format.replace(/^@/, '')
}

export function ShopeeSMLRouteBundleDialog({ open, onOpenChange, onSaved }: Props) {
  const [loading, setLoading] = useState(false)
  const [loadError, setLoadError] = useState('')
  const [loadedRevision, setLoadedRevision] = useState(0)
  const [bundle, setBundle] = useState<BundleResponse | null>(null)
  const [mainRoute, setMainRoute] = useState<MainRoute>('saleinvoice')
  const [cancellationRoute, setCancellationRoute] = useState<CancellationRoute>('creditnote')
  const [main, setMain] = useState<ChannelDefaultRow | null>(null)
  const [cancellation, setCancellation] = useState<ChannelDefaultRow | null>(null)
  const [party, setParty] = useState<Party | null>(null)
  const [manualWarehouse, setManualWarehouse] = useState(false)
  const [mainFormats, setMainFormats] = useState<SmlDocFormat[]>([])
  const [cancelFormats, setCancelFormats] = useState<SmlDocFormat[]>([])
  const [mainFormatsLoading, setMainFormatsLoading] = useState(false)
  const [cancelFormatsLoading, setCancelFormatsLoading] = useState(false)
  const [formatError, setFormatError] = useState('')
  const [dirty, setDirty] = useState(false)
  const [previewing, setPreviewing] = useState(false)
  const [preview, setPreview] = useState<BundlePreview | null>(null)
  const [saving, setSaving] = useState(false)
  const [shippingPickerOpen, setShippingPickerOpen] = useState(false)
  const [shippingItemName, setShippingItemName] = useState('')

  const markDirty = useCallback(() => {
    setDirty(true)
    setPreview(null)
  }, [])

  const loadBundle = useCallback(async () => {
    setLoading(true)
    setLoadError('')
    setPreview(null)
    try {
      const response = await client.get<BundleResponse>('/api/settings/shopee-sml-route-bundle')
      const data = response.data
      const nextMainRoute: MainRoute = data.main_route === 'saleorder' ? 'saleorder' : 'saleinvoice'
      const compatibleCancel: CancellationRoute = nextMainRoute === 'saleorder'
        ? 'saleordercancel'
        : data.cancellation_route === 'saleinvoicecancel'
          ? 'saleinvoicecancel'
          : 'creditnote'
      const mainOption = routeOption(nextMainRoute)
      const cancelOption = routeOption(compatibleCancel)
      const nextMain = data.main ?? emptyRoute(mainOption, 'shopee_realtime')
      const nextCancel = data.cancellation ?? emptyRoute(cancelOption, 'shopee_realtime_cancel')
      setBundle(data)
      setMainRoute(nextMainRoute)
      setCancellationRoute(compatibleCancel)
      setMain({ ...nextMain, endpoint: mainOption.apiPath })
      setCancellation({ ...nextCancel, endpoint: cancelOption.apiPath })
      setParty(nextMain.party_code ? { code: nextMain.party_code, name: nextMain.party_name || nextMain.party_code } : null)
      setDirty(false)
      setLoadedRevision((value) => value + 1)
    } catch (error: any) {
      setLoadError(error?.response?.data?.error ?? 'โหลดการตั้งค่าไม่สำเร็จ กรุณาลองใหม่')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    if (open) void loadBundle()
  }, [open, loadBundle])

  useEffect(() => {
    if (!open || !main || loadedRevision === 0) return
    let cancelled = false
    const option = routeOption(mainRoute)
    setMainFormatsLoading(true)
    setFormatError('')
    client.get<{ data: SmlDocFormat[] }>(`/api/sml/doc-formats?screen_code=${option.screenCode}`)
      .then((response) => {
        if (cancelled) return
        const formats = response.data.data ?? []
        setMainFormats(formats)
        const selected = formats.find((format) => format.code === main.doc_format_code) ?? formats[0]
        if (selected && selected.code !== main.doc_format_code) {
          setMain((value) => value ? {
            ...value,
            doc_format_code: selected.code,
            doc_prefix: selected.code,
            doc_running_format: runningFormat(selected.format),
          } : value)
          markDirty()
        }
      })
      .catch(() => {
        if (!cancelled) {
          setMainFormats([])
          setFormatError('โหลดรูปแบบเอกสารจาก SML ไม่สำเร็จ')
        }
      })
      .finally(() => {
        if (!cancelled) setMainFormatsLoading(false)
      })
    return () => { cancelled = true }
    // loadedRevision deliberately reloads formats after each fresh bundle GET.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, mainRoute, loadedRevision])

  useEffect(() => {
    if (!open || !cancellation || loadedRevision === 0) return
    let cancelled = false
    const option = routeOption(cancellationRoute)
    setCancelFormatsLoading(true)
    setFormatError('')
    client.get<{ data: SmlDocFormat[] }>(`/api/sml/doc-formats?screen_code=${option.screenCode}`)
      .then((response) => {
        if (cancelled) return
        const formats = response.data.data ?? []
        setCancelFormats(formats)
        const selected = formats.find((format) => format.code === cancellation.doc_format_code) ?? formats[0]
        if (selected && selected.code !== cancellation.doc_format_code) {
          setCancellation((value) => value ? {
            ...value,
            doc_format_code: selected.code,
            doc_prefix: selected.code,
            doc_running_format: runningFormat(selected.format),
          } : value)
          markDirty()
        }
      })
      .catch(() => {
        if (!cancelled) {
          setCancelFormats([])
          setFormatError('โหลดรูปแบบเอกสารจาก SML ไม่สำเร็จ')
        }
      })
      .finally(() => {
        if (!cancelled) setCancelFormatsLoading(false)
      })
    return () => { cancelled = true }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, cancellationRoute, loadedRevision])

  const updateMain = <K extends keyof ChannelDefaultRow>(key: K, value: ChannelDefaultRow[K]) => {
    markDirty()
    setMain((current) => current ? { ...current, [key]: value } : current)
  }

  const updateCancellation = <K extends keyof ChannelDefaultRow>(key: K, value: ChannelDefaultRow[K]) => {
    markDirty()
    setCancellation((current) => current ? { ...current, [key]: value } : current)
  }

  const cancelOptions = useMemo(() => mainRoute === 'saleorder'
    ? [routeOption('saleordercancel')]
    : [routeOption('creditnote'), routeOption('saleinvoicecancel')], [mainRoute])

  const remarkError = validateProfileText(main?.remark ?? '')
  const remark2Error = validateProfileText(main?.remark_2 ?? '')
  const validationError = (() => {
    if (!main || !cancellation) return 'ยังโหลดข้อมูลไม่ครบ'
    if (!main.doc_format_code || mainFormats.length === 0) return 'ไม่พบรูปแบบเอกสารหลักใน SML'
    if (!cancellation.doc_format_code || cancelFormats.length === 0) return 'ไม่พบรูปแบบเอกสารเมื่อยกเลิกใน SML'
    if (!main.doc_prefix || !main.doc_running_format.includes('#')) return 'รูปแบบเลขเอกสารหลักไม่ครบ'
    if (!cancellation.doc_prefix || !cancellation.doc_running_format.includes('#')) return 'รูปแบบเลขเอกสารเมื่อยกเลิกไม่ครบ'
    if (!party?.code) return 'กรุณาเลือกลูกค้า SML'
    if (!main.wh_code || !main.shelf_code) return 'กรุณาเลือกคลังและพื้นที่เก็บ'
    if (main.vat_type < 0 || !Number.isFinite(main.vat_rate) || main.vat_rate < 0) return 'กรุณากำหนดประเภทและอัตราภาษี'
    if (main.shipping_item_enabled && (!main.shipping_item_code || !main.shipping_item_unit_code)) return 'กรุณาเลือกสินค้าและหน่วยสำหรับค่าจัดส่ง'
    if (remarkError) return `หมายเหตุ 1: ${remarkError}`
    if (remark2Error) return `หมายเหตุ 2: ${remark2Error}`
    if (formatError) return formatError
    return ''
  })()

  const payload = () => {
    if (!main || !cancellation || !bundle) return null
    return {
      main_route: mainRoute,
      cancellation_route: cancellationRoute,
      main: { ...main, party_code: party?.code ?? '', party_name: party?.name ?? '' },
      cancellation,
      expected_main_config_version: bundle.main_config_version,
      expected_cancel_config_version: bundle.cancel_config_version,
    }
  }

  const handlePreview = async () => {
    const body = payload()
    if (!body || validationError || previewing) return
    setPreviewing(true)
    setPreview(null)
    try {
      const response = await client.post<BundlePreview>('/api/settings/shopee-sml-route-bundle/preview', body)
      setPreview(response.data)
    } catch (error: any) {
      const code = error?.response?.data?.code
      if (code === 'capability_mismatch') {
        toast.error('SML Gateway ยังไม่รองรับชุดเส้นทางนี้ กรุณาติดต่อผู้ดูแลระบบ')
      } else {
        toast.error(error?.response?.data?.error ?? 'ตรวจสอบการตั้งค่าไม่สำเร็จ')
      }
    } finally {
      setPreviewing(false)
    }
  }

  const handleSave = async () => {
    const body = payload()
    if (!body || !preview || saving) return
    setSaving(true)
    try {
      await client.put('/api/settings/shopee-sml-route-bundle', {
        ...body,
        preview_token: preview.preview_token,
      })
      toast.success('บันทึกเส้นทาง Shopee แล้ว ระบบอัตโนมัติที่เปิดอยู่ถูกพักเพื่อให้ตรวจสอบก่อนเปิดใหม่')
      setDirty(false)
      onSaved()
      onOpenChange(false)
    } catch (error: any) {
      const code = error?.response?.data?.code
      if (code === 'preview_stale' || code === 'preview_required') {
        setPreview(null)
        setDirty(true)
        toast.error('ข้อมูลเปลี่ยนหรือ Preview หมดอายุ กรุณาโหลดและตรวจสอบใหม่')
      } else {
        toast.error(error?.response?.data?.error ?? 'บันทึกการตั้งค่าไม่สำเร็จ')
      }
    } finally {
      setSaving(false)
    }
  }

  const changeMainRoute = (value: MainRoute) => {
    const option = routeOption(value)
    const nextCancellation: CancellationRoute = value === 'saleorder' ? 'saleordercancel' : 'creditnote'
    const cancelOption = routeOption(nextCancellation)
    markDirty()
    setMainRoute(value)
    setCancellationRoute(nextCancellation)
    setMain((current) => ({
      ...(current ?? emptyRoute(option, 'shopee_realtime')),
      endpoint: option.apiPath,
      doc_format_code: '',
      doc_prefix: option.docPrefix,
      doc_running_format: option.docRunningFormat,
    }))
    setCancellation((current) => ({
      ...(current ?? emptyRoute(cancelOption, 'shopee_realtime_cancel')),
      endpoint: cancelOption.apiPath,
      doc_format_code: '',
      doc_prefix: cancelOption.docPrefix,
      doc_running_format: cancelOption.docRunningFormat,
    }))
  }

  const changeCancellationRoute = (value: CancellationRoute) => {
    const option = routeOption(value)
    markDirty()
    setCancellationRoute(value)
    setCancellation((current) => ({
      ...(current ?? emptyRoute(option, 'shopee_realtime_cancel')),
      endpoint: option.apiPath,
      doc_format_code: '',
      doc_prefix: option.docPrefix,
      doc_running_format: option.docRunningFormat,
    }))
  }

  const requestOpenChange = (nextOpen: boolean) => {
    if (!nextOpen && dirty && !window.confirm('มีการตั้งค่าที่ยังไม่ได้บันทึก ต้องการปิดโดยทิ้งการเปลี่ยนแปลงหรือไม่')) return
    if (!nextOpen) setShippingPickerOpen(false)
    onOpenChange(nextOpen)
  }

  return (
    <>
      <Dialog open={open} onOpenChange={requestOpenChange}>
        <DialogContent className="grid max-h-[92vh] max-w-2xl grid-rows-[auto_minmax(0,1fr)_auto] p-4 sm:p-6">
          <DialogHeader>
            <DialogTitle>ตั้งค่าเอกสาร SML สำหรับคำสั่งซื้อ Shopee</DialogTitle>
            <DialogDescription>
              ตั้งค่าเอกสารหลักและเอกสารเมื่อยกเลิกพร้อมกัน เพื่อให้ทั้งสองเส้นทางอ้างอิงกันถูกต้อง
            </DialogDescription>
          </DialogHeader>

          <div className="-mx-4 space-y-4 overflow-y-auto px-4 py-2 sm:-mx-6 sm:px-6">
            {loading ? (
              <div className="rounded-lg border border-border bg-muted/25 p-6 text-center text-sm text-muted-foreground" role="status">
                กำลังโหลดการตั้งค่าและตรวจสอบ SML Gateway...
              </div>
            ) : loadError ? (
              <div className="rounded-lg border border-destructive/30 bg-destructive/5 p-4" role="alert">
                <div className="flex items-start gap-2 text-sm text-destructive">
                  <AlertCircle className="mt-0.5 h-4 w-4 shrink-0" />
                  <span>{loadError}</span>
                </div>
                <Button type="button" variant="outline" size="sm" className="mt-3 gap-2" onClick={loadBundle}>
                  <RefreshCw className="h-4 w-4" /> ลองใหม่
                </Button>
              </div>
            ) : main && cancellation && bundle ? (
              <>
                <div className={`rounded-lg border px-3 py-2.5 text-sm ${bundle.capability.compatible ? 'border-success/30 bg-success/5' : 'border-warning/35 bg-warning/[0.08]'}`} role="status">
                  <div className="flex items-start gap-2">
                    {bundle.capability.compatible
                      ? <CheckCircle2 className="mt-0.5 h-4 w-4 shrink-0 text-success" />
                      : <AlertCircle className="mt-0.5 h-4 w-4 shrink-0 text-warning" />}
                    <div>
                      <div className="font-medium text-foreground">{bundle.capability.message}</div>
                      <div className="mt-1 text-xs text-muted-foreground">
                        บันทึกการตั้งค่าไม่ทำให้ระบบอัตโนมัติเปิดเอง และมีผลเฉพาะเอกสารใหม่
                      </div>
                    </div>
                  </div>
                </div>

                <section className="space-y-4 rounded-lg border border-border p-3 sm:p-4" aria-labelledby="main-route-heading">
                  <div>
                    <div id="main-route-heading" className="font-semibold text-foreground">1. เมื่อคำสั่งซื้อพร้อมส่ง</div>
                    <p className="mt-1 text-xs text-muted-foreground">เลือกเอกสารหลักที่ต้องการสร้างใน SML</p>
                  </div>
                  <div className="space-y-1.5">
                    <Label>เอกสารที่จะสร้าง</Label>
                    <Select value={mainRoute} onValueChange={(value) => changeMainRoute(value as MainRoute)}>
                      <SelectTrigger><SelectValue /></SelectTrigger>
                      <SelectContent>
                        {mainOptions.map((option) => <SelectItem key={option.value} value={option.value}>{option.label}</SelectItem>)}
                      </SelectContent>
                    </Select>
                  </div>
                  <div className="grid gap-3 sm:grid-cols-2">
                    <div className="space-y-1.5 sm:col-span-2">
                      <Label>รูปแบบเอกสาร</Label>
                      <Select
                        value={main.doc_format_code}
                        disabled={mainFormatsLoading || mainFormats.length === 0}
                        onValueChange={(code) => {
                          const selected = mainFormats.find((format) => format.code === code)
                          updateMain('doc_format_code', code)
                          if (selected) {
                            updateMain('doc_prefix', selected.code)
                            updateMain('doc_running_format', runningFormat(selected.format))
                          }
                        }}
                      >
                        <SelectTrigger><SelectValue placeholder={mainFormatsLoading ? 'กำลังโหลด...' : 'เลือกรูปแบบเอกสาร'} /></SelectTrigger>
                        <SelectContent>
                          {mainFormats.map((format) => <SelectItem key={format.code} value={format.code}>{format.code} · {format.name_1}</SelectItem>)}
                        </SelectContent>
                      </Select>
                    </div>
                    <div className="space-y-1.5 sm:col-span-2">
                      <Label>ลูกค้า SML</Label>
                      <PartyPicker
                        billType="sale"
                        value={party}
                        onChange={(value) => { markDirty(); setParty(value) }}
                      />
                    </div>
                    <div className="space-y-1.5">
                      <div className="flex items-center justify-between gap-2">
                        <Label>คลัง</Label>
                        <Button type="button" variant="ghost" size="sm" className="h-6 px-1.5 text-[11px]" onClick={() => setManualWarehouse((value) => !value)}>
                          {manualWarehouse ? 'เลือกจาก SML' : 'พิมพ์รหัสเอง'}
                        </Button>
                      </div>
                      {manualWarehouse ? (
                        <Input value={main.wh_code} onChange={(event) => { updateMain('wh_code', event.target.value.toUpperCase()); updateMain('shelf_code', '') }} />
                      ) : (
                        <WarehousePicker value={main.wh_code} onChange={(warehouse) => { updateMain('wh_code', warehouse.code); updateMain('shelf_code', '') }} />
                      )}
                    </div>
                    <div className="space-y-1.5">
                      <Label>พื้นที่เก็บ</Label>
                      {manualWarehouse ? (
                        <Input value={main.shelf_code} onChange={(event) => updateMain('shelf_code', event.target.value.toUpperCase())} />
                      ) : (
                        <ShelfPicker warehouseCode={main.wh_code} value={main.shelf_code} onChange={(shelf) => updateMain('shelf_code', shelf.code)} />
                      )}
                    </div>
                    <div className="space-y-1.5">
                      <Label>ประเภทภาษี</Label>
                      <Select value={main.vat_type >= 0 ? String(main.vat_type) : ''} onValueChange={(value) => updateMain('vat_type', Number(value))}>
                        <SelectTrigger><SelectValue placeholder="เลือกประเภทภาษี" /></SelectTrigger>
                        <SelectContent>
                          <SelectItem value="0">แยกนอก</SelectItem>
                          <SelectItem value="1">รวมใน</SelectItem>
                          <SelectItem value="2">อัตรา 0%</SelectItem>
                        </SelectContent>
                      </Select>
                    </div>
                    <div className="space-y-1.5">
                      <Label htmlFor="bundle-vat-rate">อัตราภาษี (%)</Label>
                      <Input id="bundle-vat-rate" inputMode="decimal" value={main.vat_rate >= 0 ? String(main.vat_rate) : ''} onChange={(event) => updateMain('vat_rate', event.target.value === '' ? -1 : Number(event.target.value))} />
                    </div>
                    <div className="space-y-1.5">
                      <Label>ประเภทรายการ</Label>
                      <Select value={main.inquiry_type >= 0 ? String(main.inquiry_type) : '0'} onValueChange={(value) => updateMain('inquiry_type', Number(value))}>
                        <SelectTrigger><SelectValue /></SelectTrigger>
                        <SelectContent>
                          <SelectItem value="0">ขายเงินเชื่อ</SelectItem>
                          <SelectItem value="1">ขายเงินสด</SelectItem>
                          <SelectItem value="2">ขายเงินเชื่อ (สินค้าบริการ)</SelectItem>
                          <SelectItem value="3">ขายเงินสด (สินค้าบริการ)</SelectItem>
                        </SelectContent>
                      </Select>
                    </div>
                    <div className="space-y-1.5">
                      <Label>สาขา</Label>
                      <SMLMasterCodePicker kind="branch" value={main.branch_code} onChange={(value) => updateMain('branch_code', value)} />
                    </div>
                    <div className="space-y-1.5 sm:col-span-2">
                      <Label>พนักงานขาย</Label>
                      <SMLMasterCodePicker kind="sale" value={main.sale_code} onChange={(value) => updateMain('sale_code', value)} />
                    </div>
                    <div className="space-y-1.5 sm:col-span-2">
                      <Label htmlFor="bundle-remark">หมายเหตุ 1</Label>
                      <Input id="bundle-remark" value={main.remark ?? ''} aria-invalid={Boolean(remarkError)} onChange={(event) => updateMain('remark', event.target.value)} placeholder="ข้อความอิสระ ไม่เกิน 255 ตัวอักษร" />
                      {remarkError && <p className="text-xs text-destructive">{remarkError}</p>}
                    </div>
                    <div className="space-y-1.5 sm:col-span-2">
                      <Label htmlFor="bundle-remark-2">หมายเหตุ 2</Label>
                      <Input id="bundle-remark-2" value={main.remark_2 ?? ''} aria-invalid={Boolean(remark2Error)} onChange={(event) => updateMain('remark_2', event.target.value)} placeholder="ข้อความอิสระ ไม่เกิน 255 ตัวอักษร" />
                      {remark2Error && <p className="text-xs text-destructive">{remark2Error}</p>}
                    </div>
                  </div>

                  <div className="space-y-3 rounded-md border border-border bg-muted/20 p-3">
                    <div className="flex items-start justify-between gap-3">
                      <div>
                        <div className="text-sm font-medium text-foreground">ค่าจัดส่งเข้า SML</div>
                        <p className="mt-1 text-xs text-muted-foreground">เปิดเมื่อให้ค่าจัดส่งที่ผู้ซื้อชำระเป็นรายการบริการแยกในบิล</p>
                      </div>
                      <Switch checked={Boolean(main.shipping_item_enabled)} onCheckedChange={(value) => updateMain('shipping_item_enabled', value)} aria-label="ส่งค่าจัดส่งเข้า SML" />
                    </div>
                    {main.shipping_item_enabled && (
                      <div className="grid gap-3 sm:grid-cols-[minmax(0,1fr)_auto]">
                        <div className="rounded-md border border-border bg-background px-3 py-2 text-sm">
                          {main.shipping_item_code
                            ? <><span className="font-mono font-semibold">{main.shipping_item_code}</span><span className="ml-2 text-muted-foreground">{shippingItemName}</span></>
                            : <span className="text-muted-foreground">ยังไม่ได้เลือกสินค้า</span>}
                        </div>
                        <Button type="button" variant="outline" className="gap-2" onClick={() => setShippingPickerOpen(true)}>
                          <PackageSearch className="h-4 w-4" /> เลือกสินค้า
                        </Button>
                        <div className="sm:col-span-2">
                          <UnitSelect value={main.shipping_item_unit_code ?? ''} onValueChange={(value) => updateMain('shipping_item_unit_code', value)} productCode={main.shipping_item_code ?? ''} autoSelectSingle />
                        </div>
                      </div>
                    )}
                  </div>
                </section>

                <section className="space-y-4 rounded-lg border border-border p-3 sm:p-4" aria-labelledby="cancel-route-heading">
                  <div>
                    <div id="cancel-route-heading" className="font-semibold text-foreground">2. เมื่อ Shopee ยกเลิกคำสั่งซื้อ</div>
                    <p className="mt-1 text-xs text-muted-foreground">ลูกค้า สินค้า สาขา คลัง ภาษี และยอดเงินจะอ้างอิงจากเอกสารต้นทาง ระบบไม่ให้กรอกซ้ำ</p>
                  </div>
                  <div className="space-y-1.5">
                    <Label>เอกสารที่จะสร้าง</Label>
                    <Select value={cancellationRoute} onValueChange={(value) => changeCancellationRoute(value as CancellationRoute)}>
                      <SelectTrigger><SelectValue /></SelectTrigger>
                      <SelectContent>
                        {cancelOptions.map((option) => <SelectItem key={option.value} value={option.value}>{option.label}</SelectItem>)}
                      </SelectContent>
                    </Select>
                  </div>
                  <div className="space-y-1.5">
                    <Label>รูปแบบเอกสาร</Label>
                    <Select
                      value={cancellation.doc_format_code}
                      disabled={cancelFormatsLoading || cancelFormats.length === 0}
                      onValueChange={(code) => {
                        const selected = cancelFormats.find((format) => format.code === code)
                        updateCancellation('doc_format_code', code)
                        if (selected) {
                          updateCancellation('doc_prefix', selected.code)
                          updateCancellation('doc_running_format', runningFormat(selected.format))
                        }
                      }}
                    >
                      <SelectTrigger><SelectValue placeholder={cancelFormatsLoading ? 'กำลังโหลด...' : 'เลือกรูปแบบเอกสาร'} /></SelectTrigger>
                      <SelectContent>
                        {cancelFormats.map((format) => <SelectItem key={format.code} value={format.code}>{format.code} · {format.name_1}</SelectItem>)}
                      </SelectContent>
                    </Select>
                  </div>
                </section>

                <div className="rounded-lg border border-border bg-muted/20 p-3 text-xs text-muted-foreground">
                  <div className="flex flex-wrap gap-2">
                    <Badge variant="outline">เอกสารหลัก: {modeLabel((preview?.route_modes ?? bundle.route_modes)[mainRoute])}</Badge>
                    <Badge variant="outline">เมื่อยกเลิก: {modeLabel((preview?.route_modes ?? bundle.route_modes)[cancellationRoute])}</Badge>
                  </div>
                  <p className="mt-2">การบันทึกกับการเปิด Auto SML เป็นคนละขั้นตอน ระบบจะไม่เปิดงานอัตโนมัติจาก dialog นี้</p>
                </div>

                {preview && (
                  <div className={`rounded-lg border p-3 text-sm ${preview.readiness.automation_ready ? 'border-success/30 bg-success/5' : 'border-warning/35 bg-warning/[0.08]'}`} role="status" aria-live="polite">
                    <div className="font-medium text-foreground">ตรวจสอบค่าล่าสุดแล้ว พร้อมบันทึก</div>
                    <p className="mt-1 text-xs text-muted-foreground">
                      {preview.readiness.automation_ready
                        ? 'ทั้งสองเส้นทางพร้อมสำหรับระบบอัตโนมัติ หลังบันทึกยังต้องไปยืนยันเปิดใช้งานแยกต่างหาก'
                        : 'บันทึกค่าได้ แต่ระบบอัตโนมัติจะยังเปิดไม่ได้จนกว่าผู้ดูแลจะเปิดโหมดของทั้งสองเส้นทาง'}
                    </p>
                  </div>
                )}
              </>
            ) : null}
          </div>

          <DialogFooter className="flex-col items-stretch gap-2 sm:flex-row sm:items-center sm:justify-between">
            <p className="text-xs text-muted-foreground" role="status">{validationError || (dirty && !preview ? 'แก้ไขแล้ว กรุณาตรวจสอบค่าก่อนบันทึก' : '')}</p>
            <div className="flex w-full flex-col gap-2 sm:w-auto sm:flex-row sm:justify-end">
              <Button className="w-full sm:w-auto" type="button" variant="outline" onClick={() => requestOpenChange(false)} disabled={saving}>ยกเลิก</Button>
              <Button className="w-full sm:w-auto" type="button" variant="outline" onClick={handlePreview} disabled={Boolean(loading || loadError || validationError || previewing || saving)}>
                {previewing ? 'กำลังตรวจ...' : 'ตรวจสอบความพร้อม'}
              </Button>
              <Button className="w-full sm:w-auto" type="button" onClick={handleSave} disabled={Boolean(!preview || saving)}>
                {saving ? 'กำลังบันทึก...' : 'บันทึกทั้งสองเส้นทาง'}
              </Button>
            </div>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {main && (
        <MapItemModal
          open={open && shippingPickerOpen}
          rawName="ค่าจัดส่ง Shopee"
          currentCode={main.shipping_item_code ?? ''}
          currentUnit={main.shipping_item_unit_code ?? ''}
          currentPrice={0}
          rawNameLabel="รายการค่าจัดส่งจาก Shopee"
          onPick={(code, unitCode, picked?: CatalogMatch) => {
            updateMain('shipping_item_code', code)
            updateMain('shipping_item_unit_code', unitCode || '')
            setShippingItemName(picked?.item_name || '')
            setShippingPickerOpen(false)
          }}
          onClose={() => setShippingPickerOpen(false)}
        />
      )}
    </>
  )
}
