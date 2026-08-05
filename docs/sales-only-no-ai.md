# Nexflow Sales-Only / No-AI Runtime

Nexflow production runs as a deterministic sales operations system. AI, OCR,
audio transcription, embeddings, Daily Insight, email ingestion, LINE chat,
and purchase workflows are disabled.

## Product Resolution Order

1. Exact active SML `item_code` from marketplace SKU.
2. Active user-confirmed alias for `platform + source_sku`.
3. Exact verified raw-name mapping only when the source has no SKU.
4. Otherwise mark the item `needs_review` for manual selection.

Shopee uses `model_sku` before `item_sku`. Normalization trims BOM and
whitespace only. It does not change case or guess a code. An exact SKU always
wins over an older alias.

If a SKU is missing from the local catalog, preview performs a bounded exact
read-through from SML. The batch limit is 50 unique codes, concurrency is 3,
and each lookup has a 3-second timeout. A slow SML lookup marks only that item
for review; it does not fail the whole preview.

## Disabled Runtime Surface

- Server startup does not create AI, OCR, insight, embedding, IMAP, or purchase workers.
- Compatibility endpoints return `410 Gone` for stale frontend caches.
- Purchase routes are guarded server-side and old UI URLs redirect to the dashboard.
- `PURCHASE_FLOW_ENABLED` is not configurable; the compiled server forces it to `false`.
- Historical `ai_usage_logs` remain read-only for audit.
- Catalog embeddings and Daily Insight data are cleared by migration 073.

Run the release guard before every production deployment:

```bash
bash scripts/check_sales_only_runtime.sh
```

The deploy script runs this guard automatically. It builds the server and
fails if the resulting binary links disabled AI packages or contains an AI
provider endpoint.

## Rollout Checks

1. Back up `.env` and PostgreSQL for the target instance.
2. Record catalog, alias, verified mapping, open bill, and AI usage counts.
3. Deploy one instance at a time: demo, AOY, then Lanboon.
4. Confirm startup logs contain `ai_enabled=false` and `purchase_flow_enabled=false`.
5. Verify marketplace preview, manual product selection, mapping confirmation,
   sales send to SML, Shopee Gateway, NextStep Marketplace, and LINE Push.
6. Confirm `ai_usage_logs` does not increase after deployment.
7. Remove legacy AI secrets from each runtime `.env`, restart, and revoke the
   provider keys after every instance passes smoke tests.

Rollback uses the pre-deploy database dump, `.env` backup, and previous Git
commit. Never restore only the database when application code is still on the
new migration contract.
