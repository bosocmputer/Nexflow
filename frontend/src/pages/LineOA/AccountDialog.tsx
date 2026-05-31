import { useEffect, useState } from 'react'
import { Eye, EyeOff } from 'lucide-react'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import client from '@/api/client'

export interface LineOAAccount {
  id: string
  name: string
  channel_secret?: string
  channel_access_token?: string
  bot_user_id: string
  admin_user_id: string
  greeting: string
  enabled: boolean
  mark_as_read_enabled?: boolean
  created_at: string
  updated_at: string
}

interface Props {
  open: boolean
  onOpenChange: (v: boolean) => void
  account: LineOAAccount | null
  onSaved: () => void
}

// Add/edit dialog for one LINE OA. On edit we pre-fill the secret + token
// from a follow-up GET (list endpoint masks them for safety).
export function LineOADialog({ open, onOpenChange, account, onSaved }: Props) {
  const isEdit = !!account
  const [name, setName] = useState('')
  const [secret, setSecret] = useState('')
  const [token, setToken] = useState('')
  const [adminUserID, setAdminUserID] = useState('')
  const [greeting, setGreeting] = useState('')
  const [enabled, setEnabled] = useState(true)
  const [markAsRead, setMarkAsRead] = useState(false)
  const [showSecret, setShowSecret] = useState(false)
  const [saving, setSaving] = useState(false)

  useEffect(() => {
    if (!open) return
    if (account) {
      setName(account.name)
      setAdminUserID(account.admin_user_id || '')
      setGreeting(account.greeting || '')
      setEnabled(account.enabled)
      setMarkAsRead(account.mark_as_read_enabled ?? false)
      // Fetch full account (with credentials) to pre-fill the dialog.
      client
        .get<LineOAAccount>(`/api/settings/line-oa/${account.id}`)
        .then((res) => {
          setSecret(res.data.channel_secret || '')
          setToken(res.data.channel_access_token || '')
        })
        .catch(() => {
          /* keep blank — admin can re-enter */
        })
    } else {
      setName('')
      setSecret('')
      setToken('')
      setAdminUserID('')
      setGreeting('')
      setEnabled(true)
      setMarkAsRead(false)
    }
    setShowSecret(false)
  }, [open, account])

  const submit = async () => {
    if (!name.trim() || (!isEdit && (!secret.trim() || !token.trim()))) {
      toast.error('กรุณากรอก ชื่อ + Channel Secret + Access Token')
      return
    }
    setSaving(true)
    try {
      const body = {
        name: name.trim(),
        // On edit, empty values mean "keep existing". On create, both required.
        channel_secret: secret.trim(),
        channel_access_token: token.trim(),
        admin_user_id: adminUserID.trim(),
        greeting: greeting.trim(),
        enabled,
        mark_as_read_enabled: markAsRead,
      }
      if (isEdit && account) {
        await client.put(`/api/settings/line-oa/${account.id}`, body)
      } else {
        await client.post('/api/settings/line-oa', body)
      }
      toast.success(isEdit ? 'บันทึกสำเร็จ' : 'เพิ่ม LINE OA สำเร็จ')
      onSaved()
      onOpenChange(false)
    } catch (e: any) {
      toast.error('บันทึกไม่สำเร็จ: ' + (e?.response?.data?.error ?? e?.message ?? 'unknown'))
    } finally {
      setSaving(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="grid max-h-[90vh] max-w-lg grid-rows-[auto_minmax(0,1fr)_auto]">
        <DialogHeader>
          <DialogTitle>{isEdit ? 'แก้ไข LINE OA' : 'เพิ่ม LINE OA'}</DialogTitle>
        </DialogHeader>

        <div className="-mx-6 space-y-4 overflow-y-auto px-6 py-2">
          <div className="space-y-1">
            <Label className="text-xs">ชื่อ (admin label)</Label>
            <Input
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="เช่น ร้านสาขา A"
            />
          </div>

          <div className="space-y-1">
            <Label className="text-xs">Channel Secret</Label>
            <div className="flex gap-2">
              <Input
                type={showSecret ? 'text' : 'password'}
                value={secret}
                onChange={(e) => setSecret(e.target.value)}
                placeholder={isEdit ? '(ไม่ต้องกรอกถ้าไม่เปลี่ยน)' : 'จาก LINE Developer Console'}
                className="font-mono text-xs"
              />
              <Button
                type="button"
                variant="outline"
                size="sm"
                onClick={() => setShowSecret((v) => !v)}
              >
                {showSecret ? <EyeOff className="h-3.5 w-3.5" /> : <Eye className="h-3.5 w-3.5" />}
              </Button>
            </div>
          </div>

          <div className="space-y-1">
            <Label className="text-xs">Channel Access Token (long-lived)</Label>
            <Textarea
              value={token}
              onChange={(e) => setToken(e.target.value)}
              placeholder={isEdit ? '(ไม่ต้องกรอกถ้าไม่เปลี่ยน)' : 'จาก LINE Developer Console → Messaging API → Issue token'}
              className="min-h-[60px] resize-none font-mono text-[11px]"
            />
          </div>

          <div className="space-y-1">
            <Label className="text-xs">Admin LINE userID (optional)</Label>
            <Input
              value={adminUserID}
              onChange={(e) => setAdminUserID(e.target.value)}
              placeholder="Uxxxxxxxxxxxx — ใช้ส่ง error notification (เลือกได้)"
              className="font-mono text-xs"
            />
          </div>

          <div className="space-y-1">
            <Label className="text-xs">Greeting (auto-reply ครั้งแรกที่ลูกค้าทักมา)</Label>
            <Textarea
              value={greeting}
              onChange={(e) => setGreeting(e.target.value)}
              placeholder="เช่น 'ขอบคุณค่ะ ทางร้านจะติดต่อกลับเร็ว ๆ นี้นะคะ 🙏' (เว้นว่าง = ไม่ตอบอัตโนมัติ)"
              className="min-h-[60px] resize-none text-sm"
            />
          </div>

          <div className="flex items-center gap-2">
            <input
              type="checkbox"
              id="oa-enabled"
              checked={enabled}
              onChange={(e) => setEnabled(e.target.checked)}
              className="h-4 w-4"
            />
            <Label htmlFor="oa-enabled" className="cursor-pointer text-sm font-normal">
              เปิดใช้งาน (รับ webhook + ส่ง reply)
            </Label>
          </div>

          <div className="flex items-start gap-2">
            <input
              type="checkbox"
              id="oa-mark-as-read"
              checked={markAsRead}
              onChange={(e) => setMarkAsRead(e.target.checked)}
              className="mt-0.5 h-4 w-4"
            />
            <div className="flex-1">
              <Label htmlFor="oa-mark-as-read" className="cursor-pointer text-sm font-normal">
                ส่ง read receipt ให้ลูกค้า (อ่านแล้ว ✓✓)
              </Label>
              <p className="mt-0.5 text-[11px] text-muted-foreground">
                ⚠ ใช้ได้เฉพาะ <strong>OA Plus (พรีเมียม)</strong> เท่านั้น —
                บัญชีฟรีจะ silently fail ทุกครั้งที่ admin เปิดห้อง.
                เปิดเมื่อ OA นี้อัปเกรดแล้ว
              </p>
            </div>
          </div>

          {!isEdit && (
            <div className="rounded-md border border-info/30 bg-info/5 p-3 text-xs text-info">
              💡 หลังกด "บันทึก" → ระบบจะคืนค่า ID ให้คัดลอก URL{' '}
              <code className="rounded bg-background px-1 py-0.5">
                /webhook/line/&lt;ID&gt;
              </code>{' '}
              ไปวางใน LINE Developer Console → Messaging API → Webhook URL
            </div>
          )}
        </div>

        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={saving}>
            ยกเลิก
          </Button>
          <Button onClick={submit} disabled={saving}>
            {saving ? 'กำลังบันทึก…' : isEdit ? 'บันทึก' : 'เพิ่ม LINE OA'}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
