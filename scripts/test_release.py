import importlib.util
import json
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest import mock

ROOT = Path(__file__).resolve().parents[1]
REPOSITORY = "registry.cn-hangzhou.aliyuncs.com/cateyes/go"
STAMP = "1789519600"
IMAGES = [f"{REPOSITORY}:wb2api-{name}-{STAMP}" for name in ("core", "webui")]


class ReleaseTests(unittest.TestCase):
    def load_release(self):
        spec = importlib.util.spec_from_file_location("release", ROOT / "scripts/release.py")
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        return module

    def pipeline(self, failure=None, registry_error=None, existing=False, concurrent=False, mismatched=False):
        release = self.load_release()
        calls = []
        metadata = [{"Os": "linux", "Architecture": "amd64", "Id": f"sha256:{i}"} for i in range(2)]

        def command(args, **kwargs):
            calls.append((args, kwargs))
            if failure and failure in " ".join(args):
                raise subprocess.CalledProcessError(1, args)
            out = ""
            if args[:3] == ["git", "rev-parse", "HEAD"]:
                out = "1" * 40
            elif args[:3] == ["docker", "manifest", "inspect"]:
                accepted = any("scripts/acceptance.sh" in " ".join(a) for a, _ in calls)
                found = existing or (concurrent and accepted)
                if registry_error or not found:
                    return subprocess.CompletedProcess(args, 1, "", registry_error or "manifest unknown")
                out = "{}"
            elif args[:3] == ["docker", "image", "inspect"]:
                changed = mismatched and any(a[:2] == ["docker", "pull"] for a, _ in calls)
                out = json.dumps([{**i, "Id": "sha256:unexpected"} for i in metadata] if changed else metadata)
            return subprocess.CompletedProcess(args, 0, out, "")

        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            compose = root / "docker-compose.yml"
            before = (ROOT / "docker-compose.yml").read_text()
            compose.write_text(before)
            with mock.patch.object(release.subprocess, "run", side_effect=command), mock.patch("builtins.print"):
                if failure:
                    with self.assertRaises(subprocess.CalledProcessError):
                        release.release(root, STAMP)
                elif existing or concurrent or registry_error or mismatched:
                    with self.assertRaises(RuntimeError):
                        release.release(root, STAMP)
                else:
                    with mock.patch.object(release.time, "time", return_value=int(STAMP)):
                        release.release(root)
            after = compose.read_text()
            if failure or registry_error or existing or concurrent or mismatched:
                self.assertEqual(after, before, "failed publication must preserve Compose")
            else:
                for image in IMAGES:
                    self.assertEqual(after.count(image), 1)
                self.assertNotIn("WB2A_VERSION", after)
        return calls

    def test_bash_entry_works_outside_repository(self):
        result = subprocess.run(["bash", str(ROOT / "scripts/release.sh"), "--help"],
                                cwd="/tmp", capture_output=True, text=True)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("timestamp", result.stdout)

    def test_invalid_timestamp_stops_before_external_commands(self):
        release = self.load_release()
        for version in ("latest", "../x", "1789519600\n", "0.1.0", "", "-1789519600"):
            with self.subTest(version=version), mock.patch.object(release.subprocess, "run") as run:
                with self.assertRaises(ValueError):
                    release.release(ROOT, version)
                run.assert_not_called()

    def test_checks_acceptance_push_or_pull_failure_preserves_compose(self):
        for failure in ("scripts/check.sh", "scripts/acceptance.sh", "docker push " + IMAGES[1], "docker pull"):
            with self.subTest(failure=failure):
                calls = self.pipeline(failure=failure)
                if failure.startswith("scripts/"):
                    self.assertFalse(any(a[:2] == ["docker", "push"] for a, _ in calls))

    def test_existing_tag_or_registry_failure_never_publishes(self):
        for options in ({"existing": True}, {"concurrent": True},
                        {"registry_error": "unauthorized"}, {"registry_error": "context deadline exceeded"}):
            with self.subTest(options=options):
                calls = self.pipeline(**options)
                self.assertFalse(any(a[:2] == ["docker", "push"] for a, _ in calls))

    def test_timestamp_pair_is_accepted_then_pushed_and_pulled(self):
        calls = self.pipeline()
        builds = [a for a, _ in calls if a[:3] == ["docker", "buildx", "build"]]
        self.assertEqual([a[a.index("--tag") + 1] for a in builds], IMAGES)
        for args in builds:
            self.assertEqual(args[args.index("--platform") + 1], "linux/amd64")
            self.assertIn("--load", args)
            self.assertIn("--pull", args)
            self.assertNotIn("--push", args)
        accepted = next(i for i, (a, _) in enumerate(calls) if "scripts/acceptance.sh" in " ".join(a))
        env = calls[accepted][1]["env"]
        self.assertEqual(env["WB2A_ACCEPTANCE_SKIP_BUILD"], "true")
        self.assertEqual(env["WB2A_ACCEPTANCE_CORE_IMAGE"], IMAGES[0])
        self.assertEqual(env["WB2A_ACCEPTANCE_CONSOLE_IMAGE"], IMAGES[1])
        pushes = [(i, a[-1]) for i, (a, _) in enumerate(calls) if a[:2] == ["docker", "push"]]
        self.assertEqual([ref for _, ref in pushes], IMAGES)
        self.assertTrue(all(i > accepted for i, _ in pushes))
        pulls = [(i, a[-1]) for i, (a, _) in enumerate(calls) if a[:2] == ["docker", "pull"]]
        self.assertEqual([ref for _, ref in pulls], IMAGES)
        self.assertTrue(all(i > pushes[-1][0] for i, _ in pulls))

    def test_pulled_image_mismatch_preserves_compose(self):
        self.pipeline(mismatched=True)

    def test_container_proxy_only_reaches_build_arguments(self):
        with mock.patch.dict("os.environ", {"HTTP_PROXY": "http://127.0.0.1:7890",
                                           "WB2A_BUILD_HTTP_PROXY": "http://host.docker.internal:7890"}):
            calls = self.pipeline()
        builds = [a for a, _ in calls if a[:3] == ["docker", "buildx", "build"]]
        for args in builds:
            self.assertIn("HTTP_PROXY=http://host.docker.internal:7890", args)
            self.assertNotIn("HTTP_PROXY=http://127.0.0.1:7890", args)
        for args, kwargs in calls:
            if args[:3] == ["docker", "manifest", "inspect"]:
                self.assertIsNone(kwargs.get("env"), "registry CLI must inherit host proxy")


if __name__ == "__main__":
    unittest.main()
