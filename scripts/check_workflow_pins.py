#!/usr/bin/env python3
from __future__ import annotations

import re
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
HEX40 = re.compile(r"^[0-9a-f]{40}$")


def main() -> int:
    failures: list[str] = []
    for path in sorted((ROOT / ".github" / "workflows").glob("*.y*ml")):
        for line_number, line in enumerate(path.read_text().splitlines(), 1):
            stripped = line.strip()
            if not stripped.startswith("uses:") and not stripped.startswith("- uses:"):
                continue
            value = stripped.split("uses:", 1)[1].strip().split("#", 1)[0].strip()
            if value.startswith("./"):
                continue
            if "@" not in value or not HEX40.fullmatch(value.rsplit("@", 1)[1]):
                failures.append(f"{path.relative_to(ROOT)}:{line_number}: {value}")
    if failures:
        raise SystemExit("unpinned GitHub Action reference(s):\n" + "\n".join(failures))
    print("all GitHub Action references are pinned by commit")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
