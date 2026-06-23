import { useEffect, useState, type ReactNode } from 'react'
import { Link } from 'react-router-dom'
import {
  CheckCircle2,
  ChevronDown,
  Copy,
  ExternalLink,
  HelpCircle,
  Pencil,
  Plus,
  RefreshCw,
  ShoppingBag,
  Trash2,
} from 'lucide-react'
import { toast } from 'sonner'

import client from '@/api/client'
import { ConfirmDialog } from '@/components/common/ConfirmDialog'
import { PageHeader } from '@/components/common/PageHeader'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import { Checkbox } from '@/components/ui/checkbox'
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from '@/components/ui/collapsible'
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from '@/components/ui/popover'
import { Switch } from '@/components/ui/switch'
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { cn } from '@/lib/utils'

const OA_PLUS_URL = 'https://oaplus.line.biz/'
const OA_PLUS_API_DOC_URL = 'https://www.line-website.com/oaplus-public-api-doc-client/'

interface LineMyShopConnection {
  id: string
  name: string
  channel_id?: number
  premium_id?: string
  random_id?: string
  enabled: boolean
  has_api_key: boolean
  has_webhook_secret: boolean
  webhook_url?: string
  last_sync_at?: string
  last_error?: string
  updated_at: string
}

interface FormState {
  name: string
  api_key: string
  webhook_secret: string
  clear_webhook_secret: boolean
  channel_id: string
  premium_id: string
  random_id: string
  enabled: boolean
}

const blankForm: FormState = {
  name: '',
  api_key: '',
  webhook_secret: '',
  clear_webhook_secret: false,
  channel_id: '',
  premium_id: '',
  random_id: '',
  enabled: true,
}

export default function LineMyShopSettings() {
  const [connections, setConnections] = useState<LineMyShopConnection[]>([])
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [syncingId, setSyncingId] = useState<string | null>(null)
  const [editing, setEditing] = useState<LineMyShopConnection | 'new' | null>(null)
  const [deleting, setDeleting] = useState<LineMyShopConnection | null>(null)
  const [form, setForm] = useState<FormState>(blankForm)

  const load = async () => {
    setLoading(true)
    try {
      const res = await client.get<{ data: LineMyShopConnection[] }>('/api/settings/line-myshop/connections')
      setConnections(res.data.data ?? [])
    } catch (e: any) {
      toast.error(e?.response?.data?.error ?? 'โหลดบัญชี LINE MyShop ไม่สำเร็จ')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    void load()
  }, [])

  const openCreate = () => {
    setForm(blankForm)
    setEditing('new')
  }

  const openEdit = (conn: LineMyShopConnection) => {
    setForm({
      name: conn.name,
      api_key: '',
      webhook_secret: '',
      clear_webhook_secret: false,
      channel_id: conn.channel_id ? String(conn.channel_id) : '',
      premium_id: conn.premium_id ?? '',
      random_id: conn.random_id ?? '',
      enabled: conn.enabled,
    })
    setEditing(conn)
  }

  const save = async () => {
    const name = form.name.trim()
    const apiKey = form.api_key.trim()
    const webhookSecret = form.clear_webhook_secret ? '' : form.webhook_secret.trim()
    const channelID = parseChannelID(form.channel_id)

    if (!name) {
      toast.error('กรุณากรอกชื่อบัญชี')
      return
    }
    if (editing === 'new' && !apiKey) {
      toast.error('กรุณากรอก API key')
      return
    }
    if (form.api_key && hasUnsafeSecretChars(form.api_key)) {
      toast.error('API key ต้องไม่มีช่องว่างหรืออักขระขึ้นบรรทัดใหม่', {
        description: 'กรุณาตรวจการ copy/paste จาก OA Plus แล้วลองอีกครั้ง',
      })
      return
    }
    if (!form.clear_webhook_secret && form.webhook_secret && hasUnsafeSecretChars(form.webhook_secret)) {
      toast.error('Webhook secret ต้องไม่มีช่องว่างหรืออักขระขึ้นบรรทัดใหม่', {
        description: 'กรุณาตรวจการ copy/paste จาก OA Plus แล้วลองอีกครั้ง',
      })
      return
    }
    if (channelID.error) {
      toast.error(channelID.error)
      return
    }

    const payload = {
      name,
      api_key: apiKey,
      webhook_secret: webhookSecret,
      clear_webhook_secret: editing !== 'new' ? form.clear_webhook_secret : false,
      channel_id: channelID.value,
      premium_id: form.premium_id.trim(),
      random_id: form.random_id.trim(),
      enabled: form.enabled,
    }

    setSaving(true)
    try {
      if (editing === 'new') {
        await client.post('/api/settings/line-myshop/connections', payload)
        toast.success('เพิ่มบัญชี LINE MyShop แล้ว')
      } else if (editing) {
        await client.put(`/api/settings/line-myshop/connections/${editing.id}`, payload)
        toast.success(payload.clear_webhook_secret ? 'บันทึกบัญชีและล้าง Webhook secret แล้ว' : 'บันทึกบัญชี LINE MyShop แล้ว')
      }
      setEditing(null)
      await load()
    } catch (e: any) {
      toast.error(e?.response?.data?.error ?? 'บันทึกบัญชี LINE MyShop ไม่สำเร็จ')
    } finally {
      setSaving(false)
    }
  }

  const deleteConnection = async () => {
    if (!deleting) return
    try {
      await client.delete(`/api/settings/line-myshop/connections/${deleting.id}`)
      toast.success('ลบบัญชี LINE MyShop แล้ว')
      setDeleting(null)
      await load()
    } catch (e: any) {
      toast.error(e?.response?.data?.error ?? 'ลบบัญชี LINE MyShop ไม่สำเร็จ')
    }
  }

  const syncConnection = async (conn: LineMyShopConnection) => {
    setSyncingId(conn.id)
    try {
      const res = await client.post<{ data: { orders_scanned?: number; bills_created?: number; bills_existing?: number; notifications_queued?: number; errors?: string[] } }>(
        `/api/settings/line-myshop/connections/${conn.id}/sync`,
        { lookback_hours: 48, per_page: 50, page_limit: 3 },
      )
      const data = res.data.data ?? {}
      const errors = data.errors?.length ?? 0
      toast.success('ซิงก์ LINE MyShop แล้ว', {
        description: `ออเดอร์ ${data.orders_scanned ?? 0}, บิลใหม่ ${data.bills_created ?? 0}, บิลเดิม ${data.bills_existing ?? 0}, แจ้งเตือน ${data.notifications_queued ?? 0}${errors ? `, error ${errors}` : ''}`,
      })
      await load()
    } catch (e: any) {
      toast.error(e?.response?.data?.detail ?? e?.response?.data?.error ?? 'ซิงก์ LINE MyShop ไม่สำเร็จ')
    } finally {
      setSyncingId(null)
    }
  }

  const copyWebhook = async (url?: string) => {
    if (!url) return
    await navigator.clipboard.writeText(url)
    toast.success('คัดลอก Webhook URL แล้ว')
  }

  const editingConnection = editing && editing !== 'new' ? editing : null

  return (
    <TooltipProvider delayDuration={120}>
      <div className="space-y-5">
        <PageHeader
          title="LINE MyShop"
          description="บัญชี OA Plus, API key และ Webhook URL สำหรับคำสั่งซื้อ LINE MyShop"
          actions={
            <div className="flex flex-wrap gap-2">
              <Button variant="outline" size="sm" className="gap-2" onClick={load} disabled={loading}>
                <RefreshCw className={cn('h-4 w-4', loading && 'animate-spin')} />
                รีเฟรช
              </Button>
              <Button size="sm" className="gap-2" onClick={openCreate}>
                <Plus className="h-4 w-4" />
                เพิ่มบัญชี
              </Button>
            </div>
          }
        />

        <SetupGuide />

        <div className="grid gap-3">
          {connections.map((conn) => (
            <Card key={conn.id}>
              <CardContent className="p-4">
                <div className="flex flex-col gap-3 lg:flex-row lg:items-start lg:justify-between">
                  <div className="min-w-0 space-y-2">
                    <div className="flex flex-wrap items-center gap-2">
                      <ShoppingBag className="h-4 w-4 text-muted-foreground" />
                      <h2 className="text-sm font-semibold">{conn.name}</h2>
                      <Badge variant={conn.enabled ? 'default' : 'secondary'}>
                        {conn.enabled ? 'เปิดใช้งาน' : 'ปิด'}
                      </Badge>
                      <Badge variant="outline">{conn.has_api_key ? 'API key พร้อม' : 'ยังไม่มี API key'}</Badge>
                      <Badge variant="outline">{conn.has_webhook_secret ? 'Webhook secret แยก' : 'ใช้ API key verify'}</Badge>
                    </div>
                    <div className="grid gap-1 text-xs text-muted-foreground sm:grid-cols-3">
                      <div>Channel ID: {conn.channel_id ?? '-'}</div>
                      <div>Premium ID: {conn.premium_id || '-'}</div>
                      <div>Random ID: {conn.random_id || '-'}</div>
                    </div>
                    <div className="space-y-1.5">
                      <div className="flex min-w-0 items-center gap-2 rounded-md border bg-muted/30 px-3 py-2 text-xs">
                        <code className="min-w-0 flex-1 truncate">{conn.webhook_url || '-'}</code>
                        <Button
                          variant="ghost"
                          size="sm"
                          className="h-7 w-7 p-0"
                          aria-label={`คัดลอก Webhook URL ของ ${conn.name}`}
                          onClick={() => copyWebhook(conn.webhook_url)}
                        >
                          <Copy className="h-3.5 w-3.5" />
                        </Button>
                      </div>
                      <p className="text-[11px] leading-snug text-muted-foreground">
                        Webhook URL นี้สร้างหลังบันทึกบัญชี ให้นำไปตั้งใน OA Plus Webhook URL ของบัญชีเดียวกัน ถ้า domain/ngrok เปลี่ยนต้องคัดลอกไปตั้งใหม่
                      </p>
                    </div>
                    {conn.last_error && <div className="text-xs text-destructive">{conn.last_error}</div>}
                  </div>
                  <div className="flex shrink-0 flex-wrap gap-2">
                    <Tooltip>
                      <TooltipTrigger asChild>
                        <span className="inline-flex">
                          <Button
                            variant="outline"
                            size="sm"
                            className="gap-2"
                            onClick={() => syncConnection(conn)}
                            disabled={!conn.enabled || syncingId === conn.id}
                          >
                            <RefreshCw className={cn('h-4 w-4', syncingId === conn.id && 'animate-spin')} />
                            ซิงก์ย้อนหลัง 48 ชม.
                          </Button>
                        </span>
                      </TooltipTrigger>
                      <TooltipContent className="max-w-xs text-xs leading-relaxed">
                        ดึงออเดอร์ย้อนหลังแบบจำกัดจาก LINE MyShop ใช้หลังเพิ่มบัญชีหรือเมื่อสงสัยว่า webhook ตกหล่น อาจสร้างบิลและคิวแจ้งเตือน LINE
                      </TooltipContent>
                    </Tooltip>
                    <Button variant="outline" size="sm" className="gap-2" onClick={() => openEdit(conn)}>
                      <Pencil className="h-4 w-4" />
                      แก้ไข
                    </Button>
                    <Button variant="outline" size="sm" className="gap-2 text-destructive hover:text-destructive" onClick={() => setDeleting(conn)}>
                      <Trash2 className="h-4 w-4" />
                      ลบ
                    </Button>
                  </div>
                </div>
              </CardContent>
            </Card>
          ))}

          {!loading && connections.length === 0 && (
            <Card>
              <CardContent className="flex flex-col gap-3 p-4 sm:flex-row sm:items-center sm:justify-between">
                <div>
                  <div className="text-sm font-medium">ยังไม่มีบัญชี LINE MyShop</div>
                  <div className="text-xs text-muted-foreground">เริ่มจากสร้าง API key ใน OA Plus แล้วเพิ่มบัญชีนี้เพื่อรับ webhook และสร้างบิลจากออเดอร์</div>
                </div>
                <Button size="sm" className="gap-2 self-start sm:self-center" onClick={openCreate}>
                  <Plus className="h-4 w-4" />
                  เพิ่มบัญชี
                </Button>
              </CardContent>
            </Card>
          )}
        </div>

        <Dialog open={!!editing} onOpenChange={(open) => !open && setEditing(null)}>
          <DialogContent className="max-h-[min(92vh,760px)] overflow-y-auto sm:max-w-2xl">
            <DialogHeader>
              <DialogTitle>{editing === 'new' ? 'เพิ่มบัญชี LINE MyShop' : 'แก้ไขบัญชี LINE MyShop'}</DialogTitle>
            </DialogHeader>
            <div className="grid gap-4">
              <FieldBlock
                htmlFor="myshop-name"
                label="ชื่อบัญชี"
                badge="ต้องกรอก"
                helpId="myshop-name-help"
                helpText="ตั้งชื่อให้ทีมจำได้ เช่น ชื่อร้านหรือ OA นี้ ใช้แสดงในรายการและแจ้งเตือนเท่านั้น"
                guideTitle="ชื่อบัญชี"
                guide={
                  <p>ชื่อนี้อยู่ใน Nexflow เพื่อช่วยแยกหลายบัญชี OA Plus / MyShop ไม่ได้ส่งไป LINE และไม่กระทบชื่อร้านจริงใน OA Plus</p>
                }
              >
                <Input
                  id="myshop-name"
                  aria-describedby="myshop-name-help"
                  value={form.name}
                  onChange={(e) => setForm((v) => ({ ...v, name: e.target.value }))}
                />
              </FieldBlock>

              <FieldBlock
                htmlFor="myshop-api-key"
                label="API key"
                badge={editing === 'new' ? 'ต้องกรอก' : 'เว้นว่างได้'}
                helpId="myshop-api-key-help"
                helpText={editing === 'new' ? 'เอาจาก OA Plus Admin แล้ว Nexflow จะใช้เป็น header X-API-KEY' : 'เว้นว่างเพื่อใช้ API key เดิม ถ้าจะเปลี่ยน key ให้กรอกค่าใหม่จาก OA Plus'}
                guideTitle="ไปเอา API key จากที่ไหน"
                guide={
                  <div className="space-y-2">
                    <p>สร้างใน OA Plus Admin: Settings → API keys → Generate แล้วนำค่าที่ได้มากรอกในช่องนี้</p>
                    <p>Nexflow ใช้ค่านี้เฉพาะฝั่ง backend เพื่อเรียก LINE SHOPPING API ผ่าน header <code>X-API-KEY</code></p>
                    <ExternalGuideLink href={OA_PLUS_URL}>เปิด OA Plus</ExternalGuideLink>
                  </div>
                }
              >
                <Input
                  id="myshop-api-key"
                  type="password"
                  aria-describedby="myshop-api-key-help"
                  value={form.api_key}
                  placeholder={editing === 'new' ? '' : 'เว้นว่างเพื่อใช้ค่าเดิม'}
                  onChange={(e) => setForm((v) => ({ ...v, api_key: e.target.value }))}
                />
              </FieldBlock>

              <FieldBlock
                htmlFor="myshop-webhook-secret"
                label="Webhook secret"
                badge="ไม่บังคับ"
                helpId="myshop-webhook-secret-help"
                helpText={webhookSecretHelpText(editingConnection)}
                guideTitle="Webhook secret ใช้ทำอะไร"
                guide={
                  <div className="space-y-2">
                    <p>ใช้ตรวจลายเซ็น webhook จาก header <code>x-myshop-signature</code> ก่อนรับ event เข้า Nexflow</p>
                    <p>ถ้า OA Plus ให้ token/secret แยก ให้ใส่ที่นี่ ถ้าไม่มี ให้เว้นว่างเพื่อให้ Nexflow ใช้ API key เป็น secret fallback</p>
                    <ExternalGuideLink href={OA_PLUS_API_DOC_URL}>เปิดเอกสาร API</ExternalGuideLink>
                  </div>
                }
              >
                <Input
                  id="myshop-webhook-secret"
                  type="password"
                  aria-describedby="myshop-webhook-secret-help"
                  value={form.webhook_secret}
                  disabled={form.clear_webhook_secret}
                  placeholder={webhookSecretPlaceholder(editingConnection)}
                  onChange={(e) => setForm((v) => ({ ...v, webhook_secret: e.target.value, clear_webhook_secret: false }))}
                />
                {editingConnection?.has_webhook_secret && (
                  <label className="flex items-start gap-2 rounded-md border bg-muted/30 px-3 py-2 text-xs leading-relaxed">
                    <Checkbox
                      id="myshop-clear-webhook-secret"
                      checked={form.clear_webhook_secret}
                      onCheckedChange={(checked) => setForm((v) => ({ ...v, clear_webhook_secret: checked === true, webhook_secret: checked === true ? '' : v.webhook_secret }))}
                    />
                    <span className="space-y-0.5">
                      <span className="block font-medium text-foreground">ล้าง Webhook secret แยก</span>
                      <span className="block text-muted-foreground">หลังบันทึก บัญชีนี้จะกลับไปใช้ API key สำหรับ verify signature</span>
                    </span>
                  </label>
                )}
              </FieldBlock>

              <div className="grid gap-3 sm:grid-cols-3">
                <FieldBlock
                  htmlFor="myshop-channel-id"
                  label="Channel ID"
                  badge="ไม่บังคับ"
                  helpId="myshop-channel-id-help"
                  helpText="ตัวเลขจากข้อมูลร้านหรือ webhook payload shop.channelId ใช้กันตั้งค่าซ้ำข้ามบัญชี"
                  guideTitle="Channel ID"
                  guide={<p>ถ้าเห็นค่า <code>shop.channelId</code> ใน webhook payload หรือหน้า shop/channel ใน OA Plus ให้กรอกเป็นตัวเลขจำนวนเต็มบวก ช่องนี้ช่วยระบุบัญชีและป้องกันซ้ำ</p>}
                >
                  <Input
                    id="myshop-channel-id"
                    inputMode="numeric"
                    pattern="[0-9]*"
                    aria-describedby="myshop-channel-id-help"
                    value={form.channel_id}
                    onChange={(e) => setForm((v) => ({ ...v, channel_id: e.target.value }))}
                  />
                </FieldBlock>

                <FieldBlock
                  htmlFor="myshop-premium-id"
                  label="Premium ID"
                  badge="ไม่บังคับ"
                  helpId="myshop-premium-id-help"
                  helpText="พบได้จาก webhook payload shop.premiumId เก็บเป็น metadata สำหรับตรวจสอบบัญชี"
                  guideTitle="Premium ID"
                  guide={<p>ใช้เมื่อบัญชีไม่มี Channel ID หรืออยากเก็บ metadata เพิ่ม ค่าอ้างอิงมาจาก <code>shop.premiumId</code> ใน payload ของ LINE MyShop</p>}
                >
                  <Input
                    id="myshop-premium-id"
                    aria-describedby="myshop-premium-id-help"
                    value={form.premium_id}
                    onChange={(e) => setForm((v) => ({ ...v, premium_id: e.target.value }))}
                  />
                </FieldBlock>

                <FieldBlock
                  htmlFor="myshop-random-id"
                  label="Random ID"
                  badge="ไม่บังคับ"
                  helpId="myshop-random-id-help"
                  helpText="พบได้จาก webhook payload shop.randomId ใช้เป็น metadata ช่วยแยกบัญชี"
                  guideTitle="Random ID"
                  guide={<p>ค่าอ้างอิงจาก <code>shop.randomId</code> ใน payload ใช้ช่วยเทียบบัญชีเมื่อยังไม่มี Channel ID หรือ Premium ID ที่ชัดเจน</p>}
                >
                  <Input
                    id="myshop-random-id"
                    aria-describedby="myshop-random-id-help"
                    value={form.random_id}
                    onChange={(e) => setForm((v) => ({ ...v, random_id: e.target.value }))}
                  />
                </FieldBlock>
              </div>

              <label className="flex items-center justify-between gap-3 rounded-md border px-3 py-2">
                <span className="space-y-0.5">
                  <span className="block text-sm font-medium">เปิดใช้งาน</span>
                  <span id="myshop-enabled-help" className="block text-xs text-muted-foreground">
                    ปิดเพื่อหยุดรับ webhook และ sync ของบัญชีนี้ชั่วคราว โดยไม่ลบบิลเดิม
                  </span>
                </span>
                <Switch
                  checked={form.enabled}
                  aria-describedby="myshop-enabled-help"
                  onCheckedChange={(enabled) => setForm((v) => ({ ...v, enabled }))}
                />
              </label>
            </div>
            <DialogFooter className="gap-2 sm:gap-2">
              <Button type="button" variant="outline" onClick={() => setEditing(null)} disabled={saving}>
                ยกเลิก
              </Button>
              <Button type="button" onClick={save} disabled={saving}>
                {saving ? 'กำลังบันทึก...' : 'บันทึก'}
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>

        <ConfirmDialog
          open={!!deleting}
          onOpenChange={(open) => !open && setDeleting(null)}
          title="ลบบัญชี LINE MyShop?"
          description={deleting ? `บัญชี ${deleting.name} และ snapshot/webhook events ที่ผูกกับบัญชีนี้จะถูกลบ` : undefined}
          confirmLabel="ลบ"
          variant="destructive"
          onConfirm={deleteConnection}
        />
      </div>
    </TooltipProvider>
  )
}

function SetupGuide() {
  const [open, setOpen] = useState(true)
  return (
    <Card>
      <Collapsible open={open} onOpenChange={setOpen}>
        <CollapsibleTrigger asChild>
          <button type="button" className="flex w-full items-center justify-between gap-3 px-4 py-3 text-left">
            <div>
              <div className="text-sm font-medium">คู่มือตั้งค่า LINE MyShop</div>
              <div className="text-xs text-muted-foreground">ทำครบ 3 ขั้นตอนเพื่อให้ webhook และ sync ทำงานถูกบัญชี</div>
            </div>
            <ChevronDown className={cn('h-4 w-4 shrink-0 text-muted-foreground transition-transform', open && 'rotate-180')} />
          </button>
        </CollapsibleTrigger>
        <CollapsibleContent>
          <CardContent className="grid gap-3 border-t p-4 text-sm md:grid-cols-3">
            <GuideStep title="1. สร้าง API key">
              ไปที่ OA Plus Admin → Settings → API keys → Generate แล้วนำมากรอกในบัญชีนี้
            </GuideStep>
            <GuideStep title="2. คัดลอก Webhook URL">
              บันทึกบัญชีใน Nexflow ก่อน แล้ว copy URL ที่สร้างจาก domain production ไปตั้งใน OA Plus
            </GuideStep>
            <GuideStep title="3. ตั้งค่าเส้นทาง SML">
              เปิด <Link to="/settings/channels" className="font-medium text-link hover:underline">ตั้งค่าช่องทาง</Link> แล้วกำหนด LINE MyShop / sale ก่อนส่งเข้า SML
            </GuideStep>
          </CardContent>
        </CollapsibleContent>
      </Collapsible>
    </Card>
  )
}

function GuideStep({ title, children }: { title: string; children: ReactNode }) {
  return (
    <div className="flex gap-2">
      <CheckCircle2 className="mt-0.5 h-4 w-4 shrink-0 text-success" />
      <div>
        <div className="font-medium">{title}</div>
        <div className="mt-0.5 text-xs leading-relaxed text-muted-foreground">{children}</div>
      </div>
    </div>
  )
}

function FieldBlock({
  htmlFor,
  label,
  badge,
  helpId,
  helpText,
  guideTitle,
  guide,
  children,
}: {
  htmlFor: string
  label: string
  badge: string
  helpId: string
  helpText: string
  guideTitle: string
  guide: ReactNode
  children: ReactNode
}) {
  return (
    <div className="grid min-w-0 gap-2">
      <div className="flex min-w-0 items-center gap-2">
        <Label htmlFor={htmlFor}>{label}</Label>
        <Badge variant="outline" className="h-5 rounded px-1.5 text-[10px] font-normal">
          {badge}
        </Badge>
        <GuidePopover label={label} title={guideTitle}>
          {guide}
        </GuidePopover>
      </div>
      {children}
      <p id={helpId} className="text-[11px] leading-snug text-muted-foreground">
        {helpText}
      </p>
    </div>
  )
}

function GuidePopover({ label, title, children }: { label: string; title: string; children: ReactNode }) {
  return (
    <Popover modal>
      <PopoverTrigger asChild>
        <Button
          type="button"
          variant="ghost"
          size="sm"
          className="h-7 w-7 p-0 text-muted-foreground"
          aria-label={`ดูวิธีหา ${label}`}
        >
          <HelpCircle className="h-3.5 w-3.5" />
        </Button>
      </PopoverTrigger>
      <PopoverContent
        align="start"
        className="w-[min(380px,calc(100vw-2rem))] overflow-y-auto text-xs leading-relaxed"
        style={{ maxHeight: 'min(70vh, var(--radix-popover-content-available-height, 70vh))' }}
      >
        <div className="space-y-2">
          <div className="text-sm font-semibold">{title}</div>
          <div className="text-muted-foreground">{children}</div>
        </div>
      </PopoverContent>
    </Popover>
  )
}

function ExternalGuideLink({ href, children }: { href: string; children: ReactNode }) {
  return (
    <a href={href} target="_blank" rel="noopener noreferrer" className="inline-flex items-center gap-1 font-medium text-link hover:underline">
      {children}
      <ExternalLink className="h-3 w-3" />
    </a>
  )
}

function webhookSecretHelpText(conn: LineMyShopConnection | null) {
  if (!conn) return 'เว้นว่างเพื่อใช้ API key verify signature ถ้า OA Plus ไม่มี secret แยก'
  if (conn.has_webhook_secret) return 'เว้นว่างเพื่อใช้ Webhook secret เดิม หรือเลือกล้าง secret เพื่อกลับไปใช้ API key verify'
  return 'บัญชีนี้ยังใช้ API key verify signature อยู่ กรอกเฉพาะเมื่อมี secret แยกจาก OA Plus'
}

function webhookSecretPlaceholder(conn: LineMyShopConnection | null) {
  if (conn?.has_webhook_secret) return 'เว้นว่างเพื่อใช้ค่าเดิม'
  return 'เว้นว่างเพื่อใช้ API key verify signature'
}

function hasUnsafeSecretChars(value: string) {
  return /[\s\x00-\x1F\x7F]/.test(value)
}

function parseChannelID(value: string): { value: number | null; error?: string } {
  const trimmed = value.trim()
  if (!trimmed) return { value: null }
  if (!/^[1-9]\d*$/.test(trimmed)) {
    return { value: null, error: 'Channel ID ต้องเป็นตัวเลขจำนวนเต็มบวกเท่านั้น' }
  }
  const parsed = Number(trimmed)
  if (!Number.isSafeInteger(parsed)) {
    return { value: null, error: 'Channel ID มีค่ามากเกินกว่าที่ browser จะส่งได้อย่างปลอดภัย' }
  }
  return { value: parsed }
}
