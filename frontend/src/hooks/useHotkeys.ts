import { useEffect } from 'react'

const isMac =
  typeof navigator !== 'undefined' &&
  /Mac|iPhone|iPod|iPad/i.test(navigator.platform)

export interface Hotkey {
  key: string // e.g. "k", "Escape", "?"
  mod?: boolean // requires ⌘ (mac) or Ctrl (win/linux)
  shift?: boolean
  alt?: boolean
  description?: string
  preventDefault?: boolean
  action: () => void
}

function shouldIgnoreTarget(e: KeyboardEvent): boolean {
  const t = e.target
  if (!(t instanceof HTMLElement)) return false
  const tag = t.tagName
  return tag === 'INPUT' || tag === 'TEXTAREA' || t.isContentEditable
}

/** Single global key matcher — keep tiny, no library. */
export function useHotkeys(hotkeys: Hotkey[]) {
  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      for (const hk of hotkeys) {
        const modPressed = isMac ? e.metaKey : e.ctrlKey
        if (hk.mod && !modPressed) continue
        if (!hk.mod && (e.metaKey || e.ctrlKey)) continue
        if (hk.shift && !e.shiftKey) continue
        if (!hk.shift && e.shiftKey) continue
        if (hk.alt && !e.altKey) continue
        if (e.key.toLowerCase() !== hk.key.toLowerCase()) continue
        // Skip text-field targets unless mod-modified hotkey (⌘K should still
        // open palette even from inside an Input).
        if (!hk.mod && shouldIgnoreTarget(e)) continue
        if (hk.preventDefault) e.preventDefault()
        hk.action()
        return
      }
    }
    window.addEventListener('keydown', handler)
    return () => window.removeEventListener('keydown', handler)
  }, [hotkeys])
}

/** Two-key chord for "g d", "g b" navigation. Resets after 1s timeout. */
export function useChordHotkeys(chords: Record<string, () => void>) {
  useEffect(() => {
    let prefix: string | null = null
    let timer: ReturnType<typeof setTimeout> | null = null

    const reset = () => {
      prefix = null
      if (timer) {
        clearTimeout(timer)
        timer = null
      }
    }

    const handler = (e: KeyboardEvent) => {
      if (shouldIgnoreTarget(e)) return reset()
      if (e.metaKey || e.ctrlKey || e.altKey) return reset()
      if (e.key.length !== 1) return reset()

      const k = e.key.toLowerCase()

      if (!prefix) {
        // Look for any chord that starts with this key
        const hasChord = Object.keys(chords).some((c) => c.startsWith(k + ' '))
        if (hasChord) {
          prefix = k
          if (timer) clearTimeout(timer)
          timer = setTimeout(reset, 1000)
        }
        return
      }

      const combo = `${prefix} ${k}`
      const action = chords[combo]
      if (action) {
        e.preventDefault()
        action()
      }
      reset()
    }

    window.addEventListener('keydown', handler)
    return () => {
      window.removeEventListener('keydown', handler)
      if (timer) clearTimeout(timer)
    }
  }, [chords])
}
