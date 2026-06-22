# Nexflow Behavior Inventory

Updated: 2026-06-22

Purpose: this file is the behavior-preservation checklist for the UX redesign.
Visual changes must keep these routes, API calls, permissions, states, and
dangerous actions working unless a later ADR explicitly changes behavior.

Production flags verified for the current build:

```text
VITE_PHASE=2
VITE_ENABLE_SALES_ORDERS=true
VITE_ENABLE_SHOPEE_EXCEL=true
VITE_ENABLE_SHOPEE_REALTIME_OPS=true
VITE_ENABLE_LAZADA_EXCEL=true
VITE_ENABLE_TIKTOK_EXCEL=true
VITE_ENABLE_CHAT=false
ENABLE_SHOPEE_REALTIME_OPS=true
ENABLE_SHOPEE_CANCEL_AFTER_SML_ALERTS=true
ENABLE_SHOPEE_SML_CANCEL_DOCUMENTS=true
ENABLE_SHOPEE_RICH_LINE_FLEX=true
ENABLE_SHOPEE_SETTLEMENT_LINE_ALERTS=true
ENABLE_SHOPEE_ORDER_ESCROW_ENRICHMENT=true
```

## Route Inventory

| Route | Page/component | Primary API calls | Dangerous actions | Empty/loading/error states | Expected permissions |
| --- | --- | --- | --- | --- | --- |
| `/login` | `Login` | `POST /api/auth/login` | none | invalid credentials alert; submit loading | public, rate-limited by backend |
| `/` | `Navigate` | none | none | redirects | authenticated shell; now should land on `/dashboard` |
| `/setup` | `SetupCenter` | `GET /api/setup/status`, optional `?refresh_sml=1` | `POST /api/setup/reset-test-data` with typed `RESET`, optional doc counter/email dedup reset; blocked by default when `ENV=production` | readiness cards, loading, toast errors | admin only |
| `/dashboard` | `Dashboard` | `GET /api/dashboard/stats`, `GET /api/dashboard/insights`, `GET /api/mappings/stats`, `GET /api/setup/status` | `POST /api/dashboard/insights/generate` | setup warning, no-documents CTA, loading skeletons | dashboard read authenticated; setup status and insight generation require admin |
| `/shopee-operations` | `ShopeeOperations` | `GET /api/shopee-operations/readiness`, `GET /api/shopee-operations/orders`, `GET /api/shopee-operations/counts`, `GET /api/shopee-operations/:shop_id/:order_sn/timeline`, tracking/shipping parameter APIs, SSE updates | sync now, create document, bulk create documents, ship order, create/download shipping document, create SML cancel document, payment refresh read-only but calls Shopee | empty shop/order states, cancelled-after-SML badge, payment card states, timeline loading/error, shipping dialogs | admin/staff for operations; SML cancel/create actions require backend preconditions and feature flags |
| `/bills` | `Bills mode=purchase-order` | `GET /api/bills`, `GET /api/bills/counts`, `GET /api/settings/imap-accounts`, `GET /api/shopee-api/connections` | archive/restore, delete, permanent delete, bulk SML job | empty purchase queue, filters, pagination, table loading | read authenticated; archive/restore/bulk admin/staff; delete admin |
| `/sales-orders` | `Bills mode=sales-order` | same bills APIs with `bill_type=sale&document_route=saleorder` | same as `/bills` | empty Sales Order queue must remain usable | route visible when sales flag true; same bill permissions |
| `/sale-invoices` | `Bills mode=sale-invoice` | same bills APIs with `bill_type=sale&document_route=saleinvoice` | same as `/bills` | primary marketplace production queue; empty state still works | route visible when sales flag true; same bill permissions |
| `/bills/:id`, `/sales-orders/:id`, `/sale-invoices/:id` | `BillDetail` | `GET /api/bills/:id`, `GET /api/bills/:id/timeline`, artifact preview/download, catalog search/product APIs, SML master data lookups | `POST /api/bills/:id/retry`, item add/edit/delete, confirm match, regenerate/latest doc no, ensure Shopee shipping line | loading, not found/error, validation blocks, SML send progress/success/error dialogs | read authenticated; item edits/admin send controls admin/staff where backend enforces; retry endpoint currently authenticated |
| `/import` | `Import` | `POST /api/import/upload`, `POST /api/import/confirm` | confirm writes local bills | upload preview, parse errors, confirmation result | admin/staff |
| `/import/shopee` | `ShopeeImport` | `GET /settings/shopee-config`, `GET /api/settings/shopee-api/status`, `GET /api/shopee-api/connections`, `GET /api/import/shopee/runs`, `POST /api/import/shopee/preview`, `POST /api/import/shopee/api/preview`, `POST /api/import/shopee/confirm`, `POST /api/shopee-api/auth-url`, `PATCH /api/shopee-api/connections/:id` | confirm import writes bills; OAuth connect; disable/enable connection | no connection, token/status warnings, preview errors, duplicates/no-SKU summaries, loading run history | config read authenticated; import/status admin/staff; OAuth and connection edits admin |
| `/shopee-settlements` | `ShopeeSettlement` | `GET /api/shopee-api/connections`, `GET /api/settings/shopee-settlement-defaults`, `GET /api/settings/shopee-api/status`, `GET /api/shopee-settlements`, `GET /api/shopee-settlements/counts`, `GET /api/shopee-settlements/:id` | preview, reconcile, send to SML, hide/restore run | empty runs, loading, detail error, send validation | admin/staff; settlement defaults update admin |
| `/import/lazada` | `LazadaImport` | `GET /api/import/lazada/runs`, `GET /api/settings/lazada-config`, `GET/PUT /api/settings/column-mappings/lazada`, `POST /api/import/lazada/preview`, `POST /api/import/lazada/confirm` | confirm writes bills; admin column mapping update | upload/preview/errors/run history | preview/confirm admin/staff; mapping update admin |
| `/import/tiktok` | `TikTokImport` | `GET /api/import/tiktok/runs`, `GET /api/settings/tiktok-config`, `GET/PUT /api/settings/column-mappings/tiktok`, `POST /api/import/tiktok/preview`, `POST /api/import/tiktok/confirm` | confirm writes bills; admin column mapping update | upload/preview/errors/run history | preview/confirm admin/staff; mapping update admin |
| `/mappings` | `Mappings` | `GET /api/mappings`, `GET /api/mappings/stats`, `GET /api/bills?status=needs_review`, `GET /api/bills/:id` | create/update/delete mappings | loading, empty mapping table, delete confirmation | read authenticated; create/update admin/staff; delete admin |
| `/marketplace-aliases` | `MarketplaceAliases` | `GET /api/marketplace-aliases/review-groups`, `POST /api/marketplace-aliases/confirm` | confirms alias and may mark ready bills | loading, empty review queue, confirm modal/errors | admin/staff |
| `/settings/old-data` | `OldDataSettings` | `GET /api/bills/old-data/summary`, `POST /api/bills/old-data/archive`, `POST /api/bills/old-data/purge` | archive old bills; purge archived bills/audit/AI logs | summary loading/errors, purge confirmation | admin only |
| `/logs` | `Logs` | `GET /api/logs`, inline `POST /api/bills/:id/retry` for `sml_failed` rows | retry from log row can send SML | loading, cursor pagination, filters, raw/detail expand, retry toast | list admin/staff; retry authenticated |
| `/bulk-send-jobs` | `BulkSendJobs` | `GET /api/bills/bulk-send-jobs`, `GET /api/bills/bulk-send-jobs/:id` | retry failed job if exposed by detail controls | empty history, loading, status/detail error | admin/staff |
| `/settings/catalog` | `CatalogSettings` | `GET /api/catalog`, `GET /api/catalog/stats`, `GET /api/settings/instance`, `GET /api/catalog/hidden-codes`, `POST /api/catalog/sync`, `POST /api/catalog/embed-all`, `POST /api/catalog/reload-index`, `POST /api/catalog/:code/embed`, `POST /api/catalog/:code/refresh`, `POST /api/catalog/refresh-batch`, `DELETE /api/catalog/:code` | full sync, embed-all, reload index, refresh/delete catalog rows | loading, sync running, empty catalog, hidden-code warnings | read authenticated; unit/product create admin/staff; sync/embed/refresh/delete admin |
| `/settings/email` | `EmailAccounts` | `GET/POST/PUT/DELETE /api/settings/imap-accounts`, test/list folders, poll job, reset progress, active poll jobs | create/update/delete inbox, poll now, reset progress/backlog | production currently empty; loading, active poll progress, warning/error states | admin only |
| `/settings/channels` | `ChannelDefaults` | `GET/PUT /api/settings/channel-defaults`, SML party/master lookups from edit dialog | changes SML routing, doc format, doc counter prefix/running format | unset route warnings, edit dialog validation | admin only |
| `/settings/instance` | `InstanceSettings` | `GET/PUT /api/settings/instance`, `POST /api/settings/instance/test-connection`, `POST /api/settings/instance/restart`, `GET /health` | config save and backend restart | loading, pending restart, connection test results/errors | admin only |
| `/settings/line-notifications` | `LineNotifications` | `GET /api/settings/line-notifications`, sender/recipient CRUD, sender test, recipient rich Flex test | create/update/delete LINE sender/recipient; test push consumes LINE push quota | readiness cards, masked IDs, recent delivery states, rich Flex fallback sample, failed test error | admin only |
| `/settings/ai-usage` | `AIUsage` | `GET /api/ai-usage/summary`, `GET /api/ai-usage/logs` | none | loading, empty usage/log states, filters | admin only |
| `/settings/users` | `UserSettings` | `GET/POST/PUT/DELETE /api/settings/users` | create/update/delete users, role/password changes | loading, form validation, delete confirmation | admin only; sidebar hides for non-admin |
| `/settings` | redirect | none | none | redirects to `/settings/instance` | authenticated |

## Disabled Or Development Routes

- `/messages`, `/settings/line-oa`, `/settings/quick-replies`, and
  `/settings/chat-tags` are hidden in production because `VITE_ENABLE_CHAT=false`.
  Source code remains present and should not be broken by shared UI changes.
- `/settings/line-notifications` is not part of LINE chat; it remains visible to
  admins while `VITE_ENABLE_CHAT=false` because Shopee alerting is active.
- `/dev/showcase` is available only in Vite dev mode.

## Redesign Constraints From Inventory

- No backend route, SML routing rule, migration, env, role middleware, retry body,
  import confirmation payload, or pagination/filter query shape changes.
- Keep existing polling intervals. Sidebar queue polling remains the 60s safety net
  unless a separate performance change is tested and approved.
- Do not execute these during QA without explicit approval: import confirmations,
  SML sends/retries, reset test data, delete/purge, connection disable, catalog
  delete, or settings saves.
- Primary marketplace production path is `/sale-invoices` because production data is
  mostly `saleinvoice`; Shopee Realtime uses saleinvoice, while legacy Shopee import
  can still create saleorder documents. `/sales-orders` must stay visible and functional.
- Page render and LINE worker must not call Shopee live APIs inline. Payment
  breakdown is cached in `shopee_order_payment_snapshots`; refresh actions are
  explicit and read-only.
