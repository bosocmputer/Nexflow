import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { Link } from 'react-router-dom'
import {
  ArrowRight,
  Bell,
  Building2,
  CheckCircle2,
  CircleAlert,
  CircleHelp,
  Database,
  Loader2,
  RefreshCw,
  Save,
  Store,
} from 'lucide-react'
import { toast } from 'sonner'

import client from '@/api/client'
import { PageHeader } from '@/components/common/PageHeader'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { cn } from '@/lib/utils'

type InstanceSetting = {
  key: 'instance.name' | 'instance.support_contact'
  label: string
  value: string
  description?: string
  required?: boolean
}

type InstanceResponse = {
  settings: InstanceSetting[]
}

type SMLReadiness = {
  configured: boolean
  ready: boolean
  status: string
  checked_at?: string
  cached?: boolean
}

type SetupStatus = {
  checked_at?: string
  sml_readiness?: SMLReadiness
}

type ShopeeStatus = {
  checked_at?: string
  mode: 'direct' | 'gateway'
  enabled: boolean
  configured: boolean
  connected: boolean
  can_fetch: boolean
  shop_name?: string
  redirect_mismatch?: boolean
}

type LineStatus = {
  checked_at?: string
  ready: boolean
  sender_count: number
  enabled_sender_count: number
  recipient_count: number
  enabled_recipient_count: number
}

type ServiceState<T> = {
  data: T | null
  loading: boolean
  error: boolean
}

type StatusTone = 'ready' | 'setup' | 'error' | 'unknown'

type IntegrationView = {
  key: string
  title: string
  status: string
  detail: string
  checkedAt?: string
  tone: StatusTone
  href: string
  actionLabel: string
  icon: typeof Database
}

const INITIAL_SERVICE_STATE = { data: null, loading: true, error: false }

const STATUS_STYLE: Record<StatusTone, string> = {
  ready: 'border-success/30 bg-success/10 text-success',
  setup: 'border-warning/35 bg-warning/10 text-foreground',
  error: 'border-destructive/30 bg-destructive/10 text-destructive',
  unknown: 'border-border bg-muted text-muted-foreground',
}

function normalizeProfileValue(value: string) {
  return value.trim().replace(/\s+/g, ' ')
}

function formatCheckedAt(value?: string) {
  if (!value) return 'ยังไม่มีข้อมูล'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return 'ยังไม่มีข้อมูล'
  return new Intl.DateTimeFormat('th-TH', {
    day: '2-digit',
    month: '2-digit',
    year: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  }).format(date)
}

function loadingIntegration(key: string, title: string, href: string, actionLabel: string, icon: typeof Database): IntegrationView {
  return {
    key,
    title,
    status: 'กำลังตรวจสอบ',
    detail: 'กำลังโหลดสถานะล่าสุด',
    tone: 'unknown',
    href,
    actionLabel,
    icon,
  }
}

function IntegrationRow({ item, loading }: { item: IntegrationView; loading: boolean }) {
  const Icon = item.icon
  return (
    <div className="grid gap-3 px-4 py-4 sm:grid-cols-[minmax(0,1fr)_auto] sm:items-center sm:px-5">
      <div className="flex min-w-0 items-start gap-3">
        <div className="flex h-9 w-9 shrink-0 items-center justify-center rounded-md border bg-muted/40 text-muted-foreground">
          <Icon className="h-4 w-4" aria-hidden="true" />
        </div>
        <div className="min-w-0">
          <div className="flex flex-wrap items-center gap-2">
            <h3 className="text-sm font-semibold text-foreground">{item.title}</h3>
            <Badge variant="outline" className={cn('h-6 whitespace-nowrap', STATUS_STYLE[item.tone])}>
              {loading && <Loader2 className="mr-1 h-3 w-3 animate-spin" aria-hidden="true" />}
              {item.status}
            </Badge>
          </div>
          <p className="mt-1 text-sm leading-5 text-muted-foreground">{item.detail}</p>
          <p className="mt-1 text-xs text-muted-foreground">
            ตรวจล่าสุด: {formatCheckedAt(item.checkedAt)}
          </p>
        </div>
      </div>
      <Button asChild variant="outline" size="sm" className="w-full sm:w-auto">
        <Link to={item.href}>
          {item.actionLabel}
          <ArrowRight className="h-4 w-4" aria-hidden="true" />
        </Link>
      </Button>
    </div>
  )
}

export default function InstanceSettings() {
  const [settings, setSettings] = useState<InstanceSetting[]>([])
  const [draft, setDraft] = useState<Record<string, string>>({})
  const [profileLoading, setProfileLoading] = useState(true)
  const [profileError, setProfileError] = useState(false)
  const [saving, setSaving] = useState(false)
  const [refreshing, setRefreshing] = useState(false)
  const [sml, setSML] = useState<ServiceState<SetupStatus>>(INITIAL_SERVICE_STATE)
  const [shopee, setShopee] = useState<ServiceState<ShopeeStatus>>(INITIAL_SERVICE_STATE)
  const [line, setLine] = useState<ServiceState<LineStatus>>(INITIAL_SERVICE_STATE)

  const statusSequence = useRef(0)
  const statusAbort = useRef<AbortController | null>(null)
  const statusPending = useRef(false)

  const loadProfile = useCallback(async () => {
    setProfileLoading(true)
    setProfileError(false)
    try {
      const response = await client.get<InstanceResponse>('/api/settings/instance')
      const nextSettings = response.data.settings ?? []
      setSettings(nextSettings)
      setDraft(Object.fromEntries(nextSettings.map((setting) => [setting.key, setting.value ?? ''])))
    } catch {
      setProfileError(true)
    } finally {
      setProfileLoading(false)
    }
  }, [])

  const loadStatuses = useCallback(async (force = false) => {
    if (statusPending.current && force) return
    statusPending.current = true
    const sequence = statusSequence.current + 1
    statusSequence.current = sequence
    statusAbort.current?.abort()
    const controller = new AbortController()
    statusAbort.current = controller
    setRefreshing(true)
    setSML((current) => ({ ...current, loading: true, error: false }))
    setShopee((current) => ({ ...current, loading: true, error: false }))
    setLine((current) => ({ ...current, loading: true, error: false }))

    const setupPath = force ? '/api/setup/status?summary=1&refresh_sml=1' : '/api/setup/status?summary=1'
    const results = await Promise.allSettled([
      client.get<SetupStatus>(setupPath, { signal: controller.signal }),
      client.get<ShopeeStatus>('/api/settings/shopee-api/status?summary=1', { signal: controller.signal }),
      client.get<LineStatus>('/api/settings/line-notifications/status', { signal: controller.signal }),
    ])

    if (controller.signal.aborted || statusSequence.current !== sequence) return
    const [setupResult, shopeeResult, lineResult] = results
    setSML(setupResult.status === 'fulfilled'
      ? { data: setupResult.value.data, loading: false, error: false }
      : { data: null, loading: false, error: true })
    setShopee(shopeeResult.status === 'fulfilled'
      ? { data: shopeeResult.value.data, loading: false, error: false }
      : { data: null, loading: false, error: true })
    setLine(lineResult.status === 'fulfilled'
      ? { data: lineResult.value.data, loading: false, error: false }
      : { data: null, loading: false, error: true })
    statusPending.current = false
    setRefreshing(false)
  }, [])

  useEffect(() => {
    void loadProfile()
    void loadStatuses()
    return () => statusAbort.current?.abort()
  }, [loadProfile, loadStatuses])

  const hasChanges = useMemo(() => settings.some((setting) => (
    normalizeProfileValue(draft[setting.key] ?? '') !== normalizeProfileValue(setting.value ?? '')
  )), [draft, settings])

  const storeName = draft['instance.name'] ?? ''
  const supportContact = draft['instance.support_contact'] ?? ''
  const canSave = !profileLoading && !saving && hasChanges && normalizeProfileValue(storeName).length > 0

  const saveProfile = async () => {
    if (!canSave) return
    setSaving(true)
    try {
      await client.put('/api/settings/instance', {
        settings: {
          'instance.name': storeName,
          'instance.support_contact': supportContact,
        },
      })
      await loadProfile()
      toast.success('บันทึกข้อมูลร้านแล้ว')
    } catch (error: any) {
      toast.error(error?.response?.data?.error || 'บันทึกข้อมูลร้านไม่สำเร็จ')
    } finally {
      setSaving(false)
    }
  }

  const integrationViews = useMemo<IntegrationView[]>(() => {
    let smlView = loadingIntegration('sml', 'SML ERP และ NextStep Marketplace', '/setup', 'ดูสถานะ', Database)
    if (sml.error) {
      smlView = { ...smlView, status: 'ตรวจสอบไม่ได้', detail: 'ระบบยังแสดงส่วนอื่นได้ตามปกติ ลองตรวจใหม่อีกครั้ง', tone: 'unknown' }
    } else if (sml.data?.sml_readiness) {
      const readiness = sml.data.sml_readiness
      if (!readiness.configured) {
        smlView = { ...smlView, status: 'ต้องตั้งค่า', detail: 'ยังไม่ได้เปิดการเชื่อมต่อ SML สำหรับร้านนี้', tone: 'setup', checkedAt: readiness.checked_at || sml.data.checked_at }
      } else if (readiness.ready) {
        smlView = { ...smlView, status: 'พร้อมใช้งาน', detail: 'เชื่อมต่อข้อมูลสินค้าและออเดอร์จาก SML ได้', tone: 'ready', checkedAt: readiness.checked_at || sml.data.checked_at }
      } else {
        smlView = { ...smlView, status: 'มีปัญหา', detail: 'เชื่อมต่อ SML ไม่สำเร็จ กรุณาตรวจหน้าสถานะหรือติดต่อผู้ดูแลระบบ', tone: 'error', checkedAt: readiness.checked_at || sml.data.checked_at }
      }
    }

    let shopeeView = loadingIntegration('shopee', 'Shopee', '/settings/shopee-connections', 'จัดการร้าน', Store)
    if (shopee.error) {
      shopeeView = { ...shopeeView, status: 'ตรวจสอบไม่ได้', detail: 'ไม่สามารถโหลดสถานะ Shopee ได้ในขณะนี้', tone: 'unknown' }
    } else if (shopee.data) {
      const status = shopee.data
      const gatewayDetail = status.mode === 'gateway' ? ' เชื่อมผ่าน Central Shopee Gateway' : ''
      if (!status.enabled) {
        shopeeView = { ...shopeeView, status: 'ต้องตั้งค่า', detail: 'ฟังก์ชัน Shopee ยังไม่ได้เปิดใช้งานสำหรับร้านนี้', tone: 'setup', checkedAt: status.checked_at }
      } else if (!status.configured) {
        shopeeView = { ...shopeeView, status: 'มีปัญหา', detail: 'การเชื่อมต่อ Shopee ยังตั้งค่าไม่ครบ กรุณาติดต่อผู้ดูแลระบบ', tone: 'error', checkedAt: status.checked_at }
      } else if (status.mode === 'direct' && status.redirect_mismatch) {
        shopeeView = { ...shopeeView, status: 'มีปัญหา', detail: 'Redirect ของ Shopee ไม่ตรงกับร้านนี้ กรุณาติดต่อผู้ดูแลระบบ', tone: 'error', checkedAt: status.checked_at }
      } else if (status.connected && status.can_fetch) {
        shopeeView = { ...shopeeView, status: 'พร้อมใช้งาน', detail: [status.shop_name || 'เชื่อมร้าน Shopee แล้ว', gatewayDetail.trim()].filter(Boolean).join(' · '), tone: 'ready', checkedAt: status.checked_at }
      } else if (!status.connected) {
        shopeeView = { ...shopeeView, status: 'ต้องตั้งค่า', detail: ['ยังไม่ได้เชื่อมร้าน Shopee', gatewayDetail.trim()].filter(Boolean).join(' · '), tone: 'setup', checkedAt: status.checked_at }
      } else {
        shopeeView = { ...shopeeView, status: 'มีปัญหา', detail: 'เชื่อมร้านแล้วแต่ยังดึงข้อมูลไม่ได้ กรุณาตรวจร้าน Shopee', tone: 'error', checkedAt: status.checked_at }
      }
    }

    let lineView = loadingIntegration('line', 'LINE แจ้งเตือน', '/settings/line-notifications', 'จัดการ LINE', Bell)
    if (line.error) {
      lineView = { ...lineView, status: 'ตรวจสอบไม่ได้', detail: 'ไม่สามารถโหลดสถานะ LINE แจ้งเตือนได้ในขณะนี้', tone: 'unknown' }
    } else if (line.data) {
      const status = line.data
      if (status.ready) {
        lineView = {
          ...lineView,
          status: 'พร้อมใช้งาน',
          detail: `เปิดใช้ LINE OA ${status.enabled_sender_count} บัญชี และผู้รับ ${status.enabled_recipient_count} ราย`,
          tone: 'ready',
          checkedAt: status.checked_at,
        }
      } else {
        lineView = {
          ...lineView,
          status: 'ต้องตั้งค่า',
          detail: status.enabled_sender_count === 0 ? 'เพิ่มและเปิดใช้ LINE OA ก่อนตั้งค่าผู้รับแจ้งเตือน' : 'ยังไม่มีผู้รับแจ้งเตือนที่เปิดใช้งาน',
          tone: 'setup',
          checkedAt: status.checked_at,
        }
      }
    }
    return [smlView, shopeeView, lineView]
  }, [line, shopee, sml])

  return (
    <div className="space-y-4">
      <PageHeader
        title="ข้อมูลร้านและการเชื่อมต่อ"
        description="แก้ไขข้อมูลร้านและตรวจสถานะบริการที่ใช้งานอยู่"
        actions={(
          <Button variant="outline" onClick={() => void loadStatuses(true)} disabled={refreshing}>
            <RefreshCw className={cn('h-4 w-4', refreshing && 'animate-spin')} aria-hidden="true" />
            ตรวจสถานะใหม่
          </Button>
        )}
      />

      {profileError && (
        <Alert variant="destructive">
          <CircleAlert className="h-4 w-4" />
          <AlertTitle>โหลดข้อมูลร้านไม่สำเร็จ</AlertTitle>
          <AlertDescription className="flex flex-wrap items-center gap-2">
            <span>สถานะการเชื่อมต่อด้านล่างยังตรวจแยกได้ตามปกติ</span>
            <Button variant="outline" size="sm" onClick={() => void loadProfile()}>ลองใหม่</Button>
          </AlertDescription>
        </Alert>
      )}

      <div className="grid gap-4 xl:grid-cols-[minmax(320px,0.85fr)_minmax(520px,1.35fr)]">
        <Card>
          <CardHeader className="p-5 pb-4">
            <div className="flex items-start gap-3">
              <div className="flex h-9 w-9 shrink-0 items-center justify-center rounded-md border bg-muted/40 text-muted-foreground">
                <Building2 className="h-4 w-4" aria-hidden="true" />
              </div>
              <div>
                <CardTitle className="text-base">ข้อมูลร้าน</CardTitle>
                <CardDescription className="mt-1">ข้อมูลสำหรับระบุร้านและติดต่อผู้ดูแล</CardDescription>
              </div>
            </div>
          </CardHeader>
          <CardContent className="space-y-4 px-5 pb-5">
            <div className="space-y-1.5">
              <Label htmlFor="instance-name">ชื่อร้าน</Label>
              <Input
                id="instance-name"
                value={storeName}
                maxLength={120}
                disabled={profileLoading || saving}
                onChange={(event) => setDraft((current) => ({ ...current, 'instance.name': event.target.value }))}
                placeholder="ชื่อร้าน"
              />
              <p className="text-xs text-muted-foreground">ใช้แสดงเพื่อให้ทีมแยกร้านนี้ได้ชัดเจน</p>
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="support-contact">ผู้ดูแลระบบ / ช่องทางติดต่อ</Label>
              <Input
                id="support-contact"
                value={supportContact}
                maxLength={200}
                disabled={profileLoading || saving}
                onChange={(event) => setDraft((current) => ({ ...current, 'instance.support_contact': event.target.value }))}
                placeholder="เช่น คุณเอ 08x-xxx-xxxx"
              />
              <p className="text-xs text-muted-foreground">เว้นว่างได้เมื่อยังไม่มีผู้ดูแลประจำร้าน</p>
            </div>
            <div className="flex items-center justify-end border-t pt-4">
              <Button onClick={() => void saveProfile()} disabled={!canSave}>
                {saving ? <Loader2 className="h-4 w-4 animate-spin" aria-hidden="true" /> : <Save className="h-4 w-4" aria-hidden="true" />}
                {saving ? 'กำลังบันทึก' : 'บันทึกข้อมูลร้าน'}
              </Button>
            </div>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="p-5 pb-3">
            <div className="flex items-start justify-between gap-3">
              <div>
                <CardTitle className="text-base">สถานะการเชื่อมต่อ</CardTitle>
                <CardDescription className="mt-1">ตรวจแต่ละบริการแยกกันโดยไม่กระทบการใช้งานส่วนอื่น</CardDescription>
              </div>
              <CircleHelp className="h-5 w-5 shrink-0 text-muted-foreground" aria-hidden="true" />
            </div>
          </CardHeader>
          <CardContent className="divide-y p-0">
            {integrationViews.map((item) => (
              <IntegrationRow
                key={item.key}
                item={item}
                loading={item.key === 'sml' ? sml.loading : item.key === 'shopee' ? shopee.loading : line.loading}
              />
            ))}
          </CardContent>
        </Card>
      </div>

      <div className="flex items-start gap-2 rounded-md border bg-muted/25 px-4 py-3 text-sm text-muted-foreground">
        <CheckCircle2 className="mt-0.5 h-4 w-4 shrink-0 text-success" aria-hidden="true" />
        <p>ค่าระบบและข้อมูลลับดูแลผ่านระบบ deployment เพื่อป้องกันการแก้ผิดและไม่แสดงในหน้านี้</p>
      </div>
    </div>
  )
}
