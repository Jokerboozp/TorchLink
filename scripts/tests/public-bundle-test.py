#!/usr/bin/env python3
"""Exercise public account defaults and independent target-only secrets."""
import importlib.util
import json
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest

SCRIPTS = Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location('public_bundle', SCRIPTS / 'prepare-public-bundle.py')
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)


class PublicBundleTest(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory()
        self.addCleanup(self.directory.cleanup)
        self.bundle = Path(self.directory.name) / 'bundle'
        (self.bundle / 'scripts').mkdir(parents=True)
        (self.bundle / 'manifest.json').write_text(json.dumps({'generatedCredentials': True}))
        self.env = self.bundle / '.env.offline'
        subprocess.run(['bash', '-c', 'source "$1"; ensure_deployment_env "$2"',
                        '--', str(SCRIPTS / 'lib/deployment.sh'), str(self.env)],
                       check=True, stdout=subprocess.DEVNULL)
        self.original = self.env.read_text()

    def initialize(self, bundle):
        subprocess.run(['bash', str(bundle / 'scripts/init-offline-env.sh'), str(bundle)],
                       check=True, stdout=subprocess.DEVNULL)

    def test_publication_and_independent_repeatable_installations(self):
        secrets = [line.split('=', 1)[1] for line in self.original.splitlines()
                   if '=' in line and line.split('=', 1)[0].endswith(('_PASSWORD', '_TOKEN', '_SECRET'))
                   and line.split('=', 1)[0] not in module.ADMIN_PASSWORD_KEYS]
        with self.env.open('a') as stream:
            stream.write('LITERAL=$(touch should-never-exist)\n')
        module.prepare(self.bundle)
        self.assertFalse(self.env.exists())
        for path in self.bundle.rglob('*'):
            if path.is_file():
                content = path.read_text()
                for secret in secrets:
                    self.assertNotIn(secret, content)
        other = Path(self.directory.name) / 'other'
        shutil.copytree(self.bundle, other)
        self.initialize(self.bundle)
        self.initialize(other)
        first = self.env.read_text()
        self.assertNotEqual(first, (other / '.env.offline').read_text())
        self.assertNotIn('__TORCHLINK_RANDOM_HEX__', first)
        self.assertNotIn('__TORCHLINK_RANDOM_BASE64_32__', first)
        self.assertIn('LITERAL=$(touch should-never-exist)', first)
        self.assertEqual(self.env.stat().st_mode & 0o777, 0o600)
        values = dict(line.split('=', 1) for line in first.splitlines() if '=' in line and not line.startswith('#'))
        for key in module.ADMIN_PASSWORD_KEYS:
            self.assertEqual(values[key], 'admin123', key)
        for key in module.ADMIN_USERNAME_KEYS:
            self.assertEqual(values[key], 'admin', key)
        other_values = dict(line.split('=', 1) for line in (other / '.env.offline').read_text().splitlines()
                            if '=' in line and not line.startswith('#'))
        for key in ('IOT_JWT_SECRET', 'IOT_AI_HARNESS_TOKEN', 'IOT_BACKUP_ADMIN_TOKEN', 'GB26875_CONTROL_TOKEN'):
            self.assertRegex(values[key], r'^[a-f0-9]{64}$')
            self.assertNotEqual(values[key], other_values[key], key)
        self.assertNotEqual(values['IOT_JWT_SECRET'], values['IOT_AI_HARNESS_TOKEN'])
        self.assertRegex(values['IOT_VIDEO_PLATFORM_SECRETS'], r'^video-platform-1:[a-f0-9]{64}$')
        self.assertEqual(values['IOT_EMBEDDING_API_KEY'], '')
        self.initialize(self.bundle)
        self.assertEqual(first, self.env.read_text())
        self.env.write_text(self.original)
        self.initialize(self.bundle)
        self.assertEqual(self.original, self.env.read_text())

    def test_generated_account_values_are_replaced_not_published(self):
        values = dict(line.split('=', 1) for line in self.original.splitlines()
                      if '=' in line and not line.startswith('#'))
        original_values = []
        for key in module.ADMIN_PASSWORD_KEYS | module.ADMIN_USERNAME_KEYS:
            values[key] = 'private-packaging-value-' + key
            original_values.append(values[key])
        # Unlisted password settings remain internal secrets, not shared defaults.
        values['IOT_INTERNAL_TEST_PASSWORD'] = 'private-internal-test-password'
        original_values.append(values['IOT_INTERNAL_TEST_PASSWORD'])
        self.env.write_text(''.join(f'{key}={value}\n' for key, value in values.items()))
        module.prepare(self.bundle)
        for path in self.bundle.rglob('*'):
            if path.is_file():
                content = path.read_text()
                for value in original_values:
                    self.assertNotIn(value, content)
        self.initialize(self.bundle)
        installed = dict(line.split('=', 1) for line in self.env.read_text().splitlines()
                         if '=' in line and not line.startswith('#'))
        for key in module.ADMIN_PASSWORD_KEYS:
            self.assertEqual(installed[key], 'admin123', key)
        for key in module.ADMIN_USERNAME_KEYS:
            self.assertEqual(installed[key], 'admin', key)
        self.assertRegex(installed['IOT_INTERNAL_TEST_PASSWORD'], r'^[a-f0-9]{64}$')

    def test_external_config_is_rejected_without_removing_it(self):
        for value in (False, None, 'true', 1):
            with self.subTest(value=value):
                (self.bundle / 'manifest.json').write_text(json.dumps({'generatedCredentials': value}))
                with self.assertRaises(ValueError):
                    module.prepare(self.bundle)
                self.assertEqual(self.original, self.env.read_text())
                self.assertFalse((self.bundle / '.env.offline.template').exists())

    def test_api_key_is_rejected_without_removing_config(self):
        for key in ('DEEPSEEK_API_KEY', 'IOT_AI_API_KEY', 'IOT_EMBEDDING_API_KEY'):
            with self.subTest(key=key):
                self.env.write_text(self.original.replace(key + '=', key + '=test-only-key'))
                with self.assertRaises(ValueError):
                    module.prepare(self.bundle)
                self.assertTrue(self.env.exists())
                self.assertFalse((self.bundle / '.env.offline.template').exists())


if __name__ == '__main__':
    unittest.main()
