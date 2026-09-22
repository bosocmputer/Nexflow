import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { AlertTriangle, Boxes, Check, CheckCircle2, CircleOff, Info, Loader2, PackagePlus, Pencil, RefreshCw, Settings2, Trash2 } from 'lucide-react'

import client from '@/api/client'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Checkbox } from '@/components/ui/checkbox'
import { Command, CommandInput, CommandList, CommandGroup, CommandItem } from '@/components/ui/command'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { ShelfPicker, WarehousePicker } from '@/pages/BillDetail/components/WarehousePicker'
import { cn } from '@/lib/utils'
import { permissionForMenu } from '@/lib/navigation'
import { useAuthStore } from '@/store/auth'
import { ConfirmDialog } from '@/components/common/ConfirmDialog'
import { InputChannelBadge } from '@/components/marketplace/InputChannelBadge'

type Source = 'shopee' | 'tiktok'
type Mode = 'quota' | 'shared'

interface Settings { warehouse_code: string; location_code: string; default_buffer_pct: number; kill_switch_enabled: boolean; config_version: number }
interface Member { id: string; source: Source; account_key: string; external_product_id: string; external_sku_id: string; external_warehouse_id?: string; marketplace_alias_id?: string; product_name: string; variant_name: string; unit_factor: number; allocation_pct: number; enabled: boolean; last_target_qty?: number; last_actual_qty?: number; last_error?: string }
interface Pool { id: string; sml_item_code: string; sml_item_name?: string; sml_unit_code: string; allocation_mode: Mode; buffer_pct_override?: number; shared_risk_acknowledged: boolean; status: 'draft' | 'ready' | 'paused' | 'active'; auto_enabled: boolean; kill_switch_enabled: boolean; dry_run_required: boolean; paused_reason?: string; config_version: number; last_sml_available_qty?: number; last_error?: string; members: Member[] }
interface Overview { available: boolean; settings: Settings; pools: Pool[] }
interface Candidate extends Omit<Member, 'id' | 'last_target_qty' | 'last_actual_qty' | 'last_error'> { sml_item_code: string; sml_item_name?: string; sml_unit_code: string }
interface CandidateGroup { key: string; smlItem: string; smlItemName: string; smlUnit: string; members: Candidate[]; searchableText: string }
interface PreviewResult { run_id: string; sml_available_qty: number; reservation_qty: number; usable_qty: number; buffer_qty: number; distributable_qty: number; expires_at: string; lines: { member_id: string; target_qty: number; status: string; message?: string }[] }

const sourceLabel: Record<Source, string> = { shopee: 'Shopee', tiktok: 'TikTok' }
const poolStatus: Record<Pool['status'], string> = { draft: 'รอตั้งค่า', ready: 'พร้อมตรวจ', paused: 'หยุดชั่วคราว', active: 'เปิดใช้งาน' }

export default function MarketplaceStock() {
  const user = useAuthStore((state) => state.user)
  const permission = permissionForMenu(user, 'marketplace_stock')
  const canManage = permission?.can_update === true
  const canOperate = permission?.can_create === true
  const [data, setData] = useState<Overview | null>(null)
  const [tab, setTab] = useState<'all' | Source>('all')
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')
  const [settingsOpen, setSettingsOpen] = useState(false)
  const [createOpen, setCreateOpen] = useState(false)
  const [candidates, setCandidates] = useState<Candidate[]>([])
  const [saving, setSaving] = useState(false)
  const [previewingPoolID, setPreviewingPoolID] = useState('')
  const [previews, setPreviews] = useState<Record<string, PreviewResult>>({})
  const [actioningPoolID, setActioningPoolID] = useState('')
  const [confirmAction, setConfirmAction] = useState<{ pool: Pool; kind: 'auto-on' | 'auto-off' | 'sync' } | null>(null)
  const [editingPool, setEditingPool] = useState<Pool | null>(null)
  const [archivingPool, setArchivingPool] = useState<Pool | null>(null)

  const load = useCallback(async () => {
    setLoading(true); setError('')
    try {
      const response = await client.get<Overview>('/api/settings/marketplace-stock')
      setData(response.data)
    } catch (cause) {
      setError(messageOf(cause, 'โหลดหน้าควบคุมสต๊อกไม่สำเร็จ'))
    } finally { setLoading(false) }
  }, [])

  useEffect(() => { void load() }, [load])

  const filteredPools = useMemo(() => (data?.pools ?? []).filter((pool) => tab === 'all' || pool.members.some((member) => member.source === tab)), [data?.pools, tab])
  const activePools = data?.pools.filter((pool) => pool.status === 'active' && pool.auto_enabled).length ?? 0

  const openCreate = async () => {
    setError(''); setNotice('')
    try {
      const response = await client.get<{ data: Candidate[] }>('/api/settings/marketplace-stock/candidates')
      setCandidates(response.data.data ?? [])
      setCreateOpen(true)
    } catch (cause) { setError(messageOf(cause, 'โหลดรายการสินค้าที่พร้อมตั้งค่าสต๊อกไม่สำเร็จ')) }
  }

  const saveSettings = async (draft: Settings) => {
    if (!data) return
    setSaving(true); setError(''); setNotice('')
    try {
      await client.put('/api/settings/marketplace-stock', { ...draft, expected_config_version: data.settings.config_version, confirm_action: 'UPDATE_MARKETPLACE_STOCK_SETTINGS' })
      setSettingsOpen(false); setNotice('บันทึกแหล่งสต๊อกแล้ว ทุกกลุ่มถูกหยุดไว้ให้ตรวจสอบใหม่ก่อนเปิดใช้งาน')
      await load()
    } catch (cause) { setError(messageOf(cause, 'บันทึกแหล่งสต๊อกไม่สำเร็จ')) } finally { setSaving(false) }
  }

  const createPool = async (input: { smlItem: string; smlUnit: string; mode: Mode; sharedAcknowledged: boolean; members: Candidate[] }) => {
    setSaving(true); setError(''); setNotice('')
    try {
      await client.post('/api/settings/marketplace-stock/pools', {
        sml_item_code: input.smlItem, sml_unit_code: input.smlUnit, allocation_mode: input.mode,
        shared_risk_acknowledged: input.sharedAcknowledged,
        // Disabled rows are only retained while editing the dialog. Sending them
        // to the create endpoint made an unselected SKU look configured in the
        // overview, even though the worker correctly ignored it.
        members: input.members.filter((member) => member.enabled).map(({ sml_item_code: _item, sml_unit_code: _unit, ...member }) => member),
      })
      setCreateOpen(false); setNotice('สร้างกลุ่มสต๊อกแบบร่างแล้ว ขั้นต่อไปคือกดตรวจสอบก่อนเปิดซิงก์จริง')
      await load()
    } catch (cause) { setError(messageOf(cause, 'สร้างกลุ่มสต๊อกไม่สำเร็จ')) } finally { setSaving(false) }
  }

  const openEdit = async (pool: Pool) => {
    setError(''); setNotice('')
    try {
      const response = await client.get<{ data: Candidate[] }>('/api/settings/marketplace-stock/candidates')
      setCandidates(response.data.data ?? [])
      setEditingPool(pool)
    } catch (cause) { setError(messageOf(cause, 'โหลด SKU สำหรับแก้ไขกลุ่มสต๊อกไม่สำเร็จ')) }
  }

  const updatePool = async (pool: Pool, input: { mode: Mode; sharedAcknowledged: boolean; members: Candidate[] }) => {
    setSaving(true); setError(''); setNotice('')
    try {
      await client.put(`/api/settings/marketplace-stock/pools/${pool.id}`, {
        sml_item_code: pool.sml_item_code,
        sml_unit_code: pool.sml_unit_code,
        allocation_mode: input.mode,
        shared_risk_acknowledged: input.sharedAcknowledged,
        members: input.members.filter((member) => member.enabled).map(({ sml_item_code: _item, sml_unit_code: _unit, sml_item_name: _name, ...member }) => member),
        expected_config_version: pool.config_version,
        confirm_action: 'UPDATE_MARKETPLACE_STOCK_POOL',
      })
      setEditingPool(null)
      setNotice('บันทึกกลุ่มแล้ว ระบบพัก Auto และให้ตรวจ SML ใหม่ก่อนซิงก์ครั้งถัดไป')
      await load()
    } catch (cause) { setError(messageOf(cause, 'บันทึกการแก้ไขกลุ่มสต๊อกไม่สำเร็จ')) } finally { setSaving(false) }
  }

  const archivePool = async (pool: Pool) => {
    setActioningPoolID(pool.id); setError(''); setNotice('')
    try {
      await client.delete(`/api/settings/marketplace-stock/pools/${pool.id}`, {
        data: { expected_config_version: pool.config_version, confirm_action: 'ARCHIVE_MARKETPLACE_STOCK_POOL' },
      })
      setNotice('ลบกลุ่มออกจากการใช้งานแล้ว ประวัติการตรวจและซิงก์เดิมยังเก็บไว้ในบันทึกระบบ')
      await load()
    } catch (cause) { setError(messageOf(cause, 'ลบกลุ่มสต๊อกไม่สำเร็จ')) } finally { setActioningPoolID('') }
  }

  const previewPool = async (pool: Pool) => {
    setPreviewingPoolID(pool.id); setError(''); setNotice('')
    try {
      const response = await client.post<PreviewResult>(`/api/settings/marketplace-stock/pools/${pool.id}/preview`, {
        expected_config_version: pool.config_version, confirm_action: 'PREVIEW_MARKETPLACE_STOCK_POOL',
      })
      setPreviews((current) => ({ ...current, [pool.id]: response.data }))
      setNotice('ตรวจยอดจาก SML แล้ว เป็นแผนอ่านอย่างเดียว ยังไม่ได้ส่งยอดไป Shopee หรือ TikTok Shop')
      await load()
    } catch (cause) { setError(messageOf(cause, 'ตรวจยอดจาก SML ไม่สำเร็จ')) } finally { setPreviewingPoolID('') }
  }

  const updateAuto = async (pool: Pool, enabled: boolean) => {
    setActioningPoolID(pool.id); setError(''); setNotice('')
    try {
      await client.put(`/api/settings/marketplace-stock/pools/${pool.id}/auto`, {
        enabled, expected_config_version: pool.config_version,
        confirm_action: enabled ? 'ENABLE_MARKETPLACE_STOCK_AUTO' : 'DISABLE_MARKETPLACE_STOCK_AUTO',
      })
      setNotice(enabled ? 'เปิด Auto แล้ว ระบบจะตรวจทุก 5 นาทีหลังผ่าน pilot' : 'ปิด Auto แล้ว งานใหม่จะไม่ถูกส่งออก')
      await load()
    } catch (cause) { setError(messageOf(cause, 'เปลี่ยนสถานะ Auto ไม่สำเร็จ')) } finally { setActioningPoolID('') }
  }

  const queueSync = async (pool: Pool) => {
    setActioningPoolID(pool.id); setError(''); setNotice('')
    try {
      await client.post(`/api/settings/marketplace-stock/pools/${pool.id}/sync`, { expected_config_version: pool.config_version, confirm_action: 'SYNC_MARKETPLACE_STOCK_POOL' })
      setNotice('รับงานซิงก์แล้ว ระบบจะทำงานในคิวและตรวจผลจริงหลังส่ง')
      await load()
    } catch (cause) { setError(messageOf(cause, 'สั่งซิงก์สต๊อกไม่สำเร็จ')) } finally { setActioningPoolID('') }
  }

  return (
    <main className="space-y-4 p-0 sm:p-0" aria-busy={loading}>
      <header className="flex flex-col gap-3 border-b pb-4 sm:flex-row sm:items-start sm:justify-between">
        <div>
          <div className="flex flex-wrap items-center gap-2"><h1 className="text-xl font-semibold">ควบคุมสต๊อก Marketplace</h1><Badge variant="outline">AOY</Badge></div>
          <p className="mt-1 max-w-3xl text-sm text-muted-foreground">SML เป็นยอดกลาง ระบบจะคำนวณยอดพร้อมขายแล้วส่งยอดไป Shopee และ TikTok ตามนโยบายของแต่ละกลุ่มสินค้า</p>
        </div>
        <div className="flex flex-wrap gap-2">
          <Button variant="outline" size="sm" onClick={() => void load()} disabled={loading}><RefreshCw className={cn('mr-2 h-4 w-4', loading && 'animate-spin')} />รีเฟรช</Button>
          {canManage && <><Button variant="outline" size="sm" onClick={() => setSettingsOpen(true)}><Settings2 className="mr-2 h-4 w-4" />แหล่งสต๊อก</Button><Button size="sm" onClick={() => void openCreate()}><PackagePlus className="mr-2 h-4 w-4" />สร้างกลุ่ม</Button></>}
        </div>
      </header>

      {error && <Alert variant="destructive"><AlertTriangle className="h-4 w-4" /><AlertTitle>ดำเนินการไม่สำเร็จ</AlertTitle><AlertDescription>{error}</AlertDescription></Alert>}
      {notice && <Alert><CheckCircle2 className="h-4 w-4 text-success" /><AlertTitle>บันทึกแล้ว</AlertTitle><AlertDescription>{notice}</AlertDescription></Alert>}
      {!canManage && <Alert><Info className="h-4 w-4" /><AlertTitle>{canOperate ? 'โหมดตรวจสอบและสั่งงาน' : 'โหมดติดตามผล'}</AlertTitle><AlertDescription>{canOperate ? 'คุณตรวจยอดจาก SML แบบอ่านอย่างเดียวได้ ส่วนการเปลี่ยนนโยบายและเปิด Auto ต้องให้ผู้ดูแลดำเนินการ' : 'ผู้ดูแลระบบกำหนดนโยบายและเปิดการซิงก์อัตโนมัติ พนักงานสามารถตรวจสถานะและผลการทำงานได้จากหน้านี้'}</AlertDescription></Alert>}
      {data?.settings.kill_switch_enabled && <Alert variant="destructive"><CircleOff className="h-4 w-4" /><AlertTitle>หยุดส่งสต๊อกทั้งร้านอยู่</AlertTitle><AlertDescription>งานที่ยังไม่เริ่มจะไม่ส่งยอดออกไป Marketplace จนกว่าผู้ดูแลจะเปิดระบบอีกครั้ง</AlertDescription></Alert>}

      <section className="flex flex-wrap items-center gap-x-5 gap-y-2 border-y py-3 text-xs text-muted-foreground" aria-label="สรุปการควบคุมสต๊อก">
        <p><span>แหล่งสต๊อก SML: </span><span className="font-medium text-foreground">{data?.settings.warehouse_code ? `${data.settings.warehouse_code} / ${data.settings.location_code}` : 'ยังไม่ได้เลือก'}</span></p>
        <p><span>กันสต๊อกเริ่มต้น: </span><span className="font-medium text-foreground">{data ? `${data.settings.default_buffer_pct}%` : '—'}</span></p>
        <p><span>เปิด Auto: </span><span className="font-medium text-foreground">{activePools} กลุ่ม</span><span> · กลุ่มใหม่เริ่มปิดเสมอ</span></p>
      </section>

      <Tabs value={tab} onValueChange={(value) => setTab(value as 'all' | Source)}>
        <TabsList aria-label="กรองช่องทาง"><TabsTrigger value="all">ทั้งหมด</TabsTrigger><TabsTrigger value="shopee">Shopee</TabsTrigger><TabsTrigger value="tiktok">TikTok</TabsTrigger></TabsList>
        <TabsContent value={tab} className="space-y-3">
          {loading ? <LoadingRows /> : filteredPools.length === 0 ? <EmptyState canManage={canManage} onCreate={() => void openCreate()} /> : filteredPools.map((pool) => <PoolCard key={pool.id} pool={pool} canManage={canManage} canOperate={canOperate} preview={previews[pool.id]} previewing={previewingPoolID === pool.id} actioning={actioningPoolID === pool.id} onPreview={() => void previewPool(pool)} onAuto={(enabled) => setConfirmAction({ pool, kind: enabled ? 'auto-on' : 'auto-off' })} onSync={() => setConfirmAction({ pool, kind: 'sync' })} onEdit={() => void openEdit(pool)} onArchive={() => setArchivingPool(pool)} />)}
        </TabsContent>
      </Tabs>

      {data && <SettingsDialog open={settingsOpen} settings={data.settings} saving={saving} onOpenChange={setSettingsOpen} onSave={saveSettings} />}
      <CreatePoolDialog open={createOpen} candidates={candidates} saving={saving} onOpenChange={setCreateOpen} onCreate={createPool} />
      <EditPoolDialog open={Boolean(editingPool)} pool={editingPool} candidates={candidates} saving={saving} onOpenChange={(open) => !open && setEditingPool(null)} onSave={updatePool} />
      <ConfirmDialog open={Boolean(confirmAction)} onOpenChange={(open) => !open && setConfirmAction(null)}
        title={confirmAction?.kind === 'auto-on' ? 'เปิดส่งสต๊อกอัตโนมัติหรือไม่?' : confirmAction?.kind === 'auto-off' ? 'ปิดส่งสต๊อกอัตโนมัติหรือไม่?' : 'ส่งงานซิงก์สต๊อกด้วยมือหรือไม่?'}
        description={confirmAction?.kind === 'auto-on' ? 'ระบบจะเริ่มเฉพาะงานใหม่หลังจากผ่าน dry-run และการซิงก์ด้วยมือสำเร็จแล้ว ไม่ส่งย้อนหลัง' : confirmAction?.kind === 'auto-off' ? 'งานที่ยังไม่เริ่มจะถูกยกเลิก แต่งานที่เริ่มเขียนแล้วอาจทำต่อจนตรวจ read-back เสร็จ เพื่อไม่ให้ยอดค้างกลางทาง' : 'ระบบจะอ่าน SML และยอด Marketplace ล่าสุดอีกครั้งก่อนเขียน แล้วอ่านกลับทุก SKU ที่เปลี่ยน'}
        confirmLabel={confirmAction?.kind === 'auto-on' ? 'เปิด Auto' : confirmAction?.kind === 'auto-off' ? 'ปิด Auto' : 'ยืนยันส่งงาน'}
        variant={confirmAction?.kind === 'auto-off' ? 'destructive' : 'default'}
        onConfirm={async () => { if (!confirmAction) return; if (confirmAction.kind === 'sync') await queueSync(confirmAction.pool); else await updateAuto(confirmAction.pool, confirmAction.kind === 'auto-on') }} />
      <ConfirmDialog open={Boolean(archivingPool)} onOpenChange={(open) => !open && setArchivingPool(null)}
        title="ลบกลุ่มสต๊อกนี้หรือไม่?"
        description={archivingPool ? `${archivingPool.sml_item_code} · ${archivingPool.sml_item_name || 'สินค้า SML'} จะหายจากหน้าควบคุมสต๊อก\n\nระบบจะปิด Auto และยกเลิกงานที่ยังไม่เริ่ม แต่จะเก็บประวัติการตรวจ/ซิงก์และ audit log ไว้เสมอ หากมีงานกำลังทำอยู่ ระบบจะไม่อนุญาตให้ลบจนกว่างานนั้นเสร็จ` : ''}
        confirmLabel="ลบกลุ่ม"
        variant="destructive"
        onConfirm={async () => { if (archivingPool) await archivePool(archivingPool) }} />
    </main>
  )
}

function PoolCard({ pool, canManage, canOperate, preview, previewing, actioning, onPreview, onAuto, onSync, onEdit, onArchive }: { pool: Pool; canManage: boolean; canOperate: boolean; preview?: PreviewResult; previewing: boolean; actioning: boolean; onPreview: () => void; onAuto: (enabled: boolean) => void; onSync: () => void; onEdit: () => void; onArchive: () => void }) {
  const enabledMembers = pool.members.filter((member) => member.enabled)
  return (
    <Card className="overflow-hidden">
      <CardHeader className="gap-3 p-3 lg:flex-row lg:items-start lg:justify-between">
        <div className="min-w-0">
          <CardTitle className="line-clamp-1 text-base" title={`${pool.sml_item_code} · ${pool.sml_item_name || 'ยังไม่พบชื่อสินค้า SML'}`}><span className="font-mono">{pool.sml_item_code}</span><span className="font-normal"> · {pool.sml_item_name || 'ยังไม่พบชื่อสินค้า SML'}</span></CardTitle>
          <PoolSnapshot pool={pool} members={enabledMembers} />
        </div>
        <div className="flex flex-wrap items-center gap-2 lg:justify-end">
          <Badge variant={pool.status === 'active' ? 'default' : 'secondary'}>{poolStatus[pool.status]}</Badge>
          <Badge variant="outline">{pool.auto_enabled ? 'Auto ทุก 5 นาที' : 'Auto ปิด'}</Badge>
          {canOperate && <Button variant="outline" size="sm" onClick={onPreview} disabled={previewing || actioning || pool.kill_switch_enabled}><RefreshCw className={cn('mr-2 h-4 w-4', previewing && 'animate-spin')} />{pool.status === 'paused' ? 'ตรวจ SML อีกครั้ง' : 'ตรวจ SML'}</Button>}
          {canOperate && !pool.dry_run_required && pool.status !== 'paused' && <Button size="sm" onClick={onSync} disabled={actioning}>ซิงก์ด้วยมือ</Button>}
          {canManage && <label className="flex items-center gap-2 rounded-md border bg-muted/30 px-2 py-1 text-xs"><Switch checked={pool.auto_enabled} onCheckedChange={onAuto} disabled={actioning || pool.dry_run_required || pool.kill_switch_enabled} /><span>ส่งอัตโนมัติ</span></label>}
          {canManage && <Button variant="outline" size="sm" onClick={onEdit} disabled={actioning}><Pencil className="mr-2 h-4 w-4" />แก้ไข</Button>}
          {canManage && <Button variant="ghost" size="sm" className="text-destructive hover:text-destructive" onClick={onArchive} disabled={actioning}><Trash2 className="mr-2 h-4 w-4" />ลบกลุ่ม</Button>}
        </div>
      </CardHeader>
      <CardContent className="p-0">
        {enabledMembers.length > 0 && <div className="divide-y border-t">
          {enabledMembers.map((member) => <div key={member.id} className="grid min-w-0 gap-x-6 gap-y-1 px-3 py-2 text-sm md:grid-cols-[minmax(0,1fr)_9rem_11rem] md:items-center">
            <div className="min-w-0">
              <div className="flex min-w-0 items-center gap-2"><SourceBadge source={member.source} /><p className="truncate font-medium" title={member.product_name || 'ยังไม่มีชื่อสินค้า'}>{member.product_name || 'ยังไม่มีชื่อสินค้า'}</p></div>
              <p className={cn('mt-0.5 truncate text-xs', member.last_error ? 'text-destructive' : 'text-muted-foreground')} title={member.last_error || member.variant_name || member.external_sku_id}>{member.last_error ? `ต้องตรวจ: ${member.last_error}` : member.variant_name ? `ตัวเลือก: ${member.variant_name}` : `SKU: ${member.external_sku_id}`}</p>
              {!member.last_error && <p className="mt-1 text-xs text-muted-foreground md:hidden">{pool.allocation_mode === 'quota' ? `โควตา ${formatNumber(member.allocation_pct)}%` : 'ใช้ยอดร่วม'} · ยอดล่าสุด {formatNumber(member.last_actual_qty)}</p>}
            </div>
            {!member.last_error && <div className="hidden text-right md:block"><p className="text-xs text-muted-foreground">การจัดสรร</p><p className="mt-0.5 font-medium">{pool.allocation_mode === 'quota' ? `โควตา ${formatNumber(member.allocation_pct)}%` : 'ใช้ยอดร่วม'}</p></div>}
            {!member.last_error && <div className="hidden text-right md:block"><p className="text-xs text-muted-foreground">สต๊อกล่าสุดบน {sourceLabel[member.source]}</p><p className="mt-0.5 font-semibold tabular-nums">{formatNumber(member.last_actual_qty)} <span className="font-normal text-muted-foreground">{pool.sml_unit_code}</span></p></div>}
          </div>)}
        </div>}
      </CardContent>
      {preview && <div className="border-t bg-muted/30 px-4 py-3 text-sm"><p className="font-medium">แผนจาก SML: พร้อมใช้ {preview.usable_qty} {pool.sml_unit_code} · กันชน {preview.buffer_qty} · ส่งออกได้ {preview.distributable_qty}</p><p className="mt-1 text-xs text-muted-foreground">หัก reservation {preview.reservation_qty} แล้ว; แผนหมดอายุใน 60 วินาที และยังไม่ได้อ่านหรือเขียนยอด Marketplace</p><div className="mt-2 flex flex-wrap gap-2">{preview.lines.map((line) => <Badge key={line.member_id} variant="outline">SKU → {line.target_qty}</Badge>)}</div></div>}
      {pool.paused_reason && <div className="border-t px-4 py-2 text-xs text-warning">หยุดชั่วคราว: {thaiPause(pool.paused_reason)}</div>}
    </Card>
  )
}

function SourceBadge({ source }: { source: Source }) {
  return <InputChannelBadge channel={source === 'shopee' ? 'shopee' : 'tiktok_shop'} label={sourceLabel[source]} />
}

function PoolSnapshot({ pool, members }: { pool: Pool; members: Member[] }) {
  const sources: Source[] = ['shopee', 'tiktok']
  const bufferLabel = pool.buffer_pct_override === undefined ? 'กันชนเริ่มต้น' : `กันชน ${formatNumber(pool.buffer_pct_override)}%`
  return (
    <div className="mt-1 flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-muted-foreground" aria-label="ยอดสต๊อกล่าสุดที่ระบบตรวจสอบแล้ว">
      <span>หน่วย <strong className="font-medium text-foreground">{pool.sml_unit_code}</strong></span>
      <span>{pool.allocation_mode === 'quota' ? 'แบ่งโควตา' : 'ใช้ยอดร่วม'}</span>
      <span>{bufferLabel}</span>
      <span title="ยอดจากการตรวจ SML ล่าสุด ไม่ได้เรียก SML ใหม่เมื่อเปิดหน้า">SML ล่าสุด <strong className="tabular-nums text-foreground">{formatNumber(pool.last_sml_available_qty)}</strong> {pool.sml_unit_code}</span>
      {sources.filter((source) => members.some((member) => member.source === source)).map((source) => <PlatformSnapshot key={source} source={source} members={members.filter((member) => member.source === source)} />)}
      <span className="text-muted-foreground/80">ข้อมูลจากการตรวจ/ซิงก์ล่าสุด</span>
    </div>
  )
}

function PlatformSnapshot({ source, members }: { source: Source; members: Member[] }) {
  const verified = members.map((member) => member.last_actual_qty).filter((quantity): quantity is number => quantity !== undefined)
  const label = members.length === 1
    ? formatNumber(verified[0])
    : verified.length === members.length ? `${formatNumber(verified.reduce((total, quantity) => total + quantity, 0))} รวม ${members.length} SKU` : `${verified.length}/${members.length} SKU มีข้อมูล`
  return <span className="inline-flex items-center gap-1"><SourceBadge source={source} /><strong className="tabular-nums text-foreground">{label}</strong></span>
}

function formatNumber(value: number | undefined) {
  if (value === undefined || !Number.isFinite(value)) return '—'
  return new Intl.NumberFormat('th-TH', { maximumFractionDigits: 2 }).format(value)
}

function EmptyState({ canManage, onCreate }: { canManage: boolean; onCreate: () => void }) { return <Card><CardContent className="flex flex-col items-center px-6 py-12 text-center"><Boxes className="h-9 w-9 text-muted-foreground" /><h2 className="mt-3 font-semibold">ยังไม่มีกลุ่มสต๊อก</h2><p className="mt-1 max-w-md text-sm text-muted-foreground">เริ่มจากจับคู่สินค้า Marketplace กับสินค้า SML ให้พร้อม แล้วสร้างกลุ่มเพื่อเลือกว่าจะแบ่งโควตาหรือใช้สต๊อกร่วม</p>{canManage && <Button className="mt-4" onClick={onCreate}>สร้างกลุ่มสต๊อก</Button>}</CardContent></Card> }
function LoadingRows() { return <div className="space-y-3" aria-label="กำลังโหลด"><div className="h-28 animate-pulse rounded-md bg-muted" /><div className="h-28 animate-pulse rounded-md bg-muted" /></div> }

function SettingsDialog({ open, settings, saving, onOpenChange, onSave }: { open: boolean; settings: Settings; saving: boolean; onOpenChange: (open: boolean) => void; onSave: (settings: Settings) => void }) {
  const [draft, setDraft] = useState(settings)
  useEffect(() => { if (open) setDraft(settings) }, [open, settings])
  return <Dialog open={open} onOpenChange={(next) => !saving && onOpenChange(next)}>
    <DialogContent>
      <DialogHeader>
        <DialogTitle>แหล่งสต๊อก SML และกันชน</DialogTitle>
        <DialogDescription>เลือกคลังและพื้นที่เก็บจาก SML เพื่อป้องกันรหัสผิด การเปลี่ยนค่าเหล่านี้จะหยุดทุกกลุ่มไว้ก่อน เพื่อให้ตรวจสอบและ dry-run ใหม่</DialogDescription>
      </DialogHeader>
      <div className="grid gap-3 sm:grid-cols-2">
        <div className="space-y-1">
          <Label>คลัง SML</Label>
          <WarehousePicker
            value={draft.warehouse_code}
            disabled={saving}
            onChange={(warehouse) => setDraft((current) => ({ ...current, warehouse_code: warehouse.code, location_code: '' }))}
          />
        </div>
        <div className="space-y-1">
          <Label>พื้นที่เก็บ</Label>
          <ShelfPicker
            warehouseCode={draft.warehouse_code}
            value={draft.location_code}
            disabled={saving}
            onChange={(shelf) => setDraft((current) => ({ ...current, location_code: shelf.code }))}
          />
        </div>
        <div className="space-y-1">
          <Label htmlFor="buffer">กันสต๊อกเริ่มต้น (%)</Label>
          <Input id="buffer" type="number" min="0" max="100" value={draft.default_buffer_pct} onChange={(event) => setDraft({ ...draft, default_buffer_pct: Number(event.target.value) })} />
        </div>
        <label className="flex items-center gap-2 pt-6 text-sm"><Switch checked={draft.kill_switch_enabled} onCheckedChange={(checked) => setDraft({ ...draft, kill_switch_enabled: checked })} />หยุดส่งสต๊อกทั้งร้าน</label>
      </div>
      <DialogFooter>
        <Button variant="outline" onClick={() => onOpenChange(false)} disabled={saving}>ยกเลิก</Button>
        <Button onClick={() => onSave(draft)} disabled={saving || !draft.warehouse_code || !draft.location_code}>{saving && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}ยืนยันบันทึก</Button>
      </DialogFooter>
    </DialogContent>
  </Dialog>
}

function CreatePoolDialog({ open, candidates, saving, onOpenChange, onCreate }: { open: boolean; candidates: Candidate[]; saving: boolean; onOpenChange: (open: boolean) => void; onCreate: (input: { smlItem: string; smlUnit: string; mode: Mode; sharedAcknowledged: boolean; members: Candidate[] }) => void }) {
  const groups = useMemo<CandidateGroup[]>(() => {
    const grouped = new Map<string, Candidate[]>()
    for (const candidate of candidates) {
      const key = `${candidate.sml_item_code}|${candidate.sml_unit_code}`
      grouped.set(key, [...(grouped.get(key) ?? []), candidate])
    }
    return [...grouped.entries()].map(([key, members]) => ({
      key,
      smlItem: members[0]?.sml_item_code ?? '',
      smlItemName: members[0]?.sml_item_name ?? '',
      smlUnit: members[0]?.sml_unit_code ?? '',
      members,
      searchableText: [members[0]?.sml_item_code, members[0]?.sml_item_name, members[0]?.sml_unit_code, ...members.flatMap((member) => [member.product_name, member.variant_name])].join(' ').toLocaleLowerCase(),
    }))
  }, [candidates])
  const [groupKey, setGroupKey] = useState('')
  const [mode, setMode] = useState<Mode>('quota')
  const [sharedAcknowledged, setSharedAcknowledged] = useState(false)
  const [members, setMembers] = useState<Candidate[]>([])
  const [reviewingCreate, setReviewingCreate] = useState(false)
  const initializedForOpen = useRef(false)
  const draftMembers = (key: string) => {
    const selected = candidates.filter((candidate) => `${candidate.sml_item_code}|${candidate.sml_unit_code}` === key)
    const share = selected.length ? Math.floor(10000 / selected.length) / 100 : 0
    return selected.map((candidate, index) => ({ ...candidate, allocation_pct: index === selected.length - 1 ? Number((100 - share * Math.max(0, selected.length - 1)).toFixed(2)) : share, enabled: true }))
  }
  useEffect(() => {
    if (!open) { initializedForOpen.current = false; return }
    if (initializedForOpen.current) return
    setGroupKey(''); setMode('quota'); setSharedAcknowledged(false); setMembers([]); setReviewingCreate(false)
    initializedForOpen.current = true
  }, [open, candidates, groups])
  const changeGroup = (value: string) => { setGroupKey(value); setMembers(draftMembers(value)) }
  const toggleMember = (index: number, enabled: boolean) => {
    setMembers((current) => {
      const next = current.map((member, currentIndex) => ({ ...member, enabled: currentIndex === index ? enabled : member.enabled }))
      if (mode !== 'quota') return next
      const enabledIndexes = next.flatMap((member, currentIndex) => member.enabled ? [currentIndex] : [])
      const share = enabledIndexes.length ? Math.floor(10000 / enabledIndexes.length) / 100 : 0
      return next.map((member, currentIndex) => !member.enabled ? member : {
        ...member,
        allocation_pct: currentIndex === enabledIndexes[enabledIndexes.length - 1] ? Number((100 - share * Math.max(0, enabledIndexes.length - 1)).toFixed(2)) : share,
      })
    })
  }
  const total = members.filter((member) => member.enabled).reduce((sum, member) => sum + member.allocation_pct, 0)
  const channels = [...new Set(members.filter((member) => member.enabled).map((member) => sourceLabel[member.source]))]
  const submit = () => setReviewingCreate(true)
  const confirmCreatePool = () => { const [smlItem, smlUnit] = groupKey.split('|', 2); if (smlItem && smlUnit) onCreate({ smlItem, smlUnit, mode, sharedAcknowledged, members }) }
  const canCreate = !saving && groupKey && members.some((member) => member.enabled) && (mode !== 'quota' || total <= 100) && (mode !== 'shared' || sharedAcknowledged)
  const summary = `${members.filter((member) => member.enabled).length} SKU · ${channels.join(', ') || 'ยังไม่ได้เลือก SKU'} · ${mode === 'quota' ? `รวมโควตา ${total.toFixed(2)}%` : 'ใช้ยอดร่วม'}`

  return <Dialog open={open} onOpenChange={(next) => !saving && onOpenChange(next)}>
    <DialogContent className="max-h-[90vh] overflow-y-auto sm:max-w-2xl">
      {reviewingCreate ? <>
        <DialogHeader>
          <DialogTitle>ยืนยันสร้างกลุ่มสต๊อกแบบร่าง</DialogTitle>
          <DialogDescription>ตรวจทานรายการนี้ก่อนบันทึก ระบบจะยังไม่ส่งหรือเขียนสต๊อกไป Marketplace</DialogDescription>
        </DialogHeader>
        <Alert><Info className="h-4 w-4" /><AlertTitle>{selectedGroupLabel(groups, groupKey)}</AlertTitle><AlertDescription>{summary}<br />ขั้นต่อไปต้องกด Dry-run เพื่ออ่านยอด SML ก่อน จึงจะมีสิทธิ์สั่งซิงก์ด้วยมือ</AlertDescription></Alert>
        <DialogFooter>
          <Button variant="outline" onClick={() => setReviewingCreate(false)} disabled={saving}>กลับไปแก้ไข</Button>
          <Button onClick={confirmCreatePool} disabled={saving}>{saving && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}สร้างแบบร่าง</Button>
        </DialogFooter>
      </> : <>
        <DialogHeader>
          <DialogTitle>สร้างกลุ่มสต๊อก Marketplace</DialogTitle>
          <DialogDescription>เลือก SKU ที่จับคู่กับสินค้า SML เดียวกันแล้ว ระบบจะเริ่มเป็นแบบร่าง ยังไม่ส่งสต๊อกจริง</DialogDescription>
        </DialogHeader>
        {groups.length === 0 ? <Alert><Info className="h-4 w-4" /><AlertTitle>ยังไม่มีสินค้าที่พร้อม</AlertTitle><AlertDescription>ไปที่ “จับคู่สินค้า Marketplace” และตรวจหน่วย/การแปลงของ Shopee หรือ TikTok ให้พร้อมก่อน</AlertDescription></Alert> : <div className="space-y-4">
          <SMLGroupAutocomplete groups={groups} value={groupKey} onChange={changeGroup} />
          <div className="space-y-1"><Label>นโยบายสต๊อก</Label><Select value={mode} onValueChange={(value) => setMode(value as Mode)}><SelectTrigger><SelectValue /></SelectTrigger><SelectContent><SelectItem value="quota">แบ่งโควตา</SelectItem><SelectItem value="shared">ใช้สต๊อกร่วม</SelectItem></SelectContent></Select></div>
          {mode === 'shared' && <label className="flex gap-2 rounded-md border border-warning/40 bg-warning/10 p-3 text-sm"><Checkbox checked={sharedAcknowledged} onCheckedChange={(checked) => setSharedAcknowledged(checked === true)} /><span><b>ฉันเข้าใจความเสี่ยง</b><br />หลายช่องทางอาจขายพร้อมกันได้ แม้ Nexflow จะคำนวณจากยอด SML ก้อนเดียว</span></label>}
          <div className="space-y-2"><Label>SKU ในกลุ่ม</Label><p className="text-xs text-muted-foreground">เลือกเฉพาะ SKU ที่ต้องการควบคุม เมื่อเลือกหรือเอาออก ระบบจะกระจายโควตาของ SKU ที่เลือกใหม่ให้รวม 100%.</p>{members.map((member, index) => <div className="grid gap-2 rounded-md border p-3 sm:grid-cols-[auto_1fr_110px] sm:items-center" key={`${member.source}|${member.account_key}|${member.external_product_id}|${member.external_sku_id}`}><Checkbox checked={member.enabled} onCheckedChange={(checked) => toggleMember(index, checked === true)} /><div className="min-w-0"><p className="text-sm font-medium">{sourceLabel[member.source]} · {member.product_name}</p><p className="truncate text-xs text-muted-foreground">{member.variant_name || member.external_sku_id}</p></div>{mode === 'quota' ? <Input aria-label={`โควตา ${member.product_name}`} type="number" min="0" max="100" value={member.allocation_pct} onChange={(event) => setMembers((current) => current.map((value, i) => i === index ? { ...value, allocation_pct: Number(event.target.value) } : value))} /> : <span className="text-sm text-muted-foreground">ยอดร่วม</span>}</div>)}{mode === 'quota' && <p className={cn('text-xs', total > 100 ? 'text-destructive' : 'text-muted-foreground')}>รวมโควตา {total.toFixed(2)}% {total > 100 ? '— ต้องไม่เกิน 100%' : ''}</p>}</div>
          <Alert><Info className="h-4 w-4" /><AlertTitle>สรุปก่อนสร้างแบบร่าง</AlertTitle><AlertDescription>{summary}<br />ขั้นต่อไปยังเป็น Dry-run เท่านั้น ระบบจะยังไม่เขียนสต๊อกไป Marketplace</AlertDescription></Alert>
        </div>}
        <DialogFooter><Button variant="outline" onClick={() => onOpenChange(false)} disabled={saving}>ยกเลิก</Button><Button onClick={submit} disabled={!canCreate}>{saving && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}ตรวจสอบก่อนสร้าง</Button></DialogFooter>
      </>}
    </DialogContent>
  </Dialog>
}

function poolMemberKey(member: Pick<Candidate, 'source' | 'account_key' | 'external_product_id' | 'external_sku_id'>) {
  return `${member.source}|${member.account_key}|${member.external_product_id}|${member.external_sku_id}`
}

function candidateFromPoolMember(pool: Pool, member: Member): Candidate {
  return {
    source: member.source,
    account_key: member.account_key,
    external_product_id: member.external_product_id,
    external_sku_id: member.external_sku_id,
    external_warehouse_id: member.external_warehouse_id,
    marketplace_alias_id: member.marketplace_alias_id,
    product_name: member.product_name,
    variant_name: member.variant_name,
    unit_factor: member.unit_factor,
    allocation_pct: member.allocation_pct,
    enabled: member.enabled,
    sml_item_code: pool.sml_item_code,
    sml_item_name: pool.sml_item_name,
    sml_unit_code: pool.sml_unit_code,
  }
}

function EditPoolDialog({ open, pool, candidates, saving, onOpenChange, onSave }: { open: boolean; pool: Pool | null; candidates: Candidate[]; saving: boolean; onOpenChange: (open: boolean) => void; onSave: (pool: Pool, input: { mode: Mode; sharedAcknowledged: boolean; members: Candidate[] }) => void }) {
  const [mode, setMode] = useState<Mode>('quota')
  const [sharedAcknowledged, setSharedAcknowledged] = useState(false)
  const [members, setMembers] = useState<Candidate[]>([])
  const [reviewing, setReviewing] = useState(false)
  const initializedForOpen = useRef(false)
  const availableMembers = useMemo(() => {
    if (!pool) return []
    const key = `${pool.sml_item_code}|${pool.sml_unit_code}`
    const merged = new Map<string, Candidate>()
    for (const member of pool.members) {
      const candidate = candidateFromPoolMember(pool, member)
      merged.set(poolMemberKey(candidate), candidate)
    }
    for (const candidate of candidates) {
      if (`${candidate.sml_item_code}|${candidate.sml_unit_code}` !== key) continue
      const candidateKey = poolMemberKey(candidate)
      if (!merged.has(candidateKey)) merged.set(candidateKey, { ...candidate, enabled: false, allocation_pct: 0 })
    }
    return [...merged.values()]
  }, [candidates, pool])

  useEffect(() => {
    if (!open || !pool) { initializedForOpen.current = false; return }
    if (initializedForOpen.current) return
    setMode(pool.allocation_mode)
    setSharedAcknowledged(pool.shared_risk_acknowledged)
    setMembers(availableMembers)
    setReviewing(false)
    initializedForOpen.current = true
  }, [availableMembers, open, pool])

  const toggleMember = (index: number, enabled: boolean) => {
    setMembers((current) => {
      const next = current.map((member, currentIndex) => currentIndex === index ? { ...member, enabled } : member)
      if (mode !== 'quota') return next
      const enabledIndexes = next.flatMap((member, currentIndex) => member.enabled ? [currentIndex] : [])
      const share = enabledIndexes.length ? Math.floor(10000 / enabledIndexes.length) / 100 : 0
      return next.map((member, currentIndex) => !member.enabled ? member : {
        ...member,
        allocation_pct: currentIndex === enabledIndexes[enabledIndexes.length - 1] ? Number((100 - share * Math.max(0, enabledIndexes.length - 1)).toFixed(2)) : share,
      })
    })
  }
  const enabledMembers = members.filter((member) => member.enabled)
  const total = enabledMembers.reduce((sum, member) => sum + member.allocation_pct, 0)
  const canSave = Boolean(pool) && !saving && enabledMembers.length > 0 && (mode !== 'quota' || total <= 100) && (mode !== 'shared' || sharedAcknowledged)
  const summary = `${enabledMembers.length} SKU · ${[...new Set(enabledMembers.map((member) => sourceLabel[member.source]))].join(', ') || 'ยังไม่ได้เลือก SKU'} · ${mode === 'quota' ? `รวมโควตา ${total.toFixed(2)}%` : 'ใช้ยอดร่วม'}`

  return <Dialog open={open} onOpenChange={(next) => !saving && onOpenChange(next)}>
    <DialogContent className="max-h-[90dvh] overflow-y-auto sm:max-w-2xl">
      {reviewing ? <>
        <DialogHeader>
          <DialogTitle>ยืนยันบันทึกการแก้ไขกลุ่มสต๊อก</DialogTitle>
          <DialogDescription>การเปลี่ยน SKU, โควตา หรือนโยบายสต๊อกจะปิด Auto และบังคับตรวจ SML ใหม่ก่อนซิงก์ครั้งถัดไป</DialogDescription>
        </DialogHeader>
        <Alert><Info className="h-4 w-4" /><AlertTitle>{pool ? `${pool.sml_item_code} · ${pool.sml_item_name || 'สินค้า SML'} · ${pool.sml_unit_code}` : ''}</AlertTitle><AlertDescription>{summary}<br />ยังไม่มีการเขียนสต๊อกไป Marketplace จากการบันทึกครั้งนี้</AlertDescription></Alert>
        <DialogFooter>
          <Button variant="outline" onClick={() => setReviewing(false)} disabled={saving}>กลับไปแก้ไข</Button>
          <Button onClick={() => pool && onSave(pool, { mode, sharedAcknowledged, members })} disabled={!canSave}>{saving && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}บันทึกและพักกลุ่ม</Button>
        </DialogFooter>
      </> : <>
        <DialogHeader>
          <DialogTitle>แก้ไขกลุ่มสต๊อก Marketplace</DialogTitle>
          <DialogDescription>แก้ไขเฉพาะ SKU ที่จับคู่กับสินค้า SML เดียวกัน เพื่อไม่ให้ย้ายสต๊อกข้ามสินค้าโดยไม่ตั้งใจ</DialogDescription>
        </DialogHeader>
        {pool && <div className="space-y-4">
          <div className="rounded-md border bg-muted/30 px-3 py-2 text-sm"><span className="font-mono font-medium">{pool.sml_item_code}</span><span> · {pool.sml_item_name || 'ยังไม่พบชื่อสินค้า SML'} · หน่วย {pool.sml_unit_code}</span></div>
          <div className="space-y-1"><Label>นโยบายสต๊อก</Label><Select value={mode} onValueChange={(value) => setMode(value as Mode)}><SelectTrigger><SelectValue /></SelectTrigger><SelectContent><SelectItem value="quota">แบ่งโควตา</SelectItem><SelectItem value="shared">ใช้สต๊อกร่วม</SelectItem></SelectContent></Select></div>
          {mode === 'shared' && <label className="flex gap-2 rounded-md border border-warning/40 bg-warning/10 p-3 text-sm"><Checkbox checked={sharedAcknowledged} onCheckedChange={(checked) => setSharedAcknowledged(checked === true)} /><span><b>ฉันเข้าใจความเสี่ยง</b><br />หลายช่องทางอาจขายพร้อมกันได้ แม้ Nexflow จะคำนวณจากยอด SML ก้อนเดียว</span></label>}
          <div className="space-y-2"><Label>SKU ในกลุ่ม</Label><p className="text-xs text-muted-foreground">เลือกหรือเอา SKU ออกจากกลุ่มได้ การเปลี่ยนรายการจะพักกลุ่มและให้ตรวจ SML ใหม่</p>{members.map((member, index) => <div className="grid gap-2 rounded-md border p-3 sm:grid-cols-[auto_1fr_110px] sm:items-center" key={poolMemberKey(member)}><Checkbox checked={member.enabled} onCheckedChange={(checked) => toggleMember(index, checked === true)} /><div className="min-w-0"><p className="text-sm font-medium">{sourceLabel[member.source]} · {member.product_name}</p><p className="truncate text-xs text-muted-foreground">{member.variant_name || member.external_sku_id}</p></div>{mode === 'quota' ? <Input aria-label={`โควตา ${member.product_name}`} type="number" min="0" max="100" value={member.allocation_pct} disabled={!member.enabled} onChange={(event) => setMembers((current) => current.map((value, currentIndex) => currentIndex === index ? { ...value, allocation_pct: Number(event.target.value) } : value))} /> : <span className="text-sm text-muted-foreground">ยอดร่วม</span>}</div>)}{mode === 'quota' && <p className={cn('text-xs', total > 100 ? 'text-destructive' : 'text-muted-foreground')}>รวมโควตา {total.toFixed(2)}% {total > 100 ? '— ต้องไม่เกิน 100%' : ''}</p>}</div>
          <Alert><Info className="h-4 w-4" /><AlertTitle>ผลหลังบันทึก</AlertTitle><AlertDescription>{summary}<br />Auto จะปิดและต้องกดตรวจ SML ใหม่ก่อนสั่งซิงก์ด้วยมือหรือเปิด Auto อีกครั้ง</AlertDescription></Alert>
        </div>}
        <DialogFooter><Button variant="outline" onClick={() => onOpenChange(false)} disabled={saving}>ยกเลิก</Button><Button onClick={() => setReviewing(true)} disabled={!canSave}>ตรวจสอบก่อนบันทึก</Button></DialogFooter>
      </>}
    </DialogContent>
  </Dialog>
}

function SMLGroupAutocomplete({ groups, value, onChange }: { groups: CandidateGroup[]; value: string; onChange: (value: string) => void }) {
  const [query, setQuery] = useState('')
  const normalizedQuery = query.trim().toLocaleLowerCase()
  const visibleGroups = groups.filter((group) => !normalizedQuery || group.searchableText.includes(normalizedQuery))
  const selected = groups.find((group) => group.key === value)

  return <div className="space-y-1">
    <Label htmlFor="marketplace-stock-group-autocomplete">สินค้า SML ที่จะควบคุม</Label>
    <Command shouldFilter={false} className="h-auto rounded-md border">
      <CommandInput id="marketplace-stock-group-autocomplete" value={query} onValueChange={setQuery} placeholder="ค้นหาด้วยรหัส SML, ชื่อสินค้า หรือชื่อ Marketplace…" />
      <CommandList className="max-h-64 overscroll-contain">
        {visibleGroups.length === 0 ? <p className="px-3 py-6 text-center text-sm text-muted-foreground">ไม่พบสินค้า ลองค้นหาด้วยรหัส SML หรือชื่อสินค้า</p> : <CommandGroup>
          {visibleGroups.map((group) => <CommandItem key={group.key} value={group.key} onSelect={() => { onChange(group.key); setQuery('') }}>
            <Check className={cn('mt-0.5 h-4 w-4 shrink-0', value === group.key ? 'opacity-100' : 'opacity-0')} aria-hidden="true" />
            <span className="min-w-0"><span className="block truncate">{selectedGroupLabel([group], group.key)}</span><span className="block text-xs text-muted-foreground">{group.members.length} SKU ที่พร้อมควบคุม</span></span>
          </CommandItem>)}
        </CommandGroup>}
      </CommandList>
    </Command>
    <p className="text-xs text-muted-foreground">{selected ? `เลือกแล้ว: ${selectedGroupLabel([selected], selected.key)}` : 'เลือกสินค้า SML 1 รายการ ระบบจะแสดง SKU ที่จับคู่ได้ด้านล่าง'}</p>
  </div>
}

function selectedGroupLabel(groups: CandidateGroup[], key: string) {
  const group = groups.find((candidate) => candidate.key === key)
  if (!group) return key.replace('|', ' · ')
  return [group.smlItem, group.smlItemName || 'ยังไม่พบชื่อสินค้า SML', group.smlUnit].join(' · ')
}

function thaiPause(value: string) {
  if (value === 'configuration_changed') return 'การตั้งค่ากลุ่มเปลี่ยน'
  if (value === 'stock_source_changed') return 'แหล่งสต๊อก SML เปลี่ยน'
  if (value === 'execution_needs_review') return 'ตรวจผลการซิงก์ครั้งก่อนแล้วพบความไม่แน่นอน'
  if (value === 'auto_disabled_by_admin') return 'ผู้ดูแลปิดการส่งอัตโนมัติ'
  return value
}
function messageOf(cause: unknown, fallback: string) { const error = cause as { response?: { data?: { error?: string } }; message?: string }; return error?.response?.data?.error || error?.message || fallback }
