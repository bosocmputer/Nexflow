# AGENTS.md — Nexflow

> อ่านไฟล์นี้ให้ครบก่อนเริ่ม code ทุกครั้ง — ห้าม assume สิ่งที่ไม่ได้ระบุ
> **local workspace:** `/Users/nontawatwongnuk/dev_bos/Nexflow`
> **production server:** `10.121.20.83` (`ubuntu`)
> **production folders:** `/mnt/data/nextstep-node-2/nexflow` (demo), `/mnt/data/nextstep-node-2/nexflow-aoy` (aoy), `/mnt/data/nextstep-node-2/nexflow-lanboon` (lanboon)
> **deploy flow:** ดู [docs/nextstep-server-deploy-flow.md](docs/nextstep-server-deploy-flow.md)
> **legacy DEV only:** `192.168.2.109` / ngrok / `/home/bosscatdog/billflow-henna`

---

## 0. Current UAT Handoff (2026-08-27)

Read this section first when resuming work in a new session.

- Application baseline is intentionally split for AOY-only UAT: AOY runs
  `0470b3c` (`fix: localize Shopee SML audit history`),
  while Demo and Lanboon remain on `82baa4a` (`Simplify marketplace stock
  quantity setup`). The Central SML Gateway is on `42992f5`
  (`feat: separate SML sale cancellations from credit notes`).
- Tenant databases, SML tenants, credentials, channel routes, feature flags, and
  deployment revisions remain isolated and must never be copied between
  instances without an explicit migration request.
- Verified on 2026-08-25 after deploying application commit `82baa4a`:
  Demo, AOY, Lanboon, and both Central Gateway health endpoints returned HTTP
  200. Each tenant backend resolved the Central Shopee Gateway on the shared
  Docker network.
- Migration 085 is applied on all three tenant PostgreSQL 16 databases. The new
  Catalog-generation, exact SML attempt, reservation, mapping-job, stock-policy
  job, and asynchronous stock-run tables/columns exist. Post-deploy checks found
  zero queued mapping/backfill/policy/stock jobs and zero active reservations.
- Marketplace activation remains tenant-scoped and fail-closed:
  - Demo: unit Catalog enabled for SML validation; grouped UI, conversion,
    reservation ledger, and set-stock remain disabled. Active generation 3 has
    206 products, 371 units, and 115 multi-unit products with no invalid units.
  - AOY: grouped UI and unit Catalog enabled; conversion is `active`; reservation
    ledger enabled; set-stock enabled under the user's explicit controlled-UAT
    risk acceptance. Runtime readiness is true for Catalog, mapping, and
    reservations. Of 72 aliases, 71 are ready and one TikTok alias needs review
    because its saved unit is no longer valid for the mapped item.
  - Lanboon: application code updated, but all five marketplace feature gates
    remain disabled pending tenant-specific preflight.
- Pre-deploy database backups from 2026-08-25 are stored under
  `/mnt/data/nextstep-node-2/nexflow-backups`: Demo
  `pre-deploy-20260825-080734.sql.gz`, AOY
  `pre-deploy-20260825-080912.sql.gz`, and Lanboon
  `pre-deploy-20260825-081050.sql.gz`.
- Activation backups are also stored under the same tenant folders. Demo and AOY
  use the `pre-marketplace-activate-20260825-044809.sql.gz` snapshots; Lanboon's
  code-alignment backup is `pre-marketplace-code-20260825-054410.sql.gz`.
- AOY is the active production UAT tenant. As of 2026-08-26 the user is actively
  testing the current AOY build. Do not treat silence as acceptance; inspect the
  reported tenant, record, and runtime logs before fixing any new feedback.

Current AOY UAT scope:

1. Marketplace sales flow: Marketplace input -> Nexflow bill -> Product Master
   mapping -> SML sale invoice -> SML stock recalculation.
2. Marketplace amount correctness: full product amount, discount, buyer-paid
   shipping item, and net document totals for Shopee API/Excel, Lazada Excel, and
   TikTok Excel.
3. Unified Marketplace Product Master shared by imports, Bill Detail, and Shopee
   stock mapping.
4. Shopee stock sync: exactly one SML warehouse/location, dry-run before writes,
   full stock numbers, shared-stock allocation, and excluded-location details.
5. SML set-product document expansion. Controlled AOY document
   `BF-INV26080028` was verified in SML and then deleted by the user. AOY document
   expansion is enabled; Shopee set-stock still lacks a real Shopee set-product
   UAT case.
6. UI under feedback: `/sale-invoices` input-channel filters/tags and compact
   `/shopee-operations` rows/status explanations. Shopee brand color is
   `#EE4D2D`.
7. Shopee READY_TO_SHIP Auto SML migrations 082–083 and UI are deployed. Demo and
   Lanboon have the global flag disabled. AOY has
   `SHOPEE_AUTO_SML_ENABLED=true`, but both AOY shop settings remain disabled,
   have no cutoff, and the durable queue is empty. Do not enable a shop without
   a controlled new order and the checklist in
   `docs/shopee-auto-sml-runbook.md`.
8. Auto SML persists `doc_time` immediately before its first SML write using
   Asia/Bangkok and reuses it across retries. It is not a fixed channel setting.
9. Controlled normal-product Shopee stock UAT passed for shop `264993963`, item
   `6278820512`, model `43634992848`, mapped to SML `AH-0033` / `แท่ง` at factor
   1. Preview `cd5a5944-8955-4660-b32d-baaccbe2e672` proved target stock 1 from
   SML balance 1 and pending demand 0. Manual sync
   `63ca255f-c56c-496f-b5f4-011bb79f2a99` changed Shopee 0 -> 1 with no error or
   unknown result; catalog read-back confirmed seller stock 1. Final preview
   `8a03d431-7263-4f9e-a7c7-d6cac213ecfb` was unchanged 1 -> 1. Automatic stock
   sync remains disabled.
10. Marketplace unit and Shopee-stock configuration UX was simplified in
    `9f2461f`–`82baa4a`: normal users only choose the SML item, unit, and one of
    two quantity modes: follow Marketplace quantity (multiplier 1) or set an
    integer SML quantity per Marketplace item. Sales/stock policy controls are
    no longer shown in the normal flow. A stock check promotes safe ready
    listings to managed and creates equal shared-stock allocation only for a
    completely unconfigured duplicate group; existing partial or explicit pool
    configuration is preserved. Manual sync is independent from the automatic
    schedule but still requires a fresh successful stock check and no safety
    pause.
11. AOY post-deploy stock check on 2026-08-25 passed for all 44 active Shopee
    listings: 6 targets would change, 38 were unchanged, and 0 were blocked.
    The previous 37 `stock_policy_blocked` listings no longer require a manual
    policy choice. Existing shared pools passed validation. The check found an
    excluded negative balance of -3 `กล่อง` in `AB-2 / 002`; it is displayed but
    excluded from the selected `AB-1 / 001` calculation. No Shopee stock write
    was performed during this validation. For shop `264993963`, automatic stock
    sync remains disabled and there is no active pause. On 2026-08-25 the user
    explicitly accepted the pending set-product UAT risk and enabled
    `SHOPEE_SET_STOCK_ENABLED=true` for the AOY instance only so AOY staff can
    test the first real mapped set product manually. Demo and Lanboon were not
    changed. Treat AOY's first real set-product preview/write as controlled UAT;
    do not enable the automatic schedule until the Seller Centre read-back and
    SML component-balance checklist pass.
    Desktop and 390px mobile QA passed for `/marketplace-aliases` and
    `/settings/shopee-stock`; the browser console had no new warnings/errors.
12. AOY-only Catalog Marketplace visibility is deployed at `990c028`. Migration
    086 adds an active-alias lookup index; `/settings/catalog` now shows bounded
    Shopee/Lazada/TikTok mapping counts for each SML item and lazy-loads the
    matched account, product, variant, and SKU details. Production browser QA
    found 41 mapped rows and 9 unmapped rows on the first 50-item page; opening
    an item showed five cross-channel variants correctly. Catalog remains at 65
    active products and the alias table remains at 72 rows. The pre-deploy AOY
    backup is `pre-deploy-20260825-084650.sql.gz`. Demo and Lanboon did not
    receive migration 086 or the Catalog UI/API change.
13. AOY-only excluded-location product identity is deployed at `039b9cf`.
    `/settings/shopee-stock` keeps the exact excluded warehouse/location summary
    while also showing bounded SML item details: item code, item name, unit,
    warehouse/location, and balance. Details are capped at 200 rows in the run
    summary with exact total/negative counts to protect preview response size.
    Production dry-run `ae495fbe-6240-4014-94dd-7058f98d1358` checked all 44
    listings with 0 changed and 0 blocked. The excluded AB-2/002 aggregate -3
    `กล่อง` was resolved into `AH-0001` (-1 กล่อง) and `AH-0003` (-2 กล่อง),
    including their SML names. Only the preview endpoint ran; no Shopee stock
    write endpoint was called. The pre-deploy backup is
    `pre-deploy-20260825-090208.sql.gz`. Demo and Lanboon were not deployed.
14. AOY-only Marketplace channel-label cleanup is deployed at `e069f2b`.
    `/sale-invoices`, `/marketplace-aliases`, and `/settings/catalog` use one
    input-channel vocabulary: `Shopee API`, `Shopee Excel`, `Lazada Excel`, and
    `TikTok Excel`. Because a scoped Shopee Product Master mapping is shared by
    API and Excel, both tags are shown; the connected shop is displayed inside
    the API tag as `Shopee API (Henna.milkford)`. Generic `บัญชีหลัก` labels and
    the detached shop-name line were removed. Production browser QA passed for
    the grouped Product Master, Catalog summary, and Catalog link dialog; AOY
    health and both routes returned HTTP 200. The two latest pre-deploy AOY
    backups are `pre-deploy-20260825-091802.sql.gz` and
    `pre-deploy-20260825-092638.sql.gz`. Demo and Lanboon remain on `82baa4a`.
15. AOY-only Shopee stock-column clarification is deployed at `44b5a72`.
    Expanded product groups now identify the three numbers as `สต๊อก SML`,
    `สต๊อก Shopee`, and `สต๊อกที่จะส่งไป Shopee`; the last value is the absolute
    target Nexflow will set, not a quantity to add. At wide desktop sizes the
    labels appear as aligned column headers. Laptop and smaller content widths
    use a stacked row with labels beside every value so the target and mapping
    action are not clipped by the sidebar. Production browser QA passed at the
    default laptop viewport and 1440px, with no console warnings/errors. The
    verification was read-only: no preview or Shopee stock write was triggered.
    The final pre-deploy AOY backup is `pre-deploy-20260826-021900.sql.gz`.
    Demo and Lanboon remain on `82baa4a`.
16. AOY-only SML outstanding-sales-order stock deduction is deployed at
    `1cd9af0`; additive migration 087 is applied on AOY. The shared Central SML
    Gateway is deployed at `7723a0b` and AOY is explicitly configured with
    `SML_STOCK_AVAILABILITY_MODE=net_sale_order_v1` plus the verified source
    fingerprint. Stock availability is now `max(SML physical - active SML
    TRANS_FLAG 36 outstanding, 0)`, followed by Nexflow pending demand and the
    saved unit conversion. The gateway subtracts active TRANS_FLAG 44 invoice
    fulfillment by exact source document/item identity and fails closed on
    ambiguous or invalid evidence. AOY document `SO26040001` has four active
    item lines and `INV26040015` fulfills all four exactly, so its verified
    current outstanding amount is zero. Active-mode production dry-run
    `7237f377-da68-43be-b0cf-04bea5134cd0` checked all 44 mapped listings: all 44
    source fingerprints and formulas matched, with 0 blocked and 0 errors. The
    expanded UI now labels the derived value `SML พร้อมใช้` and shows the
    per-item breakdown such as `คงเหลือ 7 แท่ง − ค้างส่ง 0 แท่ง`. This rollout
    performed no Shopee stock write, and both AOY automatic schedules remain
    disabled. Net availability for set products remains fail-closed until a
    real set-product sales-order case proves component semantics. The AOY
    pre-deploy database backup is `pre-deploy-20260826-041945.sql.gz`; runtime
    `.env` backups with `pre-stock-net` prefixes are retained in the AOY folder.
    Demo and Lanboon application/runtime modes were not changed.
17. AOY Shopee catalog sync connectivity incident is resolved at `9bebf3e`.
    Recreating only `nexflow-aoy-backend` had removed its manually attached
    `nexflow-shopee-gateway_default` network, causing Docker DNS lookup failures
    and HTTP 500 from `/api/settings/shopee-stock/264993963/catalog-sync`. The
    managed Compose override now declares both the tenant default network and
    the external Shopee Gateway network, so future backend recreates preserve
    connectivity. The deployment script also probes Gateway health from inside
    the fresh backend and fails the deploy if DNS/connectivity is unavailable.
    Post-deploy browser QA called the same catalog-sync endpoint successfully:
    HTTP 200 in 2.217 seconds, 45 stored products / 44 active, and no browser
    console or recent backend errors. The pre-deploy AOY backup is
    `pre-deploy-20260826-043401.sql.gz`. Demo and Lanboon were not deployed.
18. AOY-only Shopee Operations timeline close race is fixed at `40ac450`.
    The controlled drawer previously wrote `?order=<order_sn>` while open; on
    close, `timelineOpen=false` could render before React Router removed the
    query, so the deep-link effect immediately reopened the same order and made
    users press close twice. A dismissed-order guard now blocks only that
    transition until the query is cleared. Regression tests cover initial deep
    links, dismissal of the same order, a different order, empty queries, and
    the already-open state. Production browser QA passed for one-click X close,
    Escape close, opening another order, reopening the first order, query
    cleanup, and a clean console. AOY health remained HTTP 200 with no recent
    backend 500/panic/fatal errors. The pre-deploy AOY backup is
    `pre-deploy-20260826-044708.sql.gz`. Demo and Lanboon were not deployed.
19. AOY Auto SML order `260826C78TFM12` was reconciled end to end on
    2026-08-26. The READY_TO_SHIP push produced exactly one Auto SML job, one
    active bill `5d78fcab-8b50-4a66-a5e8-33073328618e`, and one immutable SML
    attempt for `BF-INV26080055`; it succeeded on the first attempt. SML returned
    HTTP 201 and the durable stock-recalculation job subsequently verified exact
    `AH-0003` stock-movement evidence before incorporating the reservation. LINE
    sent the new-order and Auto SML success notifications once to each of two
    enabled recipients, with no failed delivery. AOY-only commit `6f6f74f` now
    adds a bounded `รายการสินค้า` section to future Auto SML Flex and text
    notifications: up to five Marketplace product/variant lines with Marketplace
    quantity, excluding the synthetic Shopee shipping line and all buyer PII;
    longer orders show the remaining line count. The historical notification was
    intentionally not resent. Post-deploy health, Gateway connectivity, Auto SML
    enabled/unpaused state, and recent error logs passed. The pre-deploy AOY
    backup is `pre-deploy-20260826-060440.sql.gz`. Demo and Lanboon remain on
    `82baa4a`.
20. AOY-only Shopee Operations Auto SML status cleanup is deployed at
    `0a1a616`. A successful automatic send with a persisted SML document now
    renders as exactly two lines: `ส่ง SML แล้ว (AUTO)` and the SML document
    number. Queued, running, retry, needs-review, failed, manual-send, and
    missing-document cases remain separate so operational failures are not
    hidden. Production browser QA on order `260826C78TFM12` verified one merged
    badge, `BF-INV26080055`, no duplicate `Auto: ส่ง SML แล้ว` label, and a clean
    console. Frontend regression tests, lint with zero errors, production build,
    sales-only guard, deployment health, and Gateway health passed. The
    pre-deploy AOY backup is `pre-deploy-20260826-061917.sql.gz`. Demo and
    Lanboon remain on `82baa4a`.
21. AOY-only collapsed Shopee stock summaries are deployed at `8ba0bd6`
    (feature commit `9e85519`). Every grouped parent row now shows the safe
    aggregate `SML พร้อมใช้รวม`, current `Shopee ปัจจุบันรวม`, and absolute
    `Shopee เป้าหมายรวม`, plus whether any variants would change, without
    loading or expanding its children. The aggregate fails closed: a group that
    participates in shared allocation shows `สต๊อกร่วม` instead of summing SML
    availability, and mixed base units show `หลายหน่วย`; current and target
    Shopee totals remain visible when independently safe. Successful manual
    Catalog refreshes and stock writes now also leave a persistent result alert,
    while the existing stock-check result remains visible after its async job.
    AOY's 44 ready variants resolve into 11 parent rows: eight exact SML totals,
    two shared-stock indicators, and one mixed-unit indicator. The bounded parent
    query uses no child N+1 loading and executed in 4.643 ms on the current AOY
    dataset. Production QA passed at the default
    1100px laptop viewport and 390px mobile, all 11 collapsed summaries were
    present, and the browser console plus recent backend error scan were clean.
    QA was read-only and did not call Catalog refresh, preview, or Shopee stock
    write endpoints. Shop `264993963` was already showing automatic stock sync
    enabled every five minutes; this deployment did not change that setting.
    The final pre-deploy AOY backup is
    `pre-deploy-20260826-085727.sql.gz`. Demo and Lanboon remain running their
    existing `82baa4a` containers.
22. AOY-only verifiable shared/mixed-unit stock totals are deployed at
    `ea803ec`. Collapsed parent rows no longer replace quantities with the
    internal labels `สต๊อกร่วม` or `หลายหน่วย`. The backend deduplicates each
    SML item code before aggregating, so AOY's two shared-stock parents each
    display the proven `91 กล่อง` source total with a warning that the balance
    is shared with another Shopee product. The mixed-unit parent displays
    `0 แท่ง · 0 แพ็ค` and warns that unlike units must be inspected separately;
    they are never added into a false scalar total. The old scalar API fields
    remain compatible and the new per-unit breakdown is additive. Production
    API requests completed in 6.8–18.9 ms, laptop and 390px mobile browser QA
    passed, and the browser console plus recent backend error scan were clean.
    Verification was read-only and did not call stock preview or Shopee write
    endpoints. At verification time both AOY shops had automatic sync disabled;
    shop `264993963` retained its five-minute interval and fresh dry-run state,
    while shop `1029622928` remained stale with
    `catalog_generation_reconcile`. This deployment did not change either shop
    setting. The pre-deploy AOY backup is
    `pre-deploy-20260826-091915.sql.gz`. Demo and Lanboon remain on `82baa4a`.
23. AOY-only Shopee cancellation destinations are deployed at application
    `0f31748` (feature commit `cf93b4c`) with additive migration 088; the shared
    Central SML Gateway is deployed at `42992f5`. The channel dialog now offers
    the two verified AOY document semantics separately: TRANS_FLAG 45
    `ยกเลิกขายสินค้าและบริการ` through `/void` and SML screen `SIC`, or
    TRANS_FLAG 48 `รับคืนสินค้า/ลดหนี้` through `/cancel` and SML screen `ST`.
    `doc_format_code` is loaded from the selected SML screen and is selectable
    when the tenant has multiple formats. The current AOY legacy setting was
    normalized to the corrected TRANS_FLAG 48 route and remains `CN`; no user
    destination choice was overwritten. Read-only production previews against
    source invoice `BF-INV26080055` proved TRANS_FLAG 45 as a header-only void
    and TRANS_FLAG 48 as a two-line return/credit note with source references.
    No cancellation document was created and the durable cancellation table
    remained empty. Successful future creates have bounded LINE notification
    support without buyer PII, but creation remains a confirmed operator action
    during UAT rather than an automatic external write. Browser QA verified both
    destination choices, real `SIC`/`CN` formats, the source-invoice explanation,
    the accurate `ตัวอย่างรูปแบบเลขเอกสาร` label, and a clean console. AOY
    backups are `pre-deploy-20260826-110234.sql.gz`,
    `pre-deploy-20260826-110730.sql.gz`, and
    `pre-deploy-20260826-111132.sql.gz`; the Gateway source/runtime/image backups
    use the `pre-sml-cancel-20260826-1054` and `pre-42992f5-20260826-1059`
    prefixes. Demo and Lanboon remain on `82baa4a`.
24. AOY-only automatic Shopee cancellation-after-SML is deployed and enabled at
    `95a8c8d` with additive migration 089 and
    `SHOPEE_AUTO_SML_CANCEL_ENABLED=true`. The current AOY destination remains
    TRANS_FLAG 48 `รับคืนสินค้า/ลดหนี้` with format `CN`; TRANS_FLAG 45
    `ยกเลิกขายสินค้าและบริการ` remains selectable by an admin. Only a newly
    observed transition to exact Shopee `CANCELLED` can enqueue work; historical
    cancelled snapshots are not backfilled and `IN_CANCEL` waits for the final
    state. The worker snapshots the route, document number, and byte-stable SML
    payload before the first external write, then uses bounded exact retries.
    Successful document creation queues SML stock recalculation separately, so
    a recalculation failure cannot resend the cancellation document. Manual and
    automatic creation share one durable record and successful ownership to
    prevent duplicates. Success and terminal-failure LINE notifications contain
    no buyer PII. Production browser QA verified the two destinations, the
    current CN format, disabled historical actions, and a clean console. After
    more than four minutes live, the cancellation table remained empty and the
    backend had no panic/fatal/error/5xx log. The AOY database backup is
    `pre-deploy-20260826-115315.sql.gz`; the pre-flag runtime backup is
    `.env.pre-auto-sml-cancel-20260826-115455`. Demo and Lanboon were not
    recreated or enabled. A real new-order cancellation is still required to
    complete the first external-write UAT; record the Order SN, generated CN,
    LINE delivery, and post-recalculation stock evidence.
25. AOY-only Shopee cancellation queue navigation and status presentation are
    deployed at `a4c67a8`. The new sidebar shortcut
    `เอกสารยกเลิก/รับคืน Shopee` reuses the existing
    `/shopee-operations?status_group=cancelled` page and the existing
    `shopee_operations` permission; it does not add a second queue or a second
    workflow. The filtered page now identifies the cancellation document type,
    automatic or user-triggered creation, SML document number, creation result,
    and durable SML stock-recalculation result. The API exposes semantic
    `sale_cancel` / `credit_note` presentation values rather than leaking raw
    route endpoints. Production browser QA passed on desktop and 390px mobile:
    the shortcut, breadcrumb, heading, selected cancelled tab, and
    `ใบขาย / เอกสารหลังยกเลิก` column were present, the mobile page had no
    horizontal overflow, and the console was clean. Runtime verification found
    all three cancellation/Auto SML feature gates enabled, shop `264993963`
    enabled and unpaused, the active TRANS_FLAG 48 `CN` route unchanged, two
    enabled LINE recipients, and zero cancellation rows; no historical
    cancellation was backfilled. The final pre-deploy AOY backup is
    `pre-deploy-20260826-124252.sql.gz`. Demo and Lanboon were not rebuilt and
    remain on `82baa4a`. The first real new-order cancellation UAT is still
    pending.
26. AOY-only configurable Auto SML trigger and final-bill shipping details are
    deployed at `0cf1a97` with additive migration 090. Each shop may use
    `READY_TO_SHIP` (default/recommended) or `PROCESSED`; changing the trigger
    creates a new optimistic configuration version and cutoff for future
    transitions only. Jobs snapshot the trigger, exact transition evidence,
    and configuration version, while forward lifecycle states may finish the
    same immutable job. Unknown/missing evidence fails closed, and
    `UNPAID`/`IN_CANCEL`/`CANCELLED` cannot create an SML sale. AOY shop
    `264993963` remains enabled, unpaused, and unchanged at `READY_TO_SHIP`,
    `config_version=1`, with its existing 2026-08-24 cutoff; no queued job or
    historical order was created. The two existing jobs remain succeeded.
    Future Auto SML LINE Flex/text adds a separate `ค่าจัดส่งเข้า SML` section
    only when the final bill contains `source_sku=__shopee_shipping__`.
    Production evidence confirms `BF-INV26080055` will show
    `15.00 บาท · AH-0061 · 1 ชิ้น`, while `BF-INV26080059` has no shipping line
    and will not show estimated Shopee fees. Desktop and 390px browser QA passed
    without saving `PROCESSED`; the selected value, no-backfill copy, queue
    snapshot copy, and two enabled LINE recipients were visible, with no
    overflow or console error. Go tests, race tests, vet, frontend regression
    tests, lint/build, sales-only guard, schema constraints, health, Gateway
    connectivity, and post-deploy error scan passed. Auto SML, automatic
    cancellation, set-stock, sales/CN routes, and LINE recipients were unchanged.
    The AOY backup is `pre-deploy-20260826-145316.sql.gz`. Demo and Lanboon were
    not deployed and remain on `82baa4a`. A controlled real `PROCESSED` trigger
    order remains pending user UAT; do not switch AOY automatically.
27. AOY-only Shopee Operations status cleanup is deployed at `5e8ac44`.
    Successfully sent manual bills now show only `ส่ง SML แล้ว` and the SML
    document number; the redundant `Auto: ส่งด้วยมือ` badge is suppressed.
    Successful automatic bills still show `ส่ง SML แล้ว (AUTO)`, while queued,
    running, retry, needs-review, failure, pause, cancellation, and other
    actionable Auto SML states remain visible. Production browser QA verified
    manual order `260824752AP990` / `BF-INV26080056`, automatic order
    `260826C78TFM12` / `BF-INV26080055`, zero occurrences of the removed badge,
    and a clean console. Frontend regression tests, lint, production build,
    sales-only guard, deployment health, Gateway connectivity, and the recent
    backend error scan passed. The AOY pre-deploy backup is
    `pre-deploy-20260826-151707.sql.gz`. No migration or runtime configuration
    changed; Demo and Lanboon were not deployed and remain on `82baa4a`.
28. AOY's first real automatic cancellation-after-SML UAT completed for Shopee
    order `260827ECCFMCSC`. READY_TO_SHIP Auto SML created exactly one bill and
    immutable sale attempt for `BF-INV26080060`; the later exact `CANCELLED`
    transition created exactly one TRANS_FLAG 48 `รับคืนสินค้า/ลดหนี้` document
    `CN26080002`, then completed the separate SML stock-recalculation job. The
    order, bill, sale attempt, cancellation record, CN, reservation, and LINE
    outbox showed no duplicates or failed delivery. AOY-only commit `17da3da`
    compacts the cancelled order's document cell to two visible lines:
    `รับคืนสินค้า/ลดหนี้แล้ว (AUTO)` and
    `BF-INV26080060 → CN26080002`; an accessible info button shows the document
    type, trigger, both document numbers, create result, stock-recalculation
    result, and any error. Future cancellation-document notifications are stored
    as immutable red Flex payloads with a bounded Marketplace item list and no
    buyer PII. Future Shopee new-order, Auto SML, and cancellation-document Flex
    messages omit the `เปิดใน Nexflow` footer button; text fallbacks retain the
    URL for delivery recovery. Already-sent historical LINE messages are not
    rewritten or resent. This change does not modify SML amounts, VAT, routes,
    `vat_type`, Auto SML settings, or automatic-cancellation settings. Production
    browser QA verified the compact row and details popover on the real order,
    with no console warning/error. Frontend tests/lint/build, Go tests/race/vet,
    sales-only guard, deploy health, Gateway health, and the backend error scan
    passed. The AOY pre-deploy backup is
    `pre-deploy-20260827-024219.sql.gz`. Demo and Lanboon were not deployed and
    remain on `82baa4a`.
29. AOY-only Thai Auto SML/cancellation audit presentation is deployed at
    `0470b3c`. The raw immutable action keys and historical audit rows are
    unchanged, but the shared UI metadata used by `/logs` and each bill
    timeline now renders the Auto SML setting, manual/automatic cancellation
    creation, terminal failure, and post-cancellation stock-recalculation
    events in Thai. Shopee realtime creation shows Order SN/item count;
    successful Auto SML shows `ส่งอัตโนมัติจาก Shopee`; cancellation history
    shows the exact sale-to-cancel document transition and item-code count.
    The `/logs` SML quick view now includes `shopee_sml_*` and
    `shopee_auto_sml_*` actions, so the real AOY `BF-INV26080060 → CN26080002`
    lifecycle appears without raw technical event names. Business milestones
    and terminal errors belong in `audit_logs`; queue leases, retry counters,
    and worker internals remain in their durable job tables to avoid noisy or
    misleading user-facing history. Production browser QA verified the four
    existing events on bill `67b51c10-5790-4205-8fca-74fa59813383`, all 15
    current SML quick-view rows including both cancellation events, and a clean
    console. Frontend regression tests, lint, production build, sales-only
    guard, deploy health, Gateway health, and recent backend error scan passed.
    The AOY pre-deploy backup is `pre-deploy-20260827-030228.sql.gz`. No
    migration, audit backfill, VAT/amount logic, runtime setting, Demo, or
    Lanboon deployment changed.
30. AOY-only Marketplace mapping reconciliation and buyer-charge explanation
    are deployed at `be5f7e7` (feature commit `6f5ff04`). TikTok bill
    `c06503f9-335d-40ef-9eb5-fb8604b88e07`, order
    `585684843131602849`, now reuses the existing exact scoped alias
    `ea24e04c-86ec-43f5-a9d8-d1e0635eea1c` for `AH-0006 / แท่ง` instead of
    attempting a duplicate insert. The failed mapping job
    `69d3af4f-32dd-46e3-a6ad-7b45998d7932` was retried successfully after
    correcting the PostgreSQL quantity-multiplier parameter type. Reservation
    reconciliation is scoped to the exact bill item and also runs inside the
    bill-item mapping transaction, closing the handler/worker race without
    changing unrelated items in the same bill. Historical Lazada reservation
    `b47ad062-424c-467c-aaff-e9672c57bb9e` was repaired from immutable bill and
    SML attempt evidence; its existing recalculation job verified the exact
    `BF-INV26080064` / `AH-0009` stock movement before incorporation. Current
    reservation evidence is 13/13 verified with no blocked mapping.
31. The same AOY rollout explains TikTok's unitemized buyer charge without
    manufacturing an SML sale line. For the controlled bill, TikTok Order
    Amount `256.42` equals itemized product revenue `250.00` plus buyer-paid
    protection/insurance `6.42` that is absent from the Excel detail. The bill
    UI and Thai audit history therefore show Order Amount as reconciliation
    evidence while the SML document authority remains the final bill items,
    totaling `250.00`. `vat_type` only splits that SML line amount into taxable
    base and VAT; it does not add the platform-collected `6.42`. AOY dry-run
    `d7357a2c-f224-4187-a02f-98bddf8cb40a` then checked all 44 Shopee listings:
    12 would change, 32 were unchanged, and 0 were blocked or errored. Automatic
    stock sync for shop `264993963` was restored to its prior five-minute
    setting after the successful dry-run; no manual sync endpoint was called.
    Backups are `pre-deploy-20260827-034901.sql.gz` and
    `pre-deploy-20260827-040005.sql.gz`. Go tests, frontend lint/build,
    sales-only guard, health, Gateway connectivity, bill/log browser QA, and
    exact production reconciliation passed. Demo and Lanboon were not deployed.

Known deferred or incomplete validation:

- Lazada Open API is pending approval; current Lazada flow is Excel import.
- Marketplace settlement / accounts-receivable posting is deferred. Do not infer
  payment posting from the sales invoice flow.
- Cross-environment duplicate prevention was intentionally deferred after the
  user removed 13 restored/legacy duplicate SML documents.
- Shopee set-stock needs a future tenant/shop with a real set product before it
  can be declared production-validated end to end.
- AOY automatic Shopee cancellation has passed its first real
  Shopee CANCELLED -> SML CN -> stock recalculation -> LINE end-to-end UAT on
  order `260827ECCFMCSC`; broader route coverage for TRANS_FLAG 45 still needs a
  future controlled case if AOY changes destinations.
- Demo and Lanboon currently run the older `82baa4a` application revision and
  may also have paid features or Shopee runtime flags disabled. Verify both the
  deployed revision and per-instance settings instead of assuming AOY behavior.

Resume checklist:

1. Run `git status --short` and `git log -1 --oneline`.
2. Check `/health` for all three instances and the Shopee Gateway.
3. Ask for or inspect the newest AOY user feedback and the exact affected order,
   bill, SKU, shop, or SML document number.
4. Preserve tenant isolation and deploy the same committed code with
   `scripts/deploy_nextstep_instances.py`; for a split AOY-only baseline always
   pass `--ref <verified-commit>` because the script defaults to `origin/main`.
   Change runtime flags only for the explicitly requested tenant.

---

## 1. Tech Stack

```
Backend:   Go 1.24 (Gin)  module: nexflow
Frontend:  React + Vite + TypeScript
Database:  PostgreSQL 16
AI:        Disabled — production is deterministic sales-only
Deploy:    Docker Compose + Cloudflare proxied domains
```

Production ports:

| instance | frontend | backend | postgres |
| --- | --- | --- | --- |
| demo | edge **6323**, debug **127.0.0.1:16323** | **8110** | **5440** |
| aoy | edge **6323**, debug **127.0.0.1:16324** | **8111** | **5441** |
| lanboon | edge **6323**, debug **127.0.0.1:16325** | **8112** | **5442** |

---

## 2. Key Database Tables

```sql
bills               -- source, bill_type, status, sml_doc_no
bill_items          -- item_code, qty, unit_code, price, discount_amount
mappings            -- verified exact raw_name → item_code fallback when SKU is absent
marketplace_item_aliases -- scoped Marketplace Product Master shared by imports, bills, and Shopee stock
channel_defaults    -- per-(channel, bill_type): cust_code, endpoint, doc_format, WH/VAT overrides
imap_accounts       -- retained schema; runtime disabled in sales-only mode
app_settings        -- instance config UI (replaces most env vars)
sml_catalog         -- active SML products for deterministic lookup
sml_catalog_set_components -- normalized SML item_type=3 component definitions and diagnostics
sml_bulk_jobs       -- async bulk SML send jobs
shopee_api_connections   -- Shopee OAuth multi-shop
doc_counters        -- atomic doc_no per prefix/period
processed_email_keys -- retained historical email dedup data
audit_logs          -- all admin actions
shopee_order_snapshots         -- Shopee Realtime order state/timeline source
shopee_order_payment_snapshots -- cached Shopee escrow/payment breakdowns
shopee_sml_cancellations       -- Shopee cancelled-after-SML CN tracking
line_notification_deliveries   -- LINE notification outbox with Flex payload fallback
shopee_stock_settings          -- per-shop scope, percentage, interval, pause/dry-run state
shopee_stock_products          -- local Shopee item/model stock catalog
shopee_stock_mappings          -- Shopee model -> SML item/unit conversion
shopee_stock_runs/attempts     -- dry-run/sync history and changed/error/unknown writes
```

Migrations: **001–089** (all idempotent/re-runnable). Full schema in `docs/current-state.md`.

---

## 3. SML Sales Routing (bills.go)

3-way sales dispatch on `source` + `bill_type` + `channel_defaults.endpoint`:

| source | bill_type | default route | client |
| --- | --- | --- | --- |
| legacy line / email / lazada | sale | sale_reserve | SML #1 JSON-RPC :3248 |
| shopee / tiktok | sale | saleorder REST v3 | SML #2 :8080 |
| explicit endpoint | sale | saleinvoice v4 | SML #1 REST :8086 |

Purchase routing and ingestion are disabled. Historical purchase code/schema is
retained for a future redesign, but must not be started or exposed at runtime.

SML #1: `provider=BRSMLST, db=smlst2016` | SML #2: `provider=SMLGOH, db=SML1_2026`

---

## 4. Key Services (navigate code)

```
MapperService      exact user-verified raw-name fallback when marketplace SKU is absent
AnomalyService     F2 rules: price_zero/qty_zero/duplicate_bill=block; price_too_high/new_customer=warn
LineRegistry       oa_id → LINE service (multi-OA)
PartyCache         in-memory SML customers only in sales-only production
Catalog            database-only exact/prefix/contains product search
events/broker      in-process SSE pubsub (sync.RWMutex + buffered ch 16)
media/signer       HMAC-SHA256, /public/media/:id?t=, 1h TTL
WorkerPool         semaphore: 5 webhook tasks, 3 SML tasks
ShopeeOpenAPI      OAuth2 multi-shop + settlement reconciliation
```

---

## 5. Critical Gotchas

1. **SML mojibake** — `marshalASCII()` escapes non-ASCII as `\uXXXX` in ALL 6 SML POST clients. SML Java reads body as Latin-1 always — `Content-Type charset` is ignored. File: `backend/internal/services/sml/json_ascii.go`. Storage (sml_payload, audit_logs) uses plain `json.Marshal`.

2. **doc_no SML bug** — pattern `prefix-YYYY` or `prefix-YY` silently drops docs in SML UI (never appears). Use `YYMM####` counter with no hyphen before year: `BF-SO260400001` ✅ vs `BF-SO-2026...` ❌. `doc_no` reuse on retry: bills.go saves to DB before SML call; retry reuses existing doc_no.

3. **channel_defaults empty** — sales retry routes fail with "ยังไม่ได้ตั้งค่า". Run Quick Setup at `/settings/channels`. `applyChannelOverrides()` overlays wh_code/shelf_code/vat_type/vat_rate per channel.

4. **Sales-only capability guard** — AI, embedding, OCR, IMAP, LINE chat, and purchase runtime are disabled. Run `bash scripts/check_sales_only_runtime.sh` before deploy; compatibility APIs return `410 Gone`.

5. **LINE Push quota** — Free OA = 200 push/month. Reply API is free. `last_reply_token` cached from webhook → admin reply tries Reply first, falls back to Push only on token error. `ConsumeReplyToken` uses CTE + `SELECT FOR UPDATE` to prevent race.

6. **SSE auth** — EventSource cannot send custom headers. Flow: `POST /api/admin/events/token` (JWT-auth) → HMAC token → `GET /api/admin/events?u=<userId>&t=<token>`.

7. **SML 248 product lookup** — `{"data":null}` = SKU not found (not an error). Always set `SHOPEE_SML_UNIT_CODE` fallback — SML rejects `unit_code=""`.

8. **Party quick-create DROPPED** — SML API requires ~25 fields / returns NPE. Create in SML UI, click "รีเฟรช" in Nexflow.

9. **`app_settings` vs `.env`** — `/settings/instance` แก้ได้เฉพาะชื่อร้านและช่องทางติดต่อ. ค่า SML tenant/URL, public URL, Shopee gateway และ infrastructure อื่นจัดการผ่าน deployment runbook; ค่าเดิมใน `app_settings`/`.env` ยังเป็น runtime source และห้าม serialize ไปหน้า instance.

10. **sml-api-bybos** — current production gateway is `nexflow-sml-api-bybos` on `10.121.20.83:8200` with `ALLOWED_TENANTS=demo,aoy,lbk63`. Nexflow instances call `http://172.17.0.1:8200` and select tenant through `app_settings.sml.database` (`demo`, `aoy`, or `lbk63`). The NextStep SQL uses `FROM ic_trans ic_qt`; `ic_qt` is an alias, not a physical table. Do not use the old `192.168.2.109` / ngrok deploy path for production.

11. **Webhook URL per OA** — `/webhook/line/<oa_id>`. Must be set in LINE Developer Console per OA.

12. **Shopee LINE notifications** — `/settings/line-notifications` is active even though LINE chat UI is disabled. It sends rich Flex from `line_notification_deliveries.flex_payload`, falls back to `message_text`, uses Asia/Bangkok time, and must not include buyer name, phone, address, or username.

13. **Shopee payment breakdown** — `shopee_order_payment_snapshots` is populated by worker/manual refresh from `get_escrow_detail`. Page render and LINE worker must read cached snapshots only, never call Shopee live APIs inline.

14. **Central Shopee gateway** — production target is `nexflow-shopee-gateway` at `shopee-gateway.nextstep-soft.com`. Gateway mode stores Partner Key and encrypted access/refresh tokens only in the gateway DB. Every tenant receives a derived HMAC identity even while in direct mode so the gateway can discover active shop routes and deliver the one app-wide push callback during staged rollout; legacy tokens are never copied. Tenant `.env` uses `SHOPEE_OPEN_API_MODE=gateway` only after explicit cutover, while direct mode remains rollback. Push is authenticated/deduped centrally and tenant reconciliation still fetches order detail as source of truth. See `docs/shopee-gateway-runbook.md`.

15. **Shopee direct mode is rollback only** — do not create a new Shopee Open Platform App for each customer. The old per-customer cutover helper and direct Partner credentials are retained only for an explicit rollback while gateway rollout is incomplete.

---

## 6. Deploy

```bash
# deploy the same committed code to all production instances
NX_PASS='<server-password>' python scripts/deploy_nextstep_instances.py --target all

# deploy one instance only when intentionally isolated
NX_PASS='<server-password>' python scripts/deploy_nextstep_instances.py --target demo
NX_PASS='<server-password>' python scripts/deploy_nextstep_instances.py --target aoy
NX_PASS='<server-password>' python scripts/deploy_nextstep_instances.py --target lanboon
NX_PASS='<server-password>' python scripts/deploy_nextstep_instances.py --target gateway
```

Adding the next customer instance uses the registry helper first:

```bash
python3 scripts/nextstep_instance_registry.py suggest
python3 scripts/nextstep_instance_registry.py add \
  --name <shop-key> \
  --hostname nextflow-<shop-key>.nextstep-soft.com \
  --sml-tenant <sml-db-or-tenant> \
  --sml-host <customer-pg-host> \
  --sml-port <customer-pg-port>
```

Commit/push the registry change, then bootstrap the server runtime folder and
SML API env with secrets stored only on the server. Never commit customer PG
passwords.

---

## 6b. Graphify Auto-Lite

Use Graphify as a context map for cross-subsystem work, not as source of truth.

Use Graphify before broad raw searches when work spans Shopee Open API, settlement, logistics, SML routing, email, LINE, backend/frontend behavior, and deployment docs.

Skip Graphify for small single-file edits, exact symbol lookups, logs, or test failure triage where `rg` and source reads are faster.

Commands:

```bash
bash scripts/graphify-update.sh
bash scripts/graphify-query.sh "Shopee order sync"
bash scripts/graphify-preflight.sh
```

Rules:

- Always open source files before editing.
- If Graphify disagrees with code or docs, code/docs win.
- `graphify-out/` is local-only and must remain untracked.
- Update Graphify manually after flow or architecture changes.
- Do not install Graphify hooks until the manual workflow has proven stable.

---

## 7. API Routes (key)

```
POST /api/auth/login
GET  /api/bills                   -- cursor: status, source, bill_type, date, archived
GET  /api/bills/:id               -- includes route preview
POST /api/bills/:id/retry         -- sales SML dispatch; purchase returns 410
POST /api/bills/bulk-send-jobs    -- async bulk (cap 100)
PUT  /api/bills/:id/items/:iid    -- edit + F1 auto-learn
POST /api/bills/:id/archive | DEL /api/bills/:id

GET/POST/PUT/DEL /api/mappings
GET  /api/mappings/stats

GET  /api/catalog | /api/catalog/search?q=
POST /api/catalog/sync
POST /api/catalog/embed-all       -- compatibility only, returns 410 Gone

ANY  /api/settings/imap-accounts* -- compatibility only, returns 410 Gone
GET  /api/settings/channel-defaults | PUT ...
GET  /api/settings/instance | PUT ...
GET  /api/settings/line-oa  | POST ...
GET/POST/PUT/DEL /api/settings/line-notifications

POST /api/import/shopee-api/orders/preview | .../confirm
POST /api/import/shopee/preview | /confirm
POST /api/import/lazada/preview | /confirm
POST /api/import/tiktok/preview | /confirm

GET  /api/sml/customers | /suppliers | POST /api/sml/refresh-parties
GET  /api/dashboard/stats | /api/logs | /api/bills/:id/timeline
GET  /api/shopee-operations/:shop_id/:order_sn/timeline
POST /api/shopee-operations/:shop_id/:order_sn/payment-breakdown/refresh
GET/POST /api/shopee-operations/:shop_id/:order_sn/cancel-sml-document(/preview)

ANY  /api/admin/conversations*    -- compatibility only, returns 410 Gone
POST /api/admin/events/token | GET /api/admin/events  -- SSE

POST /webhook/line/:oaId
GET  /public/media/:id?t=   -- HMAC-signed, no JWT
GET  /health
```

---

Last updated: 2026-08-27 | Ports: edge 6323, backends 8110/8111/8112, postgres 5440/5441/5442
