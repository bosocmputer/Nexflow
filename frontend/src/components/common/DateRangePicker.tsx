import { useEffect, useId, useState } from 'react'
import dayjs from 'dayjs'
import { CalendarDays, ChevronLeft, ChevronRight } from 'lucide-react'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'
import { cn } from '@/lib/utils'

interface DateRangePickerProps {
  from: string
  to: string
  onFromChange: (value: string) => void
  onToChange: (value: string) => void
  className?: string
  title?: string
  description?: string
  presets?: DateRangePreset[]
  clearLabel?: string
}

export type DateRangePreset = {
  label: string
  getRange: () => { from: string; to: string }
}

const defaultPresets: DateRangePreset[] = [
  {
    label: 'วันนี้',
    getRange: () => {
      const today = dayjs().format('YYYY-MM-DD')
      return { from: today, to: today }
    },
  },
  {
    label: '7 วัน',
    getRange: () => ({
      from: dayjs().subtract(6, 'day').format('YYYY-MM-DD'),
      to: dayjs().format('YYYY-MM-DD'),
    }),
  },
  {
    label: 'เดือนนี้',
    getRange: () => ({
      from: dayjs().startOf('month').format('YYYY-MM-DD'),
      to: dayjs().format('YYYY-MM-DD'),
    }),
  },
]

const THAI_MONTHS = [
  'มกราคม',
  'กุมภาพันธ์',
  'มีนาคม',
  'เมษายน',
  'พฤษภาคม',
  'มิถุนายน',
  'กรกฎาคม',
  'สิงหาคม',
  'กันยายน',
  'ตุลาคม',
  'พฤศจิกายน',
  'ธันวาคม',
]

const WEEKDAY_LABELS = ['จ', 'อ', 'พ', 'พฤ', 'ศ', 'ส', 'อา']

function isoDateParts(value: string): { year: string; month: string; day: string } | null {
  const match = value.match(/^(\d{4})-(\d{2})-(\d{2})$/)
  if (!match) return null
  return { year: match[1], month: match[2], day: match[3] }
}

function isValidIsoDate(value: string): boolean {
  const parts = isoDateParts(value)
  if (!parts) return false

  const parsed = new Date(`${value}T00:00:00`)
  return (
    !Number.isNaN(parsed.getTime()) &&
    parsed.getFullYear() === Number(parts.year) &&
    parsed.getMonth() + 1 === Number(parts.month) &&
    parsed.getDate() === Number(parts.day)
  )
}

function displayDate(value: string): string {
  const parts = isoDateParts(value)
  if (!parts || !isValidIsoDate(value)) return value
  return `${parts.day}/${parts.month}/${parts.year}`
}

function parseDateInput(value: string): string | null {
  const raw = value.trim()
  if (!raw) return ''

  const displayMatch = raw.match(/^(\d{1,2})\/(\d{1,2})\/(\d{4})$/)
  if (displayMatch) {
    const day = displayMatch[1].padStart(2, '0')
    const month = displayMatch[2].padStart(2, '0')
    const iso = `${displayMatch[3]}-${month}-${day}`
    return isValidIsoDate(iso) ? iso : null
  }

  const isoMatch = raw.match(/^(\d{4})-(\d{1,2})-(\d{1,2})$/)
  if (isoMatch) {
    const month = isoMatch[2].padStart(2, '0')
    const day = isoMatch[3].padStart(2, '0')
    const iso = `${isoMatch[1]}-${month}-${day}`
    return isValidIsoDate(iso) ? iso : null
  }

  return null
}

interface MiniCalendarProps {
  month: dayjs.Dayjs
  from: string
  to: string
  hoverDate: string | null
  onSelect: (date: string) => void
  onHover: (date: string | null) => void
  onPrevMonth: () => void
  onNextMonth: () => void
}

function MiniCalendar({
  month,
  from,
  to,
  hoverDate,
  onSelect,
  onHover,
  onPrevMonth,
  onNextMonth,
}: MiniCalendarProps) {
  const cells: Array<dayjs.Dayjs | null> = []
  const firstDay = month.startOf('month')
  const startOffset = (firstDay.day() + 6) % 7
  for (let i = 0; i < startOffset; i += 1) cells.push(null)
  for (let d = 0; d < month.daysInMonth(); d += 1) cells.push(firstDay.add(d, 'day'))
  while (cells.length < 42) cells.push(null)

  const today = dayjs().format('YYYY-MM-DD')
  const effectiveTo = from && !to && hoverDate ? hoverDate : to
  const rangeStart = from && effectiveTo ? (from < effectiveTo ? from : effectiveTo) : null
  const rangeEnd = from && effectiveTo ? (from < effectiveTo ? effectiveTo : from) : null

  return (
    <div className="select-none">
      <div className="mb-1.5 flex items-center justify-between">
        <Button type="button" variant="ghost" size="icon" className="h-6 w-6" onClick={onPrevMonth}>
          <ChevronLeft className="h-3.5 w-3.5" />
        </Button>
        <span className="text-xs font-medium">
          {THAI_MONTHS[month.month()]} {month.year() + 543}
        </span>
        <Button type="button" variant="ghost" size="icon" className="h-6 w-6" onClick={onNextMonth}>
          <ChevronRight className="h-3.5 w-3.5" />
        </Button>
      </div>

      <div className="mb-0.5 grid grid-cols-7">
        {WEEKDAY_LABELS.map((day) => (
          <div key={day} className="flex h-6 items-center justify-center text-[10px] font-medium text-muted-foreground">
            {day}
          </div>
        ))}
      </div>

      <div className="grid grid-cols-7">
        {cells.map((date, index) => {
          if (!date) return <div key={index} className="h-7 w-full" />

          const dateStr = date.format('YYYY-MM-DD')
          const isFrom = dateStr === from
          const isTo = dateStr === to
          const isSelected = isFrom || isTo
          const isDifferentRange = from !== to
          const isInRange = Boolean(
            rangeStart &&
              rangeEnd &&
              isDifferentRange &&
              dateStr > rangeStart &&
              dateStr < rangeEnd,
          )
          const isRangeStart = Boolean(rangeStart && rangeEnd && isDifferentRange && dateStr === rangeStart)
          const isRangeEnd = Boolean(rangeStart && rangeEnd && isDifferentRange && dateStr === rangeEnd)
          const isToday = dateStr === today

          return (
            <div
              key={index}
              className={cn(
                'flex h-7 items-center justify-center',
                isInRange && 'bg-primary/15',
                isRangeStart && 'rounded-l-full bg-primary/15',
                isRangeEnd && 'rounded-r-full bg-primary/15',
              )}
            >
              <button
                type="button"
                className={cn(
                  'flex h-7 w-7 cursor-pointer items-center justify-center rounded-full text-xs transition-colors',
                  isSelected && 'bg-primary font-semibold text-primary-foreground',
                  isToday && !isSelected && 'ring-1 ring-primary ring-offset-1',
                  !isSelected && 'hover:bg-accent',
                )}
                onClick={() => onSelect(dateStr)}
                onMouseEnter={() => onHover(dateStr)}
                onMouseLeave={() => onHover(null)}
              >
                {date.date()}
              </button>
            </div>
          )
        })}
      </div>
    </div>
  )
}

export function DateRangePicker({
  from,
  to,
  onFromChange,
  onToChange,
  className,
  title = 'ช่วงวันที่',
  description = 'ใช้กรองประวัติการทำงานตามวันที่เกิดรายการ',
  presets = defaultPresets,
  clearLabel = 'ล้างช่วงวันที่',
}: DateRangePickerProps) {
  const id = useId()
  const label = from || to
    ? `${displayDate(from) || 'เริ่มต้น'} - ${displayDate(to) || 'วันนี้'}`
    : 'เลือกช่วงวันที่'
  const [calendarMonth, setCalendarMonth] = useState<dayjs.Dayjs>(
    () => (from ? dayjs(from) : dayjs()).startOf('month'),
  )
  const [selectingStep, setSelectingStep] = useState<'from' | 'to'>('from')
  const [hoverDate, setHoverDate] = useState<string | null>(null)
  const [fromInput, setFromInput] = useState(() => displayDate(from))
  const [toInput, setToInput] = useState(() => displayDate(to))

  useEffect(() => {
    if (from && dayjs(from).isValid()) {
      setCalendarMonth(dayjs(from).startOf('month'))
    }
  }, [from])

  useEffect(() => {
    setFromInput(displayDate(from))
  }, [from])

  useEffect(() => {
    setToInput(displayDate(to))
  }, [to])

  const handleCalendarSelect = (date: string) => {
    if (selectingStep === 'from') {
      onFromChange(date)
      onToChange('')
      setSelectingStep('to')
      return
    }

    if (from && date < from) {
      onToChange(from)
      onFromChange(date)
    } else {
      onToChange(date)
    }
    setSelectingStep('from')
    setHoverDate(null)
  }

  const handleFromInputChange = (value: string) => {
    setFromInput(value)
    const parsed = parseDateInput(value)
    if (parsed !== null) onFromChange(parsed)
  }

  const handleToInputChange = (value: string) => {
    setToInput(value)
    const parsed = parseDateInput(value)
    if (parsed !== null) onToChange(parsed)
  }

  const normalizeFromInput = () => {
    const parsed = parseDateInput(fromInput)
    setFromInput(displayDate(parsed !== null ? parsed : from))
  }

  const normalizeToInput = () => {
    const parsed = parseDateInput(toInput)
    setToInput(displayDate(parsed !== null ? parsed : to))
  }

  return (
    <Popover>
      <PopoverTrigger asChild>
        <Button
          type="button"
          variant="outline"
          className={cn('h-10 min-w-[210px] justify-start gap-2 px-3 font-normal', className)}
        >
          <CalendarDays className="h-3.5 w-3.5 text-muted-foreground" />
          <span className={cn('text-sm', !(from || to) && 'text-muted-foreground')}>
            {label}
          </span>
        </Button>
      </PopoverTrigger>
      <PopoverContent align="start" className="w-[min(320px,calc(100vw-2rem))] p-2.5">
        <div className="space-y-2.5">
          <div>
            <div className="text-sm font-medium">{title}</div>
            <div className="mt-0.5 text-xs text-muted-foreground">
              {description}
            </div>
          </div>

          <div className="grid grid-cols-3 gap-1.5">
            {presets.map((preset) => (
              <Button
                key={preset.label}
                type="button"
                variant="secondary"
                size="sm"
                className="h-7 px-2 text-xs"
                onClick={() => {
                  const range = preset.getRange()
                  onFromChange(range.from)
                  onToChange(range.to)
                  setCalendarMonth(dayjs(range.from).startOf('month'))
                  setSelectingStep('from')
                  setHoverDate(null)
                }}
              >
                {preset.label}
              </Button>
            ))}
          </div>

          <MiniCalendar
            month={calendarMonth}
            from={from}
            to={to}
            hoverDate={selectingStep === 'to' ? hoverDate : null}
            onSelect={handleCalendarSelect}
            onHover={setHoverDate}
            onPrevMonth={() => setCalendarMonth((month) => month.subtract(1, 'month'))}
            onNextMonth={() => setCalendarMonth((month) => month.add(1, 'month'))}
          />

          {selectingStep === 'to' && (
            <p className="-mt-1 text-center text-[10px] text-muted-foreground">
              เลือกวันสิ้นสุด
            </p>
          )}

          <div className="grid grid-cols-2 gap-2">
            <div className="space-y-1">
              <Label htmlFor={`${id}-date-range-from`} className="text-xs text-muted-foreground">
                ตั้งแต่
              </Label>
              <Input
                id={`${id}-date-range-from`}
                value={fromInput}
                onChange={(e) => handleFromInputChange(e.target.value)}
                onBlur={normalizeFromInput}
                placeholder="DD/MM/YYYY"
                className="h-8 font-mono text-xs"
              />
            </div>
            <div className="space-y-1">
              <Label htmlFor={`${id}-date-range-to`} className="text-xs text-muted-foreground">
                ถึง
              </Label>
              <Input
                id={`${id}-date-range-to`}
                value={toInput}
                onChange={(e) => handleToInputChange(e.target.value)}
                onBlur={normalizeToInput}
                placeholder="DD/MM/YYYY"
                className="h-8 font-mono text-xs"
              />
            </div>
          </div>

          {(from || to) && (
            <Button
              type="button"
              variant="ghost"
              size="sm"
              className="h-7 w-full text-xs"
              onClick={() => {
                onFromChange('')
                onToChange('')
                setSelectingStep('from')
                setHoverDate(null)
              }}
            >
              {clearLabel}
            </Button>
          )}
        </div>
      </PopoverContent>
    </Popover>
  )
}
