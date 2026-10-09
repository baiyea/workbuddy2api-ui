import hashlib
import json
import os
import shutil
import stat
import subprocess
import tempfile
import unittest
from unittest import mock
from pathlib import Path

from overlay import _fetch_candidate_snapshot, export_snapshot, materialize, overlay_identity, source_digest, update


CANONICAL_REPOSITORY = "https://github.com/Sliverkiss/workbuddy2api"


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


def make_update_root(root):
    upstream = root / "upstream"
    upstream.mkdir()
    (upstream / "version.txt").write_text("old\n", encoding="utf-8")
    for name in ("extensions", "patches", "deploy", "scripts", "console"):
        (root / name).mkdir()
    (root / "patches" / "series").write_text("", encoding="utf-8")
    (root / "scripts" / "check.sh").write_text("#!/bin/sh\n", encoding="utf-8")
    (root / "scripts" / "acceptance.sh").write_text("#!/bin/sh\n", encoding="utf-8")
    (root / "deploy" / "acceptance.env").write_text("SAFE=fixture\n", encoding="utf-8")
    (root / "deploy" / ".env").write_text("SECRET=never-copy\n", encoding="utf-8")
    (root / "deploy" / "auths").mkdir()
    (root / "deploy" / "auths" / "account.json").write_text("secret\n", encoding="utf-8")
    (root / "docker-compose.yml").write_text("services: {}\n", encoding="utf-8")
    (root / "docker-compose.build.yaml").write_text("services: {}\n", encoding="utf-8")
    (root / "LICENSE").write_text("upstream license\n", encoding="utf-8")
    (root / ".dockerignore").write_text("**\n", encoding="utf-8")
    lock = {
        "format": 1,
        "repository": CANONICAL_REPOSITORY,
        "commit": "1" * 40,
        "source_sha256": source_digest(upstream),
    }
    (root / "upstream.lock").write_text(
        json.dumps(lock, indent=2) + "\n", encoding="utf-8"
    )


def install_fake_fetch(root, calls, fail_command=None, dirty_on_second=False):
    status_calls = 0

    def fake_fetch(repository, ref, dest):
        calls.append(["fetch", repository, ref, str(dest)])
        if fail_command == "fetch":
            raise subprocess.CalledProcessError(1, ["git", "fetch"])
        dest.mkdir()
        (dest / "version.txt").write_text("new\n", encoding="utf-8")
        return "2" * 40

    def fake_run(args, **kwargs):
        nonlocal status_calls
        command = [str(value) for value in args]
        calls.append(command)
        if command[:3] == ["git", "status", "--porcelain"]:
            status_calls += 1
            dirty = dirty_on_second and status_calls == 2
            return subprocess.CompletedProcess(args, 0, b" M patches/series\n" if dirty else b"", b"")
        if fail_command and any(part == fail_command or part.endswith("/" + fail_command) for part in command):
            raise subprocess.CalledProcessError(1, args)
        return subprocess.CompletedProcess(args, 0, b"", b"")

    return fake_fetch, fake_run


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

    def test_candidate_fetch_resolves_requested_ref_without_default_branch_fallback(self):
        with tempfile.TemporaryDirectory() as d:
            temp = Path(d)
            repo = temp / "repo"
            repo.mkdir()
            commit = commit_fixture(repo)
            dest = temp / "work" / "upstream"
            dest.parent.mkdir()

            self.assertEqual(commit, _fetch_candidate_snapshot(str(repo), commit, dest))
            self.assertEqual(b"license bytes\n", (dest / "LICENSE").read_bytes())
            self.assertFalse((dest.parent / "fetch").exists())

            missing = temp / "missing-work" / "upstream"
            missing.parent.mkdir()
            with self.assertRaises(subprocess.CalledProcessError):
                _fetch_candidate_snapshot(str(repo), "missing-ref", missing)
            self.assertFalse(missing.exists())
            self.assertFalse((missing.parent / "fetch").exists())


class MaterializeTests(unittest.TestCase):
    def test_windows_checkout_preserves_snapshot_digest_and_rejects_content_changes(self):
        with tempfile.TemporaryDirectory() as d:
            repo, checkout = Path(d) / 'repo', Path(d) / 'checkout'
            repo.mkdir()
            make_overlay_root(repo)
            script = repo / 'upstream/run.sh'
            script.write_bytes(b'#!/bin/sh\nexit 0\n')
            script.chmod(0o755)
            (repo / 'patches/series').write_bytes(b'edit.patch\n')
            (repo / 'patches/edit.patch').write_bytes(b'--- a/base.txt\n+++ b/base.txt\n@@ -1 +1 @@\n-old\n+new\n')
            lock = json.loads((repo / 'upstream.lock').read_text())
            lock['source_sha256'] = source_digest(repo / 'upstream', file_modes={
                'base.txt': '100644', 'run.sh': '100755',
            })
            (repo / 'upstream.lock').write_text(json.dumps(lock))
            attributes = Path(__file__).resolve().parents[1] / '.gitattributes'
            (repo / '.gitattributes').write_bytes(attributes.read_bytes() if attributes.exists() else b'')
            run_git(repo, 'init', '-q')
            run_git(repo, 'add', '.')
            run_git(repo, 'update-index', '--chmod=+x', 'upstream/run.sh')
            run_git(repo, '-c', 'user.name=Test', '-c', 'user.email=test@example.invalid', 'commit', '-qm', 'fixture')
            run_git(Path(d), 'clone', '-q', '-c', 'core.autocrlf=true', str(repo), str(checkout))
            # Windows checkout lacks POSIX execution bits even though the index retains them.
            for path in (checkout / 'upstream').rglob('*'):
                if path.is_file():
                    path.chmod(0o644)
            with mock.patch('overlay.sys.platform', 'win32'):
                materialize(checkout, Path(d) / 'result')
                self.assertEqual((Path(d) / 'result/run.sh').read_bytes(), script.read_bytes())
                self.assertEqual((Path(d) / 'result/base.txt').read_bytes(), b'new\n')
                (checkout / 'upstream/run.sh').write_bytes(b'#!/bin/sh\nexit 1\n')
                with self.assertRaisesRegex(ValueError, 'source digest'):
                    materialize(checkout, Path(d) / 'tampered')
                self.assertFalse((Path(d) / 'tampered').exists())
                (checkout / 'upstream/run.sh').write_bytes(script.read_bytes())
                run_git(checkout, 'update-index', '--chmod=-x', 'upstream/run.sh')
                with self.assertRaisesRegex(ValueError, 'source digest'):
                    materialize(checkout, Path(d) / 'changed-mode')

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


class UpdateTests(unittest.TestCase):
    def test_candidate_copy_must_match_inputs_even_if_root_edit_is_restored(self):
        with tempfile.TemporaryDirectory() as d:
            temp = Path(d)
            root = temp / "root"
            root.mkdir()
            make_update_root(root)
            old_lock = (root / "upstream.lock").read_bytes()
            calls = []
            fake_fetch, fake_run = install_fake_fetch(root, calls)
            work = temp / "candidate-work"
            real_copy2 = shutil.copy2
            resolved_dockerignore = root.resolve() / ".dockerignore"

            def transient_edit_during_copy(source, dest, *args, **kwargs):
                source = Path(source)
                if source == resolved_dockerignore:
                    original = source.read_bytes()
                    source.write_text("transient candidate input\n", encoding="utf-8")
                    try:
                        return real_copy2(source, dest, *args, **kwargs)
                    finally:
                        source.write_bytes(original)
                return real_copy2(source, dest, *args, **kwargs)

            with mock.patch("overlay._fetch_candidate_snapshot", side_effect=fake_fetch), mock.patch(
                "overlay.subprocess.run", side_effect=fake_run
            ), mock.patch("overlay.tempfile.mkdtemp", side_effect=lambda **_: (work.mkdir(), str(work))[1]), mock.patch(
                "overlay.shutil.copy2", side_effect=transient_edit_during_copy
            ):
                with self.assertRaisesRegex(RuntimeError, "candidate inputs do not match"):
                    update(root, "candidate-ref")

            self.assertEqual(old_lock, (root / "upstream.lock").read_bytes())
            self.assertEqual("old\n", (root / "upstream" / "version.txt").read_text(encoding="utf-8"))
            self.assertEqual("**\n", (root / ".dockerignore").read_text(encoding="utf-8"))
            self.assertFalse(any(command and command[0] == "bash" for command in calls))

    def test_candidate_changed_by_validation_is_rejected_before_install(self):
        with tempfile.TemporaryDirectory() as d:
            temp = Path(d)
            root = temp / "root"
            root.mkdir()
            make_update_root(root)
            old_lock = (root / "upstream.lock").read_bytes()
            calls = []
            fake_fetch, base_run = install_fake_fetch(root, calls)

            def change_candidate(args, **kwargs):
                command = [str(value) for value in args]
                result = base_run(args, **kwargs)
                if command and command[0] == "bash" and command[1].endswith("/check.sh"):
                    (Path(command[2]) / ".dockerignore").write_text(
                        "changed by validation\n", encoding="utf-8"
                    )
                return result

            work = temp / "candidate-work"
            with mock.patch("overlay._fetch_candidate_snapshot", side_effect=fake_fetch), mock.patch(
                "overlay.subprocess.run", side_effect=change_candidate
            ), mock.patch("overlay.tempfile.mkdtemp", side_effect=lambda **_: (work.mkdir(), str(work))[1]):
                with self.assertRaisesRegex(RuntimeError, "candidate inputs do not match"):
                    update(root, "candidate-ref")

            self.assertEqual(old_lock, (root / "upstream.lock").read_bytes())
            self.assertEqual("old\n", (root / "upstream" / "version.txt").read_text(encoding="utf-8"))

    def test_update_rejects_unsafe_ref_and_noncanonical_lock_without_running_commands(self):
        for ref in ("-main", "main\nnext", "main\x00next"):
            with self.subTest(ref=ref), tempfile.TemporaryDirectory() as d:
                root = Path(d)
                make_update_root(root)
                with mock.patch("overlay.subprocess.run") as run:
                    with self.assertRaises(ValueError):
                        update(root, ref)
                    run.assert_not_called()

        with tempfile.TemporaryDirectory() as d:
            root = Path(d)
            make_update_root(root)
            lock = json.loads((root / "upstream.lock").read_text(encoding="utf-8"))
            lock["repository"] = "https://evil.invalid/upstream"
            (root / "upstream.lock").write_text(json.dumps(lock), encoding="utf-8")
            with mock.patch("overlay.subprocess.run") as run:
                with self.assertRaises(ValueError):
                    update(root, "main")
                run.assert_not_called()

    def test_candidate_validation_failure_preserves_old_combination_and_runs_no_deploy_or_git_publish(self):
        with tempfile.TemporaryDirectory() as d:
            temp = Path(d)
            root = temp / "root"
            root.mkdir()
            make_update_root(root)
            old_lock = (root / "upstream.lock").read_bytes()
            old_digest = source_digest(root / "upstream")
            calls = []
            fake_fetch, fake_run = install_fake_fetch(root, calls, "acceptance.sh")
            work = temp / "candidate-work"

            with mock.patch("overlay._fetch_candidate_snapshot", side_effect=fake_fetch), mock.patch(
                "overlay.subprocess.run", side_effect=fake_run
            ), mock.patch("overlay.tempfile.mkdtemp", side_effect=lambda **_: (work.mkdir(), str(work))[1]):
                with self.assertRaises(subprocess.CalledProcessError):
                    update(root, "candidate-ref")

            self.assertEqual(old_lock, (root / "upstream.lock").read_bytes())
            self.assertEqual(old_digest, source_digest(root / "upstream"))
            self.assertTrue((work / "candidate").is_dir())
            self.assertTrue((work / "candidate" / "deploy" / "acceptance.env").is_file())
            self.assertTrue((work / "candidate" / "docker-compose.build.yaml").is_file(), "candidate cannot build")
            self.assertTrue((work / "candidate" / "LICENSE").is_file(), "candidate omits license")
            self.assertEqual((work / "candidate" / "docker-compose.build.yaml").read_bytes(),
                             (root / "docker-compose.build.yaml").read_bytes())
            self.assertEqual((work / "candidate" / "LICENSE").read_bytes(), (root / "LICENSE").read_bytes())
            self.assertFalse((work / "candidate" / "deploy" / ".env").exists())
            self.assertFalse((work / "candidate" / "deploy" / "auths").exists())
            flat = [part for command in calls for part in command]
            self.assertNotIn("commit", flat)
            self.assertNotIn("push", flat)
            self.assertNotIn("restart", flat)
            self.assertNotIn("up", flat)

    def test_each_candidate_failure_keeps_snapshot_and_lock(self):
        failures = (
            "fetch",
            "check.sh:manifest",
            "check.sh:duplicate-patch",
            "check.sh:patch-conflict",
            "check.sh:extension-collision",
            "check.sh:go-test",
            "acceptance.sh:image-build",
            "acceptance.sh:runtime",
        )
        for failure in failures:
            with self.subTest(failure=failure), tempfile.TemporaryDirectory() as d:
                temp = Path(d)
                root = temp / "root"
                root.mkdir()
                make_update_root(root)
                old_lock = (root / "upstream.lock").read_bytes()
                old_digest = source_digest(root / "upstream")
                calls = []
                command = failure.split(":", 1)[0]
                fake_fetch, fake_run = install_fake_fetch(root, calls, command)
                work = temp / "candidate-work"
                with mock.patch("overlay._fetch_candidate_snapshot", side_effect=fake_fetch), mock.patch(
                    "overlay.subprocess.run", side_effect=fake_run
                ), mock.patch("overlay.tempfile.mkdtemp", side_effect=lambda **_: (work.mkdir(), str(work))[1]):
                    with self.assertRaises(subprocess.CalledProcessError):
                        update(root, "candidate-ref")
                self.assertEqual(old_lock, (root / "upstream.lock").read_bytes())
                self.assertEqual(old_digest, source_digest(root / "upstream"))

    def test_second_dirty_check_aborts_before_replacement(self):
        with tempfile.TemporaryDirectory() as d:
            temp = Path(d)
            root = temp / "root"
            root.mkdir()
            make_update_root(root)
            old_lock = (root / "upstream.lock").read_bytes()
            calls = []
            fake_fetch, fake_run = install_fake_fetch(root, calls, dirty_on_second=True)
            work = temp / "candidate-work"
            with mock.patch("overlay._fetch_candidate_snapshot", side_effect=fake_fetch), mock.patch(
                "overlay.subprocess.run", side_effect=fake_run
            ), mock.patch("overlay.tempfile.mkdtemp", side_effect=lambda **_: (work.mkdir(), str(work))[1]):
                with self.assertRaises(RuntimeError):
                    update(root, "candidate-ref")
            self.assertEqual(old_lock, (root / "upstream.lock").read_bytes())
            self.assertEqual("old\n", (root / "upstream" / "version.txt").read_text(encoding="utf-8"))

    def test_edit_after_candidate_staging_is_restored_instead_of_overwritten(self):
        with tempfile.TemporaryDirectory() as d:
            temp = Path(d)
            root = temp / "root"
            root.mkdir()
            make_update_root(root)
            old_lock = (root / "upstream.lock").read_bytes()
            calls = []
            fake_fetch, fake_run = install_fake_fetch(root, calls)
            work = temp / "candidate-work"
            real_replace = os.replace
            resolved_root = root.resolve()

            def edit_as_snapshot_is_backed_up(source, dest):
                if (
                    Path(source) == resolved_root / "upstream"
                    and Path(dest)
                    == resolved_root / ".upstream-update-backup" / "upstream"
                ):
                    (Path(source) / "version.txt").write_text(
                        "concurrent edit\n", encoding="utf-8"
                    )
                return real_replace(source, dest)

            with mock.patch("overlay._fetch_candidate_snapshot", side_effect=fake_fetch), mock.patch(
                "overlay.subprocess.run", side_effect=fake_run
            ), mock.patch("overlay.tempfile.mkdtemp", side_effect=lambda **_: (work.mkdir(), str(work))[1]), mock.patch(
                "overlay.os.replace", side_effect=edit_as_snapshot_is_backed_up
            ):
                with self.assertRaisesRegex(RuntimeError, "changed during publish"):
                    update(root, "candidate-ref")

            self.assertEqual(old_lock, (root / "upstream.lock").read_bytes())
            self.assertEqual(
                "concurrent edit\n",
                (root / "upstream" / "version.txt").read_text(encoding="utf-8"),
            )
            self.assertFalse((root / ".upstream-update-journal.json").exists())

    def test_changed_candidate_input_aborts_even_when_second_git_status_is_clean(self):
        with tempfile.TemporaryDirectory() as d:
            temp = Path(d)
            root = temp / "root"
            root.mkdir()
            make_update_root(root)
            old_lock = (root / "upstream.lock").read_bytes()
            calls = []
            fake_fetch, base_run = install_fake_fetch(root, calls)
            status_calls = 0

            def change_after_clean_status(args, **kwargs):
                nonlocal status_calls
                result = base_run(args, **kwargs)
                command = [str(value) for value in args]
                if command[:3] == ["git", "status", "--porcelain"]:
                    status_calls += 1
                    if status_calls == 2:
                        (root / ".dockerignore").write_text("changed\n", encoding="utf-8")
                return result

            work = temp / "candidate-work"
            with mock.patch("overlay._fetch_candidate_snapshot", side_effect=fake_fetch), mock.patch(
                "overlay.subprocess.run", side_effect=change_after_clean_status
            ), mock.patch("overlay.tempfile.mkdtemp", side_effect=lambda **_: (work.mkdir(), str(work))[1]):
                with self.assertRaisesRegex(RuntimeError, "candidate inputs changed"):
                    update(root, "candidate-ref")

            self.assertEqual(old_lock, (root / "upstream.lock").read_bytes())
            self.assertEqual("old\n", (root / "upstream" / "version.txt").read_text(encoding="utf-8"))
            self.assertEqual("changed\n", (root / ".dockerignore").read_text(encoding="utf-8"))

    def test_initial_dirty_check_stops_before_fetching(self):
        with tempfile.TemporaryDirectory() as d:
            root = Path(d)
            make_update_root(root)
            dirty = subprocess.CompletedProcess([], 0, b" M extensions/file.go\n", b"")
            with mock.patch("overlay.subprocess.run", return_value=dirty) as run, mock.patch(
                "overlay._fetch_candidate_snapshot"
            ) as fetch:
                with self.assertRaisesRegex(RuntimeError, "extensions/file.go"):
                    update(root, "candidate-ref")
                fetch.assert_not_called()
            self.assertEqual(
                [
                    "git", "status", "--porcelain", "--", "upstream",
                    "upstream.lock", "extensions", "patches", "deploy", "scripts",
                    "console", "docker-compose.yml", "docker-compose.build.yaml", "LICENSE", ".dockerignore",
                ],
                run.call_args.args[0],
            )
            self.assertEqual("old\n", (root / "upstream" / "version.txt").read_text(encoding="utf-8"))

    def test_publish_failure_rolls_back_both_snapshot_and_lock(self):
        with tempfile.TemporaryDirectory() as d:
            temp = Path(d)
            root = temp / "root"
            root.mkdir()
            make_update_root(root)
            old_lock = (root / "upstream.lock").read_bytes()
            calls = []
            fake_fetch, fake_run = install_fake_fetch(root, calls)
            work = temp / "candidate-work"
            real_replace = os.replace

            def fail_new_lock(source, dest):
                if Path(source).name == "upstream.lock" and Path(source).parent.name == ".upstream-update-new":
                    raise OSError("synthetic publish failure")
                return real_replace(source, dest)

            with mock.patch("overlay._fetch_candidate_snapshot", side_effect=fake_fetch), mock.patch(
                "overlay.subprocess.run", side_effect=fake_run
            ), mock.patch("overlay.tempfile.mkdtemp", side_effect=lambda **_: (work.mkdir(), str(work))[1]), mock.patch(
                "overlay.os.replace", side_effect=fail_new_lock
            ):
                with self.assertRaises(OSError):
                    update(root, "candidate-ref")
            self.assertEqual(old_lock, (root / "upstream.lock").read_bytes())
            self.assertEqual("old\n", (root / "upstream" / "version.txt").read_text(encoding="utf-8"))

    def test_success_changes_only_snapshot_and_lock_and_uses_candidate_entrypoints(self):
        with tempfile.TemporaryDirectory() as d:
            temp = Path(d)
            root = temp / "root"
            root.mkdir()
            make_update_root(root)
            old_lock = (root / "upstream.lock").read_bytes()
            custom_before = {
                path: source_digest(root / path)
                for path in ("extensions", "patches", "deploy", "scripts", "console")
            }
            compose_before = (root / "docker-compose.yml").read_bytes()
            dockerignore_before = (root / ".dockerignore").read_bytes()
            calls = []
            fake_fetch, fake_run = install_fake_fetch(root, calls)
            work = temp / "candidate-work"
            with mock.patch("overlay._fetch_candidate_snapshot", side_effect=fake_fetch), mock.patch(
                "overlay.subprocess.run", side_effect=fake_run
            ), mock.patch("overlay.tempfile.mkdtemp", side_effect=lambda **_: (work.mkdir(), str(work))[1]):
                update(root, "candidate-ref")

            lock = json.loads((root / "upstream.lock").read_text(encoding="utf-8"))
            self.assertEqual("2" * 40, lock["commit"])
            self.assertEqual("new\n", (root / "upstream" / "version.txt").read_text(encoding="utf-8"))
            self.assertEqual(lock["source_sha256"], source_digest(root / "upstream"))
            self.assertEqual(custom_before, {path: source_digest(root / path) for path in custom_before})
            self.assertEqual(compose_before, (root / "docker-compose.yml").read_bytes())
            self.assertEqual(dockerignore_before, (root / ".dockerignore").read_bytes())
            self.assertFalse((root / ".upstream-update-journal.json").exists())
            self.assertEqual(
                "old\n",
                (root / ".upstream-update-backup" / "upstream" / "version.txt").read_text(
                    encoding="utf-8"
                ),
            )
            self.assertEqual(
                old_lock,
                (root / ".upstream-update-backup" / "upstream.lock").read_bytes(),
            )
            self.assertFalse((root / ".upstream-update-new").exists())
            bash = [command for command in calls if command and command[0] == "bash"]
            self.assertEqual(2, len(bash))
            self.assertTrue(bash[0][1].endswith("/candidate/scripts/check.sh"))
            self.assertTrue(bash[1][1].endswith("/candidate/scripts/acceptance.sh"))
            self.assertEqual(bash[0][2], str((work / "candidate").resolve()))
            self.assertEqual(bash[1][2], str((work / "candidate").resolve()))

    def test_two_successful_updates_rotate_backup_to_the_previous_pair(self):
        with tempfile.TemporaryDirectory() as d:
            temp = Path(d)
            root = temp / "root"
            root.mkdir()
            make_update_root(root)
            calls = []
            _, fake_run = install_fake_fetch(root, calls)
            fetch_count = 0
            work_count = 0

            def varying_fetch(_repository, _ref, dest):
                nonlocal fetch_count
                fetch_count += 1
                dest.mkdir()
                (dest / "version.txt").write_text(f"new-{fetch_count}\n", encoding="utf-8")
                return str(fetch_count + 1) * 40

            def new_work(**_kwargs):
                nonlocal work_count
                work_count += 1
                work = temp / f"candidate-work-{work_count}"
                work.mkdir()
                return str(work)

            with mock.patch("overlay._fetch_candidate_snapshot", side_effect=varying_fetch), mock.patch(
                "overlay.subprocess.run", side_effect=fake_run
            ), mock.patch("overlay.tempfile.mkdtemp", side_effect=new_work):
                update(root, "first-ref")
                first_lock = (root / "upstream.lock").read_bytes()
                update(root, "second-ref")

            self.assertEqual("new-2\n", (root / "upstream" / "version.txt").read_text(encoding="utf-8"))
            self.assertEqual("3" * 40, json.loads((root / "upstream.lock").read_bytes())["commit"])
            backup = root / ".upstream-update-backup"
            self.assertEqual("new-1\n", (backup / "upstream" / "version.txt").read_text(encoding="utf-8"))
            self.assertEqual(first_lock, (backup / "upstream.lock").read_bytes())

    def test_publish_failure_after_prior_backup_restores_current_pair(self):
        with tempfile.TemporaryDirectory() as d:
            temp = Path(d)
            root = temp / "root"
            root.mkdir()
            make_update_root(root)
            calls = []
            _, fake_run = install_fake_fetch(root, calls)
            fetch_count = 0
            work_count = 0

            def varying_fetch(_repository, _ref, dest):
                nonlocal fetch_count
                fetch_count += 1
                dest.mkdir()
                (dest / "version.txt").write_text(f"new-{fetch_count}\n", encoding="utf-8")
                return str(fetch_count + 1) * 40

            def new_work(**_kwargs):
                nonlocal work_count
                work_count += 1
                work = temp / f"candidate-work-{work_count}"
                work.mkdir()
                return str(work)

            real_replace = os.replace
            with mock.patch("overlay._fetch_candidate_snapshot", side_effect=varying_fetch), mock.patch(
                "overlay.subprocess.run", side_effect=fake_run
            ), mock.patch("overlay.tempfile.mkdtemp", side_effect=new_work):
                update(root, "first-ref")
                current_lock = (root / "upstream.lock").read_bytes()
                current_digest = source_digest(root / "upstream")

                def fail_second_new_lock(source, dest):
                    if (
                        Path(source).name == "upstream.lock"
                        and Path(source).parent.name == ".upstream-update-new"
                    ):
                        raise OSError("synthetic second publish failure")
                    return real_replace(source, dest)

                with mock.patch("overlay.os.replace", side_effect=fail_second_new_lock):
                    with self.assertRaisesRegex(OSError, "second publish failure"):
                        update(root, "second-ref")

            self.assertEqual(current_lock, (root / "upstream.lock").read_bytes())
            self.assertEqual(current_digest, source_digest(root / "upstream"))
            self.assertEqual("new-1\n", (root / "upstream" / "version.txt").read_text(encoding="utf-8"))
            self.assertFalse((root / ".upstream-update-journal.json").exists())

    def test_journal_unlink_failure_keeps_new_pair_and_complete_old_backup(self):
        with tempfile.TemporaryDirectory() as d:
            temp = Path(d)
            root = temp / "root"
            root.mkdir()
            make_update_root(root)
            old_lock = (root / "upstream.lock").read_bytes()
            calls = []
            fake_fetch, fake_run = install_fake_fetch(root, calls)
            work = temp / "candidate-work"
            real_unlink = type(root).unlink
            resolved_root = root.resolve()

            def fail_committed_journal_unlink(path, *args, **kwargs):
                if path == resolved_root / ".upstream-update-journal.json":
                    raise OSError("synthetic journal unlink failure")
                return real_unlink(path, *args, **kwargs)

            with mock.patch("overlay._fetch_candidate_snapshot", side_effect=fake_fetch), mock.patch(
                "overlay.subprocess.run", side_effect=fake_run
            ), mock.patch("overlay.tempfile.mkdtemp", side_effect=lambda **_: (work.mkdir(), str(work))[1]), mock.patch(
                "pathlib.PosixPath.unlink", side_effect=fail_committed_journal_unlink, autospec=True
            ):
                with self.assertRaisesRegex(OSError, "journal unlink"):
                    update(root, "candidate-ref")

            self.assertEqual("new\n", (root / "upstream" / "version.txt").read_text(encoding="utf-8"))
            self.assertEqual(old_lock, (root / ".upstream-update-backup" / "upstream.lock").read_bytes())
            self.assertEqual(
                "old\n",
                (root / ".upstream-update-backup" / "upstream" / "version.txt").read_text(
                    encoding="utf-8"
                ),
            )
            journal = json.loads((root / ".upstream-update-journal.json").read_text(encoding="utf-8"))
            self.assertEqual("committed", journal["phase"])
            with mock.patch("overlay.subprocess.run") as run:
                with self.assertRaisesRegex(RuntimeError, "recovered interrupted"):
                    update(root, "candidate-ref")
                run.assert_not_called()
            self.assertEqual("new\n", (root / "upstream" / "version.txt").read_text(encoding="utf-8"))
            self.assertEqual(old_lock, (root / ".upstream-update-backup" / "upstream.lock").read_bytes())
            self.assertFalse((root / ".upstream-update-journal.json").exists())

    def test_prepare_refuses_unfinished_update_journal(self):
        with tempfile.TemporaryDirectory() as d:
            root = Path(d)
            make_overlay_root(root)
            (root / ".upstream-update-journal.json").write_text("{}\n", encoding="utf-8")
            with self.assertRaises(RuntimeError):
                materialize(root, root / "build")
            self.assertFalse((root / "build").exists())

    def test_next_update_restores_an_interrupted_pair_before_fetching(self):
        with tempfile.TemporaryDirectory() as d:
            root = Path(d)
            make_update_root(root)
            old_lock = (root / "upstream.lock").read_bytes()
            old_digest = source_digest(root / "upstream")
            backup = root / ".upstream-update-backup"
            backup.mkdir()
            os.replace(root / "upstream", backup / "upstream")
            os.replace(root / "upstream.lock", backup / "upstream.lock")
            (root / "upstream").mkdir()
            (root / "upstream" / "version.txt").write_text("partial-new\n", encoding="utf-8")
            new_digest = source_digest(root / "upstream")
            (root / ".upstream-update-journal.json").write_text(
                json.dumps({
                    "format": 1,
                    "phase": "upstream_installed",
                    "candidate": "/tmp/diagnostic-candidate",
                    "old_lock_sha256": hashlib.sha256(old_lock).hexdigest(),
                    "old_source_sha256": old_digest,
                    "new_lock_sha256": "2" * 64,
                    "new_source_sha256": new_digest,
                }),
                encoding="utf-8",
            )

            with mock.patch("overlay.subprocess.run") as run:
                with self.assertRaisesRegex(RuntimeError, "recovered interrupted"):
                    update(root, "candidate-ref")
                run.assert_not_called()
            self.assertEqual(old_lock, (root / "upstream.lock").read_bytes())
            self.assertEqual(old_digest, source_digest(root / "upstream"))
            self.assertFalse((root / ".upstream-update-journal.json").exists())
            self.assertFalse(backup.exists())

    def test_recovery_refuses_to_delete_concurrent_current_data(self):
        with tempfile.TemporaryDirectory() as d:
            root = Path(d)
            make_update_root(root)
            old_lock = (root / "upstream.lock").read_bytes()
            old_digest = source_digest(root / "upstream")
            backup = root / ".upstream-update-backup"
            backup.mkdir()
            os.replace(root / "upstream", backup / "upstream")
            os.replace(root / "upstream.lock", backup / "upstream.lock")
            (root / "upstream").mkdir()
            current = root / "upstream" / "version.txt"
            current.write_text("new\n", encoding="utf-8")
            expected_new_digest = source_digest(root / "upstream")
            current.write_text("concurrent edit after crash\n", encoding="utf-8")
            (root / ".upstream-update-journal.json").write_text(
                json.dumps({
                    "format": 1,
                    "phase": "upstream_installed",
                    "candidate": "/tmp/diagnostic-candidate",
                    "old_lock_sha256": hashlib.sha256(old_lock).hexdigest(),
                    "old_source_sha256": old_digest,
                    "new_lock_sha256": "2" * 64,
                    "new_source_sha256": expected_new_digest,
                }),
                encoding="utf-8",
            )

            with mock.patch("overlay.subprocess.run") as run:
                with self.assertRaisesRegex(RuntimeError, "current upstream changed"):
                    update(root, "candidate-ref")
                run.assert_not_called()
            self.assertEqual("concurrent edit after crash\n", current.read_text(encoding="utf-8"))
            self.assertEqual(old_lock, (backup / "upstream.lock").read_bytes())
            self.assertEqual("old\n", (backup / "upstream" / "version.txt").read_text(encoding="utf-8"))
            self.assertTrue((root / ".upstream-update-journal.json").exists())


class AcceptanceScriptTests(unittest.TestCase):
    def test_build_proxy_args_are_empty_by_default_and_use_only_explicit_environment(self):
        acceptance = Path(__file__).parent / "acceptance.sh"
        script = acceptance.read_text(encoding="utf-8")
        self.assertNotIn("http://host.docker.internal:7890", script)
        self.assertNotIn("WB2A_ACCEPTANCE_BUILD_PROXY", script)
        self.assertEqual(2, script.count('build_images "$fresh_project" "$fresh_port"'))
        subprocess.run(["bash", "-n", str(acceptance)], check=True)

        def first_build(proxy=None, build_proxy=None, host_proxy=""):
            with tempfile.TemporaryDirectory() as d:
                temp = Path(d)
                docker = temp / "docker"
                log = temp / "docker.log"
                client_env = temp / "client-env.log"
                docker.write_text(
                    "#!/bin/sh\n"
                    "printf '%s\\n' \"$*\" >> \"$DOCKER_LOG\"\n"
                    "case \" $* \" in *' build '*)\n"
                    "  printf '%s\\n' \"${http_proxy:-}\" \"${https_proxy:-}\" > \"$DOCKER_ENV_LOG\"\n"
                    "  exit 42;; esac\n"
                    "exit 0\n",
                    encoding="utf-8",
                )
                docker.chmod(0o755)
                env = os.environ.copy()
                env["PATH"] = str(temp) + os.pathsep + env["PATH"]
                env["DOCKER_LOG"] = str(log)
                env["DOCKER_ENV_LOG"] = str(client_env)
                env["http_proxy"] = env["https_proxy"] = host_proxy
                env.pop("HTTP_PROXY", None)
                env.pop("HTTPS_PROXY", None)
                env.pop("WB2A_BUILD_HTTP_PROXY", None)
                env.pop("WB2A_BUILD_HTTPS_PROXY", None)
                if proxy:
                    env["HTTP_PROXY"] = proxy
                    env["HTTPS_PROXY"] = proxy
                if build_proxy is not None:
                    env["WB2A_BUILD_HTTP_PROXY"] = build_proxy
                    env["WB2A_BUILD_HTTPS_PROXY"] = build_proxy
                result = subprocess.run(
                    ["bash", str(acceptance), str(acceptance.parent.parent)],
                    env=env,
                    stdout=subprocess.PIPE,
                    stderr=subprocess.PIPE,
                )
                lines = log.read_text(encoding="utf-8").splitlines() if log.exists() else []
                builds = [line for line in lines if "build" in line]
                self.assertTrue(builds, result.stderr.decode("utf-8", errors="replace"))
                self.assertEqual(client_env.read_text().splitlines(), [host_proxy, host_proxy])
                return builds[0]

        direct = first_build()
        self.assertNotIn("HTTP_PROXY=", direct)
        self.assertNotIn("HTTPS_PROXY=", direct)
        configured = first_build("http://host.docker.internal:7890")
        self.assertIn("--build-arg HTTP_PROXY=http://host.docker.internal:7890", configured)
        self.assertIn("--build-arg HTTPS_PROXY=http://host.docker.internal:7890", configured)
        dedicated = first_build("http://127.0.0.1:7890", "http://host.docker.internal:7890")
        self.assertIn("--build-arg HTTP_PROXY=http://host.docker.internal:7890", dedicated)
        self.assertIn("--build-arg HTTPS_PROXY=http://host.docker.internal:7890", dedicated)
        self.assertNotIn("127.0.0.1:7890", dedicated)
        disabled = first_build("http://127.0.0.1:7890", "")
        self.assertNotIn("HTTP_PROXY=", disabled)
        self.assertNotIn("HTTPS_PROXY=", disabled)
        first_build(build_proxy="http://host.docker.internal:7890", host_proxy="http://127.0.0.1:7890")


if __name__ == "__main__":
    unittest.main()
