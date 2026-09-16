"""Exercise the production Compose configuration without reading user .env files."""
import json
import os
from pathlib import Path
import subprocess
import shutil
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]


class ProductionConfigTest(unittest.TestCase):
    def config(self, selected="", files=None, directory=ROOT, overrides=None):
        env = {key: value for key, value in os.environ.items() if not key.startswith("WB2A_")}
        env.pop("COMPOSE_FILE", None)
        env.pop("COMPOSE_PROJECT_NAME", None)
        env.update(WB2A_CONFIG_FILE=selected, WB2A_API_KEY="mock-shared-api")
        env.update(overrides or {})
        args = ["docker", "compose", "--env-file", str(ROOT / "deploy/acceptance.env")]
        for file in files or [directory / "docker-compose.yml"]:
            args.extend(["-f", str(file)])
        result = subprocess.run(
            [*args, "config", "--format", "json"], cwd=directory,
            env=env, capture_output=True, text=True, check=True)
        return json.loads(result.stdout)

    def test_runtime_needs_only_compose_and_never_builds_or_mounts_source(self):
        with tempfile.TemporaryDirectory() as temporary:
            directory = Path(temporary)
            shutil.copy2(ROOT / "docker-compose.yml", directory)
            config = self.config(directory=directory)
        self.assertEqual(set(config["services"]), {"core", "console"})
        for name in ("core", "console"):
            service = config["services"][name]
            self.assertNotIn("build", service, "runtime must not build images")
            tag = "core" if name == "core" else "webui"
            self.assertRegex(service["image"], rf"^registry\.cn-hangzhou\.aliyuncs\.com/cateyes/go:wb2api-{tag}-[1-9][0-9]{{9}}$")
            self.assertEqual(service["platform"], "linux/amd64")
            for mount in service["volumes"]:
                self.assertEqual(mount["type"], "bind")
                self.assertTrue(mount["source"].startswith(str(directory / "runtime/wb2api") + "/"))
            self.assertNotIn("healthcheck", service)
            self.assertEqual(service["environment"], {"TZ": "Asia/Shanghai"})
        self.assertNotIn("ports", config["services"]["core"])
        self.assertEqual(len(config["services"]["console"]["ports"]), 1)
        self.assertEqual(config["services"]["core"]["image"].rsplit("-", 1)[1],
                         config["services"]["console"]["image"].rsplit("-", 1)[1])

        self.assertEqual(config["services"]["console"]["depends_on"]["core"]["condition"], "service_started")
        self.assertNotIn("depends_on", config["services"]["core"])
        self.assertTrue(config["services"]["console"]["volumes"][0]["read_only"])

    def test_runtime_ignores_host_environment_without_explicit_yaml_configuration(self):
        baseline = self.config()
        config = self.config(overrides={"WB2A_VERSION": "1789519600", "TZ": "UTC",
                                      "WB2A_PORT": "19999", "WB2A_BIND_ADDRESS": "127.0.0.2",
                                      "WB2A_AUTHS_VOLUME": "unwanted", "WB2A_CONFIG_FILE": "/unwanted.json",
                                      "WB2A_PUBLIC_ORIGIN": "https://unwanted.test",
                                      "WB2A_CORE_IMAGE": "other/core:old",
                                      "WB2A_CONSOLE_IMAGE": "other/console:new",
                                      "WB2A_ADMIN_KEY": "unexpected-admin",
                                      "WB2A_API_KEY": "unexpected-api"})
        self.assertEqual(config, baseline, "host environment must not alter deployment")

    def test_commented_keys_can_be_configured_directly_in_yaml(self):
        with tempfile.TemporaryDirectory() as temporary:
            directory = Path(temporary)
            text = (ROOT / "docker-compose.yml").read_text()
            text = text.replace('# WB2A_ADMIN_KEY: ""', 'WB2A_ADMIN_KEY: "' + "c" * 32 + '"')
            text = text.replace('# WB2A_API_KEY: ""', 'WB2A_API_KEY: "fixture-api"')
            (directory / "docker-compose.yml").write_text(text)
            config = self.config(directory=directory)
        for service in config["services"].values():
            self.assertEqual(service["environment"], {"TZ": "Asia/Shanghai",
                             "WB2A_ADMIN_KEY": "c" * 32, "WB2A_API_KEY": "fixture-api"})

    def test_source_build_entry_preserves_config_and_runtime_topology(self):
        build_file = ROOT / "docker-compose.build.yaml"
        self.assertTrue(build_file.is_file(), "source build entry missing")
        for selected in ("", str(ROOT / "deploy/acceptance-config.json")):
            with self.subTest(selected=selected):
                config = self.config(selected, files=[build_file])
                self.assertEqual(set(config["services"]), {"core", "console"})
                for name in ("core", "console"):
                    self.assertEqual(config["services"][name]["build"]["dockerfile"], f"deploy/{name}.Dockerfile")
                    self.assertTrue(config["services"][name]["build"].get("pull"), "build must resolve target-platform base")
                mounts = [m for m in config["services"]["core"]["volumes"] if m["target"] == "/app/config.json"]
                self.assertEqual(len(mounts), 1, "production config selection is missing")
                mount = mounts[0]
                self.assertEqual(mount["source"], str(ROOT / "deploy/default-config.json"))
                self.assertTrue(Path(mount["source"]).is_file())
                self.assertTrue(mount["read_only"])
                self.assertFalse(mount.get("bind", {}).get("create_host_path", False))
                for service in ("core", "console"):
                    self.assertEqual(config["services"][service]["environment"], {"TZ": "Asia/Shanghai"})

    def test_optional_runtime_config_is_explicit_and_readonly(self):
        override = ROOT / "deploy/compose.config.yml"
        self.assertTrue(override.is_file(), "explicit config entry missing")
        config = self.config(str(ROOT / "deploy/acceptance-config.json"),
                             files=[ROOT / "docker-compose.yml", override])
        mounts = [m for m in config["services"]["core"]["volumes"] if m["target"] == "/app/config.json"]
        self.assertEqual(len(mounts), 1)
        self.assertTrue(mounts[0]["read_only"])
        self.assertFalse(mounts[0].get("bind", {}).get("create_host_path", False))
        self.assertEqual(mounts[0]["source"], str(ROOT / "runtime/wb2api/config.json"))
        self.assertEqual(config, self.config(files=[ROOT / "docker-compose.yml", override]))


if __name__ == "__main__":
    unittest.main()
