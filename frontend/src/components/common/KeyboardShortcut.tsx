import { cn } from '@/lib/utils'

const isMac =
  typeof navigator !== 'undefined' &&
  /Mac|iPhone|iPod|iPad/i.test(navigator.platform)

const keyMap: Record<string, { mac: string; win: string }> = {
  mod: { mac: '⌘', win: 'Ctrl' },
  meta: { mac: '⌘', win: 'Win' },
  shift: { mac: '⇧', win: 'Shift' },
  alt: { mac: '⌥', win: 'Alt' },
  ctrl: { mac: '⌃', win: 'Ctrl' },
  enter: { mac: '↵', win: '↵' },
  escape: { mac: 'Esc', win: 'Esc' },
  tab: { mac: '⇥', win: 'Tab' },
  backspace: { mac: '⌫', win: '⌫' },
  arrowup: { mac: '↑', win: '↑' },
  arrowdown: { mac: '↓', win: '↓' },
  arrowleft: { mac: '←', win: '←' },
  arrowright: { mac: '→', win: '→' },
}

function renderKey(k: string): string {
  const lower = k.toLowerCase()
  const m = keyMap[lower]
  if (m) return isMac ? m.mac : m.win
  return k.length === 1 ? k.toUpperCase() : k
}

export function KeyboardShortcut({
  keys,
  className,
}: {
  keys: string | string[]
  className?: string
}) {
  const arr = Array.isArray(keys) ? keys : keys.split('+')
  return (
    <span
      className={cn(
        'inline-flex items-center gap-0.5 font-mono text-[11px] text-muted-foreground',
        className,
      )}
    >
      {arr.map((k, i) => (
        <kbd
          key={i}
          className="inline-flex h-5 min-w-[20px] items-center justify-center rounded border border-border bg-muted px-1.5 font-sans text-[10px] font-medium text-muted-foreground"
        >
          {renderKey(k)}
        </kbd>
      ))}
    </span>
  )
}
