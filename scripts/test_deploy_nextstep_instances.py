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


if __name__ == "__main__":
    unittest.main()
