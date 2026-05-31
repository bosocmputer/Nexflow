import { useEffect, useState } from 'react'
import { PackageSearch } from 'lucide-react'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
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
import { REMARK2_NONE, SML_REMARK2_OPTIONS, normalizeRemark2 } from '@/lib/smlRemark2'
import { MapItemModal } from '../BillDetail/components/MapItemModal'

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
  const [remark2Str, setRemark2Str] = useState(REMARK2_NONE)
  const [passbookCode, setPassbookCode] = useState('')
  const [expenseCode, setExpenseCode] = useState('')
  const [passbooks, setPassbooks] = useState<SMLMasterOption[]>([])
  const [expenses, setExpenses] = useState<SMLMasterOption[]>([])
  const [settlementMastersLoading, setSettlementMastersLoading] = useState(false)
  const [saving, setSaving] = useState(false)

  useEffect(() => {
    if (!open || !row) return
    const detectedDestination = destinationFor(
      row.channel as ChannelKey,
      row.bill_type,
      row.endpoint ?? '',
      row.doc_format_code ?? '',
    )
    const defaultDestination = destinationOptionsFor(row.bill_type)[0]
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
    setRemark2Str(normalizeRemark2(row.remark_2 || ''))
    setPassbookCode(row.passbook_code || '')
    setExpenseCode(row.expense_code || '')
  }, [open, row])

  // Fetch doc formats from SML when destination changes; auto-fill prefix + running format from selected format
  useEffect(() => {
    if (!open) return
    let cancelled = false
    const screenCodeMap: Record<EndpointKind, string> = {
      saleorder: 'SR',
      saleinvoice: 'SI',
      purchaseorder: 'PO',
      arreceipt: 'EE',
    }
    const screenCode = screenCodeMap[selectedDestination]
    if (!screenCode) return
    setDocFormatsLoading(true)
    client.get(`/api/sml/doc-formats?screen_code=${screenCode}`)
      .then((res) => {
        if (cancelled) return
        const formats: SmlDocFormat[] = res.data?.data ?? []
        setDocFormats(formats)
        if (formats.length === 0) return
        // Keep current selection if still in list; otherwise default to first
        const current = formats.find((f) => f.code === selectedDocFormatCode)
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
  }, [open, selectedDestination, selectedDocFormatCode])

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
  const showPartyPicker =
    (row.channel === 'shopee_shipped' && row.bill_type === 'purchase') ||
    (row.channel === 'shopee' && row.bill_type === 'sale') ||
    (row.channel === 'lazada' && row.bill_type === 'sale') ||
    (row.channel === 'tiktok' && row.bill_type === 'sale')
  const channelLabel = isShopeePurchase
    ? 'Email บิลซื้อ Shopee'
    : CHANNEL_LABELS[row.channel as ChannelKey] ?? row.channel
  const billTypeLabel = isPurchase ? 'บิลซื้อ' : isSettlement ? 'ลูกหนี้' : 'บิลขาย'
  const destinationOptions = destinationOptionsFor(row.bill_type)
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
  const docWarning = docNoPatternWarning(docPrefixTrimmed, docRunningFormatTrimmed)
  const selectedPassbook = passbooks.find((p) => p.code === passbookCodeTrimmed)
  const selectedExpense = expenses.find((p) => p.code === expenseCodeTrimmed)
  const canSave =
    !!selectedDestinationMeta &&
    (isSettlement || (
      docPrefixTrimmed !== '' &&
      docRunningFormatTrimmed !== '' &&
      docRunningFormatTrimmed.includes('#')
    )) &&
    (!isSettlement || (selectedDocFormatCode !== '' && passbookCodeTrimmed !== '')) &&
    (!isShopeePurchase || !shippingEnabled || shippingItemCodeTrimmed !== '') &&
    (isSettlement || !docWarning) &&
    !saving

  const handleDestinationChange = (value: EndpointKind) => {
    const destination = destinationOptions.find((option) => option.value === value)
    setSelectedDestination(value)
    setSelectedDocFormatCode('') // reset — useEffect will re-fetch and select first
    if (!destination) return
    setDocPrefix(destination.docPrefix)
    setDocRunningFormat(destination.docRunningFormat)
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
    if (!isSettlement && (!docPrefixTrimmed || !docRunningFormatTrimmed || !docRunningFormatTrimmed.includes('#'))) {
      toast.error('เลือกรูปแบบเอกสารก่อน ระบบจะดึง prefix และรูปแบบเลขรันจาก SML ให้อัตโนมัติ')
      return
    }
    if (!isSettlement && docWarning) {
      toast.error('แก้รูปแบบเลขเอกสารตามคำเตือนก่อนบันทึก')
      return
    }
    if (isShopeePurchase && shippingEnabled && !shippingItemCodeTrimmed) {
      toast.error('กรุณาเลือกสินค้า SML สำหรับค่าขนส่งก่อนเปิดใช้งาน')
      return
    }
    setSaving(true)
    try {
      await client.put('/api/settings/channel-defaults', {
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
        shipping_item_enabled: isShopeePurchase ? shippingEnabled : false,
        shipping_item_code: isShopeePurchase ? shippingItemCodeTrimmed : '',
        shipping_item_unit_code: isShopeePurchase ? shippingItemUnitCodeTrimmed : '',
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
        remark_2: isSettlement ? '' : (remark2Str === REMARK2_NONE ? '' : remark2Str),
      })
      toast.success('บันทึกสำเร็จ')
      onSaved()
      onOpenChange(false)
    } catch (e: any) {
      toast.error('บันทึกล้มเหลว: ' + (e?.response?.data?.error ?? e?.message ?? 'unknown'))
    } finally {
      setSaving(false)
    }
  }

  const handleShippingPick = (code: string, unitCode: string, picked?: CatalogMatch) => {
    setShippingItemCode(code)
    setShippingItemUnitCode(unitCode || '')
    setShippingItemName(picked?.item_name || '')
    setShippingPickerOpen(false)
  }

  return (
    <>
      <Dialog
        open={open}
        onOpenChange={(v) => {
          if (!v) setShippingPickerOpen(false)
          onOpenChange(v)
        }}
      >
        <DialogContent className="grid max-h-[90vh] max-w-xl grid-rows-[auto_minmax(0,1fr)_auto]">
          <DialogHeader>
            <DialogTitle>
              ตั้งค่าเส้นทาง SML สำหรับ {channelLabel} ({billTypeLabel})
            </DialogTitle>
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
                <div className="rounded-md border border-border bg-muted/30 px-3 py-2 font-mono text-sm text-foreground">
                  {selectedDocFormatCode || selectedDestinationMeta?.docFormatCode || '-'}
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
                  <div className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">
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
                    <select
                      className="h-10 w-full rounded-md border border-input bg-background px-3 text-sm"
                      value={passbookCode}
                      disabled={settlementMastersLoading}
                      onChange={(e) => setPassbookCode(e.target.value)}
                    >
                      <option value="">เลือกบัญชีรับเงินจาก SML</option>
                      {passbooks.map((p) => (
                        <option key={p.code} value={p.code}>
                          {p.code} · {p.name_1}{p.bank_code ? ` · ${p.bank_code}` : ''}{p.bank_branch ? ` ${p.bank_branch}` : ''}
                        </option>
                      ))}
                    </select>
                    <p className="text-[11px] text-muted-foreground">
                      {passbookCodeTrimmed
                        ? 'ค่าที่เลือกนี้จะถูกบันทึกเป็นบัญชีรับเงินจริงสำหรับรับชำระ Shopee'
                        : 'รายการในช่องนี้เป็นตัวเลือกจาก SML ยังไม่ใช่ค่าที่บันทึก จนกว่าจะเลือกแล้วกดบันทึก'}
                    </p>
                  </div>
                  <div className="space-y-1.5">
                    <Label className="text-xs">ส่วนต่าง Shopee</Label>
                    <select
                      className="h-10 w-full rounded-md border border-input bg-background px-3 text-sm"
                      value={expenseCode}
                      disabled={settlementMastersLoading}
                      onChange={(e) => setExpenseCode(e.target.value)}
                    >
                      <option value="">ยังไม่กำหนดค่าใช้จ่ายส่วนต่าง</option>
                      {expenses.map((p) => (
                        <option key={p.code} value={p.code}>
                          {p.code} · {p.name_1}
                        </option>
                      ))}
                    </select>
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
                <div className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">
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
                <b>ตัวอย่างถัดไป:</b>{' '}
                <code className="rounded bg-background px-1.5 py-0.5 font-mono text-foreground">
                  {previewDocNo(docPrefixTrimmed || 'BF', docRunningFormatTrimmed || 'YYMM####')}
                </code>
              </div>
            </div>
            )}

            {!isSettlement && (
            <div className="space-y-3 rounded-md border border-border bg-muted/20 p-3">
              <div>
                <div className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">
                  ค่าเริ่มต้นตอนส่ง SML
                </div>
                <p className="mt-1 text-xs text-muted-foreground">
                  ค่าชุดนี้จะถูกเติมใน dialog ส่งบิลให้ user เห็นก่อนกดยืนยัน ถ้าเว้นว่าง ระบบจะให้ user เลือกเองก่อนส่ง
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
                      onChange={setParty}
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
                      onClick={() => setManualWarehouse((v) => !v)}
                    >
                      {manualWarehouse ? 'เลือกจาก SML' : 'พิมพ์รหัสเอง'}
                    </Button>
                  </div>
                  {manualWarehouse ? (
                    <Input
                      value={whCode}
                      onChange={(e) => {
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
                      onChange={(e) => setShelfCode(e.target.value.toUpperCase())}
                      placeholder="เช่น SH-01"
                      className="font-mono"
                    />
                  ) : (
                    <ShelfPicker
                      warehouseCode={whCode}
                      value={shelfCode}
                      onChange={(shelf) => setShelfCode(shelf.code)}
                    />
                  )}
                </div>
                <div className="space-y-1.5">
                  <Label className="text-xs">ประเภทภาษี (vat_type)</Label>
                  <Select value={vatTypeStr} onValueChange={setVatTypeStr}>
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
                    onChange={(e) => setVatRate(e.target.value)}
                    placeholder="เช่น 7"
                    inputMode="decimal"
                    className="font-mono"
                  />
                </div>
                <div className="space-y-1.5">
                  <Label className="text-xs">ประเภทรายการ (inquiry_type)</Label>
                  <Select value={inquiryTypeStr} onValueChange={setInquiryTypeStr}>
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
                <div className="space-y-1.5">
                  <Label className="text-xs">สถานะเอกสาร (remark_2)</Label>
                  <Select value={remark2Str} onValueChange={setRemark2Str}>
                    <SelectTrigger className="h-10">
                      <SelectValue placeholder="ไม่ระบุ (กรอกตอนส่ง)" />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value={REMARK2_NONE}>ไม่ระบุ</SelectItem>
                      {SML_REMARK2_OPTIONS.map((o) => (
                        <SelectItem key={o.value} value={o.value}>{o.label}</SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>
                <div className="space-y-1.5">
                  <Label className="text-xs">สาขา (branch_code)</Label>
                  <SMLMasterCodePicker
                    kind="branch"
                    value={branchCode}
                    onChange={setBranchCode}
                  />
                </div>
                <div className="space-y-1.5">
                  <Label className="text-xs">พนักงานขาย (sale_code)</Label>
                  <SMLMasterCodePicker
                    kind="sale"
                    value={saleCode}
                    onChange={setSaleCode}
                  />
                </div>
              </div>
              {(!whCodeTrimmed || !shelfCodeTrimmed || vatTypeStr === '' || vatRateValue < 0) && (
                <div className="rounded-md border border-warning/35 bg-warning/[0.08] px-3 py-2 text-xs text-warning">
                  ยังตั้งค่า default สำหรับส่ง SML ไม่ครบ บันทึกได้ แต่ตอนส่งบิล user ต้องเลือกค่าที่ขาดก่อนยืนยัน
                </div>
              )}
            </div>
            )}

            {isShopeePurchase && (
              <div className="space-y-3 rounded-md border border-border bg-muted/20 p-3">
                <div className="flex items-start justify-between gap-3">
                  <div>
                    <div className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">
                      ค่าขนส่งจาก Shopee
                    </div>
                    <p className="mt-1 text-xs text-muted-foreground">
                      ถ้าเปิดใช้ ระบบจะเพิ่มค่าส่งจากเมล Shopee เป็นรายการสินค้าในบิลซื้อใหม่.
                      ถ้าปิดไว้ จะไม่เพิ่มรายการค่าส่งใด ๆ
                    </p>
                  </div>
                  <Switch
                    checked={shippingEnabled}
                    onCheckedChange={setShippingEnabled}
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
                      onValueChange={setShippingItemUnitCode}
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

          <DialogFooter>
            <Button variant="outline" onClick={() => onOpenChange(false)} disabled={saving}>
              ยกเลิก
            </Button>
            <Button onClick={handleSave} disabled={!canSave}>
              {saving ? 'กำลังบันทึก...' : 'บันทึก'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {isShopeePurchase && (
        <MapItemModal
          open={open && shippingPickerOpen}
          rawName="ค่าขนส่งสินค้า"
          currentCode={shippingItemCode}
          currentUnit={shippingItemUnitCode}
          currentPrice={0}
          rawNameLabel="รายการค่าส่งจาก Shopee"
          onPick={handleShippingPick}
          onClose={() => setShippingPickerOpen(false)}
        />
      )}
    </>
  )
}
