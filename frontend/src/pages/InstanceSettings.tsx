import { useEffect, useMemo, useState } from 'react'
import { AlertCircle, AlertTriangle, Bell, Bot, Building2, CheckCircle2, Database, FileClock, PackageCheck, Plug, ReceiptText, RotateCw, Save, Settings2, ShieldCheck, XCircle } from 'lucide-react'
import { toast } from 'sonner'

import client from '@/api/client'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { ConfirmDialog } from '@/components/common/ConfirmDialog'
import { PageHeader } from '@/components/common/PageHeader'
import { cn } from '@/lib/utils'

type SettingGroup = 'instance' | 'sml' | 'sml_db' | 'line' | 'ai' | 'automation'

type InstanceSetting = {
  key: string
  label: string
  group: SettingGroup
  type: 'text' | 'url' | 'number' | 'password'
  value: string
  source: 'database' | 'env' | 'default' | 'unset'
  env_key?: string
  secret?: boolean
  has_secret?: boolean
  restart_required?: boolean
  description?: string
  overridden?: boolean
  runtime_value?: string
  active?: boolean
  pending_restart?: boolean
  locked?: boolean
  missing?: boolean
}

type Response = {
  settings: InstanceSetting[]
  note?: string
  pending_restart?: boolean
  pending_restart_settings?: string[]
}

type TestResult = {
  ok: boolean
  skipped?: boolean
  error?: string
  detail?: string
  layer?: string
  http_status?: number
  latency_ms?: number
}
type TestResults = {
  sml?: TestResult
  sml_proxy?: TestResult
  sml_tenant?: TestResult
  sml_stock_request?: TestResult
  line?: TestResult
  openrouter?: TestResult
  db?: TestResult
}
type ShopeeAPIStatus = {
  enabled: boolean
  connected: boolean
  shop_id?: number
  shop_name?: string
  redirect_url?: string
  blocking_reason?: string
}

type SetupStatusLite = {
  blocking_ready_count: number
  blocking_total_count: number
  steps: Array<{ key: string; ready: boolean; status: string; blocking?: boolean }>
  documents: {
    pending: number
    needs_review: number
    failed: number
    sent: number
    saleinvoice: number
  }
}

const GROUP_META: Record<SettingGroup, { title: string; description: string; icon: typeof Building2 }> = {
  instance: {
    title: 'ข้อมูลร้าน (ไม่บังคับ)',
    description: 'ใช้เป็นป้ายกำกับให้ทีมดูแลระบบ ไม่เกี่ยวกับการส่ง SML หรือ LINE',
    icon: Building2,
  },
  sml: {
    title: 'SML ERP',
    description: 'ข้อมูลเชื่อมต่อ SML ผ่าน sml-api-byboss และ endpoint คำนวณต้นทุนสต๊อก',
    icon: Database,
  },
  sml_db: {
    title: 'SML Database Connection',
    description: 'ข้อมูลเชื่อมต่อ PostgreSQL ของร้านค้านี้ — ส่งเป็น X-DB-* headers ไปยัง sml-api-byboss ทุก request ไม่ต้อง restart',
    icon: Database,
  },
  line: {
    title: 'LINE แจ้งเตือนระบบ',
    description: 'Token และ userId สำหรับส่ง error/สถานะระบบไปหาแอดมิน',
    icon: Bell,
  },
  ai: {
    title: 'OpenRouter AI',
    description: 'API key และ model ที่ใช้ดึงข้อมูลจากอีเมล',
    icon: Bot,
  },
  automation: {
    title: 'Automation',
    description: 'ค่าควบคุมการทำงานอัตโนมัติ',
    icon: Settings2,
  },
}

const GROUP_ORDER: SettingGroup[] = ['instance', 'sml', 'sml_db', 'line', 'ai', 'automation']
const PHASE = Number(import.meta.env.VITE_PHASE ?? 99)

const PHASE1_HIDDEN_KEYS = new Set([
  'sml.json_rpc_base_url',
  'ai.openrouter_audio_model',
  'automation.auto_confirm_threshold',
])

const TEST_SERVICE_LABEL: Record<string, string> = {
  sml: 'SML ERP ภาพรวม',
  sml_proxy: 'sml-api-byboss proxy',
  sml_tenant: 'Tenant/Product lookup',
  sml_stock_request: 'Stock Request URL',
  line: 'LINE แจ้งเตือน',
  openrouter: 'OpenRouter AI',
  db: 'SML Database',
}
const TEST_RESULT_ORDER: Array<keyof TestResults> = [
  'sml_proxy',
  'sml_tenant',
  'sml_stock_request',
  'line',
  'openrouter',
  'db',
]

function testResultTone(result: TestResult): 'ok' | 'warn' | 'danger' {
  if (result.ok && result.skipped) return 'warn'
  if (result.ok) return 'ok'
  return 'danger'
}

function testResultMeta(result: TestResult) {
  const parts: string[] = []
  if (result.http_status) parts.push(`HTTP ${result.http_status}`)
  if (typeof result.latency_ms === 'number' && result.latency_ms > 0) parts.push(`${result.latency_ms}ms`)
  return parts.join(' · ')
}

function sourceLabel(s: InstanceSetting) {
  if (s.locked) return 'ค่าตายตัว'
  if (s.source === 'database') return 'ตั้งค่าในหน้านี้'
  if (s.source === 'env') return 'ค่าจาก env'
  if (s.source === 'default') return 'ค่าเริ่มต้น'
  return 'ยังไม่ได้ตั้งค่า'
}

function sourceBadgeVariant(s: InstanceSetting): 'default' | 'outline' | 'secondary' {
  if (s.locked) return 'secondary'
  if (s.source === 'database') return 'default'
  if (s.source === 'env' || s.source === 'default') return 'secondary'
  return 'outline'
}

function isCriticalSetting(s: InstanceSetting) {
  const key = s.key.toLowerCase()
  return (
    s.restart_required ||
    s.group === 'sml' ||
    s.group === 'sml_db' ||
    s.group === 'ai' ||
    s.secret ||
    key.includes('public') ||
    key.includes('redirect') ||
    key.includes('url') ||
    key.includes('database') ||
    key.includes('provider')
  )
}

function settingPreviewValue(s: InstanceSetting, value: string) {
  if (s.secret || s.type === 'password') return value ? '••••••••' : 'ว่าง'
  return value || 'ว่าง'
}

function ReadinessMini({
  icon: Icon,
  label,
  value,
  detail,
  tone,
}: {
  icon: typeof Building2
  label: string
  value: string
  detail: string
  tone: 'ok' | 'warn' | 'danger'
}) {
  return (
    <div
      className={cn(
        'rounded-md border p-3',
        tone === 'ok' && 'border-success/25 bg-success/[0.04]',
        tone === 'warn' && 'border-warning/30 bg-warning/[0.06]',
        tone === 'danger' && 'border-destructive/25 bg-destructive/[0.05]',
      )}
    >
      <div className="flex items-start gap-2.5">
        <Icon
          className={cn(
            'mt-0.5 h-4 w-4 shrink-0',
            tone === 'ok' && 'text-success',
            tone === 'warn' && 'text-warning',
            tone === 'danger' && 'text-destructive',
          )}
        />
        <div className="min-w-0">
          <div className="text-[11px] text-muted-foreground">{label}</div>
          <div className="mt-0.5 truncate text-sm font-semibold text-foreground">{value}</div>
          <div className="mt-1 line-clamp-2 text-[11px] leading-relaxed text-muted-foreground">{detail}</div>
        </div>
      </div>
    </div>
  )
}

export default function InstanceSettings() {
  const [settings, setSettings] = useState<InstanceSetting[]>([])
  const [setupStatus, setSetupStatus] = useState<SetupStatusLite | null>(null)
  const [draft, setDraft] = useState<Record<string, string>>({})
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [restarting, setRestarting] = useState(false)
  const [pendingRestart, setPendingRestart] = useState(false)
  const [restartKeys, setRestartKeys] = useState<string[]>([])

  const [testing, setTesting] = useState(false)
  const [testResults, setTestResults] = useState<TestResults | null>(null)
  const [shopeeAPIStatus, setShopeeAPIStatus] = useState<ShopeeAPIStatus | null>(null)

  const [confirmSave, setConfirmSave] = useState(false)
  const [confirmSaveDesc, setConfirmSaveDesc] = useState('')
  const [confirmRestart, setConfirmRestart] = useState(false)

  const load = async () => {
    setLoading(true)
    try {
      const res = await client.get<Response>('/api/settings/instance')
      setSettings(res.data.settings ?? [])
      setPendingRestart(!!res.data.pending_restart)
      setRestartKeys(res.data.pending_restart_settings ?? [])
      setDraft(
        Object.fromEntries((res.data.settings ?? []).map((s) => [s.key, s.value ?? ''])),
      )
      client
        .get<ShopeeAPIStatus>('/api/settings/shopee-api/status')
        .then((statusRes) => setShopeeAPIStatus(statusRes.data))
        .catch(() => setShopeeAPIStatus(null))
      client
        .get<SetupStatusLite>('/api/setup/status')
        .then((statusRes) => setSetupStatus(statusRes.data))
        .catch(() => setSetupStatus(null))
    } catch {
      toast.error('โหลดค่าการเชื่อมต่อไม่สำเร็จ')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    load()
  }, [])

  const grouped = useMemo(() => {
    return GROUP_ORDER.map((group) => ({
      group,
      items: settings.filter((s) => !s.locked && s.group === group && !(PHASE < 2 && PHASE1_HIDDEN_KEYS.has(s.key))),
    })).filter((g) => g.items.length > 0)
  }, [settings])
  const criticalGrouped = useMemo(() => (
    grouped
      .map(({ group, items }) => ({ group, items: items.filter(isCriticalSetting) }))
      .filter((g) => g.items.length > 0)
  ), [grouped])
  const optionalGrouped = useMemo(() => (
    grouped
      .map(({ group, items }) => ({ group, items: items.filter((s) => !isCriticalSetting(s)) }))
      .filter((g) => g.items.length > 0)
  ), [grouped])

  const visibleKeys = useMemo(
    () => new Set(grouped.flatMap((g) => g.items.map((s) => s.key))),
    [grouped],
  )
  const shopeeRedirectHost = useMemo(() => {
    if (!shopeeAPIStatus?.redirect_url) return ''
    try {
      return new URL(shopeeAPIStatus.redirect_url).host
    } catch {
      return ''
    }
  }, [shopeeAPIStatus?.redirect_url])
  const currentHost = typeof window !== 'undefined' ? window.location.host : ''
  const shopeeRedirectMismatch = Boolean(shopeeAPIStatus?.enabled && shopeeRedirectHost && currentHost && shopeeRedirectHost !== currentHost)
  const setupStep = (key: string) => setupStatus?.steps.find((step) => step.key === key)
  const channelsStep = setupStep('channels')
  const catalogStep = setupStep('catalog')
  const docs = setupStatus?.documents
  const pendingWork = (docs?.pending ?? 0) + (docs?.needs_review ?? 0)
  const readinessLabel = setupStatus
    ? `${setupStatus.blocking_ready_count}/${setupStatus.blocking_total_count}`
    : 'กำลังโหลด'

  const waitForBackend = async () => {
    await new Promise((resolve) => setTimeout(resolve, 1200))
    for (let i = 0; i < 24; i += 1) {
      try {
        await client.get('/health', { timeout: 2000 })
        return
      } catch {
        await new Promise((resolve) => setTimeout(resolve, 1500))
      }
    }
    throw new Error('backend restart timeout')
  }

  const doSave = async () => {
    setSaving(true)
    setRestarting(false)
    const toastID = toast.loading('กำลังบันทึกค่า...')
    try {
      const payload = Object.fromEntries(
        Object.entries(draft).filter(([key]) => visibleKeys.has(key)),
      )
      await client.put('/api/settings/instance', { settings: payload })
      await load()
      setRestarting(true)
      toast.loading('บันทึกแล้ว กำลังเริ่มใช้ค่าใหม่...', { id: toastID })
      await client.post('/api/settings/instance/restart', {}, { timeout: 5000 })
      await waitForBackend()
      toast.success('บันทึกค่าแล้ว ระบบพร้อมใช้ค่าใหม่', { id: toastID })
      setTestResults(null)
      await load()
    } catch {
      toast.error('บันทึกหรือเริ่มใช้ค่าใหม่ไม่สำเร็จ', { id: toastID })
    } finally {
      setSaving(false)
      setRestarting(false)
    }
  }

  const requestSave = () => {
    if (saving || restarting || loading) return
    const changed = settings.filter(
      (s) => visibleKeys.has(s.key) && !s.locked && (draft[s.key] ?? '') !== (s.value ?? ''),
    )
    const important = changed.filter(isCriticalSetting)
    if (important.length > 0) {
      const labels = important.slice(0, 5).map((s) => {
        const before = settingPreviewValue(s, s.value ?? '')
        const after = settingPreviewValue(s, draft[s.key] ?? '')
        return `- ${s.label}: ${before} เป็น ${after}`
      }).join('\n')
      const more = important.length > 5 ? ` และอีก ${important.length - 5} ค่า` : ''
      setConfirmSaveDesc(`ค่าที่จะเปลี่ยนบน production:\n${labels}${more}\n\nผลกระทบ: backend จะ restart ประมาณ 10-30 วินาที\nตรวจหลังบันทึก: health, SML connection, Shopee status, channel routes และ logs ล่าสุด\nRollback: กลับมาใส่ค่าเดิม แล้วกดบันทึกและเริ่มใช้ค่าใหม่อีกครั้ง`)
      setConfirmSave(true)
    } else {
      void doSave()
    }
  }

  const doRestart = async () => {
    setRestarting(true)
    const toastID = toast.loading('กำลังรีสตาร์ท backend...')
    try {
      await client.post('/api/settings/instance/restart', {}, { timeout: 5000 })
      await waitForBackend()
      toast.success('backend กลับมาแล้ว และใช้ค่าล่าสุดแล้ว', { id: toastID })
      await load()
    } catch {
      toast.error('รีสตาร์ทไม่สำเร็จหรือ backend กลับมาช้าเกินไป', { id: toastID })
    } finally {
      setRestarting(false)
    }
  }

  const testConnection = async () => {
    setTesting(true)
    setTestResults(null)
    try {
      const payload = Object.fromEntries(
        Object.entries(draft).filter(([key]) => visibleKeys.has(key)),
      )
      const res = await client.post<TestResults>('/api/settings/instance/test-connection', { settings: payload })
      setTestResults(res.data)
    } catch {
      toast.error('ทดสอบการเชื่อมต่อไม่สำเร็จ')
    } finally {
      setTesting(false)
    }
  }

  return (
    <div className="space-y-5">
      <PageHeader
        title="การเชื่อมต่อระบบ"
        description={PHASE < 2
          ? 'ตั้งค่าเฉพาะที่ใช้ใน Phase 1: SML ผ่าน sml-api-byboss, LINE แจ้งเตือนระบบ และ OpenRouter'
          : 'ตั้งค่า SML ERP, OpenRouter และข้อมูลร้านที่ใช้กับ Nexflow ชุดนี้'}
        actions={
          <div className="flex flex-wrap items-center gap-2">
            <Button variant="outline" onClick={testConnection} disabled={testing || saving || restarting || loading}>
              <Plug className={cn('h-4 w-4', testing && 'animate-pulse')} />
              {testing ? 'กำลังทดสอบ...' : 'ทดสอบค่าที่กรอกอยู่'}
            </Button>
            {pendingRestart && (
              <Button variant="outline" onClick={() => setConfirmRestart(true)} disabled={saving || restarting || loading}>
                <RotateCw className={cn('h-4 w-4', restarting && 'animate-spin')} />
                {restarting ? 'กำลังรีสตาร์ท...' : 'รีสตาร์ทและใช้ค่าทันที'}
              </Button>
            )}
            <Button onClick={requestSave} disabled={saving || restarting || loading}>
              <Save className="h-4 w-4" />
              {restarting ? 'กำลังเริ่มใช้ค่าใหม่...' : saving ? 'กำลังบันทึก...' : 'บันทึกและเริ่มใช้ค่าใหม่'}
            </Button>
          </div>
        }
      />

      <Card className="border-primary/20 bg-primary/[0.03] shadow-none">
        <CardContent className="space-y-3 p-4">
          <div className="flex flex-col gap-2 lg:flex-row lg:items-start lg:justify-between">
            <div className="min-w-0">
              <p className="text-sm font-semibold text-foreground">Production readiness</p>
              <p className="mt-0.5 text-xs leading-relaxed text-muted-foreground">
                สรุปค่าที่มีผลต่อการใช้งานจริงก่อนบันทึกหรือ restart: SML, route, catalog, Shopee/SI และงานค้าง
              </p>
            </div>
            <Badge variant="outline" className="w-fit bg-background text-xs">
              ขั้นตอนสำคัญ {readinessLabel}
            </Badge>
          </div>
          <div className="grid gap-2 sm:grid-cols-2 xl:grid-cols-5">
            <ReadinessMini
              icon={ShieldCheck}
              label="Backend"
              value={pendingRestart ? 'มีค่ารอเริ่มใช้' : 'ใช้ค่าล่าสุดแล้ว'}
              detail={pendingRestart ? 'กด restart ก่อนส่ง SML หรือ sync สินค้า' : 'ค่าที่บันทึกตรงกับ runtime'}
              tone={pendingRestart ? 'warn' : 'ok'}
            />
            <ReadinessMini
              icon={Database}
              label="SML ERP"
              value={testResults?.sml ? (testResults.sml.ok ? 'ทดสอบผ่าน' : 'ทดสอบไม่ผ่าน') : 'รอทดสอบค่า'}
              detail={testResults?.sml?.detail || testResults?.sml?.error || 'กดทดสอบค่าที่กรอกอยู่ก่อนบันทึก production'}
              tone={testResults?.sml ? (testResults.sml.ok ? 'ok' : 'danger') : 'warn'}
            />
            <ReadinessMini
              icon={FileClock}
              label="เส้นทางเอกสาร"
              value={channelsStep?.ready ? 'พร้อมใช้งาน' : 'ต้องตั้งค่า'}
              detail={channelsStep?.status || 'Shopee/Marketplace ควรชี้ไปขายสินค้าและบริการ / SI'}
              tone={channelsStep?.ready ? 'ok' : 'danger'}
            />
            <ReadinessMini
              icon={PackageCheck}
              label="สินค้าใน SML"
              value={catalogStep?.ready ? 'พร้อมจับคู่' : 'ควรซิงก์สินค้า'}
              detail={catalogStep?.status || 'ช่วยลด mapping ผิดก่อนส่งเอกสารจริง'}
              tone={catalogStep?.ready ? 'ok' : 'warn'}
            />
            <ReadinessMini
              icon={ReceiptText}
              label="คิวขายสินค้า"
              value={(docs?.failed ?? 0) > 0 ? `${(docs?.failed ?? 0).toLocaleString('th-TH')} ส่งไม่สำเร็จ` : `${(docs?.saleinvoice ?? 0).toLocaleString('th-TH')} SI`}
              detail={`ส่งแล้ว ${(docs?.sent ?? 0).toLocaleString('th-TH')} · ค้าง ${pendingWork.toLocaleString('th-TH')}`}
              tone={(docs?.failed ?? 0) > 0 ? 'danger' : pendingWork > 0 ? 'warn' : 'ok'}
            />
          </div>
        </CardContent>
      </Card>

      {/* Status banner */}
      {pendingRestart && (
      <div className="rounded-lg border border-warning/35 bg-warning/[0.07] p-3 text-sm">
        <div className="flex gap-2.5">
          <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0 text-warning" />
          <div>
            <p className="font-medium text-foreground">มีค่าที่บันทึกแล้ว แต่ backend ยังไม่ได้เริ่มใช้</p>
            <p className="mt-0.5 text-xs leading-relaxed text-muted-foreground">
              กด "รีสตาร์ทและใช้ค่าทันที" ก่อน Sync สินค้าหรือส่ง SML เพื่อไม่ให้ระบบใช้ headers ชุดเก่า
            </p>
            {restartKeys.length > 0 && (
              <div className="mt-2 flex flex-wrap gap-1">
                {restartKeys.map((key) => (
                  <Badge key={key} variant="outline" className="h-5 px-1.5 text-[10px]">
                    {settings.find((s) => s.key === key)?.label ?? key}
                  </Badge>
                ))}
              </div>
            )}
          </div>
        </div>
      </div>
      )}

      {shopeeAPIStatus && (!shopeeAPIStatus.enabled || shopeeRedirectMismatch) && (
        <div className="rounded-lg border border-warning/35 bg-warning/[0.07] p-3 text-sm">
          <div className="flex gap-2.5">
            <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0 text-warning" />
            <div className="min-w-0">
              <p className="font-medium text-foreground">
                {shopeeAPIStatus.enabled ? 'Shopee Redirect URL อาจไม่ตรงกับ instance นี้' : 'Shopee API ปิดใช้งานใน instance นี้'}
              </p>
              <p className="mt-0.5 text-xs leading-relaxed text-muted-foreground">
                {shopeeAPIStatus.enabled
                  ? `หน้าปัจจุบันคือ ${currentHost || 'ไม่ทราบ host'} แต่ redirect URL ชี้ไปที่ ${shopeeRedirectHost}. ให้ตรวจ PUBLIC_BASE_URL / SHOPEE_OPEN_API_REDIRECT_URL ก่อนเชื่อมร้าน`
                  : shopeeAPIStatus.connected
                    ? `มีข้อมูลร้านที่เคยเชื่อมต่อ (${shopeeAPIStatus.shop_name || shopeeAPIStatus.shop_id || 'Shopee shop'}) แต่ระบบปิดการใช้งาน API อยู่ จึงไม่ถือว่าพร้อมใช้งาน`
                    : shopeeAPIStatus.blocking_reason || 'เปิด SHOPEE_OPEN_API_ENABLED=true เมื่อต้องการใช้ Shopee API'}
              </p>
            </div>
          </div>
        </div>
      )}

      {/* Test connection results */}
      {testResults && (
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="flex items-center gap-2 text-sm">
              <Plug className="h-4 w-4 text-accent-strong" />
              ผลการทดสอบการเชื่อมต่อ
            </CardTitle>
          </CardHeader>
          <CardContent>
            {testResults.sml && !testResults.sml.ok && (
              <div className="mb-3 rounded-md border border-destructive/25 bg-destructive/[0.04] px-3 py-2 text-xs leading-relaxed">
                <div className="font-medium text-destructive">SML ERP ยังไม่พร้อม</div>
                <div className="mt-0.5 text-muted-foreground">
                  {testResults.sml.error || testResults.sml.detail || 'ตรวจรายละเอียดแยกชั้นด้านล่าง'}
                </div>
              </div>
            )}
            <div className="grid gap-2 lg:grid-cols-2">
              {TEST_RESULT_ORDER.map((svc) => {
                const r = testResults[svc]
                if (!r) return null
                const tone = testResultTone(r)
                const meta = testResultMeta(r)
                return (
                  <div key={svc} className={cn(
                    'flex items-start gap-3 rounded-md border px-3 py-2 text-sm',
                    tone === 'ok' && 'border-success/25 bg-success/[0.04]',
                    tone === 'warn' && 'border-warning/30 bg-warning/[0.06]',
                    tone === 'danger' && 'border-destructive/25 bg-destructive/[0.04]',
                  )}>
                    {tone === 'ok' && <CheckCircle2 className="mt-0.5 h-4 w-4 shrink-0 text-success" />}
                    {tone === 'warn' && <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0 text-warning" />}
                    {tone === 'danger' && <XCircle className="mt-0.5 h-4 w-4 shrink-0 text-destructive" />}
                    <div className="min-w-0">
                      <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
                        <span className="font-medium">{TEST_SERVICE_LABEL[svc]}</span>
                        {r.skipped && (
                          <Badge variant="outline" className="h-5 px-1.5 text-[10px]">
                            optional
                          </Badge>
                        )}
                        {meta && <span className="text-[11px] text-muted-foreground">{meta}</span>}
                      </div>
                      {r.detail && <p className="mt-0.5 text-xs leading-relaxed text-muted-foreground">{r.detail}</p>}
                      {r.error && <p className="mt-0.5 text-xs text-destructive">{r.error}</p>}
                    </div>
                  </div>
                )
              })}
            </div>
          </CardContent>
        </Card>
      )}

      {/* Setting groups */}
      {[
        {
          key: 'critical',
          title: 'ค่าที่มีผลต่อ production',
          description: 'ตรวจให้ถูกก่อนบันทึก เพราะมีผลกับ SML, database, public URL, token และการ restart',
          groups: criticalGrouped,
        },
        {
          key: 'optional',
          title: 'ค่าเสริมและการแจ้งเตือน',
          description: 'ข้อมูลร้าน การแจ้งเตือน และ automation ที่ไม่ใช่เส้นทางส่งเอกสารหลัก',
          groups: optionalGrouped,
        },
      ].map((section) => section.groups.length > 0 && (
        <section key={section.key} className="space-y-2.5">
          <div>
            <h2 className="text-sm font-semibold text-foreground">{section.title}</h2>
            <p className="mt-0.5 text-xs leading-relaxed text-muted-foreground">
              {section.description}
            </p>
          </div>
          {section.groups.map(({ group, items }) => {
        const meta = GROUP_META[group]
        const Icon = meta.icon
        return (
          <Card key={group}>
            <CardHeader className="pb-3">
              <CardTitle className="flex items-start gap-2 text-sm font-semibold">
                <Icon className="mt-0.5 h-4 w-4 text-accent-strong" />
                <span>
                  {meta.title}
                  <span className="mt-0.5 block text-xs font-normal text-muted-foreground">
                    {meta.description}
                  </span>
                </span>
              </CardTitle>
            </CardHeader>
            <CardContent>
              {group === 'sml' && (
                <div className="mb-4 rounded-lg border border-primary/20 bg-primary/[0.04] px-3 py-2 text-xs leading-relaxed text-muted-foreground">
                  <span className="font-medium text-foreground">sml-api-byboss URL</span> คือ proxy ภายใน Docker เช่น <span className="font-mono text-foreground">http://172.24.0.1:8200</span> ไม่ใช่ domain SML ปลายทางของลูกค้า
                  ถ้า domain ของ tenant เปลี่ยน ให้แก้ที่ <span className="font-mono text-foreground">~/sml-api-bybos</span> แล้วใช้ <span className="font-mono text-foreground">docker compose up -d --force-recreate</span>.
                  Stock Request URL เป็น endpoint แยกสำหรับคำนวณต้นทุนสต๊อก และการทดสอบในหน้านี้ไม่สั่งคำนวณต้นทุนจริง
                </div>
              )}
              <div className="grid gap-4 lg:grid-cols-2">
                {items.map((s) => {
                  const draftVal = draft[s.key] ?? ''
                  const isMissing = !!s.missing && draftVal === ''
                  return (
                    <div key={s.key} className="space-y-1.5">
                      <div className="flex items-center justify-between gap-2">
                        <Label htmlFor={s.key} className={isMissing ? 'text-destructive' : undefined}>
                          {s.label}
                          {s.missing && <span className="ml-1 text-destructive">*</span>}
                        </Label>
                        <div className="flex items-center gap-1">
                          <Badge
                            variant={sourceBadgeVariant(s)}
                            className="h-5 px-1.5 text-[10px]"
                          >
                            {sourceLabel(s)}
                          </Badge>
                          {!s.locked && (
                            s.pending_restart ? (
                              <Badge variant="destructive" className="h-5 px-1.5 text-[10px]">
                                รอรีสตาร์ท
                              </Badge>
                            ) : s.restart_required && (
                              <Badge variant="outline" className="h-5 px-1.5 text-[10px]">
                                ใช้งานอยู่
                              </Badge>
                            )
                          )}
                        </div>
                      </div>
                      <Input
                        id={s.key}
                        type={s.type === 'password' ? 'password' : s.type}
                        value={draftVal}
                        placeholder={s.locked ? undefined : s.source === 'database' ? undefined : 'กรอกค่าของร้านนี้'}
                        disabled={s.locked}
                        onChange={(e) => setDraft((d) => ({ ...d, [s.key]: e.target.value }))}
                        className={cn(
                          s.key.includes('url') || s.key.includes('model') || s.key.includes('database') ? 'font-mono text-xs' : undefined,
                          s.locked && 'cursor-not-allowed bg-muted opacity-60',
                          isMissing && 'border-destructive focus-visible:ring-destructive',
                        )}
                      />
                      {isMissing && (
                        <p className="text-[11px] text-destructive">จำเป็นต้องกรอกก่อนบันทึก</p>
                      )}
                      {s.description && !isMissing && (
                        <p className="text-[11px] leading-relaxed text-muted-foreground">
                          {s.description}
                        </p>
                      )}
                      {!s.locked && s.restart_required && s.runtime_value !== undefined && (
                        <div className={cn(
                          'rounded-md border px-2 py-1.5 text-[11px]',
                          s.pending_restart ? 'border-warning/30 bg-warning/[0.06]' : 'border-border bg-muted/25',
                        )}>
                          <div className="flex items-start gap-1.5">
                            {s.pending_restart ? (
                              <AlertCircle className="mt-0.5 h-3.5 w-3.5 shrink-0 text-warning" />
                            ) : (
                              <CheckCircle2 className="mt-0.5 h-3.5 w-3.5 shrink-0 text-success" />
                            )}
                            <div className="min-w-0">
                              <span className="font-medium text-foreground">ค่าที่ backend ใช้อยู่ตอนนี้: </span>
                              <span className="break-all font-mono text-muted-foreground">
                                {s.runtime_value || '—'}
                              </span>
                            </div>
                          </div>
                        </div>
                      )}
                    </div>
                  )
                })}
              </div>
            </CardContent>
          </Card>
        )
          })}
        </section>
      ))}

      {/* Confirm save dialog */}
      <ConfirmDialog
        open={confirmSave}
        onOpenChange={setConfirmSave}
        title="บันทึกและเริ่มใช้ค่าบน production?"
        description={confirmSaveDesc}
        confirmLabel="บันทึกและเริ่มใช้ค่าใหม่"
        onConfirm={doSave}
      />

      {/* Confirm restart dialog */}
      <ConfirmDialog
        open={confirmRestart}
        onOpenChange={setConfirmRestart}
        title="รีสตาร์ท backend เพื่อใช้ค่าล่าสุด?"
        description="ผลกระทบ: backend จะหยุดชั่วคราวประมาณ 10-30 วินาที แล้วกลับมาพร้อมใช้ค่าที่บันทึกไว้ล่าสุด\nRollback: ถ้าหลังรีสตาร์ทเชื่อมต่อไม่ได้ ให้ตรวจค่าที่เพิ่งแก้ในหน้านี้หรือ restore ค่าเดิมจาก .env/เอกสาร deploy"
        confirmLabel="รีสตาร์ทเลย"
        onConfirm={doRestart}
      />
    </div>
  )
}
