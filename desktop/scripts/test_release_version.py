import json
from pathlib import Path
import shutil
import tempfile
import tomllib
import unittest

from release_version import set_version


class ReleaseVersionTest(unittest.TestCase):
    def test_tag_updates_all_manifests_without_changing_dependencies(self):
        source = Path(__file__).resolve().parents[1]
        names = ('tauri.conf.json', 'package.json', 'package-lock.json', 'Cargo.toml', 'Cargo.lock')
        with tempfile.TemporaryDirectory() as temporary:
            desktop = Path(temporary)
            for name in names:
                shutil.copy2(source / name, desktop / name)
            dependencies = tomllib.loads((desktop / 'Cargo.lock').read_text())['package']
            dependencies = [item for item in dependencies if item['name'] != 'wb2api-desktop']
            for tag in ('v1.2.3', 'v1.2.4-rc.1'):
                with self.subTest(tag=tag):
                    version = set_version(desktop, tag)
                    self.assertEqual(version, tag[1:])
                    for name in names[:3]:
                        self.assertEqual(json.loads((desktop / name).read_text())['version'], version)
                    lock = json.loads((desktop / 'package-lock.json').read_text())
                    self.assertEqual(lock['packages']['']['version'], version)
                    cargo = tomllib.loads((desktop / 'Cargo.toml').read_text())
                    self.assertEqual(cargo['package']['version'], version)
                    packages = tomllib.loads((desktop / 'Cargo.lock').read_text())['package']
                    self.assertEqual([p['version'] for p in packages if p['name'] == 'wb2api-desktop'], [version])
                    self.assertEqual([p for p in packages if p['name'] != 'wb2api-desktop'], dependencies)
            before = {name: (desktop / name).read_bytes() for name in names}
            for tag in ('1.2.3', 'v01.2.3', 'v1.2', 'v1.2.3\n', 'v1.2.3+build',
                        'v1.2.3-rc.01', 'v65536.0.0', 'v1.2.3;echo unsafe'):
                with self.subTest(tag=tag), self.assertRaises(ValueError):
                    set_version(desktop, tag)
            self.assertEqual(before, {name: (desktop / name).read_bytes() for name in names})


if __name__ == '__main__':
    unittest.main()
