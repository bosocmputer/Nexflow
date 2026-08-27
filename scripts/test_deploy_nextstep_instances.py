import unittest
from unittest.mock import patch

from scripts import deploy_nextstep_instances as deploy


class DeployNextstepInstancesTest(unittest.TestCase):
    def make_target(self, *, extra_hosts: tuple[str, ...] = ()) -> deploy.Target:
        return deploy.Target(
            name="aoy",
            remote="/srv/nexflow-aoy",
            hostname="nexflow-aoy.example.com",
            frontend_debug_port=16324,
            previous_frontend_port=3031,
            backend_port=8111,
            postgres_port=5441,
            postgres_container="nexflow-aoy-postgres",
            backend_container="nexflow-aoy-backend",
            frontend_container="nexflow-aoy-frontend",
            public_url="https://nexflow-aoy.example.com",
            folder="nexflow-aoy",
            sml_tenant="aoy",
            backend_extra_hosts=extra_hosts,
        )

    def test_instance_override_keeps_gateway_network_across_recreates(self) -> None:
        rendered = deploy.render_instance_override(self.make_target())

        self.assertIn("      - default", rendered)
        self.assertIn("      - shopee_gateway", rendered)
        self.assertIn(f"    name: {deploy.GATEWAY_NETWORK}", rendered)
        self.assertIn("    external: true", rendered)

    def test_instance_override_preserves_backend_extra_hosts(self) -> None:
        rendered = deploy.render_instance_override(
            self.make_target(extra_hosts=("legacy-sml.example.com:192.0.2.10",))
        )

        self.assertIn('      - "legacy-sml.example.com:192.0.2.10"', rendered)
        self.assertIn("      - shopee_gateway", rendered)

    def test_gateway_connection_check_probes_health_from_backend(self) -> None:
        target = self.make_target()

        with patch.object(deploy, "sudo") as sudo:
            deploy.connect_target_to_gateway(target)

        script = sudo.call_args.args[0]
        self.assertIn(
            "docker network connect nexflow-shopee-gateway_default nexflow-aoy-backend",
            script,
        )
        self.assertIn(
            "wget -qO- http://nexflow-shopee-gateway:8091/health",
            script,
        )

    def test_fresh_runtime_compose_is_isolated_and_local_only(self) -> None:
        target = self.make_target()

        rendered = deploy.render_fresh_instance_compose(target)

        self.assertIn("container_name: nexflow-aoy-postgres", rendered)
        self.assertIn('127.0.0.1:5441:5432', rendered)
        self.assertIn('127.0.0.1:8111:8090', rendered)
        self.assertIn('127.0.0.1:16324:80', rendered)
        self.assertIn("name: nexflow-aoy_pgdata", rendered)
        self.assertNotIn("name: nexflow_pgdata\n", rendered)

    def test_fresh_runtime_env_uses_target_tenant_and_fail_closed_features(self) -> None:
        target = self.make_target()
        secrets = deploy.BootstrapSecrets(
            db_password="fresh-db-secret",
            jwt_secret="fresh-jwt-secret",
            media_signing_key="fresh-media-secret",
            admin_password="fresh-admin-secret",
        )

        rendered = deploy.render_fresh_instance_env(target, secrets)

        self.assertIn("PUBLIC_BASE_URL=https://nexflow-aoy.example.com", rendered)
        self.assertIn("SHOPEE_SML_DATABASE=aoy", rendered)
        self.assertIn("SHOPEE_GATEWAY_TENANT=aoy", rendered)
        self.assertIn("MARKETPLACE_CONVERSION_MODE=off", rendered)
        self.assertIn("SHOPEE_OPEN_API_ENABLED=false", rendered)
        self.assertIn("SHOPEE_AUTO_SML_ENABLED=false", rendered)
        self.assertIn("SHOPEE_AUTO_SML_CANCEL_ENABLED=false", rendered)
        self.assertIn("SHOPEE_SET_STOCK_ENABLED=false", rendered)
        self.assertIn("SML_SET_PRODUCT_EXPANSION_ENABLED=false", rendered)
        self.assertNotIn("aoy-password", rendered)

    def test_bootstrap_secrets_are_unique_and_long(self) -> None:
        first = deploy.generate_bootstrap_secrets()
        second = deploy.generate_bootstrap_secrets()

        self.assertNotEqual(first, second)
        for value in (
            first.db_password,
            first.jwt_secret,
            first.media_signing_key,
            first.admin_password,
        ):
            self.assertGreaterEqual(len(value), 32)

    def test_bootstrap_runtime_preflight_refuses_existing_state_and_keeps_env_private(self) -> None:
        target = self.make_target()

        with (
            patch.object(deploy, "sudo") as sudo,
            patch.object(deploy, "provision_target_gateway_identity"),
            patch.object(deploy, "connect_target_to_gateway"),
            patch.object(deploy, "snapshot_target_sales_counts"),
            patch.object(deploy, "ssh", return_value='{"status":"ok"}'),
        ):
            sudo.return_value = "1|0|0|0|0"
            deploy.bootstrap_target_runtime(target)

        prepare_script = sudo.call_args_list[0].args[0]
        self.assertIn("test ! -e /srv/nexflow-aoy", prepare_script)
        self.assertIn("docker inspect nexflow-aoy-postgres", prepare_script)
        self.assertIn("docker volume inspect nexflow-aoy_pgdata", prepare_script)
        self.assertIn("chmod 600 /srv/nexflow-aoy/.env", prepare_script)
        self.assertIn("docker compose config", prepare_script)


if __name__ == "__main__":
    unittest.main()
