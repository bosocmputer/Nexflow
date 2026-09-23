export type TikTokSettlementRouteSummary = {
  configured: boolean;
  doc_format_code?: string;
  passbook_code?: string;
  passbook_name?: string;
};

export function settlementDestinationLabel(
  route?: TikTokSettlementRouteSummary | null,
): string {
  if (!route?.configured) return "ยังไม่ได้ตั้งค่าเส้นทาง SML";

  const parts = ["เอกสาร SML"];
  if (route.doc_format_code) parts.push(`รูปแบบ ${route.doc_format_code}`);
  const passbook = route.passbook_name || route.passbook_code;
  if (passbook) parts.push(`บัญชี ${passbook}`);
  return parts.join(" · ");
}

export function settlementPrimaryActionLabel(
  status?: string,
  routeConfigured?: boolean,
): string {
  if (!routeConfigured) return "ยังไม่ได้ตั้งค่าเส้นทาง SML";
  if (status !== "ready") return "ยังสร้างเอกสารไม่ได้";
  return "ยืนยันสร้างเอกสารใน SML";
}
