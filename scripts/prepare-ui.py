#!/usr/bin/env python3
"""Prepare one locked UI tree. Startup never invokes this build tool."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import tarfile
import urllib.request

ROOT = Path(__file__).resolve().parent.parent


def digest(path):
    with path.open("rb") as stream:
        return hashlib.file_digest(stream, "sha256").hexdigest()


def run(args, cwd):
    subprocess.run(args, cwd=cwd, check=True)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--arch", choices=["amd64", "arm64"], required=True)
    parser.add_argument("--release", default="dev")
    parser.add_argument("--commit", default="")
    args = parser.parse_args()
    pins = json.loads((ROOT / "ui/runtime.json").read_text())
    if subprocess.check_output(["npm", "--version"], text=True).strip() != pins["npm"]:
        raise SystemExit("Use the npm version pinned in ui/runtime.json")
    if subprocess.check_output(["node", "--version"], text=True).strip() != "v" + pins["node"]:
        raise SystemExit("Use the Node version pinned in ui/runtime.json")
    build = ROOT / ".ui-build"
    cache = build / "downloads"
    cache.mkdir(parents=True, exist_ok=True)
    archive = pins["archives"][args.arch]
    source = cache / archive["file"]
    if not source.exists():
        url = f'https://nodejs.org/dist/v{pins["node"]}/{archive["file"]}'
        temporary = source.with_suffix(".download")
        with urllib.request.urlopen(url, timeout=60) as response, temporary.open("wb") as output:
            shutil.copyfileobj(response, output)
        temporary.replace(source)
    if digest(source) != archive["sha256"]:
        raise SystemExit("Node archive checksum mismatch")
    run(["npm", "ci", "--ignore-scripts", "--no-audit", "--no-fund"], ROOT / "ui")
    run(["npm", "run", "build"], ROOT / "ui")
    destination = build / args.arch / "lib/wpc-ui"
    if destination.exists():
        shutil.rmtree(destination)
    app = destination / "app"
    licenses = destination / "licenses"
    app.mkdir(parents=True)
    licenses.mkdir()
    with tarfile.open(source) as tar:
        prefix = archive["file"].removesuffix(".tar.xz")
        for entry, target in [("bin/node", "node/bin/node"), ("LICENSE", "licenses/node-LICENSE")]:
            member = tar.getmember(prefix + "/" + entry)
            if not member.isfile():
                raise SystemExit("Unexpected runtime archive entry")
            output = destination / target
            output.parent.mkdir(parents=True, exist_ok=True)
            with tar.extractfile(member) as stream, output.open("wb") as handle:
                shutil.copyfileobj(stream, handle)
            output.chmod(0o755 if entry == "bin/node" else 0o644)
    for name in ["package.json", "package-lock.json"]:
        shutil.copy2(ROOT / "ui" / name, app / name)
    shutil.copytree(ROOT / "ui/dist", app / "dist", ignore=shutil.ignore_patterns("*.test.js"))
    run(["npm", "ci", "--omit=dev", "--ignore-scripts", "--no-audit", "--no-fund"], app)
    # CLI launchers are build conveniences. Runtime modules remain intact and regular.
    for folder, dirs, files in os.walk(app / "node_modules", topdown=True):
        if ".bin" in dirs:
            shutil.rmtree(Path(folder) / ".bin")
            dirs.remove(".bin")
        for name in files:
            path = Path(folder) / name
            if path.is_symlink():
                raise SystemExit("Unexpected symlink in production module tree")
            if name.lower().startswith(("license", "licence", "copying", "notice")):
                target = licenses / path.relative_to(app / "node_modules")
                target.parent.mkdir(parents=True, exist_ok=True)
                shutil.copy2(path, target)
    manifest = {"schema_version": 1, "protocol_version": 2, "release": args.release,
                "commit": args.commit, "os": "linux", "arch": args.arch, "node": pins["node"],
                "lock_sha256": digest(app / "package-lock.json"), "files": {}}
    for path in sorted(destination.rglob("*")):
        if path.is_file():
            manifest["files"][path.relative_to(destination).as_posix()] = digest(path)
    (destination / "manifest.json").write_text(json.dumps(manifest, indent=2) + "\n")
    print(destination)


if __name__ == "__main__":
    main()
