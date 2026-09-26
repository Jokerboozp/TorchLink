#!/usr/bin/env python3
"""Choose the next deployment version from existing release/tag names."""
import re
import sys


def next_version(names):
    versions = []
    for name in names:
        match = re.fullmatch(r'v(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)', name.strip())
        if match:
            versions.append(tuple(map(int, match.groups())))
    if not versions or max(versions) < (1, 0, 0):
        return 'v1.0.0'
    major, minor, patch = max(versions)
    return f'v{major}.{minor}.{patch + 1}'


if __name__ == '__main__':
    print(next_version(sys.stdin))
