#!/usr/bin/env python3
from __future__ import annotations

import json
import re
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
PIN_PATH = ROOT / "UPSTREAM_PIN.json"
CONTRACT_GO = ROOT / "internal" / "contract" / "contract.go"
UPSTREAM_MD = ROOT / "UPSTREAM.md"
HEX40 = re.compile(r"^[0-9a-f]{40}$")


def main() -> int:
    pin = json.loads(PIN_PATH.read_text())
    if pin.get("schema") != "aladdin-upstream-pin/v1":
        raise SystemExit("unexpected upstream pin schema")
    commit = pin.get("commit", "")
    if not HEX40.fullmatch(commit):
        raise SystemExit("upstream commit is not a lowercase 40-hex SHA")
    files = pin.get("files", [])
    if len(files) < 5:
        raise SystemExit("upstream pin is missing authoritative files")
    seen_paths: set[str] = set()
    contract_source = CONTRACT_GO.read_text()
    documentation = UPSTREAM_MD.read_text()
    for item in files:
        path = item.get("path", "")
        blob = item.get("blob", "")
        if not path or path in seen_paths or not HEX40.fullmatch(blob):
            raise SystemExit(f"invalid upstream file pin: {item}")
        seen_paths.add(path)
        if path not in contract_source or blob not in contract_source:
            raise SystemExit(f"internal/contract does not bind {path} to {blob}")
        if path not in documentation or blob not in documentation:
            raise SystemExit(f"UPSTREAM.md does not document {path} at {blob}")
    if commit not in contract_source or commit not in documentation:
        raise SystemExit("upstream commit is not consistently bound")
    print(f"verified upstream pin {commit} across {len(files)} authoritative files")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
