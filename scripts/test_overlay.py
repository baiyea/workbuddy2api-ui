import json
import os
import stat
import subprocess
import tempfile
import unittest
from unittest import mock
from pathlib import Path

from overlay import export_snapshot, materialize, overlay_identity, source_digest


def run_git(repo, *args, input=None):
    return subprocess.run(
        ["git", *args],
        cwd=repo,
        input=input,
        stdout=subprocess.PIPE,
        check=True,
    ).stdout


def commit_fixture(repo):
    run_git(repo, "init", "-q")
    run_git(repo, "config", "user.name", "Overlay Test")
    run_git(repo, "config", "user.email", "overlay@example.invalid")
    (repo / "LICENSE").write_bytes(b"license bytes\n")
    script = repo / "run.sh"
    script.write_bytes(b"#!/bin/sh\nexit 0\n")
    script.chmod(0o755)
    run_git(repo, "add", "LICENSE", "run.sh")
    run_git(repo, "commit", "-qm", "fixture")
    return run_git(repo, "rev-parse", "HEAD").decode().strip()


def make_overlay_root(root):
    upstream = root / "upstream"
    upstream.mkdir()
    (upstream / "base.txt").write_text("old\n", encoding="utf-8")
    (root / "patches").mkdir()
    (root / "patches" / "series").write_text("", encoding="utf-8")
    lock = {
        "format": 1,
        "repository": "https://example.invalid/upstream.git",
        "commit": "1" * 40,
        "source_sha256": source_digest(upstream),
    }
    (root / "upstream.lock").write_text(
        json.dumps(lock, indent=2) + "\n", encoding="utf-8"
    )


class SourceDigestTests(unittest.TestCase):
    def test_digest_tracks_content_and_executable_mode(self):
        with tempfile.TemporaryDirectory() as d:
            root = Path(d)
            path = root / "run.sh"
            path.write_bytes(b"exit 0\n")
            path.chmod(0o644)
            first = source_digest(root)
            path.chmod(0o755)
            mode_changed = source_digest(root)
            self.assertNotEqual(first, mode_changed)
            path.write_bytes(b"exit 1\n")
            self.assertNotEqual(mode_changed, source_digest(root))

    def test_digest_hashes_symlink_text_without_following_it(self):
        with tempfile.TemporaryDirectory() as d:
            root = Path(d)
            os.symlink("first", root / "link")
            first = source_digest(root)
            (root / "link").unlink()
            os.symlink("second", root / "link")
            self.assertNotEqual(first, source_digest(root))

    def test_digest_rejects_escaping_symlink_nested_git_and_device(self):
        cases = (
            lambda root: os.symlink("../outside", root / "link"),
            lambda root: (root / ".git").mkdir(),
            lambda root: os.mkfifo(root / "pipe"),
        )
        for build in cases:
            with self.subTest(build=build), tempfile.TemporaryDirectory() as d:
                root = Path(d)
                build(root)
                with self.assertRaises(ValueError):
                    source_digest(root)


class OverlayIdentityTests(unittest.TestCase):
    def test_identity_hashes_extension_manifest_series_and_ordered_patch_bytes(self):
        with tempfile.TemporaryDirectory() as d:
            root = Path(d)
            extension = root / "extensions" / "extra"
            extension.mkdir(parents=True)
            (extension / "new.txt").write_bytes(b"extension\n")
            patches = root / "patches"
            patches.mkdir()
            (patches / "series").write_bytes(b"# order\nchange.patch\n")
            (patches / "change.patch").write_bytes(b"patch bytes\n")

            self.assertEqual(
                "9318ffe4a70f3e26e3e3194355ca15609bf7c5d1c3871e458a8e1c0a3d4c587b",
                overlay_identity(root),
            )


class ExportSnapshotTests(unittest.TestCase):
    def test_export_uses_committed_blobs_and_preserves_license_and_modes(self):
        with tempfile.TemporaryDirectory() as d:
            temp = Path(d)
            repo = temp / "repo"
            repo.mkdir()
            commit = commit_fixture(repo)
            (repo / "LICENSE").write_bytes(b"dirty bytes\n")

            dest = temp / "snapshot"
            export_snapshot(repo, commit, dest)

            self.assertEqual(b"license bytes\n", (dest / "LICENSE").read_bytes())
            self.assertEqual(b"#!/bin/sh\nexit 0\n", (dest / "run.sh").read_bytes())
            self.assertEqual(0o755, stat.S_IMODE((dest / "run.sh").stat().st_mode))
            self.assertFalse((dest / ".git").exists())

    def test_export_rejects_gitlink_and_removes_only_its_new_destination(self):
        with tempfile.TemporaryDirectory() as d:
            temp = Path(d)
            repo = temp / "repo"
            repo.mkdir()
            commit = commit_fixture(repo)
            run_git(repo, "update-index", "--add", "--cacheinfo", f"160000,{commit},nested")
            run_git(repo, "commit", "-qm", "gitlink")
            gitlink_commit = run_git(repo, "rev-parse", "HEAD").decode().strip()
            dest = temp / "snapshot"

            with self.assertRaises(ValueError):
                export_snapshot(repo, gitlink_commit, dest)
            self.assertFalse(dest.exists())


class MaterializeTests(unittest.TestCase):
    def test_materialize_applies_extension_and_ordered_patch(self):
        with tempfile.TemporaryDirectory() as d:
            root = Path(d)
            make_overlay_root(root)
            extension = root / "extensions" / "extra"
            extension.mkdir(parents=True)
            (extension / "new.txt").write_text("extension\n", encoding="utf-8")
            patch = root / "patches" / "change.patch"
            patch.write_text(
                "diff --git a/base.txt b/base.txt\n"
                "--- a/base.txt\n"
                "+++ b/base.txt\n"
                "@@ -1 +1 @@\n"
                "-old\n"
                "+new\n",
                encoding="utf-8",
            )
            (root / "patches" / "series").write_text("change.patch\n", encoding="utf-8")

            dest = root / "build"
            materialize(root, dest)

            self.assertEqual("new\n", (dest / "base.txt").read_text(encoding="utf-8"))
            self.assertEqual(
                "extension\n", (dest / "extra" / "new.txt").read_text(encoding="utf-8")
            )

    def test_materialize_without_overlay_is_reproducible_and_keeps_upstream_pristine(self):
        with tempfile.TemporaryDirectory() as d:
            root = Path(d)
            make_overlay_root(root)
            before = source_digest(root / "upstream")

            first = root / "first"
            second = root / "second"
            materialize(root, first)
            materialize(root, second)

            self.assertEqual(before, source_digest(root / "upstream"))
            self.assertEqual(source_digest(first), source_digest(second))
            self.assertEqual(before, source_digest(first))

    def test_materialize_applies_patch_only_inside_destination_nested_in_git_repo(self):
        with tempfile.TemporaryDirectory() as d:
            root = Path(d)
            make_overlay_root(root)
            (root / "base.txt").write_text("old\n", encoding="utf-8")
            run_git(root, "init", "-q")
            patch = root / "patches" / "change.patch"
            patch.write_text(
                "diff --git a/base.txt b/base.txt\n"
                "--- a/base.txt\n"
                "+++ b/base.txt\n"
                "@@ -1 +1 @@\n"
                "-old\n"
                "+new\n",
                encoding="utf-8",
            )
            (root / "patches" / "series").write_text("change.patch\n", encoding="utf-8")

            dest = root / ".build" / "core"
            materialize(root, dest)

            self.assertEqual("new\n", (dest / "base.txt").read_text(encoding="utf-8"))
            self.assertFalse((dest / ".git").exists())
            self.assertEqual("old\n", (root / "base.txt").read_text(encoding="utf-8"))
            self.assertEqual(
                "old\n", (root / "upstream" / "base.txt").read_text(encoding="utf-8")
            )

    def test_materialize_rejects_existing_destination_without_touching_it(self):
        with tempfile.TemporaryDirectory() as d:
            root = Path(d)
            make_overlay_root(root)
            dest = root / "build"
            dest.mkdir()
            sentinel = dest / "keep"
            sentinel.write_text("mine", encoding="utf-8")

            with self.assertRaises(FileExistsError):
                materialize(root, dest)
            self.assertEqual("mine", sentinel.read_text(encoding="utf-8"))

    def test_materialize_rejects_damaged_lock_before_creating_destination(self):
        with tempfile.TemporaryDirectory() as d:
            root = Path(d)
            make_overlay_root(root)
            (root / "upstream" / "base.txt").write_text("tampered\n", encoding="utf-8")
            dest = root / "build"

            with self.assertRaises(ValueError):
                materialize(root, dest)
            self.assertFalse(dest.exists())

    def test_materialize_rejects_extension_collision_and_cleans_new_destination(self):
        with tempfile.TemporaryDirectory() as d:
            root = Path(d)
            make_overlay_root(root)
            extension = root / "extensions"
            extension.mkdir()
            (extension / "base.txt").write_text("overwrite\n", encoding="utf-8")
            dest = root / "build"

            with self.assertRaises(FileExistsError):
                materialize(root, dest)
            self.assertFalse(dest.exists())

    def test_materialize_rejects_extension_below_symlink_parent(self):
        with tempfile.TemporaryDirectory() as d:
            root = Path(d)
            make_overlay_root(root)
            (root / "upstream" / "real").mkdir()
            os.symlink("real", root / "upstream" / "link")
            lock = json.loads((root / "upstream.lock").read_text(encoding="utf-8"))
            lock["source_sha256"] = source_digest(root / "upstream")
            (root / "upstream.lock").write_text(
                json.dumps(lock, indent=2) + "\n", encoding="utf-8"
            )
            extension = root / "extensions" / "link"
            extension.mkdir(parents=True)
            (extension / "escaped.txt").write_text("no\n", encoding="utf-8")
            dest = root / "build"

            with self.assertRaises(ValueError):
                materialize(root, dest)
            self.assertFalse(dest.exists())

    def test_materialize_rejects_series_traversal_and_duplicates(self):
        series_values = ("../outside.patch\n", "same.patch\nsame.patch\n")
        for series in series_values:
            with self.subTest(series=series), tempfile.TemporaryDirectory() as d:
                root = Path(d)
                make_overlay_root(root)
                (root / "patches" / "same.patch").write_text("", encoding="utf-8")
                (root / "patches" / "series").write_text(series, encoding="utf-8")
                dest = root / "build"

                with self.assertRaises(ValueError):
                    materialize(root, dest)
                self.assertFalse(dest.exists())

    def test_materialize_rejects_patch_alias_duplicate_and_symlink_parent(self):
        series_values = ("change.patch\n./change.patch\n", "linked/change.patch\n")
        for series in series_values:
            with self.subTest(series=series), tempfile.TemporaryDirectory() as d:
                root = Path(d)
                make_overlay_root(root)
                patch_dir = root / "patches"
                if series.startswith("linked"):
                    outside = root / "outside"
                    outside.mkdir()
                    os.symlink(outside, patch_dir / "linked")
                    patch = outside / "change.patch"
                else:
                    patch = patch_dir / "change.patch"
                patch.write_text(
                    "diff --git a/base.txt b/base.txt\n"
                    "--- a/base.txt\n"
                    "+++ b/base.txt\n"
                    "@@ -1 +1 @@\n"
                    "-old\n"
                    "+new\n",
                    encoding="utf-8",
                )
                (patch_dir / "series").write_text(series, encoding="utf-8")
                dest = root / "build"

                with self.assertRaises(ValueError):
                    materialize(root, dest)
                self.assertFalse(dest.exists())

    def test_materialize_rejects_output_inside_all_input_trees_before_mutation(self):
        cases = ("upstream", "extensions", "patches", "resolved-upstream")
        for case in cases:
            with self.subTest(case=case), tempfile.TemporaryDirectory() as d:
                root = Path(d)
                make_overlay_root(root)
                if case == "resolved-upstream":
                    os.symlink("upstream", root / "redirect")
                    input_tree = root / "upstream"
                    output_base = root / "redirect"
                else:
                    input_tree = root / case
                    output_base = input_tree
                before = source_digest(input_tree) if input_tree.exists() else None
                dest = output_base / "generated" / "core"

                with mock.patch(
                    "overlay.shutil.copytree",
                    side_effect=AssertionError("copytree must not run"),
                ):
                    with self.assertRaises(ValueError):
                        materialize(root, dest)

                self.assertFalse((input_tree / "generated").exists())
                if before is None:
                    self.assertFalse(input_tree.exists())
                else:
                    self.assertEqual(before, source_digest(input_tree))

    def test_materialize_stops_on_patch_conflict_and_cleans_new_destination(self):
        with tempfile.TemporaryDirectory() as d:
            root = Path(d)
            make_overlay_root(root)
            (root / "patches" / "conflict.patch").write_text(
                "diff --git a/base.txt b/base.txt\n"
                "--- a/base.txt\n"
                "+++ b/base.txt\n"
                "@@ -1 +1 @@\n"
                "-not-old\n"
                "+new\n",
                encoding="utf-8",
            )
            (root / "patches" / "series").write_text("conflict.patch\n", encoding="utf-8")
            dest = root / "build"

            with self.assertRaises(subprocess.CalledProcessError):
                materialize(root, dest)
            self.assertFalse(dest.exists())


if __name__ == "__main__":
    unittest.main()
