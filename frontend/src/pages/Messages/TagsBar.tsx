import { useEffect, useState } from 'react'
import { Plus, X } from 'lucide-react'
import { toast } from 'sonner'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from '@/components/ui/popover'
import client from '@/api/client'
import { cn } from '@/lib/utils'
import type { ChatTag } from './types'

interface Props {
  lineUserID: string
}

// Tailwind palette → HSL classes for tag chips. Falls back to gray.
const COLOR_CLASSES: Record<string, string> = {
  gray: 'bg-muted text-muted-foreground',
  red: 'bg-destructive/15 text-destructive',
  orange: 'bg-warning/15 text-warning',
  yellow: 'bg-yellow-200/40 text-yellow-700 dark:text-yellow-400',
  green: 'bg-success/15 text-success',
  blue: 'bg-info/15 text-info',
  purple: 'bg-purple-200/40 text-purple-700 dark:text-purple-400',
}

// TagsBar — small chip row in the thread header showing tags attached to
// this conversation, plus a "+ tag" combobox for attach/detach (Phase 4.9).
// Tag list is global (managed at /settings/chat-tags); this just curates the
// per-conversation set.
export function TagsBar({ lineUserID }: Props) {
  const [conversationTags, setConversationTags] = useState<ChatTag[]>([])
  const [allTags, setAllTags] = useState<ChatTag[]>([])
  const [popoverOpen, setPopoverOpen] = useState(false)

  const loadAttached = async () => {
    try {
      const res = await client.get<{ data: ChatTag[] | null }>(
        `/api/admin/conversations/${encodeURIComponent(lineUserID)}/tags`,
      )
      setConversationTags(res.data.data ?? [])
    } catch {
      setConversationTags([])
    }
  }

  const loadAll = async () => {
    try {
      const res = await client.get<{ data: ChatTag[] | null }>('/api/settings/chat-tags')
      setAllTags(res.data.data ?? [])
    } catch {
      setAllTags([])
    }
  }

  useEffect(() => {
    loadAttached()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [lineUserID])

  useEffect(() => {
    if (popoverOpen && allTags.length === 0) {
      loadAll()
    }
  }, [popoverOpen, allTags.length])

  const updateTags = async (next: ChatTag[]) => {
    try {
      const res = await client.put<{ data: ChatTag[] | null }>(
        `/api/admin/conversations/${encodeURIComponent(lineUserID)}/tags`,
        { tag_ids: next.map((t) => t.id) },
      )
      setConversationTags(res.data.data ?? next)
    } catch (e: any) {
      toast.error('อัปเดต tag ไม่สำเร็จ: ' + (e?.message ?? 'unknown'))
    }
  }

  const toggleTag = (t: ChatTag) => {
    const has = conversationTags.some((c) => c.id === t.id)
    const next = has
      ? conversationTags.filter((c) => c.id !== t.id)
      : [...conversationTags, t]
    updateTags(next)
  }

  if (conversationTags.length === 0 && !popoverOpen) {
    // Render a tiny "+ tag" affordance only — no row of empty space
    return (
      <Popover open={popoverOpen} onOpenChange={setPopoverOpen}>
        <PopoverTrigger asChild>
          <Button
            variant="ghost"
            size="sm"
            className="h-5 gap-0.5 px-1.5 text-[10px] text-muted-foreground hover:text-foreground"
          >
            <Plus className="h-3 w-3" />
            tag
          </Button>
        </PopoverTrigger>
        <TagPickerContent
          allTags={allTags}
          attached={conversationTags}
          onToggle={toggleTag}
        />
      </Popover>
    )
  }

  return (
    <div className="flex flex-wrap items-center gap-1">
      {conversationTags.map((t) => (
        <Badge
          key={t.id}
          variant="secondary"
          className={cn('h-5 gap-0.5 px-1.5 text-[10px] font-normal', COLOR_CLASSES[t.color] ?? COLOR_CLASSES.gray)}
        >
          {t.label}
          <button
            type="button"
            onClick={() => toggleTag(t)}
            className="ml-0.5 opacity-60 hover:opacity-100"
            title="ลบ tag"
          >
            <X className="h-2.5 w-2.5" />
          </button>
        </Badge>
      ))}
      <Popover open={popoverOpen} onOpenChange={setPopoverOpen}>
        <PopoverTrigger asChild>
          <Button
            variant="ghost"
            size="sm"
            className="h-5 gap-0.5 px-1.5 text-[10px] text-muted-foreground hover:text-foreground"
          >
            <Plus className="h-3 w-3" />
            tag
          </Button>
        </PopoverTrigger>
        <TagPickerContent
          allTags={allTags}
          attached={conversationTags}
          onToggle={toggleTag}
        />
      </Popover>
    </div>
  )
}

function TagPickerContent({
  allTags,
  attached,
  onToggle,
}: {
  allTags: ChatTag[]
  attached: ChatTag[]
  onToggle: (t: ChatTag) => void
}) {
  return (
    <PopoverContent className="w-[min(16rem,calc(100vw-2rem))] p-0" align="start">
      {allTags.length === 0 ? (
        <div className="p-3 text-xs text-muted-foreground">
          ยังไม่มี tag — สร้างได้ที่{' '}
          <a className="text-primary underline" href="/settings/chat-tags">
            /settings/chat-tags
          </a>
        </div>
      ) : (
        <ul
          className="divide-y divide-border overflow-y-auto"
          style={{ maxHeight: 'min(16rem, var(--radix-popover-content-available-height, 16rem))' }}
        >
          {allTags.map((t) => {
            const isAttached = attached.some((a) => a.id === t.id)
            return (
              <li key={t.id}>
                <button
                  type="button"
                  onClick={() => onToggle(t)}
                  className="flex w-full items-center justify-between gap-2 px-3 py-1.5 text-left text-xs hover:bg-accent/40"
                >
                  <Badge
                    variant="secondary"
                    className={cn('h-4 px-1.5 text-[10px] font-normal', COLOR_CLASSES[t.color] ?? COLOR_CLASSES.gray)}
                  >
                    {t.label}
                  </Badge>
                  {isAttached && <span className="text-[10px] text-success">✓ ใช้แล้ว</span>}
                </button>
              </li>
            )
          })}
        </ul>
      )}
    </PopoverContent>
  )
}
