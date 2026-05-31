# BillFlow Main + sml-api-bybos Architecture & Data Flow

> Updated: 2026-05-20
> Scope: BillFlow main only (`backend :8090`, `frontend :3010`, tenant `SML1_2026`)  
> Out of scope for this document: Henna and Thaisunsport instances

---

## 1. Executive Summary

BillFlow main is the operational UI and workflow engine for turning marketplace/email documents into reviewed SML ERP documents. It receives data from Email, Shopee Excel, Lazada Excel, and TikTok Excel/CSV, stores them as local BillFlow documents, lets staff review product matching and routing, then sends the final document to SML through `sml-api-bybos`.

`sml-api-bybos` is now the production SML access layer for BillFlow main. BillFlow no longer depends on the old SML Java REST API directly for the main SML write/read routes. The API keeps BillFlow-native behavior, consistent response envelopes, request IDs, OpenAPI/Swagger docs, readiness checks, validation, duplicate handling, and SML table writes.

---

## 2. Runtime Architecture

```mermaid
flowchart LR
  subgraph Users["Users"]
    Staff["Admin / Staff"]
  end

  subgraph BillFlowMain["BillFlow Main Server - 192.168.2.109"]
    FE["React Frontend\n:3010"]
    BE["Go Gin Backend\n:8090"]
    BFDB[("BillFlow PostgreSQL\n:5438")]
    Jobs["Background Jobs\nEmail poller / AI usage / backups"]
  end

  subgraph ExternalInputs["Input Channels"]
    Email["Email IMAP\nmulti-account"]
    Shopee["Shopee Excel"]
    Lazada["Lazada Excel"]
    TikTok["TikTok Excel/CSV"]
    LineOA["LINE OA\nhuman chat inbox"]
  end

  subgraph AI["AI Providers"]
    OpenRouter["OpenRouter\nextract + embeddings"]
    OCR["Mistral OCR\nPDF/image where enabled"]
  end

  subgraph SMLLayer["SML Access Layer - 192.168.2.109"]
    SMLAPI["sml-api-bybos\n:8200\n/api/v1 + /docs"]
  end

  subgraph SMLERP["SML ERP PostgreSQL - 192.168.2.248"]
    SMLDB[("sml1_2026\nic_trans / ic_trans_detail\nic_inventory / ar / ap")]
  end

  Staff --> FE
  FE -->|"JWT API calls"| BE
  Email --> Jobs
  Shopee --> FE
  Lazada --> FE
  TikTok --> FE
  LineOA --> BE
  Jobs --> BE
  BE <--> BFDB
  BE --> OpenRouter
  BE --> OCR
  BE -->|"Docker gateway\nhttp://172.24.0.1:8200"| SMLAPI
  SMLAPI -->|"pgx tenant pool\nX-Tenant/databaseName=SML1_2026"| SMLDB
```

### Runtime Ports

| Component | Host/Port | Purpose |
|---|---:|---|
| BillFlow frontend | `192.168.2.109:3010` | Staff UI |
| BillFlow backend | `192.168.2.109:8090` | Auth, document workflow, retry, logs |
| BillFlow PostgreSQL | `192.168.2.109:5438` | Local BillFlow state |
| sml-api-bybos | `192.168.2.109:8200` | BillFlow-native SML API |
| SML ERP database | `192.168.2.248:5432/sml1_2026` | SML ERP tables |

---

## 3. Main Component Architecture

```mermaid
flowchart TB
  subgraph Frontend["BillFlow Frontend"]
    Login["/login"]
    ReviewPO["/bills\npurchase orders"]
    ReviewSO["/sales-orders\nsale orders"]
    ReviewSI["/sale-invoices\nsale invoices"]
    CatalogUI["/settings/catalog"]
    LogsUI["/logs + /settings/ai-usage"]
    SettingsUI["/settings/*"]
  end

  subgraph Backend["BillFlow Backend"]
    Auth["Auth + JWT"]
    ImportHandlers["Import Handlers\nShopee/Lazada/TikTok"]
    EmailCoordinator["Email Coordinator\nIMAP polling"]
    BillHandlers["Bill Review + Retry Handlers"]
    BulkJobs["Async SML Bulk Jobs\nprogress + retry failed"]
    Mapping["Product Mapping\nmanual + learned aliases"]
    Catalog["SML Catalog Service\nsync + embedding index"]
    AIUsage["AI Usage Logging\nrequest/session metadata"]
    SMLClients["SML Clients\nsaleorder/saleinvoice/purchaseorder/product/master"]
  end

  subgraph BillFlowDB["BillFlow DB"]
    Bills[("bills")]
    Items[("bill_items")]
    CatalogTable[("sml_catalog")]
    Mappings[("mappings\nmarketplace aliases")]
    Audit[("audit_logs\nai_usage_logs")]
    BulkJobTables[("sml_bulk_jobs\nsml_bulk_job_items")]
    Settings[("channel_defaults\nimap_accounts\ninstance_settings")]
  end

  subgraph SMLAPI["sml-api-bybos"]
    Middleware["API key + tenant middleware"]
    DTOs["DTO + validation"]
    Repos["SML repositories"]
    TxWriter["Shared SML transaction writer"]
    OpenAPI["OpenAPI + Swagger UI"]
    Health["/health + /health/ready"]
  end

  Frontend --> Auth
  Frontend --> ImportHandlers
  Frontend --> BillHandlers
  Frontend --> Catalog
  Frontend --> LogsUI
  EmailCoordinator --> ImportHandlers
  ImportHandlers --> Bills
  ImportHandlers --> Items
  BillHandlers --> Bills
  BillHandlers --> Items
  BillHandlers --> Mapping
  BillHandlers --> SMLClients
  BillHandlers --> BulkJobs
  BulkJobs --> BulkJobTables
  BulkJobs --> BillHandlers
  Catalog --> CatalogTable
  Catalog --> AIUsage
  AIUsage --> Audit
  Audit --> Lifecycle["Data Lifecycle\nrollup + batch purge"]
  SettingsUI --> Settings
  SMLClients --> Middleware
  Middleware --> DTOs
  DTOs --> Repos
  DTOs --> TxWriter
```

---

## 4. Data Flow Diagram - Level 0

```mermaid
flowchart LR
  CustomerChannels["External Data Sources\nEmail / Excel / LINE"] --> P1["1. Ingest Documents"]
  P1 --> D1[("BillFlow DB\nlocal documents")]
  D1 --> P2["2. Review + Match Products"]
  P2 --> D2[("Catalog + Mapping Data")]
  P2 --> P3["3. Send to SML\nsingle retry or async bulk job"]
  P3 --> P4["4. sml-api-bybos\nvalidate + write"]
  P4 --> D3[("SML ERP DB\nSML1_2026")]
  P3 --> D4[("Audit Logs\ncompact SML support fields")]
  P4 --> D4
  Admin["Admin / Staff"] --> P2
  Admin --> P3
  P2 --> Admin
  P3 --> Admin
```

### Level 0 Explanation

| Step | What Happens | Main Data |
|---|---|---|
| 1. Ingest Documents | BillFlow imports email or marketplace files and creates local bills | source file/email, parsed header, parsed items |
| 2. Review + Match Products | Staff verifies party, route, item codes, units, warehouse/shelf, VAT | bill, bill_items, candidates, catalog, aliases |
| 3. Send to SML | Staff clicks retry/send or creates a bulk job; BillFlow builds SML payload per bill | saleorder, saleinvoice, purchaseorder payload, bulk job progress/results |
| 4. sml-api-bybos | API validates tenant/master data/totals and writes SML tables | `ic_trans`, `ic_trans_detail`, master lookups |

---

## 5. Data Flow Diagram - Level 1: Ingest to Review

```mermaid
flowchart TB
  subgraph Inputs["Input Channels"]
    Email["Email IMAP"]
    ShopeeExcel["Shopee Excel"]
    LazadaExcel["Lazada Excel"]
    TikTokExcel["TikTok Excel/CSV"]
  end

  Email --> EmailPoller["EmailCoordinator\npoll + classify"]
  EmailPoller --> Extract["Extract document data\nAI/OCR when needed"]
  ShopeeExcel --> Preview["Preview + dedup"]
  LazadaExcel --> Preview
  TikTokExcel --> Preview

  Extract --> Normalize["Normalize to BillFlow document model"]
  Preview --> Normalize
  Normalize --> Route["Apply channel defaults\nbill_type + endpoint + party + doc format"]
  Route --> LocalBill[("bills")]
  Route --> LocalItems[("bill_items")]
  Route --> Artifacts[("bill_artifacts\nsource files")]
  LocalBill --> ReviewUI["Review UI\n/bills /sales-orders /sale-invoices"]
  LocalItems --> ReviewUI
  Artifacts --> ReviewUI
```

### Important Rules

| Area | Rule |
|---|---|
| Local-first | Imported documents are stored in BillFlow before SML write |
| Human review | Staff can edit item code, unit, qty, price, route, party, warehouse, shelf |
| Retryable | Failed SML sends remain in BillFlow and can be retried |
| Source traceability | Source email/file/artifact and audit logs stay available for debugging |

---

## 6. Data Flow Diagram - Level 1: Review to SML

```mermaid
sequenceDiagram
  autonumber
  actor Staff as Admin / Staff
  participant FE as BillFlow Frontend
  participant BE as BillFlow Backend
  participant BFDB as BillFlow DB
  participant SMLC as BillFlow SML Client
  participant API as sml-api-bybos
  participant SMLDB as SML DB sml1_2026

  Staff->>FE: Review document and click Retry/Send
  FE->>BE: POST /api/bills/:id/retry
  BE->>BFDB: Load bill, items, route, channel defaults
  BE->>BE: Validate local data and build SML payload
  BE->>SMLC: Select route: saleorder / saleinvoice / purchaseorder
  SMLC->>API: POST /api/v1/ic/* with guid + databaseName + request_id
  API->>API: Auth, tenant validation, DTO validation
  API->>SMLDB: Validate party, product, unit, warehouse/shelf, duplicate doc_no
  API->>SMLDB: Transaction insert ic_trans + ic_trans_detail
  API->>SMLDB: Post-insert normalization
  API-->>SMLC: success/data/meta or stable error object
  SMLC-->>BE: Parsed response
  BE->>BFDB: Save sml_payload, sml_response, status, error if any
  BE-->>FE: Updated bill status
  FE-->>Staff: Show sent/failed state and details
```

Bulk send uses the same send core as the single-bill retry path, but the browser first creates `POST /api/bills/bulk-send-jobs`. The backend stores `sml_bulk_jobs` + ordered `sml_bulk_job_items`, sends serially with concurrency `1`, polls progress through `GET /api/bills/bulk-send-jobs/:id`, and supports `retry-failed` as a new job that reuses the original payload snapshot.

---

## 7. SML Write Mapping

```mermaid
flowchart LR
  Retry["BillFlow retry/send"] --> Route{"Document route"}
  Route -->|"saleorder"| SO["POST /api/v1/ic/sale-orders\ntrans_flag=36"]
  Route -->|"saleinvoice"| SI["POST /api/v1/ic/sale-invoices\ntrans_flag=44"]
  Route -->|"purchaseorder"| PO["POST /api/v1/ic/purchase-orders\ntrans_flag=6"]

  SO --> Writer["Shared SML transaction writer"]
  SI --> Writer
  PO --> Writer

  Writer --> Header[("ic_trans\nheader totals, party, branch,\ndoc ref/date, VAT, doc format")]
  Writer --> Detail[("ic_trans_detail\nitems, unit, wh/shelf,\nVAT fields, line numbers")]
  Writer --> Normalize["Post-insert normalization\nitem name, unit ratios,\ndoc_date_calc/doc_time_calc,\nserial flags"]
```

### SML API Response Contract

```json
{
  "success": true,
  "data": {},
  "meta": {
    "request_id": "..."
  }
}
```

```json
{
  "success": false,
  "error": {
    "code": "duplicate_doc_no",
    "message": "doc_no already exists",
    "details": {}
  }
}
```

BillFlow clients parse this contract directly and store the response in `bills.sml_response` for support/debugging. Audit logs keep compact debug summaries instead of copying full SML payload/response into every activity row.

---

## 8. Master Data and Catalog Flow

```mermaid
flowchart TB
  SMLAPI["sml-api-bybos\n/api/v1 master reads"] --> Products["Products"]
  SMLAPI --> Customers["Customers"]
  SMLAPI --> Suppliers["Suppliers"]
  SMLAPI --> Warehouses["Warehouses / shelves"]

  Products --> Sync["BillFlow catalog sync"]
  Sync --> CatalogDB[("sml_catalog")]
  CatalogDB --> Embed["Embedding batch\nOpenRouter session_id"]
  Embed --> CatalogDB
  CatalogDB --> Match["Product matching\nreview suggestions"]
  Customers --> PartyCache["BillFlow party cache"]
  Suppliers --> PartyCache
  Warehouses --> WarehouseCache["Warehouse cache"]
  PartyCache --> Review["Review forms + retry payload"]
  WarehouseCache --> Review
  Match --> Review
```

### Catalog Status

| Data | Purpose |
|---|---|
| `sml_catalog` | Local searchable copy of SML products |
| embeddings | Similarity search for product matching |
| OpenRouter `session_id` | Groups embedding/generation logs in OpenRouter Sessions |
| `/settings/catalog` | Shows sync, embed progress, ETA, and session link |

---

## 9. Observability and Support Flow

```mermaid
flowchart LR
  Request["BillFlow request"] --> Trace["request_id / trace_id"]
  Trace --> BFLog["BillFlow audit_logs\nand app logs"]
  Trace --> AIUsage["ai_usage_logs\nOpenRouter generation/session"]
  Trace --> SMLAPILog["sml-api-bybos structured logs"]
  SMLAPILog --> ErrorCode["stable error code\nuser-fixable/support/API/SML"]
  ErrorCode --> LogsUI["BillFlow /logs\nsupport classification"]
  AIUsage --> AIUI["/settings/ai-usage\nOpenRouter session link"]
```

Detailed logs are hot data. BillFlow keeps `/logs` fast by using cursor pagination and by rolling detailed audit/AI usage rows into daily summaries before old detailed rows are purged in small batches.

### Typical Debug Order

1. Check BillFlow bill status and `sml_response`.
2. Check BillFlow `/logs` for user-facing error classification.
3. Check `sml-api-bybos` structured log by `request_id`, `doc_no`, or `trans_flag`.
4. Check SML tables `ic_trans` and `ic_trans_detail`.
5. If AI/extraction issue, open OpenRouter Sessions from `/settings/ai-usage`.

---

## 10. Production Data Lifecycle

```mermaid
flowchart LR
  HotBills[("bills\nactive review queues")] --> ArchiveJob["Daily lifecycle job\nsent/skipped older than 180d"]
  ArchiveJob --> ArchivedBills[("bills\narchived_at set")]
  HotLogs[("audit_logs + ai_usage_logs\nhot detail 90d")] --> Rollup["Daily summaries"]
  Rollup --> Summary[("audit_log_daily_summaries\nai_usage_daily_summaries\nretain 730d")]
  HotLogs --> BatchPurge["Batch purge\nmanual/scheduled safe batches"]
  OldDataUI["/settings/old-data"] --> Metrics["row count, table size,\noldest row, eligible rows"]
  OldDataUI --> DryRun["dry-run purge summary\nnothing selected by default"]
```

### Lifecycle Rules

| Data | Hot Window | Long-Term Handling |
|---|---:|---|
| `bills` with `sent` / `skipped` | 180 days active | Auto-archive by setting `archived_at`; not deleted |
| `failed` bills | No auto-archive | Kept visible for human investigation |
| `audit_logs` detail | 90 days | Roll up to daily summary, then purge detail in batches |
| `ai_usage_logs` detail | 90 days | Roll up to daily summary, then purge detail in batches |
| Daily summaries | 730 days | Used for long-range old-data reporting |

### List API Rules

| Endpoint | Production Behavior |
|---|---|
| `/api/logs` | Cursor pagination with `limit`, `cursor`, `has_more`, `next_cursor`; no total unless `include_total=true` |
| `/api/bills` | Cursor pagination plus legacy `page/per_page`; supports `archived`, `date_from`, `date_to`; defaults to active rows only |
| `/api/bills/counts` | Counts review queues in one request for `/bills`, `/sales-orders`, and `/sale-invoices` |
| `/api/bills/bulk-send-jobs` | DB-backed async SML bulk send jobs; cap 100 bills, progress polling, history list, resume active job, retry failed only |

---

## 11. Deployment and Readiness Checks

```mermaid
flowchart LR
  Preflight["scripts/preflight-main.sh"] --> BFHealth["BillFlow /health"]
  Preflight --> FEHealth["Frontend /login"]
  Preflight --> APIHealth["sml-api /health"]
  Preflight --> APIReady["sml-api /health/ready\nX-Tenant: SML1_2026"]
  Preflight --> OpenAPI["sml-api /openapi.json"]
  APIReady --> SMLTables["SML core table access\nic_inventory + ic_trans"]
```

### Production Acceptance Checklist

| Check | Expected |
|---|---|
| BillFlow backend | `GET /health` returns status OK |
| BillFlow frontend | `/login` loads current frontend asset |
| sml-api-bybos liveness | `/health` returns OK |
| sml-api-bybos readiness | `/health/ready` with `X-Tenant: SML1_2026` returns OK |
| OpenAPI | `/openapi.json` parses and Swagger UI loads at `/docs` |
| Catalog | Products synced and embeddings completed |
| Data lifecycle | Migration `037_data_lifecycle.sql` applied; `/api/bills/old-data/summary` returns policy and table metrics |
| Async bulk SML send | Migration `044_sml_bulk_jobs.sql` applied; one-bill smoke or 5-10 bill batch completes with accurate sent/failed/skipped counts |
| Golden SML write | SO/SI/PO test docs write to `ic_trans` and `ic_trans_detail` |

---

## 12. Failure Boundaries

| Boundary | Example Failure | Where It Appears | Recovery |
|---|---|---|---|
| Input parsing | Missing marketplace column or bad file | Import preview/confirm error | Fix file or column mapping |
| Product matching | Unknown product name | Review UI / marketplace aliases | Map item or create product |
| Local validation | Missing party, warehouse, shelf, unit | Retry validation error | Fix BillFlow settings/document |
| SML API validation | Duplicate `doc_no`, missing master data | Stable `error.code` from sml-api-bybos | Fix data or retry with correct doc number |
| SML DB write | Constraint/table/database issue | sml-api-bybos log + BillFlow failed status | Support investigates SML DB |
| AI provider | Timeout/rate/model error | `ai_usage_logs`, `/settings/ai-usage` | Retry or inspect OpenRouter session |

---

## 13. Source of Truth

| Topic | Source |
|---|---|
| BillFlow app workflow | `docs/overview.md` |
| Server/instance registry | `docs/deploy-instances.md` |
| Current deploy state | `docs/current-state.md` |
| sml-api-bybos migration notes | `docs/sml-api-migration.md` |
| sml-api-bybos API contract | `http://192.168.2.109:8200/docs` and `/openapi.json` |
| BillFlow main code | `/Users/nontawatwongnuk/dev_bos/billflow` |
| sml-api-bybos code | `/Users/nontawatwongnuk/dev/sml-api-bybos` |
