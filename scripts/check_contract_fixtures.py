#!/usr/bin/env python3
from __future__ import annotations

import hashlib
import json
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
FIXTURES = ROOT / "testdata" / "contracts"
MANIFEST = FIXTURES / "manifest.json"
PIN = ROOT / "UPSTREAM_PIN.json"


def main() -> int:
    manifest = json.loads(MANIFEST.read_text())
    pin = json.loads(PIN.read_text())
    if manifest.get("schema") != "aladdin-contract-fixture-manifest/v1":
        raise SystemExit("unexpected fixture manifest schema")
    upstream = manifest.get("upstream", {})
    if upstream.get("repository") != pin.get("repository") or upstream.get("commit") != pin.get("commit"):
        raise SystemExit("fixture manifest is not bound to UPSTREAM_PIN.json")
    listed = set()
    for fixture in manifest.get("fixtures", []):
        path = FIXTURES / fixture["path"]
        if not path.is_file() or path.name == "manifest.json":
            raise SystemExit(f"missing fixture {path}")
        digest = hashlib.sha256(path.read_bytes()).hexdigest()
        if digest != fixture.get("sha256"):
            raise SystemExit(f"fixture digest mismatch: {path}")
        json.loads(path.read_text())
        listed.add(path.name)
    actual = {path.name for path in FIXTURES.glob("*.json") if path.name != "manifest.json"}
    if listed != actual:
        raise SystemExit(f"fixture manifest mismatch: listed={sorted(listed)} actual={sorted(actual)}")
    print(f"verified {len(listed)} contract fixtures")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
