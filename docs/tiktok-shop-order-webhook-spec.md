# TikTok Shop Order Webhook — AOY Shadow UAT

## Objective

Receive TikTok Shop `ORDER_STATUS_CHANGE` notifications through the Central
TikTok Shop Gateway, verify the official webhook signature, deduplicate every
notification durably, route it to the owning Nexflow tenant, and refresh the
exact order through the existing Order Detail + Price Detail snapshot flow.

This slice is read-only. It must not create Nexflow Bills, SML documents, LINE
notifications, fulfillment actions, cancellations, or stock writes.

## Source contract

- Public endpoint: `POST /webhook/tiktok-shop`.
- Read the request body once with a 1 MiB hard limit.
- TikTok sends a lowercase hexadecimal HMAC-SHA256 signature in
  `Authorization`.
- Signature input is the exact byte sequence `app_key + raw_request_body`; the
  HMAC key is `app_secret`. Compare in constant time before JSON decoding.
- The order topic is numeric `type = 1` and contains
  `tts_notification_id`, `shop_id`, `timestamp`, `data.order_id`,
  `data.order_status`, and `data.update_time`.
- A valid accepted or duplicate notification receives HTTP 200 with an empty
  body within three seconds. Missing or invalid signatures receive HTTP 401
  with an empty body. Invalid authenticated payloads and unavailable durable
  storage receive a non-200 response so TikTok can retry.
- Delivery is at-least-once. `tts_notification_id` is the durable idempotency
  key. An unknown shop is retained as safe diagnostic metadata, acknowledged,
  and never routed to another tenant.

Official references:

- <https://partner.tiktokshop.com/docv2/page/configuration-guide>
- <https://partner.tiktokshop.com/docv2/page/tts-webhooks-overview>
- <https://partner.tiktokshop.com/docv2/page/1-order-status-change>

## Architecture and trust boundaries

1. TikTok sends an untrusted public request to the Central Gateway.
2. The Central Gateway authenticates the exact raw bytes with the App Secret.
3. One Gateway transaction stores safe event metadata and, for a known active
   shop, creates a tenant delivery outbox row.
4. A Gateway worker sends a minimal payload to the owning tenant over the
   private Docker network, authenticated with the existing tenant-derived HMAC.
5. The tenant authenticates and replay-protects the Gateway request, verifies
   the shop belongs to that tenant, then inserts one durable webhook job.
6. A tenant worker refreshes only `shop_id + order_id` using the existing typed
   Order Detail and Price Detail clients. The webhook payload is never treated
   as order or amount truth.
7. Scheduled polling remains enabled as reconciliation backup.

Neither boundary logs or stores the raw Authorization header, App Secret,
tokens, buyer PII, recipient data, or the raw webhook body. The Central Gateway
stores only a SHA-256 body fingerprint plus typed business identifiers.

## Feature gates and rollout

- Add `TIKTOK_SHOP_WEBHOOK_ENABLED`, default `false`, to the Central Gateway
  and tenant application.
- The public route exists only when the Central Gateway gate is enabled.
- The tenant internal receiver and worker fail closed unless its own gate,
  TikTok Open API, Gateway identity, and active local shop connection are all
  present.
- First enable only the AOY Central Gateway and AOY tenant. Demo, Lanboon, and
  Ploy remain disabled.
- Configure only `ORDER_STATUS_CHANGE` after code, migrations, edge routing,
  and synthetic signed canaries pass. For a Custom App, use the shop-specific
  `PUT /event/202309/webhooks` operation with the owning seller token and
  `shop_cipher`; the Central Gateway supplies its trusted callback URL.

## Required tests

- Official stable signature vector succeeds; body mutation, missing signature,
  uppercase/non-hex signature, and bearer prefix fail.
- Oversized public body is rejected before parsing or persistence.
- Signed type-1 payload is parsed strictly and stored once.
- Replaying the same `tts_notification_id` returns success and creates neither
  a second event nor a second outbox/job.
- Unknown `shop_id` is acknowledged, retained, and never routed.
- Gateway database failure returns non-200; authentication failure returns 401
  with an empty body.
- Gateway-to-tenant delivery uses tenant-derived HMAC and retries durably.
- Tenant rejects replayed HMAC requests and shop/tenant mismatches.
- Tenant worker refreshes exactly one order, marks success idempotently, and
  retries a bounded safe error without creating marketplace side effects.
- Existing TikTok snapshot, polling reconciliation, OAuth, and Gateway tests
  remain green; Go race tests and vet pass for changed packages.

## AOY UAT success criteria

- Public invalid-signature canary returns 401 with an empty body.
- Synthetic valid-signature canary is accepted once and duplicate replay does
  not add rows.
- A real AOY order status change appears once in Central Gateway receipt,
  delivery outbox, AOY webhook job, and final AOY snapshot.
- The snapshot has fresh TikTok request IDs and contains no buyer PII.
- Bill, SML attempt, notification, LINE delivery, fulfillment, cancellation,
  and stock-write counts remain unchanged.
- Webhook logs contain stable event names, request ID, notification ID,
  `shop_id`, `order_id`, tenant, result, attempt count, and safe error code;
  no raw body, signature, token, or buyer data appears.
- Scheduled polling remains operational as a recovery path.

## Rollback

Disable `TIKTOK_SHOP_WEBHOOK_ENABLED` at AOY and the Central Gateway, remove the
Partner Center `ORDER_STATUS_CHANGE` subscription or restore its prior callback,
and redeploy. Keep additive tables and receipt/job rows as audit evidence.
