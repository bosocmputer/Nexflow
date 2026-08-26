export function shouldMergeAutoSMLSuccessStatus(
  erpStatus: string,
  smlDocNo?: string,
  autoSMLStatus?: string,
) {
  return erpStatus.trim().toLowerCase() === 'sent'
    && Boolean(smlDocNo?.trim())
    && autoSMLStatus?.trim().toLowerCase() === 'succeeded'
}
