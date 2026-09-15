#!/usr/bin/env python3
"""Manually publish the two tested linux/amd64 images to baiyea on Docker Hub."""
import argparse
import json
import os
from pathlib import Path
import re
import subprocess

from overlay import UPDATE_PATHS


def run(args, root, capture=False, env=None):
    result = subprocess.run(args, cwd=root, check=True, text=True,
                            capture_output=capture, env=env)
    return result.stdout.strip() if capture else None


def public_tag_status(root, name, version):
    return run(["curl", "--silent", "--show-error", "--max-time", "30",
                "--output", "/dev/null", "--write-out", "%{http_code}",
                f"https://hub.docker.com/v2/repositories/baiyea/workbuddy2api-{name}/tags/{version}/"], root, True)


def require_unused_tags(root, version):
    for name in ("core", "console"):
        status = public_tag_status(root, name, version)
        if status == "200":
            raise RuntimeError(f"{name}:{version} already exists; choose a new version")
        if status != "404":
            raise RuntimeError(f"cannot verify unused {name} tag: HTTP {status}")


def release(root, version):
    if not re.fullmatch(r"[0-9]+\.[0-9]+\.[0-9]+", version):
        raise ValueError("version must be a fixed X.Y.Z tag, not latest")
    revision = run(["git", "rev-parse", "HEAD"], root, True)
    if run(["git", "status", "--porcelain", "--", *UPDATE_PATHS], root, True):
        raise RuntimeError("commit source/build inputs before publishing")
    images = [f"baiyea/workbuddy2api-{name}:{version}" for name in ("core", "console")]
    require_unused_tags(root, version)
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
            if os.environ.get(key):
                args.extend(["--build-arg", f"{key}={os.environ[key]}"])
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
    require_unused_tags(root, version)
    # Hub immutable tags must enforce the remaining check/push race (see README).
    # No auto-delete on a partial push: use a new version after diagnosis.
    for image in images:
        run(["docker", "push", image], root)
    for name in ("core", "console"):
        if public_tag_status(root, name, version) != "200":
            raise RuntimeError("push finished but public access is unverified; check Docker Hub visibility")
    print("Published tested linux/amd64 images: " + ", ".join(images))
    print(f"Deploy with WB2A_VERSION={version} docker compose up -d (no source/build needed).")


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("version", help="new, fixed X.Y.Z tag; docker login -u baiyea first")
    args = parser.parse_args()
    try:
        release(Path(__file__).resolve().parent.parent, args.version)
    except (ValueError, RuntimeError, subprocess.CalledProcessError) as error:
        parser.exit(1, f"Release stopped: {error}\nNo existing tags or running deployments are removed.\n")
