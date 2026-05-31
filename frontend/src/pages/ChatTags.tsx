import { useEffect, useState } from 'react'
import { Plus, Tag, Pencil, Trash2 } from 'lucide-react'
import { toast } from 'sonner'

import { Badge } from '@/components/ui/badge'
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
import { ConfirmDialog } from '@/components/common/ConfirmDialog'
import { DataTable } from '@/components/common/DataTable'
import { PageHeader } from '@/components/common/PageHeader'
import { EmptyState } from '@/components/common/EmptyState'
import client from '@/api/client'
import { cn } from '@/lib/utils'

interface ChatTag {
  id: string
  label: string
  color: string
  created_at: string
}

const COLOR_OPTIONS = [
  { value: 'gray', label: 'เทา', cls: 'bg-muted text-muted-foreground' },
  { value: 'red', label: 'แดง', cls: 'bg-destructive/15 text-destructive' },
  { value: 'orange', label: 'ส้ม', cls: 'bg-warning/15 text-warning' },
  { value: 'yellow', label: 'เหลือง', cls: 'bg-yellow-200/40 text-yellow-700 dark:text-yellow-400' },
  { value: 'green', label: 'เขียว', cls: 'bg-success/15 text-success' },
  { value: 'blue', label: 'ฟ้า', cls: 'bg-info/15 text-info' },
  { value: 'purple', label: 'ม่วง', cls: 'bg-purple-200/40 text-purple-700 dark:text-purple-400' },
]

const colorClass = (color: string) =>
  COLOR_OPTIONS.find((c) => c.value === color)?.cls ?? COLOR_OPTIONS[0].cls

// /settings/chat-tags — admin manages global tag list (Phase 4.9). Tags are
// many-to-many with conversations; admins attach/detach in the chat header.
export default function ChatTags() {
  const [rows, setRows] = useState<ChatTag[]>([])
  const [loading, setLoading] = useState(true)
  const [editing, setEditing] = useState<ChatTag | null>(null)
  const [editOpen, setEditOpen] = useState(false)
  const [deleting, setDeleting] = useState<ChatTag | null>(null)

  const load = async () => {
    setLoading(true)
    try {
      const r = await client.get<{ data: ChatTag[] }>('/api/settings/chat-tags')
      setRows(r.data.data ?? [])
    } catch (e: any) {
      toast.error('โหลดไม่สำเร็จ: ' + (e?.message ?? 'unknown'))
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    load()
  }, [])

  const handleDelete = async () => {
    if (!deleting) return
    try {
      await client.delete(`/api/settings/chat-tags/${deleting.id}`)
      toast.success('ลบสำเร็จ')
      setDeleting(null)
      await load()
    } catch (e: any) {
      toast.error(e?.response?.data?.error ?? 'ลบไม่สำเร็จ')
    }
  }

  return (
    <div className="space-y-5">
      <PageHeader
        title="Chat Tags"
        description="Tag สำหรับจัดหมวดบทสนทนาในห้องแชท (VIP / ขายส่ง / spam ฯลฯ) — กรอง inbox ตาม tag ได้ที่ /messages"
        actions={
          <Button
            size="sm"
            className="gap-1.5"
            onClick={() => {
              setEditing(null)
              setEditOpen(true)
            }}
          >
            <Plus className="h-3.5 w-3.5" />
            เพิ่ม Tag
          </Button>
        }
      />

      {!loading && rows.length === 0 ? (
        <EmptyState
          icon={Tag}
          title="ยังไม่มี tag"
          description="สร้าง tag เพื่อจัดหมวดลูกค้า — เช่น VIP / ขายส่ง / spam"
          action={
            <Button onClick={() => setEditOpen(true)}>
              <Plus className="h-4 w-4" />
              สร้าง tag แรก
            </Button>
          }
        />
      ) : (
        <DataTable<ChatTag>
          data={rows}
          loading={loading}
          empty="ยังไม่มี tag"
          columns={[
            {
              key: 'preview',
              header: 'ดูตัวอย่าง',
              cell: (r) => (
                <Badge
                  variant="secondary"
                  className={cn('h-5 px-2 text-[11px] font-normal', colorClass(r.color))}
                >
                  {r.label}
                </Badge>
              ),
            },
            { key: 'label', header: 'ชื่อ', cell: (r) => <span className="font-medium">{r.label}</span> },
            { key: 'color', header: 'สี', cell: (r) => <span className="text-xs text-muted-foreground">{r.color}</span> },
            {
              key: 'actions',
              header: '',
              headerClassName: 'text-right',
              className: 'text-right',
              cell: (r) => (
                <div className="flex items-center justify-end gap-1">
                  <Button
                    variant="outline"
                    size="sm"
                    className="h-7 gap-1 px-2.5 text-xs"
                    onClick={() => {
                      setEditing(r)
                      setEditOpen(true)
                    }}
                  >
                    <Pencil className="h-3 w-3" />
                    แก้ไข
                  </Button>
                  <Button
                    variant="ghost"
                    size="sm"
                    className="h-7 px-2 text-destructive hover:text-destructive"
                    onClick={() => setDeleting(r)}
                  >
                    <Trash2 className="h-3.5 w-3.5" />
                  </Button>
                </div>
              ),
            },
          ]}
        />
      )}

      <ChatTagDialog
        open={editOpen}
        onOpenChange={setEditOpen}
        tag={editing}
        onSaved={load}
      />

      <ConfirmDialog
        open={!!deleting}
        onOpenChange={(v) => !v && setDeleting(null)}
        title="ลบ tag"
        description={
          deleting
            ? `ลบ "${deleting.label}"? — ห้องแชททั้งหมดที่ใช้ tag นี้จะถูก unattach อัตโนมัติ`
            : ''
        }
        confirmLabel="ลบ"
        variant="destructive"
        onConfirm={handleDelete}
      />
    </div>
  )
}

interface DialogProps {
  open: boolean
  onOpenChange: (v: boolean) => void
  tag: ChatTag | null
  onSaved: () => void
}

function ChatTagDialog({ open, onOpenChange, tag, onSaved }: DialogProps) {
  const isEdit = !!tag
  const [label, setLabel] = useState('')
  const [color, setColor] = useState('gray')
  const [saving, setSaving] = useState(false)

  useEffect(() => {
    if (!open) return
    if (tag) {
      setLabel(tag.label)
      setColor(tag.color)
    } else {
      setLabel('')
      setColor('gray')
    }
  }, [open, tag])

  const submit = async () => {
    if (!label.trim()) {
      toast.error('กรุณากรอกชื่อ tag')
      return
    }
    setSaving(true)
    try {
      const payload = { label: label.trim(), color }
      if (isEdit && tag) {
        await client.put(`/api/settings/chat-tags/${tag.id}`, payload)
      } else {
        await client.post('/api/settings/chat-tags', payload)
      }
      toast.success(isEdit ? 'บันทึกสำเร็จ' : 'เพิ่ม tag สำเร็จ')
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
      <DialogContent className="max-w-md">
        <DialogHeader>
          <DialogTitle>{isEdit ? 'แก้ไข Tag' : 'เพิ่ม Tag'}</DialogTitle>
        </DialogHeader>
        <div className="space-y-3 py-2">
          <div className="space-y-1">
            <Label className="text-xs">ชื่อ tag</Label>
            <Input
              value={label}
              onChange={(e) => setLabel(e.target.value)}
              placeholder="VIP / ขายส่ง / ติดตาม"
            />
          </div>
          <div className="space-y-1">
            <Label className="text-xs">สี</Label>
            <div className="grid grid-cols-7 gap-1.5">
              {COLOR_OPTIONS.map((opt) => (
                <button
                  key={opt.value}
                  type="button"
                  onClick={() => setColor(opt.value)}
                  className={cn(
                    'flex h-8 items-center justify-center rounded-md border text-[10px] transition-all',
                    opt.cls,
                    color === opt.value
                      ? 'border-primary ring-2 ring-primary/40'
                      : 'border-transparent hover:border-border',
                  )}
                  title={opt.label}
                >
                  Aa
                </button>
              ))}
            </div>
          </div>
          <div className="rounded-md border border-border bg-muted/30 px-3 py-2">
            <div className="text-[10px] uppercase tracking-wide text-muted-foreground">ดูตัวอย่าง</div>
            <Badge variant="secondary" className={cn('mt-1 h-5 px-2 text-[11px] font-normal', colorClass(color))}>
              {label || 'ตัวอย่าง'}
            </Badge>
          </div>
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={saving}>
            ยกเลิก
          </Button>
          <Button onClick={submit} disabled={saving}>
            {saving ? 'กำลังบันทึก…' : isEdit ? 'บันทึก' : 'เพิ่ม'}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
