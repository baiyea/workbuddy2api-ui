#!/usr/bin/env python3
"""Set desktop build versions from a release tag, without committing any files."""
import argparse
import json
from pathlib import Path
import re
import tomllib


def set_version(desktop, tag):
    number = r'(0|[1-9][0-9]*)'
    match = re.fullmatch(rf'v{number}\.{number}\.{number}(?:-(?:alpha|beta|rc)\.{number})?', tag)
    if not match or any(int(value) > 65535 for value in match.groups() if value is not None):
        raise ValueError('Expected vX.Y.Z or vX.Y.Z-{alpha|beta|rc}.N; numbers must be 0..65535')
    version = tag[1:]
    changes = {}
    for name in ('tauri.conf.json', 'package.json', 'package-lock.json'):
        path = desktop / name
        value = json.loads(path.read_text(encoding='utf-8'))
        value['version'] = version
        if name == 'package-lock.json':
            value['packages']['']['version'] = version
        changes[path] = json.dumps(value, indent=2, ensure_ascii=False) + '\n'
    for name, header in (
        ('Cargo.toml', r'\[package\]'),
        ('Cargo.lock', r'\[\[package\]\]'),
    ):
        path = desktop / name
        content = path.read_text(encoding='utf-8')
        pattern = rf'({header}\nname = "wb2api-desktop"\nversion = ")[^"]+("\n)'
        content, count = re.subn(pattern, lambda match: match[1] + version + match[2], content)
        if count != 1:
            raise ValueError(f'Expected exactly one wb2api-desktop package version in {name}')
        tomllib.loads(content)
        changes[path] = content
    # Validate every input before changing any file; dependency versions stay locked.
    for path, content in changes.items():
        path.write_text(content, encoding='utf-8', newline='\n')
    return version


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--tag', required=True)
    args = parser.parse_args()
    print(set_version(Path(__file__).resolve().parents[1], args.tag))
