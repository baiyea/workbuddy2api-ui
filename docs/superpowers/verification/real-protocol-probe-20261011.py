"""One-off real-model probe. Credentials and runtime logs stay under ignored .build."""
from pathlib import Path
import datetime as dt
import hashlib
import json
import os
import shutil
import subprocess
import sys
import time
import urllib.request

ROOT = Path(__file__).resolve().parents[3]
CONTEXT = ROOT / ".build/real-protocol-probe-context.json"
EVIDENCE = Path(__file__).with_suffix(".json")


def docker(*args):
    return subprocess.check_output(["docker", *args], stderr=subprocess.STDOUT, text=True).strip()


def setup():
    if CONTEXT.exists():
        raise RuntimeError("Existing probe context: inspect before creating another instance")
    stamp = str(int(time.time()))
    work = ROOT / ".build" / ("real-protocol-probe-" + stamp)
    source = Path(os.environ["LOCALAPPDATA"]) / "com.workbuddy2api.desktop/auths"
    files = list(source.glob("*.json"))
    if not files:
        raise RuntimeError("No logged-in desktop account found")
    for part in ("auths", "data", "keys"):
        (work / part).mkdir(parents=True)
    originals = {}
    realms = []
    for file in files:
        originals[str(file)] = hashlib.sha256(file.read_bytes()).hexdigest()
        shutil.copyfile(file, work / "auths" / file.name)
        realms.append(json.loads(file.read_text(encoding="utf-8-sig")).get("auth", {}).get("realm", "cn"))
    config = {"schedule": {name + "_enabled": False for name in
              ("checkin", "travel", "activity", "keepalive", "school", "cat")},
              "pool": {"max_in_flight": 1},
              "upstream": {"timeout_seconds": 45, "header_timeout_seconds": 90, "idle_timeout_seconds": 90}}
    (work / "config.json").write_text(json.dumps(config), encoding="utf-8")
    core = "sha256:5fe4573e7a38fab434b3ec6628145aba6c9c20e1ff05b72614c246baa7b04d5e"
    console = "sha256:a2919dceb8330b5cf3f66ae71a97d8724c0414878434aa7ab282fee9fa52800b"
    compose = {"services": {
        "core": {"image": core, "platform": "linux/amd64", "environment": {"TZ": "Asia/Shanghai"},
                 "volumes": [f"{(work / 'auths').as_posix()}:/app/auths", f"{(work / 'data').as_posix()}:/app/data",
                             f"{(work / 'keys').as_posix()}:/run/wb2a", f"{(work / 'config.json').as_posix()}:/app/config.json:ro"]},
        "console": {"image": console, "platform": "linux/amd64", "environment": {"TZ": "Asia/Shanghai"},
                    "ports": ["127.0.0.1::7863"], "volumes": [f"{(work / 'keys').as_posix()}:/run/wb2a:ro"],
                    "depends_on": ["core"]}}}
    cp = work / "compose.json"
    cp.write_text(json.dumps(compose), encoding="utf-8")
    state = {"project": "wb2a-real-probe-" + stamp, "compose": str(cp), "work": str(work),
             "originals": originals, "realms": realms, "core_image": core, "console_image": console}
    CONTEXT.write_text(json.dumps(state), encoding="utf-8")
    docker("compose", "-p", state["project"], "-f", str(cp), "up", "-d", "--pull", "never")
    address = docker("compose", "-p", state["project"], "-f", str(cp), "port", "console", "7863")
    state["base_url"] = "http://" + address
    CONTEXT.write_text(json.dumps(state), encoding="utf-8")
    for _ in range(60):
        try:
            with urllib.request.urlopen(state["base_url"] + "/livez", timeout=2) as response:
                if response.status == 200:
                    break
        except Exception:
            time.sleep(1)
    else:
        raise RuntimeError("Probe startup failed; runtime retained for diagnosis")
    keys = json.loads((work / "keys/keys.json").read_text())
    request = urllib.request.Request(state["base_url"] + "/v1/models", headers={"Authorization": "Bearer " + keys["api_key"]})
    catalog = json.load(urllib.request.urlopen(request, timeout=60))["data"]
    state["catalog"] = catalog
    CONTEXT.write_text(json.dumps(state), encoding="utf-8")
    print(json.dumps({"base_url": state["base_url"], "account_count": len(files), "realms": realms,
                      "models": [m["id"] for m in catalog if m["id"].split(":")[0] in realms]}, ensure_ascii=False), flush=True)


def cleanup():
    state = json.loads(CONTEXT.read_text())
    work = Path(state["work"])
    output = docker("compose", "-p", state["project"], "-f", state["compose"], "logs", "--no-color")
    (work / "runtime.log").write_text(output, encoding="utf-8")
    docker("compose", "-p", state["project"], "-f", state["compose"], "down", "--remove-orphans")
    unchanged = all(Path(p).is_file() and hashlib.sha256(Path(p).read_bytes()).hexdigest() == digest
                    for p, digest in state["originals"].items())
    state["stopped"] = True
    state["original_credentials_unchanged"] = unchanged
    CONTEXT.write_text(json.dumps(state), encoding="utf-8")
    print(json.dumps({"stopped": True, "original_credentials_unchanged": unchanged}))


if __name__ == "__main__":
    {"setup": setup, "cleanup": cleanup}[sys.argv[1]]()
