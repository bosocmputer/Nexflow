export function shouldOpenTimelineFromQuery(
  focusedOrderSN: string,
  timelineOpen: boolean,
  dismissedOrderSN: string,
) {
  const orderSN = focusedOrderSN.trim()
  if (!orderSN || timelineOpen) return false
  return orderSN !== dismissedOrderSN.trim()
}
