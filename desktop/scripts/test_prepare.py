import hashlib
import importlib.util
import io
import json
import pathlib
import tarfile
import tempfile
import unittest
from unittest import mock

SCRIPT = pathlib.Path(__file__).with_name('prepare.py')


class PrepareTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        if SCRIPT.exists():
            spec = importlib.util.spec_from_file_location('prepare', SCRIPT)
            cls.prepare = importlib.util.module_from_spec(spec)
            spec.loader.exec_module(cls.prepare)
        else:
            cls.prepare = None

    def setUp(self):
        self.assertIsNotNone(self.prepare, 'runtime preparation tool must exist')
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = pathlib.Path(self.tmp.name)

    def archive(self, entries):
        path = self.root / 'python.tar.gz'
        with tarfile.open(path, 'w:gz') as archive:
            for name, content, link in entries:
                info = tarfile.TarInfo(name)
                if link:
                    info.type, info.linkname = tarfile.SYMTYPE, link
                    archive.addfile(info)
                else:
                    info.size = len(content)
                    archive.addfile(info, io.BytesIO(content))
        return path

    def test_bad_digest_rejected_before_extract(self):
        archive = self.archive([('python/bin/python3', b'fake', None)])
        destination = self.root / 'stage'
        with self.assertRaisesRegex(ValueError, 'SHA-256'):
            self.prepare.extract_python(archive, '0' * 64, destination)
        self.assertFalse(destination.exists())

    def test_archive_escape_rejected(self):
        for name, link in [('../escaped', None), ('/escaped', None),
                           ('python/../../escaped', None), ('python/escape', '../../escaped'),
                           ('python/escape', '/tmp/escaped'), ('python\\..\\escaped', None)]:
            with self.subTest(name=name, link=link):
                archive = self.archive([(name, b'x', link)])
                with self.assertRaises((ValueError, tarfile.FilterError)):
                    self.prepare.extract_python(archive, hashlib.sha256(archive.read_bytes()).hexdigest(), self.root / 'stage')
                self.assertFalse((self.root / 'escaped').exists())

    def test_internal_python_symlink_is_preserved(self):
        archive = self.archive([('python/bin/python3.12', b'binary', None),
                                ('python/bin/python3', b'', 'python3.12')])
        self.prepare.extract_python(archive, hashlib.sha256(archive.read_bytes()).hexdigest(), self.root / 'stage')
        self.assertEqual((self.root / 'stage/python/bin/python3').read_bytes(), b'binary')

    def test_target_build_layout(self):
        cases = [('aarch64-apple-darwin', 'darwin', 'arm64', 'core', 'python/bin/python3'),
                 ('x86_64-apple-darwin', 'darwin', 'amd64', 'core', 'python/bin/python3'),
                 ('x86_64-pc-windows-msvc', 'windows', 'amd64', 'core.exe', 'python/python.exe')]
        for target, goos, goarch, core, interpreter in cases:
            with self.subTest(target=target):
                config = self.prepare.target_config(target)
                self.assertEqual((config['goos'], config['goarch'], config['core'], config['python']),
                                 (goos, goarch, core, interpreter))
        with self.assertRaises(ValueError):
            self.prepare.target_config('x86_64-unknown-linux-gnu')

    def test_macos_minimum_checks_services_and_python_shared_libraries(self):
        self.assertTrue(hasattr(self.prepare, 'validate_macos_runtime'), 'must verify bundled Mach-O deployment targets')
        stage = self.root / 'runtime'
        (stage / 'python/lib').mkdir(parents=True)
        for name in ('core', 'console', 'python/lib/libpython.dylib'):
            (stage / name).write_bytes(b'\xcf\xfa\xed\xfe' + b'fixture')
        (stage / 'config.json').write_text('{}')
        compatible = 'Load command 1\n      cmd LC_BUILD_VERSION\n    minos 12.0\n   ntools 1\n     tool LD\n  version 22.1.3\n'
        legacy = 'Load command 1\n      cmd LC_VERSION_MIN_MACOSX\n  version 11.0\n'
        def otool(args, **kwargs):
            self.assertEqual(args[:2], ['otool', '-l'])
            return ''.join(str(p) + ':\n' + (legacy if str(p).endswith('.dylib') else compatible) for p in args[2:])
        with mock.patch.object(self.prepare.subprocess, 'check_output', side_effect=otool) as command:
            self.prepare.validate_macos_runtime(stage, '12.0')
            self.assertIn(str((stage / 'python/lib/libpython.dylib').resolve()), command.call_args.args[0])
            with self.assertRaisesRegex(ValueError, '12.0.*11.0'):
                self.prepare.validate_macos_runtime(stage, '11.0')
        with mock.patch.object(self.prepare.subprocess, 'check_output', return_value=compatible + legacy.replace('11.0', '13.0')):
            with self.assertRaisesRegex(ValueError, '13.0.*12.0'):
                self.prepare.validate_macos_runtime(stage, '12.0')
        with mock.patch.object(self.prepare.subprocess, 'check_output', return_value='no deployment target'):
            with self.assertRaisesRegex(ValueError, 'deployment target'):
                self.prepare.validate_macos_runtime(stage, '12.0')

    def test_published_manifest_identifies_prepared_target_and_python(self):
        self.assertTrue(hasattr(self.prepare, 'write_runtime_metadata'), 'prepared runtime must carry target metadata')
        for target in ('aarch64-apple-darwin', 'x86_64-apple-darwin', 'x86_64-pc-windows-msvc'):
            with self.subTest(target=target):
                stage = self.root / target
                stage.mkdir()
                config = self.prepare.target_config(target)
                self.prepare.write_runtime_metadata(stage, config, {'python_version': '3.12.fixture'})
                output = self.root / 'runtime'
                self.prepare.publish(stage, output)
                self.assertEqual(json.loads((output / 'runtime-manifest.json').read_text()),
                                 {'target': target, 'python_version': '3.12.fixture'})
                self.assertEqual(json.loads((output / 'config.json').read_text()), {})

    def test_resources_include_scripts_not_credentials_or_tests(self):
        source = self.root / 'materialized'
        (source / 'scripts').mkdir(parents=True)
        for name in ('task_common.py', 'task_runner.py', 'school_open_day_2026.py', 'task_events.py', 'global_region.py', 'test_task_events.py', 'secret.json', 'school_open_day_cron.sh'):
            (source / 'scripts' / name).write_text('# fixture\n')
        destination = self.root / 'resources'
        destination.mkdir()
        self.prepare.copy_scripts(source, destination)
        self.assertEqual({p.name for p in (destination / 'scripts').iterdir()},
                         {'task_common.py', 'task_runner.py', 'school_open_day_2026.py', 'task_events.py', 'global_region.py'})

    def test_failed_replacement_restores_existing_runtime(self):
        output = self.root / 'runtime'
        stage = self.root / 'stage'
        output.mkdir(); stage.mkdir()
        (output / 'marker').write_text('old')
        (stage / 'marker').write_text('new')
        original = pathlib.Path.rename
        def rename(path, dest):
            if path == stage:
                raise OSError('simulated final rename failure')
            return original(path, dest)
        with mock.patch.object(pathlib.Path, 'rename', rename):
            with self.assertRaises(OSError):
                self.prepare.publish(stage, output)
        self.assertEqual((output / 'marker').read_text(), 'old')

    def test_successful_replacement_publishes_complete_stage(self):
        output, stage = self.root / 'runtime', self.root / 'stage'
        output.mkdir(); stage.mkdir()
        (output / 'old').write_text('old')
        (stage / 'core').write_text('new')
        self.prepare.publish(stage, output)
        self.assertEqual([p.name for p in output.iterdir()], ['core'])


if __name__ == '__main__':
    unittest.main()
