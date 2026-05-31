import { useEffect, useState } from 'react'
import { Plus, RefreshCw, Pencil, Trash2, MessageSquare, Copy, Check } from 'lucide-react'
import dayjs from 'dayjs'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { ConfirmDialog } from '@/components/common/ConfirmDialog'
import { DataTable } from '@/components/common/DataTable'
import { PageHeader } from '@/components/common/PageHeader'
import { EmptyState } from '@/components/common/EmptyState'
import client from '@/api/client'
import { LineOADialog, type LineOAAccount } from './LineOA/AccountDialog'

// /settings/line-oa — admin manages multiple LINE OAs.
// Each row: name, bot_user_id (auto-fetched), enabled toggle, webhook URL
// (admin copies into LINE Developer Console).
//
// Multi-OA goal: "ร้านมี 5 LINE OA ก็รวม chat มาที่ Nexflow ที่เดียว".
export default function LineOA() {
  const [rows, setRows] = useState<LineOAAccount[]>([])
  const [loading, setLoading] = useState(true)
  const [editOpen, setEditOpen] = useState(false)
  const [editing, setEditing] = useState<LineOAAccount | null>(null)
  const [deleting, setDeleting] = useState<LineOAAccount | null>(null)
  const [copiedId, setCopiedId] = useState<string>('')

  const load = async () => {
    setLoading(true)
    try {
      const r = await client.get<{ data: LineOAAccount[] }>('/api/settings/line-oa')
      setRows(r.data.data ?? [])
    } catch (e: any) {
      toast.error('โหลดข้อมูลไม่สำเร็จ: ' + (e?.message ?? 'unknown'))
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
      await client.delete(`/api/settings/line-oa/${deleting.id}`)
      toast.success('ลบสำเร็จ')
      setDeleting(null)
      await load()
    } catch (e: any) {
      toast.error(e?.response?.data?.error ?? 'ลบไม่สำเร็จ')
    }
  }

  const handleTest = async (a: LineOAAccount) => {
    const id = toast.loading('ทดสอบเชื่อมต่อ LINE…')
    try {
      const res = await client.post<{
        ok: boolean
        bot_user_id: string
        display_name: string
        basic_id: string
      }>(`/api/settings/line-oa/${a.id}/test`)
      toast.success(
        `เชื่อมต่อสำเร็จ — ${res.data.display_name} (${res.data.basic_id || res.data.bot_user_id.slice(0, 10)})`,
        { id },
      )
      await load()
    } catch (e: any) {
      toast.error('ทดสอบไม่สำเร็จ: ' + (e?.response?.data?.error ?? e?.message ?? 'unknown'), { id })
    }
  }

  const webhookURL = (id: string) => `${window.location.origin}/webhook/line/${id}`

  const copyWebhook = async (a: LineOAAccount) => {
    try {
      await navigator.clipboard.writeText(webhookURL(a.id))
      setCopiedId(a.id)
      toast.success('คัดลอก URL แล้ว')
      setTimeout(() => setCopiedId(''), 2000)
    } catch {
      toast.error('คัดลอกไม่สำเร็จ — ใช้คลิกขวา → Copy')
    }
  }

  return (
    <div className="space-y-5">
      <PageHeader
        title="LINE OA Accounts"
        description="หลาย LINE OA → รวม inbox มาที่ Nexflow ที่เดียว (เช่น ร้านสาขาแต่ละสาขา)"
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
              เพิ่ม LINE OA
            </Button>
          </>
        }
      />

      {!loading && rows.length === 0 ? (
        <EmptyState
          icon={MessageSquare}
          title="ยังไม่มี LINE OA"
          description="เพิ่ม LINE OA เพื่อเริ่มรับข้อความจากลูกค้าใน Nexflow"
          action={
            <Button onClick={() => setEditOpen(true)}>
              <Plus className="h-4 w-4" />
              เพิ่ม LINE OA แรก
            </Button>
          }
        />
      ) : (
        <DataTable<LineOAAccount>
          data={rows}
          loading={loading}
          empty="ยังไม่มี LINE OA"
          columns={[
            {
              key: 'name',
              header: 'ชื่อ',
              cell: (r) => (
                <div className="flex flex-col">
                  <span className="font-medium">{r.name}</span>
                  {r.bot_user_id && (
                    <span className="font-mono text-[10px] text-muted-foreground">
                      bot: {r.bot_user_id.slice(0, 14)}…
                    </span>
                  )}
                </div>
              ),
            },
            {
              key: 'webhook',
              header: 'Webhook URL',
              cell: (r) => (
                <div className="flex items-center gap-1.5">
                  <code className="rounded bg-muted px-1.5 py-0.5 font-mono text-[10px]">
                    /webhook/line/{r.id.slice(0, 8)}…
                  </code>
                  <Button
                    variant="ghost"
                    size="sm"
                    className="h-6 px-1.5"
                    onClick={() => copyWebhook(r)}
                    title="คัดลอก URL เต็มไปวางใน LINE Developer Console"
                  >
                    {copiedId === r.id ? (
                      <Check className="h-3 w-3 text-success" />
                    ) : (
                      <Copy className="h-3 w-3" />
                    )}
                  </Button>
                </div>
              ),
            },
            {
              key: 'enabled',
              header: 'สถานะ',
              cell: (r) =>
                r.enabled ? (
                  <Badge variant="secondary" className="bg-success/15 text-success">
                    เปิดใช้งาน
                  </Badge>
                ) : (
                  <Badge variant="secondary" className="bg-muted text-muted-foreground">
                    ปิด
                  </Badge>
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
                    className="h-7 px-2 text-xs"
                    onClick={() => handleTest(r)}
                    title="ทดสอบ token + ดึง bot info จาก LINE"
                  >
                    Test
                  </Button>
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

      <LineOADialog
        open={editOpen}
        onOpenChange={setEditOpen}
        account={editing}
        onSaved={load}
      />

      <ConfirmDialog
        open={!!deleting}
        onOpenChange={(v) => !v && setDeleting(null)}
        title="ลบ LINE OA"
        description={
          deleting
            ? `ลบ "${deleting.name}"? — ถ้ามีบทสนทนาผูกอยู่จะลบไม่สำเร็จ ต้องลบห้องก่อน`
            : ''
        }
        confirmLabel="ลบ"
        variant="destructive"
        onConfirm={handleDelete}
      />
    </div>
  )
}
