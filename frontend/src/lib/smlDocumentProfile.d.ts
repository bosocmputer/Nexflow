export const SML_DOCUMENT_PROFILE_TOKENS: readonly string[]
export function validateProfileText(value: string, allowTokens?: boolean): string
export function resolveProfileTemplate(
  template: string,
  context: { channel?: string; order_ref?: string; bill_no?: string },
): string
