export type SMLPayloadRecord = Record<string, unknown>

export function documentItems(payload?: SMLPayloadRecord | null): SMLPayloadRecord[]
export function documentLocation(payload?: SMLPayloadRecord | null): {
  whCode: unknown
  shelfCode: unknown
}
export function documentSendPresentation(bill?: Record<string, unknown> | null): {
  complete: boolean
  headline: string
  detail: string
}
