import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { AlertTriangle, Boxes, CheckCircle2, CircleOff, Info, Loader2, PackagePlus, RefreshCw, Settings2 } from 'lucide-react'

import client from '@/api/client'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Checkbox } from '@/components/ui/checkbox'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { cn } from '@/lib/utils'
import { permissionForMenu } from '@/lib/navigation'
import { useAuthStore } from '@/store/auth'
import { ConfirmDialog } from '@/components/common/ConfirmDialog'

type Source = 'shopee' | 'tiktok'
type Mode = 'quota' | 'shared'

interface Settings { warehouse_code: string; location_code: string; default_buffer_pct: number; kill_switch_enabled: boolean; config_version: number }
interface Member { id: string; source: Source; account_key: string; external_product_id: string; external_sku_id: string; external_warehouse_id?: string; marketplace_alias_id?: string; product_name: string; variant_name: string; unit_factor: number; allocation_pct: number; enabled: boolean; last_target_qty?: number; last_actual_qty?: number; last_error?: string }
interface Pool { id: string; sml_item_code: string; sml_unit_code: string; allocation_mode: Mode; buffer_pct_override?: number; shared_risk_acknowledged: boolean; status: 'draft' | 'ready' | 'paused' | 'active'; auto_enabled: boolean; kill_switch_enabled: boolean; dry_run_required: boolean; paused_reason?: string; config_version: number; last_error?: string; members: Member[] }
interface Overview { available: boolean; settings: Settings; pools: Pool[] }
interface Candidate extends Omit<Member, 'id' | 'last_target_qty' | 'last_actual_qty' | 'last_error'> { sml_item_code: string; sml_unit_code: string }
interface PreviewResult { run_id: string; sml_available_qty: number; reservation_qty: number; usable_qty: number; buffer_qty: number; distributable_qty: number; expires_at: string; lines: { member_id: string; target_qty: number; status: string; message?: string }[] }

const sourceLabel: Record<Source, string> = { shopee: 'Shopee', tiktok: 'TikTok Shop' }
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
        members: input.members.map(({ sml_item_code: _item, sml_unit_code: _unit, ...member }) => member),
      })
      setCreateOpen(false); setNotice('สร้างกลุ่มสต๊อกแบบร่างแล้ว ขั้นต่อไปคือกดตรวจสอบก่อนเปิดซิงก์จริง')
      await load()
    } catch (cause) { setError(messageOf(cause, 'สร้างกลุ่มสต๊อกไม่สำเร็จ')) } finally { setSaving(false) }
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
    <main className="space-y-4 p-4 sm:p-6" aria-busy={loading}>
      <header className="flex flex-col gap-3 border-b pb-4 sm:flex-row sm:items-start sm:justify-between">
        <div>
          <div className="flex flex-wrap items-center gap-2"><h1 className="text-xl font-semibold">ควบคุมสต๊อก Marketplace</h1><Badge variant="outline">AOY</Badge></div>
          <p className="mt-1 max-w-3xl text-sm text-muted-foreground">SML เป็นยอดกลาง ระบบจะคำนวณยอดพร้อมขายแล้วส่งยอดไป Shopee และ TikTok Shop ตามนโยบายของแต่ละกลุ่มสินค้า</p>
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

      <section className="grid gap-3 sm:grid-cols-3" aria-label="ภาพรวมการควบคุมสต๊อก">
        <Metric label="แหล่งสต๊อก SML" value={data?.settings.warehouse_code ? `${data.settings.warehouse_code} / ${data.settings.location_code}` : 'ยังไม่ได้เลือก'} detail="ใช้ร่วมกันทั้ง Shopee และ TikTok" />
        <Metric label="กันสต๊อกเริ่มต้น" value={data ? `${data.settings.default_buffer_pct}%` : '—'} detail="ปรับเฉพาะกลุ่มสินค้าได้" />
        <Metric label="เปิด Auto แล้ว" value={`${activePools} กลุ่ม`} detail="กลุ่มใหม่เริ่มปิดเสมอ" />
      </section>

      <Tabs value={tab} onValueChange={(value) => setTab(value as 'all' | Source)}>
        <TabsList aria-label="กรองช่องทาง"><TabsTrigger value="all">ทั้งหมด</TabsTrigger><TabsTrigger value="shopee">Shopee</TabsTrigger><TabsTrigger value="tiktok">TikTok Shop</TabsTrigger></TabsList>
        <TabsContent value={tab} className="space-y-3">
          {loading ? <LoadingRows /> : filteredPools.length === 0 ? <EmptyState canManage={canManage} onCreate={() => void openCreate()} /> : filteredPools.map((pool) => <PoolCard key={pool.id} pool={pool} canManage={canManage} canOperate={canOperate} preview={previews[pool.id]} previewing={previewingPoolID === pool.id} actioning={actioningPoolID === pool.id} onPreview={() => void previewPool(pool)} onAuto={(enabled) => setConfirmAction({ pool, kind: enabled ? 'auto-on' : 'auto-off' })} onSync={() => setConfirmAction({ pool, kind: 'sync' })} />)}
        </TabsContent>
      </Tabs>

      {data && <SettingsDialog open={settingsOpen} settings={data.settings} saving={saving} onOpenChange={setSettingsOpen} onSave={saveSettings} />}
      <CreatePoolDialog open={createOpen} candidates={candidates} saving={saving} onOpenChange={setCreateOpen} onCreate={createPool} />
      <ConfirmDialog open={Boolean(confirmAction)} onOpenChange={(open) => !open && setConfirmAction(null)}
        title={confirmAction?.kind === 'auto-on' ? 'เปิดส่งสต๊อกอัตโนมัติหรือไม่?' : confirmAction?.kind === 'auto-off' ? 'ปิดส่งสต๊อกอัตโนมัติหรือไม่?' : 'ส่งงานซิงก์สต๊อกด้วยมือหรือไม่?'}
        description={confirmAction?.kind === 'auto-on' ? 'ระบบจะเริ่มเฉพาะงานใหม่หลังจากผ่าน dry-run และการซิงก์ด้วยมือสำเร็จแล้ว ไม่ส่งย้อนหลัง' : confirmAction?.kind === 'auto-off' ? 'งานที่ยังไม่เริ่มจะถูกยกเลิก แต่งานที่เริ่มเขียนแล้วอาจทำต่อจนตรวจ read-back เสร็จ เพื่อไม่ให้ยอดค้างกลางทาง' : 'ระบบจะอ่าน SML และยอด Marketplace ล่าสุดอีกครั้งก่อนเขียน แล้วอ่านกลับทุก SKU ที่เปลี่ยน'}
        confirmLabel={confirmAction?.kind === 'auto-on' ? 'เปิด Auto' : confirmAction?.kind === 'auto-off' ? 'ปิด Auto' : 'ยืนยันส่งงาน'}
        variant={confirmAction?.kind === 'auto-off' ? 'destructive' : 'default'}
        onConfirm={async () => { if (!confirmAction) return; if (confirmAction.kind === 'sync') await queueSync(confirmAction.pool); else await updateAuto(confirmAction.pool, confirmAction.kind === 'auto-on') }} />
    </main>
  )
}

function Metric({ label, value, detail }: { label: string; value: string; detail: string }) {
  return <Card><CardContent className="p-4"><p className="text-sm text-muted-foreground">{label}</p><p className="mt-1 text-lg font-semibold">{value}</p><p className="mt-1 text-xs text-muted-foreground">{detail}</p></CardContent></Card>
}

function PoolCard({ pool, canManage, canOperate, preview, previewing, actioning, onPreview, onAuto, onSync }: { pool: Pool; canManage: boolean; canOperate: boolean; preview?: PreviewResult; previewing: boolean; actioning: boolean; onPreview: () => void; onAuto: (enabled: boolean) => void; onSync: () => void }) {
  const channels = [...new Set(pool.members.map((member) => member.source))]
  return <Card><CardHeader className="gap-2 p-4 sm:flex-row sm:items-start sm:justify-between"><div><CardTitle className="text-base">{pool.sml_item_code} <span className="font-normal text-muted-foreground">· {pool.sml_unit_code}</span></CardTitle><CardDescription className="mt-1">{pool.allocation_mode === 'quota' ? 'แบ่งโควตาตามสัดส่วนที่กำหนด' : 'ใช้สต๊อกร่วมทุกช่องทาง'} · กันสต๊อก {pool.buffer_pct_override ?? 'ค่าเริ่มต้น'}%</CardDescription></div><div className="flex flex-wrap items-center gap-2"><Badge variant={pool.status === 'active' ? 'default' : 'secondary'}>{poolStatus[pool.status]}</Badge><Badge variant="outline">{pool.auto_enabled ? 'Auto ทุก 5 นาที' : 'Auto ปิด'}</Badge>{canOperate && pool.status !== 'paused' && <Button variant="outline" size="sm" onClick={onPreview} disabled={previewing || actioning}><RefreshCw className={cn('mr-2 h-4 w-4', previewing && 'animate-spin')} />ตรวจ SML</Button>}{canOperate && !pool.dry_run_required && pool.status !== 'paused' && <Button size="sm" onClick={onSync} disabled={actioning}>ซิงก์ด้วยมือ</Button>}{canManage && <label className="flex items-center gap-2 rounded-md border px-2 py-1 text-xs"><Switch checked={pool.auto_enabled} onCheckedChange={onAuto} disabled={actioning || pool.dry_run_required || pool.kill_switch_enabled} /><span>ส่งอัตโนมัติ</span></label>}</div></CardHeader><CardContent className="grid gap-2 p-4 pt-0 sm:grid-cols-2">{pool.members.map((member) => <div key={member.id} className="rounded-md border px-3 py-2 text-sm"><div className="flex items-center justify-between gap-2"><span className="font-medium">{sourceLabel[member.source]}</span><span className="text-muted-foreground">{pool.allocation_mode === 'quota' ? `${member.allocation_pct}%` : 'ยอดร่วม'}</span></div><p className="mt-1 truncate text-muted-foreground" title={[member.product_name, member.variant_name].filter(Boolean).join(' · ')}>{[member.product_name, member.variant_name].filter(Boolean).join(' · ') || 'ยังไม่มีชื่อสินค้า'}</p>{member.last_error && <p className="mt-1 text-xs text-destructive">ต้องตรวจ: {member.last_error}</p>}</div>)}</CardContent>{preview && <div className="border-t bg-muted/30 px-4 py-3 text-sm"><p className="font-medium">แผนจาก SML: พร้อมใช้ {preview.usable_qty} {pool.sml_unit_code} · กันชน {preview.buffer_qty} · ส่งออกได้ {preview.distributable_qty}</p><p className="mt-1 text-xs text-muted-foreground">หัก reservation {preview.reservation_qty} แล้ว; แผนหมดอายุใน 60 วินาที และยังไม่ได้อ่านหรือเขียนยอด Marketplace</p><div className="mt-2 flex flex-wrap gap-2">{preview.lines.map((line) => <Badge key={line.member_id} variant="outline">SKU → {line.target_qty}</Badge>)}</div></div>}{pool.paused_reason && <div className="border-t px-4 py-2 text-xs text-warning">หยุดชั่วคราว: {thaiPause(pool.paused_reason)}</div>}{channels.length === 0 && <div className="border-t px-4 py-2 text-xs text-muted-foreground">ยังไม่มี SKU ในกลุ่มนี้</div>}</Card>
}

function EmptyState({ canManage, onCreate }: { canManage: boolean; onCreate: () => void }) { return <Card><CardContent className="flex flex-col items-center px-6 py-12 text-center"><Boxes className="h-9 w-9 text-muted-foreground" /><h2 className="mt-3 font-semibold">ยังไม่มีกลุ่มสต๊อก</h2><p className="mt-1 max-w-md text-sm text-muted-foreground">เริ่มจากจับคู่สินค้า Marketplace กับสินค้า SML ให้พร้อม แล้วสร้างกลุ่มเพื่อเลือกว่าจะแบ่งโควตาหรือใช้สต๊อกร่วม</p>{canManage && <Button className="mt-4" onClick={onCreate}>สร้างกลุ่มสต๊อก</Button>}</CardContent></Card> }
function LoadingRows() { return <div className="space-y-3" aria-label="กำลังโหลด"><div className="h-28 animate-pulse rounded-md bg-muted" /><div className="h-28 animate-pulse rounded-md bg-muted" /></div> }

function SettingsDialog({ open, settings, saving, onOpenChange, onSave }: { open: boolean; settings: Settings; saving: boolean; onOpenChange: (open: boolean) => void; onSave: (settings: Settings) => void }) {
  const [draft, setDraft] = useState(settings)
  useEffect(() => { if (open) setDraft(settings) }, [open, settings])
  return <Dialog open={open} onOpenChange={(next) => !saving && onOpenChange(next)}><DialogContent><DialogHeader><DialogTitle>แหล่งสต๊อก SML และกันชน</DialogTitle><DialogDescription>การเปลี่ยนคลังหรือกันชนจะหยุดทุกกลุ่มไว้ก่อน เพื่อให้ตรวจสอบและ dry-run ใหม่</DialogDescription></DialogHeader><div className="grid gap-3 sm:grid-cols-2"><div className="space-y-1"><Label htmlFor="warehouse">คลัง SML</Label><Input id="warehouse" value={draft.warehouse_code} onChange={(event) => setDraft({ ...draft, warehouse_code: event.target.value })} /></div><div className="space-y-1"><Label htmlFor="location">พื้นที่เก็บ</Label><Input id="location" value={draft.location_code} onChange={(event) => setDraft({ ...draft, location_code: event.target.value })} /></div><div className="space-y-1"><Label htmlFor="buffer">กันสต๊อกเริ่มต้น (%)</Label><Input id="buffer" type="number" min="0" max="100" value={draft.default_buffer_pct} onChange={(event) => setDraft({ ...draft, default_buffer_pct: Number(event.target.value) })} /></div><label className="flex items-center gap-2 pt-6 text-sm"><Switch checked={draft.kill_switch_enabled} onCheckedChange={(checked) => setDraft({ ...draft, kill_switch_enabled: checked })} />หยุดส่งสต๊อกทั้งร้าน</label></div><DialogFooter><Button variant="outline" onClick={() => onOpenChange(false)} disabled={saving}>ยกเลิก</Button><Button onClick={() => onSave(draft)} disabled={saving || !draft.warehouse_code || !draft.location_code}>{saving && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}ยืนยันบันทึก</Button></DialogFooter></DialogContent></Dialog>
}

function CreatePoolDialog({ open, candidates, saving, onOpenChange, onCreate }: { open: boolean; candidates: Candidate[]; saving: boolean; onOpenChange: (open: boolean) => void; onCreate: (input: { smlItem: string; smlUnit: string; mode: Mode; sharedAcknowledged: boolean; members: Candidate[] }) => void }) {
  const groups = useMemo(() => {
    const grouped = new Map<string, Candidate[]>()
    for (const candidate of candidates) {
      const key = `${candidate.sml_item_code}|${candidate.sml_unit_code}`
      grouped.set(key, [...(grouped.get(key) ?? []), candidate])
    }
    return [...grouped.entries()].map(([key, members]) => ({
      key,
      smlItem: members[0]?.sml_item_code ?? '',
      smlUnit: members[0]?.sml_unit_code ?? '',
      members,
      searchableText: [members[0]?.sml_item_code, members[0]?.sml_unit_code, ...members.flatMap((member) => [member.product_name, member.variant_name])].join(' ').toLocaleLowerCase(),
    }))
  }, [candidates])
  const [groupKey, setGroupKey] = useState('')
  const [groupSearch, setGroupSearch] = useState('')
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
    const first = groups[0]?.key ?? ''
    setGroupKey(first); setGroupSearch(''); setMode('quota'); setSharedAcknowledged(false); setMembers(draftMembers(first)); setReviewingCreate(false)
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
  const visibleGroups = groups.filter((group) => !groupSearch.trim() || group.searchableText.includes(groupSearch.trim().toLocaleLowerCase()))
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
        <Alert><Info className="h-4 w-4" /><AlertTitle>{groupKey.replace('|', ' · ')}</AlertTitle><AlertDescription>{summary}<br />ขั้นต่อไปต้องกด Dry-run เพื่ออ่านยอด SML ก่อน จึงจะมีสิทธิ์สั่งซิงก์ด้วยมือ</AlertDescription></Alert>
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
          <div className="space-y-1"><Label htmlFor="marketplace-stock-group-search">ค้นหาสินค้า SML หรือชื่อสินค้า Marketplace</Label><Input id="marketplace-stock-group-search" value={groupSearch} onChange={(event) => setGroupSearch(event.target.value)} placeholder="เช่น AH-0029 หรือ สีชมพู" /></div>
          <div className="space-y-1"><Label>สินค้าและหน่วย SML</Label><Select value={groupKey} onValueChange={changeGroup}><SelectTrigger><SelectValue /></SelectTrigger><SelectContent>{visibleGroups.map((group) => <SelectItem key={group.key} value={group.key}>{group.smlItem} · {group.smlUnit} · {group.members.length} SKU</SelectItem>)}</SelectContent></Select>{visibleGroups.length === 0 && <p className="text-xs text-destructive">ไม่พบสินค้าในคำค้นหา ลองค้นหาด้วยรหัส SML หรือชื่อสินค้า</p>}</div>
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

function thaiPause(value: string) { return value === 'configuration_changed' ? 'การตั้งค่ากลุ่มเปลี่ยน' : value === 'stock_source_changed' ? 'แหล่งสต๊อก SML เปลี่ยน' : value }
function messageOf(cause: unknown, fallback: string) { const error = cause as { response?: { data?: { error?: string } }; message?: string }; return error?.response?.data?.error || error?.message || fallback }
