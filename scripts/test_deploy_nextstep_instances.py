import unittest
from dataclasses import replace
from unittest.mock import patch

from scripts import deploy_nextstep_instances as deploy


class DeployNextstepInstancesTest(unittest.TestCase):
    def test_ploy_registry_uses_production_sml_database(self) -> None:
        target = deploy.TARGETS["ploy"]

        self.assertEqual(target.sml_tenant, "ploy_test")

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
        self.assertIn(
            "VITE_ENABLE_TIKTOK_SHOP_API: ${VITE_ENABLE_TIKTOK_SHOP_API:-false}",
            rendered,
        )

    def test_instance_override_adds_tiktok_network_only_for_enabled_tenant(self) -> None:
        disabled = deploy.render_instance_override(self.make_target())
        enabled = deploy.render_instance_override(
            self.make_target(), include_tiktok_gateway=True
        )

        self.assertNotIn("tiktok_gateway", disabled)
        self.assertIn("      - tiktok_gateway", enabled)
        self.assertIn(f"    name: {deploy.TIKTOK_GATEWAY_NETWORK}", enabled)

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

    def test_gateway_registration_restarts_only_when_committed_tenant_is_missing(self) -> None:
        target = self.make_target()
        sql = deploy.gateway_registration_sql(target)

        self.assertIn("slug='aoy'", sql)
        self.assertIn("public_base_url='https://nexflow-aoy.example.com'", sql)
        self.assertIn("backend_url='http://172.17.0.1:8111'", sql)

        with patch.object(deploy, "sudo") as sudo:
            deploy.ensure_target_gateway_registration(target)

        script = sudo.call_args.args[0]
        self.assertIn("docker restart nexflow-shopee-gateway", script)
        self.assertIn("$(check_registration)", script)
        self.assertEqual(sudo.call_args.kwargs["label"], "verify aoy Shopee gateway registration")

    def test_gateway_registration_sql_escapes_registry_url(self) -> None:
        target = replace(self.make_target(), public_url="https://nexflow.example.com/o'hare")

        sql = deploy.gateway_registration_sql(target)

        self.assertIn("public_base_url='https://nexflow.example.com/o''hare'", sql)

    def test_gateway_registration_prefers_private_docker_network_url(self) -> None:
        target = replace(
            self.make_target(),
            gateway_backend_url="http://nexflow-aoy-backend:8090",
        )

        sql = deploy.gateway_registration_sql(target)

        self.assertIn("backend_url='http://nexflow-aoy-backend:8090'", sql)

    def test_gateway_deploy_recreates_service_to_reload_mounted_registry(self) -> None:
        with (
            patch.object(deploy, "ensure_gateway_runtime"),
            patch.object(deploy, "backup_gateway"),
            patch.object(deploy, "sudo", side_effect=["", '{"status":"ok"}']) as sudo,
        ):
            deploy.deploy_gateway()

        self.assertIn(
            "docker compose up -d --build --force-recreate gateway",
            sudo.call_args_list[0].args[0],
        )

    def test_tiktok_gateway_deploy_is_explicit_and_health_checked(self) -> None:
        with (
            patch.object(deploy, "ensure_tiktok_gateway_runtime"),
            patch.object(deploy, "backup_tiktok_gateway"),
            patch.object(deploy, "sudo", side_effect=["", '{"status":"ok"}']) as sudo,
        ):
            deploy.deploy_tiktok_gateway()

        self.assertIn(
            "docker compose up -d --build --force-recreate gateway",
            sudo.call_args_list[0].args[0],
        )
        self.assertIn(deploy.TIKTOK_GATEWAY_NETWORK, sudo.call_args_list[1].args[0])
        self.assertIn("nexflow-tiktok-shop-gateway:8092/health", sudo.call_args_list[1].args[0])

    def test_edge_exposes_only_public_tiktok_callbacks_when_enabled(self) -> None:
        default_nginx = deploy.render_edge_nginx({"aoy": self.make_target()})
        enabled_nginx = deploy.render_edge_nginx(
            {"aoy": self.make_target()}, include_tiktok_gateway=True
        )
        enabled_compose = deploy.render_edge_compose(
            {"aoy": self.make_target()}, include_tiktok_gateway=True
        )

        self.assertNotIn(deploy.TIKTOK_GATEWAY_HOSTNAME, default_nginx)
        self.assertIn(f"server_name {deploy.TIKTOK_GATEWAY_HOSTNAME};", enabled_nginx)
        self.assertIn("location /internal/ { return 404; }", enabled_nginx)
        self.assertIn("location = /api/tiktok-shop/callback", enabled_nginx)
        self.assertIn("location /webhook/ { return 404; }", enabled_nginx)
        self.assertIn("location = /webhook/tiktok-shop", enabled_nginx)
        self.assertIn(deploy.TIKTOK_GATEWAY_NETWORK, enabled_compose)

    def test_start_edge_reloads_nginx_after_tenant_recreate(self) -> None:
        with patch.object(deploy, "sudo") as sudo:
            deploy.start_edge()

        command = sudo.call_args.args[0]
        self.assertIn("docker compose up -d", command)
        self.assertIn("docker exec nexflow-edge nginx -t", command)
        self.assertIn("docker exec nexflow-edge nginx -s reload", command)

    def test_tiktok_webhook_smoke_uses_post_and_accepts_signature_gate(self) -> None:
        responses = [
            "000",
            '{"status":"ok"}',
            "404",
            '{"status":"ok"}',
            "404",
            "401",
        ]
        with patch.object(deploy, "ssh", side_effect=responses) as ssh:
            deploy.smoke_edge([], include_tiktok_gateway=True)

        webhook_call = ssh.call_args_list[-1]
        self.assertIn("-X POST", webhook_call.args[0])
        self.assertIn("/webhook/tiktok-shop", webhook_call.args[0])

    def test_tiktok_gateway_connection_probe_runs_only_for_enabled_tenant(self) -> None:
        target = self.make_target()
        with patch.object(deploy, "sudo") as sudo:
            deploy.connect_target_to_tiktok_gateway(target)

        script = sudo.call_args.args[0]
        self.assertIn("TIKTOK_SHOP_OPEN_API_ENABLED", script)
        self.assertIn(
            "docker network connect nexflow-tiktok-shop-gateway_default nexflow-aoy-backend",
            script,
        )
        self.assertIn(
            "wget -qO- http://nexflow-tiktok-shop-gateway:8092/health",
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

        self.assertIn("BOOTSTRAP_ADMIN_EMAIL=admin@nexflow.local", rendered)
        self.assertNotIn("BOOTSTRAP_ADMIN_EMAIL=admin@aoy.nexflow.local", rendered)
        self.assertIn("PUBLIC_BASE_URL=https://nexflow-aoy.example.com", rendered)
        self.assertIn("SHOPEE_SML_DATABASE=aoy", rendered)
        self.assertIn("SHOPEE_GATEWAY_TENANT=aoy", rendered)
        self.assertIn("MARKETPLACE_CONVERSION_MODE=off", rendered)
        self.assertIn("SHOPEE_OPEN_API_ENABLED=false", rendered)
        self.assertIn("SHOPEE_AUTO_SML_ENABLED=false", rendered)
        self.assertIn("SHOPEE_AUTO_SML_CANCEL_ENABLED=false", rendered)
        self.assertIn("SHOPEE_SET_STOCK_ENABLED=false", rendered)
        self.assertIn("TIKTOK_SHOP_OPEN_API_ENABLED=false", rendered)
        self.assertIn("TIKTOK_SHOP_GATEWAY_TENANT=aoy", rendered)
        self.assertIn("VITE_ENABLE_TIKTOK_SHOP_API=false", rendered)
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
            patch.object(deploy, "connect_target_to_tiktok_gateway"),
            patch.object(deploy, "snapshot_target_sales_counts"),
            patch.object(deploy, "ssh", return_value='{"status":"ok"}') as ssh,
        ):
            sudo.return_value = "1|0|0|0|0"
            deploy.bootstrap_target_runtime(target)

        prepare_script = sudo.call_args_list[0].args[0]
        self.assertIn("test ! -e /srv/nexflow-aoy", prepare_script)
        self.assertIn("docker inspect nexflow-aoy-postgres", prepare_script)
        self.assertIn("docker volume inspect nexflow-aoy_pgdata", prepare_script)
        self.assertIn("chmod 600 /srv/nexflow-aoy/.env", prepare_script)
        self.assertIn("docker compose config", prepare_script)
        health_script = ssh.call_args.args[0]
        self.assertIn("seq 1 60", health_script)

    def test_deploy_precheck_supports_private_bootstrap_runtime_directory(self) -> None:
        target = self.make_target()

        with (
            patch.object(deploy, "sudo") as sudo,
            patch.object(deploy, "ssh", return_value='{"status":"ok"}'),
            patch.object(deploy, "ensure_instance_compose"),
            patch.object(deploy, "provision_target_gateway_identity"),
            patch.object(deploy, "ensure_target_gateway_registration") as registration,
            patch.object(deploy, "snapshot_target_sales_counts"),
            patch.object(deploy, "backup_target"),
            patch.object(deploy, "sanitize_target_disabled_env"),
            patch.object(deploy, "connect_target_to_gateway"),
            patch.object(deploy, "connect_target_to_tiktok_gateway"),
        ):
            deploy.deploy_target(target)

        precheck = sudo.call_args_list[0]
        self.assertEqual(precheck.kwargs["label"], "precheck aoy")
        self.assertIn("test -d /srv/nexflow-aoy", precheck.args[0])
        self.assertIn("test -f /srv/nexflow-aoy/.env", precheck.args[0])
        registration.assert_called_once_with(target)


if __name__ == "__main__":
    unittest.main()
