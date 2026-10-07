#!/usr/bin/env python3
"""Build native clients and a Linux server bundle; Python is build tooling only."""
import argparse
import gzip
import io
import os
from pathlib import Path
import re
import subprocess
import tarfile
import tempfile
import zipfile

ROOT = Path(__file__).resolve().parent.parent
PLATFORMS = ("linux-amd64", "linux-arm64", "darwin-amd64", "darwin-arm64", "windows-amd64")


def tar(path, entries):
    with path.open("wb") as raw, gzip.GzipFile(fileobj=raw, mode="wb", mtime=0) as gz, tarfile.open(fileobj=gz, mode="w") as archive:
        for name, data, mode in entries:
            info = tarfile.TarInfo(name)
            info.size, info.mode, info.mtime = len(data), mode, 0
            archive.addfile(info, io.BytesIO(data))


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--version", required=True)
    parser.add_argument("--revision", required=True)
    parser.add_argument("--platform", action="append", choices=PLATFORMS)
    parser.add_argument("--output", type=Path, default=ROOT / "dist")
    args = parser.parse_args()
    if not re.fullmatch(r"v\d+\.\d+\.\d+(?:[.-][0-9A-Za-z.-]+)?", args.version) or not re.fullmatch(r"[0-9a-f]{40}", args.revision):
        parser.error("invalid version/revision")
    args.output.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix="atlas-go-build-") as directory:
        for platform in args.platform or PLATFORMS:
            goos, arch = platform.split("-")
            name = "hive-atlas.exe" if goos == "windows" else "hive-atlas"
            binary = Path(directory) / name
            package = "github.com/H-I-V-E-Tec/hive_atlas/internal/app"
            subprocess.run(["go", "build", "-trimpath", "-buildvcs=false", "-ldflags",
                            f"-s -w -X {package}.Version={args.version} -X {package}.Revision={args.revision}",
                            "-o", str(binary), "./cmd/hive-atlas"], cwd=ROOT,
                           env={**os.environ, "GOOS": goos, "GOARCH": arch, "CGO_ENABLED": "0"}, check=True)
            data = binary.read_bytes()
            if goos == "windows":
                with zipfile.ZipFile(args.output / f"atlas-{args.version}-{platform}.zip", "w", compression=zipfile.ZIP_DEFLATED) as archive:
                    info = zipfile.ZipInfo(name, date_time=(1980, 1, 1, 0, 0, 0))
                    info.external_attr = 0o755 << 16
                    archive.writestr(info, data, compress_type=zipfile.ZIP_DEFLATED)
            else:
                tar(args.output / f"atlas-{args.version}-{platform}.tar.gz", [(name, data, 0o755)])
            if platform == "linux-amd64":
                entries = [("REVISION", (args.revision + "\n").encode(), 0o644), ("bin/hive-atlas", data, 0o755)]
                for relative in ("signals/core.json", "deploy/deploy_server.sh", "deploy/atlas.env.example", "deploy/hive-atlas.service"):
                    entries.append((relative, (ROOT / relative).read_bytes(), 0o755 if relative.endswith(".sh") else 0o644))
                tar(args.output / f"atlas-server-{args.version}.tar.gz", entries)
    (args.output / "SOURCE.txt").write_text(f"version={args.version}\ncommit={args.revision}\n")


if __name__ == "__main__":
    main()
