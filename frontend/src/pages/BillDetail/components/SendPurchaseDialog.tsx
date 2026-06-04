import { useEffect, useMemo, useState } from "react";
import { AlertTriangle, RefreshCw, Send } from "lucide-react";
import { Button } from "@/components/ui/button";
import { ActionSafetyPanel } from "@/components/common/ActionSafetyPanel";
import { Input } from "@/components/ui/input";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogFooter,
} from "@/components/ui/dialog";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import type { RetryBillPayload } from "@/hooks/useBills";
import type { Bill, SMLReadiness } from "@/types";
import {
  REMARK2_NONE,
  SML_REMARK2_OPTIONS,
  normalizeRemark2,
  remark2PayloadValue,
} from "@/lib/smlRemark2";
import { ENABLE_REMARK2 } from "@/lib/featureFlags";
import { isSMLReady, smlBlockedMessage } from "@/lib/sml-readiness";
import { PartyPicker, type Party } from "@/pages/ChannelDefaults/PartyPicker";
import { SMLMasterCodePicker } from "./SMLMasterCodePicker";
import { ShelfPicker, WarehousePicker } from "./WarehousePicker";

function payloadString(
  payload: Record<string, unknown> | null | undefined,
  key: string,
) {
  const value = payload?.[key];
  return typeof value === "string" ? value.trim() : "";
}

function payloadNumber(
  payload: Record<string, unknown> | null | undefined,
  key: string,
) {
  const value = payload?.[key];
  return typeof value === "number" && Number.isFinite(value) ? value : null;
}

function currentTimeHHMM() {
  const now = new Date();
  return `${String(now.getHours()).padStart(2, "0")}:${String(now.getMinutes()).padStart(2, "0")}`;
}

function firstPayloadLine(payload: Record<string, unknown> | null | undefined) {
  const details = payload?.details;
  if (
    Array.isArray(details) &&
    typeof details[0] === "object" &&
    details[0] !== null
  ) {
    return details[0] as Record<string, unknown>;
  }
  const items = payload?.items;
  if (
    Array.isArray(items) &&
    typeof items[0] === "object" &&
    items[0] !== null
  ) {
    return items[0] as Record<string, unknown>;
  }
  return null;
}

function routeDestination(route?: string, isSale = false) {
  if (route === "saleinvoice") {
    return { label: "ขาย -> ขายสินค้าและบริการ", code: "SI" };
  }
  if (route === "saleorder" || isSale) {
    return { label: "ขาย -> ใบสั่งขาย", code: "SO" };
  }
  return { label: "ซื้อ -> ใบสั่งซื้อ", code: "PO" };
}

const PURCHASE_INQUIRY_TYPE_OPTIONS = [
  { value: "0", label: "0: ซื้อสินค้าเงินเชื่อ" },
  { value: "1", label: "1: ซื้อสินค้าเงินสด" },
  { value: "2", label: "2: ซื้อสินค้าเงินเชื่อ (สินค้าบริการ)" },
  { value: "3", label: "3: ซื้อสินค้าเงินสด (สินค้าบริการ)" },
];

const SALE_INQUIRY_TYPE_OPTIONS = [
  { value: "0", label: "0: ขายเงินเชื่อ" },
  { value: "1", label: "1: ขายเงินสด" },
  { value: "2", label: "2: ขายเงินเชื่อ (สินค้าบริการ)" },
  { value: "3", label: "3: ขายเงินสด (สินค้าบริการ)" },
];

function rawString(
  payload: Record<string, unknown> | null | undefined,
  key: string,
) {
  const value = payload?.[key];
  return typeof value === "string" ? value.trim() : "";
}

function rawNumber(
  payload: Record<string, unknown> | null | undefined,
  key: string,
) {
  const value = payload?.[key];
  return typeof value === "number" && Number.isFinite(value) ? value : null;
}

function rawBool(
  payload: Record<string, unknown> | null | undefined,
  key: string,
) {
  const value = payload?.[key];
  return typeof value === "boolean" ? value : false;
}

function rawObject(
  payload: Record<string, unknown> | null | undefined,
  key: string,
) {
  const value = payload?.[key];
  return typeof value === "object" && value !== null && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : null;
}

function formatBaht(value: number | null) {
  if (value == null) return "";
  return `฿${value.toLocaleString("th-TH", { minimumFractionDigits: 2, maximumFractionDigits: 2 })}`;
}

function round2(value: number) {
  return Math.round((value + Number.EPSILON) * 100) / 100;
}

function orderIDFromRaw(raw: Record<string, unknown> | null | undefined) {
  return (
    rawString(raw, "order_id") ||
    rawString(raw, "shopee_order_id") ||
    rawString(raw, "order_no")
  );
}

interface Props {
  open: boolean;
  bill: Bill;
  onConfirm: (body: RetryBillPayload) => void;
  onCancel: () => void;
  onRegenerateDocNo?: () => Promise<string | null> | string | null | void;
  regeneratingDocNo?: boolean;
  smlReadiness?: SMLReadiness | null;
  smlReadinessLoading?: boolean;
}

export function SendPurchaseDialog({
  open,
  bill,
  onConfirm,
  onCancel,
  onRegenerateDocNo,
  regeneratingDocNo = false,
  smlReadiness,
  smlReadinessLoading = false,
}: Props) {
  const billType = bill.bill_type === "sale" ? "sale" : "purchase";
  const isSale = billType === "sale";
  const isPurchaseOrder = !isSale;
  const isShopeePurchaseEmail =
    bill.source === "shopee_shipped" && bill.bill_type === "purchase";
  const destination = routeDestination(bill.preview?.route, isSale);
  const documentName =
    bill.preview?.route === "saleinvoice"
      ? "ขายสินค้าและบริการ"
      : isSale
        ? "ใบสั่งขาย"
        : "ใบสั่งซื้อ";
  const defaults = bill.preview?.sml_defaults;
  const [party, setParty] = useState<Party | null>(null);
  const [docNo, setDocNo] = useState("");
  const [remark, setRemark] = useState("");
  const [branchCode, setBranchCode] = useState("");
  const [saleCode, setSaleCode] = useState("");
  const [docTime, setDocTime] = useState(currentTimeHHMM);
  const [whCode, setWhCode] = useState("");
  const [shelfCode, setShelfCode] = useState("");
  const [manualWarehouse, setManualWarehouse] = useState(false);
  const [vatTypeStr, setVatTypeStr] = useState("");
  const [vatRateStr, setVatRateStr] = useState("");
  const [inquiryTypeStr, setInquiryTypeStr] = useState("");
  const [remark2Str, setRemark2Str] = useState(REMARK2_NONE);

  const effectivePartyCode = party?.code ?? "";
  const parsedVatRate = Number(vatRateStr);
  const vatRateValid =
    vatRateStr.trim() !== "" &&
    Number.isFinite(parsedVatRate) &&
    parsedVatRate >= 0;
  const vatRateNum = vatRateValid ? parsedVatRate : 0;
  const paymentSummary = rawObject(bill.raw_data, "payment_summary");
  const paymentMethod = rawString(paymentSummary, "payment_method");
  const paymentPaidAmount = rawNumber(paymentSummary, "payment_paid_amount");
  const paymentDocRefAmount = rawString(paymentSummary, "doc_ref_amount");
  const paymentIsCard = rawBool(paymentSummary, "is_credit_debit_card");
  const sellerFromEmail = rawString(bill.raw_data, "seller_name");
  const orderID = orderIDFromRaw(bill.raw_data);
  const smlReady = isSMLReady(smlReadiness);
  const canConfirm =
    smlReady &&
    !!effectivePartyCode &&
    whCode.trim() !== "" &&
    shelfCode.trim() !== "" &&
    vatTypeStr !== "" &&
    vatRateValid &&
    (!isPurchaseOrder || inquiryTypeStr !== "") &&
    docTime.trim() !== "";
  const missingFields = useMemo(
    () =>
      [
        !effectivePartyCode
          ? isSale
            ? "ลูกค้า (cust_code, cust_name)"
            : "ผู้ขาย (cust_code, cust_name)"
          : "",
        whCode.trim() === "" ? "คลัง (wh_code)" : "",
        shelfCode.trim() === "" ? "พื้นที่เก็บ (shelf_code)" : "",
        vatTypeStr === "" ? "ประเภทภาษี (vat_type)" : "",
        !vatRateValid ? "อัตราภาษี (vat_rate)" : "",
        isPurchaseOrder && inquiryTypeStr === ""
          ? "ประเภทรายการซื้อ (inquiry_type)"
          : "",
        docTime.trim() === "" ? "เวลาเอกสาร (doc_time)" : "",
      ].filter(Boolean),
    [
      docTime,
      effectivePartyCode,
      inquiryTypeStr,
      isPurchaseOrder,
      isSale,
      shelfCode,
      vatRateValid,
      vatTypeStr,
      whCode,
    ],
  );
  const billTotal = useMemo(
    () =>
      (bill.items ?? []).reduce(
        (sum, item) =>
          sum +
          Math.max(
            (item.qty ?? 0) * (item.price ?? 0) -
              (item.discount_amount ?? 0),
            0,
          ),
        0,
      ),
    [bill.items],
  );
  const sendDisabledReason = !smlReady
    ? smlBlockedMessage(smlReadiness)
    : missingFields.length > 0
      ? `ต้องกรอกเพิ่มก่อนส่ง: ${missingFields.join(", ")}`
      : "";
  const hiddenCodeItems = useMemo(
    () =>
      (bill.items ?? []).filter(
        (item) => item.has_hidden_chars && item.item_code,
      ),
    [bill.items],
  );
  const smlTotalPreview = useMemo(() => {
    if (vatTypeStr === "") return null;
    const vatType = Number(vatTypeStr);
    const vatRate = Number.isFinite(vatRateNum) ? vatRateNum : 7;
    let totalValue = 0;
    let totalDiscount = 0;
    let totalNet = 0;
    let totalVat = 0;
    let totalExcVat = 0;

    for (const item of bill.items ?? []) {
      const qty = Number.isFinite(item.qty) ? item.qty : 0;
      const price =
        item.price != null && Number.isFinite(item.price) ? item.price : 0;
      const gross = round2(price * qty);
      const discount = Math.min(Math.max(item.discount_amount ?? 0, 0), gross);
      const net = round2(gross - discount);
      const rate = vatRate / 100;
      let vatAmount = 0;
      let excVat = net;
      if (vatType === 1) {
        excVat = round2(net / (1 + rate));
        vatAmount = round2(net - excVat);
      } else if (vatType !== 2) {
        vatAmount = round2(net * rate);
      }
      totalValue += gross;
      totalDiscount += discount;
      totalNet += net;
      totalVat += vatAmount;
      totalExcVat += excVat;
    }

    totalValue = round2(totalValue);
    totalDiscount = round2(totalDiscount);
    totalNet = round2(totalNet);
    totalVat = round2(totalVat);
    totalExcVat = round2(totalExcVat);
    const totalBeforeVat = vatType === 1 ? totalExcVat : totalNet;
    const totalAmount = vatType === 0 ? round2(totalNet + totalVat) : totalNet;
    const shopeePaidTotal =
      rawNumber(bill.raw_data, "paid_total_amount") ??
      rawNumber(bill.raw_data, "total_paid_amount") ??
      paymentPaidAmount;
    const paidDelta =
      shopeePaidTotal != null ? round2(totalAmount - shopeePaidTotal) : null;

    return {
      totalValue,
      totalDiscount,
      totalBeforeVat,
      totalVat,
      totalAmount,
      shopeePaidTotal,
      paidDelta,
    };
  }, [bill.items, bill.raw_data, paymentPaidAmount, vatRateNum, vatTypeStr]);

  useEffect(() => {
    if (!open) return;
    const payload = bill.sml_payload;
    const firstLine = firstPayloadLine(payload);
    const partyCode = payloadString(payload, "cust_code");
    const partyName = payloadString(payload, "supplier_name") || partyCode;
    setParty(
      partyCode
        ? { code: partyCode, name: partyName }
        : defaults?.party_code
          ? { code: defaults.party_code, name: defaults.party_name || defaults.party_code }
          : null,
    );
    setDocNo(
      payloadString(payload, "doc_no") ||
        bill.sml_doc_no ||
        bill.preview?.doc_no ||
        "",
    );
    setRemark(bill.remark ?? "");
    setBranchCode(defaults?.branch_code ?? "");
    setSaleCode(defaults?.sale_code ?? "");
    setDocTime(currentTimeHHMM());
    setWhCode(
      payloadString(payload, "wh_code") ||
        payloadString(firstLine, "wh_code") ||
        defaults?.wh_code ||
        "",
    );
    setShelfCode(
      payloadString(payload, "shelf_code") ||
        payloadString(firstLine, "shelf_code") ||
        defaults?.shelf_code ||
        "",
    );
    setManualWarehouse(false);
    const vatType = payloadNumber(payload, "vat_type");
    const vatRate = payloadNumber(payload, "vat_rate");
    const inquiryType = payloadNumber(payload, "inquiry_type");
    setRemark2Str(normalizeRemark2(
      payloadString(payload, "remark_2") || defaults?.remark_2 || ""
    ));
    setVatTypeStr(
      vatType != null
        ? String(vatType)
        : typeof defaults?.vat_type === "number" && defaults.vat_type >= 0
          ? String(defaults.vat_type)
          : "",
    );
    setVatRateStr(
      vatRate != null
        ? String(vatRate)
        : typeof defaults?.vat_rate === "number" && defaults.vat_rate >= 0
          ? String(defaults.vat_rate)
          : "7",
    );
    setInquiryTypeStr(
      inquiryType != null
        ? String(inquiryType)
        : typeof defaults?.inquiry_type === "number" && defaults.inquiry_type >= 0
          ? String(defaults.inquiry_type)
          : "",
    );
  }, [open, bill.id, bill.remark, bill.sml_doc_no, bill.sml_payload, defaults]);

  const handleConfirm = () => {
    if (!canConfirm) return;
    onConfirm({
      party_code: effectivePartyCode,
      party_name: party?.name,
      doc_no: docNo.trim() || undefined,
      remark: isShopeePurchaseEmail ? undefined : remark.trim() || undefined,
      remark_2: remark2PayloadValue(remark2Str),
      branch_code: branchCode.trim() || undefined,
      sale_code: saleCode.trim() || undefined,
      doc_time: docTime.trim() || undefined,
      wh_code: whCode.trim(),
      shelf_code: shelfCode.trim(),
      vat_type: Number(vatTypeStr),
      vat_rate: vatRateNum,
      inquiry_type: inquiryTypeStr !== "" ? Number(inquiryTypeStr) : undefined,
    });
  };

  const handleRegenerateDocNo = async () => {
    if (!onRegenerateDocNo) return;
    const nextDocNo = await onRegenerateDocNo();
    if (typeof nextDocNo === "string" && nextDocNo.trim()) {
      setDocNo(nextDocNo.trim());
    }
  };

  return (
    <Dialog
      open={open}
      onOpenChange={(v) => {
        if (!v) onCancel();
      }}
    >
      <DialogContent className="grid max-h-[90vh] grid-rows-[auto_minmax(0,1fr)_auto] sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>ยืนยันการส่ง {documentName} ไปยัง SML</DialogTitle>
        </DialogHeader>

        <div className="-mx-6 space-y-4 overflow-y-auto px-6 py-2">
          <div className="rounded-md border border-info/25 bg-info/[0.04] px-3 py-2 text-xs text-muted-foreground">
            <div className="font-medium text-foreground">
              ปลายทาง SML / รูปแบบเอกสาร (doc_format_code): {destination.label}{" "}
              · {bill.preview?.doc_format_code || destination.code}
            </div>
            <div className="mt-0.5">
              เลือกค่าที่จะใช้กับบิลใบนี้เท่านั้น
              ระบบจะไม่บันทึกค่าเหล่านี้กลับไปเป็นค่าของช่องทาง
            </div>
          </div>

          <ActionSafetyPanel
            title={`ตรวจผลกระทบก่อนส่ง ${documentName} เข้า SML`}
            description="เมื่อส่งเข้า SML สำเร็จแล้ว Nexflow ไม่สามารถ rollback เอกสารใน SML ให้เองได้ หากต้องแก้หรือยกเลิกต้องดำเนินการใน SML ตามขั้นตอนของร้าน"
            tone="warning"
            items={[
              {
                label: "เอกสาร",
                value: bill.sml_doc_no || bill.preview?.doc_no || bill.id.slice(0, 8),
                detail: orderID ? `Order ${orderID}` : "ใช้บิลใบนี้เท่านั้น",
              },
              {
                label: "ปลายทาง",
                value: `${destination.label} · ${bill.preview?.doc_format_code || destination.code}`,
                detail: bill.preview?.endpoint || "ใช้ route จาก backend preview ปัจจุบัน",
              },
              {
                label: "จำนวนรายการ",
                value: `${(bill.items ?? []).length.toLocaleString("th-TH")} รายการ`,
                detail: `ยอดรวมประมาณ ${formatBaht(billTotal) || "—"}`,
              },
              {
                label: "เลขเอกสาร",
                value: docNo || "ให้ระบบออกเลขตอนส่ง",
                detail: bill.sml_doc_no
                  ? "Retry จะใช้เลขเดิมที่บันทึกไว้"
                  : "ถ้า SML แจ้งเลขซ้ำ ให้ดึงเลขล่าสุดแล้วส่งใหม่",
              },
            ]}
          />

          {!smlReady && (
            <div className="rounded-md border border-warning/35 bg-warning/[0.08] px-3 py-2 text-xs">
              <div className="flex items-start gap-2">
                <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0 text-warning" />
                <div className="min-w-0 flex-1">
                  <div className="font-medium text-foreground">
                    ยังส่ง SML ไม่ได้: ฐานข้อมูลร้านยังไม่พร้อม
                  </div>
                  <div className="mt-0.5 text-muted-foreground">
                    {smlReadinessLoading
                      ? "กำลังตรวจสถานะ SML ของร้านนี้"
                      : smlBlockedMessage(smlReadiness)}{" "}
                    เปิดเครื่อง SML/Postgres ของร้านนี้
                    แล้วกดตรวจอีกครั้งบนแถบแจ้งเตือนด้านบน
                  </div>
                </div>
              </div>
            </div>
          )}

          {hiddenCodeItems.length > 0 && (
            <div className="rounded-md border border-warning/35 bg-warning/[0.08] px-3 py-2 text-xs">
              <div className="flex items-start gap-2">
                <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0 text-warning" />
                <div className="min-w-0 flex-1">
                  <div className="font-medium text-foreground">
                    พบรหัสสินค้าที่มีอักขระมองไม่เห็น
                  </div>
                  <div className="mt-0.5 text-muted-foreground">
                    รหัสเหล่านี้มีอยู่ใน SML จึงยังส่งได้
                    แต่ควรตรวจสอบก่อนยืนยัน
                  </div>
                  <div className="mt-2 space-y-1">
                    {hiddenCodeItems.slice(0, 8).map((item) => (
                      <div key={item.id} className="truncate">
                        <code className="font-mono">{item.item_code}</code>
                        {item.clean_item_code && (
                          <span className="text-muted-foreground">
                            {" "}
                            · ควรเป็น{" "}
                            <code className="font-mono">
                              {item.clean_item_code}
                            </code>
                          </span>
                        )}
                        <span className="text-muted-foreground">
                          {" "}
                          · {item.raw_name}
                        </span>
                      </div>
                    ))}
                    {hiddenCodeItems.length > 8 && (
                      <div className="text-muted-foreground">
                        และอีก {hiddenCodeItems.length - 8} รายการ
                      </div>
                    )}
                  </div>
                </div>
              </div>
            </div>
          )}

          <div className="space-y-1.5">
            <Label>
              {isSale
                ? "ลูกค้า (cust_code, cust_name)"
                : "ผู้ขาย (cust_code, cust_name)"}{" "}
              <span className="text-destructive">*</span>
            </Label>
            <PartyPicker
              billType={billType}
              value={party}
              onChange={setParty}
            />
            {!effectivePartyCode && (
              <p className="text-[11px] text-warning">
                ต้องเลือก
                {isSale
                  ? "ลูกค้า (cust_code, cust_name)"
                  : "ผู้ขาย (cust_code, cust_name)"}
                ก่อนส่งเข้า SML
              </p>
            )}
          </div>

          <div className="grid gap-2.5 rounded-md border border-border bg-muted/20 p-3 sm:grid-cols-2">
            <div className="space-y-1">
              <div className="flex items-center justify-between gap-2">
                <Label className="text-xs">เลขเอกสาร SML (doc_no)</Label>
                <Button
                  type="button"
                  variant="ghost"
                  size="sm"
                  className="h-6 gap-1 px-1.5 text-[11px]"
                  onClick={handleRegenerateDocNo}
                  disabled={
                    !onRegenerateDocNo || regeneratingDocNo || !smlReady
                  }
                  title="ดึงเลขล่าสุดจาก SML มาใส่ในช่องนี้ โดยยังไม่บันทึกลงบิล"
                >
                  <RefreshCw
                    className={`h-3 w-3 ${regeneratingDocNo ? "animate-spin" : ""}`}
                  />
                  ดึงเลขล่าสุด
                </Button>
              </div>
              <Input
                value={docNo}
                readOnly
                placeholder="เว้นว่างเพื่อให้ระบบออกเลข running ตอนส่ง"
                className="font-mono bg-muted/50 cursor-not-allowed"
              />
              <p className="text-[10px] text-muted-foreground">
                ปุ่มนี้ดึงเลขล่าสุดมาแสดงใน dialog เท่านั้น
                เลขที่จะส่งคือค่าที่อยู่ในช่องนี้
              </p>
            </div>
            <div className="space-y-1">
              <Label className="text-xs">
                เวลาเอกสาร (doc_time){" "}
                <span className="text-destructive">*</span>
              </Label>
              <Input
                value={docTime}
                readOnly
                placeholder="เช่น 09:00"
                className="font-mono bg-muted/50 cursor-not-allowed"
              />
              <p className="text-[10px] text-muted-foreground">
                ใช้เวลาปัจจุบันตอนเปิด dialog
              </p>
            </div>
            <div className="space-y-1">
              <div className="flex items-center justify-between gap-2">
                <Label className="text-xs">
                  คลัง (wh_code) <span className="text-destructive">*</span>
                </Label>
                <Button
                  type="button"
                  variant="ghost"
                  size="sm"
                  className="h-6 px-1.5 text-[11px]"
                  onClick={() => setManualWarehouse((v) => !v)}
                >
                  {manualWarehouse ? "เลือกจาก SML" : "พิมพ์รหัสเอง"}
                </Button>
              </div>
              {manualWarehouse ? (
                <Input
                  value={whCode}
                  onChange={(e) => {
                    setWhCode(e.target.value.toUpperCase());
                    setShelfCode("");
                  }}
                  placeholder="เช่น WH-01"
                  className="font-mono"
                />
              ) : (
                <WarehousePicker
                  value={whCode}
                  onChange={(warehouse) => {
                    setWhCode(warehouse.code);
                    setShelfCode("");
                  }}
                />
              )}
              <p className="text-[10px] text-muted-foreground">
                เลือกจากคลังใน SML หรือพิมพ์เองถ้า service ยังไม่พร้อม
              </p>
            </div>
            <div className="space-y-1">
              <Label className="text-xs">
                พื้นที่เก็บ (shelf_code){" "}
                <span className="text-destructive">*</span>
              </Label>
              {manualWarehouse ? (
                <Input
                  value={shelfCode}
                  onChange={(e) => setShelfCode(e.target.value.toUpperCase())}
                  placeholder="เช่น SH-01"
                  className="font-mono"
                />
              ) : (
                <ShelfPicker
                  warehouseCode={whCode}
                  value={shelfCode}
                  onChange={(shelf) => setShelfCode(shelf.code)}
                />
              )}
              <p className="text-[10px] text-muted-foreground">
                พื้นที่เก็บจะถูกกรองตามคลังที่เลือก
              </p>
            </div>
            <div className="space-y-1">
              <Label className="text-xs">
                ประเภทภาษี (vat_type){" "}
                <span className="text-destructive">*</span>
              </Label>
              <Select value={vatTypeStr} onValueChange={setVatTypeStr}>
                <SelectTrigger className="h-9 text-sm">
                  <SelectValue placeholder="เลือกประเภทภาษี" />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="0">0: แยกนอก</SelectItem>
                  <SelectItem value="1">1: รวมใน</SelectItem>
                  <SelectItem value="2">2: ศูนย์%</SelectItem>
                </SelectContent>
              </Select>
            </div>
            <div className="space-y-1">
              <Label className="text-xs">
                อัตราภาษี (vat_rate) <span className="text-destructive">*</span>
              </Label>
              <Input
                value={vatRateStr}
                onChange={(e) => setVatRateStr(e.target.value)}
                placeholder="เช่น 7"
                inputMode="decimal"
                className="font-mono"
              />
            </div>
            <div className="space-y-1">
              <Label className="text-xs">
                {isPurchaseOrder ? "ประเภทรายการซื้อ" : "ประเภทรายการขาย"} (inquiry_type)
                {isPurchaseOrder && <span className="text-destructive"> *</span>}
              </Label>
              <Select value={inquiryTypeStr} onValueChange={setInquiryTypeStr}>
                <SelectTrigger className="h-9 text-sm">
                  <SelectValue placeholder={isPurchaseOrder ? "เลือกประเภทรายการ" : "ไม่ระบุ (ไม่บังคับ)"}>
                    {(isPurchaseOrder ? PURCHASE_INQUIRY_TYPE_OPTIONS : SALE_INQUIRY_TYPE_OPTIONS).find((o) => o.value === inquiryTypeStr)?.label}
                  </SelectValue>
                </SelectTrigger>
                <SelectContent>
                  {(isPurchaseOrder ? PURCHASE_INQUIRY_TYPE_OPTIONS : SALE_INQUIRY_TYPE_OPTIONS).map((option) => (
                    <SelectItem key={option.value} value={option.value}>
                      {option.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            {ENABLE_REMARK2 && (
              <div className="space-y-1">
                <Label className="text-xs">สถานะเอกสาร (remark_2)</Label>
                <Select value={remark2Str} onValueChange={setRemark2Str}>
                  <SelectTrigger className="h-9 text-sm">
                    <SelectValue placeholder="ไม่ระบุ" />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value={REMARK2_NONE}>ไม่ระบุ</SelectItem>
                    {SML_REMARK2_OPTIONS.map((option) => (
                      <SelectItem key={option.value} value={option.value}>
                        {option.label}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
            )}
            {!vatRateValid && (
              <div className="rounded-md bg-warning/[0.08] px-2.5 py-1.5 text-[11px] text-warning sm:col-span-2">
                ตั้งค่าอัตราภาษีใน /settings/channels หรือกรอกใน dialog
                นี้ก่อนส่ง
              </div>
            )}
            {smlTotalPreview && (
              <div className="rounded-md border border-border bg-background px-3 py-2 text-xs sm:col-span-2">
                <div className="font-medium text-foreground">
                  ตรวจยอดที่จะเข้า SML
                </div>
                <div className="mt-2 grid gap-x-4 gap-y-1 sm:grid-cols-2">
                  <div className="flex items-center justify-between gap-3">
                    <span className="text-muted-foreground">
                      สินค้า/ค่าขนส่งก่อนส่วนลด
                    </span>
                    <span className="font-mono text-foreground">
                      {formatBaht(smlTotalPreview.totalValue)}
                    </span>
                  </div>
                  <div className="flex items-center justify-between gap-3">
                    <span className="text-muted-foreground">ส่วนลดรวม</span>
                    <span className="font-mono text-foreground">
                      {formatBaht(smlTotalPreview.totalDiscount)}
                    </span>
                  </div>
                  <div className="flex items-center justify-between gap-3">
                    <span className="text-muted-foreground">
                      ยอดก่อนภาษี (total_before_vat)
                    </span>
                    <span className="font-mono text-foreground">
                      {formatBaht(smlTotalPreview.totalBeforeVat)}
                    </span>
                  </div>
                  <div className="flex items-center justify-between gap-3">
                    <span className="text-muted-foreground">
                      ภาษี (total_vat_value)
                    </span>
                    <span className="font-mono text-foreground">
                      {formatBaht(smlTotalPreview.totalVat)}
                    </span>
                  </div>
                </div>
                <div className="mt-2 flex items-center justify-between gap-3 border-t border-border pt-2">
                  <span className="font-medium text-foreground">
                    ยอดรวม SML
                  </span>
                  <span className="font-mono font-semibold text-foreground">
                    {formatBaht(smlTotalPreview.totalAmount)}
                  </span>
                </div>
                {smlTotalPreview.shopeePaidTotal != null &&
                  Math.abs(smlTotalPreview.paidDelta ?? 0) >= 0.01 && (
                    <div className="mt-2 rounded-md bg-warning/[0.08] px-2.5 py-1.5 text-[11px] text-warning">
                      ยอดชำระในอีเมล{" "}
                      {formatBaht(smlTotalPreview.shopeePaidTotal)} ต่างจากยอด
                      SML {formatBaht(smlTotalPreview.totalAmount)}{" "}
                      ตามประเภทภาษีที่เลือก
                    </div>
                  )}
              </div>
            )}
            <details className="space-y-2 rounded-md border border-border bg-background px-3 py-2 sm:col-span-2">
              <summary className="cursor-pointer text-xs font-medium text-muted-foreground">
                ตัวเลือกเพิ่มเติม: สาขา (branch_code) / พนักงานขาย (sale_code)
                (ไม่บังคับ)
              </summary>
              <div className="mt-3 grid gap-3 sm:grid-cols-2">
                <div className="space-y-1">
                  <Label className="text-xs">สาขา (branch_code)</Label>
                  <SMLMasterCodePicker
                    kind="branch"
                    value={branchCode}
                    onChange={setBranchCode}
                  />
                </div>
                <div className="space-y-1">
                  <Label className="text-xs">พนักงานขาย (sale_code)</Label>
                  <SMLMasterCodePicker
                    kind="sale"
                    value={saleCode}
                    onChange={setSaleCode}
                  />
                </div>
              </div>
            </details>
            <div className="rounded-md bg-background/70 px-2.5 py-1.5 text-[11px] text-muted-foreground sm:col-span-2">
              เลขเอกสารจะใช้ค่าที่แสดงอยู่ใน dialog นี้ ถ้า SML แจ้งเลขซ้ำ
              ให้กดดึงเลขล่าสุดแล้วส่งใหม่
            </div>
          </div>

          {isShopeePurchaseEmail && (
            <div className="rounded-md border border-info/25 bg-info/[0.04] px-3 py-2.5 text-xs">
              <div className="font-medium text-foreground">
                ข้อมูลที่จะส่งไปหัวเอกสาร SML จากอีเมล Shopee
              </div>
              <div className="mt-2 grid gap-2 sm:grid-cols-2">
                <div>
                  <div className="text-muted-foreground">
                    ผู้ขาย → หมายเหตุ (remark)
                  </div>
                  <div className="font-medium text-foreground">
                    {sellerFromEmail || "ไม่พบผู้ขายในอีเมล"}
                  </div>
                </div>
                <div>
                  <div className="text-muted-foreground">
                    หมายเลขคำสั่งซื้อ → หมายเหตุ 5 (remark_5)
                  </div>
                  <div className="font-mono font-medium text-foreground">
                    {orderID || "ไม่พบหมายเลขคำสั่งซื้อ"}
                  </div>
                </div>
                <div>
                  <div className="text-muted-foreground">วิธีชำระเงิน</div>
                  <div className="font-medium text-foreground">
                    {paymentMethod || "ไม่พบรายละเอียดการชำระเงินในอีเมล"}
                  </div>
                </div>
                <div>
                  <div className="text-muted-foreground">
                    เลขอ้างอิง (doc_ref)
                  </div>
                  <div className="font-medium text-foreground">
                    {paymentIsCard
                      ? paymentDocRefAmount
                        ? `${paymentDocRefAmount} (${formatBaht(paymentPaidAmount)})`
                        : "เป็นบัตรเครดิต/เดบิต แต่ไม่พบจำนวนเงินที่จ่าย"
                      : paymentMethod
                        ? "ไม่ใช่บัตรเครดิต/เดบิต จึงไม่ส่ง doc_ref"
                        : "ไม่พบรายละเอียดการชำระเงินในอีเมล"}
                  </div>
                </div>
              </div>
              <div className="mt-2 text-[11px] text-muted-foreground">
                หมายเหตุ SML ของบิล Shopee ซื้อจะใช้ผู้ขายจากอีเมลอัตโนมัติ
                เพื่อไม่ให้ชนกับ requirement หัวเอกสาร
              </div>
            </div>
          )}

          {!isShopeePurchaseEmail && (
            <div className="space-y-1.5">
              <Label htmlFor="remark">หมายเหตุ (remark)</Label>
              <textarea
                id="remark"
                value={remark}
                onChange={(e) => setRemark(e.target.value)}
                placeholder="หมายเหตุสำหรับ SML (ถ้ามี)"
                rows={3}
                className="w-full rounded-md border border-input bg-background px-3 py-2 text-sm placeholder:text-muted-foreground focus:outline-none focus:ring-2 focus:ring-ring resize-none"
              />
            </div>
          )}
          {missingFields.length > 0 && (
            <div className="flex items-start gap-2 rounded-md border border-warning/35 bg-warning/[0.07] px-3 py-2 text-xs text-warning">
              <AlertTriangle className="mt-0.5 h-3.5 w-3.5 shrink-0" />
              <div>ต้องกรอกเพิ่มก่อนส่ง: {missingFields.join(", ")}</div>
            </div>
          )}
          {!smlReady && (
            <div className="flex items-start gap-2 rounded-md border border-warning/35 bg-warning/[0.07] px-3 py-2 text-xs text-warning">
              <AlertTriangle className="mt-0.5 h-3.5 w-3.5 shrink-0" />
              <div>{smlBlockedMessage(smlReadiness)}</div>
            </div>
          )}
        </div>

        <DialogFooter className="gap-2">
          <Button type="button" variant="outline" onClick={onCancel}>
            ยกเลิก
          </Button>
          <Button
            type="button"
            onClick={handleConfirm}
            disabled={!canConfirm}
            className="gap-2"
            title={sendDisabledReason || undefined}
          >
            <Send className="h-4 w-4" />
            ส่งเข้า SML 1 ใบ
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
