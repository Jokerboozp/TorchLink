#!/usr/bin/env python3
import importlib.util
from pathlib import Path
import unittest

spec = importlib.util.spec_from_file_location(
    'release_version', Path(__file__).resolve().parents[1] / 'next-release-version.py')
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)


class ReleaseVersionTest(unittest.TestCase):
    def test_first_release_and_legacy_names(self):
        for names in ([], ['build-4-1-042e5c284a49'], ['v0.9.9']):
            self.assertEqual(module.next_version(names), 'v1.0.0')

    def test_numeric_order_and_existing_versions(self):
        self.assertEqual(module.next_version(['v1.0.0', 'v1.0.1']), 'v1.0.2')
        self.assertEqual(module.next_version(['v1.9.9', 'v1.10.2', 'v1.10.2']), 'v1.10.3')
        self.assertEqual(module.next_version(['v2.0.0', 'v1.99.99']), 'v2.0.1')

    def test_unsupported_names_are_ignored(self):
        self.assertEqual(module.next_version(['v1.0.0', 'v9.0.0-beta', 'v01.2.3', 'garbage']), 'v1.0.1')


if __name__ == '__main__':
    unittest.main()
