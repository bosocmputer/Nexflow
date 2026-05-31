import { useState, type KeyboardEvent } from 'react'
import { X } from 'lucide-react'
import { Badge } from '@/components/ui/badge'
import { Input } from '@/components/ui/input'
import { cn } from '@/lib/utils'

export interface TagInputProps {
  value: string[]
  onChange: (next: string[]) => void
  placeholder?: string
  className?: string
  /** Lowercase every tag on insert. */
  lower?: boolean
}

/**
 * Minimal multi-tag input — Enter or comma adds a tag, Backspace on empty
 * removes the last one. shadcn doesn't ship a tag input, so we hand-roll.
 */
export function TagInput({
  value,
  onChange,
  placeholder = 'พิมพ์แล้วกด Enter…',
  className,
  lower,
}: TagInputProps) {
  const [draft, setDraft] = useState('')

  const addTag = (raw: string) => {
    let v = raw.trim()
    if (!v) return
    if (lower) v = v.toLowerCase()
    if (value.includes(v)) {
      setDraft('')
      return
    }
    onChange([...value, v])
    setDraft('')
  }

  const removeTag = (idx: number) => {
    onChange(value.filter((_, i) => i !== idx))
  }

  const onKeyDown = (e: KeyboardEvent<HTMLInputElement>) => {
    if (e.key === 'Enter' || e.key === ',') {
      e.preventDefault()
      addTag(draft)
    } else if (e.key === 'Backspace' && draft === '' && value.length > 0) {
      e.preventDefault()
      removeTag(value.length - 1)
    }
  }

  return (
    <div
      className={cn(
        'flex min-h-9 flex-wrap items-center gap-1.5 rounded-md border border-input bg-background px-2 py-1.5 text-sm focus-within:ring-2 focus-within:ring-ring focus-within:ring-offset-2',
        className,
      )}
    >
      {value.map((tag, i) => (
        <Badge
          key={`${tag}-${i}`}
          variant="secondary"
          className="gap-1 pr-1 font-normal"
        >
          {tag}
          <button
            type="button"
            onClick={() => removeTag(i)}
            className="rounded-sm hover:bg-foreground/10 focus:outline-none"
            aria-label={`ลบ ${tag}`}
          >
            <X className="h-3 w-3" />
          </button>
        </Badge>
      ))}
      <Input
        value={draft}
        onChange={(e) => setDraft(e.target.value)}
        onKeyDown={onKeyDown}
        onBlur={() => addTag(draft)}
        placeholder={value.length === 0 ? placeholder : ''}
        className="h-6 flex-1 border-0 bg-transparent p-0 text-sm shadow-none focus-visible:ring-0 focus-visible:ring-offset-0"
      />
    </div>
  )
}
