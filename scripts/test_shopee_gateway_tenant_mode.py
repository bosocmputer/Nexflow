import stat
import tempfile
import unittest
from pathlib import Path

from scripts import shopee_gateway_tenant_mode as tenant_mode


class ShopeeGatewayTenantModeTest(unittest.TestCase):
    def test_gateway_mode_moves_direct_credentials_out_of_active_env(self) -> None:
        current = {
            "SHOPEE_OPEN_API_PARTNER_ID": "2034838",
            "SHOPEE_OPEN_API_PARTNER_KEY": "partner-secret",
        }
        with tempfile.TemporaryDirectory() as directory:
            rollback = Path(directory) / tenant_mode.DIRECT_ROLLBACK_ENV
            updates = tenant_mode.prepare_mode_updates("gateway", current, rollback)

            self.assertEqual(updates["SHOPEE_OPEN_API_MODE"], "gateway")
            self.assertEqual(updates["SHOPEE_OPEN_API_PARTNER_ID"], "")
            self.assertEqual(updates["SHOPEE_OPEN_API_PARTNER_KEY"], "")
            _, saved = tenant_mode.read_env(rollback)
            self.assertEqual(saved, current)
            self.assertEqual(stat.S_IMODE(rollback.stat().st_mode), 0o600)

    def test_gateway_mode_rerun_preserves_existing_rollback_credentials(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            rollback = Path(directory) / tenant_mode.DIRECT_ROLLBACK_ENV
            tenant_mode.write_env_atomic(
                rollback,
                [
                    "SHOPEE_OPEN_API_PARTNER_ID=2034838",
                    "SHOPEE_OPEN_API_PARTNER_KEY=partner-secret",
                ],
            )

            tenant_mode.prepare_mode_updates(
                "gateway",
                {"SHOPEE_OPEN_API_PARTNER_ID": "", "SHOPEE_OPEN_API_PARTNER_KEY": ""},
                rollback,
            )

            _, saved = tenant_mode.read_env(rollback)
            self.assertEqual(saved["SHOPEE_OPEN_API_PARTNER_KEY"], "partner-secret")

    def test_gateway_mode_rejects_partial_direct_credentials(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            rollback = Path(directory) / tenant_mode.DIRECT_ROLLBACK_ENV
            with self.assertRaises(SystemExit):
                tenant_mode.prepare_mode_updates(
                    "gateway",
                    {"SHOPEE_OPEN_API_PARTNER_ID": "2034838"},
                    rollback,
                )
            self.assertFalse(rollback.exists())

    def test_direct_mode_restores_rollback_credentials(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            rollback = Path(directory) / tenant_mode.DIRECT_ROLLBACK_ENV
            tenant_mode.write_env_atomic(
                rollback,
                [
                    "SHOPEE_OPEN_API_PARTNER_ID=2034838",
                    "SHOPEE_OPEN_API_PARTNER_KEY=partner-secret",
                ],
            )

            updates = tenant_mode.prepare_mode_updates("direct", {}, rollback)

            self.assertEqual(updates["SHOPEE_OPEN_API_MODE"], "direct")
            self.assertEqual(updates["SHOPEE_OPEN_API_PARTNER_ID"], "2034838")
            self.assertEqual(updates["SHOPEE_OPEN_API_PARTNER_KEY"], "partner-secret")

    def test_direct_mode_fails_without_rollback_credentials(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            rollback = Path(directory) / tenant_mode.DIRECT_ROLLBACK_ENV
            with self.assertRaises(SystemExit):
                tenant_mode.prepare_mode_updates("direct", {}, rollback)


if __name__ == "__main__":
    unittest.main()
