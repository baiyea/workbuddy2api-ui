#!/usr/bin/env python3
import argparse
import hashlib
import json
import os
import re
import subprocess
import sys
import tarfile
from pathlib import Path


REQUIRED_DESTINATIONS = ("/app/auths", "/app/data")
VOLUME_NAME = re.compile(r"^[A-Za-z0-9][A-Za-z0-9_.-]*$")


def inspect_container(name):
    if not name:
        raise ValueError("container name is required")
    result = subprocess.run(
        ["docker", "inspect", name], capture_output=True, text=True, check=False
    )
    if result.returncode:
        raise ValueError("docker inspect failed")
    try:
        rows = json.loads(result.stdout)
    except json.JSONDecodeError as error:
        raise ValueError("docker inspect returned invalid JSON") from error
    if len(rows) != 1 or not isinstance(rows[0], dict):
        raise ValueError("docker inspect did not identify exactly one container")
    row = rows[0]
    selected = []
    for destination in REQUIRED_DESTINATIONS:
        matches = [mount for mount in row.get("Mounts", []) if mount.get("Destination") == destination]
        if len(matches) != 1:
            raise ValueError(f"expected exactly one {destination} mount")
        mount = matches[0]
        if mount.get("Type") not in ("bind", "volume") or not mount.get("Source"):
            raise ValueError(f"unsupported {destination} mount")
        if mount["Type"] == "volume" and not mount.get("Name"):
            raise ValueError(f"unnamed {destination} volume")
        selected.append(
            {
                "Name": mount.get("Name", ""),
                "Source": mount["Source"],
                "Destination": destination,
                "Type": mount["Type"],
            }
        )
    return {
        "container": {
            "Id": row.get("Id", ""),
            "Name": str(row.get("Name", "")).removeprefix("/"),
            "Image": row.get("Image", ""),
            "Running": bool(row.get("State", {}).get("Running")),
        },
        "mounts": selected,
    }


def _validated_mounts(manifest):
    if not isinstance(manifest, dict) or not isinstance(manifest.get("container"), dict):
        raise ValueError("invalid migration manifest")
    mounts = manifest.get("mounts")
    if not isinstance(mounts, list):
        raise ValueError("invalid migration manifest")
    selected = []
    for destination in REQUIRED_DESTINATIONS:
        matches = [mount for mount in mounts if isinstance(mount, dict) and mount.get("Destination") == destination]
        if len(matches) != 1:
            raise ValueError(f"expected exactly one {destination} mount")
        mount = matches[0]
        if mount.get("Type") == "bind":
            source = Path(str(mount.get("Source", "")))
            if not source.is_absolute() or source.is_symlink() or not source.is_dir():
                raise ValueError(f"invalid bind source for {destination}")
        elif mount.get("Type") == "volume":
            if not VOLUME_NAME.fullmatch(str(mount.get("Name", ""))):
                raise ValueError(f"invalid volume name for {destination}")
        else:
            raise ValueError(f"unsupported mount type for {destination}")
        selected.append(mount)
    return selected


def _archive_bind(source, archive):
    with tarfile.open(archive, "x:gz") as bundle:
        for child in sorted(Path(source).iterdir(), key=lambda path: path.name):
            bundle.add(child, arcname=child.name, recursive=True)


def _archive_volume(name, archive, output):
    result = subprocess.run(
        [
            "docker",
            "run",
            "--rm",
            "--network",
            "none",
            "--mount",
            f"type=volume,src={name},dst=/source,readonly",
            "--mount",
            f"type=bind,src={output.resolve()},dst=/backup",
            "alpine:3.20",
            "sh",
            "-eu",
            "-c",
            'tar -C /source -czf "$1" .; chown "$2:$3" "$1"; chmod 600 "$1"',
            "backup",
            f"/backup/{archive.name}",
            str(os.getuid()),
            str(os.getgid()),
        ],
        capture_output=True,
        text=True,
        check=False,
    )
    if result.returncode:
        raise ValueError("volume backup failed")


def _write_json(path, value):
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(fd, "w", encoding="utf-8") as stream:
        json.dump(value, stream, ensure_ascii=False, indent=2)
        stream.write("\n")
        stream.flush()
        os.fsync(stream.fileno())


def backup(manifest_path, output):
    manifest_path, output = Path(manifest_path), Path(output)
    try:
        manifest = json.loads(manifest_path.read_text(encoding="utf-8"))
    except (OSError, UnicodeError, json.JSONDecodeError) as error:
        raise ValueError("invalid migration manifest") from error
    mounts = _validated_mounts(manifest)
    output.mkdir(mode=0o700)
    output.chmod(0o700)
    archives = []
    for mount in mounts:
        label = mount["Destination"].removeprefix("/app/")
        archive = output / f"{label}.tar.gz"
        if mount["Type"] == "bind":
            _archive_bind(mount["Source"], archive)
        else:
            _archive_volume(mount["Name"], archive, output)
        archive.chmod(0o600)
        raw = archive.read_bytes()
        archives.append(
            {
                "destination": mount["Destination"],
                "file": archive.name,
                "sha256": hashlib.sha256(raw).hexdigest(),
                "size": len(raw),
            }
        )
    running = bool(manifest["container"].get("Running"))
    result = {
        "container": manifest["container"],
        "consistent": not running,
        "warning": "运行中备份不构成最终一致性备份；切换前停止旧实例后重新备份。" if running else "",
        "archives": archives,
    }
    _write_json(output / "backup-manifest.json", result)
    return result


def main(argv=None):
    parser = argparse.ArgumentParser()
    commands = parser.add_subparsers(dest="command", required=True)
    inspect_parser = commands.add_parser("inspect")
    inspect_parser.add_argument("--container", required=True)
    backup_parser = commands.add_parser("backup")
    backup_parser.add_argument("--manifest", type=Path, required=True)
    backup_parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args(argv)
    try:
        result = inspect_container(args.container) if args.command == "inspect" else backup(args.manifest, args.output)
    except (OSError, ValueError) as error:
        print(str(error), file=sys.stderr)
        return 1
    print(json.dumps(result, ensure_ascii=False, indent=2))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
