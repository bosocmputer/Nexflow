import { useCallback, useEffect, useRef, useState } from 'react'

// useNotifications wraps the browser Notification API + a small audio cue
// for incoming chat messages (Phase 4.11).
//
// Behavior:
//   - On first mount, request permission (browsers gate this behind a user
//     gesture; calling on mount only works if user has interacted with the
//     page already, which is true since they navigated to /messages).
//   - notify(title, body) only fires when the tab is hidden — admins see the
//     in-app sonner toast when the tab is focused (handled by parent).
//   - A boolean preference stored in localStorage lets admin mute the cue.
//   - Audio cue is synthesized via WebAudio (no asset files needed).

const STORAGE_KEY = 'nexflow.messages.notify_enabled'

export function useNotifications() {
  const [enabled, setEnabled] = useState<boolean>(() => {
    if (typeof window === 'undefined') return true
    const v = localStorage.getItem(STORAGE_KEY)
    return v === null ? true : v === '1'
  })
  const audioCtxRef = useRef<AudioContext | null>(null)

  useEffect(() => {
    localStorage.setItem(STORAGE_KEY, enabled ? '1' : '0')
  }, [enabled])

  // Request permission on mount (idempotent — Notification.requestPermission
  // is a no-op if already granted/denied).
  useEffect(() => {
    if (typeof window === 'undefined' || !('Notification' in window)) return
    if (Notification.permission === 'default') {
      Notification.requestPermission().catch(() => {
        /* silent */
      })
    }
  }, [])

  const playChime = useCallback(() => {
    if (typeof window === 'undefined') return
    try {
      // Lazy-init AudioContext on first use (browser gesture requirement is
      // satisfied because notify() is called during a poll-driven update,
      // typically while user has already interacted with the tab).
      if (!audioCtxRef.current) {
        const Ctx = window.AudioContext || (window as any).webkitAudioContext
        if (!Ctx) return
        audioCtxRef.current = new Ctx()
      }
      const ctx = audioCtxRef.current
      if (!ctx) return
      // Simple two-note chime — sine wave, ~200ms.
      const now = ctx.currentTime
      const osc = ctx.createOscillator()
      const gain = ctx.createGain()
      osc.type = 'sine'
      osc.frequency.setValueAtTime(880, now)
      osc.frequency.setValueAtTime(1320, now + 0.08)
      gain.gain.setValueAtTime(0, now)
      gain.gain.linearRampToValueAtTime(0.15, now + 0.01)
      gain.gain.exponentialRampToValueAtTime(0.0001, now + 0.2)
      osc.connect(gain).connect(ctx.destination)
      osc.start(now)
      osc.stop(now + 0.22)
    } catch {
      /* silent */
    }
  }, [])

  const notify = useCallback(
    (title: string, body: string) => {
      if (!enabled) return
      const hidden =
        typeof document !== 'undefined' && document.visibilityState === 'hidden'
      // Browser notification only when tab is hidden; in-app toast handles
      // the focused case so admins don't get double-notified.
      if (hidden && 'Notification' in window && Notification.permission === 'granted') {
        try {
          new Notification(title, { body, tag: 'nexflow-message' })
        } catch {
          /* silent */
        }
      }
      playChime()
    },
    [enabled, playChime],
  )

  const toggle = useCallback(() => setEnabled((v) => !v), [])

  return { enabled, toggle, notify }
}
