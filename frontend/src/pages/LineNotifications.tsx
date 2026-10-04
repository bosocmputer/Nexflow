import { useEffect, useMemo, useRef, useState } from 'react'
import dayjs from 'dayjs'
import {
  AlertTriangle,
  Bell,
  CheckCircle2,
  Copy,
  Edit3,
  Eye,
  EyeOff,
  History,
  MessageCircle,
  MessageSquareText,
  Plus,
  RefreshCw,
  Send,
  Settings2,
  Trash2,
  UserPlus,
  Users,
} from 'lucide-react'
import { toast } from 'sonner'

import client from '@/api/client'
import { Badge } from '@/components/ui/badge'
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
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import { Switch } from '@/components/ui/switch'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Textarea } from '@/components/ui/textarea'
import { ConfirmDialog } from '@/components/common/ConfirmDialog'
import { DataTable } from '@/components/common/DataTable'
import { EmptyState } from '@/components/common/EmptyState'
import { PageHeader } from '@/components/common/PageHeader'
import {
  eventGroupLabel,
  eventLabelForKey,
  eventSourceForKey,
  FlexMessagePreview,
  RecipientEventPreferences,
  recipientSourceSummary,
  sourceBadgeClass,
  sourceLabel,
  type LineEventDefinition,
  type LineEventSample,
  type LineNotificationSource,
} from '@/components/line-notifications/LineNotificationPreferences'
import {
  lineQuotaErrorLabel,
  presentLineQuota,
  type LineOAQuota,
} from '@/lib/line-quota'
import { cn } from '@/lib/utils'

interface LineSender {
  id: string
  name: string
  bot_user_id: string
  enabled: boolean
  updated_at: string
}

interface LineRecipient {
  id: string
  line_oa_id: string
  line_oa_name?: string
  name: string
  destination_type: 'user' | 'group' | 'room'
  destination_id: string
  enabled: boolean
  event_keys: string[]
  last_test_at?: string
  last_test_status: string
  last_test_error: string
  last_sent_at?: string
  last_error: string
  updated_at: string
}

interface LineDelivery {
  id: string
  recipient: string
  line_oa_name?: string
  event_key: string
  title: string
  entity_id: string
  status: 'queued' | 'sending' | 'sent' | 'failed' | 'suppressed'
  attempts: number
  last_error: string
  sent_at?: string
  created_at: string
}

interface LineCandidate {
  id: string
  line_oa_id: string
  line_oa_name?: string
  destination_type: 'user' | 'group' | 'room'
  destination_id: string
  display_name: string
  last_message_preview: string
  last_webhook_event_id: string
  is_recipient: boolean
  recipient_id?: string
  last_seen_at: string
}

interface Overview {
  senders: LineSender[]
  recipients: LineRecipient[]
  candidates: LineCandidate[]
  deliveries: LineDelivery[]
  sample_text: string
  sample_texts?: Partial<Record<LineSampleSource, string>>
  event_catalog: LineEventDefinition[]
  readiness: {
    sender_count: number
    enabled_sender_count: number
    recipient_count: number
    enabled_recipient_count: number
    shopee_realtime_enabled?: boolean
    tiktok_shop_notifications_enabled?: boolean
    tiktok_shop_notifications_eligible_after?: string
  }
}

type LineSampleSource = LineNotificationSource

const destinationLabels: Record<LineRecipient['destination_type'], string> = {
  user: 'User ID',
  group: 'Group ID',
  room: 'Room ID',
}

const statusTone: Record<string, string> = {
  sent: 'bg-success/15 text-success',
  sending: 'bg-info/15 text-info',
  queued: 'bg-warning/15 text-warning',
  failed: 'bg-destructive/15 text-destructive',
  suppressed: 'bg-muted text-muted-foreground',
}

export default function LineNotifications() {
  const [data, setData] = useState<Overview | null>(null)
  const [loading, setLoading] = useState(true)
  const [quotaByOA, setQuotaByOA] = useState<Record<string, LineOAQuota>>({})
  const [quotaLoading, setQuotaLoading] = useState(false)
  const [quotaRefreshing, setQuotaRefreshing] = useState<Set<string>>(new Set())
  const [quotaLoadError, setQuotaLoadError] = useState(false)
  const [quotaCooldowns, setQuotaCooldowns] = useState<Record<string, number>>({})
  const [activeSection, setActiveSection] = useState<'recipients' | 'connection'>('recipients')
  const [candidateSheetOpen, setCandidateSheetOpen] = useState(false)
  const [senderDialog, setSenderDialog] = useState<LineSender | 'new' | null>(null)
  const [recipientDialog, setRecipientDialog] = useState<LineRecipient | null>(null)
  const [deleteRecipient, setDeleteRecipient] = useState<LineRecipient | null>(null)
  const [testRecipient, setTestRecipient] = useState<LineRecipient | null>(null)
  const [candidateToAdd, setCandidateToAdd] = useState<LineCandidate | null>(null)
  const [candidateToHide, setCandidateToHide] = useState<LineCandidate | null>(null)
  const [sampleSource, setSampleSource] = useState<LineSampleSource>('shopee')
  const [sampleEventKey, setSampleEventKey] = useState('shopee.order.new')
  const [eventSamples, setEventSamples] = useState<Record<string, LineEventSample>>({})
  const [eventSamplesLoading, setEventSamplesLoading] = useState(false)
  const [supportDialog, setSupportDialog] = useState<'sample' | 'history' | null>(null)
  const supportTriggerRef = useRef<HTMLButtonElement | null>(null)
  const quotaRequestedRef = useRef(false)

  const load = async () => {
    setLoading(true)
    try {
      const res = await client.get<Overview>('/api/settings/line-notifications')
      setData(res.data)
    } catch (e: any) {
      toast.error(e?.response?.data?.error ?? 'โหลด LINE แจ้งเตือนไม่สำเร็จ')
    } finally {
      setLoading(false)
    }
  }

  const loadQuota = async ({ refresh = false, oaID = '' }: { refresh?: boolean; oaID?: string } = {}) => {
    if (oaID) {
      setQuotaRefreshing((current) => new Set(current).add(oaID))
    } else if (refresh) {
      setQuotaRefreshing(new Set(data?.senders.map((sender) => sender.id) ?? []))
    } else if (!refresh) {
      setQuotaLoading(true)
    }
    try {
      const response = await client.get<{ data: LineOAQuota[] }>(
        '/api/settings/line-notifications/quota',
        { params: { ...(refresh ? { refresh: 'true' } : {}), ...(oaID ? { oa_id: oaID } : {}) } },
      )
      setQuotaByOA((current) => {
        const next = oaID ? { ...current } : {}
        response.data.data.forEach((quota) => {
          next[quota.line_oa_id] = quota
        })
        return next
      })
      setQuotaLoadError(false)
      if (refresh) {
        const now = Date.now()
        const cooldowns: Record<string, number> = {}
        response.data.data.forEach((quota) => {
          cooldowns[quota.line_oa_id] = now + Math.max(10, quota.retry_after_seconds ?? 0) * 1000
        })
        setQuotaCooldowns((current) => ({ ...current, ...cooldowns }))
        window.setTimeout(() => {
          setQuotaCooldowns((current) => {
            const next = { ...current }
            Object.entries(next).forEach(([id, until]) => {
              if (until <= Date.now()) delete next[id]
            })
            return next
          })
        }, 10_100)
      }
    } catch (error: any) {
      setQuotaLoadError(true)
      if (oaID) {
        setQuotaByOA((current) => ({
          ...current,
          [oaID]: {
            line_oa_id: oaID,
            name: data?.senders.find((sender) => sender.id === oaID)?.name || 'LINE OA',
            quota_type: undefined,
            limit: null,
            used: null,
            remaining: null,
            status: 'error',
            checked_at: null,
            is_stale: false,
            error_code: error?.response?.data?.error_code || 'line_unavailable',
          },
        }))
      }
    } finally {
      if (oaID) {
        setQuotaRefreshing((current) => {
          const next = new Set(current)
          next.delete(oaID)
          return next
        })
      } else {
        setQuotaLoading(false)
        if (refresh) setQuotaRefreshing(new Set())
      }
    }
  }

  useEffect(() => {
    void load()
  }, [])

  const refreshPage = () => {
    void load()
    if (activeSection === 'connection') {
      quotaRequestedRef.current = true
      void loadQuota({ refresh: true })
    }
  }

  const handleSectionChange = (value: string) => {
    const next = value === 'connection' ? 'connection' : 'recipients'
    setActiveSection(next)
    if (next === 'connection' && !quotaRequestedRef.current) {
      quotaRequestedRef.current = true
      void loadQuota()
    }
  }

  const readiness = data?.readiness
  const ready = !!readiness?.enabled_sender_count && !!readiness.enabled_recipient_count
  const enabledRecipients = data?.recipients.filter((r) => r.enabled).length ?? 0
  const sampleText = data?.sample_texts?.[sampleSource] || data?.sample_text || 'กำลังโหลดตัวอย่างข้อความ'
  const eventCatalog = data?.event_catalog ?? []
  const selectedEvent = eventCatalog.find((event) => event.key === sampleEventKey) ?? eventCatalog[0]
  const selectedSample = selectedEvent ? eventSamples[selectedEvent.key] : undefined
  const quotaCooldownActive = Object.values(quotaCooldowns).some((until) => until > Date.now())

  const openSampleDialog = async (trigger: HTMLButtonElement) => {
    supportTriggerRef.current = trigger
    setSupportDialog('sample')
    if (Object.keys(eventSamples).length > 0 || eventSamplesLoading) return
    setEventSamplesLoading(true)
    try {
      const response = await client.get<{ data: Record<string, LineEventSample> }>('/api/settings/line-notifications/samples')
      setEventSamples(response.data.data ?? {})
    } catch (e: any) {
      toast.error(e?.response?.data?.error ?? 'โหลดตัวอย่าง Flex Message ไม่สำเร็จ')
    } finally {
      setEventSamplesLoading(false)
    }
  }

  const senderNameById = useMemo(() => {
    const map = new Map<string, string>()
    data?.senders.forEach((s) => map.set(s.id, s.name))
    return map
  }, [data?.senders])

  const handleTestSender = async (sender: LineSender) => {
    const id = toast.loading('กำลังทดสอบ LINE OA')
    try {
      const res = await client.post<{ display_name: string; basic_id: string }>(
        `/api/settings/line-notifications/senders/${sender.id}/test`,
      )
      toast.success(`LINE OA ใช้งานได้: ${res.data.display_name || res.data.basic_id || sender.name}`, { id })
      await load()
    } catch (e: any) {
      toast.error(e?.response?.data?.error ?? 'ทดสอบ LINE OA ไม่สำเร็จ', { id })
    }
  }

  const copyWebhookURL = async (sender: LineSender) => {
    try {
      await navigator.clipboard.writeText(webhookURL(sender.id))
      toast.success('คัดลอก Webhook URL แล้ว')
    } catch {
      toast.error('คัดลอกไม่สำเร็จ กรุณาเลือกและคัดลอกเอง')
    }
  }

  const runRecipientTest = async () => {
    if (!testRecipient) return
    const id = toast.loading('กำลังส่ง LINE ทดสอบ')
    try {
      await client.post(`/api/settings/line-notifications/recipients/${testRecipient.id}/test`, {
        sample_source: sampleSource,
        sample_event_key: selectedEvent?.key || '',
      })
      toast.success('ส่งข้อความทดสอบแล้ว', { id })
      setTestRecipient(null)
      await load()
    } catch (e: any) {
      toast.error(e?.response?.data?.error ?? 'ส่งข้อความทดสอบไม่สำเร็จ', { id })
      await load()
    }
  }

  const runDeleteRecipient = async () => {
    if (!deleteRecipient) return
    try {
      await client.delete(`/api/settings/line-notifications/recipients/${deleteRecipient.id}`)
      toast.success('ลบผู้รับแจ้งเตือนแล้ว')
      setDeleteRecipient(null)
      await load()
    } catch (e: any) {
      toast.error(e?.response?.data?.error ?? 'ลบผู้รับแจ้งเตือนไม่สำเร็จ')
    }
  }

  const runHideCandidate = async () => {
    if (!candidateToHide) return
    try {
      await client.delete(`/api/settings/line-notifications/candidates/${candidateToHide.id}`)
      toast.success('ซ่อนรายการนี้แล้ว')
      setCandidateToHide(null)
      await load()
    } catch (e: any) {
      toast.error(e?.response?.data?.error ?? 'ซ่อนรายการไม่สำเร็จ')
    }
  }

  const openRecipientTest = (recipient: LineRecipient) => {
    const eventKey = recipient.event_keys?.[0] || 'shopee.order.new'
    setSampleEventKey(eventKey)
    setSampleSource(eventSourceForKey(eventKey, eventCatalog))
    setTestRecipient(recipient)
  }

  return (
    <div className="min-w-0 space-y-5">
      <PageHeader
        title="LINE แจ้งเตือน"
        description="กำหนดให้ผู้รับแต่ละคนเลือกเฉพาะออเดอร์ การยกเลิก หรือผลส่ง SML ที่เกี่ยวข้อง พร้อมดู Flex ตัวอย่างก่อนส่งทดสอบจริง"
        actions={
          <>
            <Button variant="outline" size="sm" className="gap-1.5" aria-label="ดูตัวอย่าง Flex Message" aria-haspopup="dialog" onClick={(event) => { void openSampleDialog(event.currentTarget) }}>
              <MessageSquareText className="h-4 w-4" aria-hidden />
              <span className="hidden sm:inline">ตัวอย่าง Flex</span>
            </Button>
            <Button variant="outline" size="sm" className="gap-1.5" aria-label="ดูประวัติการส่งล่าสุด" aria-haspopup="dialog" onClick={(event) => { supportTriggerRef.current = event.currentTarget; setSupportDialog('history') }}>
              <History className="h-4 w-4" aria-hidden />
              <span className="hidden sm:inline">ประวัติการส่ง</span>
            </Button>
            <Button
              variant="outline"
              size="sm"
              className="gap-1.5"
              onClick={refreshPage}
              disabled={loading || (activeSection === 'connection' && (quotaLoading || quotaRefreshing.size > 0 || quotaCooldownActive))}
              title={activeSection === 'connection'
                ? (quotaCooldownActive ? 'กรุณารอ 10 วินาทีก่อนดึงโควตาจาก LINE อีกครั้ง' : 'รีเฟรชการเชื่อมต่อและดึงโควตาล่าสุดจาก LINE')
                : 'รีเฟรชผู้รับและประวัติการส่ง'}
            >
              <RefreshCw className="h-3.5 w-3.5" />
              รีเฟรช
            </Button>
          </>
        }
      />

      <section className="flex flex-col gap-2 border-y border-border py-2.5 text-sm sm:flex-row sm:items-center sm:justify-between" role="status" aria-live="polite">
        <div className="flex min-w-0 items-center gap-2 font-medium">
          {ready ? <CheckCircle2 className="h-4 w-4 shrink-0 text-success" /> : <AlertTriangle className="h-4 w-4 shrink-0 text-warning" />}
          <span>{ready ? 'พร้อมส่ง LINE ตามประเภทที่ผู้รับเลือก' : 'ยังตั้งค่าการแจ้งเตือนไม่ครบ'}</span>
        </div>
        <div className="flex flex-wrap items-center gap-x-4 gap-y-1 text-xs text-muted-foreground">
          <span>LINE OA <strong className="font-semibold text-foreground">{readiness?.enabled_sender_count ?? 0}/{readiness?.sender_count ?? 0}</strong></span>
          <span>ผู้รับเปิด <strong className="font-semibold text-foreground">{enabledRecipients}</strong></span>
          <span>TikTok <strong className="font-semibold text-foreground">{readiness?.tiktok_shop_notifications_enabled ? 'เปิด' : 'ปิด'}</strong></span>
          <span>ล่าสุด <strong className="font-semibold text-foreground">{data?.deliveries[0]?.status ? deliveryStatusLabel(data.deliveries[0].status) : 'ยังไม่มี'}</strong></span>
        </div>
      </section>

      <Tabs value={activeSection} onValueChange={handleSectionChange} className="min-w-0">
        <TabsList className="h-9 w-full justify-start overflow-x-auto rounded-none border-b bg-transparent p-0">
          <TabsTrigger value="recipients" className="h-9 gap-2 rounded-none border-b-2 border-transparent px-3 shadow-none data-[state=active]:border-primary data-[state=active]:bg-transparent data-[state=active]:shadow-none">
            <Users className="h-4 w-4" />
            ผู้รับแจ้งเตือน
          </TabsTrigger>
          <TabsTrigger value="connection" className="h-9 gap-2 rounded-none border-b-2 border-transparent px-3 shadow-none data-[state=active]:border-primary data-[state=active]:bg-transparent data-[state=active]:shadow-none">
            <Settings2 className="h-4 w-4" />
            การเชื่อมต่อ LINE OA
          </TabsTrigger>
        </TabsList>

        <TabsContent value="recipients" className="mt-4 min-w-0">
          <section className="min-w-0 space-y-3">
            <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
              <div>
                <h2 className="text-base font-semibold">ผู้รับแจ้งเตือน ({data?.recipients.length ?? 0})</h2>
                <p className="mt-0.5 max-w-3xl text-sm text-muted-foreground">เลือกให้แต่ละคนรับเฉพาะงานที่เกี่ยวข้อง การปิดประเภทจะระงับคิวที่ยังไม่เริ่มส่ง</p>
              </div>
              <Button className="gap-1.5 self-start" onClick={() => setCandidateSheetOpen(true)} disabled={(data?.senders.length ?? 0) === 0} title={(data?.senders.length ?? 0) === 0 ? 'เพิ่มและเชื่อมต่อ LINE OA ก่อน' : 'เลือกจากผู้ที่ทัก LINE OA ล่าสุด'}>
                <UserPlus className="h-4 w-4" />
                เพิ่มผู้รับ
              </Button>
            </div>

            <div className="hidden md:block">
              <DataTable<LineRecipient>
                data={data?.recipients ?? []}
                loading={loading}
                dense
                empty={<EmptyState icon={Users} title="ยังไม่มีผู้รับแจ้งเตือน" description="ให้ผู้รับทัก LINE OA แล้วกดเพิ่มผู้รับ" />}
                columns={[
                  {
                    key: 'name', header: 'ผู้รับ', cell: (r) => <div><div className="font-medium">{r.name}</div><div className="text-xs text-muted-foreground">{r.line_oa_name || senderNameById.get(r.line_oa_id) || 'LINE OA'} · {destinationLabels[r.destination_type]}</div></div>,
                  },
                  {
                    key: 'subscriptions', header: 'รับแจ้งเตือน', cell: (r) => <RecipientSourceSummary recipient={r} catalog={eventCatalog} />,
                  },
                  {
                    key: 'status', header: 'สถานะ / ส่งล่าสุด', cell: (r) => <div><div className="flex flex-wrap gap-1">{r.enabled ? <Badge className="bg-success/15 text-success">เปิด</Badge> : <Badge variant="secondary">ปิด</Badge>}{r.last_error && <Badge className="bg-destructive/15 text-destructive">มีข้อผิดพลาด</Badge>}</div><div className="mt-1 text-xs text-muted-foreground">{r.last_sent_at ? formatDate(r.last_sent_at) : 'ยังไม่มีประวัติส่ง'}</div></div>,
                  },
                  {
                    key: 'actions', header: '', headerClassName: 'text-right', className: 'text-right', cell: (r) => <RecipientActions recipient={r} onTest={openRecipientTest} onEdit={setRecipientDialog} onDelete={setDeleteRecipient} />,
                  },
                ]}
              />
            </div>

            <div className="space-y-2 md:hidden">
              {loading ? <div className="rounded-lg border p-4 text-sm text-muted-foreground">กำลังโหลดผู้รับแจ้งเตือน</div> : (data?.recipients ?? []).length === 0 ? <EmptyState icon={Users} title="ยังไม่มีผู้รับแจ้งเตือน" description="ให้ผู้รับทัก LINE OA แล้วกดเพิ่มผู้รับ" /> : data!.recipients.map((recipient) => (
                <article key={recipient.id} className="space-y-3 rounded-lg border border-border bg-card p-3">
                  <div className="flex items-start justify-between gap-3"><div className="min-w-0"><div className="truncate font-medium">{recipient.name}</div><div className="mt-0.5 truncate text-xs text-muted-foreground">{recipient.line_oa_name || senderNameById.get(recipient.line_oa_id) || 'LINE OA'} · {destinationLabels[recipient.destination_type]}</div></div>{recipient.enabled ? <Badge className="bg-success/15 text-success">เปิด</Badge> : <Badge variant="secondary">ปิด</Badge>}</div>
                  <RecipientSourceSummary recipient={recipient} catalog={eventCatalog} />
                  <div className="flex items-center justify-between gap-2 border-t pt-2"><span className="text-xs text-muted-foreground">ล่าสุด {recipient.last_sent_at ? formatDate(recipient.last_sent_at) : 'ยังไม่มี'}</span><RecipientActions recipient={recipient} onTest={openRecipientTest} onEdit={setRecipientDialog} onDelete={setDeleteRecipient} /></div>
                </article>
              ))}
            </div>
          </section>
        </TabsContent>

        <TabsContent value="connection" className="mt-4 min-w-0">
          <section className="min-w-0 space-y-3">
            <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
              <div>
                <h2 className="text-base font-semibold">การเชื่อมต่อ LINE OA</h2>
                <p className="mt-0.5 max-w-3xl text-sm text-muted-foreground">จัดการ token, Webhook และโควตาข้อความ ส่วนนี้ใช้เมื่อตั้งค่าหรือแก้ปัญหาการเชื่อมต่อ</p>
              </div>
              <Button variant="outline" className="gap-1.5 self-start" onClick={() => setSenderDialog('new')}><Plus className="h-4 w-4" />เพิ่ม LINE OA</Button>
            </div>

            {!ready && (
              <div className="grid gap-2 rounded-lg border border-warning/30 bg-warning/5 p-3 text-sm sm:grid-cols-2 xl:grid-cols-4">
                {['เพิ่ม LINE OA', 'คัดลอก Webhook URL', 'เปิด Use webhook ใน LINE Developers', 'ให้ผู้รับทัก LINE OA'].map((step, index) => <div key={step} className="flex items-center gap-2"><span className="flex h-6 w-6 shrink-0 items-center justify-center rounded-full bg-warning/15 text-xs font-semibold text-warning">{index + 1}</span><span>{step}</span></div>)}
              </div>
            )}

            <div className="hidden md:block">
              <DataTable<LineSender>
                data={data?.senders ?? []}
                loading={loading}
                dense
                empty={<EmptyState icon={Bell} title="ยังไม่มี LINE OA" description="เพิ่ม Channel secret และ access token ก่อนกำหนดผู้รับแจ้งเตือน" />}
                columns={[
                  { key: 'name', header: 'LINE OA', cell: (s) => <div className="min-w-[170px]"><div className="font-medium">{s.name}</div><div className="mt-1 flex flex-wrap gap-1">{s.enabled ? <Badge className="bg-success/15 text-success">เปิด</Badge> : <Badge variant="secondary">ปิด</Badge>}{s.bot_user_id ? <Badge className="bg-info/15 text-info">token ใช้ได้</Badge> : <Badge className="bg-warning/15 text-warning">รอทดสอบ</Badge>}</div></div> },
                  { key: 'quota', header: 'โควตาข้อความ', cell: (s) => <div className="min-w-[230px] max-w-[320px]"><LineQuotaSummary quota={quotaByOA[s.id]} loading={quotaLoading && !quotaByOA[s.id]} refreshing={quotaRefreshing.has(s.id)} cooldown={!!quotaCooldowns[s.id] && quotaCooldowns[s.id] > Date.now()} initialLoadFailed={quotaLoadError} onRefresh={() => void loadQuota({ refresh: true, oaID: s.id })} /></div> },
                  { key: 'details', header: 'Webhook และข้อมูลเทคนิค', cell: (s) => <details className="min-w-[260px]"><summary className="cursor-pointer text-xs font-medium text-primary">แสดงรายละเอียด</summary><div className="mt-2 space-y-2"><div className="font-mono text-[11px] text-muted-foreground">{s.bot_user_id ? `bot ${shortId(s.bot_user_id)}` : 'ยังไม่มี Bot ID'}</div><div className="flex max-w-[360px] items-center gap-1"><code className="min-w-0 flex-1 truncate rounded-md bg-muted/50 px-2 py-1 font-mono text-[11px]">{webhookURL(s.id)}</code><Button variant="ghost" size="sm" className="h-7 px-2" onClick={() => copyWebhookURL(s)} aria-label={`คัดลอก Webhook URL ของ ${s.name}`}><Copy className="h-3.5 w-3.5" /></Button></div></div></details> },
                  { key: 'updated', header: 'แก้ไขล่าสุด', cell: (s) => <span className="whitespace-nowrap text-xs text-muted-foreground">{formatDate(s.updated_at)}</span> },
                  { key: 'actions', header: '', headerClassName: 'text-right', className: 'text-right', cell: (s) => <div className="flex justify-end gap-1"><Button variant="outline" size="sm" className="h-8 px-2 text-xs" onClick={() => handleTestSender(s)}>ทดสอบ OA</Button><Button variant="ghost" size="sm" className="h-8 px-2" onClick={() => setSenderDialog(s)} aria-label={`แก้ไข ${s.name}`}><Edit3 className="h-3.5 w-3.5" /></Button></div> },
                ]}
              />
            </div>

            <div className="space-y-2 md:hidden">
              {loading ? <div className="rounded-lg border p-4 text-sm text-muted-foreground">กำลังโหลดการเชื่อมต่อ LINE OA</div> : (data?.senders ?? []).length === 0 ? <EmptyState icon={Bell} title="ยังไม่มี LINE OA" description="เพิ่ม Channel secret และ access token ก่อนกำหนดผู้รับแจ้งเตือน" /> : data!.senders.map((sender) => (
                <article key={sender.id} className="space-y-3 rounded-lg border border-border bg-card p-3">
                  <div className="flex items-start justify-between gap-3">
                    <div className="min-w-0"><div className="truncate font-medium">{sender.name}</div><div className="mt-1 flex flex-wrap gap-1">{sender.enabled ? <Badge className="bg-success/15 text-success">เปิด</Badge> : <Badge variant="secondary">ปิด</Badge>}{sender.bot_user_id ? <Badge className="bg-info/15 text-info">token ใช้ได้</Badge> : <Badge className="bg-warning/15 text-warning">รอทดสอบ</Badge>}</div></div>
                    <Button variant="ghost" size="sm" className="h-8 shrink-0 px-2" onClick={() => setSenderDialog(sender)} aria-label={`แก้ไข ${sender.name}`}><Edit3 className="h-3.5 w-3.5" /></Button>
                  </div>
                  <LineQuotaSummary quota={quotaByOA[sender.id]} loading={quotaLoading && !quotaByOA[sender.id]} refreshing={quotaRefreshing.has(sender.id)} cooldown={!!quotaCooldowns[sender.id] && quotaCooldowns[sender.id] > Date.now()} initialLoadFailed={quotaLoadError} onRefresh={() => void loadQuota({ refresh: true, oaID: sender.id })} />
                  <details className="rounded-md border border-border/70 px-3 py-2"><summary className="cursor-pointer text-xs font-medium text-primary">Webhook และข้อมูลเทคนิค</summary><div className="mt-2 space-y-2"><div className="font-mono text-[11px] text-muted-foreground">{sender.bot_user_id ? `bot ${shortId(sender.bot_user_id)}` : 'ยังไม่มี Bot ID'}</div><div className="flex min-w-0 items-center gap-1"><code className="min-w-0 flex-1 truncate rounded-md bg-muted/50 px-2 py-1 font-mono text-[11px]">{webhookURL(sender.id)}</code><Button variant="ghost" size="sm" className="h-7 shrink-0 px-2" onClick={() => copyWebhookURL(sender)} aria-label={`คัดลอก Webhook URL ของ ${sender.name}`}><Copy className="h-3.5 w-3.5" /></Button></div></div></details>
                  <div className="flex items-center justify-between gap-2 border-t pt-2"><span className="text-xs text-muted-foreground">แก้ไข {formatDate(sender.updated_at)}</span><Button variant="outline" size="sm" className="h-8 px-2 text-xs" onClick={() => handleTestSender(sender)}>ทดสอบ OA</Button></div>
                </article>
              ))}
            </div>
          </section>
        </TabsContent>
      </Tabs>

      <Dialog open={supportDialog !== null} onOpenChange={(open) => !open && setSupportDialog(null)}>
        <DialogContent className="max-h-[85dvh] overflow-y-auto sm:max-w-4xl" onCloseAutoFocus={(event) => { event.preventDefault(); supportTriggerRef.current?.focus() }}>
          <DialogHeader>
            <DialogTitle>{supportDialog === 'sample' ? 'ตัวอย่าง Flex Message' : 'ประวัติการส่งล่าสุด'}</DialogTitle>
            <DialogDescription>{supportDialog === 'sample' ? 'เลือกประเภทเพื่อดูหน้าตา Flex และข้อความ fallback ก่อนส่งทดสอบจริง' : 'ผลการส่งล่าสุดที่โหลดไว้ กดรีเฟรชในหน้าหลักเพื่ออัปเดตข้อมูล'}</DialogDescription>
          </DialogHeader>
          {supportDialog === 'sample' ? <div className="min-w-0">
            <p className="mt-1 text-sm text-muted-foreground">ตัวอย่างจาก Flex payload จริงที่ระบบใช้ส่ง โดยใช้ข้อมูลจำลองและไม่มีข้อมูลผู้ซื้อ การเปิดหน้าต่างนี้ไม่ส่งข้อความ LINE</p>
            <div className="mt-3 grid min-w-0 gap-3 md:grid-cols-[220px_minmax(0,1fr)]">
              <div className="max-h-[56dvh] space-y-1 overflow-y-auto pr-1">
                {eventCatalog.map((event) => (
                  <button
                    key={event.key}
                    type="button"
                    onClick={() => {
                      setSampleEventKey(event.key)
                      setSampleSource(event.source)
                    }}
                    className={cn(
                      'w-full rounded-md border px-3 py-2 text-left transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring',
                      selectedEvent?.key === event.key ? 'border-primary bg-primary/5' : 'border-transparent hover:bg-muted/60',
                    )}
                  >
                    <span className="block text-sm font-medium">{event.label}</span>
                    <span className="mt-0.5 block text-[11px] text-muted-foreground">{eventGroupLabel(event.group)}</span>
                  </button>
                ))}
              </div>
              <div className="min-w-0 space-y-3">
                {eventSamplesLoading ? (
                  <div className="flex min-h-[260px] items-center justify-center gap-2 rounded-lg border border-dashed text-sm text-muted-foreground"><RefreshCw className="h-4 w-4 animate-spin motion-reduce:animate-none" />กำลังสร้างตัวอย่าง Flex</div>
                ) : selectedEvent && selectedSample ? (
                  <FlexMessagePreview event={selectedEvent} sample={selectedSample} />
                ) : (
                  <div className="rounded-lg border border-dashed p-6 text-center text-sm text-muted-foreground">ยังไม่มีตัวอย่างสำหรับประเภทนี้</div>
                )}
                <details className="rounded-md border border-border/70 bg-muted/20 px-3 py-2">
                  <summary className="cursor-pointer text-xs font-medium">ดูข้อความ fallback</summary>
                  <pre className="mt-2 whitespace-pre-wrap break-words text-xs leading-5 text-muted-foreground">
                    {selectedSample?.message_text || sampleText}
                  </pre>
                </details>
              </div>
            </div>
          </div> : <div className="min-w-0 break-words">
            <div className="mt-3 space-y-2">
              {(data?.deliveries ?? []).length === 0 ? (
                <p className="text-sm text-muted-foreground">ยังไม่มีการส่ง LINE แจ้งเตือนออเดอร์</p>
              ) : (
                data!.deliveries.slice(0, 8).map((d) => (
                  <div key={d.id} className="rounded-md border border-border/70 bg-background/60 p-3">
                    <div className="flex items-center justify-between gap-2">
                      <div className="min-w-0 text-sm font-medium">{d.recipient || 'ผู้รับ'}</div>
                      <Badge className={statusTone[d.status] ?? 'bg-muted text-muted-foreground'}>{deliveryStatusLabel(d.status)}</Badge>
                    </div>
                    <div className="mt-1 flex flex-wrap items-center gap-1.5 text-xs text-muted-foreground">
                      <Badge variant="outline" className={sourceBadgeClass(eventSourceForKey(d.event_key, eventCatalog))}>
                        {sourceLabel(eventSourceForKey(d.event_key, eventCatalog))}
                      </Badge>
                      <span>{eventLabelForKey(d.event_key, eventCatalog) || d.title}</span>
                    </div>
                    {d.entity_id && <div className="mt-1 text-[11px] text-muted-foreground">อ้างอิง {d.entity_id}</div>}
                    {d.last_error && <div className="mt-1 text-xs text-destructive">{d.last_error}</div>}
                    <div className="mt-2 text-[11px] text-muted-foreground">{formatDate(d.sent_at || d.created_at)}</div>
                  </div>
                ))
              )}
            </div>
          </div>}
        </DialogContent>
      </Dialog>

      <SenderDialog
        open={!!senderDialog}
        sender={senderDialog === 'new' ? null : senderDialog}
        onOpenChange={(open) => !open && setSenderDialog(null)}
        onSaved={() => {
          void load()
          quotaRequestedRef.current = true
          void loadQuota({ refresh: true })
        }}
      />
      <CandidatePickerSheet
        open={candidateSheetOpen}
        candidates={data?.candidates ?? []}
        loading={loading}
        senderNameById={senderNameById}
        onOpenChange={setCandidateSheetOpen}
        onRefresh={load}
        onSelect={(candidate) => {
          setCandidateSheetOpen(false)
          setCandidateToAdd(candidate)
        }}
        onHide={setCandidateToHide}
      />
      <RecipientSheet
        open={!!recipientDialog}
        recipient={recipientDialog}
        candidate={null}
        senders={data?.senders ?? []}
        eventCatalog={eventCatalog}
        onOpenChange={(open) => !open && setRecipientDialog(null)}
        onSaved={load}
      />
      <RecipientSheet
        open={!!candidateToAdd}
        recipient={null}
        candidate={candidateToAdd}
        senders={data?.senders ?? []}
        eventCatalog={eventCatalog}
        onOpenChange={(open) => !open && setCandidateToAdd(null)}
        onSaved={load}
      />
      <RecipientTestDialog
        recipient={testRecipient}
        catalog={eventCatalog}
        eventKey={sampleEventKey}
        onEventKeyChange={(eventKey) => {
          setSampleEventKey(eventKey)
          setSampleSource(eventSourceForKey(eventKey, eventCatalog))
        }}
        onOpenChange={(open) => !open && setTestRecipient(null)}
        onConfirm={runRecipientTest}
      />
      <ConfirmDialog
        open={!!deleteRecipient}
        onOpenChange={(open) => !open && setDeleteRecipient(null)}
        title="ลบผู้รับแจ้งเตือน"
        description={deleteRecipient ? `ลบ ${deleteRecipient.name} ออกจาก LINE แจ้งเตือน ออเดอร์ใหม่หลังจากนี้จะไม่ส่งไปยังปลายทางนี้` : ''}
        confirmLabel="ลบผู้รับ"
        variant="destructive"
        onConfirm={runDeleteRecipient}
      />
      <ConfirmDialog
        open={!!candidateToHide}
        onOpenChange={(open) => !open && setCandidateToHide(null)}
        title="ซ่อนรายการที่ทัก LINE OA"
        description={candidateToHide ? `ซ่อน ${candidateName(candidateToHide)} ออกจากรายการล่าสุด ถ้าปลายทางนี้ทัก OA อีกครั้ง ระบบจะแสดงกลับมาใหม่` : ''}
        confirmLabel="ซ่อนรายการ"
        variant="destructive"
        onConfirm={runHideCandidate}
      />
    </div>
  )
}

function LineQuotaSummary({
  quota,
  loading,
  refreshing,
  cooldown,
  initialLoadFailed,
  onRefresh,
}: {
  quota?: LineOAQuota
  loading: boolean
  refreshing: boolean
  cooldown: boolean
  initialLoadFailed: boolean
  onRefresh: () => void
}) {
  if (loading) {
    return (
      <div className="mt-2 flex items-center gap-1.5 text-[11px] text-muted-foreground" aria-live="polite">
        <RefreshCw className="h-3 w-3 animate-spin motion-reduce:animate-none" />
        กำลังดึงโควตาจาก LINE
      </div>
    )
  }
  if (!quota) {
    return (
      <div className="mt-2 flex flex-wrap items-center gap-2 rounded-md border border-warning/30 bg-warning/10 px-2 py-1.5 text-[11px]">
        <span className="text-warning">{initialLoadFailed ? 'โหลดโควตาไม่สำเร็จ' : 'ยังไม่มีข้อมูลโควตา'}</span>
        <Button type="button" variant="ghost" size="sm" className="h-6 px-1.5 text-[10px]" onClick={onRefresh} disabled={refreshing || cooldown}>
          <RefreshCw className={cn('h-3 w-3', refreshing && 'animate-spin motion-reduce:animate-none')} />
          ลองใหม่
        </Button>
      </div>
    )
  }

  const presentation = presentLineQuota(quota)
  const tone = presentation.severity === 'full' || presentation.severity === 'error'
    ? 'text-destructive'
    : presentation.severity === 'warning'
      ? 'text-warning'
      : 'text-foreground'
  const progressTone = presentation.severity === 'full'
    ? 'bg-destructive'
    : presentation.severity === 'warning'
      ? 'bg-warning'
      : 'bg-success'

  return (
    <div className="mt-2 rounded-md border border-border/70 bg-muted/30 px-2 py-1.5" aria-live="polite">
      <div className="flex items-start gap-2">
        <MessageSquareText className={cn('mt-0.5 h-3.5 w-3.5 shrink-0', tone)} aria-hidden />
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-1.5">
            <span className={cn('text-[11px] font-semibold', tone)}>{presentation.headline}</span>
            {quota.is_stale && (
              <Badge variant="outline" className="border-warning/30 px-1.5 py-0 text-[9px] text-warning">ข้อมูลล่าสุดที่มี</Badge>
            )}
            {presentation.severity === 'warning' && quota.quota_type === 'limited' && (
              <span className="text-[9px] font-medium text-warning">ใกล้เต็ม</span>
            )}
            {presentation.severity === 'full' && (
              <span className="text-[9px] font-medium text-destructive">เต็มแล้ว</span>
            )}
          </div>
          <div className="mt-0.5 text-[10px] text-muted-foreground">{presentation.detail}</div>
          {presentation.percentage != null && (
            <div
              className="mt-1.5 h-1.5 overflow-hidden rounded-full bg-muted"
              role="progressbar"
              aria-label="สัดส่วนการใช้โควตา LINE"
              aria-valuemin={0}
              aria-valuemax={100}
              aria-valuenow={Math.round(presentation.percentage)}
            >
              <div className={cn('h-full rounded-full', progressTone)} style={{ width: `${presentation.percentage}%` }} />
            </div>
          )}
          <div className="mt-1 flex flex-wrap items-center gap-x-2 gap-y-0.5 text-[9px] text-muted-foreground">
            {quota.checked_at && <span>ตรวจล่าสุด {dayjs(quota.checked_at).format('DD/MM/YY HH:mm:ss')}</span>}
            {quota.is_stale && quota.error_code && <span>{lineQuotaErrorLabel(quota.error_code)}</span>}
          </div>
        </div>
        <Button
          type="button"
          variant="ghost"
          size="sm"
          className="h-6 w-6 shrink-0 p-0"
          onClick={onRefresh}
          disabled={refreshing || cooldown}
          title={cooldown ? 'กรุณารอ 10 วินาทีก่อนรีเฟรชอีกครั้ง' : 'ดึงโควตาล่าสุดจาก LINE'}
          aria-label={cooldown ? 'รอก่อนรีเฟรชโควตาอีกครั้ง' : 'รีเฟรชโควตาจาก LINE'}
        >
          <RefreshCw className={cn('h-3 w-3', refreshing && 'animate-spin motion-reduce:animate-none')} />
        </Button>
      </div>
    </div>
  )
}

function RecipientSourceSummary({ recipient, catalog }: { recipient: LineRecipient; catalog: LineEventDefinition[] }) {
  return (
    <div className="min-w-0">
      <div className="flex flex-wrap gap-1">
        {recipientSourceSummary(recipient.event_keys ?? [], catalog).map((item) => (
          <Badge key={item.source} variant="outline" className={sourceBadgeClass(item.source)}>{sourceLabel(item.source)} {item.count}</Badge>
        ))}
      </div>
      <div className="mt-1 text-xs text-muted-foreground">{recipient.event_keys?.length ?? 0} ประเภท</div>
    </div>
  )
}

function RecipientActions({
  recipient,
  onTest,
  onEdit,
  onDelete,
}: {
  recipient: LineRecipient
  onTest: (recipient: LineRecipient) => void
  onEdit: (recipient: LineRecipient) => void
  onDelete: (recipient: LineRecipient) => void
}) {
  const canTest = recipient.enabled && (recipient.event_keys?.length ?? 0) > 0
  return (
    <div className="flex shrink-0 justify-end gap-1">
      <Button variant="outline" size="sm" className="h-8 gap-1 px-2 text-xs" disabled={!canTest} onClick={() => onTest(recipient)} title={canTest ? 'เลือกประเภทแล้วส่ง Flex ทดสอบ' : 'เปิดผู้รับและเลือกประเภทแจ้งเตือนก่อน'}><Send className="h-3.5 w-3.5" />ทดสอบ</Button>
      <Button variant="ghost" size="sm" className="h-8 px-2" onClick={() => onEdit(recipient)} aria-label={`จัดการผู้รับ ${recipient.name}`}><Edit3 className="h-3.5 w-3.5" /></Button>
      <Button variant="ghost" size="sm" className="h-8 px-2 text-destructive hover:text-destructive" onClick={() => onDelete(recipient)} aria-label={`ลบผู้รับ ${recipient.name}`}><Trash2 className="h-3.5 w-3.5" /></Button>
    </div>
  )
}

function CandidatePickerSheet({
  open,
  candidates,
  loading,
  senderNameById,
  onOpenChange,
  onRefresh,
  onSelect,
  onHide,
}: {
  open: boolean
  candidates: LineCandidate[]
  loading: boolean
  senderNameById: Map<string, string>
  onOpenChange: (open: boolean) => void
  onRefresh: () => void
  onSelect: (candidate: LineCandidate) => void
  onHide: (candidate: LineCandidate) => void
}) {
  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent side="right" className="flex w-full flex-col gap-0 p-0 sm:max-w-xl">
        <SheetHeader className="border-b px-4 py-4 pr-12 text-left">
          <SheetTitle>เพิ่มผู้รับแจ้งเตือน</SheetTitle>
          <SheetDescription>ให้ผู้รับทัก LINE OA ก่อน แล้วเลือกจากรายการล่าสุด ระบบจะใช้ปลายทางจาก Webhook โดยไม่ต้องกรอก ID เอง</SheetDescription>
        </SheetHeader>
        <div className="flex items-center justify-between gap-3 border-b px-4 py-3">
          <span className="text-sm text-muted-foreground">พบ {candidates.length} รายการ</span>
          <Button variant="outline" size="sm" className="gap-1.5" onClick={onRefresh} disabled={loading}><RefreshCw className={cn('h-3.5 w-3.5', loading && 'animate-spin motion-reduce:animate-none')} />รีเฟรช</Button>
        </div>
        <div className="min-h-0 flex-1 overflow-y-auto p-4">
          {loading ? (
            <div className="rounded-lg border border-dashed p-8 text-center text-sm text-muted-foreground">กำลังโหลดผู้ที่ทัก LINE OA</div>
          ) : candidates.length === 0 ? (
            <EmptyState icon={MessageCircle} title="ยังไม่มีคนทัก LINE OA" description="ให้ผู้รับส่งข้อความหา OA แล้วกดรีเฟรช รายการจะปรากฏที่นี่" />
          ) : (
            <div className="divide-y rounded-lg border border-border">
              {candidates.map((candidate) => (
                <div key={candidate.id} className="flex items-start gap-3 p-3">
                  <div className="min-w-0 flex-1">
                    <div className="flex flex-wrap items-center gap-2"><span className="font-medium">{candidateName(candidate)}</span>{candidate.is_recipient && <Badge className="bg-success/15 text-success">เพิ่มแล้ว</Badge>}</div>
                    <div className="mt-0.5 text-xs text-muted-foreground">{candidate.line_oa_name || senderNameById.get(candidate.line_oa_id) || 'LINE OA'} · {destinationLabels[candidate.destination_type]} · ทักล่าสุด {formatDate(candidate.last_seen_at)}</div>
                    {candidate.last_message_preview && <div className="mt-1 truncate text-xs text-muted-foreground">ข้อความล่าสุด: {candidate.last_message_preview}</div>}
                  </div>
                  <div className="flex shrink-0 gap-1">
                    <Button variant="outline" size="sm" className="h-8 gap-1 px-2 text-xs" disabled={candidate.is_recipient} onClick={() => onSelect(candidate)}><UserPlus className="h-3.5 w-3.5" />เลือก</Button>
                    <Button variant="ghost" size="sm" className="h-8 px-2 text-muted-foreground" onClick={() => onHide(candidate)} aria-label={`ซ่อน ${candidateName(candidate)}`}><Trash2 className="h-3.5 w-3.5" /></Button>
                  </div>
                </div>
              ))}
            </div>
          )}
        </div>
      </SheetContent>
    </Sheet>
  )
}

function RecipientTestDialog({
  recipient,
  catalog,
  eventKey,
  onEventKeyChange,
  onOpenChange,
  onConfirm,
}: {
  recipient: LineRecipient | null
  catalog: LineEventDefinition[]
  eventKey: string
  onEventKeyChange: (eventKey: string) => void
  onOpenChange: (open: boolean) => void
  onConfirm: () => void
}) {
  const availableEvents = catalog.filter((event) => recipient?.event_keys.includes(event.key))
  const selected = availableEvents.find((event) => event.key === eventKey)
  return (
    <Dialog open={!!recipient} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>ส่ง Flex ทดสอบ</DialogTitle>
          <DialogDescription>{recipient ? `ส่งข้อความจำลองไปที่ ${recipient.name} เพื่อทดสอบการรับ LINE โดยไม่สร้าง event งานจริง` : ''}</DialogDescription>
        </DialogHeader>
        <div className="space-y-2">
          <Label htmlFor="line-test-event">เลือกประเภท Flex ที่ต้องการส่งทดสอบ</Label>
          <Select value={eventKey} onValueChange={onEventKeyChange}>
            <SelectTrigger id="line-test-event"><SelectValue placeholder="เลือกประเภทแจ้งเตือน" /></SelectTrigger>
            <SelectContent>{availableEvents.map((event) => <SelectItem key={event.key} value={event.key}>{event.label}</SelectItem>)}</SelectContent>
          </Select>
          {selected && <p className="text-xs text-muted-foreground">{selected.description}</p>}
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>ยกเลิก</Button>
          <Button onClick={onConfirm} disabled={!selected}>ส่ง Flex ทดสอบ</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function webhookURL(senderID: string) {
  const origin = typeof window === 'undefined' ? '' : window.location.origin
  return `${origin}/webhook/line/${senderID}`
}

function candidateName(candidate: LineCandidate) {
  return candidate.display_name?.trim() || `${destinationLabels[candidate.destination_type]} ${shortId(candidate.destination_id)}`
}

function senderNameForDialog(senders: LineSender[], id: string) {
  return senders.find((sender) => sender.id === id)?.name || ''
}

function SenderDialog({
  open,
  sender,
  onOpenChange,
  onSaved,
}: {
  open: boolean
  sender: LineSender | null
  onOpenChange: (open: boolean) => void
  onSaved: () => void
}) {
  const isEdit = !!sender
  const [name, setName] = useState('')
  const [secret, setSecret] = useState('')
  const [token, setToken] = useState('')
  const [enabled, setEnabled] = useState(true)
  const [showSecret, setShowSecret] = useState(false)
  const [saving, setSaving] = useState(false)

  useEffect(() => {
    if (!open) return
    setName(sender?.name ?? '')
    setSecret('')
    setToken('')
    setEnabled(sender?.enabled ?? true)
    setShowSecret(false)
  }, [open, sender])

  const submit = async () => {
    if (!name.trim()) {
      toast.error('กรุณากรอกชื่อ LINE OA')
      return
    }
    if (!isEdit && (!secret.trim() || !token.trim())) {
      toast.error('กรุณากรอก Channel secret และ access token')
      return
    }
    setSaving(true)
    try {
      const body = {
        name: name.trim(),
        channel_secret: secret.trim(),
        channel_access_token: token.trim(),
        admin_user_id: '',
        greeting: '',
        enabled,
        mark_as_read_enabled: false,
      }
      if (isEdit && sender) {
        await client.put(`/api/settings/line-notifications/senders/${sender.id}`, body)
      } else {
        await client.post('/api/settings/line-notifications/senders', body)
      }
      toast.success(isEdit ? 'บันทึก LINE OA แล้ว' : 'เพิ่ม LINE OA แล้ว')
      onOpenChange(false)
      onSaved()
    } catch (e: any) {
      toast.error(e?.response?.data?.error ?? 'บันทึก LINE OA ไม่สำเร็จ')
    } finally {
      setSaving(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-xl">
        <DialogHeader>
          <DialogTitle>{isEdit ? 'แก้ไขการเชื่อมต่อ LINE OA' : 'เพิ่มการเชื่อมต่อ LINE OA'}</DialogTitle>
          <DialogDescription>
            ใช้สำหรับส่งแจ้งเตือน Marketplace หลังบันทึกแล้วระบบจะแสดง Webhook URL สำหรับตั้งค่าใน LINE Developers
          </DialogDescription>
        </DialogHeader>
        <div className="space-y-4">
          <div className="space-y-1.5">
            <Label>ชื่อ LINE OA</Label>
            <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="เช่น Nexflow แจ้งเตือน" />
          </div>
          <div className="space-y-1.5">
            <Label>Channel secret</Label>
            <div className="flex gap-2">
              <Input
                type={showSecret ? 'text' : 'password'}
                value={secret}
                onChange={(e) => setSecret(e.target.value)}
                placeholder={isEdit ? 'เว้นว่างถ้าไม่เปลี่ยน' : 'จาก LINE Developer Console'}
                className="font-mono text-xs"
              />
              <Button type="button" variant="outline" size="sm" onClick={() => setShowSecret((v) => !v)}>
                {showSecret ? <EyeOff className="h-3.5 w-3.5" /> : <Eye className="h-3.5 w-3.5" />}
              </Button>
            </div>
          </div>
          <div className="space-y-1.5">
            <Label>Channel access token</Label>
            <Textarea
              value={token}
              onChange={(e) => setToken(e.target.value)}
              placeholder={isEdit ? 'เว้นว่างถ้าไม่เปลี่ยน' : 'Long-lived channel access token'}
              className="min-h-[84px] resize-none font-mono text-xs"
            />
          </div>
          <label className="flex items-center justify-between rounded-md border border-border bg-muted/35 px-3 py-2">
            <span>
              <span className="block text-sm font-medium">เปิดใช้ LINE OA นี้</span>
              <span className="block text-xs text-muted-foreground">ปิดไว้ได้ถ้าต้องการหยุดส่งจาก OA นี้ชั่วคราว</span>
            </span>
            <Switch checked={enabled} onCheckedChange={setEnabled} />
          </label>
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={saving}>ยกเลิก</Button>
          <Button onClick={submit} disabled={saving}>{saving ? 'กำลังบันทึก' : 'บันทึก'}</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function RecipientSheet({
  open,
  recipient,
  candidate,
  senders,
  eventCatalog,
  onOpenChange,
  onSaved,
}: {
  open: boolean
  recipient: LineRecipient | null
  candidate?: LineCandidate | null
  senders: LineSender[]
  eventCatalog: LineEventDefinition[]
  onOpenChange: (open: boolean) => void
  onSaved: () => void
}) {
  const isEdit = !!recipient
  const isCandidateMode = !!candidate && !recipient
  const [lineOAID, setLineOAID] = useState('')
  const [name, setName] = useState('')
  const [destinationType, setDestinationType] = useState<LineRecipient['destination_type']>('user')
  const [destinationID, setDestinationID] = useState('')
  const [enabled, setEnabled] = useState(true)
  const [eventKeys, setEventKeys] = useState<string[]>([])
  const [saving, setSaving] = useState(false)

  useEffect(() => {
    if (!open) return
    if (candidate) {
      setLineOAID(candidate.line_oa_id)
      setName(candidateName(candidate))
      setDestinationType(candidate.destination_type)
      setDestinationID(candidate.destination_id)
      setEnabled(true)
      setEventKeys(eventCatalog.filter((event) => event.default_enabled).map((event) => event.key))
      return
    }
    setLineOAID(recipient?.line_oa_id || senders[0]?.id || '')
    setName(recipient?.name ?? '')
    setDestinationType(recipient?.destination_type ?? 'user')
    setDestinationID(recipient?.destination_id ?? '')
    setEnabled(recipient?.enabled ?? true)
    setEventKeys(recipient?.event_keys ?? eventCatalog.filter((event) => event.default_enabled).map((event) => event.key))
  }, [open, recipient, candidate, senders, eventCatalog])

  const submit = async () => {
    if (!lineOAID || !name.trim() || !destinationID.trim()) {
      toast.error('ข้อมูลผู้รับไม่ครบ กรุณาเพิ่มจากรายการคนที่ทัก LINE OA ล่าสุดอีกครั้ง')
      return
    }
    if (enabled && eventKeys.length === 0) {
      toast.error('กรุณาเลือกอย่างน้อย 1 ประเภท หรือปิดรับแจ้งเตือนสำหรับผู้รับนี้')
      return
    }
    setSaving(true)
    try {
      const body = {
        line_oa_id: lineOAID,
        name: name.trim(),
        destination_type: destinationType,
        destination_id: destinationID.trim(),
        enabled,
        event_keys: eventKeys,
      }
      if (isCandidateMode && candidate) {
        await client.post(`/api/settings/line-notifications/candidates/${candidate.id}/add-recipient`, {
          name: name.trim(),
          enabled,
          event_keys: eventKeys,
        })
      } else if (isEdit && recipient) {
        await client.put(`/api/settings/line-notifications/recipients/${recipient.id}`, body)
      } else {
        throw new Error('กรุณาเพิ่มผู้รับจากรายการคนที่ทัก LINE OA ล่าสุด')
      }
      toast.success(isEdit ? 'บันทึกผู้รับแล้ว' : 'เพิ่มผู้รับแล้ว')
      onOpenChange(false)
      onSaved()
    } catch (e: any) {
      toast.error(e?.response?.data?.error ?? 'บันทึกผู้รับไม่สำเร็จ')
    } finally {
      setSaving(false)
    }
  }

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent side="right" className="flex w-full flex-col gap-0 p-0 sm:max-w-2xl">
        <SheetHeader className="border-b px-4 py-4 pr-12 text-left sm:px-6">
          <SheetTitle>{isEdit ? 'จัดการผู้รับแจ้งเตือน' : 'เพิ่มผู้รับแจ้งเตือน'}</SheetTitle>
          <SheetDescription>
            {isCandidateMode
              ? 'ตรวจชื่อผู้รับแล้วกดบันทึก ระบบจะใช้ปลายทางที่จับได้จาก Webhook ให้อัตโนมัติ'
              : 'แก้ชื่อ สถานะ และประเภทแจ้งเตือน โดยไม่เปลี่ยนปลายทาง LINE ที่ตรวจพบแล้ว'}
          </SheetDescription>
        </SheetHeader>
        <div className="min-h-0 flex-1 space-y-4 overflow-y-auto px-4 py-4 sm:px-6">
          <div className="rounded-md border border-border bg-muted/35 px-3 py-2">
            <div className="text-xs text-muted-foreground">LINE OA</div>
            <div className="mt-1 text-sm font-medium">
              {senderNameForDialog(senders, lineOAID) || candidate?.line_oa_name || recipient?.line_oa_name || 'LINE OA'}
            </div>
            <div className="mt-1 text-xs text-muted-foreground">ประเภทปลายทาง: {destinationLabels[destinationType]}</div>
          </div>
          <div className="space-y-1.5">
            <Label>ชื่อผู้รับ</Label>
            <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="เช่น คุณบอส, ทีมคลัง, แอดมินกลาง" />
          </div>
          <label className="flex items-center justify-between rounded-md border border-border bg-muted/35 px-3 py-2">
            <span>
              <span className="block text-sm font-medium">เปิดรับแจ้งเตือน</span>
              <span className="block text-xs text-muted-foreground">ปิดเพื่อหยุดทุกประเภท คิวที่ยังไม่เริ่มส่งจะถูกระงับ</span>
            </span>
            <Switch checked={enabled} onCheckedChange={setEnabled} />
          </label>
          <RecipientEventPreferences catalog={eventCatalog} eventKeys={eventKeys} enabled={enabled} onChange={setEventKeys} />
        </div>
        <SheetFooter className="border-t bg-background px-4 py-3 sm:px-6">
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={saving}>ยกเลิก</Button>
          <Button onClick={submit} disabled={saving}>{saving ? 'กำลังบันทึก' : 'บันทึกการแจ้งเตือน'}</Button>
        </SheetFooter>
      </SheetContent>
    </Sheet>
  )
}

function shortId(id: string) {
  if (!id) return '-'
  if (id.length <= 14) return id
  return `${id.slice(0, 8)}...${id.slice(-4)}`
}

function formatDate(value?: string) {
  if (!value) return 'ยังไม่มี'
  return dayjs(value).format('DD/MM/YY HH:mm')
}

function deliveryStatusLabel(status: string) {
  switch (status) {
    case 'sent':
      return 'ส่งแล้ว'
    case 'sending':
      return 'กำลังส่ง'
    case 'queued':
      return 'รอส่ง'
    case 'failed':
      return 'ล้มเหลว'
    case 'suppressed':
      return 'ระงับแล้ว'
    default:
      return status || '-'
  }
}
