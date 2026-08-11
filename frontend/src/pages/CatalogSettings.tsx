import { useCallback, useEffect, useState } from 'react'
import { Database, ImageIcon, Plus, RefreshCw, Search } from 'lucide-react'
import { toast } from 'sonner'

import api from '@/api/client'
import { AuthImage } from '@/components/common/AuthImage'
import { EmptyState } from '@/components/common/EmptyState'
import { PageHeader } from '@/components/common/PageHeader'
import { ProductImagePreviewDialog } from '@/components/common/ProductImagePreviewDialog'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Skeleton } from '@/components/ui/skeleton'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { useAuth } from '@/hooks/useAuth'
import { cn } from '@/lib/utils'
import type { CatalogItem } from '@/types'

interface CatalogResponse { data: CatalogItem[]; total: number; page: number; per_page: number }
interface CatalogStats {
  total: number
  hidden_code_count?: number
  sync_running?: boolean
  sync_status?: { running: boolean; count: number; error?: string; finished_at?: string }
}

const PER_PAGE = 50

function messageFrom(error: unknown, fallback: string) {
  const candidate = error as { response?: { data?: { error?: string } } }
  return candidate.response?.data?.error ?? fallback
}

export default function CatalogSettings() {
  const { user } = useAuth()
  const [items, setItems] = useState<CatalogItem[]>([])
  const [stats, setStats] = useState<CatalogStats | null>(null)
  const [page, setPage] = useState(1)
  const [query, setQuery] = useState('')
  const [draft, setDraft] = useState('')
  const [loading, setLoading] = useState(true)
  const [syncing, setSyncing] = useState(false)
  const [refreshingCode, setRefreshingCode] = useState('')
  const [createOpen, setCreateOpen] = useState(false)
  const [preview, setPreview] = useState<CatalogItem | null>(null)
  const canManageCatalog = user?.role === 'admin'
  const totalPages = Math.max(1, Math.ceil((stats?.total ?? 0) / PER_PAGE))

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const [listResponse, statsResponse] = await Promise.all([
        api.get<CatalogResponse>('/api/catalog', { params: { page, per_page: PER_PAGE, q: query || undefined } }),
        api.get<CatalogStats>('/api/catalog/stats'),
      ])
      setItems(listResponse.data.data ?? [])
      setStats({ ...statsResponse.data, total: listResponse.data.total ?? statsResponse.data.total ?? 0 })
    } catch (error) {
      toast.error(messageFrom(error, 'โหลดสินค้า SML ไม่สำเร็จ'))
    } finally {
      setLoading(false)
    }
  }, [page, query])

  useEffect(() => { void load() }, [load])
  useEffect(() => {
    if (!stats?.sync_running) return
    const timer = window.setInterval(() => void load(), 3000)
    return () => window.clearInterval(timer)
  }, [load, stats?.sync_running])

  const syncCatalog = async () => {
    setSyncing(true)
    try {
      await api.post('/api/catalog/sync')
      toast.success('เริ่มซิงก์สินค้า SML แล้ว')
      await load()
    } catch (error) {
      toast.error(messageFrom(error, 'เริ่มซิงก์สินค้าไม่สำเร็จ'))
    } finally {
      setSyncing(false)
    }
  }

  const refreshOne = async (code: string) => {
    setRefreshingCode(code)
    try {
      await api.post(`/api/catalog/${encodeURIComponent(code)}/refresh`)
      toast.success(`อัปเดต ${code} แล้ว`)
      await load()
    } catch (error) {
      toast.error(messageFrom(error, `อัปเดต ${code} ไม่สำเร็จ`))
    } finally {
      setRefreshingCode('')
    }
  }

  return (
    <div className="space-y-4 p-4 sm:p-6">
      <PageHeader
        title="รายการสินค้า SML"
        description="ข้อมูลสินค้าที่ใช้ค้นหาและจับคู่แบบตรงจากฐานข้อมูล SML"
        actions={canManageCatalog ? (
          <div className="flex flex-wrap gap-2">
            <Button variant="outline" onClick={() => setCreateOpen(true)}><Plus className="h-4 w-4" />เพิ่มสินค้า</Button>
            <Button onClick={syncCatalog} disabled={syncing || stats?.sync_running}>
              <RefreshCw className={cn('h-4 w-4', (syncing || stats?.sync_running) && 'animate-spin')} />
              {stats?.sync_running ? 'กำลังซิงก์' : 'ซิงก์จาก SML'}
            </Button>
          </div>
        ) : undefined}
      />

      <div className="flex flex-wrap items-center justify-between gap-3 rounded-lg border bg-card p-3">
        <div>
          <div className="text-2xl font-semibold tabular-nums">{(stats?.total ?? 0).toLocaleString()}</div>
          <div className="text-xs text-muted-foreground">สินค้าที่พร้อมใช้งาน</div>
        </div>
        <div className="text-right text-xs text-muted-foreground">
          {stats?.sync_running
            ? `ซิงก์แล้ว ${(stats.sync_status?.count ?? 0).toLocaleString()} รายการ`
            : stats?.sync_status?.error
              ? <span className="text-destructive">ซิงก์ล่าสุดไม่สำเร็จ: {stats.sync_status.error}</span>
              : 'ค้นหาด้วยรหัสหรือชื่อสินค้าจากฐานข้อมูล SML'}
        </div>
      </div>

      <div className="rounded-lg border bg-card">
        <div className="flex gap-2 border-b p-3">
          <div className="relative flex-1">
            <Search className="pointer-events-none absolute left-2.5 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
            <Input
              value={draft}
              onChange={(event) => setDraft(event.target.value)}
              onKeyDown={(event) => { if (event.key === 'Enter') { setQuery(draft.trim()); setPage(1) } }}
              placeholder="ค้นหารหัสหรือชื่อสินค้า"
              className="pl-8"
            />
          </div>
          <Button variant="outline" onClick={() => { setQuery(draft.trim()); setPage(1) }}>ค้นหา</Button>
        </div>

        {!loading && items.length === 0 ? (
          <EmptyState icon={Database} title="ยังไม่มีสินค้า SML" description="กดซิงก์จาก SML ก่อนเริ่มนำเข้าและสร้างเอกสารขาย" />
        ) : (
          <div className="overflow-x-auto">
            <Table>
              <TableHeader><TableRow><TableHead className="w-16">รูป</TableHead><TableHead>รหัสสินค้า</TableHead><TableHead className="min-w-[260px]">ชื่อสินค้า</TableHead><TableHead>หน่วย</TableHead><TableHead className="text-right">ราคา</TableHead>{canManageCatalog && <TableHead className="text-right">จัดการ</TableHead>}</TableRow></TableHeader>
              <TableBody>
                {loading ? Array.from({ length: 8 }).map((_, index) => <TableRow key={index}><TableCell colSpan={canManageCatalog ? 6 : 5}><Skeleton className="h-11 w-full" /></TableCell></TableRow>) : items.map((item) => {
                  const hasImage = Boolean(item.image_url && (item.image_count ?? 0) > 0)
                  return (
                    <TableRow key={item.item_code}>
                      <TableCell><button type="button" className="h-10 w-10 rounded-md" disabled={!hasImage} onClick={() => hasImage && setPreview(item)}><AuthImage src={hasImage ? item.image_url : undefined} className="h-full w-full rounded-md border bg-muted/30" imgClassName="object-cover" fallback={<div className="flex h-full items-center justify-center"><ImageIcon className="h-4 w-4 text-muted-foreground" /></div>} /></button></TableCell>
                      <TableCell><code className="font-semibold">{item.item_code}</code>{item.has_hidden_chars && <Badge variant="destructive" className="ml-2">รหัสผิดรูปแบบ</Badge>}</TableCell>
                      <TableCell><div className="font-medium">{item.item_name}</div>{item.item_name2 && <div className="text-xs text-muted-foreground">{item.item_name2}</div>}</TableCell>
                      <TableCell>{item.unit_code || '-'}</TableCell>
                      <TableCell className="text-right tabular-nums">{item.price == null ? '-' : new Intl.NumberFormat('th-TH', { style: 'currency', currency: 'THB' }).format(item.price)}</TableCell>
                      {canManageCatalog && <TableCell className="text-right"><Button size="sm" variant="outline" disabled={refreshingCode === item.item_code} onClick={() => void refreshOne(item.item_code)}><RefreshCw className={cn('h-3.5 w-3.5', refreshingCode === item.item_code && 'animate-spin')} />อัปเดต</Button></TableCell>}
                    </TableRow>
                  )
                })}
              </TableBody>
            </Table>
          </div>
        )}

        <div className="flex items-center justify-between border-t px-3 py-2 text-xs text-muted-foreground">
          <span>หน้า {page}/{totalPages}</span>
          <div className="flex gap-2"><Button size="sm" variant="outline" disabled={page <= 1 || loading} onClick={() => setPage((value) => value - 1)}>ก่อนหน้า</Button><Button size="sm" variant="outline" disabled={page >= totalPages || loading} onClick={() => setPage((value) => value + 1)}>ถัดไป</Button></div>
        </div>
      </div>

      {canManageCatalog && <CreateProductDialog open={createOpen} onOpenChange={setCreateOpen} onCreated={load} />}
      <ProductImagePreviewDialog open={preview !== null} onOpenChange={(open) => !open && setPreview(null)} imageUrl={preview?.image_url} itemCode={preview?.item_code} itemName={preview?.item_name} imageCount={preview?.image_count ?? 0} />
    </div>
  )
}

function CreateProductDialog({ open, onOpenChange, onCreated }: { open: boolean; onOpenChange: (open: boolean) => void; onCreated: () => Promise<void> }) {
  const [form, setForm] = useState({ code: '', name: '', unit_code: 'ชิ้น', price: '0' })
  const [saving, setSaving] = useState(false)
  const create = async () => {
    if (!form.code.trim() || !form.name.trim() || !form.unit_code.trim()) { toast.error('กรอกรหัส ชื่อ และหน่วยสินค้าให้ครบ'); return }
    setSaving(true)
    try {
      await api.post('/api/catalog/products', { ...form, code: form.code.trim(), name: form.name.trim(), unit_code: form.unit_code.trim(), price: Number(form.price) || 0 })
      toast.success('เพิ่มสินค้าใน SML แล้ว')
      onOpenChange(false)
      setForm({ code: '', name: '', unit_code: 'ชิ้น', price: '0' })
      await onCreated()
    } catch (error) {
      toast.error(messageFrom(error, 'เพิ่มสินค้าไม่สำเร็จ'))
    } finally { setSaving(false) }
  }
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader><DialogTitle>เพิ่มสินค้าใน SML</DialogTitle></DialogHeader>
        <div className="grid gap-3">
          <div><Label htmlFor="catalog-code">รหัสสินค้า</Label><Input id="catalog-code" value={form.code} onChange={(event) => setForm((value) => ({ ...value, code: event.target.value }))} /></div>
          <div><Label htmlFor="catalog-name">ชื่อสินค้า</Label><Input id="catalog-name" value={form.name} onChange={(event) => setForm((value) => ({ ...value, name: event.target.value }))} /></div>
          <div className="grid grid-cols-2 gap-3"><div><Label htmlFor="catalog-unit">หน่วย</Label><Input id="catalog-unit" value={form.unit_code} onChange={(event) => setForm((value) => ({ ...value, unit_code: event.target.value }))} /></div><div><Label htmlFor="catalog-price">ราคาขาย</Label><Input id="catalog-price" type="number" min="0" step="0.01" value={form.price} onChange={(event) => setForm((value) => ({ ...value, price: event.target.value }))} /></div></div>
          <p className="text-xs text-muted-foreground">ระบบจะสร้างสินค้าใน SML ก่อน แล้วจึงนำมาใช้ใน Nexflow</p>
        </div>
        <DialogFooter><Button variant="outline" onClick={() => onOpenChange(false)} disabled={saving}>ยกเลิก</Button><Button onClick={create} disabled={saving}>{saving ? 'กำลังเพิ่ม...' : 'เพิ่มสินค้า'}</Button></DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
