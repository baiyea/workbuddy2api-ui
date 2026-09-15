#!/usr/bin/env python3
import argparse
import hashlib
import json
import os
import shutil
import stat
import subprocess
import sys
import tempfile
from pathlib import Path, PurePosixPath


CANONICAL_REPOSITORY = "https://github.com/Sliverkiss/workbuddy2api"
UPDATE_PATHS = (
    "upstream",
    "upstream.lock",
    "extensions",
    "patches",
    "deploy",
    "scripts",
    "console",
    "docker-compose.yml",
    ".dockerignore",
)
JOURNAL_NAME = ".upstream-update-journal.json"
BACKUP_NAME = ".upstream-update-backup"
STAGED_NAME = ".upstream-update-new"


def _tree_entries(root, ignore=None):
    if root.is_symlink() or not root.is_dir():
        raise ValueError(f"source is not a directory: {root}")
    root = root.resolve()
    entries = []

    def visit(directory):
        with os.scandir(directory) as children:
            items = list(children)
            ignored = set(ignore(str(directory), [item.name for item in items])) if ignore else set()
            for item in items:
                if item.name in ignored:
                    continue
                path = Path(item.path)
                relative = path.relative_to(root).as_posix()
                if item.name == ".git":
                    raise ValueError(f"nested .git is not allowed: {relative}")
                mode = item.stat(follow_symlinks=False).st_mode
                if stat.S_ISDIR(mode):
                    visit(path)
                elif stat.S_ISREG(mode):
                    git_mode = "100755" if mode & 0o111 else "100644"
                    entries.append((relative, git_mode, path.read_bytes(), path))
                elif stat.S_ISLNK(mode):
                    target = os.readlink(path)
                    resolved = (path.parent / target).resolve(strict=False)
                    if not resolved.is_relative_to(root):
                        raise ValueError(f"symlink escapes source: {relative}")
                    entries.append((relative, "120000", os.fsencode(target), path))
                else:
                    raise ValueError(f"unsupported file type: {relative}")

    visit(root)
    return sorted(entries, key=lambda entry: entry[0])


def source_digest(source: Path, ignore=None) -> str:
    records = [
        [relative, mode, hashlib.sha256(content).hexdigest()]
        for relative, mode, content, _ in _tree_entries(Path(source), ignore=ignore)
    ]
    payload = json.dumps(records, ensure_ascii=False, separators=(",", ":")).encode()
    return hashlib.sha256(payload).hexdigest()


def _run_git(repo, *args, input=None, env=None):
    return subprocess.run(
        ["git", *args],
        cwd=repo,
        input=input,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        env=env,
        check=True,
    ).stdout


def _relative_git_path(raw):
    value = raw.decode("utf-8", errors="surrogateescape")
    relative = PurePosixPath(value)
    if (
        relative.is_absolute()
        or not relative.parts
        or any(part in ("", ".", "..", ".git") for part in relative.parts)
    ):
        raise ValueError(f"unsafe Git path: {value!r}")
    return relative


def _remove_created_directory(path):
    if path.is_symlink():
        path.unlink()
    elif path.exists():
        shutil.rmtree(path)


def export_snapshot(repo: Path, commit: str, dest: Path) -> None:
    repo = Path(repo)
    dest = Path(dest)
    if os.path.lexists(dest):
        raise FileExistsError(dest)
    resolved_commit = _run_git(repo, "rev-parse", "--verify", f"{commit}^{{commit}}")
    resolved_commit = resolved_commit.decode().strip()
    listing = _run_git(repo, "ls-tree", "-r", "-z", "--full-tree", resolved_commit)
    dest.parent.mkdir(parents=True, exist_ok=True)
    dest.mkdir()
    try:
        for record in listing.split(b"\0"):
            if not record:
                continue
            metadata, raw_path = record.split(b"\t", 1)
            mode, object_type, object_id = metadata.decode().split()
            if object_type != "blob" or mode not in ("100644", "100755", "120000"):
                raise ValueError(f"unsupported Git entry: {metadata.decode()}")
            relative = _relative_git_path(raw_path)
            target = dest.joinpath(*relative.parts)
            target.parent.mkdir(parents=True, exist_ok=True)
            content = _run_git(repo, "cat-file", "blob", object_id)
            if mode == "120000":
                os.symlink(os.fsdecode(content), target)
            else:
                target.write_bytes(content)
                target.chmod(0o755 if mode == "100755" else 0o644)
        source_digest(dest)
    except Exception:
        _remove_created_directory(dest)
        raise


def _read_lock(root):
    try:
        lock = json.loads((root / "upstream.lock").read_text(encoding="utf-8"))
    except (OSError, UnicodeError, json.JSONDecodeError) as error:
        raise ValueError("invalid upstream.lock") from error
    if (
        not isinstance(lock, dict)
        or lock.get("format") != 1
        or not isinstance(lock.get("repository"), str)
        or not isinstance(lock.get("commit"), str)
        or len(lock["commit"]) != 40
        or any(character not in "0123456789abcdef" for character in lock["commit"])
        or not isinstance(lock.get("source_sha256"), str)
        or len(lock["source_sha256"]) != 64
        or any(character not in "0123456789abcdef" for character in lock["source_sha256"])
    ):
        raise ValueError("invalid upstream.lock")
    return lock


def _read_series(root):
    patches = root / "patches"
    series = patches / "series"
    if patches.is_symlink() or not patches.is_dir() or series.is_symlink():
        raise ValueError("invalid patches/series")
    patches = patches.resolve()
    try:
        lines = series.read_text(encoding="utf-8").splitlines()
    except (OSError, UnicodeError) as error:
        raise ValueError("invalid patches/series") from error
    result = []
    seen = set()
    for line in lines:
        name = line.strip()
        if not name or name.startswith("#"):
            continue
        relative = PurePosixPath(name)
        if (
            "\\" in name
            or relative.is_absolute()
            or any(part in ("", ".", "..") for part in relative.parts)
        ):
            raise ValueError(f"unsafe patch path: {name!r}")
        canonical = relative.as_posix()
        if canonical in seen:
            raise ValueError(f"duplicate patch: {canonical}")
        seen.add(canonical)
        patch = patches.joinpath(*relative.parts)
        current = patches
        for part in relative.parts[:-1]:
            current /= part
            if current.is_symlink():
                raise ValueError(f"patch parent is a symlink: {canonical}")
        if (
            patch.is_symlink()
            or not patch.is_file()
            or not patch.resolve().is_relative_to(patches)
        ):
            raise ValueError(f"patch is not a regular file: {name}")
        result.append(patch)
    return result


def _copy_extensions(root, dest):
    extensions = root / "extensions"
    if not extensions.exists():
        return
    for relative, mode, _, source in _tree_entries(extensions):
        target = dest.joinpath(*PurePosixPath(relative).parts)
        current = dest
        for part in PurePosixPath(relative).parts[:-1]:
            current /= part
            if current.is_symlink():
                raise ValueError(f"extension parent is a symlink: {relative}")
            if os.path.lexists(current) and not current.is_dir():
                raise FileExistsError(current)
            current.mkdir(exist_ok=True)
        if os.path.lexists(target):
            raise FileExistsError(target)
        if mode == "120000":
            os.symlink(os.readlink(source), target)
        else:
            shutil.copy2(source, target)


def overlay_identity(root: Path) -> str:
    root = Path(root).resolve()
    extensions = root / "extensions"
    extension_digest = (
        source_digest(extensions)
        if extensions.exists()
        else hashlib.sha256(b"[]").hexdigest()
    )
    patches = _read_series(root)
    digest = hashlib.sha256(extension_digest.encode("ascii"))
    digest.update((root / "patches" / "series").read_bytes())
    for patch in patches:
        digest.update(patch.read_bytes())
    return digest.hexdigest()


def materialize(root: Path, dest: Path) -> None:
    root = Path(root).resolve()
    if (root / JOURNAL_NAME).exists():
        raise RuntimeError("unfinished upstream update; run update again to recover")
    dest = Path(dest)
    resolved_dest = dest.resolve(strict=False)
    for name in ("upstream", "extensions", "patches"):
        input_tree = (root / name).resolve(strict=False)
        if resolved_dest == input_tree or resolved_dest.is_relative_to(input_tree):
            raise ValueError(f"output cannot be inside {name}: {dest}")
    if os.path.lexists(dest):
        raise FileExistsError(dest)
    lock = _read_lock(root)
    upstream = root / "upstream"
    if source_digest(upstream) != lock["source_sha256"]:
        raise ValueError("upstream source digest does not match upstream.lock")
    patches = _read_series(root)
    if (root / "extensions").exists():
        _tree_entries(root / "extensions")

    dest.parent.mkdir(parents=True, exist_ok=True)
    dest.mkdir()
    try:
        shutil.copytree(upstream, dest, symlinks=True, dirs_exist_ok=True)
        _copy_extensions(root, dest)
        apply_env = os.environ.copy()
        apply_env["GIT_CEILING_DIRECTORIES"] = str(dest.parent.resolve())
        for patch in patches:
            _run_git(dest, "apply", "--check", str(patch), env=apply_env)
            _run_git(dest, "apply", str(patch), env=apply_env)
    except Exception:
        _remove_created_directory(dest)
        raise


def _validate_ref(ref):
    if (
        not isinstance(ref, str)
        or not ref
        or ref.startswith("-")
        or any(ord(c) < 32 or ord(c) == 127 for c in ref)
    ):
        raise ValueError("ref must be non-empty and contain no leading '-' or control characters")


def _check_update_paths_clean(root):
    result = subprocess.run(
        ["git", "status", "--porcelain", "--", *UPDATE_PATHS],
        cwd=root,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        check=True,
    )
    if result.stdout:
        detail = result.stdout.decode("utf-8", errors="replace")
        raise RuntimeError("upstream update paths contain changes:\n" + detail)


def _fetch_candidate_snapshot(repository, ref, dest):
    fetch = dest.parent / "fetch"
    fetch.mkdir()
    try:
        _run_git(fetch, "init", "-q")
        _run_git(fetch, "remote", "add", "origin", repository)
        _run_git(fetch, "fetch", "--depth=1", "--no-tags", "origin", ref)
        commit = _run_git(
            fetch, "rev-parse", "--verify", "FETCH_HEAD^{commit}"
        ).decode().strip()
        export_snapshot(fetch, commit, dest)
        return commit
    finally:
        _remove_created_directory(fetch)


def _safe_candidate_ignore(_directory, names):
    blocked = {
        ".git", ".agents", ".build", "__pycache__", "auths", "data", ".env", ".DS_Store"
    }
    return [
        name
        for name in names
        if name in blocked
        or name.endswith(".pyc")
        or (name.endswith(".env") and name != "acceptance.env")
    ]


def _candidate_inputs_identity(root):
    records = []
    for name in ("extensions", "patches", "deploy", "scripts", "console"):
        records.append([name, source_digest(root / name, ignore=_safe_candidate_ignore)])
    for name in ("docker-compose.yml", ".dockerignore"):
        path = root / name
        if path.is_symlink() or not path.is_file():
            raise ValueError(f"invalid candidate source file: {name}")
        mode = "100755" if path.stat().st_mode & 0o111 else "100644"
        records.append([name, mode, hashlib.sha256(path.read_bytes()).hexdigest()])
    payload = json.dumps(records, separators=(",", ":")).encode()
    return hashlib.sha256(payload).hexdigest()


def _build_candidate(root, ref, candidate):
    candidate.mkdir()
    commit = _fetch_candidate_snapshot(CANONICAL_REPOSITORY, ref, candidate / "upstream")
    for name in ("extensions", "patches", "deploy", "scripts", "console"):
        source = root / name
        if not source.is_dir() or source.is_symlink():
            raise ValueError(f"invalid candidate source tree: {name}")
        shutil.copytree(
            source, candidate / name, symlinks=True, ignore=_safe_candidate_ignore
        )
        _tree_entries(candidate / name)
    for name in ("docker-compose.yml", ".dockerignore"):
        source = root / name
        if not source.is_file() or source.is_symlink():
            raise ValueError(f"invalid candidate source file: {name}")
        shutil.copy2(source, candidate / name)
    lock = {
        "format": 1,
        "repository": CANONICAL_REPOSITORY,
        "commit": commit,
        "source_sha256": source_digest(candidate / "upstream"),
    }
    (candidate / "upstream.lock").write_text(
        json.dumps(lock, indent=2) + "\n", encoding="utf-8"
    )


def _write_journal(root, journal):
    target = root / JOURNAL_NAME
    temporary = root / (JOURNAL_NAME + ".tmp")
    with temporary.open("x", encoding="utf-8") as stream:
        json.dump(journal, stream, indent=2)
        stream.write("\n")
        stream.flush()
        os.fsync(stream.fileno())
    os.replace(temporary, target)
    descriptor = os.open(root, os.O_RDONLY)
    try:
        os.fsync(descriptor)
    finally:
        os.close(descriptor)


def _restore_interrupted_update(root):
    journal_path = root / JOURNAL_NAME
    if not journal_path.exists():
        return False
    try:
        journal = json.loads(journal_path.read_text(encoding="utf-8"))
        phase = journal["phase"]
        old_lock_sha = journal["old_lock_sha256"]
        old_source_sha = journal["old_source_sha256"]
        new_lock_sha = journal["new_lock_sha256"]
        new_source_sha = journal["new_source_sha256"]
    except (OSError, KeyError, TypeError, json.JSONDecodeError) as error:
        raise RuntimeError("invalid upstream update journal; manual recovery required") from error
    backup = root / BACKUP_NAME
    staged = root / STAGED_NAME
    backup_upstream = backup / "upstream"
    backup_lock = backup / "upstream.lock"

    def lock_digest(path):
        return hashlib.sha256(path.read_bytes()).hexdigest()

    if phase == "committed":
        if (
            not (root / "upstream").is_dir()
            or not (root / "upstream.lock").is_file()
            or source_digest(root / "upstream") != new_source_sha
            or lock_digest(root / "upstream.lock") != new_lock_sha
            or not backup_upstream.is_dir()
            or not backup_lock.is_file()
            or source_digest(backup_upstream) != old_source_sha
            or lock_digest(backup_lock) != old_lock_sha
        ):
            raise RuntimeError("committed upstream update does not match journal; manual recovery required")
        _remove_created_directory(staged)
        journal_path.unlink()
        return True

    if backup_upstream.exists() and source_digest(backup_upstream) != old_source_sha:
        raise RuntimeError("upstream update backup changed; manual recovery required")
    if backup_lock.exists() and lock_digest(backup_lock) != old_lock_sha:
        raise RuntimeError("upstream update backup changed; manual recovery required")
    current_upstream = root / "upstream"
    current_lock = root / "upstream.lock"
    if backup_upstream.exists() and current_upstream.exists():
        if source_digest(current_upstream) != new_source_sha:
            raise RuntimeError("current upstream changed; manual recovery required")
    elif not backup_upstream.exists():
        if not current_upstream.is_dir() or source_digest(current_upstream) != old_source_sha:
            raise RuntimeError("upstream update recovery is incomplete; manual recovery required")
    if backup_lock.exists() and current_lock.exists():
        if lock_digest(current_lock) != new_lock_sha:
            raise RuntimeError("current upstream lock changed; manual recovery required")
    elif not backup_lock.exists():
        if not current_lock.is_file() or lock_digest(current_lock) != old_lock_sha:
            raise RuntimeError("upstream update recovery is incomplete; manual recovery required")
    if backup_upstream.exists():
        _remove_created_directory(current_upstream)
        os.replace(backup_upstream, current_upstream)
    if backup_lock.exists():
        current_lock.unlink(missing_ok=True)
        os.replace(backup_lock, current_lock)
    if not (root / "upstream").is_dir() or not (root / "upstream.lock").is_file():
        raise RuntimeError("upstream update recovery is incomplete; manual recovery required")
    lock_sha = hashlib.sha256((root / "upstream.lock").read_bytes()).hexdigest()
    if source_digest(root / "upstream") != old_source_sha or lock_sha != old_lock_sha:
        raise RuntimeError("recovered upstream combination does not match journal")
    _remove_created_directory(staged)
    _remove_created_directory(backup)
    journal_path.unlink()
    return True


def _discard_previous_backup(root):
    backup = root / BACKUP_NAME
    if not os.path.lexists(backup):
        return
    if backup.is_symlink() or not backup.is_dir():
        raise RuntimeError("invalid previous upstream backup")
    if {path.name for path in backup.iterdir()} != {"upstream", "upstream.lock"}:
        raise RuntimeError("invalid previous upstream backup")
    if (
        (backup / "upstream").is_symlink()
        or not (backup / "upstream").is_dir()
        or (backup / "upstream.lock").is_symlink()
        or not (backup / "upstream.lock").is_file()
    ):
        raise RuntimeError("invalid previous upstream backup")
    lock = _read_lock(backup)
    if (
        lock["repository"] != CANONICAL_REPOSITORY
        or source_digest(backup / "upstream") != lock["source_sha256"]
    ):
        raise RuntimeError("invalid previous upstream backup")
    _remove_created_directory(backup)


def _stage_candidate(root, candidate):
    staged = root / STAGED_NAME
    journal_path = root / JOURNAL_NAME
    journal_temporary = root / (JOURNAL_NAME + ".tmp")
    if any(
        os.path.lexists(path)
        for path in (staged, journal_path, journal_temporary)
    ):
        raise RuntimeError("upstream update staging path already exists")
    staged.mkdir()
    try:
        shutil.copytree(candidate / "upstream", staged / "upstream", symlinks=True)
        shutil.copy2(candidate / "upstream.lock", staged / "upstream.lock")
    except Exception:
        _remove_created_directory(staged)
        raise


def _install_staged_candidate(root, candidate, old_lock, old_source_sha):
    staged = root / STAGED_NAME
    backup = root / BACKUP_NAME
    journal_path = root / JOURNAL_NAME
    journal_temporary = root / (JOURNAL_NAME + ".tmp")
    new_lock_sha = hashlib.sha256((staged / "upstream.lock").read_bytes()).hexdigest()
    new_source_sha = source_digest(staged / "upstream")
    backup.mkdir()
    journal = {
        "format": 1,
        "phase": "staged",
        "candidate": str(candidate),
        "old_lock_sha256": hashlib.sha256(old_lock).hexdigest(),
        "old_source_sha256": old_source_sha,
        "new_lock_sha256": new_lock_sha,
        "new_source_sha256": new_source_sha,
    }
    try:
        _write_journal(root, journal)
        os.replace(root / "upstream", backup / "upstream")
        journal["phase"] = "upstream_backed_up"
        _write_journal(root, journal)
        os.replace(root / "upstream.lock", backup / "upstream.lock")
        journal["phase"] = "lock_backed_up"
        _write_journal(root, journal)
        if (
            source_digest(backup / "upstream") != old_source_sha
            or hashlib.sha256((backup / "upstream.lock").read_bytes()).digest()
            != hashlib.sha256(old_lock).digest()
        ):
            os.replace(backup / "upstream", root / "upstream")
            os.replace(backup / "upstream.lock", root / "upstream.lock")
            _remove_created_directory(staged)
            _remove_created_directory(backup)
            journal_path.unlink()
            raise RuntimeError("upstream snapshot or lock changed during publish")
        os.replace(staged / "upstream", root / "upstream")
        journal["phase"] = "upstream_installed"
        _write_journal(root, journal)
        os.replace(staged / "upstream.lock", root / "upstream.lock")
        journal["phase"] = "lock_installed"
        _write_journal(root, journal)
        if (
            source_digest(root / "upstream") != new_source_sha
            or hashlib.sha256((root / "upstream.lock").read_bytes()).hexdigest()
            != new_lock_sha
        ):
            raise RuntimeError("installed upstream combination does not match staged candidate")
        journal["phase"] = "committed"
        _write_journal(root, journal)
    except Exception:
        if journal_path.exists():
            try:
                phase = json.loads(journal_path.read_text(encoding="utf-8")).get("phase")
            except (OSError, json.JSONDecodeError):
                phase = None
            if phase != "committed":
                _restore_interrupted_update(root)
        else:
            _remove_created_directory(staged)
            _remove_created_directory(backup)
            journal_temporary.unlink(missing_ok=True)
        raise
    _remove_created_directory(staged)
    journal_path.unlink()


def update(root: Path, ref: str) -> None:
    root = Path(root).resolve()
    _validate_ref(ref)
    if _restore_interrupted_update(root):
        raise RuntimeError("recovered interrupted upstream update; rerun update")
    lock = _read_lock(root)
    if lock["repository"] != CANONICAL_REPOSITORY:
        raise ValueError("upstream.lock repository is not the approved canonical URL")
    _check_update_paths_clean(root)
    old_lock = (root / "upstream.lock").read_bytes()
    old_source_sha = source_digest(root / "upstream")
    old_inputs_identity = _candidate_inputs_identity(root)
    if old_source_sha != lock["source_sha256"]:
        raise ValueError("upstream source digest does not match upstream.lock")
    work = Path(tempfile.mkdtemp(prefix="wb2a-upstream-candidate-"))
    candidate = work / "candidate"
    try:
        _build_candidate(root, ref, candidate)
        candidate = candidate.resolve()
        if _candidate_inputs_identity(candidate) != old_inputs_identity:
            raise RuntimeError("candidate inputs do not match captured inputs")
        subprocess.run(
            ["bash", str(candidate / "scripts" / "check.sh"), str(candidate)],
            check=True,
        )
        subprocess.run(
            ["bash", str(candidate / "scripts" / "acceptance.sh"), str(candidate)],
            check=True,
        )
        if _candidate_inputs_identity(candidate) != old_inputs_identity:
            raise RuntimeError("candidate inputs do not match captured inputs")
        _stage_candidate(root, candidate)
        _check_update_paths_clean(root)
        if (
            (root / "upstream.lock").read_bytes() != old_lock
            or source_digest(root / "upstream") != old_source_sha
        ):
            raise RuntimeError("upstream snapshot or lock changed during candidate validation")
        if _candidate_inputs_identity(root) != old_inputs_identity:
            raise RuntimeError("candidate inputs changed during candidate validation")
        if _candidate_inputs_identity(candidate) != old_inputs_identity:
            raise RuntimeError("candidate inputs do not match captured inputs")
        _discard_previous_backup(root)
        _install_staged_candidate(root, candidate, old_lock, old_source_sha)
    except Exception:
        if not (root / JOURNAL_NAME).exists():
            _remove_created_directory(root / STAGED_NAME)
        print(f"candidate retained for diagnosis: {candidate}", file=sys.stderr)
        raise
    shutil.rmtree(work, ignore_errors=True)


def main():
    parser = argparse.ArgumentParser()
    subparsers = parser.add_subparsers(dest="command", required=True)
    prepare = subparsers.add_parser("prepare")
    prepare.add_argument("--output", type=Path, required=True)
    subparsers.add_parser("identity")
    update_parser = subparsers.add_parser("update")
    update_parser.add_argument("--ref", required=True)
    args = parser.parse_args()
    if args.command == "prepare":
        materialize(Path(__file__).resolve().parent.parent, args.output)
    elif args.command == "identity":
        print(overlay_identity(Path(__file__).resolve().parent.parent))
    else:
        update(Path(__file__).resolve().parent.parent, args.ref)


if __name__ == "__main__":
    main()
