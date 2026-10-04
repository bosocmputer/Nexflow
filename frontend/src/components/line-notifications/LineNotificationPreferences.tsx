import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { cn } from '@/lib/utils'

export type LineNotificationSource = 'shopee' | 'tiktok_shop' | 'nextstep_marketplace'
export type LineNotificationGroup = 'orders' | 'cancellations' | 'sml' | 'settlements'

export interface LineEventDefinition {
  key: string
  source: LineNotificationSource
  group: LineNotificationGroup
  label: string
  description: string
  default_enabled: boolean
  supports_flex: boolean
}

export interface LineEventSample {
  event_key: string
  alt_text: string
  message_text: string
  flex_payload: Record<string, unknown>
}

export function RecipientEventPreferences({
  catalog,
  eventKeys,
  enabled,
  onChange,
}: {
  catalog: LineEventDefinition[]
  eventKeys: string[]
  enabled: boolean
  onChange: (keys: string[]) => void
}) {
  return (
    <div className={cn('space-y-3 rounded-lg border border-border p-3', !enabled && 'opacity-60')}>
      <div className="flex flex-col gap-2 sm:flex-row sm:items-start sm:justify-between">
        <div>
          <div className="text-sm font-semibold">ประเภทที่ต้องการรับ</div>
          <div className="text-xs text-muted-foreground">เลือกเป็นรายเหตุการณ์ หรือใช้ชุดสำเร็จรูปด้านขวา</div>
        </div>
        <div className="flex flex-wrap gap-1.5">
          <Button type="button" variant="outline" size="sm" className="h-7 text-xs" disabled={!enabled} onClick={() => onChange(catalog.filter((event) => event.default_enabled).map((event) => event.key))}>งานออเดอร์</Button>
          <Button type="button" variant="outline" size="sm" className="h-7 text-xs" disabled={!enabled} onClick={() => onChange(actionRequiredEventKeys(catalog))}>เฉพาะที่ต้องดำเนินการ</Button>
          <Button type="button" variant="outline" size="sm" className="h-7 text-xs" disabled={!enabled} onClick={() => onChange(catalog.map((event) => event.key))}>รับทั้งหมด</Button>
          <Button type="button" variant="ghost" size="sm" className="h-7 text-xs" disabled={!enabled} onClick={() => onChange([])}>ล้าง</Button>
        </div>
      </div>
      <div className="grid gap-3 md:grid-cols-2">
        {eventGroups(catalog).map((group) => (
          <fieldset key={group.key} className="rounded-md border border-border/70 bg-muted/20 p-3" disabled={!enabled}>
            <legend className="px-1 text-xs font-semibold text-foreground">{eventGroupLabel(group.key)}</legend>
            <div className="space-y-2.5">
              {group.events.map((event) => {
                const checked = eventKeys.includes(event.key)
                return (
                  <label key={event.key} className="flex cursor-pointer items-start gap-2.5">
                    <Checkbox
                      className="mt-0.5"
                      checked={checked}
                      disabled={!enabled}
                      onCheckedChange={(value) => onChange(value === true ? [...new Set([...eventKeys, event.key])] : eventKeys.filter((key) => key !== event.key))}
                      aria-label={event.label}
                    />
                    <span className="min-w-0">
                      <span className="flex flex-wrap items-center gap-1.5 text-sm font-medium">
                        <Badge variant="outline" className={sourceBadgeClass(event.source)}>{sourceLabel(event.source)}</Badge>
                        {event.label.replace(/^(ออเดอร์ )?(Shopee|TikTok|NextStep) ?/, '')}
                      </span>
                      <span className="mt-0.5 block text-xs text-muted-foreground">{event.description}</span>
                    </span>
                  </label>
                )
              })}
            </div>
          </fieldset>
        ))}
      </div>
      <div className="text-xs text-muted-foreground">เลือกแล้ว {eventKeys.length} จาก {catalog.length} ประเภท · การเปลี่ยนค่านี้ไม่ส่งข้อความทดสอบ</div>
    </div>
  )
}

export function FlexMessagePreview({ event, sample }: { event: LineEventDefinition; sample: LineEventSample }) {
  const rawLines = extractFlexTexts(sample.flex_payload).filter((text, index, all) => text && all.indexOf(text) === index)
  const lines = (rawLines[0] === sourceLabel(event.source) ? rawLines.slice(1) : rawLines).slice(0, 14)
  const [title, ...details] = lines
  return (
    <div className="mx-auto w-full max-w-[390px] overflow-hidden rounded-2xl border border-border bg-white shadow-sm">
      <div className="h-1.5" style={{ backgroundColor: sourceColor(event.source) }} />
      <div className="space-y-3 p-4 text-slate-900">
        <div className="flex items-center justify-between gap-2">
          <span className="inline-flex rounded-md px-2 py-1 text-[11px] font-semibold text-white" style={{ backgroundColor: sourceColor(event.source) }}>
            {sourceLabel(event.source)}
          </span>
          <span className="text-[10px] text-slate-400">Flex Message</span>
        </div>
        <div>
          <div className="text-base font-bold leading-snug">{title || event.label}</div>
          <div className="mt-1 text-xs text-slate-500">{sample.alt_text}</div>
        </div>
        <div className="space-y-1.5 border-t border-slate-100 pt-3">
          {details.map((line, index) => (
            <div key={`${line}-${index}`} className={cn('text-xs leading-relaxed', index === 0 ? 'font-semibold text-slate-800' : 'text-slate-600')}>
              {line}
            </div>
          ))}
        </div>
      </div>
    </div>
  )
}

function extractFlexTexts(value: unknown): string[] {
  if (!value || typeof value !== 'object') return []
  if (Array.isArray(value)) return value.flatMap(extractFlexTexts)
  const record = value as Record<string, unknown>
  const current = record.type === 'text' && typeof record.text === 'string' ? [record.text] : []
  return current.concat(Object.values(record).flatMap(extractFlexTexts))
}

function eventGroups(catalog: LineEventDefinition[]) {
  const order: LineNotificationGroup[] = ['orders', 'cancellations', 'sml', 'settlements']
  return order.map((key) => ({ key, events: catalog.filter((event) => event.group === key) })).filter((group) => group.events.length > 0)
}

function actionRequiredEventKeys(catalog: LineEventDefinition[]) {
  return catalog
    .filter((event) => event.group === 'cancellations' || event.group === 'settlements' || event.key.includes('needs_review') || event.key.includes('failed'))
    .map((event) => event.key)
}

export function recipientSourceSummary(eventKeys: string[], catalog: LineEventDefinition[]) {
  const counts = new Map<LineNotificationSource, number>()
  eventKeys.forEach((key) => {
    const source = eventSourceForKey(key, catalog)
    counts.set(source, (counts.get(source) ?? 0) + 1)
  })
  return (['shopee', 'tiktok_shop', 'nextstep_marketplace'] as LineNotificationSource[])
    .filter((source) => counts.has(source))
    .map((source) => ({ source, count: counts.get(source) ?? 0 }))
}

export function eventSourceForKey(key: string, catalog: LineEventDefinition[]): LineNotificationSource {
  return catalog.find((event) => event.key === key)?.source ?? (key.startsWith('tiktok') ? 'tiktok_shop' : key.startsWith('nextstep') ? 'nextstep_marketplace' : 'shopee')
}

export function eventLabelForKey(key: string, catalog: LineEventDefinition[]) {
  return catalog.find((event) => event.key === key)?.label || key
}

export function eventGroupLabel(group: LineNotificationGroup) {
  switch (group) {
    case 'orders': return 'คำสั่งซื้อใหม่'
    case 'cancellations': return 'ยกเลิกและเอกสารหลังยกเลิก'
    case 'sml': return 'ผลการส่ง SML อัตโนมัติ'
    case 'settlements': return 'รับชำระ'
  }
}

export function sourceLabel(source: LineNotificationSource) {
  switch (source) {
    case 'tiktok_shop': return 'TikTok Shop'
    case 'nextstep_marketplace': return 'NextStep Marketplace'
    default: return 'Shopee'
  }
}

function sourceColor(source: LineNotificationSource) {
  switch (source) {
    case 'tiktok_shop': return '#111817'
    case 'nextstep_marketplace': return '#2563EB'
    default: return '#EE4D2D'
  }
}

export function sourceBadgeClass(source: LineNotificationSource) {
  switch (source) {
    case 'tiktok_shop': return 'border-[#111817]/30 bg-[#111817] text-white'
    case 'nextstep_marketplace': return 'border-blue-200 bg-blue-50 text-blue-700 dark:border-blue-900 dark:bg-blue-950/40 dark:text-blue-300'
    default: return 'border-[#EE4D2D]/30 bg-[#EE4D2D]/10 text-[#D53F22] dark:text-[#FF8A72]'
  }
}
