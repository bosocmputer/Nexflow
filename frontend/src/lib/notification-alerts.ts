import type { AppNotification } from '@/lib/notifications-store'

type OrderAlertNotification = Pick<AppNotification, 'dedupe_key'>

export function isOrderAlertNotification(notification: OrderAlertNotification): boolean {
  const key = notification.dedupe_key?.trim() ?? ''
  return key.startsWith('nextstep:new_order:')
    || key.startsWith('shopee:new_order:')
    || key.startsWith('tiktok_shop:new_order:')
}
