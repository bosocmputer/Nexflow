import { useEffect, useMemo, useState } from 'react'
import dayjs from 'dayjs'
import {
  AlertTriangle,
  Bell,
  CheckCircle2,
  Edit3,
  Eye,
  EyeOff,
  Plus,
  RefreshCw,
  Send,
  Trash2,
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

interface Overview {
  senders: LineSender[]
  recipients: LineRecipient[]
  deliveries: LineDelivery[]
  sample_text: string
  readiness: {
    sender_count: number
    enabled_sender_count: number
    recipient_count: number
    enabled_recipient_count: number
    shopee_realtime_enabled: boolean
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
  const [recipientDialog, setRecipientDialog] = useState<LineRecipient | 'new' | null>(null)
  const [deleteRecipient, setDeleteRecipient] = useState<LineRecipient | null>(null)
  const [testRecipient, setTestRecipient] = useState<LineRecipient | null>(null)

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
  const ready = !!readiness?.enabled_sender_count && !!readiness.enabled_recipient_count && readiness.shopee_realtime_enabled
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

  return (
    <div className="space-y-5">
      <PageHeader
        title="LINE แจ้งเตือน"
        description="ส่งเฉพาะออเดอร์ใหม่จาก Shopee Realtime ที่รอสร้างเอกสาร ไม่ส่ง event ซ้ำจาก sync และไม่เปิดระบบแชทลูกค้า"
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
            <Button
              size="sm"
              className="gap-1.5"
              onClick={() => setRecipientDialog('new')}
              disabled={!data?.senders.length}
              title={!data?.senders.length ? 'เพิ่ม LINE OA sender ก่อน' : undefined}
            >
              <Plus className="h-3.5 w-3.5" />
              เพิ่มผู้รับ
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
                ต้องมี LINE OA ที่เปิดใช้งาน, ผู้รับอย่างน้อย 1 คน และ Shopee Realtime เปิดอยู่ ระบบจะส่งเฉพาะ new order จริงที่ผ่าน dedupe แล้ว
              </p>
            </div>
          </div>
          <div className="grid grid-cols-2 gap-2 text-sm md:grid-cols-4">
            <ReadinessChip label="LINE OA" value={`${readiness?.enabled_sender_count ?? 0}/${readiness?.sender_count ?? 0}`} ok={!!readiness?.enabled_sender_count} />
            <ReadinessChip label="ผู้รับ" value={`${enabledRecipients}`} ok={enabledRecipients > 0} />
            <ReadinessChip label="Shopee Realtime" value={readiness?.shopee_realtime_enabled ? 'เปิด' : 'ปิด'} ok={!!readiness?.shopee_realtime_enabled} />
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
                <p className="text-sm text-muted-foreground">ใช้ Channel secret และ access token สำหรับส่ง Push notification</p>
              </div>
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
                  key: 'status',
                  header: 'สถานะ',
                  cell: (s) => s.enabled ? <Badge className="bg-success/15 text-success">เปิด</Badge> : <Badge variant="secondary">ปิด</Badge>,
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
                <h2 className="text-base font-semibold">ผู้รับแจ้งเตือน</h2>
                <p className="text-sm text-muted-foreground">กรอก LINE user ID, group ID หรือ room ID ด้วยมือก่อน ปุ่มทดสอบส่งเฉพาะข้อความตัวอย่าง</p>
              </div>
              <Button size="sm" className="gap-1.5" disabled={!data?.senders.length} onClick={() => setRecipientDialog('new')}>
                <Plus className="h-3.5 w-3.5" />
                เพิ่มผู้รับ
              </Button>
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
                  header: 'ปลายทาง',
                  cell: (r) => (
                    <div className="font-mono text-xs">
                      <span className="text-muted-foreground">{destinationLabels[r.destination_type]} </span>
                      {shortId(r.destination_id)}
                    </div>
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
            <h2 className="text-base font-semibold">ตัวอย่างข้อความ</h2>
            <p className="mt-1 text-sm text-muted-foreground">ข้อความจริงจะใช้ข้อมูล order จาก Shopee Realtime และไม่ใส่ข้อมูลลูกค้า</p>
            <pre className="mt-3 whitespace-pre-wrap rounded-md border border-border bg-muted/50 p-3 text-xs leading-5 text-foreground">
              {data?.sample_text || 'กำลังโหลดตัวอย่างข้อความ'}
            </pre>
          </div>

          <div className="rounded-lg border border-border/80 bg-card/95 p-4">
            <h2 className="text-base font-semibold">ประวัติการส่งล่าสุด</h2>
            <div className="mt-3 space-y-2">
              {(data?.deliveries ?? []).length === 0 ? (
                <p className="text-sm text-muted-foreground">ยังไม่มีการส่ง LINE จาก Shopee Realtime</p>
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
        recipient={recipientDialog === 'new' ? null : recipientDialog}
        senders={data?.senders ?? []}
        onOpenChange={(open) => !open && setRecipientDialog(null)}
        onSaved={load}
      />
      <ConfirmDialog
        open={!!testRecipient}
        onOpenChange={(open) => !open && setTestRecipient(null)}
        title="ส่งข้อความทดสอบ"
        description={testRecipient ? `ระบบจะส่งข้อความตัวอย่างไปที่ ${testRecipient.name} เพื่อยืนยันว่า destination ID ใช้งานได้ ไม่ใช่ event ออเดอร์จริง` : ''}
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
            ใช้สำหรับส่ง Push แจ้งเตือนออเดอร์ Shopee Realtime เท่านั้น ไม่เปิดระบบแชทลูกค้า
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
  senders,
  onOpenChange,
  onSaved,
}: {
  open: boolean
  recipient: LineRecipient | null
  senders: LineSender[]
  onOpenChange: (open: boolean) => void
  onSaved: () => void
}) {
  const isEdit = !!recipient
  const [lineOAID, setLineOAID] = useState('')
  const [name, setName] = useState('')
  const [destinationType, setDestinationType] = useState<LineRecipient['destination_type']>('user')
  const [destinationID, setDestinationID] = useState('')
  const [enabled, setEnabled] = useState(true)
  const [saving, setSaving] = useState(false)

  useEffect(() => {
    if (!open) return
    setLineOAID(recipient?.line_oa_id || senders[0]?.id || '')
    setName(recipient?.name ?? '')
    setDestinationType(recipient?.destination_type ?? 'user')
    setDestinationID(recipient?.destination_id ?? '')
    setEnabled(recipient?.enabled ?? true)
  }, [open, recipient, senders])

  const submit = async () => {
    if (!lineOAID || !name.trim() || !destinationID.trim()) {
      toast.error('กรุณากรอก LINE OA, ชื่อผู้รับ และ destination ID')
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
      if (isEdit && recipient) {
        await client.put(`/api/settings/line-notifications/recipients/${recipient.id}`, body)
      } else {
        await client.post('/api/settings/line-notifications/recipients', body)
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
          <DialogTitle>{isEdit ? 'แก้ไขผู้รับแจ้งเตือน' : 'เพิ่มผู้รับแจ้งเตือน'}</DialogTitle>
          <DialogDescription>
            วันนี้กรอก LINE destination ID เองก่อน รอบถัดไปค่อยทำ auto capture จาก webhook
          </DialogDescription>
        </DialogHeader>
        <div className="space-y-4">
          <div className="space-y-1.5">
            <Label>LINE OA sender</Label>
            <Select value={lineOAID} onValueChange={setLineOAID}>
              <SelectTrigger>
                <SelectValue placeholder="เลือก LINE OA" />
              </SelectTrigger>
              <SelectContent>
                {senders.map((s) => (
                  <SelectItem key={s.id} value={s.id}>
                    {s.name}{s.enabled ? '' : ' (ปิดอยู่)'}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <div className="space-y-1.5">
            <Label>ชื่อผู้รับ</Label>
            <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="เช่น คุณบอส, ทีมคลัง, แอดมินกลาง" />
          </div>
          <div className="grid gap-3 sm:grid-cols-[160px_minmax(0,1fr)]">
            <div className="space-y-1.5">
              <Label>ประเภท</Label>
              <Select value={destinationType} onValueChange={(v: LineRecipient['destination_type']) => setDestinationType(v)}>
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="user">User ID</SelectItem>
                  <SelectItem value="group">Group ID</SelectItem>
                  <SelectItem value="room">Room ID</SelectItem>
                </SelectContent>
              </Select>
            </div>
            <div className="space-y-1.5">
              <Label>Destination ID</Label>
              <Input value={destinationID} onChange={(e) => setDestinationID(e.target.value)} placeholder="U..., C..., R..." className="font-mono text-xs" />
            </div>
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
