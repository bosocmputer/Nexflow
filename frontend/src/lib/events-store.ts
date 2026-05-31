import { create } from 'zustand'

import client from '@/api/client'

// Connection state for the SSE stream — surfaced in the sidebar indicator
// so admins know whether real-time updates are working.
export type EventsConnectionState = 'connecting' | 'live' | 'reconnecting' | 'offline'

// Server-side event types — must match backend/internal/services/events/broker.go
// constants. Adding a new type means updating both ends + this union.
export type ServerEventType =
  | 'hello'
  | 'message_received'
  | 'conversation_updated'
  | 'unread_changed'

// Listener signature — handlers get the parsed JSON payload + the event type
// (so a single listener can dispatch on type if it wants).
export type EventListener = (type: ServerEventType, payload: any) => void

interface EventsState {
  status: EventsConnectionState
  // failedAttempts is shown in the sidebar tooltip; resets on successful reconnect.
  failedAttempts: number
  // Internal — listeners registered by useChatEvents.
  _listeners: Set<EventListener>
  // Internal — current EventSource (null when offline / not yet connected).
  _es: EventSource | null
  // Internal — pending reconnect timer handle.
  _reconnectTimer: number | null

  // connect opens (or reopens) the EventSource. Idempotent — safe to call
  // multiple times; existing connection is reused.
  connect: () => Promise<void>
  // disconnect tears down the connection (used on logout).
  disconnect: () => void
  // subscribe registers a listener and returns an unsubscribe func.
  subscribe: (fn: EventListener) => () => void
}

// Singleton EventSource — one per browser tab regardless of how many
// components use the hook. Multiplexing in-process is cheaper than opening
// N connections (each one costs an HTTP/2 stream + a backend goroutine).
//
// Reconnect strategy:
//   - On error → mark 'reconnecting', retry after backoff (3s, 6s, 12s, max 30s)
//   - After 5 consecutive failures → mark 'offline' (admin sees red dot;
//     polling fallback kicks in via the existing 30s intervals)
//   - On successful 'hello' event → reset failedAttempts, mark 'live'
const RECONNECT_BACKOFF_MS = [3_000, 6_000, 12_000, 20_000, 30_000]
const API_BASE_URL = (import.meta.env.VITE_API_URL || '').replace(/\/$/, '')

function apiEventURL(path: string): string {
  return `${API_BASE_URL}${path}`
}

export const useEventsStore = create<EventsState>((set, get) => ({
  status: 'connecting',
  failedAttempts: 0,
  _listeners: new Set(),
  _es: null,
  _reconnectTimer: null,

  connect: async () => {
    // If already connected, no-op.
    if (get()._es) return

    set({ status: 'connecting' })

    // Step 1 — get a short-lived signed token. JWT lives in client headers
    // already, so this call is auto-authenticated.
    let userID: string
    let token: string
    try {
      const res = await client.post<{ token: string; user_id: string }>(
        '/api/admin/events/token',
      )
      userID = res.data.user_id
      token = res.data.token
    } catch {
      // Token issue failed — likely auth expired. Schedule retry; the
      // failedAttempts counter will eventually flip status to 'offline'.
      const attempts = get().failedAttempts + 1
      const delay = RECONNECT_BACKOFF_MS[Math.min(attempts - 1, RECONNECT_BACKOFF_MS.length - 1)]
      set({
        failedAttempts: attempts,
        status: attempts >= 5 ? 'offline' : 'reconnecting',
      })
      const t = window.setTimeout(() => get().connect(), delay)
      set({ _reconnectTimer: t })
      return
    }

    // Step 2 — open the stream. EventSource auto-reconnects on its own
    // for many failure modes (network glitch, server bounce), but we still
    // listen to onerror so we can update UI state.
    const url = apiEventURL(
      `/api/admin/events?u=${encodeURIComponent(userID)}&t=${encodeURIComponent(token)}`,
    )
    const es = new EventSource(url)
    set({ _es: es })

    const dispatch = (type: ServerEventType, raw: string) => {
      try {
        const payload = JSON.parse(raw || '{}')
        get()._listeners.forEach((fn) => {
          try {
            fn(type, payload)
          } catch {
            /* listener errors don't break the stream */
          }
        })
      } catch {
        /* malformed payload — drop */
      }
    }

    es.addEventListener('hello', (ev) => {
      set({ status: 'live', failedAttempts: 0 })
      dispatch('hello', (ev as MessageEvent).data)
    })
    es.addEventListener('message_received', (ev) => {
      dispatch('message_received', (ev as MessageEvent).data)
    })
    es.addEventListener('conversation_updated', (ev) => {
      dispatch('conversation_updated', (ev as MessageEvent).data)
    })
    es.addEventListener('unread_changed', (ev) => {
      dispatch('unread_changed', (ev as MessageEvent).data)
    })

    es.onerror = () => {
      // EventSource transitions to readyState=2 (CLOSED) when the server
      // returns 4xx or the token expires. In that case it won't auto-reconnect,
      // so we tear it down and re-issue a fresh token.
      if (es.readyState === EventSource.CLOSED) {
        es.close()
        const attempts = get().failedAttempts + 1
        const delay = RECONNECT_BACKOFF_MS[Math.min(attempts - 1, RECONNECT_BACKOFF_MS.length - 1)]
        set({
          _es: null,
          failedAttempts: attempts,
          status: attempts >= 5 ? 'offline' : 'reconnecting',
        })
        const t = window.setTimeout(() => get().connect(), delay)
        set({ _reconnectTimer: t })
      } else {
        // readyState=1 means EventSource is auto-retrying — just reflect that.
        set({ status: 'reconnecting' })
      }
    }
  },

  disconnect: () => {
    const { _es, _reconnectTimer } = get()
    if (_reconnectTimer) {
      window.clearTimeout(_reconnectTimer)
    }
    if (_es) {
      _es.close()
    }
    set({ _es: null, _reconnectTimer: null, status: 'offline', failedAttempts: 0 })
  },

  subscribe: (fn: EventListener) => {
    get()._listeners.add(fn)
    return () => {
      get()._listeners.delete(fn)
    }
  },
}))
