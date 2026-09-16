#!/usr/bin/env python3
"""Build, test and publish an Aliyun linux/amd64 image pair with one timestamp."""
import argparse
import json
import os
from pathlib import Path
import re
import subprocess
import tempfile
import time

from overlay import UPDATE_PATHS

REPOSITORY = "registry.cn-hangzhou.aliyuncs.com/cateyes/go"

def run(args, root, capture=False, env=None):
    result = subprocess.run(args, cwd=root, check=True, text=True,
                            capture_output=capture, env=env)
    return result.stdout.strip() if capture else None


def require_unused_tags(root, images):
    for image in images:
        result = subprocess.run(["docker", "manifest", "inspect", image], cwd=root,
                                capture_output=True, text=True, timeout=60)
        if result.returncode == 0:
            raise RuntimeError(f"{image} already exists; use a new timestamp")
        error = result.stderr.lower()
        if not any(message in error for message in ("manifest unknown", "no such manifest:")):
            raise RuntimeError(f"cannot verify unused tag {image}: {result.stderr.strip()}")


def release(root, version=None):
    version = str(int(time.time())) if version is None else version
    if not re.fullmatch(r"[1-9][0-9]{9}", version):
        raise ValueError("timestamp must be 10 digits (Unix seconds)")
    compose = root / "docker-compose.yml"
    original = compose.read_text()
    updated, count = re.subn(r"\$\{WB2A_VERSION:-[1-9][0-9]{9}\}",
                            "${WB2A_VERSION:-" + version + "}", original)
    if count != 2:
        raise RuntimeError("Compose must contain exactly two timestamp defaults")
    revision = run(["git", "rev-parse", "HEAD"], root, True)
    if run(["git", "status", "--porcelain", "--", *UPDATE_PATHS], root, True):
        raise RuntimeError("commit source/build inputs before publishing")
    images = [f"{REPOSITORY}:wb2api-{name}-{version}" for name in ("core", "webui")]
    print(f"Release timestamp: {version}", flush=True)
    require_unused_tags(root, images)
    run(["python3", "-m", "unittest", "discover", "-s", "scripts", "-p", "test_*.py"], root)
    run(["python3", "-m", "unittest", "discover", "-s", "deploy", "-p", "test_*.py"], root)
    run(["bash", str(root / "scripts/check.sh")], root)
    for name, image in zip(("core", "console"), images):
        args = ["docker", "buildx", "build", "--platform", "linux/amd64", "--load", "--pull",
                "--tag", image, "--file", f"deploy/{name}.Dockerfile",
                "--label", f"org.opencontainers.image.version={version}",
                "--label", f"org.opencontainers.image.revision={revision}",
                "--label", "org.opencontainers.image.licenses=MIT"]
        for key in ("HTTP_PROXY", "HTTPS_PROXY"):
            proxy = os.environ.get(f"WB2A_BUILD_{key}", os.environ.get(key))
            if proxy:
                args.extend(["--build-arg", f"{key}={proxy}"])
        run([*args, "."], root)
    metadata = json.loads(run(["docker", "image", "inspect", *images], root, True))
    if len(metadata) != 2 or any(i["Os"] != "linux" or i["Architecture"] != "amd64" for i in metadata):
        raise RuntimeError("release images must both be linux/amd64")
    env = {k: v for k, v in os.environ.items() if not k.startswith("WB2A_ACCEPTANCE_")}
    env.update(WB2A_ACCEPTANCE_SKIP_BUILD="true", WB2A_ACCEPTANCE_KEEP="false",
               WB2A_ACCEPTANCE_CORE_IMAGE=images[0], WB2A_ACCEPTANCE_CONSOLE_IMAGE=images[1])
    run(["bash", str(root / "scripts/acceptance.sh")], root, env=env)
    if run(["git", "rev-parse", "HEAD"], root, True) != revision or run(
            ["git", "status", "--porcelain", "--", *UPDATE_PATHS], root, True):
        raise RuntimeError("source changed during release; nothing pushed")
    require_unused_tags(root, images)
    # Serialize publishers; registry-side immutability is needed for a hard race guarantee.
    # No auto-delete on a partial push: use a new version after diagnosis.
    for image in images:
        run(["docker", "push", image], root)
    for image in images:
        run(["docker", "pull", "--platform", "linux/amd64", image], root)
    pulled = json.loads(run(["docker", "image", "inspect", *images], root, True))
    if [i["Id"] for i in pulled] != [i["Id"] for i in metadata]:
        raise RuntimeError("pulled images differ from accepted images; Compose unchanged")
    if compose.read_text() != original:
        raise RuntimeError("Compose changed during release; published images retained, Compose unchanged")
    temporary = None
    try:
        with tempfile.NamedTemporaryFile(mode="w", dir=root, prefix=".compose-release-", delete=False) as output:
            temporary = Path(output.name)
            os.fchmod(output.fileno(), compose.stat().st_mode & 0o777)
            output.write(updated)
        temporary.replace(compose)
    finally:
        if temporary is not None:
            temporary.unlink(missing_ok=True)
    print("Published tested linux/amd64 images: " + ", ".join(images))
    print("Updated docker-compose.yml; review and commit it. No running deployment was changed.")
    print(f"Deploy with WB2A_VERSION={version} docker compose up -d (no source/build needed).")


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("timestamp", nargs="?", help="Unix seconds; defaults to current time. Log in to Aliyun first.")
    args = parser.parse_args()
    try:
        release(Path(__file__).resolve().parent.parent, args.timestamp)
    except (ValueError, RuntimeError, OSError, subprocess.SubprocessError) as error:
        parser.exit(1, f"Release stopped: {error}\nNo existing tags or running deployments are removed.\n")
