import { useCallback, useEffect, useRef, useState } from "react";
import dayjs from "dayjs";
import {
  AlertTriangle,
  CheckCircle2,
  ChevronRight,
  FileText,
  ReceiptText,
  RefreshCw,
  Store,
} from "lucide-react";
import { toast } from "sonner";
import { Link } from "react-router-dom";

import client from "@/api/client";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Checkbox } from "@/components/ui/checkbox";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Label } from "@/components/ui/label";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  DateRangePicker,
  type DateRangePreset,
} from "@/components/common/DateRangePicker";
import {
  SMLSendProgressDialog,
  type SMLSendProgressStatus,
} from "@/components/common/SMLSendProgressDialog";
import { suppressNotificationToast } from "@/lib/notification-toast-suppression";
import {
  settlementDestinationLabel,
  settlementPrimaryActionLabel,
} from "@/lib/tiktok-settlement-presentation";
import { resolveTikTokSettlementShopID } from "@/lib/tiktok-settlement";
import { tiktokOrderDetailPath } from "@/lib/tiktok-shop-operations";
import { cn } from "@/lib/utils";

type Shop = {
  shop_id: string;
  shop_name: string;
  shop_code?: string;
  disabled?: boolean;
};
type Item = {
  id: string;
  order_id: string;
  order_snapshot_available: boolean;
  bill_id?: string;
  sml_invoice_doc_no?: string;
  settlement_amount: number;
  status: string;
  block_reason?: string;
};
type Run = {
  id: string;
  shop_id: string;
  shop_label: string;
  statement_id: string;
  payment_id: string;
  payment_status: string;
  currency: string;
  payment_time?: string;
  statement_time?: string;
  total_settlement_amount: number;
  invoice_amount_total: number;
  fee_amount_total: number;
  status: string;
  config_version: number;
  item_count?: number;
  blocked_item_count?: number;
  rc_doc_no?: string;
  error_msg?: string;
  anomaly_reason?: string;
  items?: Item[];
};
type Counts = {
  processing?: number;
  ready?: number;
  needs_review?: number;
  sent?: number;
  failed?: number;
  total: number;
};
type RouteSummary = {
  configured: boolean;
  doc_format_code?: string;
  passbook_code?: string;
  passbook_name?: string;
};
type ImportNotice = {
  message: string;
  imported_count?: number;
  paid_count?: number;
  processing_count?: number;
  failed_count?: number;
};
type MissingOrderImportResult = {
  data: Run;
  imported_count: number;
  remaining_count: number;
  message: string;
};
type MissingOrderImportNotice = {
  kind: "success" | "error";
  message: string;
  importedCount?: number;
  remainingCount?: number;
};
type SendProgress = {
  open: boolean;
  runID: string | null;
  status: SMLSendProgressStatus;
  docNo: string | null;
  error: string | null;
};
type BatchPreview = {
  id?: string;
  shop_id: string;
  shop_label: string;
  currency: string;
  run_ids: string[];
  statement_ids: string[];
  statement_count: number;
  order_count: number;
  settlement_amount: number;
  invoice_amount: number;
  fee_amount: number;
  selection_digest: string;
  status?: string;
  rc_doc_no?: string;
  error_msg?: string;
};

const money = (value?: number, currency = "THB") =>
  new Intl.NumberFormat("th-TH", {
    style: "currency",
    currency: /^[A-Z]{3}$/.test(currency) ? currency : "THB",
  }).format(Number(value ?? 0));
const statusMeta: Record<string, { text: string; className: string }> = {
  importing: {
    text: "กำลังดึงข้อมูล",
    className: "bg-muted text-muted-foreground",
  },
  reconciling: { text: "กำลังตรวจข้อมูล", className: "bg-info/15 text-info" },
  ready: { text: "พร้อมสร้างเอกสาร", className: "bg-success/15 text-success" },
  needs_review: {
    text: "ต้องตรวจข้อมูล",
    className: "bg-warning/15 text-warning",
  },
  sending: { text: "กำลังสร้างเอกสาร", className: "bg-info/15 text-info" },
  sent: { text: "สร้างเอกสารแล้ว", className: "bg-success/15 text-success" },
  failed: {
    text: "ดำเนินการไม่สำเร็จ",
    className: "bg-destructive/15 text-destructive",
  },
  unknown_result: {
    text: "ต้องตรวจผลใน SML",
    className: "bg-warning/15 text-warning",
  },
  superseded: {
    text: "มีข้อมูลใหม่กว่า",
    className: "bg-muted text-muted-foreground",
  },
};
const paymentStatusText: Record<string, string> = {
  PAID: "TikTok แจ้งว่าโอนแล้ว",
  SETTLED: "TikTok ยืนยัน settlement แล้ว",
  PROCESSING: "TikTok กำลังดำเนินการ",
  FAILED: "TikTok โอนไม่สำเร็จ",
};
const statementDate = (run: Pick<Run, "statement_time" | "payment_time">) =>
  run.statement_time || run.payment_time;
const statementPresets: DateRangePreset[] = [
  {
    label: "วันนี้",
    getRange: () => ({
      from: dayjs().format("YYYY-MM-DD"),
      to: dayjs().format("YYYY-MM-DD"),
    }),
  },
  {
    label: "7 วัน",
    getRange: () => ({
      from: dayjs().subtract(6, "day").format("YYYY-MM-DD"),
      to: dayjs().format("YYYY-MM-DD"),
    }),
  },
  {
    label: "15 วัน",
    getRange: () => ({
      from: dayjs().subtract(14, "day").format("YYYY-MM-DD"),
      to: dayjs().format("YYYY-MM-DD"),
    }),
  },
  {
    label: "31 วัน",
    getRange: () => ({
      from: dayjs().subtract(30, "day").format("YYYY-MM-DD"),
      to: dayjs().format("YYYY-MM-DD"),
    }),
  },
];
const formatDate = (value?: string) =>
  value ? dayjs(value).format("DD/MM/YY HH:mm") : "-";
const statusReason = (run: Run) =>
  run.anomaly_reason ||
  run.error_msg ||
  (run.status === "needs_review" && run.blocked_item_count
    ? `มี ${run.blocked_item_count} รายการที่ต้องตรวจ`
    : (paymentStatusText[run.payment_status] ?? run.payment_status));
function TikTokChip() {
  return (
    <Badge className="border-[#111817] bg-[#111817] text-white hover:bg-[#111817]">
      TikTok
    </Badge>
  );
}

export default function TikTokSettlement() {
  const [shops, setShops] = useState<Shop[]>([]);
  const [shopID, setShopID] = useState("");
  const [runs, setRuns] = useState<Run[]>([]);
  const [counts, setCounts] = useState<Counts>({ total: 0 });
  const [loading, setLoading] = useState(true);
  const [selected, setSelected] = useState<Run | null>(null);
  const [route, setRoute] = useState<RouteSummary | null>(null);
  const [sendConfirmOpen, setSendConfirmOpen] = useState(false);
  const [bankEvidenceConfirmed, setBankEvidenceConfirmed] = useState(false);
  const [sending, setSending] = useState(false);
  const [sendProgress, setSendProgress] = useState<SendProgress>({
    open: false,
    runID: null,
    status: "sending",
    docNo: null,
    error: null,
  });
  const [selectedRunIDs, setSelectedRunIDs] = useState<string[]>([]);
  const [batchPreview, setBatchPreview] = useState<BatchPreview | null>(null);
  const [batchConfirmOpen, setBatchConfirmOpen] = useState(false);
  const [bankAmount, setBankAmount] = useState("");
  const [bankReference, setBankReference] = useState("");
  const [batchBankConfirmed, setBatchBankConfirmed] = useState(false);
  const [batchSending, setBatchSending] = useState(false);
  const [batchProgress, setBatchProgress] = useState<{
    open: boolean;
    batchID: string | null;
    status: SMLSendProgressStatus;
    docNo: string | null;
    error: string | null;
  }>({ open: false, batchID: null, status: "sending", docNo: null, error: null });
  const [importOpen, setImportOpen] = useState(false);
  const [importShopID, setImportShopID] = useState("");
  const [importFrom, setImportFrom] = useState(
    dayjs().subtract(14, "day").format("YYYY-MM-DD"),
  );
  const [importTo, setImportTo] = useState(dayjs().format("YYYY-MM-DD"));
  const [importNotice, setImportNotice] = useState<ImportNotice | null>(null);
  const [importingStatements, setImportingStatements] = useState(false);
  const [importingMissingOrders, setImportingMissingOrders] = useState(false);
  const [missingOrderImportNotice, setMissingOrderImportNotice] =
    useState<MissingOrderImportNotice | null>(null);
  const [from, setFrom] = useState(
    dayjs().subtract(14, "day").format("YYYY-MM-DD"),
  );
  const [to, setTo] = useState(dayjs().format("YYYY-MM-DD"));
  const [runStatus, setRunStatus] = useState("all");
  const requestSequence = useRef(0);
  const resolvedShopID = resolveTikTokSettlementShopID(shopID, shops);
  const resolvedImportShopID = resolveTikTokSettlementShopID(
    importShopID,
    shops,
  );

  const load = useCallback(async () => {
    const sequence = ++requestSequence.current;
    setLoading(true);
    try {
      const params = {
        ...(shopID ? { shop_id: shopID } : {}),
        date_from: from,
        date_to: to,
        ...(runStatus !== "all" ? { status: runStatus } : {}),
      };
      const summaryParams = {
        ...(shopID ? { shop_id: shopID } : {}),
        date_from: from,
        date_to: to,
      };
      const [connections, list, summary] = await Promise.all([
        client.get<{ data: Shop[] }>("/api/tiktok-shop-api/local-connections"),
        client.get<{ data: Run[] }>("/api/tiktok-settlements", { params }),
        client.get<Counts>("/api/tiktok-settlements/counts", {
          params: summaryParams,
        }),
      ]);
      if (sequence !== requestSequence.current) return;
      const active = (connections.data.data ?? []).filter(
        (shop) => !shop.disabled,
      );
      setShops(active);
      if (!shopID && active.length === 1) setShopID(active[0].shop_id);
      setRuns(list.data.data ?? []);
      setCounts(summary.data);
    } catch (error: any) {
      if (sequence === requestSequence.current)
        toast.error(
          error?.response?.data?.error?.message ??
            "โหลดรายการรับชำระ TikTok Shop ไม่สำเร็จ",
        );
    } finally {
      if (sequence === requestSequence.current) setLoading(false);
    }
  }, [from, runStatus, shopID, to]);
  useEffect(() => {
    void load();
  }, [shopID, from, to, runStatus]);
  const openImport = () => {
    setImportShopID(resolvedShopID);
    setImportFrom(from);
    setImportTo(to);
    setImportOpen(true);
  };
  const importStatements = async () => {
    if (!resolvedImportShopID) {
      toast.error("กรุณาเลือกร้านก่อนดึง Statement");
      return;
    }
    if (dayjs(importTo).diff(dayjs(importFrom), "day") > 30) {
      toast.error("เลือกช่วงข้อมูลได้ไม่เกิน 31 วันต่อครั้ง");
      return;
    }
    setImportingStatements(true);
    try {
      const response = await client.post<ImportNotice>(
        "/api/tiktok-settlements/import",
        {
          shop_id: resolvedImportShopID,
          date_from: importFrom,
          date_to: importTo,
        },
      );
      setImportNotice(response.data);
      setImportOpen(false);
      await load();
    } catch (error: any) {
      toast.error(
        error?.response?.data?.error?.message ??
          "ดึง Statement จาก TikTok Shop ไม่สำเร็จ",
      );
    } finally {
      setImportingStatements(false);
    }
  };
  const openDetail = async (run: Run) => {
    setMissingOrderImportNotice(null);
    try {
      const [detail, routeResult] = await Promise.all([
        client.get<{ data: Run }>(`/api/tiktok-settlements/${run.id}`),
        client.get<{ data: RouteSummary }>("/api/tiktok-settlements/route"),
      ]);
      setSelected(detail.data.data);
      setRoute(routeResult.data.data);
    } catch (error: any) {
      toast.error(
        error?.response?.data?.error?.message ??
          "โหลดรายละเอียด Statement ไม่สำเร็จ",
      );
    }
  };
  const importMissingOrders = async () => {
    if (!selected) return;
    setImportingMissingOrders(true);
    try {
      const response = await client.post<MissingOrderImportResult>(
        `/api/tiktok-settlements/${selected.id}/import-missing-orders`,
      );
      setSelected(response.data.data);
      await load();
      setMissingOrderImportNotice({
        kind: "success",
        message: response.data.message,
        importedCount: response.data.imported_count,
        remainingCount: response.data.remaining_count,
      });
    } catch (error: any) {
      setMissingOrderImportNotice({
        kind: "error",
        message:
          error?.response?.data?.error?.message ??
          "นำเข้าคำสั่งซื้อ TikTok Shop ไม่สำเร็จ",
      });
    } finally {
      setImportingMissingOrders(false);
    }
  };
  const toggleBatchRun = (runID: string, checked: boolean) => {
    setSelectedRunIDs((current) =>
      checked
        ? current.includes(runID)
          ? current
          : [...current, runID]
        : current.filter((id) => id !== runID),
    );
  };
  const openBatchPreview = async () => {
    if (selectedRunIDs.length < 2) {
      toast.error("เลือก Statement ที่พร้อมสร้างเอกสารอย่างน้อย 2 รายการ");
      return;
    }
    try {
      const [preview, routeResult] = await Promise.all([
        client.post<{ data: BatchPreview }>("/api/tiktok-settlements/batches/preview", { run_ids: selectedRunIDs }),
        client.get<{ data: RouteSummary }>("/api/tiktok-settlements/route"),
      ]);
      setBatchPreview(preview.data.data);
      setRoute(routeResult.data.data);
      setBankAmount(Number(preview.data.data.settlement_amount).toFixed(2));
      setBankReference("");
      setBatchBankConfirmed(false);
      setBatchConfirmOpen(true);
    } catch (error: any) {
      toast.error(error?.response?.data?.error?.message ?? "ตรวจชุด Statement ไม่สำเร็จ");
      await load();
    }
  };
  const confirmBatchSend = async () => {
    if (!batchPreview || !batchBankConfirmed) return;
    setBatchSending(true);
    setBatchConfirmOpen(false);
    try {
      const response = await client.post<{ batch_id: string }>("/api/tiktok-settlements/batches/send", {
        run_ids: batchPreview.run_ids,
        confirm: "CONFIRM_TIKTOK_RC_BATCH",
        expected_digest: batchPreview.selection_digest,
        bank_amount: bankAmount,
        bank_reference: bankReference,
      });
      setBatchProgress({ open: true, batchID: response.data.batch_id, status: "sending", docNo: null, error: null });
      setSelectedRunIDs([]);
      setBatchPreview(null);
    } catch (error: any) {
      toast.error(error?.response?.data?.error?.message ?? "เริ่มสร้าง RC รวมไม่สำเร็จ");
    } finally {
      setBatchSending(false);
    }
  };
  const confirmSend = async () => {
    if (!selected || !bankEvidenceConfirmed) return;
    const run = selected;
    setSending(true);
    suppressNotificationToast({
      source: "tiktok_settlement",
      entity_type: "tiktok_settlement",
      entity_id: run.id,
    });
    // Replace the confirmation surface before the irreversible request starts;
    // there must never be a confirmation dialog underneath a result dialog.
    setSendConfirmOpen(false);
    setSelected(null);
    setSendProgress({
      open: true,
      runID: run.id,
      status: "sending",
      docNo: null,
      error: null,
    });
    try {
      await client.post(`/api/tiktok-settlements/${run.id}/send`, {
        confirm: "CONFIRM_TIKTOK_RC",
        expected_config_version: String(run.config_version),
      });
    } catch (error: any) {
      setSendProgress((current) => ({
        ...current,
        status: "error",
        error:
          error?.response?.data?.error?.message ??
          "สร้างเอกสารใน SML ไม่สำเร็จ",
      }));
    } finally {
      setSending(false);
    }
  };
  useEffect(() => {
    if (
      !sendProgress.open ||
      sendProgress.status !== "sending" ||
      !sendProgress.runID
    ) {
      return;
    }
    let active = true;
    let timer: number | undefined;
    let attempts = 0;
    const poll = async () => {
      try {
        const response = await client.get<{ data: Run }>(
          `/api/tiktok-settlements/${sendProgress.runID}`,
        );
        if (!active) return;
        const run = response.data.data;
        if (run.status === "sent") {
          setSendProgress((current) =>
            current.runID === run.id
              ? {
                  ...current,
                  status: "success",
                  docNo: run.rc_doc_no ?? null,
                  error: null,
                }
              : current,
          );
          void load();
          return;
        }
        if (run.status === "failed") {
          setSendProgress((current) =>
            current.runID === run.id
              ? {
                  ...current,
                  status: "error",
                  error: run.error_msg ?? "สร้างเอกสารใน SML ไม่สำเร็จ",
                }
              : current,
          );
          void load();
          return;
        }
        if (run.status === "unknown_result") {
          setSendProgress((current) =>
            current.runID === run.id
              ? {
                  ...current,
                  status: "warning",
                  docNo: run.rc_doc_no ?? null,
                  error:
                    run.anomaly_reason ??
                    run.error_msg ??
                    "ไม่สามารถยืนยันผลจาก SML ได้",
                }
              : current,
          );
          void load();
          return;
        }
      } catch {
        // The durable run remains the source of truth; use the bounded timeout
        // below rather than showing transient network failures as a final result.
      }
      attempts += 1;
      if (attempts >= 45) {
        if (!active) return;
        setSendProgress((current) => ({
          ...current,
          status: "warning",
          error:
            "ระบบยังยืนยันผลจาก SML ไม่ได้ กรุณาตรวจเอกสารใน SML ก่อนลองส่งซ้ำ",
        }));
        void load();
        return;
      }
      if (active) timer = window.setTimeout(() => void poll(), 1200);
    };
    void poll();
    return () => {
      active = false;
      if (timer) window.clearTimeout(timer);
    };
  }, [load, sendProgress.open, sendProgress.runID, sendProgress.status]);
  useEffect(() => {
    if (!batchProgress.open || batchProgress.status !== "sending" || !batchProgress.batchID) return;
    let active = true;
    let timer: number | undefined;
    let attempts = 0;
    const poll = async () => {
      try {
        const response = await client.get<{ data: BatchPreview }>(`/api/tiktok-settlements/batches/${batchProgress.batchID}`);
        if (!active) return;
        const batch = response.data.data;
        if (batch.status === "sent") {
          setBatchProgress((current) => ({ ...current, status: "success", docNo: batch.rc_doc_no ?? null, error: null }));
          void load();
          return;
        }
        if (batch.status === "failed" || batch.status === "unknown_result") {
          setBatchProgress((current) => ({ ...current, status: batch.status === "unknown_result" ? "warning" : "error", docNo: batch.rc_doc_no ?? null, error: batch.error_msg ?? "สร้างเอกสารใน SML ไม่สำเร็จ" }));
          void load();
          return;
        }
      } catch {
        // Keep polling the durable batch for a bounded time; a temporary read
        // failure must not be presented as a final SML result.
      }
      attempts += 1;
      if (attempts >= 45) {
        setBatchProgress((current) => ({ ...current, status: "warning", error: "ระบบยังยืนยันผลจาก SML ไม่ได้ กรุณาตรวจเอกสารก่อนลองใหม่" }));
        void load();
        return;
      }
      if (active) timer = window.setTimeout(() => void poll(), 1200);
    };
    void poll();
    return () => { active = false; if (timer) window.clearTimeout(timer); };
  }, [batchProgress.batchID, batchProgress.open, batchProgress.status, load]);
  const detailActionLabel = settlementPrimaryActionLabel(
    selected?.status,
    route?.configured,
  );

  return (
    <div className="space-y-4 p-0 sm:p-0">
      <header className="flex flex-col gap-3 border-b pb-4 lg:flex-row lg:items-end lg:justify-between">
        <div className="min-w-0">
          <div className="flex flex-wrap items-center gap-2">
            <h1 className="text-xl font-semibold tracking-tight">
              รับชำระ TikTok Shop
            </h1>
            <TikTokChip />
          </div>
          <p className="mt-1 max-w-3xl text-sm text-muted-foreground">
            ดึงข้อมูลที่ TikTok แจ้งว่าโอนแล้ว ตรวจเอกสารขาย
            และให้พนักงานยืนยันสร้างเอกสารใน SML ทีละรอบ
          </p>
        </div>
        <div className="flex flex-wrap items-center gap-1.5 lg:justify-end">
          <SettlementMetricChip
            label="กำลังตรวจ"
            value={counts.processing ?? 0}
            tone="primary"
          />
          <SettlementMetricChip
            label="พร้อมสร้าง"
            value={counts.ready ?? 0}
            tone="success"
          />
          <SettlementMetricChip
            label="ส่งแล้ว"
            value={counts.sent ?? 0}
            tone="success"
          />
          <SettlementMetricChip
            label="ต้องตรวจ"
            value={counts.needs_review ?? 0}
            tone="warning"
          />
          <SettlementMetricChip
            label="ผิดพลาด"
            value={counts.failed ?? 0}
            tone="danger"
          />
          <Button className="ml-1 h-9 gap-1.5" size="sm" onClick={openImport}>
            <ReceiptText className="h-4 w-4" />
            ดึง Statement จาก TikTok
          </Button>
        </div>
      </header>
      <section className="flex flex-wrap items-center gap-2 rounded-lg border bg-card p-3">
        <Select
          value={shopID || "all"}
          onValueChange={(value) => setShopID(value === "all" ? "" : value)}
        >
          <SelectTrigger
            className="h-9 w-full text-sm sm:w-[220px]"
            aria-label="กรองตามร้าน TikTok Shop"
          >
            <Store className="mr-2 h-4 w-4 shrink-0" />
            <SelectValue placeholder="ทุกร้าน TikTok Shop" />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">ทุกร้าน TikTok Shop</SelectItem>
            {shops.map((shop) => (
              <SelectItem key={shop.shop_id} value={shop.shop_id}>
                {shop.shop_name || shop.shop_id}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <DateRangePicker
          from={from}
          to={to}
          onFromChange={setFrom}
          onToChange={setTo}
          onRangeChange={(range) => {
            setFrom(range.from);
            setTo(range.to);
          }}
          presets={statementPresets.slice(0, 3)}
          title="ช่วงวันที่ของรายการ"
          description="ใช้กรองรายการที่นำเข้าแล้วตามเวลาประเทศไทย ไม่ได้เรียก TikTok ใหม่"
          className="h-9 w-full !min-w-0 text-sm sm:w-[260px]"
        />
        <Select value={runStatus} onValueChange={setRunStatus}>
          <SelectTrigger
            className="h-9 w-full text-sm sm:w-[180px]"
            aria-label="กรองตามสถานะงาน"
          >
            <SelectValue placeholder="ทุกสถานะงาน" />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">ทุกสถานะงาน</SelectItem>
            <SelectItem value="ready">พร้อมสร้างเอกสาร</SelectItem>
            <SelectItem value="needs_review">ต้องตรวจข้อมูล</SelectItem>
            <SelectItem value="sent">สร้างเอกสารแล้ว</SelectItem>
            <SelectItem value="failed">ดำเนินการไม่สำเร็จ</SelectItem>
          </SelectContent>
        </Select>
        <Button
          className="ml-auto h-9 gap-1.5"
          size="sm"
          variant="outline"
          onClick={() => void load()}
        >
          <RefreshCw className="h-4 w-4" />
          รีเฟรช
        </Button>
      </section>
      {selectedRunIDs.length > 0 && (
        <section className="flex flex-wrap items-center justify-between gap-3 rounded-lg border border-[#111817]/20 bg-[#111817]/[0.03] px-3 py-2.5 text-sm">
          <p className="text-muted-foreground">
            เลือก <span className="font-semibold text-foreground">{selectedRunIDs.length} Statement</span> เพื่อรวมเป็น RC เดียว
          </p>
          <div className="flex items-center gap-2">
            <Button variant="ghost" size="sm" onClick={() => setSelectedRunIDs([])}>ล้างที่เลือก</Button>
            <Button size="sm" onClick={() => void openBatchPreview()} disabled={selectedRunIDs.length < 2}>
              รวมเป็น RC เดียว
            </Button>
          </div>
        </section>
      )}
      {importNotice && (
        <div
          role="status"
          className={cn(
            "flex items-start justify-between gap-3 rounded-md border p-3 text-sm",
            importNotice.processing_count || importNotice.failed_count
              ? "border-warning/30 bg-warning/5 text-warning"
              : "border-success/30 bg-success/5 text-success",
          )}
        >
          <div className="flex min-w-0 gap-2">
            {importNotice.processing_count || importNotice.failed_count ? (
              <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0" />
            ) : (
              <CheckCircle2 className="mt-0.5 h-4 w-4 shrink-0" />
            )}
            <p>{importNotice.message}</p>
          </div>
          <Button
            variant="ghost"
            size="sm"
            className="h-7 shrink-0 px-2"
            onClick={() => setImportNotice(null)}
          >
            ปิด
          </Button>
        </div>
      )}
      <Card className="border-border/70 shadow-none">
        <CardContent className="p-0">
          {loading ? (
            <SettlementSkeleton />
          ) : (
            <>
              <div className="hidden overflow-x-auto md:block">
                <table className="w-full text-sm">
                  <thead className="border-b bg-muted/30 text-left text-muted-foreground">
                    <tr>
                      <th className="w-10 p-3"><span className="sr-only">เลือก</span></th>
                      <th className="p-3 font-medium">Statement / วันที่</th>
                      <th className="p-3 font-medium">ร้าน</th>
                      <th className="p-3 text-right font-medium">
                        ยอด TikTok / คำสั่งซื้อ
                      </th>
                      <th className="p-3 font-medium">สถานะงาน</th>
                      <th className="p-3 font-medium">เอกสาร SML</th>
                      <th className="p-3">
                        <span className="sr-only">การดำเนินการ</span>
                      </th>
                    </tr>
                  </thead>
                  <tbody>
                    {runs.map((run) => (
                      <SettlementTableRow
                        key={run.id}
                        run={run}
                        onOpen={openDetail}
                        selected={selectedRunIDs.includes(run.id)}
                        onSelect={toggleBatchRun}
                      />
                    ))}
                  </tbody>
                </table>
              </div>
              <div className="divide-y md:hidden">
                {runs.map((run) => (
                  <SettlementMobileRow
                    key={run.id}
                    run={run}
                    onOpen={openDetail}
                    selected={selectedRunIDs.includes(run.id)}
                    onSelect={toggleBatchRun}
                  />
                ))}
              </div>
              {runs.length === 0 && (
                <div className="p-10 text-center text-sm text-muted-foreground">
                  <p>ยังไม่มีรายการในตัวกรองนี้</p>
                  <Button
                    className="mt-3"
                    size="sm"
                    variant="outline"
                    onClick={openImport}
                  >
                    ดึง Statement จาก TikTok
                  </Button>
                </div>
              )}
            </>
          )}
        </CardContent>
      </Card>
      <Dialog
        open={Boolean(selected)}
        onOpenChange={(open) => {
          if (!open) {
            setSelected(null);
            setMissingOrderImportNotice(null);
          }
        }}
      >
        <DialogContent className="flex max-h-[90vh] max-w-4xl flex-col gap-0 overflow-hidden p-0">
          <DialogHeader className="border-b px-5 py-4 sm:px-6">
            <div className="flex flex-wrap items-center gap-2">
              <TikTokChip />
              <DialogTitle className="text-base">
                {selected?.statement_id || "รายละเอียด Statement"}
              </DialogTitle>
            </div>
            <DialogDescription className="pt-1">
              {selected && ["PAID", "SETTLED"].includes(selected.payment_status)
                ? "TikTok แจ้งว่ารอบนี้โอนแล้ว โปรดตรวจหลักฐานเงินเข้าบัญชีจริงก่อนสร้างเอกสารใน SML"
                : `รอบนี้อยู่สถานะ ${paymentStatusText[selected?.payment_status || ""] ?? selected?.payment_status ?? "-"} จึงยังสร้างเอกสารใน SML ไม่ได้`}
            </DialogDescription>
          </DialogHeader>
          {selected && (
            <div className="min-h-0 flex-1 overflow-y-auto px-5 py-4 sm:px-6">
              <div className="grid gap-x-6 gap-y-3 border-b pb-4 text-sm sm:grid-cols-2 lg:grid-cols-4">
                <DetailValue label="ร้าน" value={selected.shop_label} />
                <DetailValue
                  label="วันที่ Statement"
                  value={formatDate(statementDate(selected))}
                />
                <DetailValue
                  label="ยอด TikTok"
                  value={money(
                    selected.total_settlement_amount,
                    selected.currency,
                  )}
                  strong
                />
                <DetailValue
                  label="จำนวนคำสั่งซื้อ"
                  value={`${selected.items?.length ?? selected.item_count ?? 0} รายการ`}
                />
                <DetailValue
                  label="ยอดใบขาย SML"
                  value={money(
                    selected.invoice_amount_total,
                    selected.currency,
                  )}
                />
                <DetailValue
                  label="Fee / commission"
                  value={money(selected.fee_amount_total, selected.currency)}
                />
                <DetailValue
                  label="สถานะ"
                  value={statusMeta[selected.status]?.text ?? selected.status}
                />
                <DetailValue
                  label="ปลายทาง"
                  value={settlementDestinationLabel(route)}
                />
              </div>
              {(selected.anomaly_reason || selected.error_msg) && (
                <div className="mt-4 flex gap-2 rounded-md border border-warning/30 bg-warning/5 p-3 text-sm text-warning">
                  <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0" />
                  <p>{selected.anomaly_reason || selected.error_msg}</p>
                </div>
              )}
              {selected.status !== "sent" && (
                <div className="mt-4 rounded-md border border-info/20 bg-info/5 px-3 py-2.5 text-sm text-muted-foreground">
                  <p className="font-medium text-foreground">ไม่พบใบขายของออเดอร์เก่าหรือไม่?</p>
                  <p className="mt-0.5">
                    นำเข้าคำสั่งซื้อที่อยู่ใน Statement นี้ก่อน ระบบจะบันทึกเป็นรายการตรวจสอบเท่านั้น และจะไม่สร้าง Bill หรือส่ง SML เอง
                  </p>
                </div>
              )}
              {missingOrderImportNotice && (
                <div
                  role="status"
                  className={cn(
                    "mt-4 flex items-start gap-2 rounded-md border px-3 py-2.5 text-sm",
                    missingOrderImportNotice.kind === "success"
                      ? "border-success/30 bg-success/5 text-success"
                      : "border-destructive/30 bg-destructive/5 text-destructive",
                  )}
                >
                  {missingOrderImportNotice.kind === "success" ? (
                    <CheckCircle2 className="mt-0.5 h-4 w-4 shrink-0" />
                  ) : (
                    <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0" />
                  )}
                  <div className="min-w-0">
                    <p className="font-medium">
                      {missingOrderImportNotice.kind === "success"
                        ? "ผลการนำเข้าคำสั่งซื้อ"
                        : "นำเข้าคำสั่งซื้อไม่สำเร็จ"}
                    </p>
                    <p className="mt-0.5 text-foreground/80">
                      {missingOrderImportNotice.message}
                    </p>
                    {missingOrderImportNotice.kind === "success" &&
                      (missingOrderImportNotice.importedCount ?? 0) > 0 && (
                        <p className="mt-1 text-foreground/80">
                          ขั้นถัดไป: ตรวจและสร้างเอกสารขายใน Nexflow แล้วส่ง SML สำหรับออเดอร์ที่ยังขาด
                        </p>
                      )}
                  </div>
                </div>
              )}
              <div className="mt-4 overflow-hidden rounded-md border">
                <div className="grid grid-cols-[minmax(0,1fr)_auto_auto] gap-3 border-b bg-muted/30 px-3 py-2 text-xs font-medium text-muted-foreground">
                  <span>คำสั่งซื้อ / ใบขาย SML</span>
                  <span>ยอด TikTok</span>
                  <span>ผลตรวจ</span>
                </div>
                {(selected.items ?? []).map((item) => {
                  const billDetailPath = item.bill_id
                    ? `/sale-invoices/${encodeURIComponent(item.bill_id)}`
                    : "";
                  const orderDetailPath = item.order_snapshot_available
                    ? tiktokOrderDetailPath({ shopID: selected.shop_id, orderID: item.order_id })
                    : "";
                  const detailPath = billDetailPath || orderDetailPath;
                  const detailLabel = billDetailPath
                    ? `เปิด Bill ของคำสั่งซื้อ ${item.order_id}`
                    : `เปิดคำสั่งซื้อ TikTok Shop ${item.order_id}`;
                  return (
                  <div
                    key={item.id}
                    className="grid grid-cols-[minmax(0,1fr)_auto_auto] items-center gap-3 border-b px-3 py-2.5 text-sm last:border-0"
                  >
                    <div className="min-w-0">
                      {detailPath ? (
                        <Link
                          to={detailPath}
                          aria-label={detailLabel}
                          className="inline-flex max-w-full items-center gap-1 truncate font-medium text-link hover:underline focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                        >
                          <span className="truncate">{item.order_id}</span>
                          <ChevronRight className="h-3.5 w-3.5 shrink-0" aria-hidden="true" />
                        </Link>
                      ) : (
                        <p className="truncate font-medium">{item.order_id}</p>
                      )}
                      <p className="truncate text-xs text-muted-foreground">
                        {item.sml_invoice_doc_no
                          ? `ส่ง SML แล้ว · ${item.sml_invoice_doc_no}`
                          : item.bill_id
                            ? "เปิด Bill เพื่อตรวจและส่ง SML"
                          : item.order_snapshot_available
                            ? "นำเข้า order แล้ว · เปิดเพื่อตรวจ Bill และส่ง SML"
                            : item.block_reason || "ยังไม่พบ order ใน Nexflow"}
                      </p>
                    </div>
                    <span className="whitespace-nowrap tabular-nums">
                      {money(item.settlement_amount, selected.currency)}
                    </span>
                    <Badge
                      variant="secondary"
                      className={
                        item.status === "ready" || item.status === "sent"
                          ? "bg-success/15 text-success"
                          : "bg-warning/15 text-warning"
                      }
                    >
                      {item.status === "ready"
                        ? "ผ่าน"
                        : item.status === "sent"
                          ? "ส่งแล้ว"
                          : "ต้องตรวจ"}
                    </Badge>
                  </div>
                  );
                })}
              </div>
            </div>
          )}
          <DialogFooter className="border-t bg-background px-5 py-3 sm:px-6">
            <Button
              variant="outline"
              onClick={() => void importMissingOrders()}
              disabled={
                importingMissingOrders || !selected || selected.status === "sent"
              }
            >
              {importingMissingOrders ? (
                <RefreshCw className="mr-2 h-4 w-4 animate-spin" />
              ) : (
                <FileText className="mr-2 h-4 w-4" />
              )}
              ตรวจและนำเข้าคำสั่งซื้อที่ขาด
            </Button>
            <Button
              onClick={() => {
                setBankEvidenceConfirmed(false);
                setSendConfirmOpen(true);
              }}
              disabled={
                sending || selected?.status !== "ready" || !route?.configured
              }
            >
              <FileText className="mr-2 h-4 w-4" />
              {detailActionLabel}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
      <Dialog open={importOpen} onOpenChange={setImportOpen}>
        <DialogContent className="sm:max-w-xl">
          <DialogHeader>
            <DialogTitle>ดึง Statement จาก TikTok</DialogTitle>
            <DialogDescription>
              เลือกร้านและช่วงวันที่เพื่อดึงข้อมูลล่าสุดด้วยตนเอง
              ระบบจะไม่สร้างเอกสารหรือส่งข้อมูลเข้า SML ในขั้นตอนนี้
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-4 py-2">
            <div className="space-y-2">
              <Label htmlFor="tiktok-import-shop">ร้าน TikTok Shop</Label>
              <Select
                value={resolvedImportShopID}
                onValueChange={setImportShopID}
              >
                <SelectTrigger id="tiktok-import-shop">
                  <SelectValue placeholder="เลือกร้าน" />
                </SelectTrigger>
                <SelectContent>
                  {shops.map((shop) => (
                    <SelectItem key={shop.shop_id} value={shop.shop_id}>
                      {shop.shop_name || shop.shop_id}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <DateRangePicker
              from={importFrom}
              to={importTo}
              onFromChange={setImportFrom}
              onToChange={setImportTo}
              onRangeChange={(range) => {
                setImportFrom(range.from);
                setImportTo(range.to);
              }}
              presets={statementPresets}
              title="ช่วงวันที่ที่ต้องการดึง"
              description="เลือกได้ไม่เกิน 31 วันต่อครั้ง โดยอ้างอิงเวลา Asia/Bangkok"
            />
            <p className="rounded-md bg-muted/50 p-3 text-xs text-muted-foreground">
              ดึงซ้ำในช่วงเดิมได้อย่างปลอดภัย ระบบจะอัปเดต snapshot เดิมตาม
              Statement ID และไม่สร้างเอกสาร SML ซ้ำ
            </p>
          </div>
          <DialogFooter>
            <Button
              variant="outline"
              onClick={() => setImportOpen(false)}
              disabled={importingStatements}
            >
              ยกเลิก
            </Button>
            <Button
              onClick={() => void importStatements()}
              disabled={importingStatements}
            >
              {importingStatements ? "กำลังดึงข้อมูล…" : "เริ่มดึง Statement"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
      <Dialog
        open={sendConfirmOpen}
        onOpenChange={(open) => {
          setSendConfirmOpen(open);
          if (!open) setBankEvidenceConfirmed(false);
        }}
      >
        <DialogContent className="sm:max-w-lg">
          <DialogHeader>
            <DialogTitle>ยืนยันสร้างเอกสารใน SML</DialogTitle>
            <DialogDescription>
              การดำเนินการนี้สร้างเอกสารตามเส้นทาง SML ที่ร้านตั้งค่าไว้
              และยังไม่มีการทำงานอัตโนมัติ
            </DialogDescription>
          </DialogHeader>
          {selected && (
            <div className="space-y-3 rounded-md border p-3 text-sm">
              <DetailValue label="Statement" value={selected.statement_id} />
              <DetailValue
                label="ยอด TikTok"
                value={money(
                  selected.total_settlement_amount,
                  selected.currency,
                )}
                strong
              />
              <DetailValue
                label="ปลายทาง"
                value={settlementDestinationLabel(route)}
              />
              <div className="flex items-start gap-2 border-t pt-3">
                <Checkbox
                  id="bank-evidence-confirmed"
                  checked={bankEvidenceConfirmed}
                  onCheckedChange={(checked) =>
                    setBankEvidenceConfirmed(checked === true)
                  }
                />
                <Label
                  htmlFor="bank-evidence-confirmed"
                  className="cursor-pointer text-sm font-normal leading-5"
                >
                  ฉันตรวจหลักฐานเงินเข้าบัญชีจริงและข้อมูลในรอบนี้แล้ว
                </Label>
              </div>
            </div>
          )}
          <DialogFooter>
            <Button
              variant="outline"
              onClick={() => setSendConfirmOpen(false)}
              disabled={sending}
            >
              ยกเลิก
            </Button>
            <Button
              onClick={() => void confirmSend()}
              disabled={sending || !bankEvidenceConfirmed}
            >
              {sending ? "กำลังสร้างเอกสาร…" : "ยืนยันสร้างเอกสารใน SML"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
      <Dialog open={batchConfirmOpen} onOpenChange={(open) => { setBatchConfirmOpen(open); if (!open) setBatchBankConfirmed(false); }}>
        <DialogContent className="sm:max-w-lg">
          <DialogHeader>
            <DialogTitle>ยืนยันรวม Statement เป็น RC เดียว</DialogTitle>
            <DialogDescription>
              ระบบจะสร้าง RC เพียงหนึ่งใบจาก Statement ที่เลือก ไม่จับคู่รอบถอนเงินโดยการคาดเดา และไม่มีการส่งอัตโนมัติ
            </DialogDescription>
          </DialogHeader>
          {batchPreview && (
            <div className="space-y-4 py-1 text-sm">
              <div className="grid gap-3 rounded-md border bg-muted/20 p-3 sm:grid-cols-2">
                <DetailValue label="ร้าน" value={batchPreview.shop_label} />
                <DetailValue label="จำนวน Statement" value={`${batchPreview.statement_count} รอบ`} />
                <DetailValue label="จำนวนคำสั่งซื้อ" value={`${batchPreview.order_count} รายการ`} />
                <DetailValue label="ยอด TikTok รวม" value={money(batchPreview.settlement_amount, batchPreview.currency)} strong />
                <DetailValue label="Fee / commission" value={money(batchPreview.fee_amount, batchPreview.currency)} />
                <DetailValue label="ปลายทาง" value={settlementDestinationLabel(route)} />
              </div>
              <div className="space-y-2">
                <Label htmlFor="tiktok-batch-bank-amount">ยอดเงินจริงที่ได้รับเข้าบัญชี</Label>
                <Input id="tiktok-batch-bank-amount" inputMode="decimal" value={bankAmount} onChange={(event) => setBankAmount(event.target.value)} placeholder="0.00" />
                <p className="text-xs text-muted-foreground">ต้องเท่ากับยอด TikTok รวมทุกสตางค์ จึงจะส่ง RC ได้</p>
              </div>
              <div className="space-y-2">
                <Label htmlFor="tiktok-batch-bank-reference">เลขอ้างอิงธนาคาร (ถ้ามี)</Label>
                <Input id="tiktok-batch-bank-reference" value={bankReference} onChange={(event) => setBankReference(event.target.value)} maxLength={160} placeholder="เช่น เลขรายการจากธนาคาร" />
              </div>
              <div className="max-h-24 overflow-y-auto rounded-md border px-3 py-2 text-xs text-muted-foreground">
                Statement: {batchPreview.statement_ids.join(", ")}
              </div>
              <div className="flex items-start gap-2 border-t pt-3">
                <Checkbox id="tiktok-batch-bank-confirmed" checked={batchBankConfirmed} onCheckedChange={(checked) => setBatchBankConfirmed(checked === true)} />
                <Label htmlFor="tiktok-batch-bank-confirmed" className="cursor-pointer text-sm font-normal leading-5">ฉันตรวจยอดเงินจริงในบัญชีแล้ว และยืนยันว่าเท่ากับยอดรวมของ Statement ที่เลือก</Label>
              </div>
            </div>
          )}
          <DialogFooter>
            <Button variant="outline" onClick={() => setBatchConfirmOpen(false)} disabled={batchSending}>ยกเลิก</Button>
            <Button onClick={() => void confirmBatchSend()} disabled={batchSending || !batchBankConfirmed}>{batchSending ? "กำลังสร้างเอกสาร…" : "ยืนยันสร้าง RC รวม"}</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
      <SMLSendProgressDialog
        open={sendProgress.open}
        status={sendProgress.status}
        docNo={sendProgress.docNo}
        error={sendProgress.error}
        onClose={() =>
          setSendProgress((current) => ({
            ...current,
            open: false,
            runID: null,
          }))
        }
      />
      <SMLSendProgressDialog
        open={batchProgress.open}
        status={batchProgress.status}
        docNo={batchProgress.docNo}
        error={batchProgress.error}
        onClose={() => setBatchProgress((current) => ({ ...current, open: false, batchID: null }))}
      />
    </div>
  );
}

function SettlementTableRow({
  run,
  onOpen,
  selected,
  onSelect,
}: {
  run: Run;
  onOpen: (run: Run) => void;
  selected: boolean;
  onSelect: (runID: string, checked: boolean) => void;
}) {
  const status = statusMeta[run.status] ?? { text: run.status, className: "" };
  const selectable = run.status === "ready" && ["PAID", "SETTLED"].includes(run.payment_status);
  return (
    <tr className="border-b last:border-0 hover:bg-muted/30">
      <td className="p-3">
        {selectable && <Checkbox checked={selected} onCheckedChange={(checked) => onSelect(run.id, checked === true)} aria-label={`เลือก Statement ${run.statement_id}`} />}
      </td>
      <td className="p-3">
        <p className="max-w-[200px] truncate font-medium">{run.statement_id}</p>
        <p className="mt-0.5 text-xs text-muted-foreground">
          {formatDate(statementDate(run))}
        </p>
      </td>
      <td className="p-3">
        <TikTokChip />
        <p className="mt-1 max-w-[180px] truncate text-xs text-muted-foreground">
          {run.shop_label}
        </p>
      </td>
      <td className="p-3 text-right">
        <p className="font-medium tabular-nums">
          {money(run.total_settlement_amount, run.currency)}
        </p>
        <p className="mt-0.5 text-xs text-muted-foreground">
          {run.item_count ?? 0} คำสั่งซื้อ
        </p>
      </td>
      <td className="p-3">
        <Badge variant="secondary" className={status.className}>
          {status.text}
        </Badge>
        <p className="mt-1 max-w-[210px] truncate text-xs text-muted-foreground">
          {statusReason(run)}
        </p>
      </td>
      <td className="p-3">
        <p className="max-w-[150px] truncate font-mono text-xs">
          {run.rc_doc_no || "ยังไม่สร้าง"}
        </p>
        <p className="mt-0.5 text-xs text-muted-foreground">
          {run.rc_doc_no ? "สร้างแล้วใน SML" : "รอการยืนยัน"}
        </p>
      </td>
      <td className="p-3 text-right">
        <Button size="sm" variant="outline" onClick={() => onOpen(run)}>
          รายละเอียด
          <ChevronRight className="ml-1 h-4 w-4" />
        </Button>
      </td>
    </tr>
  );
}
function SettlementMobileRow({
  run,
  onOpen,
  selected,
  onSelect,
}: {
  run: Run;
  onOpen: (run: Run) => void;
  selected: boolean;
  onSelect: (runID: string, checked: boolean) => void;
}) {
  const status = statusMeta[run.status] ?? { text: run.status, className: "" };
  const selectable = run.status === "ready" && ["PAID", "SETTLED"].includes(run.payment_status);
  return (
    <article className="p-4 hover:bg-muted/30">
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <div className="flex flex-wrap items-center gap-2">
            <TikTokChip />
            <p className="truncate font-medium">{run.statement_id}</p>
          </div>
          <p className="mt-1 truncate text-xs text-muted-foreground">
            {run.shop_label} · {formatDate(statementDate(run))}
          </p>
          <p className="mt-1 text-sm font-medium">
            {money(run.total_settlement_amount, run.currency)} ·{" "}
            {run.item_count ?? 0} คำสั่งซื้อ
          </p>
        </div>
        <div className="shrink-0 text-right">
          <Badge variant="secondary" className={status.className}>
            {status.text}
          </Badge>
          <p className="mt-1 max-w-[110px] truncate text-xs text-muted-foreground">
            {run.rc_doc_no || "ยังไม่สร้าง"}
          </p>
        </div>
      </div>
      <div className="mt-3 flex items-center justify-between gap-2">
        {selectable ? <label className="flex items-center gap-2 text-xs text-muted-foreground"><Checkbox checked={selected} onCheckedChange={(checked) => onSelect(run.id, checked === true)} /> เลือกรวมเป็น RC เดียว</label> : <span />}
        <Button size="sm" variant="outline" onClick={() => onOpen(run)}>รายละเอียด<ChevronRight className="ml-1 h-4 w-4" /></Button>
      </div>
    </article>
  );
}
function SettlementMetricChip({
  label,
  value,
  tone,
}: {
  label: string;
  value: number;
  tone: "primary" | "success" | "warning" | "danger";
}) {
  const toneClass =
    tone === "success"
      ? "border-success/25 bg-success/10 text-success"
      : tone === "warning"
        ? "border-warning/30 bg-warning/10 text-warning"
        : tone === "danger"
          ? "border-destructive/25 bg-destructive/10 text-destructive"
          : "border-primary/25 bg-primary/10 text-accent-strong";
  return (
    <span
      className={cn(
        "inline-flex h-7 items-center gap-1.5 rounded-md border px-2 text-[11px]",
        toneClass,
      )}
    >
      <span className="font-semibold tabular-nums">
        {value.toLocaleString()}
      </span>
      <span className="text-foreground/75">{label}</span>
    </span>
  );
}
function DetailValue({
  label,
  value,
  strong = false,
}: {
  label: string;
  value: string;
  strong?: boolean;
}) {
  return (
    <div className="min-w-0">
      <p className="text-xs text-muted-foreground">{label}</p>
      <p
        className={cn(
          "mt-0.5 truncate text-sm",
          strong && "font-semibold tabular-nums",
        )}
      >
        {value || "-"}
      </p>
    </div>
  );
}
function SettlementSkeleton() {
  return (
    <div className="space-y-3 p-4" aria-label="กำลังโหลดรายการ">
      <div className="h-8 w-2/5 animate-pulse rounded bg-muted" />
      {[1, 2, 3, 4].map((row) => (
        <div key={row} className="grid grid-cols-5 gap-4 border-t pt-3">
          <div className="h-4 animate-pulse rounded bg-muted" />
          <div className="h-4 animate-pulse rounded bg-muted" />
          <div className="h-4 animate-pulse rounded bg-muted" />
          <div className="h-4 animate-pulse rounded bg-muted" />
          <div className="h-4 animate-pulse rounded bg-muted" />
        </div>
      ))}
    </div>
  );
}
