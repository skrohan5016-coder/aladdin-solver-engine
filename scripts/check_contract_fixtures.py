#!/usr/bin/env python3
from __future__ import annotations

import hashlib
import json
import re
from pathlib import Path, PurePosixPath

ROOT = Path(__file__).resolve().parents[1]
FIXTURES = ROOT / "testdata" / "contracts"
MANIFEST = FIXTURES / "manifest.json"
PIN = ROOT / "UPSTREAM_PIN.json"
SHA256 = re.compile(r"^[0-9a-f]{64}$")


def safe_fixture_path(name: object) -> Path:
    if not isinstance(name, str) or not name:
        raise SystemExit(f"invalid fixture path {name!r}")
    pure = PurePosixPath(name)
    if pure.is_absolute() or len(pure.parts) != 1 or pure.name != name or "\\" in name:
        raise SystemExit(f"unsafe fixture path {name!r}")
    path = FIXTURES / name
    if path.is_symlink():
        raise SystemExit(f"fixture must not be a symlink: {path}")
    return path


def main() -> int:
    manifest = json.loads(MANIFEST.read_text())
    pin = json.loads(PIN.read_text())
    if manifest.get("schema") != "aladdin-contract-fixture-manifest/v1":
        raise SystemExit("unexpected fixture manifest schema")
    upstream = manifest.get("upstream", {})
    if upstream.get("repository") != pin.get("repository") or upstream.get("commit") != pin.get("commit"):
        raise SystemExit("fixture manifest is not bound to UPSTREAM_PIN.json")
    fixtures = manifest.get("fixtures")
    if not isinstance(fixtures, list) or not fixtures:
        raise SystemExit("fixture manifest is empty")
    listed: set[str] = set()
    for fixture in fixtures:
        if not isinstance(fixture, dict):
            raise SystemExit(f"invalid fixture entry {fixture!r}")
        path = safe_fixture_path(fixture.get("path"))
        if path.name in listed:
            raise SystemExit(f"duplicate fixture path {path.name!r}")
        if not path.is_file() or path.name == "manifest.json":
            raise SystemExit(f"missing fixture {path}")
        expected = fixture.get("sha256")
        if not isinstance(expected, str) or not SHA256.fullmatch(expected):
            raise SystemExit(f"invalid fixture digest for {path}")
        digest = hashlib.sha256(path.read_bytes()).hexdigest()
        if digest != expected:
            raise SystemExit(f"fixture digest mismatch: {path}")
        json.loads(path.read_text())
        listed.add(path.name)
    actual = {
        path.name
        for path in FIXTURES.glob("*.json")
        if path.name != "manifest.json" and not path.is_symlink()
    }
    if listed != actual:
        raise SystemExit(f"fixture manifest mismatch: listed={sorted(listed)} actual={sorted(actual)}")
    print(f"verified {len(listed)} contract fixtures")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
