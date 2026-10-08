#!/usr/bin/env python3
"""Build the optional panel asset from a pinned, verified upstream release."""
import argparse
import hashlib
import json
from pathlib import Path
import urllib.request

ROOT = Path(__file__).resolve().parent


def patch_panel(data):
    source = json.loads((ROOT / "panel/source.json").read_text())
    if hashlib.sha256(data).hexdigest() != source["sha256"]:
        raise ValueError("Panel does not match the verified upstream release")
    anchor = source["anchor"].encode()
    if data.count(anchor) != 1:
        raise ValueError("Panel patch anchor is missing or ambiguous")
    redirect = (ROOT / "panel/redirect.js").read_bytes().strip()
    return data.replace(anchor, anchor + redirect, 1)


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("--source", type=Path, help="local copy of the pinned upstream management.html")
    parser.add_argument("--output", type=Path, default=ROOT / "release/management.html")
    args = parser.parse_args()
    source = json.loads((ROOT / "panel/source.json").read_text())
    if args.source:
        data = args.source.read_bytes()
    else:
        with urllib.request.urlopen(source["url"], timeout=60) as response:
            data = response.read(50 * 1024 * 1024 + 1)
    result = patch_panel(data)
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_bytes(result)
    print("Created patched management.html; SHA-256:", hashlib.sha256(result).hexdigest())
