# Spec: TikTok Shop read-only order snapshots

## Objective

Add the first durable tenant-side TikTok Shop order state without creating a
Nexflow bill, SML document, webhook subscription, polling schedule, or stock
write. An authenticated Admin or Staff user can request a bounded snapshot for
one AOY TikTok shop and up to 20 explicit order IDs.

The snapshot is the safe, idempotent input for the later Bill/SML shadow phase.
It must preserve TikTok order/variant identity, repeated-line quantity evidence,
status, and buyer-payment reconciliation amounts while excluding buyer and
recipient PII.

## Tech Stack

- Go 1.24, Gin, PostgreSQL 16
- Existing tenant-to-Central-Gateway HMAC client
- Existing TikTok Shop Order Detail `202507` and Price Detail `202407` clients

## Commands

- Focused tests: `cd backend && go test ./internal/services/tiktokshop ./internal/handlers ./internal/database`
- Full tests: `cd backend && go test ./...`
- Race tests: `cd backend && go test -race ./internal/services/tiktokshop ./internal/handlers`
- Static checks: `cd backend && go vet ./...`
- Sales-only guard: `bash scripts/check_sales_only_runtime.sh`

## Project Structure

- `backend/internal/database/migrations/` — additive tenant schema
- `backend/internal/services/tiktokshop/` — normalization and transactional store
- `backend/internal/handlers/` — authenticated bounded manual endpoint
- `backend/cmd/server/` — dependency wiring and route registration
- `docs/` — rollout evidence and operational rules

## Code Style

Follow existing explicit Go validation and context-aware database calls:

```go
if strings.TrimSpace(input.ShopID) == "" || len(input.OrderIDs) == 0 {
	return nil, ErrInvalidSnapshotInput
}
```

Use typed allowlisted payloads, parameterized SQL, stable structured log event
names, and table-driven tests. Never marshal or store an upstream untyped body.

## Testing Strategy

- Unit tests prove repeated line items group by `(product_id, sku_id)` and that
  unknown buyer/recipient fields cannot enter persisted JSON.
- Service tests prove Detail and Price Detail must agree on order total/currency
  before any database write.
- Store tests prove a bounded batch uses one transaction and an upsert on
  `(shop_id, order_id)`.
- Handler tests prove authentication-gated input limits and safe responses.
- Migration tests reject credentials, PII columns, and destructive SQL.
- AOY UAT snapshots one controlled order twice and proves one database row, no
  bill/SML side effects, and no buyer PII in the saved JSON.

## Boundaries

- Always: require an active tenant-local connection; fetch fresh Order Detail
  and Price Detail; validate identity, amounts, currency, and line evidence;
  keep source request IDs and a deterministic content hash.
- Ask first: enabling polling/webhooks, creating Bills or SML documents,
  persisting buyer/recipient PII, or adding TikTok mutations.
- Never: expose Gateway credentials/tokens, log raw payloads, manufacture amount
  reconciliation, or make partial database updates for a failed batch.

## Success Criteria

1. `POST /api/tiktok-shop-api/orders/snapshot` accepts one shop and 1–20 unique
   order IDs for Admin/Staff when the tenant feature is enabled.
2. Each successful order has one tenant row keyed by `(shop_id, order_id)`;
   replay updates that row and does not duplicate it.
3. Quantity equals the count of current `202507` line-item instances grouped by
   `(product_id, sku_id)`; seller SKU remains optional.
4. The stored order/price JSON is generated only from typed allowlisted structs
   and contains no buyer name, username, phone, email, address, message, or tax
   identifier.
5. Detail and Price Detail currency/payment mismatch fails closed before the
   transaction begins.
6. Successful logs contain only safe shop/count/request evidence. No Bill, SML,
   webhook, notification, catalog, or stock table is touched.

## Open Questions

None for this read-only slice. Status-to-Bill eligibility, scheduled windows,
webhook topics, retention, and automatic reconciliation belong to later phases.
