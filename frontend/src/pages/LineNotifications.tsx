import { useEffect, useMemo, useState } from 'react'
import dayjs from 'dayjs'
import {
  AlertTriangle,
  Bell,
  CheckCircle2,
  Copy,
  Edit3,
  Eye,
  EyeOff,
  MessageCircle,
  Plus,
  RefreshCw,
  Send,
  Trash2,
  UserPlus,
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
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'
import { ConfirmDialog } from '@/components/common/ConfirmDialog'
import { DataTable } from '@/components/common/DataTable'
import { EmptyState } from '@/components/common/EmptyState'
import { PageHeader } from '@/components/common/PageHeader'

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
  title: string
  entity_id: string
  status: 'queued' | 'sending' | 'sent' | 'failed'
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
  readiness: {
    sender_count: number
    enabled_sender_count: number
    recipient_count: number
    enabled_recipient_count: number
    shopee_realtime_enabled?: boolean
  }
}

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
}

export default function LineNotifications() {
  const [data, setData] = useState<Overview | null>(null)
  const [loading, setLoading] = useState(true)
  const [senderDialog, setSenderDialog] = useState<LineSender | 'new' | null>(null)
  const [recipientDialog, setRecipientDialog] = useState<LineRecipient | null>(null)
  const [deleteRecipient, setDeleteRecipient] = useState<LineRecipient | null>(null)
  const [testRecipient, setTestRecipient] = useState<LineRecipient | null>(null)
  const [candidateToAdd, setCandidateToAdd] = useState<LineCandidate | null>(null)
  const [candidateToHide, setCandidateToHide] = useState<LineCandidate | null>(null)

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

  useEffect(() => {
    load()
  }, [])

  const readiness = data?.readiness
  const ready = !!readiness?.enabled_sender_count && !!readiness.enabled_recipient_count
  const enabledRecipients = data?.recipients.filter((r) => r.enabled).length ?? 0

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
      await client.post(`/api/settings/line-notifications/recipients/${testRecipient.id}/test`)
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

  return (
    <div className="space-y-5">
      <PageHeader
        title="LINE แจ้งเตือน"
        description="ตั้งค่า LINE OA สำหรับส่งแจ้งเตือนออเดอร์ใหม่ ให้ผู้รับทัก OA แล้วเลือกเพิ่มจากรายการล่าสุดได้เลย"
        actions={
          <>
            <Button variant="outline" size="sm" className="gap-1.5" onClick={load}>
              <RefreshCw className="h-3.5 w-3.5" />
              รีเฟรช
            </Button>
            <Button variant="outline" size="sm" className="gap-1.5" onClick={() => setSenderDialog('new')}>
              <Plus className="h-3.5 w-3.5" />
              เพิ่ม LINE OA
            </Button>
          </>
        }
      />

      <section className="rounded-lg border border-border/80 bg-card/95 p-4">
        <div className="flex flex-col gap-3 md:flex-row md:items-start md:justify-between">
          <div className="flex items-start gap-3">
            <div className="rounded-md border border-border bg-muted p-2">
              {ready ? <CheckCircle2 className="h-5 w-5 text-success" /> : <AlertTriangle className="h-5 w-5 text-warning" />}
            </div>
            <div>
              <h2 className="text-base font-semibold text-foreground">
                {ready ? 'พร้อมส่ง LINE เมื่อมีออเดอร์ใหม่' : 'ยังตั้งค่า LINE แจ้งเตือนไม่ครบ'}
              </h2>
              <p className="mt-1 text-sm text-muted-foreground">
                เพิ่ม LINE OA, copy Webhook URL ไปเปิด Use webhook ใน LINE Developers, ให้ผู้รับทัก OA แล้วเพิ่มเป็นผู้รับแจ้งเตือน
              </p>
            </div>
          </div>
          <div className="grid grid-cols-2 gap-2 text-sm md:grid-cols-3">
            <ReadinessChip label="LINE OA" value={`${readiness?.enabled_sender_count ?? 0}/${readiness?.sender_count ?? 0}`} ok={!!readiness?.enabled_sender_count} />
            <ReadinessChip label="ผู้รับ" value={`${enabledRecipients}`} ok={enabledRecipients > 0} />
            <ReadinessChip label="ล่าสุด" value={data?.deliveries[0]?.status ? deliveryStatusLabel(data.deliveries[0].status) : 'ยังไม่มี'} ok={data?.deliveries[0]?.status === 'sent'} />
          </div>
        </div>
      </section>

      <section className="grid gap-5 xl:grid-cols-[minmax(0,1fr)_360px]">
        <div className="space-y-5">
          <div className="rounded-lg border border-border/80 bg-card/95 p-4">
            <div className="mb-3 flex items-center justify-between gap-2">
              <div>
                <h2 className="text-base font-semibold">LINE OA sender</h2>
                <p className="text-sm text-muted-foreground">เพิ่ม OA แล้วนำ Webhook URL ไปใส่ใน LINE Developers ของ OA นั้น</p>
              </div>
            </div>
            <div className="mb-3 grid gap-2 text-sm sm:grid-cols-2 xl:grid-cols-4">
              {[
                'เพิ่ม LINE OA',
                'คัดลอก Webhook URL',
                'เปิด Use webhook ใน LINE Developers',
                'ให้ผู้รับทัก LINE OA',
              ].map((step, index) => (
                <div key={step} className="rounded-md border border-border bg-muted/35 px-3 py-2">
                  <span className="mr-2 font-mono text-xs text-muted-foreground">{index + 1}</span>
                  <span className="font-medium">{step}</span>
                </div>
              ))}
            </div>
            <DataTable<LineSender>
              data={data?.senders ?? []}
              loading={loading}
              dense
              empty={<EmptyState icon={Bell} title="ยังไม่มี LINE OA" description="เพิ่ม Channel secret และ access token ก่อนกำหนดผู้รับแจ้งเตือน" />}
              columns={[
                {
                  key: 'name',
                  header: 'ชื่อ',
                  cell: (s) => (
                    <div>
                      <div className="font-medium">{s.name}</div>
                      <div className="font-mono text-[11px] text-muted-foreground">{s.bot_user_id ? `bot ${shortId(s.bot_user_id)}` : 'ยังไม่ได้ทดสอบ token'}</div>
                    </div>
                  ),
                },
                {
                  key: 'webhook',
                  header: 'Webhook URL',
                  cell: (s) => (
                    <div className="flex max-w-[360px] items-center gap-2">
                      <code className="min-w-0 flex-1 truncate rounded-md bg-muted/50 px-2 py-1 font-mono text-[11px] text-foreground">
                        {webhookURL(s.id)}
                      </code>
                      <Button variant="ghost" size="sm" className="h-7 px-2" onClick={() => copyWebhookURL(s)} title="คัดลอก Webhook URL">
                        <Copy className="h-3.5 w-3.5" />
                      </Button>
                    </div>
                  ),
                },
                {
                  key: 'status',
                  header: 'สถานะ',
                  cell: (s) => (
                    <div className="flex flex-wrap gap-1">
                      {s.enabled ? <Badge className="bg-success/15 text-success">เปิด</Badge> : <Badge variant="secondary">ปิด</Badge>}
                      {s.bot_user_id ? <Badge className="bg-info/15 text-info">token ใช้ได้</Badge> : <Badge className="bg-warning/15 text-warning">รอทดสอบ</Badge>}
                    </div>
                  ),
                },
                {
                  key: 'updated',
                  header: 'แก้ไขล่าสุด',
                  cell: (s) => <span className="text-xs text-muted-foreground">{formatDate(s.updated_at)}</span>,
                },
                {
                  key: 'actions',
                  header: '',
                  headerClassName: 'text-right',
                  className: 'text-right',
                  cell: (s) => (
                    <div className="flex justify-end gap-1">
                      <Button variant="outline" size="sm" className="h-7 px-2 text-xs" onClick={() => handleTestSender(s)}>
                        ทดสอบ OA
                      </Button>
                      <Button variant="ghost" size="sm" className="h-7 px-2" onClick={() => setSenderDialog(s)}>
                        <Edit3 className="h-3.5 w-3.5" />
                      </Button>
                    </div>
                  ),
                },
              ]}
            />
          </div>

          <div className="rounded-lg border border-border/80 bg-card/95 p-4">
            <div className="mb-3 flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
              <div>
                <h2 className="text-base font-semibold">ผู้ที่ทัก LINE OA ล่าสุด</h2>
                <p className="text-sm text-muted-foreground">หลังตั้ง Webhook แล้ว ให้ผู้รับส่งข้อความหา OA จากนั้นกดเพิ่มเป็นผู้รับแจ้งเตือน</p>
              </div>
              <Button variant="outline" size="sm" className="gap-1.5" onClick={load}>
                <RefreshCw className="h-3.5 w-3.5" />
                รีเฟรช
              </Button>
            </div>
            <DataTable<LineCandidate>
              data={data?.candidates ?? []}
              loading={loading}
              dense
              empty={<EmptyState icon={MessageCircle} title="ยังไม่มีคนทัก LINE OA" description="ให้ผู้รับส่งข้อความหา OA หลังเปิด Webhook แล้วกดรีเฟรช รายการจะขึ้นที่นี่" />}
              columns={[
                {
                  key: 'contact',
                  header: 'ปลายทาง',
                  cell: (c) => (
                    <div className="min-w-0">
                      <div className="truncate font-medium">{candidateName(c)}</div>
                      <div className="truncate text-xs text-muted-foreground">{c.line_oa_name || senderNameById.get(c.line_oa_id) || 'LINE OA'}</div>
                      {c.last_message_preview && <div className="mt-1 max-w-[360px] truncate text-xs text-muted-foreground">ล่าสุด: {c.last_message_preview}</div>}
                    </div>
                  ),
                },
                {
                  key: 'destination',
                  header: 'ประเภท',
                  cell: (c) => (
                    <Badge variant="secondary">{destinationLabels[c.destination_type]}</Badge>
                  ),
                },
                {
                  key: 'seen',
                  header: 'ทักล่าสุด',
                  cell: (c) => <span className="text-xs text-muted-foreground">{formatDate(c.last_seen_at)}</span>,
                },
                {
                  key: 'status',
                  header: 'สถานะ',
                  cell: (c) => c.is_recipient ? <Badge className="bg-success/15 text-success">เพิ่มแล้ว</Badge> : <Badge className="bg-warning/15 text-warning">ยังไม่ได้เพิ่ม</Badge>,
                },
                {
                  key: 'actions',
                  header: '',
                  headerClassName: 'text-right',
                  className: 'text-right',
                  cell: (c) => (
                    <div className="flex justify-end gap-1">
                      <Button
                        variant="outline"
                        size="sm"
                        className="h-7 gap-1 px-2 text-xs"
                        disabled={c.is_recipient}
                        onClick={() => setCandidateToAdd(c)}
                      >
                        <UserPlus className="h-3 w-3" />
                        เพิ่มเป็นผู้รับ
                      </Button>
                      <Button variant="ghost" size="sm" className="h-7 px-2 text-muted-foreground" onClick={() => setCandidateToHide(c)} title="ซ่อนรายการนี้">
                        <Trash2 className="h-3.5 w-3.5" />
                      </Button>
                    </div>
                  ),
                },
              ]}
            />
          </div>

          <div className="rounded-lg border border-border/80 bg-card/95 p-4">
            <div className="mb-3 flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
              <div>
                <h2 className="text-base font-semibold">ผู้รับแจ้งเตือน</h2>
                <p className="text-sm text-muted-foreground">เพิ่มผู้รับจากรายการคนที่ทัก LINE OA ล่าสุด ระบบจะจับปลายทางให้อัตโนมัติ</p>
              </div>
            </div>
            <DataTable<LineRecipient>
              data={data?.recipients ?? []}
              loading={loading}
              dense
              empty="ยังไม่มีผู้รับแจ้งเตือน"
              columns={[
                {
                  key: 'name',
                  header: 'ผู้รับ',
                  cell: (r) => (
                    <div>
                      <div className="font-medium">{r.name}</div>
                      <div className="text-xs text-muted-foreground">{r.line_oa_name || senderNameById.get(r.line_oa_id) || 'LINE OA'}</div>
                    </div>
                  ),
                },
                {
                  key: 'destination',
                  header: 'ประเภท',
                  cell: (r) => (
                    <Badge variant="secondary">{destinationLabels[r.destination_type]}</Badge>
                  ),
                },
                {
                  key: 'status',
                  header: 'สถานะ',
                  cell: (r) => (
                    <div className="flex flex-wrap gap-1">
                      {r.enabled ? <Badge className="bg-success/15 text-success">เปิด</Badge> : <Badge variant="secondary">ปิด</Badge>}
                      {r.last_error && <Badge className="bg-destructive/15 text-destructive">มี error</Badge>}
                    </div>
                  ),
                },
                {
                  key: 'last',
                  header: 'ส่งล่าสุด',
                  cell: (r) => <span className="text-xs text-muted-foreground">{r.last_sent_at ? formatDate(r.last_sent_at) : 'ยังไม่มี'}</span>,
                },
                {
                  key: 'actions',
                  header: '',
                  headerClassName: 'text-right',
                  className: 'text-right',
                  cell: (r) => (
                    <div className="flex justify-end gap-1">
                      <Button variant="outline" size="sm" className="h-7 gap-1 px-2 text-xs" disabled={!r.enabled} onClick={() => setTestRecipient(r)} title={r.enabled ? 'ส่งข้อความทดสอบไปยังปลายทางนี้' : 'เปิดผู้รับก่อนส่งข้อความทดสอบ'}>
                        <Send className="h-3 w-3" />
                        ทดสอบ
                      </Button>
                      <Button variant="ghost" size="sm" className="h-7 px-2" onClick={() => setRecipientDialog(r)}>
                        <Edit3 className="h-3.5 w-3.5" />
                      </Button>
                      <Button variant="ghost" size="sm" className="h-7 px-2 text-destructive hover:text-destructive" onClick={() => setDeleteRecipient(r)}>
                        <Trash2 className="h-3.5 w-3.5" />
                      </Button>
                    </div>
                  ),
                },
              ]}
            />
          </div>
        </div>

        <aside className="space-y-5">
          <div className="rounded-lg border border-border/80 bg-card/95 p-4">
            <h2 className="text-base font-semibold">ตัวอย่างข้อความสำรอง</h2>
            <p className="mt-1 text-sm text-muted-foreground">ปุ่มทดสอบจะส่ง Flex รูปแบบใหม่ก่อน ข้อความด้านล่างใช้เป็น fallback เมื่อ LINE Flex ส่งไม่สำเร็จ และไม่ใส่ข้อมูลลูกค้า</p>
            <pre className="mt-3 whitespace-pre-wrap rounded-md border border-border bg-muted/50 p-3 text-xs leading-5 text-foreground">
              {data?.sample_text || 'กำลังโหลดตัวอย่างข้อความ'}
            </pre>
          </div>

          <div className="rounded-lg border border-border/80 bg-card/95 p-4">
            <h2 className="text-base font-semibold">ประวัติการส่งล่าสุด</h2>
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
                    <div className="mt-1 text-xs text-muted-foreground">{d.entity_id || d.title}</div>
                    {d.last_error && <div className="mt-1 text-xs text-destructive">{d.last_error}</div>}
                    <div className="mt-2 text-[11px] text-muted-foreground">{formatDate(d.sent_at || d.created_at)}</div>
                  </div>
                ))
              )}
            </div>
          </div>
        </aside>
      </section>

      <SenderDialog
        open={!!senderDialog}
        sender={senderDialog === 'new' ? null : senderDialog}
        onOpenChange={(open) => !open && setSenderDialog(null)}
        onSaved={load}
      />
      <RecipientDialog
        open={!!recipientDialog}
        recipient={recipientDialog}
        candidate={null}
        senders={data?.senders ?? []}
        onOpenChange={(open) => !open && setRecipientDialog(null)}
        onSaved={load}
      />
      <RecipientDialog
        open={!!candidateToAdd}
        recipient={null}
        candidate={candidateToAdd}
        senders={data?.senders ?? []}
        onOpenChange={(open) => !open && setCandidateToAdd(null)}
        onSaved={load}
      />
      <ConfirmDialog
        open={!!testRecipient}
        onOpenChange={(open) => !open && setTestRecipient(null)}
        title="ส่ง Flex ทดสอบ"
        description={testRecipient ? `ระบบจะส่ง Flex ตัวอย่างของออเดอร์ใหม่ไปที่ ${testRecipient.name} เพื่อยืนยันว่าปลายทาง LINE ใช้งานได้ ไม่ใช่ event ออเดอร์จริง` : ''}
        confirmLabel="ส่งทดสอบ"
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

function ReadinessChip({ label, value, ok }: { label: string; value: string; ok: boolean }) {
  return (
    <div className="rounded-md border border-border bg-background/70 px-3 py-2">
      <div className="text-xs text-muted-foreground">{label}</div>
      <div className={ok ? 'text-sm font-semibold text-foreground' : 'text-sm font-semibold text-warning'}>{value}</div>
    </div>
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
          <DialogTitle>{isEdit ? 'แก้ไข LINE OA sender' : 'เพิ่ม LINE OA sender'}</DialogTitle>
          <DialogDescription>
            ใช้สำหรับส่ง Push แจ้งเตือนออเดอร์ใหม่ หลังบันทึกแล้วระบบจะแสดง Webhook URL ให้คัดลอกไป Verify และเปิด Use webhook ใน LINE Developers
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
              <span className="block text-sm font-medium">เปิดใช้งาน sender นี้</span>
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

function RecipientDialog({
  open,
  recipient,
  candidate,
  senders,
  onOpenChange,
  onSaved,
}: {
  open: boolean
  recipient: LineRecipient | null
  candidate?: LineCandidate | null
  senders: LineSender[]
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
  const [saving, setSaving] = useState(false)

  useEffect(() => {
    if (!open) return
    if (candidate) {
      setLineOAID(candidate.line_oa_id)
      setName(candidateName(candidate))
      setDestinationType(candidate.destination_type)
      setDestinationID(candidate.destination_id)
      setEnabled(true)
      return
    }
    setLineOAID(recipient?.line_oa_id || senders[0]?.id || '')
    setName(recipient?.name ?? '')
    setDestinationType(recipient?.destination_type ?? 'user')
    setDestinationID(recipient?.destination_id ?? '')
    setEnabled(recipient?.enabled ?? true)
  }, [open, recipient, candidate, senders])

  const submit = async () => {
    if (!lineOAID || !name.trim() || !destinationID.trim()) {
      toast.error('ข้อมูลผู้รับไม่ครบ กรุณาเพิ่มจากรายการคนที่ทัก LINE OA ล่าสุดอีกครั้ง')
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
      }
      if (isCandidateMode && candidate) {
        await client.post(`/api/settings/line-notifications/candidates/${candidate.id}/add-recipient`, {
          name: name.trim(),
          enabled,
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
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-xl">
        <DialogHeader>
          <DialogTitle>{isEdit ? 'แก้ไขผู้รับแจ้งเตือน' : 'เพิ่มผู้รับจาก LINE OA ล่าสุด'}</DialogTitle>
          <DialogDescription>
            {isCandidateMode
              ? 'ตรวจชื่อผู้รับแล้วกดบันทึก ระบบจะใช้ปลายทางที่จับได้จาก Webhook ให้อัตโนมัติ'
              : 'แก้ชื่อหรือสถานะการรับแจ้งเตือน ปลายทาง LINE ถูกจับจาก Webhook แล้ว'}
          </DialogDescription>
        </DialogHeader>
        <div className="space-y-4">
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
              <span className="block text-xs text-muted-foreground">ปิดไว้ได้ถ้าคนนี้ยังไม่ต้องรับออเดอร์ใหม่</span>
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
    default:
      return status || '-'
  }
}
