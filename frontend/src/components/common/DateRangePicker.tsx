import { useId } from 'react'
import dayjs from 'dayjs'
import { CalendarDays } from 'lucide-react'

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

function displayDate(value: string): string {
  return value ? dayjs(value).format('DD/MM/YY') : ''
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
      <PopoverContent align="start" className="w-[min(300px,calc(100vw-2rem))] p-2.5">
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
                }}
              >
                {preset.label}
              </Button>
            ))}
          </div>

          <div className="grid grid-cols-2 gap-2">
            <div className="space-y-1">
              <Label htmlFor={`${id}-date-range-from`} className="text-xs text-muted-foreground">
                ตั้งแต่
              </Label>
              <Input
                id={`${id}-date-range-from`}
                value={from}
                onChange={(e) => onFromChange(e.target.value)}
                placeholder="YYYY-MM-DD"
                className="h-8 font-mono text-xs"
              />
            </div>
            <div className="space-y-1">
              <Label htmlFor={`${id}-date-range-to`} className="text-xs text-muted-foreground">
                ถึง
              </Label>
              <Input
                id={`${id}-date-range-to`}
                value={to}
                onChange={(e) => onToChange(e.target.value)}
                placeholder="YYYY-MM-DD"
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
