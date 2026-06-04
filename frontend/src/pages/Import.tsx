import { useState, useCallback } from 'react'
import { Link } from 'react-router-dom'
import { useDropzone } from 'react-dropzone'
import {
  AlertCircle,
  AlertTriangle,
  ArrowRight,
  Construction,
  FileSpreadsheet,
  Settings2,
  ShieldCheck,
  ShoppingBag,
  Upload,
} from 'lucide-react'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import { Checkbox } from '@/components/ui/checkbox'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { PageHeader } from '@/components/common/PageHeader'
import client from '@/api/client'
import { cn } from '@/lib/utils'
import { useAuth } from '@/hooks/useAuth'
import type { BillPreview, ImportConfirmResponse } from '@/types'
import { LazadaColumnMapping } from './Import/LazadaColumnMapping'

const PHASE = Number(import.meta.env.VITE_PHASE ?? 99)

type Step = 'idle' | 'uploading' | 'preview' | 'confirming' | 'result'

function AnomalyBadges({
  anomalies,
  hasBlock,
}: {
  anomalies: BillPreview['anomalies']
  hasBlock: boolean
}) {
  if (!anomalies?.length && !hasBlock) return null
  return (
    <TooltipProvider delayDuration={0}>
      <div className="flex flex-wrap gap-1">
        {anomalies?.map((a, i) => {
          const isBlock = a.severity === 'block'
          return (
            <Tooltip key={i}>
              <TooltipTrigger asChild>
                <Badge
                  variant={isBlock ? 'destructive' : 'secondary'}
                  className="cursor-help font-normal"
                >
                  {isBlock ? '🔴' : '🟡'} {a.code}
                </Badge>
              </TooltipTrigger>
              <TooltipContent className="max-w-xs">
                {a.message}
              </TooltipContent>
            </Tooltip>
          )
        })}
      </div>
    </TooltipProvider>
  )
}

export default function Import() {
  const { user } = useAuth()
  const [step, setStep] = useState<Step>('idle')
  const [platform, setPlatform] = useState<'lazada' | 'shopee'>('lazada')
  const [billType, setBillType] = useState<'sale' | 'purchase'>('sale')
  const [bills, setBills] = useState<BillPreview[]>([])
  const [selectedIds, setSelectedIds] = useState<Set<string>>(new Set())
  const [result, setResult] = useState<ImportConfirmResponse | null>(null)
  const [errorMsg, setErrorMsg] = useState<string | null>(null)

  const lazadaDisabled = platform === 'lazada'

  const toggleSelect = (id: string) => {
    setSelectedIds((prev) => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }

  const toggleAll = () => {
    const confirmable = bills.filter((b) => !b.has_block).map((b) => b.bill_id)
    if (selectedIds.size === confirmable.length) setSelectedIds(new Set())
    else setSelectedIds(new Set(confirmable))
  }

  const onDrop = useCallback(
    async (files: File[]) => {
      if (!files.length) return
      setStep('uploading')
      setErrorMsg(null)
      try {
        const form = new FormData()
        form.append('file', files[0])
        form.append('platform', platform)
        form.append('bill_type', billType)
        const res = await client.post('/api/import/upload', form, {
          headers: { 'Content-Type': 'multipart/form-data' },
        })
        const data = res.data
        setBills(data.bills || [])
        const preselected = (data.bills as BillPreview[])
          .filter((b) => !b.has_block)
          .map((b) => b.bill_id)
        setSelectedIds(new Set(preselected))
        setStep('preview')
      } catch (e: unknown) {
        const err = e as { response?: { data?: { error?: string } } }
        setErrorMsg(err?.response?.data?.error ?? 'อัปโหลดไม่สำเร็จ')
        setStep('idle')
      }
    },
    [platform, billType],
  )

  const { getRootProps, getInputProps, isDragActive } = useDropzone({
    onDrop,
    accept: {
      'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet': ['.xlsx'],
      'application/vnd.ms-excel': ['.xls'],
    },
    maxFiles: 1,
    disabled: step === 'uploading' || lazadaDisabled,
  })

  const handleConfirm = async () => {
    if (selectedIds.size === 0) return
    setStep('confirming')
    try {
      const res = await client.post<ImportConfirmResponse>('/api/import/confirm', {
        bill_ids: Array.from(selectedIds),
      })
      setResult(res.data)
      setStep('result')
    } catch (e: unknown) {
      const err = e as { response?: { data?: { error?: string } } }
      setErrorMsg(err?.response?.data?.error ?? 'ยืนยันไม่สำเร็จ')
      setStep('preview')
    }
  }

  const reset = () => {
    setStep('idle')
    setBills([])
    setSelectedIds(new Set())
    setResult(null)
    setErrorMsg(null)
  }

  const confirmable = bills.filter((b) => !b.has_block)
  const blocked = bills.filter((b) => b.has_block)
  const legacyOpen = step !== 'idle' || Boolean(errorMsg)

  const importChannels = [
    {
      title: 'Shopee',
      description: 'เชื่อมร้านหรือดึงรายการจากไฟล์ แล้วตรวจรายการก่อนสร้างเอกสาร',
      to: '/import/shopee',
      badge: 'ใช้งานหลัก',
      route: 'Shopee -> ขายสินค้าและบริการ / SI',
      primary: true,
    },
    {
      title: 'Lazada Excel',
      description: 'อัปโหลดไฟล์จาก Seller Center และสร้างคิวเอกสารให้ตรวจต่อ',
      to: '/import/lazada',
      badge: 'ไฟล์ Excel',
      route: 'Lazada -> ขายสินค้าและบริการ / SI',
      primary: false,
    },
    {
      title: 'TikTok Excel',
      description: 'อัปโหลด Excel/CSV จาก TikTok และกันรายการซ้ำก่อนสร้างเอกสาร',
      to: '/import/tiktok',
      badge: 'ไฟล์ Excel/CSV',
      route: 'TikTok -> ขายสินค้าและบริการ / SI',
      primary: false,
    },
  ]

  return (
    <div className="space-y-5">
      <PageHeader
        title="ศูนย์นำเข้า Marketplace"
        description="เลือกช่องทางนำเข้า แล้วตรวจรายการก่อนสร้างเอกสารขายและส่งเข้า SML"
      />

      <div className="rounded-lg border border-border bg-card p-4 shadow-sm">
        <div className="grid gap-4 lg:grid-cols-[minmax(0,1fr)_auto] lg:items-center">
          <div className="min-w-0">
            <div className="flex flex-wrap items-center gap-2">
              <Badge className="bg-primary text-primary-foreground hover:bg-primary">
                เส้นทางใช้งานหลัก
              </Badge>
              <Badge variant="outline">ขายสินค้าและบริการ / SI</Badge>
            </div>
            <h2 className="mt-3 text-base font-semibold text-foreground">
              Marketplace → ตรวจรายการ → สร้างเอกสาร → ส่ง SML
            </h2>
            <p className="mt-1 max-w-3xl text-sm leading-relaxed text-muted-foreground">
              หน้านี้เป็นจุดเลือกช่องทางนำเข้า ไม่ใช่หน้าส่ง SML โดยตรง เพื่อให้คนทำงานเห็นชัดว่าต้องตรวจข้อมูลก่อนสร้างเอกสารจริง
            </p>
          </div>
          <Button asChild className="w-full lg:w-auto">
            <Link to="/import/shopee">
              ตรวจรายการ Shopee
              <ArrowRight className="h-4 w-4" />
            </Link>
          </Button>
        </div>
      </div>

      <div className="grid gap-3 md:grid-cols-3">
        {importChannels.map((channel) => (
          <Link
            key={channel.to}
            to={channel.to}
            className="group rounded-lg border border-border bg-card p-4 transition-colors hover:border-accent-strong/40 hover:bg-muted/30"
          >
            <div className="flex items-start justify-between gap-3">
              <div className="flex h-9 w-9 items-center justify-center rounded-md bg-muted text-accent-strong">
                <ShoppingBag className="h-4 w-4" />
              </div>
              <Badge variant={channel.primary ? 'default' : 'secondary'}>
                {channel.badge}
              </Badge>
            </div>
            <div className="mt-4 min-w-0">
              <div className="flex items-center gap-2">
                <h3 className="text-sm font-semibold text-foreground">{channel.title}</h3>
                <ArrowRight className="h-3.5 w-3.5 text-muted-foreground transition-transform group-hover:translate-x-0.5" />
              </div>
              <p className="mt-1 text-xs leading-relaxed text-muted-foreground">
                {channel.description}
              </p>
              <div className="mt-3 rounded-md bg-muted/45 px-2.5 py-1.5 text-[11px] text-foreground">
                {channel.route}
              </div>
            </div>
          </Link>
        ))}
      </div>

      <details className="rounded-lg border border-border bg-card p-4">
        <summary className="flex cursor-pointer list-none items-center justify-between gap-3 text-sm font-semibold text-foreground">
          <span className="inline-flex items-center gap-2">
            <Settings2 className="h-4 w-4 text-muted-foreground" />
            รายละเอียดสำหรับแอดมิน: Column Mapping Lazada
          </span>
          <Badge variant="secondary">พับไว้</Badge>
        </summary>
        <div className="mt-4">
          <LazadaColumnMapping platform="lazada" adminOnly={user?.role === 'admin'} />
        </div>
      </details>

      <details open={legacyOpen} className="rounded-lg border border-warning/30 bg-warning/[0.04] p-4">
        <summary className="flex cursor-pointer list-none items-center justify-between gap-3 text-sm font-semibold text-foreground">
          <span className="inline-flex items-center gap-2">
            <ShieldCheck className="h-4 w-4 text-warning" />
            เครื่องมือเก่า: อัปโหลดแล้วส่ง SML โดยตรง
          </span>
          <Badge variant="outline" className="border-warning/40 text-warning">
            ใช้เมื่อจำเป็น
          </Badge>
        </summary>

        <Alert className="mt-4 border-warning/30 bg-warning/[0.05]">
          <AlertTriangle className="h-4 w-4 text-warning" />
          <AlertTitle>เครื่องมือนี้ไม่ใช่ flow หลักสำหรับ production</AlertTitle>
          <AlertDescription>
            หลังยืนยัน ระบบจะส่งเข้า SML ทันที ใช้เฉพาะกรณีที่ต้องใช้ endpoint เก่าและตรวจไฟล์เรียบร้อยแล้ว หากเป็นงานประจำให้ใช้ Shopee, Lazada Excel หรือ TikTok Excel ด้านบน
          </AlertDescription>
        </Alert>

      {(step === 'idle' || step === 'uploading') && (
        <>
          {lazadaDisabled && (
            <Alert>
              <Construction className="h-4 w-4" />
              <AlertTitle>Legacy Lazada direct send ไม่ใช่ช่องทางหลัก</AlertTitle>
              {PHASE < 2 ? (
                <AlertDescription>
                  ระบบ production ให้ใช้เมนู Lazada Excel ที่สร้างรายการให้ตรวจก่อนส่งเข้า SML
                </AlertDescription>
              ) : (
                <AlertDescription>
                  ถ้าต้องนำเข้า Lazada ให้ใช้{' '}
                  <Link to="/import/lazada" className="font-medium text-link hover:underline">
                    Lazada Excel
                  </Link>{' '}
                  ที่มีขั้นตรวจรายการก่อนสร้างเอกสาร
                </AlertDescription>
              )}
            </Alert>
          )}

          <Card>
            <CardContent className="grid grid-cols-1 gap-4 p-5 sm:grid-cols-2">
              <div className="space-y-1.5">
                <Label htmlFor="platform">Platform</Label>
                <Select
                  value={platform}
                  onValueChange={(v) => setPlatform(v as 'lazada' | 'shopee')}
                >
                  <SelectTrigger id="platform">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="lazada">Lazada</SelectItem>
                    <SelectItem value="shopee">Shopee</SelectItem>
                  </SelectContent>
                </Select>
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="bill-type">ประเภทบิล</Label>
                <Select
                  value={billType}
                  onValueChange={(v) => setBillType(v as 'sale' | 'purchase')}
                >
                  <SelectTrigger id="bill-type">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="sale">บิลขาย (Sale)</SelectItem>
                    <SelectItem value="purchase">บิลซื้อ (Purchase)</SelectItem>
                  </SelectContent>
                </Select>
              </div>
            </CardContent>
          </Card>

          <div
            {...getRootProps()}
            className={cn(
              'flex flex-col items-center justify-center rounded-lg border-2 border-dashed transition-colors',
              'min-h-[220px] cursor-pointer p-8 text-center',
              isDragActive
                ? 'border-primary bg-primary/5'
                : 'border-border bg-muted/20 hover:bg-muted/40',
              (step === 'uploading' || lazadaDisabled) && 'cursor-not-allowed opacity-60',
            )}
            aria-disabled={lazadaDisabled}
          >
            <input {...getInputProps()} />
            {step === 'uploading' ? (
              <p className="text-sm text-muted-foreground">กำลังประมวลผล…</p>
            ) : isDragActive ? (
              <p className="text-sm font-medium text-accent-strong">วางไฟล์ที่นี่</p>
            ) : (
              <>
                <FileSpreadsheet className="mb-3 h-10 w-10 text-muted-foreground" />
                <p className="text-sm font-medium text-foreground">
                  ลากไฟล์ Excel มาวาง หรือคลิกเพื่อเลือก
                </p>
                <p className="mt-1 text-xs text-muted-foreground">รองรับ .xlsx, .xls</p>
              </>
            )}
          </div>

          {errorMsg && (
            <Alert variant="destructive">
              <AlertCircle className="h-4 w-4" />
              <AlertDescription>{errorMsg}</AlertDescription>
            </Alert>
          )}
        </>
      )}

      {step === 'preview' && (
        <>
          <Card>
            <CardContent className="flex flex-wrap items-center justify-between gap-3 p-4">
              <div className="flex items-baseline gap-3 text-sm">
                <span className="font-semibold text-foreground">
                  {bills.length} ออเดอร์
                </span>
                <span className="text-success">พร้อมยืนยัน {confirmable.length}</span>
                {blocked.length > 0 && (
                  <span className="text-destructive">บล็อก {blocked.length}</span>
                )}
              </div>
              <div className="flex gap-2">
                <Button variant="outline" size="sm" onClick={reset}>
                  อัปโหลดใหม่
                </Button>
                <Button
                  size="sm"
                  onClick={handleConfirm}
                  disabled={selectedIds.size === 0}
                >
                  ยืนยัน {selectedIds.size} ออเดอร์
                </Button>
              </div>
            </CardContent>
          </Card>

          {errorMsg && (
            <Alert variant="destructive">
              <AlertCircle className="h-4 w-4" />
              <AlertDescription>{errorMsg}</AlertDescription>
            </Alert>
          )}

          <div className="overflow-hidden rounded-lg border border-border bg-card">
            <Table>
              <TableHeader>
                <TableRow className="bg-muted/40">
                  <TableHead className="w-10">
                    <Checkbox
                      checked={
                        selectedIds.size === confirmable.length && confirmable.length > 0
                      }
                      onCheckedChange={toggleAll}
                      aria-label="เลือกทั้งหมด"
                    />
                  </TableHead>
                  <TableHead>หมายเลขออเดอร์</TableHead>
                  <TableHead>ชื่อลูกค้า</TableHead>
                  <TableHead className="text-center">รายการ</TableHead>
                  <TableHead className="text-center">จับคู่</TableHead>
                  <TableHead className="text-right">ยอดรวม</TableHead>
                  <TableHead>Anomaly</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {bills.map((bill) => {
                  const checked = selectedIds.has(bill.bill_id)
                  return (
                    <TableRow
                      key={bill.bill_id}
                      className={cn(
                        bill.has_block && 'bg-destructive/5 text-muted-foreground',
                        checked && !bill.has_block && 'bg-primary/5',
                      )}
                    >
                      <TableCell>
                        <Checkbox
                          checked={checked}
                          disabled={bill.has_block}
                          onCheckedChange={() => toggleSelect(bill.bill_id)}
                        />
                      </TableCell>
                      <TableCell className="font-mono text-xs">
                        {bill.order_id || '—'}
                      </TableCell>
                      <TableCell>{bill.customer_name}</TableCell>
                      <TableCell className="text-center">{bill.item_count}</TableCell>
                      <TableCell className="text-center">
                        <span
                          className={cn(
                            'tabular-nums',
                            bill.mapped_count < bill.item_count
                              ? 'text-warning'
                              : 'text-success',
                          )}
                        >
                          {bill.mapped_count}/{bill.item_count}
                        </span>
                      </TableCell>
                      <TableCell className="text-right tabular-nums">
                        {bill.total_amount > 0
                          ? `฿${bill.total_amount.toLocaleString('th-TH', {
                              minimumFractionDigits: 2,
                            })}`
                          : '—'}
                      </TableCell>
                      <TableCell>
                        <AnomalyBadges
                          anomalies={bill.anomalies}
                          hasBlock={bill.has_block}
                        />
                      </TableCell>
                    </TableRow>
                  )
                })}
              </TableBody>
            </Table>
          </div>
        </>
      )}

      {step === 'confirming' && (
        <Card>
          <CardContent className="flex items-center justify-center gap-2 p-8 text-sm text-muted-foreground">
            <Upload className="h-4 w-4 animate-pulse" />
            กำลังส่งไปยัง SML ERP… โปรดรอสักครู่
          </CardContent>
        </Card>
      )}

      {step === 'result' && result && (
        <>
          <div className="grid grid-cols-2 gap-4">
            <Card className="border-success/30 bg-success/5">
              <CardContent className="p-5 text-center">
                <p className="text-3xl font-semibold tabular-nums text-success">
                  {result.success}
                </p>
                <p className="mt-1 text-xs font-medium text-muted-foreground">
                  สำเร็จ
                </p>
              </CardContent>
            </Card>
            <Card className="border-destructive/30 bg-destructive/5">
              <CardContent className="p-5 text-center">
                <p className="text-3xl font-semibold tabular-nums text-destructive">
                  {result.failed}
                </p>
                <p className="mt-1 text-xs font-medium text-muted-foreground">
                  ล้มเหลว
                </p>
              </CardContent>
            </Card>
          </div>

          {result.errors?.length > 0 && (
            <>
              <h3 className="flex items-center gap-2 text-sm font-semibold">
                <AlertTriangle className="h-4 w-4 text-destructive" />
                รายการที่ล้มเหลว
              </h3>
              <div className="overflow-hidden rounded-lg border border-border bg-card">
                <Table>
                  <TableHeader>
                    <TableRow className="bg-muted/40">
                      <TableHead>Bill ID</TableHead>
                      <TableHead>สาเหตุ</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {result.errors.map((e, i) => (
                      <TableRow key={i}>
                        <TableCell className="font-mono text-xs">{e.bill_id}</TableCell>
                        <TableCell className="text-destructive">{e.reason}</TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              </div>
            </>
          )}

          <Button onClick={reset}>นำเข้าไฟล์ใหม่</Button>
        </>
      )}
      </details>
    </div>
  )
}
