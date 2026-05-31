import { useEffect } from 'react'

import {
  type EventListener,
  type ServerEventType,
  useEventsStore,
} from '@/lib/events-store'

interface Handlers {
  onMessage?: (payload: { line_user_id: string; message: any }) => void
  onConvUpdated?: (payload: { line_user_id: string; [k: string]: any }) => void
  onUnreadChanged?: (payload: { total: number }) => void
}

// useChatEvents subscribes the calling component to one or more SSE event
// types. The connection itself is shared across the whole app via the
// singleton in events-store.ts — this hook just adds/removes a dispatcher.
//
// Pass a handler object; pass undefined to ignore that event type. Handlers
// can be inline arrow functions but must be wrapped in useCallback if they
// depend on changing values (the hook re-subscribes when handlers change).
export function useChatEvents(handlers: Handlers): void {
  const subscribe = useEventsStore((s) => s.subscribe)

  useEffect(() => {
    const dispatcher: EventListener = (type: ServerEventType, payload: any) => {
      switch (type) {
        case 'message_received':
          handlers.onMessage?.(payload)
          break
        case 'conversation_updated':
          handlers.onConvUpdated?.(payload)
          break
        case 'unread_changed':
          handlers.onUnreadChanged?.(payload)
          break
        // 'hello' is internal — ignored here
      }
    }
    return subscribe(dispatcher)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [handlers.onMessage, handlers.onConvUpdated, handlers.onUnreadChanged])
}
