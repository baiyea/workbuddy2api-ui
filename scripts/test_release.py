import importlib.util
import json
from pathlib import Path
import subprocess
import unittest
from unittest import mock


class ReleaseTests(unittest.TestCase):
    def load_release(self):
        path = Path(__file__).with_name("release.py")
        self.assertTrue(path.is_file(), "manual release entry missing")
        spec = importlib.util.spec_from_file_location("release", path)
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        return module

    def pipeline(self, failure=None, existing=False, concurrent=False):
        release = self.load_release()
        calls = []

        def command(args, **kwargs):
            calls.append((args, kwargs))
            if failure and failure in " ".join(args):
                raise subprocess.CalledProcessError(1, args)
            out = ""
            if args[:3] == ["git", "rev-parse", "HEAD"]:
                out = "1" * 40
            elif args[0] == "curl":
                accepted = any("scripts/acceptance.sh" in " ".join(a) for a, _ in calls)
                out = "200" if existing or (concurrent and accepted) or any(a[:2] == ["docker", "push"] for a, _ in calls) else "404"
            elif args[:3] == ["docker", "image", "inspect"]:
                out = json.dumps([{"Os":"linux", "Architecture":"amd64"}] * 2)
            return subprocess.CompletedProcess(args, 0, out, "")

        with mock.patch.object(release.subprocess, "run", side_effect=command), mock.patch("builtins.print"):
            if failure:
                with self.assertRaises(subprocess.CalledProcessError):
                    release.release(Path("/fixture"), "0.1.0")
            elif existing or concurrent:
                with self.assertRaisesRegex(RuntimeError, "already exists"):
                    release.release(Path("/fixture"), "0.1.0")
            else:
                release.release(Path("/fixture"), "0.1.0")
        return calls

    def test_invalid_version_stops_before_any_external_command(self):
        release = self.load_release()
        for version in ("latest", "../x", "0.1.0\n", "0.1"):
            with self.subTest(version=version), mock.patch.object(release.subprocess, "run") as run:
                with self.assertRaises(ValueError):
                    release.release(Path("/fixture"), version)
                run.assert_not_called()

    def test_checks_or_acceptance_failure_never_publishes(self):
        for failure in ("scripts/check.sh", "scripts/acceptance.sh"):
            with self.subTest(failure=failure):
                calls = self.pipeline(failure=failure)
                self.assertFalse(any(args[:2] == ["docker", "push"] for args, _ in calls))

    def test_existing_public_tag_is_not_overwritten(self):
        calls = self.pipeline(existing=True)
        self.assertFalse(any(args[0] == "docker" for args, _ in calls))

    def test_tag_created_during_acceptance_stops_before_push(self):
        calls = self.pipeline(concurrent=True)
        self.assertFalse(any(args[:2] == ["docker", "push"] for args, _ in calls))

    def test_both_amd64_images_are_accepted_before_push_and_publicly_checked(self):
        calls = self.pipeline()
        builds = [args for args, _ in calls if args[:3] == ["docker", "buildx", "build"]]
        self.assertEqual(len(builds), 2)
        for args in builds:
            self.assertEqual(args[args.index("--platform") + 1], "linux/amd64")
            self.assertIn("--load", args)
            self.assertIn("--pull", args)
            self.assertNotIn("--push", args)
        acceptance_index = next(i for i, (args, _) in enumerate(calls) if "scripts/acceptance.sh" in " ".join(args))
        env = calls[acceptance_index][1]["env"]
        self.assertEqual(env["WB2A_ACCEPTANCE_SKIP_BUILD"], "true")
        self.assertEqual(env["WB2A_ACCEPTANCE_CORE_IMAGE"], "baiyea/workbuddy2api-core:0.1.0")
        self.assertEqual(env["WB2A_ACCEPTANCE_CONSOLE_IMAGE"], "baiyea/workbuddy2api-console:0.1.0")
        pushes = [(i, args) for i, (args, _) in enumerate(calls) if args[:2] == ["docker", "push"]]
        self.assertEqual([args[-1] for _, args in pushes], ["baiyea/workbuddy2api-core:0.1.0", "baiyea/workbuddy2api-console:0.1.0"])
        self.assertTrue(all(i > acceptance_index for i, _ in pushes))
        self.assertTrue(any(args[0] == "curl" for args, _ in calls[pushes[-1][0] + 1:]))


if __name__ == "__main__":
    unittest.main()
