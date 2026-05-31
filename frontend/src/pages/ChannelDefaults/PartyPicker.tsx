import { useEffect, useMemo, useRef, useState } from 'react'
import { Check, ChevronsUpDown, Plus, RefreshCw, Search } from 'lucide-react'
import { toast } from 'sonner'

import client from '@/api/client'
import { ConfirmDialog } from '@/components/common/ConfirmDialog'
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
  Popover,
  PopoverContent,
  PopoverTrigger,
} from '@/components/ui/popover'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Textarea } from '@/components/ui/textarea'
import { cn } from '@/lib/utils'
import { humanizeSMLConnectionError } from '@/lib/sml-readiness'
import dayjs from 'dayjs'

export interface Party {
  code: string
  name: string
  name_1?: string
  name_eng_1?: string
  first_name?: string
  last_name?: string
  firstname?: string
  lastname?: string
  tax_id?: string
  card_id?: string
  branch_type?: number
  branch_code?: string
  ar_status?: number
  ap_status?: number
  telephone?: string
  address?: string
  remark?: string
}

interface PartyPickerProps {
  billType: 'sale' | 'purchase'
  value: Party | null
  onChange: (p: Party) => void
  disabled?: boolean
}

const HIDDEN_CODE_CHARS = /[\u0000-\u001F\u007F\u200B-\u200D\u2060\uFEFF\u0300-\u036F\u0E31\u0E34-\u0E3A\u0E47-\u0E4E]/
const HIDDEN_CODE_CHARS_GLOBAL = /[\u0000-\u001F\u007F\u200B-\u200D\u2060\uFEFF\u0300-\u036F\u0E31\u0E34-\u0E3A\u0E47-\u0E4E]/g

function normalizeCodeInput(value: string) {
  return value.toUpperCase()
}

function cleanCodeSuggestion(value: string) {
  return value.replace(HIDDEN_CODE_CHARS_GLOBAL, '').trim().toUpperCase()
}

function isExactCodeMatch(party: Party, code: string) {
  return party.code?.trim().toUpperCase() === code
}

function createErrorMessage(error: any) {
  const status = error?.response?.status
  const raw = error?.response?.data?.error
  const message = (typeof raw === 'string' ? raw : raw?.message) ?? error?.message ?? ''
  const lower = String(message).toLowerCase()

  if (
    status === 409 ||
    lower.includes('duplicate') ||
    lower.includes('unique') ||
    lower.includes('already exists') ||
    lower.includes('มีอยู่')
  ) {
    return 'รหัสนี้มีใน SML แล้ว ให้เลือกจากรายการแทน'
  }

  return humanizeSMLConnectionError(message || 'สร้างข้อมูลไม่สำเร็จ')
}

// Searchable combobox over /api/sml/customers or /api/sml/suppliers.
// Backend caches both lists in memory + scores results by relevance.
export function PartyPicker({ billType, value, onChange, disabled }: PartyPickerProps) {
  const [open, setOpen] = useState(false)
  const [createDialogOpen, setCreateDialogOpen] = useState(false)
  const [query, setQuery] = useState('')
  const [results, setResults] = useState<Party[]>([])
  const [loading, setLoading] = useState(false)
  const [refreshing, setRefreshing] = useState(false)
  const [lastSync, setLastSync] = useState<string | null>(null)
  const [syncStatus, setSyncStatus] = useState<string>('not_ready')
  const [syncError, setSyncError] = useState('')
  const [total, setTotal] = useState(0)

  const endpoint = billType === 'purchase' ? '/api/sml/suppliers' : '/api/sml/customers'
  const partyLabel = billType === 'purchase' ? 'ผู้ขาย' : 'ลูกค้า'
  const masterLabel = billType === 'purchase' ? 'เจ้าหนี้' : 'ลูกหนี้'
  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  const fetchResults = useMemo(
    () =>
      (q: string) => {
        setLoading(true)
        client
          .get<{
            data: Party[]
            total: number
            last_sync: string | null
            status?: string
            error?: string
          }>(
            `${endpoint}?search=${encodeURIComponent(q)}&limit=20`,
          )
          .then((r) => {
            setResults(r.data.data ?? [])
            setTotal(r.data.total ?? 0)
            setLastSync(r.data.last_sync)
            setSyncStatus(r.data.status ?? (r.data.last_sync ? 'ok' : 'not_ready'))
            setSyncError(r.data.error ?? '')
          })
          .catch((e) => {
            setResults([])
            setSyncStatus('error')
            setSyncError(humanizeSMLConnectionError(e?.response?.data?.error ?? e?.message ?? 'โหลดรายชื่อไม่สำเร็จ'))
          })
          .finally(() => setLoading(false))
      },
    [endpoint],
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
        customers: number
        suppliers: number
        last_sync: string | null
        status?: string
      }>('/api/sml/refresh-parties')
      setLastSync(r.data.last_sync)
      setSyncStatus(r.data.status ?? 'ok')
      setSyncError('')
      toast.success(`ซิงก์เสร็จ — ${r.data.customers} ลูกค้า / ${r.data.suppliers} ผู้ขาย`)
      fetchResults(query)
    } catch (e: any) {
      const msg = humanizeSMLConnectionError(e?.response?.data?.error ?? e?.message ?? 'unknown')
      setSyncStatus('error')
      setSyncError(msg)
      toast.error('รีเฟรชไม่สำเร็จ: ' + msg)
    } finally {
      setRefreshing(false)
    }
  }

  const startCreate = () => {
    setOpen(false)
    setCreateDialogOpen(true)
  }

  const handleCreated = (party: Party) => {
    onChange(party)
    setQuery(party.code)
    setResults((prev) => [party, ...prev.filter((p) => p.code !== party.code)])
  }

  return (
    <>
      <Popover modal open={open} onOpenChange={setOpen}>
        <PopoverTrigger asChild>
          <Button
            variant="outline"
            role="combobox"
            aria-expanded={open}
            className="w-full justify-between font-normal"
            disabled={disabled}
          >
            {value ? (
              <span className="flex items-center gap-2 truncate text-left">
                <span className="font-mono text-xs text-muted-foreground">{value.code}</span>
                <span className="truncate">{value.name}</span>
              </span>
            ) : (
              <span className="text-muted-foreground">
                เลือก{partyLabel}…
              </span>
            )}
            <ChevronsUpDown className="ml-2 h-4 w-4 shrink-0 opacity-50" />
          </Button>
        </PopoverTrigger>
        <PopoverContent className="w-[min(480px,calc(100vw-2rem))] p-0" align="start">
          <div className="flex items-center gap-2 border-b border-border p-2">
            <div className="relative min-w-0 flex-1">
              <Search className="pointer-events-none absolute left-3 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" />
              <input
                autoFocus
                value={query}
                onChange={(e) => setQuery(e.target.value)}
                placeholder="ค้นหาด้วยรหัส / ชื่อ / เลขภาษี / บัตรประชาชน…"
                className="h-9 w-full rounded-md border border-input bg-background px-9 text-sm placeholder:text-muted-foreground focus:outline-none focus:ring-2 focus:ring-ring"
              />
              {loading && (
                <div className="absolute right-3 top-1/2 h-3.5 w-3.5 -translate-y-1/2 animate-spin rounded-full border-2 border-muted-foreground/30 border-t-muted-foreground" />
              )}
            </div>
            <Button
              type="button"
              variant="outline"
              size="sm"
              className="h-9 shrink-0 gap-1.5 px-2.5 text-xs"
              onClick={startCreate}
            >
              <Plus className="h-3.5 w-3.5" />
              สร้าง{partyLabel}ใหม่
            </Button>
          </div>

          <div
            className="overflow-y-auto py-1"
            style={{ maxHeight: 'min(360px, var(--radix-popover-content-available-height, 360px))' }}
          >
            {results.length === 0 && !loading && (
              <div className="px-3 py-6 text-center text-sm text-muted-foreground">
                {query
                  ? `ไม่พบข้อมูล — กดสร้าง${partyLabel}ใหม่หรือรีเฟรช`
                  : billType === 'purchase'
                    ? 'ยังไม่มีผู้ขายในแคช — กดรีเฟรช'
                    : 'ยังไม่มีลูกค้าในแคช — กดรีเฟรช'}
              </div>
            )}
            {results.map((p) => {
              const isSelected = value?.code === p.code
              return (
                <button
                  key={p.code}
                  type="button"
                  onClick={() => {
                    onChange(p)
                    setOpen(false)
                  }}
                  className={cn(
                    'flex w-full items-start gap-3 px-3 py-2 text-left text-sm hover:bg-accent',
                    isSelected && 'bg-accent',
                  )}
                >
                  <Check
                    className={cn(
                      'mt-1 h-4 w-4 shrink-0',
                      isSelected ? 'opacity-100' : 'opacity-0',
                    )}
                  />
                  <div className="min-w-0 flex-1">
                    <div className="flex items-center gap-2">
                      <span className="font-mono text-xs text-muted-foreground">
                        {p.code}
                      </span>
                      <span className="truncate font-medium">{p.name}</span>
                    </div>
                    {(p.tax_id || p.telephone || p.address) && (
                      <div className="mt-0.5 truncate text-xs text-muted-foreground">
                        {p.tax_id && <span>tax: {p.tax_id} · </span>}
                        {p.telephone && <span>โทร {p.telephone} · </span>}
                        {p.address && <span className="truncate">{p.address}</span>}
                      </div>
                    )}
                  </div>
                </button>
              )
            })}
          </div>

          <div className="flex items-center justify-between gap-2 border-t border-border bg-muted/30 px-3 py-2 text-xs text-muted-foreground">
            <span className={cn((syncStatus === 'error' || syncStatus === 'not_ready') && 'text-warning')}>
              {total.toLocaleString()} รายการ
              {lastSync ? (
                <> · ซิงก์ล่าสุด {dayjs(lastSync).format('HH:mm')}</>
              ) : (
                <> · ยังไม่เคยซิงก์สำเร็จ</>
              )}
              {syncStatus === 'error' && syncError ? <> · {syncError}</> : null}
            </span>
            <Button
              variant="ghost"
              size="sm"
              className="h-7 gap-1.5 px-2 text-xs"
              onClick={handleRefresh}
              disabled={refreshing}
            >
              <RefreshCw className={cn('h-3 w-3', refreshing && 'animate-spin')} />
              รีเฟรช
            </Button>
          </div>
        </PopoverContent>
      </Popover>

      <PartyCreateDialog
        open={createDialogOpen}
        onOpenChange={setCreateDialogOpen}
        billType={billType}
        endpoint={endpoint}
        partyLabel={partyLabel}
        masterLabel={masterLabel}
        initialName={query.trim()}
        knownResults={results}
        onCreated={handleCreated}
      />
    </>
  )
}

interface PartyCreateDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  billType: 'sale' | 'purchase'
  endpoint: string
  partyLabel: string
  masterLabel: string
  initialName: string
  knownResults: Party[]
  onCreated: (party: Party) => void
}

function PartyCreateDialog({
  open,
  onOpenChange,
  billType,
  endpoint,
  partyLabel,
  masterLabel,
  initialName,
  knownResults,
  onCreated,
}: PartyCreateDialogProps) {
  const [createCode, setCreateCode] = useState('')
  const [createName, setCreateName] = useState('')
  const [createAPStatus, setCreateAPStatus] = useState<'0' | '1'>('1')
  const [createFirstname, setCreateFirstname] = useState('')
  const [createLastname, setCreateLastname] = useState('')
  const [createNameEng1, setCreateNameEng1] = useState('')
  const [createAddress, setCreateAddress] = useState('')
  const [createTaxID, setCreateTaxID] = useState('')
  const [createBranchType, setCreateBranchType] = useState<'0' | '1'>('0')
  const [createBranchCode, setCreateBranchCode] = useState('00000')
  const [createCardID, setCreateCardID] = useState('')
  const [createRemark, setCreateRemark] = useState('')
  const [creating, setCreating] = useState(false)
  const [createError, setCreateError] = useState('')
  const [discardConfirmOpen, setDiscardConfirmOpen] = useState(false)

  const statusFieldName = billType === 'purchase' ? 'ap_status' : 'ar_status'
  const firstNameFieldName = billType === 'purchase' ? 'firstname' : 'first_name'
  const lastNameFieldName = billType === 'purchase' ? 'lastname' : 'last_name'
  const companyNameMax = billType === 'purchase' ? 100 : 255
  const cardIDMax = billType === 'purchase' ? 50 : 100
  const formId = billType === 'purchase' ? 'create-sml-supplier-form' : 'create-sml-customer-form'

  useEffect(() => {
    if (!open) return
    setCreateCode('')
    setCreateAPStatus('1')
    setCreateFirstname('')
    setCreateLastname('')
    setCreateName(initialName)
    setCreateNameEng1('')
    setCreateAddress('')
    setCreateTaxID('')
    setCreateBranchType('0')
    setCreateBranchCode('00000')
    setCreateCardID('')
    setCreateRemark('')
    setCreateError('')
    setDiscardConfirmOpen(false)
    setCreating(false)
  }, [open, initialName, billType])

  const cleanCreateCode = cleanCodeSuggestion(createCode)
  const hasDirtyCreateCode =
    createCode !== createCode.trim() ||
    HIDDEN_CODE_CHARS.test(createCode)

  const partyCreateReady = (
    cleanCreateCode !== '' &&
    !hasDirtyCreateCode &&
    (createAPStatus === '0'
      ? createFirstname.trim() !== '' && createLastname.trim() !== ''
      : createName.trim() !== '') &&
    (createBranchType === '0' || createBranchCode.trim() !== '')
  )
  const canCreate = partyCreateReady

  const hasDraft = [
    createCode,
    createName,
    createFirstname,
    createLastname,
    createNameEng1,
    createAddress,
    createTaxID,
    createBranchType === '1' ? createBranchCode : '',
    createCardID,
    createRemark,
  ].some((value) => value.trim() !== '') || createAPStatus !== '1'

  const closeWithoutPrompt = () => {
    setDiscardConfirmOpen(false)
    onOpenChange(false)
  }

  const requestClose = () => {
    if (creating) return
    if (hasDraft) {
      setDiscardConfirmOpen(true)
      return
    }
    onOpenChange(false)
  }

  const findDuplicateCode = async (code: string) => {
    const localDuplicate = knownResults.find((party) => isExactCodeMatch(party, code))
    if (localDuplicate) return localDuplicate

    const r = await client.get<{ data: Party[] }>(
      `${endpoint}?search=${encodeURIComponent(code)}&limit=5`,
    )
    return (r.data.data ?? []).find((party) => isExactCodeMatch(party, code))
  }

  const handleCreate = async () => {
    const code = cleanCreateCode
    if (!canCreate) return
    setCreating(true)
    setCreateError('')
    try {
      const duplicate = await findDuplicateCode(code)
      if (duplicate) {
        const msg = 'รหัสนี้มีใน SML แล้ว ให้เลือกจากรายการแทน'
        setCreateError(msg)
        toast.error(`สร้าง${partyLabel}ไม่สำเร็จ`, { description: msg })
        return
      }

      const payload = billType === 'purchase'
        ? {
            code,
            ap_status: Number(createAPStatus),
            firstname: createAPStatus === '0' ? createFirstname.trim() : '',
            lastname: createAPStatus === '0' ? createLastname.trim() : '',
            name_1: createAPStatus === '1' ? createName.trim() : '',
            name_eng_1: createAPStatus === '1' ? createNameEng1.trim() : '',
            address: createAddress.trim(),
            tax_id: createTaxID.trim(),
            branch_type: Number(createBranchType),
            branch_code: createBranchType === '0' ? '00000' : createBranchCode.trim(),
            card_id: createCardID.trim(),
            remark: createRemark.trim(),
          }
        : {
            code,
            ar_status: Number(createAPStatus),
            first_name: createAPStatus === '0' ? createFirstname.trim() : '',
            last_name: createAPStatus === '0' ? createLastname.trim() : '',
            name_1: createAPStatus === '1' ? createName.trim() : '',
            name_eng_1: createAPStatus === '1' ? createNameEng1.trim() : '',
            address: createAddress.trim(),
            tax_id: createTaxID.trim(),
            branch_type: Number(createBranchType),
            branch_code: createBranchType === '0' ? '00000' : createBranchCode.trim(),
            card_id: createCardID.trim(),
            remark: createRemark.trim(),
          }
      const r = await client.post<{ party: Party }>(endpoint, payload)
      const created = r.data.party
      const firstName = created.first_name ?? created.firstname ?? createFirstname
      const lastName = created.last_name ?? created.lastname ?? createLastname
      const party = {
        ...created,
        code: created.code || code,
        name: created.name || created.name_1 || `${firstName} ${lastName}`.trim() || createName.trim(),
      }
      onCreated(party)
      onOpenChange(false)
      toast.success(`สร้าง${partyLabel}แล้ว`, { description: `${party.code} · ${party.name}` })
    } catch (e: any) {
      const msg = createErrorMessage(e)
      setCreateError(msg)
      toast.error(`สร้าง${partyLabel}ไม่สำเร็จ`, { description: msg })
    } finally {
      setCreating(false)
    }
  }

  return (
    <>
      <Dialog
        open={open}
        onOpenChange={(nextOpen) => {
          if (nextOpen) {
            onOpenChange(true)
            return
          }
          requestClose()
        }}
      >
        <DialogContent className="grid max-h-[90vh] grid-rows-[auto_minmax(0,1fr)_auto] overflow-hidden sm:max-w-2xl">
          <DialogHeader>
            <DialogTitle>สร้าง{masterLabel}ใหม่</DialogTitle>
            <DialogDescription>
              บันทึกลง SML แล้วเลือกเป็น{partyLabel}ในเอกสารนี้ทันที
            </DialogDescription>
          </DialogHeader>

          <form
            id={formId}
            className="-mx-6 space-y-4 overflow-y-auto px-6 py-2"
            onSubmit={(e) => {
              e.preventDefault()
              handleCreate()
            }}
          >
            <div className="space-y-1.5">
              <Label className="text-xs text-muted-foreground">
                รหัส{partyLabel} (code) <span className="text-destructive">*</span>
              </Label>
              <Input
                autoFocus
                value={createCode}
                onChange={(e) => setCreateCode(normalizeCodeInput(e.target.value))}
                placeholder={billType === 'purchase' ? 'เช่น VNEW01' : 'เช่น ARNEW01'}
                className="font-mono"
                maxLength={25}
              />
              {hasDirtyCreateCode && (
                <p className="text-[11px] text-destructive">
                  รหัสห้ามมีช่องว่างหน้า/หลัง หรืออักขระซ่อน
                  {cleanCreateCode ? <> · ควรใช้ <span className="font-mono">{cleanCreateCode}</span></> : null}
                </p>
              )}
            </div>

            <div className="grid gap-3 sm:grid-cols-2">
              <div className="space-y-1.5">
                <Label className="text-xs text-muted-foreground">
                  ชนิด{masterLabel} ({statusFieldName}) <span className="text-destructive">*</span>
                </Label>
                <Select
                  value={createAPStatus}
                  onValueChange={(value) => {
                    const next = value as '0' | '1'
                    setCreateAPStatus(next)
                    if (next === '0') {
                      setCreateName('')
                      setCreateNameEng1('')
                    } else {
                      setCreateFirstname('')
                      setCreateLastname('')
                    }
                  }}
                >
                  <SelectTrigger className="h-9 text-sm">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="1">นิติบุคคล</SelectItem>
                    <SelectItem value="0">บุคคลธรรมดา</SelectItem>
                  </SelectContent>
                </Select>
              </div>
              <div className="space-y-1.5">
                <Label className="text-xs text-muted-foreground">
                  ประเภทสาขา (branch_type) <span className="text-destructive">*</span>
                </Label>
                <Select
                  value={createBranchType}
                  onValueChange={(value) => {
                    const next = value as '0' | '1'
                    setCreateBranchType(next)
                    setCreateBranchCode(next === '0' ? '00000' : '')
                  }}
                >
                  <SelectTrigger className="h-9 text-sm">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="0">สำนักงานใหญ่</SelectItem>
                    <SelectItem value="1">สาขา</SelectItem>
                  </SelectContent>
                </Select>
              </div>
            </div>

            {createAPStatus === '0' ? (
              <div className="grid gap-3 sm:grid-cols-2">
                <div className="space-y-1.5">
                  <Label className="text-xs text-muted-foreground">
                    ชื่อ ({firstNameFieldName}) <span className="text-destructive">*</span>
                  </Label>
                  <Input value={createFirstname} onChange={(e) => setCreateFirstname(e.target.value)} maxLength={50} />
                </div>
                <div className="space-y-1.5">
                  <Label className="text-xs text-muted-foreground">
                    นามสกุล ({lastNameFieldName}) <span className="text-destructive">*</span>
                  </Label>
                  <Input value={createLastname} onChange={(e) => setCreateLastname(e.target.value)} maxLength={70} />
                </div>
              </div>
            ) : (
              <div className="grid gap-3 sm:grid-cols-2">
                <div className="space-y-1.5">
                  <Label className="text-xs text-muted-foreground">
                    ชื่อ (name_1) <span className="text-destructive">*</span>
                  </Label>
                  <Input value={createName} onChange={(e) => setCreateName(e.target.value)} placeholder={`ชื่อ${masterLabel}ใน SML`} maxLength={companyNameMax} />
                </div>
                <div className="space-y-1.5">
                  <Label className="text-xs text-muted-foreground">ชื่อภาษาอังกฤษ (name_eng_1)</Label>
                  <Input value={createNameEng1} onChange={(e) => setCreateNameEng1(e.target.value)} maxLength={companyNameMax} />
                </div>
              </div>
            )}

            <div className="grid gap-3 sm:grid-cols-2">
              <div className="space-y-1.5">
                <Label className="text-xs text-muted-foreground">เลขผู้เสียภาษี (tax_id)</Label>
                <Input value={createTaxID} onChange={(e) => setCreateTaxID(e.target.value)} inputMode="numeric" maxLength={50} />
              </div>
              <div className="space-y-1.5">
                <Label className="text-xs text-muted-foreground">เลขบัตรประชาชน (card_id)</Label>
                <Input value={createCardID} onChange={(e) => setCreateCardID(e.target.value)} inputMode="numeric" maxLength={cardIDMax} />
              </div>
            </div>

            <div className="space-y-1.5">
              <Label className="text-xs text-muted-foreground">
                รหัสสาขา (branch_code){createBranchType === '1' && <span className="text-destructive"> *</span>}
              </Label>
              <Input
                value={createBranchCode}
                onChange={(e) => setCreateBranchCode(e.target.value.trim())}
                className="font-mono"
                readOnly={createBranchType === '0'}
                maxLength={25}
              />
            </div>

            <div className="space-y-1.5">
              <Label className="text-xs text-muted-foreground">ที่อยู่ (address)</Label>
              <Textarea
                value={createAddress}
                onChange={(e) => setCreateAddress(e.target.value)}
                maxLength={255}
                rows={3}
              />
            </div>
            <div className="space-y-1.5">
              <Label className="text-xs text-muted-foreground">หมายเหตุ (remark)</Label>
              <Textarea
                value={createRemark}
                onChange={(e) => setCreateRemark(e.target.value)}
                maxLength={255}
                rows={3}
              />
            </div>

            {createError && (
              <div className="rounded-md border border-destructive/30 bg-destructive/5 px-3 py-2 text-xs text-destructive">
                {createError}
              </div>
            )}
          </form>

          <DialogFooter className="gap-2 sm:gap-2">
            <Button
              type="button"
              variant="outline"
              disabled={creating}
              onClick={requestClose}
            >
              ยกเลิก
            </Button>
            <Button
              type="submit"
              form={formId}
              className="gap-1.5"
              disabled={creating || !canCreate}
            >
              <Plus className="h-4 w-4" />
              {creating ? 'กำลังสร้าง...' : `สร้างและเลือก${partyLabel}`}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <ConfirmDialog
        open={discardConfirmOpen}
        onOpenChange={setDiscardConfirmOpen}
        title="ทิ้งข้อมูลที่กรอกไว้?"
        description="ฟอร์มนี้ยังไม่ได้บันทึก ถ้าปิดตอนนี้ข้อมูลที่กรอกไว้จะหาย"
        confirmLabel="ทิ้งข้อมูล"
        cancelLabel="ทำต่อ"
        variant="destructive"
        onConfirm={closeWithoutPrompt}
      />
    </>
  )
}
