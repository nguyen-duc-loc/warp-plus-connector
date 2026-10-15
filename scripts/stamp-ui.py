#!/usr/bin/env python3
"""Bind already verified UI trees to the GoReleaser identity without changing payloads."""
import argparse
import json
from pathlib import Path

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument("--release", required=True)
parser.add_argument("--commit", required=True)
args = parser.parse_args()
root = Path(__file__).resolve().parent.parent
for arch in ["amd64", "arm64"]:
    path = root / ".ui-build" / arch / "lib/wpc-ui/manifest.json"
    manifest = json.loads(path.read_text())
    if (manifest["arch"] != arch or manifest["os"] != "linux"
            or manifest["schema_version"] != 1 or manifest["protocol_version"] != 3):
        raise SystemExit("UI target mismatch")
    manifest["release"], manifest["commit"] = args.release, args.commit
    path.write_text(json.dumps(manifest, indent=2) + "\n")
