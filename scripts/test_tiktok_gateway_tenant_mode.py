import base64
import hashlib
import hmac
import unittest

from scripts import tiktok_gateway_tenant_mode as mode


class TikTokGatewayTenantModeTest(unittest.TestCase):
    def test_derive_matches_gateway_tenant_domain(self) -> None:
        raw = bytes(range(32))
        encoded = base64.b64encode(raw).decode()
        expected = hmac.new(
            raw,
            b"nexflow-tiktok-shop-gateway/tenant/aoy",
            hashlib.sha256,
        ).hexdigest()

        self.assertEqual(mode.derive(encoded, "aoy"), expected)

    def test_identity_only_preserves_disabled_feature_flags(self) -> None:
        current = {
            "TIKTOK_SHOP_OPEN_API_ENABLED": "false",
            "VITE_ENABLE_TIKTOK_SHOP_API": "false",
        }
        gateway = {
            "PUBLIC_BASE_URL": "https://tiktok-shop-gateway.nextstep-soft.com",
            "TIKTOK_SHOP_GATEWAY_INTERNAL_MASTER_KEY": base64.b64encode(bytes(range(32))).decode(),
        }

        updates = mode.prepare_updates("aoy", current, gateway, enabled=None)

        self.assertNotIn("TIKTOK_SHOP_OPEN_API_ENABLED", updates)
        self.assertNotIn("VITE_ENABLE_TIKTOK_SHOP_API", updates)
        self.assertEqual(updates["TIKTOK_SHOP_GATEWAY_TENANT"], "aoy")
        self.assertEqual(updates["TIKTOK_SHOP_GATEWAY_BASE_URL"], "http://nexflow-tiktok-shop-gateway:8092")
        self.assertNotEqual(updates["TIKTOK_SHOP_GATEWAY_INTERNAL_SECRET"], "")

    def test_explicit_enable_updates_backend_and_frontend_together(self) -> None:
        gateway = {
            "PUBLIC_BASE_URL": "https://tiktok-shop-gateway.nextstep-soft.com/",
            "TIKTOK_SHOP_GATEWAY_INTERNAL_MASTER_KEY": base64.b64encode(bytes(range(32))).decode(),
        }

        updates = mode.prepare_updates("aoy", {}, gateway, enabled=True)

        self.assertEqual(updates["TIKTOK_SHOP_OPEN_API_ENABLED"], "true")
        self.assertEqual(updates["VITE_ENABLE_TIKTOK_SHOP_API"], "true")
        self.assertEqual(
            updates["TIKTOK_SHOP_GATEWAY_PUBLIC_URL"],
            "https://tiktok-shop-gateway.nextstep-soft.com",
        )

    def test_webhook_enable_is_tenant_scoped(self) -> None:
        gateway = {
            "PUBLIC_BASE_URL": "https://tiktok-shop-gateway.nextstep-soft.com",
            "TIKTOK_SHOP_GATEWAY_INTERNAL_MASTER_KEY": base64.b64encode(bytes(range(32))).decode(),
        }

        updates = mode.prepare_updates("aoy", {}, gateway, enabled=None, webhook_enabled=True)

        self.assertEqual(updates["TIKTOK_SHOP_WEBHOOK_ENABLED"], "true")
        self.assertNotIn("TIKTOK_SHOP_OPEN_API_ENABLED", updates)
        self.assertNotIn("VITE_ENABLE_TIKTOK_SHOP_API", updates)

    def test_rejects_invalid_master_key(self) -> None:
        with self.assertRaises(SystemExit):
            mode.derive("not-base64", "aoy")


if __name__ == "__main__":
    unittest.main()
