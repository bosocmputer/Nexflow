# Central Shopee Gateway Runbook

Updated: 2026-07-16

## Public Contract

Shopee Console central app:

```text
Redirect URL: https://shopee-gateway.nextstep-soft.com/api/shopee/callback
Push URL:     https://shopee-gateway.nextstep-soft.com/webhook/shopee
IP whitelist: production egress IPs reported by Shopee, including 58.136.190.202
```

Cloudflare routes `shopee-gateway.nextstep-soft.com` to the existing HTTP origin
`10.121.20.83:6323` with the original Host header. Edge Nginx exposes only
`/health`, the OAuth callback, and the push callback. `/internal/*` returns 404.

## Provision Once

Run on the production release clone. Partner Key and Push secret use hidden
prompts; generated encryption/signing/database keys are never printed.

```bash
cd /mnt/data/nextstep-node-2/nexflow-release
sudo python3 scripts/provision_shopee_gateway.py --partner-id <CENTRAL_PARTNER_ID>

NX_PASS='<server-password>' python3 scripts/deploy_nextstep_instances.py --target gateway
```

When the working shared app credentials already exist in a production tenant,
copy them without exposing secrets in shell history:

```bash
sudo python3 scripts/provision_shopee_gateway.py \
  --source-env /mnt/data/nextstep-node-2/nexflow-aoy/.env
```

The runtime folder is `/mnt/data/nextstep-node-2/nexflow-shopee-gateway`.
Back up its `.env`, PostgreSQL volume, and encrypted token database together.
Losing the token encryption key makes existing ciphertext unrecoverable.

## Add a Customer

Adding a Nexflow instance to `deploy/nextstep-instances.json` automatically adds
it to the gateway tenant registry. No new Shopee App is needed. Every deploy
also provisions a derived tenant HMAC identity without changing that tenant's
current `direct` or `gateway` mode.

After the customer instance/domain is ready:

```bash
cd /mnt/data/nextstep-node-2/nexflow-release
sudo python3 scripts/shopee_gateway_tenant_mode.py --target <tenant> --mode gateway
NX_PASS='<server-password>' python3 scripts/deploy_nextstep_instances.py --target <tenant>
```

Then the customer admin opens `/settings/shopee-connections`, clicks connect,
and authorizes that customer's Shopee shop. The gateway rejects a shop already
owned by another tenant. Switching to gateway mode moves any legacy Partner ID
and Partner Key out of the active tenant `.env` into the server-only
`.shopee-direct-rollback.env` file with mode `600`. They are restored only by
an explicit `--mode direct` rollback.

## AOY Canary Rollout

Do not change Shopee Console or AOY mode until gateway health is green.

1. Deploy gateway without changing AOY (`direct` remains active).
2. Deploy all tenant backends once. The gateway polls each signed internal
   route endpoint every minute and records active `shop_id -> tenant` routes.
3. Verify every active direct shop has exactly one active route before changing
   the global Shopee push callback:

   ```bash
   docker exec nexflow-shopee-gateway-postgres \
     psql -U nexflow_gateway -d nexflow_shopee_gateway \
     -c "SELECT t.slug, r.route_source, COUNT(*) FROM shop_routes r JOIN tenants t ON t.id=r.tenant_id WHERE r.active GROUP BY 1,2 ORDER BY 1,2;"
   ```

4. Add the Cloudflare hostname and confirm gateway `/health`; public
   `/internal/*` must return 404.
5. Update the central Shopee app redirect and push callback. Signed central push
   delivery works for both direct and gateway tenants during the migration.
6. Switch only AOY to gateway mode using the helper.
7. Reconnect AOY through OAuth. Old direct credentials remain in AOY DB.
8. Verify connection metadata, latest sync, operations list, historical preview,
   escrow/payment, tracking, shipping parameter, and one real push notification.
9. Observe gateway and AOY logs before moving demo or Lanboon.

Never switch the global Shopee push callback while an active shop is missing
from `shop_routes`; a push arriving during that gap is retained as unknown but
cannot be delivered retroactively.

## Rollback

Rollback affects only the selected tenant:

```bash
cd /mnt/data/nextstep-node-2/nexflow-release
sudo python3 scripts/shopee_gateway_tenant_mode.py --target aoy --mode direct
NX_PASS='<server-password>' python3 scripts/deploy_nextstep_instances.py --target aoy
```

This does not delete gateway data or existing direct credentials. The helper
restores the legacy Partner credentials from `.shopee-direct-rollback.env` only
for direct mode. A shop first connected through gateway mode has no usable
direct token, so it must reconnect to the old direct app after switching mode.
Existing direct shops can roll back without reconnecting only while their
retained direct refresh token is valid.

## Operations

- Gateway OAuth callback without `state` must fail; never guess a tenant.
- Push is a trigger only. Tenant reconciliation fetches order detail as source
  of truth before updating snapshots or business workflows.
- Unknown `shop_id` push events are retained as diagnostics and never routed.
- Legacy route discovery returns only active shop IDs. It never copies direct
  access tokens or refresh tokens into the gateway.
- A route conflict never reassigns a shop between tenants; fix the conflicting
  tenant connection before changing the Shopee callback.
- Token, Partner Key, Push secret, buyer PII, and raw API payloads must not be
  logged.
- Read APIs retry only bounded transient failures. `ship_order` and shipping
  document creation do not automatically retry.
- Rotate OAuth signing/internal auth keys only with a planned migration. Rotate
  the token encryption key only with token re-encryption or shop reconnect.
