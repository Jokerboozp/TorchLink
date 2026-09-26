#!/usr/bin/env python3
"""Exercise credential removal and actual first-install initialization."""
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
                   if '=' in line and line.split('=', 1)[0].endswith(('_PASSWORD', '_TOKEN', '_SECRET'))]
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
        self.assertIn('LITERAL=$(touch should-never-exist)', first)
        self.assertEqual(self.env.stat().st_mode & 0o777, 0o600)
        values = dict(line.split('=', 1) for line in first.splitlines() if '=' in line and not line.startswith('#'))
        for key in ('POSTGRES_PASSWORD', 'REDIS_PASSWORD', 'IOT_ADMIN_PASSWORD', 'IOT_JWT_SECRET'):
            self.assertRegex(values[key], r'^[a-f0-9]{64}$')
        self.assertNotEqual(values['POSTGRES_PASSWORD'], values['REDIS_PASSWORD'])
        self.assertRegex(values['IOT_VIDEO_PLATFORM_SECRETS'], r'^video-platform-1:[a-f0-9]{64}$')
        self.initialize(self.bundle)
        self.assertEqual(first, self.env.read_text())
        self.env.write_text(self.original)
        self.initialize(self.bundle)
        self.assertEqual(self.original, self.env.read_text())

    def test_external_config_is_rejected_without_removing_it(self):
        (self.bundle / 'manifest.json').write_text('{"generatedCredentials": false}')
        with self.assertRaises(ValueError):
            module.prepare(self.bundle)
        self.assertEqual(self.original, self.env.read_text())

    def test_api_key_is_rejected_without_removing_config(self):
        self.env.write_text(self.original.replace('DEEPSEEK_API_KEY=', 'DEEPSEEK_API_KEY=test-only-key'))
        with self.assertRaises(ValueError):
            module.prepare(self.bundle)
        self.assertTrue(self.env.exists())
        self.assertFalse((self.bundle / '.env.offline.template').exists())


if __name__ == '__main__':
    unittest.main()
