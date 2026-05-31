import { useEffect, useState } from 'react'
import { ChevronDown, ChevronRight, Loader2, Plus, StickyNote, Trash2 } from 'lucide-react'
import dayjs from 'dayjs'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import { Textarea } from '@/components/ui/textarea'
import client from '@/api/client'
import { cn } from '@/lib/utils'
import type { ChatNote } from './types'

interface Props {
  lineUserID: string
}

// NotesPanel — collapsible bar above the message list. Admin internal notes
// (Phase 4.8) NEVER reach LINE. Visible to all admin/staff users so they
// share context (no per-admin private notes in v1).
export function NotesPanel({ lineUserID }: Props) {
  const [notes, setNotes] = useState<ChatNote[]>([])
  const [loading, setLoading] = useState(false)
  const [open, setOpen] = useState(false)
  const [draft, setDraft] = useState('')
  const [posting, setPosting] = useState(false)

  const load = async () => {
    setLoading(true)
    try {
      const res = await client.get<{ data: ChatNote[] | null }>(
        `/api/admin/conversations/${encodeURIComponent(lineUserID)}/notes`,
      )
      setNotes(res.data.data ?? [])
    } catch {
      setNotes([])
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    load()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [lineUserID])

  const addNote = async () => {
    const body = draft.trim()
    if (!body || posting) return
    setPosting(true)
    try {
      await client.post(
        `/api/admin/conversations/${encodeURIComponent(lineUserID)}/notes`,
        { body },
      )
      setDraft('')
      await load()
    } catch (e: any) {
      toast.error('บันทึก note ไม่สำเร็จ: ' + (e?.message ?? 'unknown'))
    } finally {
      setPosting(false)
    }
  }

  const removeNote = async (id: string) => {
    try {
      await client.delete(
        `/api/admin/conversations/${encodeURIComponent(lineUserID)}/notes/${id}`,
      )
      await load()
    } catch (e: any) {
      toast.error('ลบ note ไม่สำเร็จ: ' + (e?.message ?? 'unknown'))
    }
  }

  // Hide entirely when there are no notes AND the panel is closed — nothing
  // to surface, save the screen real estate.
  if (!open && !loading && notes.length === 0) {
    return null
  }

  return (
    <div className="border-b border-border bg-warning/5">
      <Button
        variant="ghost"
        className="flex h-7 w-full items-center justify-between gap-1.5 rounded-none px-3 text-[11px]"
        onClick={() => setOpen((v) => !v)}
      >
        <span className="flex items-center gap-1.5 font-medium">
          {open ? <ChevronDown className="h-3 w-3" /> : <ChevronRight className="h-3 w-3" />}
          <StickyNote className="h-3 w-3" />
          Notes (admin only)
        </span>
        <span className="text-muted-foreground">
          {loading ? '…' : `${notes.length} รายการ`}
        </span>
      </Button>
      {open && (
        <div className="space-y-2 px-3 pb-2">
          {notes.length > 0 && (
            <ul className={cn('space-y-1')}>
              {notes.map((n) => (
                <li
                  key={n.id}
                  className="group relative rounded-md border border-warning/30 bg-card px-2 py-1.5 text-xs"
                >
                  <div className="whitespace-pre-wrap break-words pr-6">{n.body}</div>
                  <div className="mt-0.5 text-[10px] text-muted-foreground">
                    {dayjs(n.created_at).format('DD/MM/YY HH:mm')}
                  </div>
                  <Button
                    variant="ghost"
                    size="sm"
                    className="absolute right-0.5 top-0.5 h-6 w-6 p-0 text-destructive opacity-0 transition-opacity group-hover:opacity-100"
                    onClick={() => removeNote(n.id)}
                    title="ลบ note"
                  >
                    <Trash2 className="h-3 w-3" />
                  </Button>
                </li>
              ))}
            </ul>
          )}
          <div className="flex items-end gap-1.5">
            <Textarea
              value={draft}
              onChange={(e) => setDraft(e.target.value)}
              placeholder="เพิ่ม note (เช่น 'ขอเครดิต 30 วัน', 'ระวัง — เคยเบี้ยว')"
              rows={2}
              className="min-h-[40px] flex-1 resize-none text-xs"
            />
            <Button
              size="sm"
              className="h-8 gap-1 px-2.5 text-xs"
              onClick={addNote}
              disabled={!draft.trim() || posting}
            >
              {posting ? <Loader2 className="h-3 w-3 animate-spin" /> : <Plus className="h-3 w-3" />}
              เพิ่ม
            </Button>
          </div>
        </div>
      )}
    </div>
  )
}
