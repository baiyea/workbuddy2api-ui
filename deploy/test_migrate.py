import hashlib
import io
import json
import os
import tarfile
import tempfile
import unittest
from contextlib import redirect_stdout
from pathlib import Path
from unittest import mock

import migrate


class InspectTests(unittest.TestCase):
    def test_inspect_reports_only_identity_and_required_mount_fields(self):
        raw = [{
            "Id": "container-id",
            "Name": "/legacy",
            "Image": "sha256:image",
            "Config": {"Env": ["TOKEN=secret"]},
            "State": {"Running": True},
            "Mounts": [
                {"Name": "legacy_auths", "Source": "/vol/auths", "Destination": "/app/auths", "Type": "volume", "RW": True},
                {"Name": "", "Source": "/srv/data", "Destination": "/app/data", "Type": "bind", "RW": True},
            ],
        }]
        completed = mock.Mock(returncode=0, stdout=json.dumps(raw), stderr="")
        with mock.patch("migrate.subprocess.run", return_value=completed):
            manifest = migrate.inspect_container("legacy")
        self.assertEqual(
            manifest,
            {
                "container": {"Id": "container-id", "Name": "legacy", "Image": "sha256:image", "Running": True},
                "mounts": [
                    {"Name": "legacy_auths", "Source": "/vol/auths", "Destination": "/app/auths", "Type": "volume"},
                    {"Name": "", "Source": "/srv/data", "Destination": "/app/data", "Type": "bind"},
                ],
            },
        )
        self.assertNotIn("secret", json.dumps(manifest))

    def test_inspect_rejects_missing_duplicate_or_unknown_mounts(self):
        cases = [
            [{"Destination": "/app/auths", "Type": "volume", "Name": "a", "Source": "/a"}],
            [
                {"Destination": "/app/auths", "Type": "volume", "Name": "a", "Source": "/a"},
                {"Destination": "/app/data", "Type": "tmpfs", "Name": "", "Source": ""},
            ],
            [
                {"Destination": "/app/auths", "Type": "volume", "Name": "a", "Source": "/a"},
                {"Destination": "/app/auths", "Type": "volume", "Name": "b", "Source": "/b"},
                {"Destination": "/app/data", "Type": "bind", "Name": "", "Source": "/data"},
            ],
        ]
        for mounts in cases:
            with self.subTest(mounts=mounts):
                raw = [{"Id": "id", "Name": "/legacy", "Image": "image", "State": {"Running": False}, "Mounts": mounts}]
                completed = mock.Mock(returncode=0, stdout=json.dumps(raw), stderr="")
                with mock.patch("migrate.subprocess.run", return_value=completed), self.assertRaises(ValueError):
                    migrate.inspect_container("legacy")


class BackupTests(unittest.TestCase):
    def test_backup_archives_confirmed_bind_mounts_with_hashes_and_restricted_modes(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            auths, data = root / "auths", root / "data"
            auths.mkdir()
            data.mkdir()
            (auths / "account.json").write_text("mock-account\n", encoding="utf-8")
            (data / "state.json").write_text("mock-state\n", encoding="utf-8")
            manifest = {
                "container": {"Id": "id", "Name": "legacy", "Image": "image", "Running": True},
                "mounts": [
                    {"Name": "", "Source": str(auths), "Destination": "/app/auths", "Type": "bind"},
                    {"Name": "", "Source": str(data), "Destination": "/app/data", "Type": "bind"},
                ],
            }
            manifest_path = root / "inspect.json"
            manifest_path.write_text(json.dumps(manifest), encoding="utf-8")
            output = root / "backup"

            with mock.patch("migrate.inspect_container", return_value=manifest):
                result = migrate.backup(manifest_path, output)

            self.assertFalse(result["consistent"])
            self.assertIn("最终一致性", result["warning"])
            self.assertEqual(0o700, output.stat().st_mode & 0o777)
            for item, expected_member in zip(result["archives"], ("account.json", "state.json")):
                archive = output / item["file"]
                self.assertEqual(0o600, archive.stat().st_mode & 0o777)
                self.assertEqual(hashlib.sha256(archive.read_bytes()).hexdigest(), item["sha256"])
                self.assertEqual(archive.stat().st_size, item["size"])
                with tarfile.open(archive, "r:gz") as bundle:
                    self.assertIn(expected_member, bundle.getnames())
            saved = json.loads((output / "backup-manifest.json").read_text(encoding="utf-8"))
            self.assertEqual(result, saved)

    def test_backup_rejects_missing_volume_before_creating_archive_or_volume(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            manifest = {
                "container": {"Id": "id", "Name": "legacy", "Image": "image", "Running": False},
                "mounts": [
                    {"Name": "legacy_auths", "Source": "/vol/auths", "Destination": "/app/auths", "Type": "volume"},
                    {"Name": "legacy_data", "Source": "/vol/data", "Destination": "/app/data", "Type": "volume"},
                ],
            }
            manifest_path = root / "inspect.json"
            manifest_path.write_text(json.dumps(manifest), encoding="utf-8")
            output = root / "backup"
            with (
                mock.patch("migrate.inspect_container", return_value=manifest),
                mock.patch("migrate._inspect_volume", side_effect=ValueError("volume does not exist")),
                mock.patch("migrate._archive_volume") as archive,
                self.assertRaises(ValueError),
            ):
                migrate.backup(manifest_path, output)
            archive.assert_not_called()
            self.assertFalse(output.exists())

    def test_backup_rejects_volume_whose_live_source_changed(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            manifest = {
                "container": {"Id": "id", "Name": "legacy", "Image": "image", "Running": False},
                "mounts": [
                    {"Name": "legacy_auths", "Source": "/vol/auths", "Destination": "/app/auths", "Type": "volume"},
                    {"Name": "legacy_data", "Source": "/vol/data", "Destination": "/app/data", "Type": "volume"},
                ],
            }
            manifest_path = root / "inspect.json"
            manifest_path.write_text(json.dumps(manifest), encoding="utf-8")
            output = root / "backup"
            with (
                mock.patch("migrate.inspect_container", return_value=manifest),
                mock.patch("migrate._inspect_volume", return_value={"Name": "legacy_auths", "Mountpoint": "/vol/replaced"}),
                mock.patch("migrate._archive_volume") as archive,
                self.assertRaises(ValueError),
            ):
                migrate.backup(manifest_path, output)
            archive.assert_not_called()
            self.assertFalse(output.exists())

    def test_backup_rejects_replaced_container_or_changed_mapping(self):
        manifest = {
            "container": {"Id": "original", "Name": "legacy", "Image": "image", "Running": False},
            "mounts": [
                {"Name": "legacy_auths", "Source": "/vol/auths", "Destination": "/app/auths", "Type": "volume"},
                {"Name": "legacy_data", "Source": "/vol/data", "Destination": "/app/data", "Type": "volume"},
            ],
        }
        replaced = {**manifest, "container": {**manifest["container"], "Id": "replacement"}}
        remapped = {**manifest, "mounts": [dict(manifest["mounts"][0]), {**manifest["mounts"][1], "Name": "other", "Source": "/vol/other"}]}
        for name, current in (("container", replaced), ("mapping", remapped)):
            with self.subTest(name=name), tempfile.TemporaryDirectory() as temp:
                root = Path(temp)
                manifest_path = root / "inspect.json"
                manifest_path.write_text(json.dumps(manifest), encoding="utf-8")
                with mock.patch("migrate.inspect_container", return_value=current), self.assertRaises(ValueError):
                    migrate.backup(manifest_path, root / "backup")
                self.assertFalse((root / "backup").exists())

    def test_backup_uses_pre_and_post_archive_running_state(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            auths, data = root / "auths", root / "data"
            auths.mkdir()
            data.mkdir()
            stopped = {
                "container": {"Id": "id", "Name": "legacy", "Image": "image", "Running": False},
                "mounts": [
                    {"Name": "", "Source": str(auths), "Destination": "/app/auths", "Type": "bind"},
                    {"Name": "", "Source": str(data), "Destination": "/app/data", "Type": "bind"},
                ],
            }
            saved = {**stopped, "container": {**stopped["container"], "Running": True}}
            manifest_path = root / "inspect.json"
            manifest_path.write_text(json.dumps(saved), encoding="utf-8")
            with mock.patch("migrate.inspect_container", side_effect=[stopped, stopped]) as inspect:
                result = migrate.backup(manifest_path, root / "backup")
            self.assertEqual(2, inspect.call_count)
            self.assertTrue(result["consistent"])
            self.assertFalse(result["container"]["Running"])

            running = {**stopped, "container": {**stopped["container"], "Running": True}}
            with mock.patch("migrate.inspect_container", side_effect=[stopped, running]):
                restarted = migrate.backup(manifest_path, root / "backup-restarted")
            self.assertFalse(restarted["consistent"])
            self.assertTrue(restarted["container"]["Running"])
            self.assertIn("最终一致性", restarted["warning"])

    def test_cli_inspect_prints_json_without_environment(self):
        manifest = {"container": {"Id": "id", "Name": "legacy", "Image": "image", "Running": False}, "mounts": []}
        stdout = io.StringIO()
        with mock.patch("migrate.inspect_container", return_value=manifest), redirect_stdout(stdout):
            self.assertEqual(0, migrate.main(["inspect", "--container", "legacy"]))
        self.assertEqual(manifest, json.loads(stdout.getvalue()))


if __name__ == "__main__":
    unittest.main()
