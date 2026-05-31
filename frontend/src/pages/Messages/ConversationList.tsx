import { useEffect, useRef, useState, useCallback } from 'react'
import dayjs from 'dayjs'
import relativeTime from 'dayjs/plugin/relativeTime'
import 'dayjs/locale/th'
import { Tag, X } from 'lucide-react'

import { Avatar, AvatarFallback, AvatarImage } from '@/components/ui/avatar'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from '@/components/ui/popover'
import { ScrollArea } from '@/components/ui/scroll-area'
import client from '@/api/client'
import { useChatEvents } from '@/hooks/useChatEvents'
import { cn } from '@/lib/utils'
import type { ChatConversation, ChatStatus, ChatTag } from './types'

const COLOR_CLASSES: Record<string, string> = {
  gray: 'bg-muted text-muted-foreground',
  red: 'bg-destructive/15 text-destructive',
  orange: 'bg-warning/15 text-warning',
  yellow: 'bg-yellow-200/40 text-yellow-700 dark:text-yellow-400',
  green: 'bg-success/15 text-success',
  blue: 'bg-info/15 text-info',
  purple: 'bg-purple-200/40 text-purple-700 dark:text-purple-400',
}

dayjs.extend(relativeTime)
dayjs.locale('th')

interface Props {
  selectedID: string
  onSelect: (lineUserID: string) => void
  onSelectedConvChange?: (conv: ChatConversation | null) => void
}

// 60s safety-net polling — SSE drives real-time updates. Polling exists
// only to recover from a missed event (broker buffer overflow / silent
// SSE breakage). Was 30s before SSE; doubled now to halve idle DB load.
const POLL_MS = 60_000

// ConversationList polls /api/admin/conversations every 30s and renders the
// inbox. Click a row to select it (URL ?u= updates via parent).
export function ConversationList({ selectedID, onSelect, onSelectedConvChange }: Props) {
  const [rows, setRows] = useState<ChatConversation[]>([])
  const [unreadOnly, setUnreadOnly] = useState(false)
  const [search, setSearch] = useState('')
  const [statusTab, setStatusTab] = useState<ChatStatus>('open')
  const [loading, setLoading] = useState(true)
  // Phase 4.9 tag filter — multi-select against /api/settings/chat-tags.
  // selectedTagIDs maps to ?tags=id1,id2 (ANY-match on the backend).
  const [allTags, setAllTags] = useState<ChatTag[]>([])
  const [selectedTagIDs, setSelectedTagIDs] = useState<string[]>([])
  const intervalRef = useRef<ReturnType<typeof setInterval> | null>(null)

  // Fetch tag catalog once on mount — small list, no need to refetch.
  useEffect(() => {
    client
      .get<{ data: ChatTag[] | null }>('/api/settings/chat-tags')
      .then((res) => setAllTags(res.data.data ?? []))
      .catch(() => setAllTags([]))
  }, [])

  // Single source of truth for fetching the inbox. Called from initial
  // mount, periodic safety-net poll, tab focus, and SSE event handler.
  // Memoized on filter state so the SSE useEffect doesn't recreate the
  // subscription on every render.
  const fetchInbox = useCallback(
    async (force = false) => {
      if (
        !force &&
        typeof document !== 'undefined' &&
        document.visibilityState === 'hidden'
      ) {
        return
      }
      try {
        const res = await client.get<{ data: ChatConversation[] | null }>(
          '/api/admin/conversations',
          {
            params: {
              unread: unreadOnly || undefined,
              status: statusTab,
              q: search.trim() || undefined,
              tags: selectedTagIDs.length > 0 ? selectedTagIDs.join(',') : undefined,
              limit: 100,
            },
          },
        )
        setRows(res.data.data ?? [])
      } catch {
        /* silent — keep last good list */
      } finally {
        setLoading(false)
      }
    },
    [unreadOnly, statusTab, search, selectedTagIDs],
  )

  useEffect(() => {
    fetchInbox(true)
    intervalRef.current = setInterval(() => fetchInbox(false), POLL_MS)

    const onVisibility = () => {
      if (document.visibilityState === 'visible') {
        fetchInbox(true)
      }
    }
    document.addEventListener('visibilitychange', onVisibility)

    return () => {
      if (intervalRef.current) clearInterval(intervalRef.current)
      document.removeEventListener('visibilitychange', onVisibility)
    }
  }, [fetchInbox])

  // SSE — refetch the list on any inbox-relevant event. The list query is
  // cheap (50 rows + EXISTS subqueries on indexed columns), so it's simpler
  // and less error-prone than patching individual rows in place. With SSE
  // wired we get sub-500ms updates without polling more often.
  const refetchInbox = useCallback(() => {
    fetchInbox(true)
  }, [fetchInbox])
  useChatEvents({
    onMessage: refetchInbox,
    onConvUpdated: refetchInbox,
  })

  // Keep parent informed of which conversation is currently selected, so it
  // can pre-fill the customer name in CreateBillPanel.
  useEffect(() => {
    if (!onSelectedConvChange) return
    const found = rows.find((r) => r.line_user_id === selectedID) ?? null
    onSelectedConvChange(found)
  }, [rows, selectedID, onSelectedConvChange])

  // Search is server-side now (passed as ?q= to /api/admin/conversations);
  // no client-side filter needed.
  const filtered = rows

  const STATUS_TABS: Array<{ key: ChatStatus; label: string }> = [
    { key: 'open', label: 'เปิดอยู่' },
    { key: 'resolved', label: 'ปิดแล้ว' },
    { key: 'archived', label: 'Archive' },
  ]

  return (
    <div className="flex min-h-0 flex-col rounded-lg border border-border bg-card">
      {/* Status tab strip */}
      <div className="flex items-center gap-0.5 border-b border-border px-1 pt-1">
        {STATUS_TABS.map((t) => (
          <button
            key={t.key}
            type="button"
            onClick={() => setStatusTab(t.key)}
            className={cn(
              'flex-1 rounded-t-md px-2 py-1 text-[11px] font-medium transition-colors',
              statusTab === t.key
                ? 'bg-accent text-accent-foreground'
                : 'text-muted-foreground hover:bg-accent/40',
            )}
          >
            {t.label}
          </button>
        ))}
      </div>

      <div className="flex flex-col gap-1.5 border-b border-border p-2">
        <div className="flex items-center gap-1.5">
          <Input
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            placeholder="ค้นหา (ชื่อหรือข้อความ)…"
            className="h-7 flex-1 text-xs"
          />
          <Popover>
            <PopoverTrigger asChild>
              <Button
                variant={selectedTagIDs.length > 0 ? 'default' : 'outline'}
                size="sm"
                className="h-7 shrink-0 gap-1 px-2 text-[11px]"
                title={
                  allTags.length === 0
                    ? 'ยังไม่มี tag — สร้างที่ /settings/chat-tags'
                    : 'กรองตาม tag'
                }
                disabled={allTags.length === 0}
              >
                <Tag className="h-3 w-3" />
                Tag
                {selectedTagIDs.length > 0 && (
                  <span className="ml-0.5 rounded-full bg-background/20 px-1 text-[10px] tabular-nums">
                    {selectedTagIDs.length}
                  </span>
                )}
              </Button>
            </PopoverTrigger>
            <PopoverContent align="end" className="w-[min(16rem,calc(100vw-2rem))] p-1">
              <div className="flex items-center justify-between px-2 py-1 text-[10px] uppercase tracking-wide text-muted-foreground">
                <span>กรองตาม tag (any-match)</span>
                {selectedTagIDs.length > 0 && (
                  <button
                    type="button"
                    onClick={() => setSelectedTagIDs([])}
                    className="text-[10px] text-primary hover:underline"
                  >
                    ล้าง
                  </button>
                )}
              </div>
              {/* Hint linking to where tags get attached. Without this admins
                  see the filter and don't realize they need to attach tags
                  in the chat thread first (TagsBar component). */}
              <div className="border-b border-border/50 px-2 pb-1.5 text-[10px] leading-snug text-muted-foreground">
                💡 ติด tag ให้ห้องแชทได้ที่ <span className="font-medium text-foreground">header ของห้อง</span>
              </div>
              <div
                className="flex flex-col overflow-auto"
                style={{ maxHeight: 'min(16rem, var(--radix-popover-content-available-height, 16rem))' }}
              >
                {allTags.map((t) => {
                  const checked = selectedTagIDs.includes(t.id)
                  return (
                    <button
                      key={t.id}
                      type="button"
                      onClick={() =>
                        setSelectedTagIDs((prev) =>
                          checked
                            ? prev.filter((id) => id !== t.id)
                            : [...prev, t.id],
                        )
                      }
                      className={cn(
                        'flex items-center gap-2 rounded-sm px-2 py-1.5 text-left text-xs hover:bg-accent',
                        checked && 'bg-accent/60',
                      )}
                    >
                      <span
                        className={cn(
                          'h-2 w-2 shrink-0 rounded-full',
                          (COLOR_CLASSES[t.color] ?? COLOR_CLASSES.gray)
                            .split(' ')[0],
                        )}
                      />
                      <span className="flex-1 truncate">{t.label}</span>
                      {checked && <span className="text-[10px] text-primary">✓</span>}
                    </button>
                  )
                })}
              </div>
            </PopoverContent>
          </Popover>
          <Button
            variant={unreadOnly ? 'default' : 'outline'}
            size="sm"
            className="h-7 shrink-0 px-2 text-[11px]"
            onClick={() => setUnreadOnly((v) => !v)}
            title={unreadOnly ? 'แสดงทั้งหมด' : 'เฉพาะที่ยังไม่อ่าน'}
          >
            {unreadOnly ? 'ทั้งหมด' : 'ยังไม่อ่าน'}
          </Button>
        </div>
        {selectedTagIDs.length > 0 && (
          <div className="flex flex-wrap gap-1">
            {selectedTagIDs.map((id) => {
              const tag = allTags.find((t) => t.id === id)
              if (!tag) return null
              return (
                <Badge
                  key={id}
                  variant="secondary"
                  className={cn(
                    'h-5 gap-0.5 px-1.5 text-[10px] font-normal',
                    COLOR_CLASSES[tag.color] ?? COLOR_CLASSES.gray,
                  )}
                >
                  {tag.label}
                  <button
                    type="button"
                    onClick={() =>
                      setSelectedTagIDs((prev) => prev.filter((x) => x !== id))
                    }
                    className="ml-0.5 rounded hover:bg-foreground/10"
                    title="เอาออก"
                  >
                    <X className="h-2.5 w-2.5" />
                  </button>
                </Badge>
              )
            })}
          </div>
        )}
      </div>

      <ScrollArea className="flex-1">
        {loading && rows.length === 0 ? (
          <div className="p-4 text-center text-xs text-muted-foreground">กำลังโหลด…</div>
        ) : filtered.length === 0 ? (
          <div className="p-4 text-center text-xs text-muted-foreground">
            {unreadOnly ? 'ไม่มีข้อความที่ยังไม่ได้อ่าน' : 'ยังไม่มีบทสนทนา'}
          </div>
        ) : (
          <ul className="divide-y divide-border">
            {filtered.map((r) => {
              const active = r.line_user_id === selectedID
              const initials = (r.display_name || '?').slice(0, 2).toUpperCase()
              return (
                <li key={r.line_user_id}>
                  <button
                    type="button"
                    onClick={() => onSelect(r.line_user_id)}
                    className={cn(
                      'flex w-full items-start gap-2.5 px-3 py-2.5 text-left transition-colors',
                      active
                        ? 'bg-accent text-accent-foreground'
                        : 'hover:bg-accent/40',
                    )}
                  >
                    <Avatar className="h-9 w-9 shrink-0">
                      {r.picture_url && <AvatarImage src={r.picture_url} alt={r.display_name} />}
                      <AvatarFallback className="text-xs">{initials}</AvatarFallback>
                    </Avatar>
                    <div className="min-w-0 flex-1">
                      <div className="flex items-baseline justify-between gap-2">
                        <span className="truncate text-sm font-medium">
                          {r.display_name || r.line_user_id.slice(0, 10)}
                        </span>
                        <span className="shrink-0 text-[10px] tabular-nums text-muted-foreground">
                          {dayjs(r.last_message_at).fromNow(true)}
                        </span>
                      </div>
                      <div className="flex items-center justify-between gap-2">
                        <span className="flex min-w-0 items-center gap-1.5">
                          {r.line_oa_name && (
                            <Badge
                              variant="outline"
                              className="h-4 shrink-0 px-1 text-[9px] font-normal"
                              title={`LINE OA: ${r.line_oa_name}`}
                            >
                              {r.line_oa_name}
                            </Badge>
                          )}
                          <span className="truncate font-mono text-[10px] text-muted-foreground">
                            {r.line_user_id}
                          </span>
                        </span>
                        {r.unread_admin_count > 0 && (
                          <Badge className="h-5 min-w-[20px] shrink-0 justify-center px-1.5 text-[10px]">
                            {r.unread_admin_count > 99 ? '99+' : r.unread_admin_count}
                          </Badge>
                        )}
                      </div>
                    </div>
                  </button>
                </li>
              )
            })}
          </ul>
        )}
      </ScrollArea>
    </div>
  )
}
