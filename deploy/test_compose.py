"""Exercise the production Compose configuration without reading user .env files."""
import json
import os
from pathlib import Path
import subprocess
import unittest

ROOT = Path(__file__).resolve().parents[1]


class ProductionConfigTest(unittest.TestCase):
    def config(self, selected):
        env = {key: value for key, value in os.environ.items() if not key.startswith("WB2A_")}
        env.update(WB2A_CONFIG_FILE=selected, WB2A_API_KEY="mock-shared-api")
        result = subprocess.run(
            ["docker", "compose", "--env-file", str(ROOT / "deploy/acceptance.env"),
             "-f", str(ROOT / "docker-compose.yml"), "config", "--format", "json"],
            env=env, capture_output=True, text=True, check=True)
        return json.loads(result.stdout)

    def test_default_and_explicit_configs_use_readonly_production_mount(self):
        for selected in ("", str(ROOT / "deploy/acceptance-config.json")):
            with self.subTest(selected=selected):
                config = self.config(selected)
                mounts = [m for m in config["services"]["core"]["volumes"] if m["target"] == "/app/config.json"]
                self.assertEqual(len(mounts), 1, "production config selection is missing")
                mount = mounts[0]
                self.assertEqual(mount["source"], selected or str(ROOT / "deploy/default-config.json"))
                self.assertTrue(Path(mount["source"]).is_file())
                self.assertTrue(mount["read_only"])
                self.assertFalse(mount.get("bind", {}).get("create_host_path", False))
                for service in ("core", "console"):
                    self.assertEqual(config["services"][service]["environment"]["WB2A_API_KEY"], "mock-shared-api")


if __name__ == "__main__":
    unittest.main()
