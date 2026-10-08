#!/usr/bin/env python3
"""Create a CPA schema-v2 direct-install registry and its checksummed archive."""
import argparse
import hashlib
import json
from pathlib import Path
import zipfile

ROOT = Path(__file__).resolve().parent
VERSION = "0.3.2"
ARCHIVE_NAME = f"key-chat-access_{VERSION}_linux_amd64.zip"


def make_release(base_url, output):
    output = Path(output)
    output.mkdir(parents=True, exist_ok=True)
    archive = output / ARCHIVE_NAME
    with zipfile.ZipFile(archive, "w", zipfile.ZIP_DEFLATED) as z:
        z.write(ROOT / "dist/linux/amd64/key-chat-access.so", "key-chat-access.so")
    data = archive.read_bytes()
    digest = hashlib.sha256(data).hexdigest()
    registry = {
        "schema_version": 2,
        "plugins": [{
            "id": "key-chat-access",
            "name": "Key Chat Access",
            "description": "Block /v1/chat/completions for selected authenticated CPA callers, before upstream execution.",
            "author": "Local administration",
            "version": VERSION,
            "tags": ["Interceptor", "Access"],
            "install": {
                "type": "direct",
                "artifacts": [{
                    "goos": "linux", "goarch": "amd64",
                    "url": base_url.rstrip("/") + "/" + ARCHIVE_NAME,
                    "sha256": digest, "size": len(data),
                }],
            },
        }],
    }
    (output / "registry.json").write_text(json.dumps(registry, indent=2) + "\n")
    (output / "checksums.txt").write_text(digest + "  " + ARCHIVE_NAME + "\n")
    return registry


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("--base-url", required=True, help="HTTPS directory URL at which both files will be hosted")
    parser.add_argument("--output", default=ROOT / "release", type=Path)
    args = parser.parse_args()
    make_release(args.base_url, args.output)
    print("Created registry.json, checksums.txt and " + ARCHIVE_NAME)
