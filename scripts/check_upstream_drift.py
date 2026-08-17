#!/usr/bin/env python3
from __future__ import annotations

import argparse
import json
import os
import urllib.parse
import urllib.request
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
PIN = json.loads((ROOT / "UPSTREAM_PIN.json").read_text())


def fetch_json(url: str) -> dict:
    headers = {"Accept": "application/vnd.github+json", "User-Agent": "aladdin-solver-engine-contract-check"}
    token = os.environ.get("GITHUB_TOKEN")
    if token:
        headers["Authorization"] = f"Bearer {token}"
    request = urllib.request.Request(url, headers=headers)
    with urllib.request.urlopen(request, timeout=30) as response:
        return json.load(response)


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--ref", default="main", help="upstream ref to compare with the accepted pin")
    parser.add_argument("--verify-pinned", action="store_true", help="also prove the accepted commit still resolves to every pinned blob")
    args = parser.parse_args()

    repository = PIN["repository"]
    base = f"https://api.github.com/repos/{repository}/contents/"
    drift: list[str] = []
    for item in PIN["files"]:
        encoded_path = urllib.parse.quote(item["path"], safe="/")
        current = fetch_json(base + encoded_path + "?ref=" + urllib.parse.quote(args.ref, safe=""))
        if current.get("sha") != item["blob"]:
            drift.append(f"{item['path']}: pinned {item['blob']} current {current.get('sha')}")
        if args.verify_pinned:
            accepted = fetch_json(base + encoded_path + "?ref=" + PIN["commit"])
            if accepted.get("sha") != item["blob"]:
                raise SystemExit(f"accepted pin no longer resolves: {item['path']}")
    if drift:
        raise SystemExit("upstream wire-contract drift detected:\n" + "\n".join(drift))
    print(f"no drift across {len(PIN['files'])} authoritative files at {repository}@{args.ref}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
