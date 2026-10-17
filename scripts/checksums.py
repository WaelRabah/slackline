#!/usr/bin/env python3
"""Checksum distributable archives, packages, and the curl installer."""
import hashlib
from pathlib import Path
import sys

root = Path(sys.argv[1] if len(sys.argv) > 1 else "dist")
files = sorted(
    p for p in root.iterdir()
    if p.is_file() and (
        p.name == "install.sh" or p.name.endswith((".tar.gz", ".pkg", ".deb", ".rpm"))
    )
)
if not files:
    raise SystemExit("No release packages found")
with (root / "checksums.txt").open("w", encoding="utf-8") as output:
    for path in files:
        with path.open("rb") as source:
            digest = hashlib.file_digest(source, "sha256").hexdigest()
        output.write(f"{digest}  {path.name}\n")
