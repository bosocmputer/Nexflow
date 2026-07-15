# ADR 001: Central Shopee Gateway for Multi-Tenant Nexflow

- Status: Accepted
- Date: 2026-07-15

## Context

Nexflow runs one isolated application/database instance per customer. A Shopee
app per customer does not scale operationally to 100+ shops and repeats IP,
redirect, push, secret, token refresh, and approval work. Path-based tenancy
would also require risky changes to React Router, API paths, SSE, callbacks, and
webhooks.

## Decision

Use one central `nexflow-shopee-gateway` and one approved Shopee Open Platform
app. Customer Nexflow instances keep their hostnames and isolated databases.

The gateway owns Partner credentials and encrypted Shopee tokens. Tenants call
an allowlisted typed API over HMAC-authenticated internal endpoints. OAuth state
is signed, short-lived, one-time, and bound to a registered tenant/user/return
origin. Shopee push is authenticated and deduplicated at the gateway, persisted
before ACK, then delivered through an outbox to the tenant selected by `shop_id`.

Tenant databases store connection metadata only in gateway mode. The existing
direct mode and credentials remain available as a per-instance rollback path.
During staged migration, the gateway discovers active direct-mode shop IDs over
the same signed internal channel and stores routing separately from credentials.
This lets the one global Shopee push callback serve both migrated and unmigrated
tenants without copying legacy tokens. Credential writes remain gateway-only.

## Consequences

- New customers connect a shop without creating another Shopee app.
- Shopee Console uses one redirect and one push callback.
- A gateway outage affects Shopee API operations across tenants, so the gateway
  has a separate database, health check, bounded retries, per-tenant limits,
  outbox delivery, encrypted tokens, and explicit rollback.
- A `shop_id` can belong to only one tenant at a time. Cross-tenant OAuth cannot
  silently move ownership.
- Push delivery is accepted by signed tenant endpoints in both modes so canary
  rollout and direct-mode rollback do not interrupt the app-wide callback.
- Shipping mutations are never automatically retried by the gateway.
- SML, bill creation, LINE, and tenant business workflows remain unchanged.
