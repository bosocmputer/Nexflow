import { useEffect, useMemo, useState } from 'react'
import { CalendarClock, ChevronDown } from 'lucide-react'

import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import { cn } from '@/lib/utils'

export type StockScheduleValue = {
  schedule_mode: 'interval' | 'monthly'
  interval_seconds: number
  monthly_interval: number
  monthly_day: number
  monthly_time: string
  schedule_risk_acknowledged: boolean
}

type CustomUnit = 'minute' | 'hour' | 'day' | 'week' | 'month'

type Props = {
  value: StockScheduleValue
  enabled: boolean
  switchDisabled?: boolean
  disabled?: boolean
  nextRunAt?: string
  scheduleDirty?: boolean
  disabledReason?: string
  onChange: (value: StockScheduleValue) => void
  onEnabledChange: (enabled: boolean) => void
}

const PRESETS = [
  { seconds: 300, label: '5 นาที' },
  { seconds: 600, label: '10 นาที' },
  { seconds: 1200, label: '20 นาที' },
  { seconds: 1800, label: '30 นาที' },
  { seconds: 3600, label: '1 ชั่วโมง' },
]

const UNIT_RULES: Record<CustomUnit, { label: string; min: number; max: number }> = {
  minute: { label: 'นาที', min: 5, max: 1440 },
  hour: { label: 'ชั่วโมง', min: 1, max: 168 },
  day: { label: 'วัน', min: 1, max: 90 },
  week: { label: 'สัปดาห์', min: 1, max: 12 },
  month: { label: 'เดือน', min: 1, max: 12 },
}

export function formatStockSchedule(value: StockScheduleValue) {
  if (value.schedule_mode === 'monthly') {
    const every = value.monthly_interval > 1 ? `ทุก ${value.monthly_interval} เดือน` : 'ทุกเดือน'
    return `${every} วันที่ ${value.monthly_day} เวลา ${value.monthly_time}`
  }
  const seconds = value.interval_seconds || 300
  if (seconds % (7 * 86400) === 0) return `ทุก ${seconds / (7 * 86400)} สัปดาห์`
  if (seconds % 86400 === 0) return `ทุก ${seconds / 86400} วัน`
  if (seconds % 3600 === 0) return `ทุก ${seconds / 3600} ชั่วโมง`
  return `ทุก ${Math.max(5, Math.round(seconds / 60))} นาที`
}

function customFromSchedule(value: StockScheduleValue): { unit: CustomUnit; amount: number } {
  if (value.schedule_mode === 'monthly') return { unit: 'month', amount: value.monthly_interval || 1 }
  const seconds = value.interval_seconds || 300
  if (seconds % (7 * 86400) === 0) return { unit: 'week', amount: seconds / (7 * 86400) }
  if (seconds % 86400 === 0) return { unit: 'day', amount: seconds / 86400 }
  if (seconds % 3600 === 0) return { unit: 'hour', amount: seconds / 3600 }
  return { unit: 'minute', amount: Math.max(5, Math.round(seconds / 60)) }
}

function intervalSeconds(unit: Exclude<CustomUnit, 'month'>, amount: number) {
  const multipliers = { minute: 60, hour: 3600, day: 86400, week: 7 * 86400 }
  return amount * multipliers[unit]
}

function nextRunLabel(value?: string) {
  if (!value) return ''
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return ''
  return new Intl.DateTimeFormat('th-TH', {
    timeZone: 'Asia/Bangkok',
    dateStyle: 'short',
    timeStyle: 'short',
  }).format(date)
}

export function StockSchedulePicker({
  value,
  enabled,
  switchDisabled,
  disabled,
  nextRunAt,
  scheduleDirty,
  disabledReason,
  onChange,
  onEnabledChange,
}: Props) {
  const [open, setOpen] = useState(false)
  const initialCustom = useMemo(() => customFromSchedule(value), [value])
  const [unit, setUnit] = useState<CustomUnit>(initialCustom.unit)
  const [amount, setAmount] = useState(initialCustom.amount)
  const [monthlyDay, setMonthlyDay] = useState(value.monthly_day || 1)
  const [monthlyTime, setMonthlyTime] = useState(value.monthly_time || '00:00')
  const [riskAcknowledged, setRiskAcknowledged] = useState(value.schedule_risk_acknowledged)

  useEffect(() => {
    if (!open) return
    const custom = customFromSchedule(value)
    setUnit(custom.unit)
    setAmount(custom.amount)
    setMonthlyDay(value.monthly_day || 1)
    setMonthlyTime(value.monthly_time || '00:00')
    setRiskAcknowledged(value.schedule_risk_acknowledged)
  }, [open, value])

  const rules = UNIT_RULES[unit]
  const customIsLong = unit === 'day' || unit === 'week' || unit === 'month' || (unit === 'hour' && amount >= 24) || (unit === 'minute' && amount >= 1440)
  const customValid = Number.isInteger(amount) && amount >= rules.min && amount <= rules.max &&
    (unit !== 'month' || (monthlyDay >= 1 && monthlyDay <= 28 && /^([01]\d|2[0-3]):[0-5]\d$/.test(monthlyTime))) &&
    (!customIsLong || riskAcknowledged)
  const nextLabel = nextRunLabel(nextRunAt)
  const scheduleStatus = scheduleDirty
    ? 'บันทึกเพื่อคำนวณรอบถัดไป'
    : enabled && nextLabel
      ? `รอบถัดไป ${nextLabel}`
      : switchDisabled && disabledReason
        ? disabledReason
        : ''

  const choosePreset = (seconds: number) => {
    onChange({ schedule_mode: 'interval', interval_seconds: seconds, monthly_interval: 1, monthly_day: 1, monthly_time: '00:00', schedule_risk_acknowledged: false })
    setOpen(false)
  }

  const applyCustom = () => {
    if (!customValid) return
    if (unit === 'month') {
      onChange({
        schedule_mode: 'monthly',
        interval_seconds: amount * 30 * 86400,
        monthly_interval: amount,
        monthly_day: monthlyDay,
        monthly_time: monthlyTime,
        schedule_risk_acknowledged: true,
      })
    } else {
      onChange({
        schedule_mode: 'interval',
        interval_seconds: intervalSeconds(unit, amount),
        monthly_interval: 1,
        monthly_day: 1,
        monthly_time: '00:00',
        schedule_risk_acknowledged: customIsLong,
      })
    }
    setOpen(false)
  }

  return (
    <div className="space-y-1.5">
      <Label>รอบซิงก์อัตโนมัติ</Label>
      <div className="flex h-10 items-center rounded-md border bg-background">
        <Popover open={open} onOpenChange={setOpen}>
          <PopoverTrigger asChild>
            <Button
              type="button"
              variant="ghost"
              className="h-full min-w-0 flex-1 justify-between rounded-r-none px-3 font-normal"
              disabled={disabled}
              aria-label={`ตั้งรอบซิงก์ ${formatStockSchedule(value)}`}
            >
              <span className="flex min-w-0 items-center gap-2">
                <CalendarClock className="h-4 w-4 shrink-0 text-muted-foreground" aria-hidden="true" />
                <span className="truncate text-sm">{formatStockSchedule(value)}</span>
              </span>
              <ChevronDown className="h-4 w-4 shrink-0 text-muted-foreground" aria-hidden="true" />
            </Button>
          </PopoverTrigger>
          <PopoverContent align="end" className="w-[30rem] max-w-[calc(100vw-2rem)] space-y-4 p-4">
            <div>
              <p className="text-sm font-semibold">เลือกรอบซิงก์</p>
              <p className="mt-0.5 text-xs text-muted-foreground">ระบบใช้เวลาประเทศไทย และทำเพียงหนึ่งรอบเมื่อ server กลับมาหลังหยุดทำงาน</p>
              {scheduleStatus && <p className="mt-1 text-xs font-medium text-primary">{scheduleStatus}</p>}
            </div>
            <div className="grid grid-cols-2 gap-2 sm:grid-cols-5">
              {PRESETS.map((preset) => {
                const active = value.schedule_mode === 'interval' && value.interval_seconds === preset.seconds
                return (
                  <Button key={preset.seconds} type="button" size="sm" variant={active ? 'default' : 'outline'} onClick={() => choosePreset(preset.seconds)}>
                    {preset.label}
                  </Button>
                )
              })}
            </div>
            <div className="border-t pt-3">
              <p className="mb-2 text-xs font-medium text-muted-foreground">กำหนดเอง</p>
              <div className="grid gap-2 sm:grid-cols-[7rem_1fr]">
                <Input
                  type="number"
                  min={rules.min}
                  max={rules.max}
                  value={amount}
                  onChange={(event) => { setAmount(Number(event.target.value)); setRiskAcknowledged(false) }}
                  aria-label="จำนวนรอบซิงก์"
                />
                <Select value={unit} onValueChange={(next) => { const nextUnit = next as CustomUnit; setUnit(nextUnit); setAmount(Math.max(UNIT_RULES[nextUnit].min, 1)); setRiskAcknowledged(false) }}>
                  <SelectTrigger aria-label="หน่วยรอบซิงก์"><SelectValue /></SelectTrigger>
                  <SelectContent>
                    {(Object.keys(UNIT_RULES) as CustomUnit[]).map((key) => <SelectItem key={key} value={key}>{UNIT_RULES[key].label}</SelectItem>)}
                  </SelectContent>
                </Select>
              </div>
              <p className={cn('mt-1 text-xs', customValid ? 'text-muted-foreground' : 'text-destructive')}>
                กำหนดได้ {rules.min}-{rules.max} {rules.label}
              </p>
              {unit === 'month' && (
                <div className="mt-3 grid gap-3 sm:grid-cols-2">
                  <div className="space-y-1.5">
                    <Label htmlFor="stock-monthly-day">วันที่ของเดือน</Label>
                    <Input id="stock-monthly-day" type="number" min={1} max={28} value={monthlyDay} onChange={(event) => { setMonthlyDay(Number(event.target.value)); setRiskAcknowledged(false) }} />
                  </div>
                  <div className="space-y-1.5">
                    <Label htmlFor="stock-monthly-time">เวลา</Label>
                    <Input id="stock-monthly-time" type="time" value={monthlyTime} onChange={(event) => { setMonthlyTime(event.target.value); setRiskAcknowledged(false) }} />
                  </div>
                </div>
              )}
              {customIsLong && (
                <label className="mt-3 flex cursor-pointer items-start gap-2 rounded-md bg-warning/10 px-3 py-2 text-xs text-warning">
                  <Checkbox className="mt-0.5" checked={riskAcknowledged} onCheckedChange={(checked) => setRiskAcknowledged(checked === true)} />
                  <span>เข้าใจว่ารอบที่ห่างตั้งแต่หนึ่งวันขึ้นไป อาจทำให้ยอด Shopee ช้ากว่าสต๊อกหน้าร้าน</span>
                </label>
              )}
              <div className="mt-3 flex justify-end">
                <Button type="button" size="sm" onClick={applyCustom} disabled={!customValid}>ใช้รอบนี้</Button>
              </div>
            </div>
          </PopoverContent>
        </Popover>
        <div className="flex h-full items-center border-l px-3" title={switchDisabled ? disabledReason : undefined}>
          <Switch aria-label="เปิดซิงก์สต๊อกอัตโนมัติ" checked={enabled} disabled={switchDisabled} onCheckedChange={onEnabledChange} />
        </div>
      </div>
    </div>
  )
}
