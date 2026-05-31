import { useEffect, useLayoutEffect, useRef, useState, type ClipboardEvent, type KeyboardEvent } from 'react'
import { MessageSquare, Paperclip, Send, X } from 'lucide-react'

import { Button } from '@/components/ui/button'
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from '@/components/ui/popover'
import client from '@/api/client'
import { cn } from '@/lib/utils'

interface QuickReply {
  id: string
  label: string
  body: string
  sort_order: number
}

export interface PendingAttachment {
  id: string         // local-only client id (Date.now() + idx)
  file: File
  previewURL: string // object URL for thumbnail render — revoked on remove/send
}

interface Props {
  disabled?: boolean
  onSend: (text: string) => Promise<void>
  // Phase B: send each attached image. Composer manages preview state; parent
  // handles upload + DB insert + LINE Push, passing back a Promise so the
  // composer can show progress.
  onSendMedia?: (file: File) => Promise<void>
  // Optional external attachments queue — when MessageThread accepts dropped
  // files, it calls this to feed Composer.
  externalAttachments?: PendingAttachment[]
  onConsumeExternal?: () => void
}

// Composer — inline compact (Discord/LINE web style):
//   ┌─ attachment thumbnails (only when attached) ───────────────┐
//   │ [📷×] [📷×]                                                │
//   ├────────────────────────────────────────────────────────────┤
//   │ 📎 💬   พิมพ์ข้อความ…                                    ➤ │
//   └────────────────────────────────────────────────────────────┘
//
// Auto-grow textarea (1-6 rows). Toolbar + send button centered via
// flex items-center. Paste image: handled on textarea via clipboard event.
// Drag-drop: handled by MessageThread parent and pushed in via externalAttachments.
export function Composer({
  disabled,
  onSend,
  onSendMedia,
  externalAttachments,
  onConsumeExternal,
}: Props) {
  const [text, setText] = useState('')
  const [sending, setSending] = useState(false)
  const [attachments, setAttachments] = useState<PendingAttachment[]>([])
  const [quickReplies, setQuickReplies] = useState<QuickReply[]>([])
  const [qrOpen, setQrOpen] = useState(false)
  const textareaRef = useRef<HTMLTextAreaElement | null>(null)
  const fileInputRef = useRef<HTMLInputElement | null>(null)

  // Auto-grow the textarea to fit content, capped at ~6 rows.
  useLayoutEffect(() => {
    const el = textareaRef.current
    if (!el) return
    el.style.height = 'auto'
    const lineHeight = 20
    const maxHeight = lineHeight * 6 + 16
    el.style.height = `${Math.min(el.scrollHeight, maxHeight)}px`
  }, [text])

  // Lazy-fetch templates on first popover open.
  useEffect(() => {
    if (!qrOpen || quickReplies.length > 0) return
    client
      .get<{ data: QuickReply[] }>('/api/admin/quick-replies')
      .then((res) => setQuickReplies(res.data.data ?? []))
      .catch(() => setQuickReplies([]))
  }, [qrOpen, quickReplies.length])

  // Accept attachments dropped from MessageThread.
  useEffect(() => {
    if (!externalAttachments || externalAttachments.length === 0) return
    setAttachments((prev) => [...prev, ...externalAttachments])
    onConsumeExternal?.()
  }, [externalAttachments, onConsumeExternal])

  // Cleanup object URLs when component unmounts.
  useEffect(() => {
    return () => {
      attachments.forEach((a) => URL.revokeObjectURL(a.previewURL))
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  const addFiles = (files: FileList | File[]) => {
    const arr: PendingAttachment[] = []
    Array.from(files).forEach((f, i) => {
      if (!f.type.startsWith('image/')) return // LINE Push supports image only
      arr.push({
        id: `${Date.now()}-${i}`,
        file: f,
        previewURL: URL.createObjectURL(f),
      })
    })
    if (arr.length > 0) {
      setAttachments((prev) => [...prev, ...arr])
    }
  }

  const removeAttachment = (id: string) => {
    setAttachments((prev) => {
      const target = prev.find((a) => a.id === id)
      if (target) URL.revokeObjectURL(target.previewURL)
      return prev.filter((a) => a.id !== id)
    })
  }

  const handlePaste = (e: ClipboardEvent<HTMLTextAreaElement>) => {
    const items = e.clipboardData?.items
    if (!items) return
    const files: File[] = []
    for (const it of Array.from(items)) {
      if (it.kind === 'file') {
        const f = it.getAsFile()
        if (f) files.push(f)
      }
    }
    if (files.length > 0) {
      e.preventDefault()
      addFiles(files)
    }
  }

  const submit = async () => {
    const trimmed = text.trim()
    if ((!trimmed && attachments.length === 0) || sending) return
    setSending(true)
    try {
      // Send images first (each is its own LINE message), then text.
      // Order matches what customer sees in their LINE thread.
      if (onSendMedia) {
        for (const a of attachments) {
          await onSendMedia(a.file)
          URL.revokeObjectURL(a.previewURL)
        }
      }
      setAttachments([])
      if (trimmed) {
        await onSend(trimmed)
        setText('')
      }
    } finally {
      setSending(false)
    }
  }

  const handleKey = (e: KeyboardEvent<HTMLTextAreaElement>) => {
    if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) {
      e.preventDefault()
      void submit()
    }
  }

  const insertTemplate = (body: string) => {
    setText((prev) => (prev ? prev + '\n' + body : body))
    setQrOpen(false)
    setTimeout(() => textareaRef.current?.focus(), 50)
  }

  const canSend = (!!text.trim() || attachments.length > 0) && !sending && !disabled

  return (
    <div className="border-t border-border bg-card p-2">
      {/* Hidden file input controlled by 📎 button */}
      <input
        ref={fileInputRef}
        type="file"
        accept="image/*"
        multiple
        className="hidden"
        onChange={(e) => {
          if (e.target.files) addFiles(e.target.files)
          e.target.value = '' // allow re-picking the same file
        }}
      />

      {/* Attachment preview strip — count badge avoids "did the paste
          succeed?" uncertainty when admin pastes 5+ images at once and
          the strip overflows horizontally. */}
      {attachments.length > 0 && (
        <div className="mb-2">
          <div className="mb-1 flex items-center justify-between px-1 text-[11px] text-muted-foreground">
            <span>
              แนบไป <span className="font-medium text-foreground">{attachments.length}</span>{' '}
              {attachments.length === 1 ? 'ไฟล์' : 'ไฟล์'}
            </span>
            <button
              type="button"
              onClick={() => attachments.forEach((a) => removeAttachment(a.id))}
              className="text-[11px] text-muted-foreground hover:text-foreground hover:underline"
            >
              ล้างทั้งหมด
            </button>
          </div>
          <div className="flex gap-2 overflow-x-auto pb-1">
            {attachments.map((a) => (
              <div
                key={a.id}
                className="group relative h-16 w-16 shrink-0 overflow-hidden rounded-md border border-border bg-background"
                title={a.file.name}
              >
                <img src={a.previewURL} alt={a.file.name} className="h-full w-full object-cover" />
                <button
                  type="button"
                  onClick={() => removeAttachment(a.id)}
                  className="absolute right-0.5 top-0.5 rounded-full bg-black/60 p-0.5 text-white hover:bg-black/80"
                  title="ลบ"
                >
                  <X className="h-3 w-3" />
                </button>
              </div>
            ))}
          </div>
        </div>
      )}

      <div
        className={cn(
          'flex items-end gap-1 rounded-2xl border border-border bg-background px-2 py-1.5 transition-colors',
          'focus-within:border-primary/40 focus-within:ring-1 focus-within:ring-primary/20',
          // When the parent disables the composer (archived chat), make the
          // disabled state actually look disabled — opacity + striped tint
          // + cursor. The banner above explains why; this makes "you can't
          // type here" visually unambiguous.
          disabled &&
            'pointer-events-none cursor-not-allowed border-dashed bg-muted/30 opacity-60',
        )}
      >
        <div className="flex shrink-0 items-center gap-0.5 self-end pb-0.5">
          <Button
            type="button"
            variant="ghost"
            size="sm"
            className="h-8 w-8 p-0"
            onClick={() => fileInputRef.current?.click()}
            disabled={disabled || sending || !onSendMedia}
            title={onSendMedia ? 'แนบรูปภาพ (รองรับเฉพาะรูป — LINE จำกัด)' : 'PUBLIC_BASE_URL ยังไม่ตั้ง — ส่งรูปไม่ได้'}
          >
            <Paperclip className="h-4 w-4" />
          </Button>

          <Popover open={qrOpen} onOpenChange={setQrOpen}>
            <PopoverTrigger asChild>
              <Button
                type="button"
                variant="ghost"
                size="sm"
                className="h-8 w-8 p-0"
                title="ใช้ template (quick reply)"
              >
                <MessageSquare className="h-4 w-4" />
              </Button>
            </PopoverTrigger>
            <PopoverContent className="w-[min(18rem,calc(100vw-2rem))] p-0" align="start" side="top">
              {quickReplies.length === 0 ? (
                <div className="p-3 text-xs text-muted-foreground">
                  ยังไม่มี template — เพิ่มใน{' '}
                  <a className="text-primary underline" href="/settings/quick-replies">
                    /settings/quick-replies
                  </a>
                </div>
              ) : (
                <ul
                  className="divide-y divide-border overflow-y-auto"
                  style={{ maxHeight: 'min(18rem, var(--radix-popover-content-available-height, 18rem))' }}
                >
                  {quickReplies.map((q) => (
                    <li key={q.id}>
                      <button
                        type="button"
                        onClick={() => insertTemplate(q.body)}
                        className="flex w-full flex-col items-start gap-0.5 px-3 py-2 text-left text-xs hover:bg-accent/40"
                      >
                        <span className="font-medium">{q.label}</span>
                        <span className="line-clamp-2 text-muted-foreground">{q.body}</span>
                      </button>
                    </li>
                  ))}
                </ul>
              )}
            </PopoverContent>
          </Popover>
        </div>

        <textarea
          ref={textareaRef}
          value={text}
          onChange={(e) => setText(e.target.value)}
          onKeyDown={handleKey}
          onPaste={handlePaste}
          disabled={disabled || sending}
          placeholder="พิมพ์ข้อความ… (Cmd/Ctrl+Enter เพื่อส่ง · paste/drag รูปได้)"
          rows={1}
          className={cn(
            'flex-1 resize-none border-0 bg-transparent px-2 py-2 text-sm leading-5',
            'placeholder:text-muted-foreground focus:outline-none',
            'min-h-[36px]',
          )}
        />

        <Button
          type="button"
          onClick={submit}
          disabled={!canSend}
          size="sm"
          className={cn(
            'shrink-0 self-end pb-0.5 transition-all',
            canSend ? 'h-8 gap-1 px-3' : 'h-8 w-8 p-0',
          )}
        >
          <Send className="h-3.5 w-3.5" />
          {canSend && <span className="text-xs">{sending ? 'กำลังส่ง…' : 'ส่ง'}</span>}
        </Button>
      </div>
    </div>
  )
}
