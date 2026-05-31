import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { AlertTriangle, ArrowRight, BookOpen, Check, Pencil, Plus, Search, Trash2, X } from 'lucide-react'
import { toast } from 'sonner'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import LearningProgress from '@/components/LearningProgress'
import { ConfirmDialog } from '@/components/common/ConfirmDialog'
import { EmptyState } from '@/components/common/EmptyState'
import { PageHeader } from '@/components/common/PageHeader'
import { Skeleton } from '@/components/ui/skeleton'
import { MapItemModal } from '@/pages/BillDetail/components/MapItemModal'
import { getBill } from '@/hooks/useBills'

const PHASE = Number(import.meta.env.VITE_PHASE ?? 99)
import client from '@/api/client'
import { PAGE_TITLE } from '@/lib/labels'
import type { Bill, BillListResponse, Mapping, MappingStats } from '@/types'

interface MappingDraft {
  raw_name: string
  item_code: string
  unit_code: string
}

interface ReviewHotspot {
  rawName: string
  count: number
  billCount: number
  firstBillId: string
  firstRoute: string
}

const emptyDraft: MappingDraft = { raw_name: '', item_code: '', unit_code: '' }

export default function Mappings() {
  const [mappings, setMappings] = useState<Mapping[]>([])
  const [stats, setStats] = useState<MappingStats | null>(null)
  const [loading, setLoading] = useState(true)
  const [editId, setEditId] = useState<string | null>(null)
  const [editDraft, setEditDraft] = useState<MappingDraft>(emptyDraft)
  const [newMapping, setNewMapping] = useState<MappingDraft>(emptyDraft)
  const [adding, setAdding] = useState(false)
  const [deleteId, setDeleteId] = useState<string | null>(null)
  const [showAddMapModal, setShowAddMapModal] = useState(false)
  const [reviewHotspots, setReviewHotspots] = useState<ReviewHotspot[]>([])
  const [hotspotLoading, setHotspotLoading] = useState(false)

  const detailPathFor = (billId: string, route: string) => {
    if (route === 'saleinvoice') return `/sale-invoices/${billId}`
    if (route === 'saleorder') return `/sales-orders/${billId}`
    return `/bills/${billId}`
  }

  const fetchReviewHotspots = async () => {
    setHotspotLoading(true)
    try {
      const res = await client.get<BillListResponse>('/api/bills?status=needs_review&page=1&per_page=50')
      const list = res.data.data ?? []
      const details = await Promise.all(list.slice(0, 30).map((bill) => getBill(bill.id)))
      const grouped = new Map<string, ReviewHotspot>()

      details.forEach((bill: Bill) => {
        const route = bill.document_route || bill.preview?.route || (bill.bill_type === 'sale' ? 'saleorder' : 'purchaseorder')
        const seenInBill = new Set<string>()
        ;(bill.items ?? []).forEach((item) => {
          if (item.mapped && item.item_code) return
          const rawName = item.raw_name.trim()
          if (!rawName) return
          const existing = grouped.get(rawName) ?? {
            rawName,
            count: 0,
            billCount: 0,
            firstBillId: bill.id,
            firstRoute: route,
          }
          existing.count += 1
          if (!seenInBill.has(rawName)) {
            existing.billCount += 1
            seenInBill.add(rawName)
          }
          grouped.set(rawName, existing)
        })
      })

      setReviewHotspots(
        [...grouped.values()]
          .sort((a, b) => b.billCount - a.billCount || b.count - a.count || a.rawName.localeCompare(b.rawName))
          .slice(0, 5),
      )
    } catch {
      setReviewHotspots([])
    } finally {
      setHotspotLoading(false)
    }
  }

  const fetchAll = async () => {
    setLoading(true)
    try {
      const [mRes, sRes] = await Promise.all([
        client.get<{ data: Mapping[] }>('/api/mappings'),
        client.get<MappingStats>('/api/mappings/stats'),
      ])
      setMappings(mRes.data.data ?? [])
      setStats(sRes.data)
      void fetchReviewHotspots()
    } catch {
      toast.error('โหลด mapping ไม่สำเร็จ')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    fetchAll()
  }, [])

  const startEdit = (m: Mapping) => {
    setEditId(m.id)
    setEditDraft({ raw_name: m.raw_name, item_code: m.item_code, unit_code: m.unit_code })
  }

  const handleSave = async (id: string) => {
    if (
      !editDraft.raw_name.trim() ||
      !editDraft.item_code.trim() ||
      !editDraft.unit_code.trim()
    ) {
      toast.error('กรอกครบทั้ง 3 ช่องก่อนบันทึก')
      return
    }
    try {
      await client.put(`/api/mappings/${id}`, editDraft)
      setEditId(null)
      fetchAll()
      toast.success('บันทึกสำเร็จ')
    } catch {
      toast.error('บันทึกไม่สำเร็จ')
    }
  }

  const handleDelete = async () => {
    if (!deleteId) return
    try {
      await client.delete(`/api/mappings/${deleteId}`)
      fetchAll()
      toast.success('ลบสำเร็จ')
    } catch {
      toast.error('ลบไม่สำเร็จ')
    }
  }

  const handleAdd = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!newMapping.raw_name || !newMapping.item_code || !newMapping.unit_code) return
    setAdding(true)
    try {
      await client.post('/api/mappings', newMapping)
      setNewMapping(emptyDraft)
      fetchAll()
      toast.success('เพิ่ม mapping สำเร็จ')
    } catch {
      toast.error('เพิ่ม mapping ไม่สำเร็จ')
    } finally {
      setAdding(false)
    }
  }

  return (
    <div className="space-y-6">
      <PageHeader
        title={PAGE_TITLE.mappings}
        description="ความจำกลางสำหรับจับคู่ชื่อสินค้าจากทุกช่องทางกับรหัสสินค้าใน SML"
      />

      {/* Mappings vs Catalog — admin context. Without this admins assume
          they're the same thing (both relate "name → SML code") when
          they actually serve different stages of the matching pipeline. */}
      <div className="rounded-lg border border-info/30 bg-info/[0.04] p-3.5 text-sm">
        <div className="flex items-start gap-2.5">
          <BookOpen className="mt-0.5 h-4 w-4 shrink-0 text-info" strokeWidth={2.25} />
          <div className="min-w-0 flex-1 space-y-1.5">
            <p className="font-medium text-foreground">
              ตารางนี้คืออะไร?
            </p>
            <p className="text-[13px] leading-relaxed text-muted-foreground">
              เก็บคู่ <span className="font-medium text-foreground">"ชื่อสินค้าจากเอกสารนำเข้า → รหัสสินค้าใน SML"</span>{' '}
              ที่ผู้ใช้เคยยืนยันแล้ว ใช้ร่วมกันทั้งฝั่งซื้อและฝั่งขาย เพื่อให้เอกสารรอบถัดไปจับคู่สินค้าได้แม่นขึ้น
              และเรียนรู้ทุกครั้งที่แก้สินค้าใน{' '}
              <Link to="/bills" className="font-medium text-primary hover:underline">ใบสั่งซื้อ</Link>
              {' '}หรือ{' '}
              <Link to="/sales-orders" className="font-medium text-primary hover:underline">ใบสั่งขาย</Link>
            </p>
            <p className="text-[12px] text-muted-foreground">
              ต่างจาก{' '}
              <Link to="/settings/catalog" className="font-medium text-primary hover:underline">
                สินค้าใน SML
              </Link>{' '}
              ที่เป็นรายการสินค้าหลักทั้งหมดจาก SML — ตารางนี้คือความจำจากการตรวจเอกสาร ส่วนสินค้าใน SML คือฐานข้อมูลสินค้าอ้างอิง
            </p>
          </div>
        </div>
      </div>

      <div className="grid grid-cols-1 gap-6 lg:grid-cols-[1fr_320px]">
        {/* Table */}
        <div className="overflow-hidden rounded-lg border border-border bg-card">
          <Table>
            <TableHeader>
              <TableRow className="bg-muted/40 hover:bg-muted/40">
                <TableHead className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">
                  ชื่อดิบ
                </TableHead>
                <TableHead className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">
                  รหัสสินค้า
                </TableHead>
                <TableHead className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">
                  หน่วย
                </TableHead>
                <TableHead className="text-center text-xs font-semibold uppercase tracking-wide text-muted-foreground">
                  แหล่ง
                </TableHead>
                <TableHead className="text-center text-xs font-semibold uppercase tracking-wide text-muted-foreground">
                  ใช้
                </TableHead>
                <TableHead className="w-[120px] text-center text-xs font-semibold uppercase tracking-wide text-muted-foreground">
                  จัดการ
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {loading ? (
                Array.from({ length: 6 }).map((_, i) => (
                  <TableRow key={i}>
                    <TableCell colSpan={6}>
                      <Skeleton className="h-5 w-full" />
                    </TableCell>
                  </TableRow>
                ))
              ) : mappings.length === 0 ? (
                <TableRow>
                  <TableCell colSpan={6} className="py-12">
                    <EmptyState
                      title="ยังไม่มี mapping"
                      description="ระบบจะเรียนรู้อัตโนมัติเมื่อ admin ยืนยันบิลที่รอตรวจสอบ — หรือเพิ่ม mapping เองจากฟอร์มด้านขวา"
                      action={
                        <Button asChild variant="outline" size="sm">
                          <Link to="/bills?status=needs_review">
                            ไปยืนยันบิลที่รอตรวจสอบ
                          </Link>
                        </Button>
                      }
                    />
                  </TableCell>
                </TableRow>
              ) : (
                mappings.map((m) => {
                  const isEditing = editId === m.id
                  return (
                    <TableRow key={m.id} className="h-12">
                      <TableCell>
                        {isEditing ? (
                          <Input
                            value={editDraft.raw_name}
                            onChange={(e) =>
                              setEditDraft((d) => ({ ...d, raw_name: e.target.value }))
                            }
                            className="h-8"
                          />
                        ) : (
                          <span className="text-sm">{m.raw_name}</span>
                        )}
                      </TableCell>
                      <TableCell>
                        {isEditing ? (
                          <Input
                            value={editDraft.item_code}
                            onChange={(e) =>
                              setEditDraft((d) => ({ ...d, item_code: e.target.value }))
                            }
                            className="h-8 font-mono text-xs"
                            autoFocus
                          />
                        ) : (
                          <span className="font-mono text-xs font-medium">{m.item_code}</span>
                        )}
                      </TableCell>
                      <TableCell>
                        {isEditing ? (
                          <Input
                            value={editDraft.unit_code}
                            onChange={(e) =>
                              setEditDraft((d) => ({ ...d, unit_code: e.target.value }))
                            }
                            className="h-8 w-20"
                          />
                        ) : (
                          <span className="text-sm text-muted-foreground">
                            {m.unit_code || '—'}
                          </span>
                        )}
                      </TableCell>
                      <TableCell className="text-center">
                        <Badge
                          variant={m.source === 'ai_learned' ? 'default' : 'secondary'}
                          className={
                            m.source === 'ai_learned'
                              ? 'bg-success/15 text-success hover:bg-success/20'
                              : ''
                          }
                        >
                          {m.source === 'ai_learned' ? 'ระบบเรียนรู้' : 'เพิ่มเอง'}
                        </Badge>
                      </TableCell>
                      <TableCell className="text-center text-xs tabular-nums text-muted-foreground">
                        {m.usage_count}
                      </TableCell>
                      <TableCell>
                        <div className="flex items-center justify-center gap-1">
                          {isEditing ? (
                            <>
                              <Button
                                size="icon"
                                variant="default"
                                className="h-7 w-7"
                                onClick={() => handleSave(m.id)}
                                title="บันทึก"
                              >
                                <Check className="h-3.5 w-3.5" />
                              </Button>
                              <Button
                                size="icon"
                                variant="outline"
                                className="h-7 w-7"
                                onClick={() => setEditId(null)}
                                title="ยกเลิก"
                              >
                                <X className="h-3.5 w-3.5" />
                              </Button>
                            </>
                          ) : (
                            <>
                              <Button
                                size="icon"
                                variant="ghost"
                                className="h-7 w-7"
                                onClick={() => startEdit(m)}
                                title="แก้ไข"
                              >
                                <Pencil className="h-3.5 w-3.5" />
                              </Button>
                              <Button
                                size="icon"
                                variant="ghost"
                                className="h-7 w-7 text-muted-foreground hover:text-destructive"
                                onClick={() => setDeleteId(m.id)}
                                title="ลบ"
                              >
                                <Trash2 className="h-3.5 w-3.5" />
                              </Button>
                            </>
                          )}
                        </div>
                      </TableCell>
                    </TableRow>
                  )
                })
              )}
            </TableBody>
          </Table>
        </div>

        {/* Sidebar */}
        <div className="space-y-4">
          <Card>
            <CardHeader className="pb-3">
              <CardTitle className="flex items-center gap-2 text-sm font-semibold">
                <AlertTriangle className="h-4 w-4 text-warning" />
                จุดที่ยังต้องจับคู่
              </CardTitle>
            </CardHeader>
            <CardContent className="space-y-3">
              <div className="text-xs leading-relaxed text-muted-foreground">
                รวมจากบิลสถานะต้องตรวจสินค้า เพื่อให้เห็นชื่อสินค้าที่ควรยืนยันก่อน จะลดงานเลือกซ้ำในบิลถัดไป
              </div>
              {hotspotLoading ? (
                <div className="space-y-2">
                  {Array.from({ length: 3 }).map((_, i) => (
                    <Skeleton key={i} className="h-10 w-full" />
                  ))}
                </div>
              ) : reviewHotspots.length === 0 ? (
                <div className="rounded-md border border-success/25 bg-success/[0.06] px-3 py-2 text-xs text-success">
                  ตอนนี้ยังไม่พบชื่อสินค้าค้างจับคู่ในบิลที่ต้องตรวจ
                </div>
              ) : (
                <div className="space-y-2">
                  {reviewHotspots.map((item) => (
                    <Link
                      key={item.rawName}
                      to={detailPathFor(item.firstBillId, item.firstRoute)}
                      className="group block rounded-md border border-border bg-background px-3 py-2 transition-colors hover:bg-accent/60"
                    >
                      <div className="flex items-start justify-between gap-2">
                        <div className="min-w-0">
                          <div className="line-clamp-2 text-xs font-medium text-foreground">
                            {item.rawName}
                          </div>
                          <div className="mt-1 text-[11px] text-muted-foreground">
                            {item.count} แถว · {item.billCount} บิล
                          </div>
                        </div>
                        <ArrowRight className="mt-0.5 h-3.5 w-3.5 shrink-0 text-muted-foreground transition-transform group-hover:translate-x-0.5" />
                      </div>
                    </Link>
                  ))}
                </div>
              )}
            </CardContent>
          </Card>

          <Card>
            <CardHeader className="pb-3">
              <CardTitle className="flex items-center gap-2 text-sm font-semibold">
                <Plus className="h-4 w-4" />
                เพิ่มคู่จับคู่ใหม่
              </CardTitle>
            </CardHeader>
            <CardContent>
              <form onSubmit={handleAdd} className="space-y-3">
                <div className="space-y-1.5">
                  <Label htmlFor="m-raw">ชื่อดิบ</Label>
                  <Input
                    id="m-raw"
                    placeholder={PHASE >= 2 ? "ชื่อสินค้าจาก LINE / Email" : "ชื่อสินค้าจาก Email"}
                    value={newMapping.raw_name}
                    onChange={(e) =>
                      setNewMapping((p) => ({ ...p, raw_name: e.target.value }))
                    }
                    required
                  />
                </div>
                <div className="space-y-1.5">
                  <Label htmlFor="m-code">รหัสสินค้าใน SML</Label>
                  <div className="flex gap-2">
                    <Input
                      id="m-code"
                      placeholder="เลือกจากสินค้าใน SML"
                      className="font-mono"
                      value={newMapping.item_code}
                      onChange={(e) =>
                        setNewMapping((p) => ({ ...p, item_code: e.target.value }))
                      }
                      required
                    />
                    <Button
                      type="button"
                      variant="outline"
                      className="shrink-0"
                      onClick={() => setShowAddMapModal(true)}
                    >
                      <Search className="h-4 w-4" />
                      เลือกจาก SML
                    </Button>
                  </div>
                </div>
                <div className="space-y-1.5">
                  <Label htmlFor="m-unit">หน่วย</Label>
                  <Input
                    id="m-unit"
                    placeholder="เช่น ถุง, เส้น"
                    value={newMapping.unit_code}
                    onChange={(e) =>
                      setNewMapping((p) => ({ ...p, unit_code: e.target.value }))
                    }
                    required
                  />
                </div>
                <Button type="submit" disabled={adding} className="w-full">
                  {adding ? 'กำลังเพิ่ม…' : 'เพิ่มคู่จับคู่'}
                </Button>
              </form>
            </CardContent>
          </Card>

          {stats && <LearningProgress stats={stats} />}
        </div>
      </div>

      <ConfirmDialog
        open={deleteId !== null}
        onOpenChange={(o) => !o && setDeleteId(null)}
        title="ลบคู่จับคู่นี้?"
        description="หลังลบแล้วระบบจะไม่ใช้คู่นี้อีก แต่จะเรียนรู้ใหม่ได้เมื่อคุณแก้สินค้าในบิลครั้งถัดไป"
        variant="destructive"
        confirmLabel="ลบ"
        onConfirm={handleDelete}
      />

      <MapItemModal
        open={showAddMapModal}
        rawName={newMapping.raw_name || 'ค้นหาสินค้าใน SML'}
        currentCode={newMapping.item_code}
        currentUnit={newMapping.unit_code}
        currentPrice={0}
        onPick={(code, unit) => {
          setNewMapping((p) => ({ ...p, item_code: code, unit_code: unit || p.unit_code }))
          setShowAddMapModal(false)
        }}
        onClose={() => setShowAddMapModal(false)}
      />
    </div>
  )
}
