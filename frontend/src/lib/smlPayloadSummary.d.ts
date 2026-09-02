export type SMLPayloadRecord = Record<string, unknown>

export function documentItems(payload?: SMLPayloadRecord | null): SMLPayloadRecord[]
export function documentLocation(payload?: SMLPayloadRecord | null): {
  whCode: unknown
  shelfCode: unknown
}
