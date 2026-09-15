#!/usr/bin/env python3
import argparse
import hashlib
import json
import os
import shutil
import stat
import subprocess
from pathlib import Path, PurePosixPath


def _tree_entries(root):
    if root.is_symlink() or not root.is_dir():
        raise ValueError(f"source is not a directory: {root}")
    root = root.resolve()
    entries = []

    def visit(directory):
        with os.scandir(directory) as children:
            for item in children:
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


def source_digest(source: Path) -> str:
    records = [
        [relative, mode, hashlib.sha256(content).hexdigest()]
        for relative, mode, content, _ in _tree_entries(Path(source))
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


def materialize(root: Path, dest: Path) -> None:
    root = Path(root).resolve()
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


def main():
    parser = argparse.ArgumentParser()
    subparsers = parser.add_subparsers(dest="command", required=True)
    prepare = subparsers.add_parser("prepare")
    prepare.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    if args.command == "prepare":
        materialize(Path(__file__).resolve().parent.parent, args.output)


if __name__ == "__main__":
    main()
