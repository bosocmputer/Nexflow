import { useEffect, useState } from 'react'
import { Eye, PackageSearch } from 'lucide-react'
import { toast } from 'sonner'

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
import { UnitSelect } from '@/components/common/UnitSelect'
import client from '@/api/client'
import type { CatalogMatch } from '@/types'
import { validateProfileText } from '@/lib/smlDocumentProfile.js'
import { SMLMasterCodePicker } from '../BillDetail/components/SMLMasterCodePicker'
import { ShelfPicker, WarehousePicker } from '../BillDetail/components/WarehousePicker'
import { PartyPicker, type Party } from './PartyPicker'

interface SmlDocFormat {
  code: string
  name_1: string
  name_2: string
  format: string
  screen_code: string
}

interface SMLMasterOption {
  code: string
  name_1: string
  bank_code?: string
  bank_branch?: string
}

const SELECT_NONE_VALUE = '__none__'

import {
  CHANNEL_LABELS,
  destinationFor,
  destinationOptionsFor,
  docNoPatternWarning,
  previewDocNo,
  type ChannelDefaultRow,
  type ChannelKey,
  type EndpointKind,
} from './labels'
import { MapItemModal } from '../BillDetail/components/MapItemModal'

interface ChannelDefaultPreview {
  profile_mode: 'off' | 'shadow' | 'active'
  profile_version: string
  route_signature: string
  resolved: { remark: string; remark_2: string }
  system_fields: Record<string, string>
  payload: {
    endpoint: string
    doc_format_code: string
    warehouse: string
    location: string
    vat_type: number
    vat_rate: number
    remark: string
    remark_2: string
    document_profile_version: string
  }
  missing_prerequisites: string[]
  warnings: string[]
}

interface Props {
  open: boolean
  onOpenChange: (v: boolean) => void
  row: ChannelDefaultRow | null
  onSaved: () => void
}

export function EditDialog({ open, onOpenChange, row, onSaved }: Props) {
  const [selectedDestination, setSelectedDestination] = useState<EndpointKind>('purchaseorder')
  const [docPrefix, setDocPrefix] = useState('')
  const [docRunningFormat, setDocRunningFormat] = useState('')
  const [selectedDocFormatCode, setSelectedDocFormatCode] = useState('')
  const [docFormats, setDocFormats] = useState<SmlDocFormat[]>([])
  const [docFormatsLoading, setDocFormatsLoading] = useState(false)

  // prefix = doc_format code (e.g. "POL"), running format = format field from SML stripped of leading "@"
  // SML uses "@" to mean "prefix with the doc_format code" — Nexflow already does that via doc_prefix
  const parseSmlFormat = (code: string, format: string): { prefix: string; runningFormat: string } => {
    return { prefix: code, runningFormat: format.replace(/^@/, '') }
  }
  const [shippingEnabled, setShippingEnabled] = useState(false)
  const [shippingItemCode, setShippingItemCode] = useState('')
  const [shippingItemUnitCode, setShippingItemUnitCode] = useState('')
  const [shippingItemName, setShippingItemName] = useState('')
  const [shippingPickerOpen, setShippingPickerOpen] = useState(false)
  const [party, setParty] = useState<Party | null>(null)
  const [branchCode, setBranchCode] = useState('')
  const [saleCode, setSaleCode] = useState('')
  const [whCode, setWhCode] = useState('')
  const [shelfCode, setShelfCode] = useState('')
  const [manualWarehouse, setManualWarehouse] = useState(false)
  const [vatTypeStr, setVatTypeStr] = useState('')
  const [vatRate, setVatRate] = useState('')
  const [inquiryTypeStr, setInquiryTypeStr] = useState('')
  const [remark, setRemark] = useState('')
  const [remark2, setRemark2] = useState('')
  const [passbookCode, setPassbookCode] = useState('')
  const [expenseCode, setExpenseCode] = useState('')
  const [passbooks, setPassbooks] = useState<SMLMasterOption[]>([])
  const [expenses, setExpenses] = useState<SMLMasterOption[]>([])
  const [settlementMastersLoading, setSettlementMastersLoading] = useState(false)
  const [saving, setSaving] = useState(false)
  const [previewing, setPreviewing] = useState(false)
  const [preview, setPreview] = useState<ChannelDefaultPreview | null>(null)
  const [dirty, setDirty] = useState(false)

  const markDirty = () => {
    setDirty(true)
    setPreview(null)
  }

  useEffect(() => {
    if (!open || !row) return
    const detectedDestination = destinationFor(
      row.channel as ChannelKey,
      row.bill_type,
      row.endpoint ?? '',
      row.doc_format_code ?? '',
    )
    const defaultDestination = destinationOptionsFor(row.bill_type, row.channel as ChannelKey)[0]
    const destination = detectedDestination ?? defaultDestination

    setSelectedDestination(destination?.value ?? 'purchaseorder')
    setDocPrefix(row.doc_prefix || destination?.docPrefix || '')
    setDocRunningFormat(row.doc_running_format || destination?.docRunningFormat || '')
    setSelectedDocFormatCode(row.doc_format_code || destination?.docFormatCode || '')
    setShippingEnabled(Boolean(row.shipping_item_enabled))
    setShippingItemCode(row.shipping_item_code || '')
    setShippingItemUnitCode(row.shipping_item_unit_code || '')
    setShippingItemName('')
    setParty(row.party_code ? { code: row.party_code, name: row.party_name || row.party_code } : null)
    setBranchCode(row.branch_code || '')
    setSaleCode(row.sale_code || '')
    setWhCode(row.wh_code || '')
    setShelfCode(row.shelf_code || '')
    setManualWarehouse(false)
    setVatTypeStr(typeof row.vat_type === 'number' && row.vat_type >= 0 ? String(row.vat_type) : '')
    setVatRate(typeof row.vat_rate === 'number' && row.vat_rate >= 0 ? String(row.vat_rate) : '')
    setInquiryTypeStr(typeof row.inquiry_type === 'number' && row.inquiry_type >= 0 ? String(row.inquiry_type) : '')
    setRemark(row.remark || '')
    setRemark2(row.remark_2 || '')
    setPassbookCode(row.passbook_code || '')
    setExpenseCode(row.expense_code || '')
    setPreview(null)
    setDirty(false)
  }, [open, row])

  // Fetch doc formats from SML when destination changes; auto-fill prefix + running format from selected format
  useEffect(() => {
    if (!open || !row) return
    let cancelled = false
    const destination = destinationOptionsFor(row.bill_type, row.channel as ChannelKey)
      .find((option) => option.value === selectedDestination)
    const screenCode = destination?.screenCode
    if (!screenCode) return
    const savedDestination = destinationFor(
      row.channel as ChannelKey,
      row.bill_type,
      row.endpoint ?? '',
      row.doc_format_code ?? '',
    )
    const savedDocFormatCode = savedDestination?.value === selectedDestination
      ? (row.doc_format_code ?? '')
      : ''
    setDocFormats([])
    setDocFormatsLoading(true)
    client.get(`/api/sml/doc-formats?screen_code=${screenCode}`)
      .then((res) => {
        if (cancelled) return
        const formats: SmlDocFormat[] = res.data?.data ?? []
        setDocFormats(formats)
        if (formats.length === 0) return
        // Keep current selection if still in list; otherwise default to first
        const current = formats.find((f) => f.code === savedDocFormatCode)
        const chosen = current ?? formats[0]
        setSelectedDocFormatCode(chosen.code)
        const { prefix, runningFormat } = parseSmlFormat(chosen.code, chosen.format)
        if (prefix) setDocPrefix(prefix)
        if (runningFormat) setDocRunningFormat(runningFormat)
      })
      .catch(() => {
        if (cancelled) return
        setDocFormats([])
      })
      .finally(() => {
        if (!cancelled) setDocFormatsLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [open, row, selectedDestination])

  useEffect(() => {
    if (!open || !row || row.channel !== 'shopee_settlement' || row.bill_type !== 'ar_receipt') return
    let cancelled = false
    setSettlementMastersLoading(true)
    Promise.all([
      client.get<{ data: SMLMasterOption[] }>('/api/sml/passbooks?limit=100'),
      client.get<{ data: SMLMasterOption[] }>('/api/sml/expenses?limit=100'),
    ])
      .then(([passbookRes, expenseRes]) => {
        if (cancelled) return
        setPassbooks(passbookRes.data.data ?? [])
        setExpenses(expenseRes.data.data ?? [])
      })
      .catch(() => {
        if (cancelled) return
        setPassbooks([])
        setExpenses([])
      })
      .finally(() => {
        if (!cancelled) setSettlementMastersLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [open, row])

  if (!row) return null

  const isPurchase = row.bill_type === 'purchase'
  const isSettlement = row.channel === 'shopee_settlement' && row.bill_type === 'ar_receipt'
  const isShopeePurchase = row.channel === 'shopee_shipped' && row.bill_type === 'purchase'
  const isMarketplaceSaleShipping =
    row.bill_type === 'sale' && (
      row.channel === 'shopee' ||
      row.channel === 'shopee_realtime' ||
      row.channel === 'lazada' ||
      row.channel === 'tiktok'
    )
  const supportsShippingItem = isShopeePurchase || isMarketplaceSaleShipping
  const shippingChannelLabel = row.channel === 'lazada'
    ? 'Lazada'
    : row.channel === 'tiktok'
      ? 'TikTok'
      : 'Shopee'
  const showPartyPicker =
    (row.channel === 'shopee_shipped' && row.bill_type === 'purchase') ||
    (row.channel === 'shopee' && row.bill_type === 'sale') ||
    (row.channel === 'shopee_realtime' && row.bill_type === 'sale') ||
    (row.channel === 'lazada' && row.bill_type === 'sale') ||
    (row.channel === 'tiktok' && row.bill_type === 'sale')
  const isShopeeRealtimeAutoRoute = row.channel === 'shopee_realtime' && row.bill_type === 'sale'
  const isShopeeRealtimeCancelRoute = row.channel === 'shopee_realtime_cancel' && row.bill_type === 'sale'
  const channelLabel = isShopeePurchase
    ? 'Email บิลซื้อ Shopee'
    : CHANNEL_LABELS[row.channel as ChannelKey] ?? row.channel
  const billTypeLabel = isPurchase ? 'บิลซื้อ' : isSettlement ? 'ลูกหนี้' : 'บิลขาย'
  const destinationOptions = destinationOptionsFor(row.bill_type, row.channel as ChannelKey)
  const selectedDestinationMeta =
    destinationOptions.find((option) => option.value === selectedDestination) ??
    destinationFor(row.channel as ChannelKey, row.bill_type, row.endpoint ?? '', row.doc_format_code ?? '') ??
    destinationOptions[0]
  const docPrefixTrimmed = docPrefix.trim()
  const docRunningFormatTrimmed = docRunningFormat.trim().toUpperCase()
  const shippingItemCodeTrimmed = shippingItemCode.trim()
  const shippingItemUnitCodeTrimmed = shippingItemUnitCode.trim()
  const branchCodeTrimmed = branchCode.trim()
  const saleCodeTrimmed = saleCode.trim()
  const whCodeTrimmed = whCode.trim()
  const shelfCodeTrimmed = shelfCode.trim()
  const passbookCodeTrimmed = passbookCode.trim()
  const expenseCodeTrimmed = expenseCode.trim()
  const vatTypeValue = vatTypeStr === '' ? -1 : Number(vatTypeStr)
  const parsedVatRate = Number(vatRate)
  const vatRateValue = vatRate.trim() === '' || !Number.isFinite(parsedVatRate) ? -1 : parsedVatRate
  const inquiryTypeValue = inquiryTypeStr === '' ? -1 : Number(inquiryTypeStr)
  const remarkError = validateProfileText(remark, true)
  const remark2Error = validateProfileText(remark2)
  const docWarning = docNoPatternWarning(docPrefixTrimmed, docRunningFormatTrimmed)
  const selectedPassbook = passbooks.find((p) => p.code === passbookCodeTrimmed)
  const selectedExpense = expenses.find((p) => p.code === expenseCodeTrimmed)
  const saveDisabledReason = (() => {
    if (saving) return 'กำลังบันทึก'
    if (!selectedDestinationMeta) return 'เลือกปลายทาง SML ก่อน'
    if (docFormatsLoading) return 'รอโหลดรูปแบบเอกสารจาก SML'
    if (isShopeeRealtimeCancelRoute && docFormats.length === 0) return 'ไม่พบรูปแบบเอกสารที่ตรงกับปลายทางนี้ใน SML'
    if (isShopeeRealtimeCancelRoute && !selectedDocFormatCode) return 'เลือกรูปแบบเอกสารจาก SML ก่อน'
    if (isSettlement && !selectedDocFormatCode) return 'เลือกรูปแบบเอกสารรับชำระก่อน'
    if (isSettlement && !passbookCodeTrimmed) return 'เลือกบัญชีรับเงินจริงก่อน'
    if (!isSettlement && (!docPrefixTrimmed || !docRunningFormatTrimmed || !docRunningFormatTrimmed.includes('#'))) {
      return 'เลือกรูปแบบเอกสารที่มีเลขรันให้ครบก่อน'
    }
    if (!isSettlement && docWarning) return docWarning
    if (isShopeeRealtimeAutoRoute && !party?.code) return 'เลือกลูกค้า SML สำหรับ Auto SML ก่อน'
    if (isShopeeRealtimeAutoRoute && !whCodeTrimmed) return 'เลือกคลังสำหรับ Auto SML ก่อน'
    if (isShopeeRealtimeAutoRoute && !shelfCodeTrimmed) return 'เลือกพื้นที่เก็บสำหรับ Auto SML ก่อน'
    if (isShopeeRealtimeAutoRoute && (vatTypeStr === '' || vatRateValue < 0)) return 'ตั้งค่า VAT สำหรับ Auto SML ก่อน'
    if (supportsShippingItem && shippingEnabled && !shippingItemCodeTrimmed) {
      return 'เลือกสินค้า SML สำหรับค่าขนส่งก่อนเปิดใช้งาน'
    }
    if (supportsShippingItem && shippingEnabled && !shippingItemUnitCodeTrimmed) {
      return 'เลือกหน่วย SML สำหรับค่าขนส่งก่อนเปิดใช้งาน'
    }
	if (remarkError) return `หมายเหตุ 1: ${remarkError}`
	if (remark2Error) return `หมายเหตุ 2: ${remark2Error}`
	if (isShopeeRealtimeAutoRoute && !preview) return 'ตรวจตัวอย่างและผลกระทบก่อนบันทึกเส้นทาง Auto SML'
    return ''
  })()
  const canSave = !saveDisabledReason

  const handleDestinationChange = (value: EndpointKind) => {
	markDirty()
    const destination = destinationOptions.find((option) => option.value === value)
    setSelectedDestination(value)
    setSelectedDocFormatCode('') // reset — useEffect will re-fetch and select first
    if (!destination) return
    setDocPrefix(destination.docPrefix)
    setDocRunningFormat(destination.docRunningFormat)
  }

  const buildPayload = () => ({
    channel: row.channel,
    bill_type: row.bill_type,
    party_code: showPartyPicker ? (party?.code ?? '') : (row.party_code ?? ''),
    party_name: showPartyPicker ? (party?.name ?? '') : (row.party_name ?? ''),
    party_phone: row.party_phone ?? '',
    party_address: row.party_address ?? '',
    party_tax_id: row.party_tax_id ?? '',
    doc_format_code: selectedDocFormatCode || selectedDestinationMeta.docFormatCode,
    endpoint: selectedDestinationMeta.apiPath,
    doc_prefix: isSettlement ? (selectedDocFormatCode || selectedDestinationMeta.docPrefix) : docPrefixTrimmed,
    doc_running_format: isSettlement ? '@YYMM####' : docRunningFormatTrimmed,
    branch_code: isSettlement ? '' : branchCodeTrimmed,
    sale_code: isSettlement ? '' : saleCodeTrimmed,
    unit_code: '',
    doc_time: '',
    shipping_item_enabled: supportsShippingItem ? shippingEnabled : false,
    shipping_item_code: supportsShippingItem ? shippingItemCodeTrimmed : '',
    shipping_item_unit_code: supportsShippingItem ? shippingItemUnitCodeTrimmed : '',
    passbook_code: isSettlement ? passbookCodeTrimmed : '',
    passbook_name: isSettlement ? (selectedPassbook?.name_1 ?? row.passbook_name ?? '') : '',
    bank_code: isSettlement ? (selectedPassbook?.bank_code ?? row.bank_code ?? '') : '',
    bank_branch: isSettlement ? (selectedPassbook?.bank_branch ?? row.bank_branch ?? '') : '',
    expense_code: isSettlement ? expenseCodeTrimmed : '',
    expense_name: isSettlement ? (selectedExpense?.name_1 ?? row.expense_name ?? '') : '',
    wh_code: isSettlement ? '' : whCodeTrimmed,
    shelf_code: isSettlement ? '' : shelfCodeTrimmed,
    vat_type: isSettlement ? -1 : vatTypeValue,
    vat_rate: isSettlement ? -1 : vatRateValue,
    inquiry_type: isSettlement ? -1 : inquiryTypeValue,
    remark: isSettlement ? '' : remark,
    remark_2: isSettlement ? '' : remark2,
    expected_config_version: row.config_version ?? 0,
  })

  const handlePreview = async () => {
    if (previewing || saving) return
    if (remarkError || remark2Error) {
      toast.error(remarkError ? `หมายเหตุ 1: ${remarkError}` : `หมายเหตุ 2: ${remark2Error}`)
      return
    }
    setPreviewing(true)
    setPreview(null)
    try {
      const response = await client.post<ChannelDefaultPreview>('/api/settings/channel-defaults/preview', {
        ...buildPayload(),
        preview_context: {
          channel: channelLabel,
          order_ref: 'ORDER-PREVIEW',
          bill_no: previewDocNo(docPrefixTrimmed || 'BF', docRunningFormatTrimmed || 'YYMM####'),
        },
      })
      setPreview(response.data)
    } catch (e: any) {
      toast.error('ตรวจตัวอย่างไม่สำเร็จ: ' + (e?.response?.data?.error ?? e?.message ?? 'unknown'))
    } finally {
      setPreviewing(false)
    }
  }

  const handleSave = async () => {
    if (saving) return
    if (!selectedDestinationMeta) {
      toast.error('กรุณาเลือกปลายทาง SML ก่อน')
      return
    }
    if (isSettlement && (!selectedDocFormatCode || !passbookCodeTrimmed)) {
      toast.error('กรุณาเลือกรูปแบบเอกสารรับชำระและบัญชีรับเงิน')
      return
    }
    if (isShopeeRealtimeCancelRoute && (!selectedDocFormatCode || docFormats.length === 0)) {
      toast.error('กรุณาเลือกรูปแบบเอกสารที่มีอยู่จริงใน SML')
      return
    }
    if (!isSettlement && (!docPrefixTrimmed || !docRunningFormatTrimmed || !docRunningFormatTrimmed.includes('#'))) {
      toast.error('เลือกรูปแบบเอกสารก่อน ระบบจะดึง prefix และรูปแบบเลขรันจาก SML ให้อัตโนมัติ')
      return
    }
    if (!isSettlement && docWarning) {
      toast.error('แก้รูปแบบเลขเอกสารตามคำเตือนก่อนบันทึก')
      return
    }
    if (supportsShippingItem && shippingEnabled && (!shippingItemCodeTrimmed || !shippingItemUnitCodeTrimmed)) {
      toast.error('กรุณาเลือกสินค้า SML สำหรับค่าขนส่งก่อนเปิดใช้งาน')
      return
    }
    setSaving(true)
    try {
		await client.put('/api/settings/channel-defaults', buildPayload())
      toast.success('บันทึกสำเร็จ')
		setDirty(false)
      onSaved()
      onOpenChange(false)
    } catch (e: any) {
		if (e?.response?.status === 409 && e?.response?.data?.code === 'config_version_conflict') {
			toast.error('มีผู้ใช้อื่นแก้ไขค่านี้แล้ว ระบบกำลังโหลดข้อมูลล่าสุด กรุณาตรวจสอบก่อนบันทึกอีกครั้ง')
			setDirty(false)
			onSaved()
			onOpenChange(false)
			return
		}
      toast.error('บันทึกล้มเหลว: ' + (e?.response?.data?.error ?? e?.message ?? 'unknown'))
    } finally {
      setSaving(false)
    }
  }

  const handleShippingPick = (code: string, unitCode: string, picked?: CatalogMatch) => {
	markDirty()
    setShippingItemCode(code)
    setShippingItemUnitCode(unitCode || '')
    setShippingItemName(picked?.item_name || '')
    setShippingPickerOpen(false)
  }

  const requestOpenChange = (nextOpen: boolean) => {
	if (!nextOpen && dirty && !window.confirm('มีการตั้งค่าที่ยังไม่ได้บันทึก ต้องการปิดโดยทิ้งการเปลี่ยนแปลงหรือไม่')) {
		return
	}
	if (!nextOpen) setShippingPickerOpen(false)
	onOpenChange(nextOpen)
  }

  return (
    <>
      <Dialog
        open={open}
		onOpenChange={requestOpenChange}
      >
        <DialogContent className="grid max-h-[90vh] max-w-xl grid-rows-[auto_minmax(0,1fr)_auto]">
          <DialogHeader>
            <DialogTitle>
              ตั้งค่าเส้นทาง SML สำหรับ {channelLabel} ({billTypeLabel})
            </DialogTitle>
            <DialogDescription>
              {isShopeeRealtimeCancelRoute
                ? 'เลือกว่าจะยกเลิกใบขายเดิม หรือสร้างเอกสารรับคืนสินค้า/ลดหนี้ พร้อมรูปแบบเอกสารจาก SML'
                : 'กำหนดปลายทางและค่าเริ่มต้นที่ Nexflow ใช้เมื่อสร้างเอกสารใน SML'}
            </DialogDescription>
          </DialogHeader>

          <div className="-mx-6 space-y-4 overflow-y-auto px-6 py-2">
            <div className="space-y-1.5">
              <Label>ปลายทาง SML</Label>
              <Select value={selectedDestination} onValueChange={handleDestinationChange}>
                <SelectTrigger>
                  <SelectValue placeholder="เลือกปลายทาง SML" />
                </SelectTrigger>
                <SelectContent>
                  {destinationOptions.map((option) => (
                    <SelectItem key={option.value} value={option.value}>
                      {option.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <div className="rounded-md border border-success/30 bg-success/5 px-3 py-2 text-xs">
                <div className="flex flex-wrap items-center gap-2">
                  <span className="font-medium text-foreground">
                    {selectedDestinationMeta?.label}
                  </span>
                  {selectedDestinationMeta?.statusLabel && (
                    <span className="rounded bg-success/10 px-1.5 py-0.5 text-[9px] font-medium text-success">
                      {selectedDestinationMeta.statusLabel}
                    </span>
                  )}
                </div>
                <code className="mt-1 block text-[10px] text-muted-foreground">
                  POST {selectedDestinationMeta?.apiPath}
                </code>
                <p className="mt-1 text-[11px] text-muted-foreground">
                  {selectedDestinationMeta?.description}
                </p>
              </div>
            </div>

            <div className="space-y-1.5">
              <Label>รูปแบบเอกสาร (doc_format_code)</Label>
              {docFormatsLoading ? (
                <div className="rounded-md border border-border bg-muted/30 px-3 py-2 text-sm text-muted-foreground">
                  กำลังโหลด...
                </div>
              ) : docFormats.length > 0 ? (
                <Select
                  value={selectedDocFormatCode}
                  onValueChange={(code) => {
					markDirty()
                    setSelectedDocFormatCode(code)
                    const fmt = docFormats.find((f) => f.code === code)
                    if (fmt) {
                      const { prefix, runningFormat } = parseSmlFormat(fmt.code, fmt.format)
                      if (prefix) setDocPrefix(prefix)
                      if (runningFormat) setDocRunningFormat(runningFormat)
                    }
                  }}
                >
                  <SelectTrigger>
                    <SelectValue placeholder="เลือกรูปแบบเอกสาร" />
                  </SelectTrigger>
                  <SelectContent>
                    {docFormats.map((fmt) => (
                      <SelectItem key={fmt.code} value={fmt.code}>
                        <span className="font-mono font-semibold">{fmt.code}</span>
                        <span className="ml-2 text-muted-foreground">— {fmt.name_1}</span>
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              ) : (
                <div className={`rounded-md border px-3 py-2 text-sm ${isShopeeRealtimeCancelRoute ? 'border-destructive/30 bg-destructive/5 text-destructive' : 'border-border bg-muted/30 font-mono text-foreground'}`}>
                  {isShopeeRealtimeCancelRoute
                    ? 'ไม่พบรูปแบบเอกสารสำหรับปลายทางนี้ใน SML กรุณาตรวจ erp_doc_format'
                    : (selectedDocFormatCode || selectedDestinationMeta?.docFormatCode || '-')}
                </div>
              )}
              <p className="text-xs text-muted-foreground">
                {docFormats.length > 0
                  ? `ดึงจาก erp_doc_format ใน SML (${docFormats.length} รายการ)`
                  : 'ค่า default จากปลายทาง SML ที่เลือกไว้'}
              </p>
            </div>

            {isSettlement && (
              <div className="space-y-3 rounded-md border border-border bg-muted/20 p-3">
                <div>
                  <div className="text-xs font-semibold text-foreground">
                    ตั้งค่ารับชำระ Shopee
                  </div>
                  <p className="mt-1 text-xs text-muted-foreground">
                    ใช้ค่าจาก SML master จริงสำหรับเมนูรับชำระหนี้. ส่วนต่าง Shopee เว้นว่างได้ตอนตั้งค่า
                    แต่ถ้ารอบส่งมีส่วนต่าง ระบบจะบังคับเลือกก่อนส่งจริง
                  </p>
                </div>
                <div className="grid gap-3">
                  <div className="space-y-1.5">
                    <Label className="text-xs">บัญชีรับเงิน</Label>
                    <Select
                      value={passbookCode || SELECT_NONE_VALUE}
					  onValueChange={(value) => {
						markDirty()
						setPassbookCode(value === SELECT_NONE_VALUE ? '' : value)
					  }}
                      disabled={settlementMastersLoading}
                    >
                      <SelectTrigger className="h-10">
                        <SelectValue placeholder="เลือกบัญชีรับเงินจาก SML" />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value={SELECT_NONE_VALUE}>เลือกบัญชีรับเงินจาก SML</SelectItem>
                      {passbooks.map((p) => (
                        <SelectItem key={p.code} value={p.code}>
                          {p.code} · {p.name_1}{p.bank_code ? ` · ${p.bank_code}` : ''}{p.bank_branch ? ` ${p.bank_branch}` : ''}
                        </SelectItem>
                      ))}
                      </SelectContent>
                    </Select>
                    <p className="text-[11px] text-muted-foreground">
                      {passbookCodeTrimmed
                        ? 'ค่าที่เลือกนี้จะถูกบันทึกเป็นบัญชีรับเงินจริงสำหรับรับชำระ Shopee'
                        : 'รายการในช่องนี้เป็นตัวเลือกจาก SML ยังไม่ใช่ค่าที่บันทึก จนกว่าจะเลือกแล้วกดบันทึก'}
                    </p>
                  </div>
                  <div className="space-y-1.5">
                    <Label className="text-xs">ส่วนต่าง Shopee</Label>
                    <Select
                      value={expenseCode || SELECT_NONE_VALUE}
					  onValueChange={(value) => {
						markDirty()
						setExpenseCode(value === SELECT_NONE_VALUE ? '' : value)
					  }}
                      disabled={settlementMastersLoading}
                    >
                      <SelectTrigger className="h-10">
                        <SelectValue placeholder="ยังไม่กำหนดค่าใช้จ่ายส่วนต่าง" />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value={SELECT_NONE_VALUE}>ยังไม่กำหนดค่าใช้จ่ายส่วนต่าง</SelectItem>
                      {expenses.map((p) => (
                        <SelectItem key={p.code} value={p.code}>
                          {p.code} · {p.name_1}
                        </SelectItem>
                      ))}
                      </SelectContent>
                    </Select>
                    <p className="text-[11px] text-muted-foreground">
                      {expenseCodeTrimmed
                        ? 'ค่าที่เลือกนี้จะใช้เมื่อต้องลงค่าธรรมเนียม/ส่วนต่าง Shopee'
                        : 'เว้นว่างได้ แต่ถ้ารอบส่งมีส่วนต่าง ระบบจะบังคับเลือกก่อนสร้าง RC'}
                    </p>
                  </div>
                </div>
                {!expenseCodeTrimmed && (
                  <div className="rounded-md border border-warning/35 bg-warning/[0.08] px-3 py-2 text-xs text-warning">
                    ยังไม่ได้ตั้งค่าใช้จ่ายส่วนต่าง Shopee บันทึกได้ แต่ถ้ารอบส่งมีส่วนต่าง
                    ระบบจะให้เลือกค่าใช้จ่ายก่อนส่งเข้า SML
                  </div>
                )}
              </div>
            )}

            {!isSettlement && (
            <div className="space-y-3 rounded-md border border-border bg-muted/20 p-3">
              <div className="flex items-center justify-between">
                <div className="text-xs font-semibold text-foreground">
                  เลขเอกสาร SML (doc_no)
                </div>
                <span className="text-[10px] text-muted-foreground">ดึงจากรูปแบบเอกสารที่เลือก</span>
              </div>
              <div className="grid grid-cols-2 gap-3">
                <div className="space-y-1">
                  <Label className="text-xs text-muted-foreground">รหัสขึ้นต้นเลขเอกสาร (doc_prefix)</Label>
                  <div className="rounded-md border border-dashed border-border bg-muted/40 px-3 py-2 font-mono text-sm text-foreground">
                    {docPrefixTrimmed || <span className="text-muted-foreground">—</span>}
                  </div>
                </div>
                <div className="space-y-1">
                  <Label className="text-xs text-muted-foreground">รูปแบบเลขรัน (doc_running_format)</Label>
                  <div className="rounded-md border border-dashed border-border bg-muted/40 px-3 py-2 font-mono text-sm text-foreground">
                    {docRunningFormatTrimmed || <span className="text-muted-foreground">—</span>}
                  </div>
                </div>
              </div>
              <div className="text-xs text-muted-foreground">
                <b>ตัวอย่างรูปแบบเลขเอกสาร:</b>{' '}
                <code className="rounded bg-background px-1.5 py-0.5 font-mono text-foreground">
                  {previewDocNo(docPrefixTrimmed || 'BF', docRunningFormatTrimmed || 'YYMM####')}
                </code>
              </div>
            </div>
            )}

            {isShopeeRealtimeCancelRoute && (
              <div className="rounded-md border border-info/30 bg-info/5 px-3 py-2 text-xs text-muted-foreground">
                ลูกค้า รายการสินค้า คลัง พื้นที่เก็บ VAT และยอดเงิน จะอ้างอิงจากใบขาย SML เดิมอัตโนมัติ
                ผู้ใช้จึงตั้งค่าเฉพาะปลายทางและรูปแบบเอกสารเท่านั้น
              </div>
            )}

            {!isSettlement && !isShopeeRealtimeCancelRoute && (
            <div className="space-y-3 rounded-md border border-border bg-muted/20 p-3">
              <div>
                <div className="text-xs font-semibold text-foreground">
                  {isShopeeRealtimeAutoRoute ? 'ค่าคงที่สำหรับ Auto SML' : 'ค่าเริ่มต้นตอนส่ง SML'}
                </div>
                <p className="mt-1 text-xs text-muted-foreground">
                  {isShopeeRealtimeAutoRoute
                    ? 'ระบบใช้ค่าชุดนี้ส่งเข้า SML โดยไม่เปิด dialog ยืนยัน เวลาเอกสารใช้เวลาปัจจุบัน ณ ตอนส่ง (Asia/Bangkok)'
                    : 'ค่าชุดนี้จะถูกเติมใน dialog ส่งบิลให้ user เห็นก่อนกดยืนยัน ถ้าเว้นว่าง ระบบจะให้ user เลือกเองก่อนส่ง'}
                </p>
              </div>
              <div className="grid gap-3 sm:grid-cols-2">
                {showPartyPicker && (
                  <div className="space-y-1.5 sm:col-span-2">
                    <Label className="text-xs">
                      {isPurchase ? 'ผู้ขาย (cust_code, cust_name)' : 'ลูกค้า (cust_code, cust_name)'}
                    </Label>
                    <PartyPicker
                      billType={isPurchase ? 'purchase' : 'sale'}
                      value={party}
					  onChange={(value) => {
						markDirty()
						setParty(value)
					  }}
                    />
                  </div>
                )}
                <div className="space-y-1.5">
                  <div className="flex items-center justify-between gap-2">
                    <Label className="text-xs">คลัง (wh_code)</Label>
                    <Button
                      type="button"
                      variant="ghost"
                      size="sm"
                      className="h-6 px-1.5 text-[11px]"
					  onClick={() => {
						markDirty()
						setManualWarehouse((v) => !v)
					  }}
                    >
                      {manualWarehouse ? 'เลือกจาก SML' : 'พิมพ์รหัสเอง'}
                    </Button>
                  </div>
                  {manualWarehouse ? (
                    <Input
                      value={whCode}
                      onChange={(e) => {
						markDirty()
                        setWhCode(e.target.value.toUpperCase())
                        setShelfCode('')
                      }}
                      placeholder="เช่น WH-01"
                      className="font-mono"
                    />
                  ) : (
                    <WarehousePicker
                      value={whCode}
                      onChange={(warehouse) => {
						markDirty()
                        setWhCode(warehouse.code)
                        setShelfCode('')
                      }}
                    />
                  )}
                </div>
                <div className="space-y-1.5">
                  <Label className="text-xs">พื้นที่เก็บ (shelf_code)</Label>
                  {manualWarehouse ? (
                    <Input
                      value={shelfCode}
					  onChange={(e) => {
						markDirty()
						setShelfCode(e.target.value.toUpperCase())
					  }}
                      placeholder="เช่น SH-01"
                      className="font-mono"
                    />
                  ) : (
                    <ShelfPicker
                      warehouseCode={whCode}
                      value={shelfCode}
					  onChange={(shelf) => {
						markDirty()
						setShelfCode(shelf.code)
					  }}
                    />
                  )}
                </div>
                <div className="space-y-1.5">
                  <Label className="text-xs">ประเภทภาษี (vat_type)</Label>
				  <Select value={vatTypeStr} onValueChange={(value) => {
					markDirty()
					setVatTypeStr(value)
				  }}>
                    <SelectTrigger className="h-10">
                      <SelectValue placeholder="ไม่ระบุ" />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="0">0 — แยกนอก</SelectItem>
                      <SelectItem value="1">1 — รวมใน</SelectItem>
                      <SelectItem value="2">2 — ศูนย์%</SelectItem>
                    </SelectContent>
                  </Select>
                </div>
                <div className="space-y-1.5">
                  <Label className="text-xs">อัตราภาษี (vat_rate)</Label>
                  <Input
                    value={vatRate}
					onChange={(e) => {
					  markDirty()
					  setVatRate(e.target.value)
					}}
                    placeholder="เช่น 7"
                    inputMode="decimal"
                    className="font-mono"
                  />
                </div>
                <div className="space-y-1.5">
                  <Label className="text-xs">ประเภทรายการ (inquiry_type)</Label>
				  <Select value={inquiryTypeStr} onValueChange={(value) => {
					markDirty()
					setInquiryTypeStr(value)
				  }}>
                    <SelectTrigger className="h-10">
                      <SelectValue placeholder="ไม่ระบุ (กรอกตอนส่ง)" />
                    </SelectTrigger>
                    <SelectContent>
                      {isPurchase ? (
                        <>
                          <SelectItem value="0">0 — ซื้อเงินเชื่อ</SelectItem>
                          <SelectItem value="1">1 — ซื้อเงินสด</SelectItem>
                          <SelectItem value="2">2 — ซื้อเงินเชื่อ (สินค้าบริการ)</SelectItem>
                          <SelectItem value="3">3 — ซื้อเงินสด (สินค้าบริการ)</SelectItem>
                        </>
                      ) : (
                        <>
                          <SelectItem value="0">0 — ขายเงินเชื่อ</SelectItem>
                          <SelectItem value="1">1 — ขายเงินสด</SelectItem>
                          <SelectItem value="2">2 — ขายเงินเชื่อ (สินค้าบริการ)</SelectItem>
                          <SelectItem value="3">3 — ขายเงินสด (สินค้าบริการ)</SelectItem>
                        </>
                      )}
                    </SelectContent>
                  </Select>
                </div>
				<div className="space-y-1.5 sm:col-span-2">
				  <Label className="text-xs" htmlFor="channel-remark">หมายเหตุ 1 (remark)</Label>
				  <Input
					id="channel-remark"
					value={remark}
					onChange={(event) => {
					  markDirty()
					  setRemark(event.target.value)
					}}
					placeholder="เช่น {{channel}} | {{order_ref}}"
					aria-invalid={Boolean(remarkError)}
				  />
				  <p className={remarkError ? 'text-xs text-destructive' : 'text-xs text-muted-foreground'}>
					{remarkError || 'ใช้ token ได้: {{channel}}, {{order_ref}}, {{bill_no}}'}
				  </p>
				</div>
				<div className="space-y-1.5 sm:col-span-2">
				  <Label className="text-xs" htmlFor="channel-remark-2">หมายเหตุ 2 (remark_2)</Label>
				  <Input
					id="channel-remark-2"
					value={remark2}
					onChange={(event) => {
					  markDirty()
					  setRemark2(event.target.value)
					}}
					placeholder="ข้อความอิสระ ไม่เกิน 255 ตัวอักษร"
					aria-invalid={Boolean(remark2Error)}
				  />
				  {remark2Error && <p className="text-xs text-destructive">{remark2Error}</p>}
				</div>
                <div className="space-y-1.5">
                  <Label className="text-xs">สาขา (branch_code)</Label>
                  <SMLMasterCodePicker
                    kind="branch"
                    value={branchCode}
					onChange={(value) => {
					  markDirty()
					  setBranchCode(value)
					}}
                  />
                </div>
                <div className="space-y-1.5">
                  <Label className="text-xs">พนักงานขาย (sale_code)</Label>
                  <SMLMasterCodePicker
                    kind="sale"
                    value={saleCode}
					onChange={(value) => {
					  markDirty()
					  setSaleCode(value)
					}}
                  />
                </div>
              </div>
              {(!whCodeTrimmed || !shelfCodeTrimmed || vatTypeStr === '' || vatRateValue < 0 || (isShopeeRealtimeAutoRoute && !party?.code)) && (
                <div className="rounded-md border border-warning/35 bg-warning/[0.08] px-3 py-2 text-xs text-warning">
                  {isShopeeRealtimeAutoRoute
                    ? 'ยังตั้งค่าคงที่สำหรับ Auto SML ไม่ครบ ระบบจะไม่อนุญาตให้บันทึกหรือเปิดใช้งานอัตโนมัติ'
                    : 'ยังตั้งค่า default สำหรับส่ง SML ไม่ครบ บันทึกได้ แต่ตอนส่งบิล user ต้องเลือกค่าที่ขาดก่อนยืนยัน'}
                </div>
              )}
            </div>
            )}

			{!isSettlement && !isShopeeRealtimeCancelRoute && (
			  <div className="space-y-3 rounded-md border border-border bg-background p-3">
				<div className="flex flex-wrap items-start justify-between gap-3">
				  <div>
					<div className="text-sm font-semibold text-foreground">ตรวจตัวอย่าง Document Profile</div>
					<p className="mt-1 max-w-[62ch] text-xs text-muted-foreground">
					  ตรวจค่าที่ resolve แล้วและผลกระทบก่อนบันทึก การเปลี่ยนแปลงนี้มีผลเฉพาะเอกสารใหม่
					</p>
				  </div>
				  <Button
					type="button"
					variant="outline"
					className="gap-2"
					onClick={handlePreview}
					disabled={previewing || saving || Boolean(remarkError || remark2Error)}
				  >
					<Eye className="h-4 w-4" />
					{previewing ? 'กำลังตรวจ...' : 'ตรวจตัวอย่าง'}
				  </Button>
				</div>

				<div className="grid gap-2 text-xs sm:grid-cols-2">
				  <div className="rounded-md bg-muted/50 px-3 py-2">
					<div className="text-muted-foreground">ค่าระบบที่แก้ไขไม่ได้</div>
					<div className="mt-1 font-mono text-foreground">BILLFLOW · NEXFLOW · THB × 1</div>
				  </div>
				  <div className="rounded-md bg-muted/50 px-3 py-2">
					<div className="text-muted-foreground">เวลาเอกสาร</div>
					<div className="mt-1 text-foreground">เวลาจริงขณะส่ง · Asia/Bangkok</div>
				  </div>
				</div>

				{preview ? (
				  <div className="space-y-2" role="status" aria-live="polite">
					<div className="rounded-md border border-success/30 bg-success/5 px-3 py-2 text-xs">
					  <div className="font-medium text-foreground">
						Profile {preview.profile_mode}{preview.profile_version ? ` · ${preview.profile_version}` : ''}
					  </div>
					  <div className="mt-1 break-words text-muted-foreground">
						หมายเหตุ 1: {preview.resolved.remark || 'ไม่ระบุ'}
					  </div>
					  <div className="mt-0.5 break-words text-muted-foreground">
						หมายเหตุ 2: {preview.resolved.remark_2 || 'ไม่ระบุ'}
					  </div>
					  <div className="mt-1 grid gap-1 text-muted-foreground sm:grid-cols-2">
						<div>เอกสาร: {preview.payload.doc_format_code || 'ยังไม่กำหนด'}</div>
						<div>คลัง/ที่เก็บ: {preview.payload.warehouse || '—'} / {preview.payload.location || '—'}</div>
						<div>VAT: type {preview.payload.vat_type} · {preview.payload.vat_rate}%</div>
						<div>ปลายทาง: {preview.payload.endpoint || 'ยังไม่กำหนด'}</div>
					  </div>
					  <div className="mt-1 break-all font-mono text-[10px] text-muted-foreground">
						{preview.system_fields.remark_5}
					  </div>
					  {preview.profile_mode === 'shadow' && (
						<div className="mt-1 text-muted-foreground">
						  Shadow ตรวจสัญญา V1 แล้ว แต่ payload ที่เขียนจริงยังไม่เปิด Document Profile
						</div>
					  )}
					  <div className="mt-1 break-all font-mono text-[10px] text-muted-foreground">
						route {preview.route_signature}
					  </div>
					</div>
					{(preview.missing_prerequisites ?? []).length > 0 && (
					  <div className="rounded-md border border-warning/35 bg-warning/[0.08] px-3 py-2 text-xs text-warning">
						ข้อมูลที่ยังขาด: {(preview.missing_prerequisites ?? []).join(', ')}
					  </div>
					)}
				  </div>
				) : (
				  <p className="text-xs text-muted-foreground" role="status">
					ยังไม่ได้ตรวจตัวอย่าง หลังแก้ไขค่าใด ๆ ต้องตรวจใหม่ก่อนบันทึกเส้นทาง Auto SML
				  </p>
				)}
			  </div>
			)}

            {supportsShippingItem && (
              <div className="space-y-3 rounded-md border border-border bg-muted/20 p-3">
                <div className="flex items-start justify-between gap-3">
                  <div>
                    <div className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">
                      ค่าจัดส่งจาก {shippingChannelLabel}
                    </div>
                    <p className="mt-1 text-xs text-muted-foreground">
                      {isShopeePurchase
                        ? 'ถ้าเปิดใช้ ระบบจะเพิ่มค่าส่งจากเมล Shopee เป็นรายการสินค้าในบิลซื้อใหม่'
                        : `ถ้าไฟล์ ${shippingChannelLabel} มีค่าส่งสุทธิ ระบบจะสร้างเป็นรายการบริการ SML แยกจากสินค้า`}
                    </p>
                  </div>
                  <Switch
                    checked={shippingEnabled}
					onCheckedChange={(value) => {
					  markDirty()
					  setShippingEnabled(value)
					}}
                    aria-label="เพิ่มค่าขนส่งเป็นรายการสินค้า"
                  />
                </div>

                <div className={shippingEnabled ? 'space-y-3' : 'space-y-3 opacity-60'}>
                  <div className="grid gap-3 sm:grid-cols-[minmax(0,1fr)_auto]">
                    <div className="space-y-1">
                      <Label className="text-xs">สินค้า SML สำหรับค่าส่ง</Label>
                      <div className="rounded-md border border-border bg-background px-3 py-2">
                        {shippingItemCodeTrimmed ? (
                          <div className="min-w-0">
                            <code className="font-mono text-sm font-semibold text-foreground">
                              {shippingItemCodeTrimmed}
                            </code>
                            <div className="mt-0.5 truncate text-xs text-muted-foreground">
                              {shippingItemName || 'เลือกไว้แล้ว ระบบจะใช้ชื่อสินค้าจาก SML ตอนแสดงในบิล'}
                            </div>
                          </div>
                        ) : (
                          <span className="text-sm text-muted-foreground">ยังไม่ได้เลือกสินค้า</span>
                        )}
                      </div>
                    </div>
                    <div className="flex items-end">
                      <Button
                        type="button"
                        variant="outline"
                        className="gap-2"
                        onClick={() => setShippingPickerOpen(true)}
                        disabled={!shippingEnabled}
                      >
                        <PackageSearch className="h-4 w-4" />
                        เลือกสินค้า
                      </Button>
                    </div>
                  </div>

                  <div className="space-y-1.5">
                    <Label className="text-xs">หน่วย</Label>
                    <UnitSelect
                      value={shippingItemUnitCode}
					  onValueChange={(value) => {
						markDirty()
						setShippingItemUnitCode(value)
					  }}
                      productCode={shippingItemCodeTrimmed}
                      disabled={!shippingEnabled || !shippingItemCodeTrimmed}
                      autoSelectSingle
                    />
                  </div>
                </div>

                {shippingEnabled && !shippingItemCodeTrimmed && (
                  <div className="rounded-md border border-warning/40 bg-warning/10 px-3 py-2 text-xs text-warning">
                    ต้องเลือกสินค้า SML ก่อนบันทึก เช่น สินค้าบริการที่ร้านตั้งไว้สำหรับค่าขนส่ง
                  </div>
                )}
              </div>
            )}
          </div>

          <DialogFooter className="items-start gap-2 sm:items-center sm:justify-between">
            {saveDisabledReason && (
              <p className="max-w-[20rem] text-left text-[11px] leading-relaxed text-muted-foreground">
                {saveDisabledReason}
              </p>
            )}
            <div className="flex gap-2">
              <Button variant="outline" onClick={() => onOpenChange(false)} disabled={saving}>
                ยกเลิก
              </Button>
              <Button onClick={handleSave} disabled={!canSave} title={saveDisabledReason || undefined}>
                {saving ? 'กำลังบันทึก...' : 'บันทึก'}
              </Button>
            </div>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {supportsShippingItem && (
        <MapItemModal
          open={open && shippingPickerOpen}
          rawName={`ค่าจัดส่ง ${shippingChannelLabel}`}
          currentCode={shippingItemCode}
          currentUnit={shippingItemUnitCode}
          currentPrice={0}
          rawNameLabel={`รายการค่าจัดส่งจาก ${shippingChannelLabel}`}
          onPick={handleShippingPick}
          onClose={() => setShippingPickerOpen(false)}
        />
      )}
    </>
  )
}
