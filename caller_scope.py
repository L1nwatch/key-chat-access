#!/usr/bin/env python3
"""Derive CPA's authenticated caller scope without exposing a key in shell history."""
import getpass
import hashlib

if __name__ == "__main__":
    key = getpass.getpass("Client API key (hidden): ").strip()
    if not key:
        raise SystemExit("API key must not be empty")
    print(hashlib.sha256(b"cli-proxy-api:caller-scope:v1\x00" + key.encode()).hexdigest())
