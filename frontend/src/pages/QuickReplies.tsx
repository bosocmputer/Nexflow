import { useEffect, useState } from 'react'
import { Plus, RefreshCw, Pencil, Trash2, MessageSquareQuote } from 'lucide-react'
import dayjs from 'dayjs'
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
import { Textarea } from '@/components/ui/textarea'
import { ConfirmDialog } from '@/components/common/ConfirmDialog'
import { DataTable } from '@/components/common/DataTable'
import { PageHeader } from '@/components/common/PageHeader'
import { EmptyState } from '@/components/common/EmptyState'
import client from '@/api/client'

interface QuickReply {
  id: string
  label: string
  body: string
  sort_order: number
  created_by?: string | null
  created_at: string
  updated_at: string
}

// /settings/quick-replies — admin manages canned response templates that
// appear in the Composer's 💬 popover (Phase 4.4). 4 seed rows ship via
// migration 015; this page lets admin extend / rename / reorder / delete.
export default function QuickReplies() {
  const [rows, setRows] = useState<QuickReply[]>([])
  const [loading, setLoading] = useState(true)
  const [editing, setEditing] = useState<QuickReply | null>(null)
  const [editOpen, setEditOpen] = useState(false)
  const [deleting, setDeleting] = useState<QuickReply | null>(null)

  const load = async () => {
    setLoading(true)
    try {
      const r = await client.get<{ data: QuickReply[] }>('/api/admin/quick-replies')
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
      await client.delete(`/api/admin/quick-replies/${deleting.id}`)
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
        title="Quick Replies (template ตอบลูกค้า)"
        description="template ที่ admin ใช้บ่อย — ปรากฏในปุ่ม 💬 ของ Composer ในหน้า /messages"
        actions={
          <>
            <Button variant="outline" size="sm" className="gap-1.5" onClick={load}>
              <RefreshCw className="h-3.5 w-3.5" />
              รีเฟรช
            </Button>
            <Button
              size="sm"
              className="gap-1.5"
              onClick={() => {
                setEditing(null)
                setEditOpen(true)
              }}
            >
              <Plus className="h-3.5 w-3.5" />
              เพิ่ม template
            </Button>
          </>
        }
      />

      {!loading && rows.length === 0 ? (
        <EmptyState
          icon={MessageSquareQuote}
          title="ยังไม่มี template"
          description="สร้าง template ที่ใช้ตอบลูกค้าบ่อย ๆ — ลด keystroke ตอนคุย"
          action={
            <Button onClick={() => setEditOpen(true)}>
              <Plus className="h-4 w-4" />
              สร้าง template แรก
            </Button>
          }
        />
      ) : (
        <DataTable<QuickReply>
          data={rows}
          loading={loading}
          empty="ยังไม่มี template"
          columns={[
            {
              key: 'sort_order',
              header: 'ลำดับ',
              cell: (r) => (
                <Badge variant="secondary" className="h-5 px-1.5 font-mono text-[10px]">
                  {r.sort_order}
                </Badge>
              ),
            },
            {
              key: 'label',
              header: 'ชื่อ template',
              cell: (r) => <span className="font-medium">{r.label}</span>,
            },
            {
              key: 'body',
              header: 'ข้อความ',
              cell: (r) => (
                <span className="line-clamp-2 max-w-md text-xs text-muted-foreground">
                  {r.body}
                </span>
              ),
            },
            {
              key: 'updated',
              header: 'แก้ไขล่าสุด',
              cell: (r) => (
                <span className="text-xs tabular-nums text-muted-foreground">
                  {dayjs(r.updated_at).format('DD/MM/YY HH:mm')}
                </span>
              ),
            },
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

      <QuickReplyDialog
        open={editOpen}
        onOpenChange={setEditOpen}
        quickReply={editing}
        onSaved={load}
      />

      <ConfirmDialog
        open={!!deleting}
        onOpenChange={(v) => !v && setDeleting(null)}
        title="ลบ template"
        description={deleting ? `ลบ "${deleting.label}"?` : ''}
        confirmLabel="ลบ"
        variant="destructive"
        onConfirm={handleDelete}
      />
    </div>
  )
}

// ── Edit dialog ──────────────────────────────────────────────────────────────

interface DialogProps {
  open: boolean
  onOpenChange: (v: boolean) => void
  quickReply: QuickReply | null
  onSaved: () => void
}

function QuickReplyDialog({ open, onOpenChange, quickReply, onSaved }: DialogProps) {
  const isEdit = !!quickReply
  const [label, setLabel] = useState('')
  const [body, setBody] = useState('')
  const [sortOrder, setSortOrder] = useState(50)
  const [saving, setSaving] = useState(false)

  useEffect(() => {
    if (!open) return
    if (quickReply) {
      setLabel(quickReply.label)
      setBody(quickReply.body)
      setSortOrder(quickReply.sort_order)
    } else {
      setLabel('')
      setBody('')
      setSortOrder(50)
    }
  }, [open, quickReply])

  const submit = async () => {
    if (!label.trim() || !body.trim()) {
      toast.error('กรุณากรอกชื่อ + ข้อความ')
      return
    }
    setSaving(true)
    try {
      const payload = {
        label: label.trim(),
        body: body.trim(),
        sort_order: Number(sortOrder) || 0,
      }
      if (isEdit && quickReply) {
        await client.put(`/api/admin/quick-replies/${quickReply.id}`, payload)
      } else {
        await client.post('/api/admin/quick-replies', payload)
      }
      toast.success(isEdit ? 'บันทึกสำเร็จ' : 'เพิ่ม template สำเร็จ')
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
      <DialogContent className="max-w-lg">
        <DialogHeader>
          <DialogTitle>{isEdit ? 'แก้ไข template' : 'เพิ่ม template'}</DialogTitle>
        </DialogHeader>

        <div className="space-y-3 py-2">
          <div className="space-y-1">
            <Label className="text-xs">ชื่อ (แสดงในปุ่ม 💬 ของ Composer)</Label>
            <Input
              value={label}
              onChange={(e) => setLabel(e.target.value)}
              placeholder="เช่น 'ทักทาย', 'แจ้งราคา'"
            />
          </div>
          <div className="space-y-1">
            <Label className="text-xs">ข้อความเต็ม</Label>
            <Textarea
              value={body}
              onChange={(e) => setBody(e.target.value)}
              placeholder="ข้อความที่จะ inject ลง textarea เมื่อ admin เลือก template นี้"
              rows={4}
              className="text-sm"
            />
            <p className="text-[11px] text-muted-foreground">
              💡 แนะนำใช้ <code>___</code> เป็น placeholder (เช่น "ราคารวม ___ บาทค่ะ") ให้ admin แก้ก่อนส่ง
            </p>
          </div>
          <div className="space-y-1">
            <Label className="text-xs">ลำดับ (sort_order)</Label>
            <Input
              type="number"
              value={sortOrder}
              onChange={(e) => setSortOrder(Number(e.target.value))}
              className="w-32 font-mono"
            />
            <p className="text-[11px] text-muted-foreground">
              เลขน้อยขึ้นก่อน — ใช้ 10/20/30 จะเหลือช่องว่างให้แทรก
            </p>
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
