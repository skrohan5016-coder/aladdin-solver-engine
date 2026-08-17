#!/usr/bin/env python3
from __future__ import annotations

import json
import os
import subprocess
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
PIN = ROOT / "UPSTREAM_PIN.json"


def run(*args: str) -> str:
    return subprocess.check_output(args, cwd=ROOT, text=True, stderr=subprocess.DEVNULL).strip()


def main() -> int:
    try:
        parent = run("git", "rev-parse", "HEAD^1")
    except (subprocess.CalledProcessError, FileNotFoundError):
        print("no parent commit available; pin-change policy not applicable")
        return 0
    try:
        run("git", "cat-file", "-e", f"{parent}:UPSTREAM_PIN.json")
    except subprocess.CalledProcessError:
        print("UPSTREAM_PIN.json introduced for the first time")
        return 0
    changed = set(run("git", "diff", "--name-only", parent, "HEAD").splitlines())
    if "UPSTREAM_PIN.json" not in changed and "UPSTREAM.md" not in changed:
        print("upstream pin unchanged")
        return 0
    pin = json.loads(PIN.read_text())
    review = f"docs/upstream-pin-reviews/{pin['commit']}.md"
    required = {"UPSTREAM_PIN.json", "UPSTREAM.md", "testdata/contracts/manifest.json", review}
    missing = required - changed
    if missing:
        raise SystemExit(f"pin update is missing dedicated review evidence: {sorted(missing)}")
    event_path = os.environ.get("GITHUB_EVENT_PATH")
    if event_path and Path(event_path).is_file():
        event = json.loads(Path(event_path).read_text())
        title = event.get("pull_request", {}).get("title", "").lower()
        if title and "upstream" not in title and "wire contract" not in title:
            raise SystemExit("upstream pin changes require a dedicated upstream/wire-contract pull request")
    print(f"upstream pin change has dedicated review evidence: {review}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
