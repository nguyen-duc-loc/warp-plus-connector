#!/usr/bin/env python3
"""Verify both final archive layouts and every manifest payload digest."""
import hashlib
import json
from pathlib import Path
import tarfile

root = Path(__file__).resolve().parent.parent
for arch in ["amd64", "arm64"]:
    archives = list((root / "dist").glob(f"*_linux_{arch}.tar.gz"))
    if len(archives) != 1:
        raise SystemExit(f"Expected one final {arch} archive")
    with tarfile.open(archives[0]) as archive:
        entries = {entry.name: entry for entry in archive.getmembers()}
        prefix = "lib/wpc-ui/"
        manifest = json.load(archive.extractfile(prefix + "manifest.json"))
        if manifest["arch"] != arch or manifest["os"] != "linux":
            raise SystemExit("Archive identity mismatch")
        if not entries["wpc"].mode & 0o111 or not entries[prefix + "node/bin/node"].mode & 0o111:
            raise SystemExit("Archive lost executable permissions")
        expected = {prefix + path for path in manifest["files"]} | {prefix + "manifest.json"}
        actual = {name for name, entry in entries.items() if name.startswith(prefix) and not entry.isdir()}
        if expected != actual:
            raise SystemExit("Archive contains missing or unexpected UI files")
        for path, digest in manifest["files"].items():
            entry = entries[prefix + path]
            if not entry.isfile():
                raise SystemExit("Archive contains a nonregular UI file")
            with archive.extractfile(entry) as stream:
                if hashlib.file_digest(stream, "sha256").hexdigest() != digest:
                    raise SystemExit("Archive payload digest mismatch")
        if manifest["files"]["app/package-lock.json"] != manifest["lock_sha256"]:
            raise SystemExit("Archive lock digest mismatch")
    print(f"Verified {archives[0].name}")
