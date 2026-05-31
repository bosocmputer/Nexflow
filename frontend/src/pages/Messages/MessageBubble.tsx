import { useState } from 'react'
import { AlertCircle, FileIcon, Phone, Sparkles } from 'lucide-react'
import dayjs from 'dayjs'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import client from '@/api/client'
import { cn } from '@/lib/utils'
import type { ChatMessage } from './types'

interface Props {
  message: ChatMessage
  onExtract?: (messageId: string, kind: string) => void
  // Phase 4.7: when a phone number is detected in incoming text, the parent
  // owns the conversation row + can refresh after save.
  onPhoneSaved?: (phone: string) => void
}

// Thai phone number — accepts 9-10 digit blocks with optional space/dash
// separators. Examples it'll match: "0812345678", "081-234-5678",
// "081 234 5678", "+66812345678"
const PHONE_RE = /(\+?\d{1,3}[\s-]?)?\d{2,3}[\s-]?\d{3,4}[\s-]?\d{4}/

// MessageBubble renders one chat row.
// - text: rounded bubble, gray for incoming / primary-tinted for outgoing
// - image: <img> via /media endpoint, click to expand
// - file: file-icon + filename + download link
// - audio: <audio controls>
// - system: muted centered note (e.g. "📄 สร้างบิลขายแล้ว")
// - failed delivery: ⚠ icon + error tooltip
// All media rows ALSO get a "🔍 สร้างบิลจากสื่อนี้" button that triggers AI extract.
export function MessageBubble({ message, onExtract, onPhoneSaved }: Props) {
  const [imgExpanded, setImgExpanded] = useState(false)
  const [savingPhone, setSavingPhone] = useState(false)
  const [phoneSaved, setPhoneSaved] = useState(false)

  // Detect phone in incoming text bubbles — outgoing/system don't get the
  // "save phone" affordance (admin replies obviously aren't customer phones).
  const detectedPhone =
    message.direction === 'incoming' && message.kind === 'text'
      ? message.text_content.match(PHONE_RE)?.[0]
      : null

  const savePhone = async () => {
    if (!detectedPhone || savingPhone) return
    setSavingPhone(true)
    try {
      await client.patch(
        `/api/admin/conversations/${encodeURIComponent(message.line_user_id)}/phone`,
        { phone: detectedPhone },
      )
      setPhoneSaved(true)
      onPhoneSaved?.(detectedPhone)
      toast.success(`บันทึกเบอร์ ${detectedPhone} แล้ว`)
    } catch (e: any) {
      toast.error('บันทึกเบอร์ไม่สำเร็จ: ' + (e?.message ?? 'unknown'))
    } finally {
      setSavingPhone(false)
    }
  }

  const isOutgoing = message.direction === 'outgoing'
  const isSystem = message.direction === 'system' || message.kind === 'system'

  if (isSystem) {
    return (
      <div className="my-2 flex justify-center">
        <div className="rounded-full bg-muted/60 px-3 py-1 text-[11px] text-muted-foreground">
          {message.text_content}
        </div>
      </div>
    )
  }

  // Optimistic outgoing messages from MessageThread use a blob: URL (local
  // preview) until the upload completes. After upload they'll be replaced
  // with the real persisted message which uses the API URL pattern.
  const localPreview = (message as ChatMessage & { _localPreviewURL?: string })._localPreviewURL
  const mediaURL = localPreview
    ? localPreview
    : message.media
      ? `/api/admin/conversations/${encodeURIComponent(message.line_user_id)}/messages/${message.id}/media`
      : null

  return (
    <div className={cn('flex w-full gap-2', isOutgoing ? 'justify-end' : 'justify-start')}>
      <div className={cn('flex max-w-[75%] flex-col gap-1', isOutgoing && 'items-end')}>
        {/* Body */}
        {message.kind === 'text' && (
          <div
            className={cn(
              'whitespace-pre-wrap break-words rounded-2xl px-3 py-1.5 text-sm',
              isOutgoing
                ? 'bg-primary text-primary-foreground rounded-br-sm'
                : 'bg-accent text-foreground rounded-bl-sm',
            )}
          >
            {message.text_content}
          </div>
        )}

        {message.kind === 'image' && mediaURL && (
          <div className="overflow-hidden rounded-lg border border-border">
            <img
              src={mediaURL}
              alt={message.media?.filename ?? 'image'}
              className={cn(
                'cursor-pointer object-contain',
                imgExpanded ? 'max-h-[80vh] max-w-full' : 'max-h-60 max-w-xs',
              )}
              onClick={() => setImgExpanded((v) => !v)}
            />
          </div>
        )}

        {message.kind === 'file' && mediaURL && message.media && (
          <a
            href={mediaURL}
            target="_blank"
            rel="noreferrer"
            className="flex items-center gap-2 rounded-lg border border-border bg-card px-3 py-2 text-sm hover:bg-accent/40"
          >
            <FileIcon className="h-5 w-5 text-muted-foreground" />
            <div className="min-w-0">
              <div className="truncate font-medium">{message.media.filename}</div>
              <div className="text-[10px] text-muted-foreground">
                {(message.media.size_bytes / 1024).toFixed(0)} KB · {message.media.content_type}
              </div>
            </div>
          </a>
        )}

        {message.kind === 'audio' && mediaURL && (
          <audio controls src={mediaURL} className="h-10">
            ระบบไม่รองรับการเล่นเสียง
          </audio>
        )}

        {/* Phase 4.7 — phone detected in incoming text → "บันทึกเบอร์" button */}
        {detectedPhone && !phoneSaved && (
          <Button
            variant="outline"
            size="sm"
            className="h-6 gap-1 px-2 text-[10px]"
            onClick={savePhone}
            disabled={savingPhone}
          >
            <Phone className="h-3 w-3" />
            บันทึกเบอร์ {detectedPhone}
          </Button>
        )}

        {/* Manual AI extract trigger for media */}
        {(message.kind === 'image' || message.kind === 'file' || message.kind === 'audio') &&
          !isOutgoing &&
          onExtract && (
            <Button
              variant="outline"
              size="sm"
              className="h-7 gap-1 px-2 text-[11px]"
              onClick={() => onExtract(message.id, message.kind)}
            >
              <Sparkles className="h-3 w-3" />
              สร้างบิลจากสื่อนี้
            </Button>
          )}

        {/* Footer: timestamp + delivery status + reply/push badge */}
        <div className="flex items-center gap-1.5 px-1 text-[10px] text-muted-foreground">
          <span>{dayjs(message.created_at).format('HH:mm')}</span>
          {isOutgoing && message.delivery_status === 'sent' && message.delivery_method === 'reply' && (
            <span
              className="text-success"
              title="ส่งผ่าน Reply API — ฟรี ไม่นับ quota (ใช้ภายในเวลาจำกัดหลังลูกค้าทัก)"
            >
              ฟรี
            </span>
          )}
          {isOutgoing && message.delivery_status === 'sent' && message.delivery_method === 'push' && (
            <span
              className="text-muted-foreground/70"
              title="ส่งผ่าน Push API — นับ quota เดือนนี้ (ฟรี OA = 200/เดือน)"
            >
              Push
            </span>
          )}
          {isOutgoing && message.delivery_status === 'failed' && (
            <span
              className="flex items-center gap-0.5 text-destructive"
              title={message.delivery_error || 'ส่งไม่สำเร็จ'}
            >
              <AlertCircle className="h-3 w-3" />
              ส่งไม่สำเร็จ
            </span>
          )}
          {isOutgoing && message.delivery_status === 'pending' && <span>กำลังส่ง…</span>}
        </div>
      </div>
    </div>
  )
}
