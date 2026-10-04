#!/usr/bin/env python3
"""Turns `go test -json` output into GitHub annotations for failed tests.

Annotations are readable through the public checks API, unlike job logs.
"""
import json
import sys
from collections import defaultdict

output = defaultdict(list)
failed = []
for line in open(sys.argv[1], encoding="utf-8", errors="replace"):
    try:
        event = json.loads(line)
    except ValueError:
        print(line, end="")
        continue
    key = (event.get("Package", ""), event.get("Test", ""))
    if event.get("Action") == "output":
        text = event.get("Output", "")
        output[key].append(text)
        sys.stdout.write(text)
    elif event.get("Action") == "fail":
        failed.append(key)

for package, test in failed[:20]:
    lines = [l.rstrip("\n") for l in output[(package, test)] if l.strip()][-25:]
    message = "\n".join(lines).replace("%", "%25").replace("\r", "").replace("\n", "%0A")
    print(f"::error title=FAIL {package} {test}::{message}")
