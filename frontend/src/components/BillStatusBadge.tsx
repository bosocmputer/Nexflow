import { StatusDot } from '@/components/common/StatusDot'
import { billStatusLabel } from '@/lib/labels'
import type { BillStatus } from '@/types'

// Variant map stays here (UI-only concern); labels come from lib/labels.ts
// so the badge text matches Bills/Dashboard/Logs everywhere.
const VARIANT: Record<string, 'success' | 'warning' | 'danger' | 'muted' | 'info' | 'primary'> = {
  pending:      'info',
  needs_review: 'warning',
  confirmed:    'primary',
  sent:         'success',
  failed:       'danger',
  skipped:      'muted',
}

export default function BillStatusBadge({
  status,
  short = true,
}: {
  status: BillStatus | string
  // Badges are space-constrained — default to short labels ("ล้มเหลว"
  // instead of "ส่ง SML ล้มเหลว"). Set short=false in dense detail
  // contexts where the full phrase fits.
  short?: boolean
}) {
  return <StatusDot variant={VARIANT[status] ?? 'muted'} label={billStatusLabel(status, short)} />
}
