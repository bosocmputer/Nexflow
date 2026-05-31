import { useEffect, useRef, useState, useCallback } from 'react'
import { Archive, ArchiveRestore, ArrowLeft, Bell, BellOff, Check, Plus, RefreshCw, RotateCcw, Search, X } from 'lucide-react'
import { toast } from 'sonner'
import dayjs from 'dayjs'
import relativeTime from 'dayjs/plugin/relativeTime'
import 'dayjs/locale/th'

// Plugin loads here are idempotent — already loaded by ConversationList,
// re-loading here is safe + decoupled (this file would still work if the
// list component is removed).
dayjs.extend(relativeTime)
dayjs.locale('th')

import { Input } from '@/components/ui/input'

import { Avatar, AvatarFallback, AvatarImage } from '@/components/ui/avatar'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import client from '@/api/client'
import { useChatEvents } from '@/hooks/useChatEvents'
import { MessageBubble } from './MessageBubble'
import { Composer, type PendingAttachment } from './Composer'
import { CustomerHistoryPanel } from './CustomerHistoryPanel'
import { NotesPanel } from './NotesPanel'
import { TagsBar } from './TagsBar'
import { useNotifications } from './useNotifications'
import type { ChatConversation, ChatMessage } from './types'

interface Props {
  lineUserID: string
  conversation: ChatConversation | null
  onOpenCreateBill: () => void
  onExtractMedia: (messageId: string, kind: string) => void
  // Mobile-only: parent passes a handler that clears the URL ?u= param so
  // the conversation list reappears. Rendered as an `ArrowLeft` button in
  // the thread header (visible at <md breakpoint only).
  onBackToList?: () => void
}

// 30s safety-net polling — SSE is the primary delivery mechanism and arrives
// in <500ms. Polling exists only to recover from a missed event (rare edge
// case where the broker dropped on a full buffer or the SSE stream broke
// silently). Was 5s before SSE — that load is no longer needed.
const ACTIVE_POLL_MS = 30_000

// MessageThread renders the right pane: header (customer info + เปิดบิลขาย),
// scrollable message list (poll 5s, delta-fetch via ?since=lastSeen), and
// composer at the bottom. Calls mark-read once per conversation switch.
export function MessageThread({
  lineUserID,
  conversation,
  onOpenCreateBill,
  onExtractMedia,
  onBackToList,
}: Props) {
  const [messages, setMessages] = useState<ChatMessage[]>([])
  const [loading, setLoading] = useState(true)
  const intervalRef = useRef<ReturnType<typeof setInterval> | null>(null)
  const scrollRootRef = useRef<HTMLDivElement | null>(null)
  const lastSeenRef = useRef<string>('')
  const notif = useNotifications()
  // Drag-drop state — pendingExternal is consumed once by Composer.
  const [pendingExternal, setPendingExternal] = useState<PendingAttachment[]>([])
  const [isDraggingFile, setIsDraggingFile] = useState(false)
  const dragCounterRef = useRef(0)
  // Phase D: thread-level search. When non-empty, polling pauses and fetch
  // calls /messages?q=<term> for filtered results.
  const [searchQ, setSearchQ] = useState('')
  const [searchOpen, setSearchOpen] = useState(false)

  // Auto-scroll to bottom when new messages arrive AND user is already near
  // the bottom (within 80px). Otherwise leave the scroll position alone so we
  // don't yank an admin who's reading older history.
  const isNearBottom = useCallback(() => {
    const root = scrollRootRef.current
    if (!root) return true
    const distanceFromBottom = root.scrollHeight - root.scrollTop - root.clientHeight
    return distanceFromBottom < 80
  }, [])

  const scrollToBottom = useCallback(() => {
    const root = scrollRootRef.current
    if (!root) return
    root.scrollTop = root.scrollHeight
  }, [])

  const fetchInitial = useCallback(async (q?: string) => {
    setLoading(true)
    try {
      const res = await client.get<{ data: ChatMessage[] | null }>(
        `/api/admin/conversations/${encodeURIComponent(lineUserID)}/messages`,
        { params: { limit: 100, q: q || undefined } },
      )
      const rows = res.data.data ?? []
      setMessages(rows)
      // Search results don't update lastSeenRef so polling keeps using the
      // pre-search marker once admin clears the search.
      if (!q) {
        lastSeenRef.current = rows.length > 0 ? rows[rows.length - 1].created_at : ''
        setTimeout(scrollToBottom, 0)
      }
    } catch (e: any) {
      toast.error('โหลดข้อความล้มเหลว: ' + (e?.message ?? 'unknown'))
    } finally {
      setLoading(false)
    }
  }, [lineUserID, scrollToBottom])

  const fetchDelta = useCallback(async () => {
    if (!lastSeenRef.current) return
    // Skip polling when the tab is hidden — saves a round-trip every 5s when
    // admin has Nexflow open in a background tab.
    if (typeof document !== 'undefined' && document.visibilityState === 'hidden') {
      return
    }
    // While search is active, polling for new messages would mix delta
    // results into the search list — pause until admin clears search.
    if (searchQ.trim() !== '') {
      return
    }
    try {
      const res = await client.get<{ data: ChatMessage[] | null }>(
        `/api/admin/conversations/${encodeURIComponent(lineUserID)}/messages`,
        { params: { since: lastSeenRef.current, limit: 100 } },
      )
      const newRows = res.data.data ?? []
      if (newRows.length === 0) return
      const stick = isNearBottom()
      // Defensive dedup by id — SSE may have inserted some of these rows
      // between the ?since=… read on the server and our setState here.
      setMessages((prev) => {
        const existing = new Set(prev.map((m) => m.id))
        const fresh = newRows.filter((m) => !existing.has(m.id))
        if (fresh.length === 0) return prev
        return [...prev, ...fresh]
      })
      lastSeenRef.current = newRows[newRows.length - 1].created_at

      // Notify on incoming messages — sound + (when tab hidden) browser
      // notification. Skip outgoing/system rows.
      const incoming = newRows.filter((m) => m.direction === 'incoming')
      if (incoming.length > 0) {
        const last = incoming[incoming.length - 1]
        const senderName = conversation?.display_name || lineUserID.slice(0, 12)
        const preview =
          last.kind === 'text'
            ? last.text_content
            : last.kind === 'image'
              ? '📷 รูปภาพ'
              : last.kind === 'file'
                ? '📄 ไฟล์'
                : last.kind === 'audio'
                  ? '🎙 voice'
                  : '(ข้อความใหม่)'
        notif.notify(senderName, preview.slice(0, 80))
      }

      if (stick) {
        setTimeout(scrollToBottom, 0)
      }
    } catch {
      /* silent */
    }
  }, [lineUserID, isNearBottom, scrollToBottom, conversation, notif, searchQ])

  // Effect 1 — runs ONCE per conversation switch. Initial load + mark-read.
  // Previously this effect listed fetchInitial+fetchDelta in deps, which
  // caused a re-run on every parent render (ConversationList polling 30s
  // returns a new conversation reference → fetchDelta useCallback rebuilds
  // → effect re-fires → mark-read spammed and initial fetch repeated).
  // Now keyed only on lineUserID so it actually fires once per switch.
  useEffect(() => {
    fetchInitial()
    client
      .post(`/api/admin/conversations/${encodeURIComponent(lineUserID)}/mark-read`)
      .catch(() => {
        /* silent */
      })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [lineUserID])

  // Effect 2 — long-poll safety net. Keep latest fetchDelta in a ref so the
  // interval always calls the most recent closure (which has the latest
  // searchQ, conversation, etc) without restarting the timer on every render.
  const fetchDeltaRef = useRef(fetchDelta)
  useEffect(() => {
    fetchDeltaRef.current = fetchDelta
  }, [fetchDelta])

  useEffect(() => {
    intervalRef.current = setInterval(() => fetchDeltaRef.current(), ACTIVE_POLL_MS)
    return () => {
      if (intervalRef.current) clearInterval(intervalRef.current)
    }
  }, [lineUserID])

  // SSE — primary real-time delivery. When this conversation receives a new
  // message, append it without polling. Other conversations' events are
  // ignored here (ConversationList handles its own subscription).
  const onSSEMessage = useCallback(
    (payload: { line_user_id: string; message: ChatMessage }) => {
      if (payload.line_user_id !== lineUserID) return
      setMessages((prev) => {
        // Three-way dedup against the race between SSE event + HTTP response.
        // Both arrive ~simultaneously after admin sends; without dedup we'd
        // briefly show the same outgoing bubble twice until next refresh.
        //
        //   1. Real id already in the list (HTTP response landed first)
        //      → skip, that path already replaced the optimistic tmp- row.
        //   2. Outgoing event matches an optimistic tmp- row by content
        //      → REPLACE that row. The HTTP response's later .map() then
        //        finds no tmp- row to replace and is a harmless no-op.
        //   3. Otherwise → genuinely new message (incoming, or sent from
        //        another admin tab) → append.
        if (prev.some((m) => m.id === payload.message.id)) return prev

        if (payload.message.direction === 'outgoing') {
          const tmpIdx = prev.findIndex((m) => {
            if (!m.id.startsWith('tmp-')) return false
            if (m.direction !== 'outgoing') return false
            if (m.kind !== payload.message.kind) return false
            // text matches by exact content; image matches by filename
            // (size is also identical but filename alone is enough).
            if (m.kind === 'text') {
              return m.text_content === payload.message.text_content
            }
            if (m.kind === 'image') {
              return m.media?.filename === payload.message.media?.filename
            }
            return false
          })
          if (tmpIdx >= 0) {
            const next = [...prev]
            next[tmpIdx] = payload.message
            return next
          }
        }

        return [...prev, payload.message]
      })
      lastSeenRef.current = payload.message.created_at
      if (isNearBottom()) {
        setTimeout(scrollToBottom, 0)
      }
      // Notify on incoming messages from this customer (tab focus check
      // happens inside the hook — no toast spam when admin is looking).
      if (payload.message.direction === 'incoming') {
        const senderName = conversation?.display_name || lineUserID.slice(0, 12)
        const preview =
          payload.message.kind === 'text'
            ? payload.message.text_content
            : payload.message.kind === 'image'
              ? '📷 รูปภาพ'
              : payload.message.kind === 'file'
                ? '📄 ไฟล์'
                : payload.message.kind === 'audio'
                  ? '🎙 voice'
                  : '(ข้อความใหม่)'
        notif.notify(senderName, preview.slice(0, 80))
      }
    },
    [lineUserID, isNearBottom, scrollToBottom, conversation, notif],
  )
  useChatEvents({ onMessage: onSSEMessage })

  const handleSend = useCallback(
    async (text: string) => {
      // Optimistic insert so the bubble appears instantly.
      const optimistic: ChatMessage = {
        id: 'tmp-' + Date.now(),
        line_user_id: lineUserID,
        direction: 'outgoing',
        kind: 'text',
        text_content: text,
        delivery_status: 'pending',
        created_at: new Date().toISOString(),
      }
      setMessages((prev) => [...prev, optimistic])
      setTimeout(scrollToBottom, 0)
      try {
        const res = await client.post<{ message: ChatMessage; delivery: string }>(
          `/api/admin/conversations/${encodeURIComponent(lineUserID)}/messages`,
          { text },
        )
        // Replace optimistic with the persisted row.
        setMessages((prev) =>
          prev.map((m) => (m.id === optimistic.id ? res.data.message : m)),
        )
        lastSeenRef.current = res.data.message.created_at
        if (res.data.delivery === 'failed') {
          toast.error('ส่งไม่สำเร็จ — กดที่ ⚠ บนข้อความเพื่อดูรายละเอียด')
        }
      } catch (e: any) {
        setMessages((prev) =>
          prev.map((m) =>
            m.id === optimistic.id
              ? { ...m, delivery_status: 'failed', delivery_error: e?.message ?? 'unknown' }
              : m,
          ),
        )
        toast.error('ส่งไม่สำเร็จ: ' + (e?.message ?? 'unknown'))
      }
    },
    [lineUserID, scrollToBottom],
  )

  // Send a single image attachment. Mirrors handleSend's optimistic insert
  // pattern but uses multipart upload and the /messages/media endpoint.
  const handleSendMedia = useCallback(
    async (file: File) => {
      const tmpURL = URL.createObjectURL(file)
      const optimistic: ChatMessage = {
        id: 'tmp-' + Date.now(),
        line_user_id: lineUserID,
        direction: 'outgoing',
        kind: 'image',
        text_content: '',
        delivery_status: 'pending',
        created_at: new Date().toISOString(),
        // Fake media row so the bubble renders the local preview immediately.
        media: {
          id: 'tmp-media',
          message_id: 'tmp-media',
          filename: file.name,
          content_type: file.type,
          size_bytes: file.size,
          sha256: '',
          storage_path: tmpURL,
          created_at: new Date().toISOString(),
        },
      }
      // Override the bubble URL so it points at the local blob during upload.
      // MessageBubble normally uses /api/.../media but tmp message has tmp id.
      ;(optimistic as ChatMessage & { _localPreviewURL?: string })._localPreviewURL = tmpURL
      setMessages((prev) => [...prev, optimistic])
      setTimeout(scrollToBottom, 0)

      try {
        const form = new FormData()
        form.append('file', file)
        const res = await client.post<{ message: ChatMessage; delivery: string }>(
          `/api/admin/conversations/${encodeURIComponent(lineUserID)}/messages/media`,
          form,
          { headers: { 'Content-Type': 'multipart/form-data' } },
        )
        URL.revokeObjectURL(tmpURL)
        setMessages((prev) =>
          prev.map((m) => (m.id === optimistic.id ? res.data.message : m)),
        )
        lastSeenRef.current = res.data.message.created_at
        if (res.data.delivery === 'failed') {
          toast.error('ส่งรูปไม่สำเร็จ — กดที่ ⚠ บนข้อความเพื่อดูรายละเอียด')
        }
      } catch (e: any) {
        URL.revokeObjectURL(tmpURL)
        setMessages((prev) =>
          prev.map((m) =>
            m.id === optimistic.id
              ? { ...m, delivery_status: 'failed', delivery_error: e?.response?.data?.error ?? e?.message ?? 'unknown' }
              : m,
          ),
        )
        toast.error('ส่งรูปไม่สำเร็จ: ' + (e?.response?.data?.error ?? e?.message ?? 'unknown'))
      }
    },
    [lineUserID, scrollToBottom],
  )

  // Debounced search — refetch on each searchQ change. Empty searchQ is a
  // signal to resume normal mode (calls fetchInitial without q which also
  // resets lastSeenRef and re-scrolls to bottom).
  useEffect(() => {
    const handle = setTimeout(() => {
      const trimmed = searchQ.trim()
      if (trimmed) {
        fetchInitial(trimmed)
      } else if (searchOpen) {
        // search box just closed → refresh to current state
        fetchInitial()
      }
    }, 250)
    return () => clearTimeout(handle)
  }, [searchQ, searchOpen, fetchInitial])

  // Phase C: change conversation status. Shows toast on success so admin
  // confirms the action; parent (Messages/index.tsx) re-fetches via
  // ConversationList's polling — list filter then hides the row from the
  // current tab.
  const handleSetStatus = useCallback(
    async (status: 'open' | 'resolved' | 'archived') => {
      try {
        await client.patch(
          `/api/admin/conversations/${encodeURIComponent(lineUserID)}/status`,
          { status },
        )
        const label = status === 'resolved' ? 'ปิดเรื่อง' : status === 'archived' ? 'archive' : 'เปิด'
        toast.success(`${label}แล้ว`)
      } catch (e: any) {
        toast.error('เปลี่ยนสถานะไม่สำเร็จ: ' + (e?.response?.data?.error ?? e?.message ?? 'unknown'))
      }
    },
    [lineUserID],
  )

  // Drag-drop on the entire thread div. dragCounter handles enter/leave on
  // children: a single drag event series can fire multiple enter/leave as the
  // pointer moves across nested elements; we only show the overlay once.
  const handleDragEnter = (e: React.DragEvent) => {
    if (!Array.from(e.dataTransfer.types).includes('Files')) return
    dragCounterRef.current += 1
    setIsDraggingFile(true)
  }
  const handleDragLeave = () => {
    dragCounterRef.current = Math.max(0, dragCounterRef.current - 1)
    if (dragCounterRef.current === 0) setIsDraggingFile(false)
  }
  const handleDragOver = (e: React.DragEvent) => {
    if (Array.from(e.dataTransfer.types).includes('Files')) e.preventDefault()
  }
  const handleDrop = (e: React.DragEvent) => {
    e.preventDefault()
    dragCounterRef.current = 0
    setIsDraggingFile(false)
    const files = Array.from(e.dataTransfer.files).filter((f) => f.type.startsWith('image/'))
    if (files.length === 0) return
    setPendingExternal(
      files.map((f, i) => ({
        id: `drop-${Date.now()}-${i}`,
        file: f,
        previewURL: URL.createObjectURL(f),
      })),
    )
  }

  const initials = (conversation?.display_name || '?').slice(0, 2).toUpperCase()

  return (
    <div
      className="relative flex min-h-0 flex-col rounded-lg border border-border bg-card"
      onDragEnter={handleDragEnter}
      onDragLeave={handleDragLeave}
      onDragOver={handleDragOver}
      onDrop={handleDrop}
    >
      {isDraggingFile && (
        <div className="pointer-events-none absolute inset-0 z-10 flex items-center justify-center rounded-lg border-2 border-dashed border-primary bg-primary/10 backdrop-blur-sm">
          <div className="rounded-full bg-card px-4 py-2 text-sm font-medium shadow">
            วางไฟล์เพื่อแนบ
          </div>
        </div>
      )}
      {/* Header — compact (h-12). Customer name + OA badge inline so admins
          see "which LINE OA" at a glance without leaving for the inbox list. */}
      <div className="flex h-12 items-center justify-between gap-2 border-b border-border px-3">
        <div className="flex min-w-0 items-center gap-2">
          {/* Back-to-list button — mobile only. Desktop sees both panes. */}
          {onBackToList && (
            <Button
              type="button"
              variant="ghost"
              size="sm"
              onClick={onBackToList}
              className="-ml-1 h-8 w-8 shrink-0 p-0 md:hidden"
              aria-label="กลับไปยังรายการ"
            >
              <ArrowLeft className="h-4 w-4" />
            </Button>
          )}
          <Avatar className="h-8 w-8 shrink-0">
            {conversation?.picture_url && (
              <AvatarImage src={conversation.picture_url} alt={conversation.display_name} />
            )}
            <AvatarFallback className="text-[10px]">{initials}</AvatarFallback>
          </Avatar>
          <div className="min-w-0">
            <div className="flex items-center gap-1.5">
              <span className="truncate text-sm font-medium">
                {conversation?.display_name || lineUserID.slice(0, 12)}
              </span>
              {conversation?.line_oa_name && (
                <Badge
                  variant="outline"
                  className="h-4 shrink-0 px-1 text-[9px] font-normal"
                  title={`LINE OA: ${conversation.line_oa_name}`}
                >
                  {conversation.line_oa_name}
                </Badge>
              )}
              {conversation?.status === 'resolved' && (
                <Badge variant="secondary" className="h-4 shrink-0 bg-success/15 px-1 text-[9px] font-normal text-success">
                  ปิดแล้ว
                </Badge>
              )}
              {conversation?.status === 'archived' && (
                <Badge variant="secondary" className="h-4 shrink-0 bg-muted px-1 text-[9px] font-normal text-muted-foreground">
                  Archive
                </Badge>
              )}
            </div>
            <div className="flex items-center gap-1.5 leading-tight">
              <span className="truncate font-mono text-[10px] text-muted-foreground">
                {conversation?.phone
                  ? `📞 ${conversation.phone}`
                  : `${lineUserID.slice(0, 18)}…`}
              </span>
              {conversation?.last_message_at && (
                <span
                  className="text-[10px] text-muted-foreground"
                  title={`อัปเดตล่าสุด ${dayjs(conversation.last_message_at).format('DD/MM/YYYY HH:mm:ss')}`}
                >
                  {/* Relative time conveys "data freshness" better than an absolute
                      timestamp does — "เมื่อสักครู่" vs "30/04 14:32" tells the
                      admin at a glance whether SSE is delivering or not. */}
                  · อัปเดต{dayjs(conversation.last_message_at).fromNow()}
                </span>
              )}
            </div>
          </div>
        </div>
        <div className="flex shrink-0 items-center gap-1">
          <Button
            variant="ghost"
            size="sm"
            className="h-7 px-2"
            onClick={notif.toggle}
            title={notif.enabled ? 'ปิดเสียง / desktop notification' : 'เปิดเสียง / desktop notification'}
          >
            {notif.enabled ? (
              <Bell className="h-3.5 w-3.5" />
            ) : (
              <BellOff className="h-3.5 w-3.5 text-muted-foreground" />
            )}
          </Button>
          <Button
            variant="ghost"
            size="sm"
            className="h-7 px-2"
            onClick={() => {
              setSearchOpen((v) => !v)
              if (searchOpen) setSearchQ('')
            }}
            title="ค้นหาในห้องนี้"
          >
            <Search className="h-3.5 w-3.5" />
          </Button>
          <Button
            variant="ghost"
            size="sm"
            className="h-7 px-2"
            onClick={() => fetchInitial()}
            title="รีเฟรชข้อความ"
          >
            <RefreshCw className="h-3.5 w-3.5" />
          </Button>
          {/* Status actions — render based on current state */}
          {conversation?.status === 'open' && (
            <Button
              variant="outline"
              size="sm"
              className="h-7 gap-1 px-2.5 text-xs"
              onClick={() => handleSetStatus('resolved')}
              title="ทำเครื่องหมายว่าจบเรื่องแล้ว — ห้องจะกลับมาเปิดอีกครั้งเมื่อลูกค้าทักใหม่"
            >
              <Check className="h-3.5 w-3.5" />
              ปิดเรื่อง
            </Button>
          )}
          {conversation?.status === 'resolved' && (
            <Button
              variant="outline"
              size="sm"
              className="h-7 gap-1 px-2.5 text-xs"
              onClick={() => handleSetStatus('open')}
              title="เปิดห้องอีกครั้ง"
            >
              <RotateCcw className="h-3.5 w-3.5" />
              เปิดอีกครั้ง
            </Button>
          )}
          {conversation?.status !== 'archived' ? (
            <Button
              variant="ghost"
              size="sm"
              className="h-7 px-2"
              onClick={() => handleSetStatus('archived')}
              title="Archive ห้องนี้ — ใช้สำหรับ spam/บอท (ไม่ revive อัตโนมัติ)"
            >
              <Archive className="h-3.5 w-3.5" />
            </Button>
          ) : (
            <Button
              variant="ghost"
              size="sm"
              className="h-7 px-2"
              onClick={() => handleSetStatus('open')}
              title="เปิดห้องจาก Archive"
            >
              <ArchiveRestore className="h-3.5 w-3.5" />
            </Button>
          )}
          <Button size="sm" className="h-7 gap-1 px-2.5 text-xs" onClick={onOpenCreateBill}>
            <Plus className="h-3.5 w-3.5" />
            เปิดบิลขาย
          </Button>
        </div>
      </div>

      {/* Tags row (Phase 4.9) — slim bar with chips + "+ tag" affordance */}
      <div className="flex items-center gap-1.5 border-b border-border bg-muted/10 px-3 py-1">
        <span className="text-[10px] uppercase tracking-wide text-muted-foreground">tags</span>
        <TagsBar lineUserID={lineUserID} />
      </div>

      {/* Notes panel (Phase 4.8) — admin-only annotations */}
      <NotesPanel lineUserID={lineUserID} />

      {/* Customer history (Phase 4.5) — past bills from this LINE user */}
      <CustomerHistoryPanel lineUserID={lineUserID} />

      {/* Phase D: search bar (collapsible) */}
      {searchOpen && (
        <div className="flex items-center gap-1.5 border-b border-border bg-muted/20 px-3 py-1.5">
          <Search className="h-3.5 w-3.5 text-muted-foreground" />
          <Input
            value={searchQ}
            onChange={(e) => setSearchQ(e.target.value)}
            placeholder="ค้นหาข้อความในห้องนี้…"
            autoFocus
            className="h-7 flex-1 text-xs"
          />
          {searchQ && (
            <span className="shrink-0 text-[11px] text-muted-foreground">
              {messages.length} ผลลัพธ์
            </span>
          )}
          <Button
            variant="ghost"
            size="sm"
            className="h-7 px-1.5"
            onClick={() => {
              setSearchOpen(false)
              setSearchQ('')
            }}
          >
            <X className="h-3.5 w-3.5" />
          </Button>
        </div>
      )}

      {/* Scrollable message list — native scrollbar so we can keep a direct ref */}
      <div ref={scrollRootRef} className="flex-1 overflow-y-auto">
        <div className="flex flex-col gap-2 p-4">
          {loading && messages.length === 0 ? (
            <div className="text-center text-xs text-muted-foreground">กำลังโหลด…</div>
          ) : messages.length === 0 ? (
            <div className="py-8 text-center text-xs text-muted-foreground">
              ยังไม่มีข้อความในบทสนทนานี้
            </div>
          ) : (
            messages.map((m) => (
              <MessageBubble key={m.id} message={m} onExtract={onExtractMedia} />
            ))
          )}
        </div>
      </div>

      {/* Composer — disabled when archived (admin must un-archive first) */}
      {conversation?.status === 'archived' && (
        <div className="flex items-center justify-between gap-2 border-t border-border bg-muted/40 px-4 py-2 text-xs text-muted-foreground">
          <span>🗄 ห้องนี้ archived แล้ว — กดเปิดอีกครั้งก่อนตอบ</span>
          <Button
            variant="outline"
            size="sm"
            className="h-6 gap-1 px-2 text-[11px]"
            onClick={() => handleSetStatus('open')}
          >
            <RotateCcw className="h-3 w-3" />
            เปิดอีกครั้ง
          </Button>
        </div>
      )}
      <Composer
        disabled={conversation?.status === 'archived'}
        onSend={handleSend}
        onSendMedia={handleSendMedia}
        externalAttachments={pendingExternal}
        onConsumeExternal={() => setPendingExternal([])}
      />
    </div>
  )
}
