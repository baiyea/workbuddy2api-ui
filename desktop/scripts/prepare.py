#!/usr/bin/env python3
"""Build desktop resources; downloads happen only during this build command."""
import argparse
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import platform
import shutil
import subprocess
import sys
import tarfile
import tempfile
import urllib.parse
import urllib.request
import zipfile

ROOT = Path(__file__).resolve().parents[2]
TARGETS = {
    'aarch64-apple-darwin': ('darwin', 'arm64'),
    'x86_64-apple-darwin': ('darwin', 'amd64'),
    'x86_64-pc-windows-msvc': ('windows', 'amd64'),
}
SCRIPTS = ('task_common.py', 'task_runner.py', 'school_open_day_2026.py',
           'task_events.py', 'global_region.py')


def native_target():
    host = (platform.system(), platform.machine().lower())
    aliases = {('Darwin', 'arm64'): 'aarch64-apple-darwin',
               ('Darwin', 'x86_64'): 'x86_64-apple-darwin',
               ('Windows', 'amd64'): 'x86_64-pc-windows-msvc'}
    if host not in aliases:
        raise ValueError(f'Unsupported native host: {host}; specify --target')
    return aliases[host]


def target_config(target):
    if target not in TARGETS:
        raise ValueError(f'Unsupported desktop target: {target}')
    goos, goarch = TARGETS[target]
    suffix = '.exe' if goos == 'windows' else ''
    return {'target': target, 'goos': goos, 'goarch': goarch, 'core': 'core' + suffix,
            'console': 'console' + suffix,
            'python': 'python/python.exe' if suffix else 'python/bin/python3'}


def verify_digest(path, expected):
    with path.open('rb') as stream:
        actual = hashlib.file_digest(stream, 'sha256').hexdigest()
    if actual != expected:
        raise ValueError(f'SHA-256 mismatch for {path.name}: expected {expected}, got {actual}')


def cached_download(entry, cache):
    cache.mkdir(parents=True, exist_ok=True)
    name = urllib.parse.unquote(Path(urllib.parse.urlparse(entry['url']).path).name)
    path = cache / name
    if path.exists():
        verify_digest(path, entry['sha256'])
        return path
    print(f'Downloading locked build dependency: {name}', flush=True)
    with tempfile.TemporaryDirectory(prefix='download-', dir=cache) as tmp:
        pending = Path(tmp) / name
        with urllib.request.urlopen(entry['url'], timeout=120) as response, pending.open('wb') as stream:
            shutil.copyfileobj(response, stream)
        verify_digest(pending, entry['sha256'])
        pending.replace(path)
    return path


def extract_python(archive, digest, destination):
    verify_digest(archive, digest)
    # Python 3.12+ data_filter rejects outside links, devices and unsafe modes.
    # Also reject Windows separators and traversal on every build host.
    with tarfile.open(archive, 'r:gz') as source:
        for member in source.getmembers():
            name = PurePosixPath(member.name)
            if (name.is_absolute() or '..' in name.parts or '\\' in member.name
                    or ':' in member.name or not name.parts or name.parts[0] != 'python'):
                raise ValueError(f'Unsafe archive path: {member.name}')
            if '\\' in member.linkname or ':' in member.linkname:
                raise ValueError(f'Unsafe archive link: {member.linkname}')
        source.extractall(destination, filter='data')


def copy_scripts(materialized, destination):
    scripts = destination / 'scripts'
    scripts.mkdir()
    for name in SCRIPTS:
        shutil.copy2(materialized / 'scripts' / name, scripts / name)


def write_runtime_metadata(stage, config, lock):
    (stage / 'config.json').write_text('{}\n', encoding='utf-8')
    manifest = {'target': config['target'], 'python_version': lock['python_version']}
    (stage / 'runtime-manifest.json').write_text(json.dumps(manifest, indent=2) + '\n', encoding='utf-8')


def publish(stage, output):
    # Keep the previous complete tree until all compilation and checks succeed.
    # Rename rollback covers ordinary failures; retain backup on process crash.
    backup = output.with_name(output.name + '.previous')
    if backup.exists():
        raise FileExistsError(f'Preserved earlier runtime at {backup}; inspect before retrying')
    if output.exists():
        output.rename(backup)
    try:
        stage.rename(output)
    except BaseException:
        if backup.exists():
            backup.rename(output)
        raise
    if backup.exists():
        shutil.rmtree(backup)


def python_environment(stage):
    env = {key: value for key, value in os.environ.items()
           if not key.upper().startswith(('PYTHON', 'WB2A_'))
           and key.upper() not in ('SSL_CERT_FILE', 'SSL_CERT_DIR')}
    env.update(PYTHONDONTWRITEBYTECODE='1', PYTHONNOUSERSITE='1',
               SSL_CERT_FILE=str(stage / 'python/cacert.pem'))
    return env


def smoke_python(stage, config):
    # Imports only: no account files and no upstream requests.
    code = (
        'import ssl, hashlib, urllib.request, sys; '
        'assert ssl.create_default_context().cert_store_stats()["x509_ca"] > 0; '
        'assert hashlib.sha256(b"desktop").hexdigest(); '
        'sys.path.insert(0, sys.argv[1]); '
        'import task_common, task_events, school_open_day_2026, task_runner, global_region; '
        'print("Bundled Python stdlib, CA certificates and task imports: OK")'
    )
    subprocess.run([str(stage / config['python']), '-s', '-B', '-c', code,
                    str(stage / 'scripts')], cwd=stage, env=python_environment(stage), check=True)


def validate_macos_runtime(stage, minimum):
    def version(value):
        return (tuple(int(part) for part in value.split('.')) + (0, 0))[:3]

    # Check executables AND shared libraries, including Python extension modules.
    magic = {bytes.fromhex(value) for value in (
        'feedface', 'cefaedfe', 'feedfacf', 'cffaedfe',
        'cafebabe', 'bebafeca', 'cafebabf', 'bfbafeca')}
    files = sorted({path.resolve() for path in stage.rglob('*') if path.is_file()})
    for path in files:
        with path.open('rb') as stream:
            if stream.read(4) not in magic:
                continue
        output = subprocess.check_output(['otool', '-l', str(path)], text=True)
        command, versions = None, []
        for line in output.splitlines():
            fields = line.split()
            if len(fields) >= 2 and fields[0] == 'cmd':
                command = fields[1]
            elif (len(fields) >= 2 and (command, fields[0]) in (
                    ('LC_BUILD_VERSION', 'minos'), ('LC_VERSION_MIN_MACOSX', 'version'))):
                versions.append(fields[1])
        if not versions:
            raise ValueError(f'Missing macOS deployment target: {path}')
        for actual in versions:
            if version(actual) > version(minimum):
                raise ValueError(f'{path.name} requires macOS {actual}, above declared minimum {minimum}')


def prepare(target, cache, output):
    config = target_config(target)
    lock = json.loads((ROOT / 'desktop/python-runtime.lock.json').read_text())
    entry = lock['targets'][target]
    archive = cached_download(entry, cache)
    certifi = cached_download(lock['certifi'], cache)
    notices = cached_download(lock['licenses'], cache)
    output.parent.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix='.prepare-', dir=output.parent) as temporary:
        working = Path(temporary)
        stage = working / 'runtime'
        extract_python(archive, entry['sha256'], stage)
        if not (stage / config['python']).is_file():
            raise ValueError(f'Python archive missing interpreter: {config["python"]}')
        licenses = stage / 'licenses'
        licenses.mkdir()
        shutil.copy2(ROOT / 'LICENSE', licenses / 'workbuddy2api-LICENSE')
        shutil.copy2(ROOT / 'upstream.lock', licenses / 'upstream.lock')
        shutil.copy2(ROOT / 'desktop/python-runtime.lock.json', licenses / 'python-runtime.lock.json')
        with tarfile.open(notices, 'r:gz') as source:
            for member in source.getmembers():
                path = PurePosixPath(member.name)
                if (member.isfile() and len(path.parts) == 2
                        and path.parts[0] == lock['licenses']['root']
                        and (path.name.startswith('LICENSE') or path.name == 'python-licenses.rst')):
                    (licenses / ('python-build-standalone-' + path.name)).write_bytes(source.extractfile(member).read())
        # Read only named wheel entries, never extract arbitrary wheel paths.
        with zipfile.ZipFile(certifi) as wheel:
            (stage / 'python/cacert.pem').write_bytes(wheel.read('certifi/cacert.pem'))
            license_name = f'certifi-{lock["certifi"]["version"]}.dist-info/licenses/LICENSE'
            (licenses / 'certifi-LICENSE').write_bytes(wheel.read(license_name))
        materialized = working / 'core-source'
        subprocess.run([sys.executable, str(ROOT / 'scripts/overlay.py'), 'prepare',
                        '--output', str(materialized)], cwd=ROOT, check=True)
        copy_scripts(materialized, stage)
        commit = json.loads((ROOT / 'upstream.lock').read_text())['commit']
        identity = subprocess.check_output([sys.executable, str(ROOT / 'scripts/overlay.py'),
                                            'identity'], cwd=ROOT, text=True).strip()
        env = dict(os.environ, CGO_ENABLED='0', GOOS=config['goos'], GOARCH=config['goarch'])
        for source, binary, flags in [
            (materialized, config['core'], f'-s -w -X main.upstreamCommit={commit} -X main.patchIdentity={identity}'),
            (ROOT / 'console', config['console'], '-s -w'),
        ]:
            subprocess.run(['go', 'build', '-trimpath', f'-ldflags={flags}', '-o',
                            str(stage / binary), './cmd/server' if source == materialized else '.'],
                           cwd=source, env=env, check=True)
        if config['goos'] == 'darwin':
            minimum = json.loads((ROOT / 'desktop/tauri.conf.json').read_text())['bundle']['macOS']['minimumSystemVersion']
            validate_macos_runtime(stage, minimum)
        try:
            can_run = target == native_target()
        except ValueError:
            can_run = False
        if can_run:
            smoke_python(stage, config)
        else:
            print(f'Cross-target resources prepared; Python execution not verified on {target}', flush=True)
        write_runtime_metadata(stage, config, lock)
        publish(stage, output)
    print(f'Prepared {target}: {output}', flush=True)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--target', choices=TARGETS)
    parser.add_argument('--cache-dir', type=Path, default=ROOT / '.build/desktop-cache')
    parser.add_argument('--output', type=Path, default=ROOT / 'desktop/resources/runtime')
    args = parser.parse_args()
    if sys.version_info < (3, 12):
        parser.error('Build preparation requires Python 3.12 or newer')
    prepare(args.target or native_target(), args.cache_dir.resolve(), args.output.resolve())


if __name__ == '__main__':
    main()
