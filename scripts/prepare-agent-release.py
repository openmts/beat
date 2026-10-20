#!/usr/bin/env python3
"""Prepare signed-Agent assets from GoReleaser archives without executing them."""

from datetime import datetime, timedelta, timezone
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import sys
import tarfile

ARCHIVE = re.compile(r"^beat_(?P<version>.+)_linux_(?P<arch>amd64|arm64)\.tar\.gz$")
MAX_METADATA_BYTES = 1 << 20


def sha256(path):
    digest = hashlib.sha256()
    with path.open("rb") as stream:
        for chunk in iter(lambda: stream.read(64 << 10), b""):
            digest.update(chunk)
    return digest.hexdigest()


def extract_agent(archive, destination):
    with tarfile.open(archive, "r:gz") as bundle:
        matches = [member for member in bundle.getmembers() if member.name == "beat-agent"]
        if len(matches) != 1 or not matches[0].isfile() or matches[0].issparse():
            raise ValueError(f"{archive.name}: exactly one regular beat-agent is required")
        source = bundle.extractfile(matches[0])
        if source is None or matches[0].size <= 0 or matches[0].size > 512 << 20:
            raise ValueError(f"{archive.name}: beat-agent is missing or oversized")
        descriptor = os.open(destination, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
        with source, os.fdopen(descriptor, "wb") as output:
            shutil.copyfileobj(source, output, length=64 << 10)
            output.flush()
            os.fsync(output.fileno())
    destination.chmod(0o700)


def required_environment(name):
    value = os.environ.get(name, "").strip()
    if not value:
        raise ValueError(f"{name} is required")
    return value


def prepare(directory):
    tag = required_environment("GITHUB_REF_NAME")
    if not tag.startswith("v"):
        raise ValueError("release tag must start with v")
    version = tag[1:]
    now = datetime.now(timezone.utc).replace(microsecond=0)
    checksums = directory / "checksums.txt"
    if not checksums.is_file() or checksums.is_symlink():
        raise ValueError("GoReleaser checksums.txt is required")
    archives = sorted(directory.glob("beat_*_linux_*.tar.gz"))
    found = {}
    for archive in archives:
        match = ARCHIVE.fullmatch(archive.name)
        if match is None or match.group("version") != version or archive.is_symlink():
            raise ValueError(f"unexpected release archive: {archive.name}")
        arch = match.group("arch")
        if arch in found:
            raise ValueError(f"duplicate archive for linux/{arch}")
        found[arch] = archive
    if set(found) != {"amd64", "arm64"}:
        raise ValueError("linux amd64 and arm64 archives are required")

    notes = directory / "beat-agent-release-notes.txt"
    notes.write_text(
        f"Beat Agent {tag}\n\nCommit: {required_environment('GITHUB_SHA')}\n"
        f"Repository: {required_environment('GITHUB_REPOSITORY')}\n",
        encoding="utf-8",
    )
    artifacts = []
    provenance = sha256(checksums)
    for arch, archive in found.items():
        filename = f"beat-agent_linux_{arch}"
        destination = directory / filename
        if destination.exists() or destination.is_symlink():
            destination.unlink()
        extract_agent(archive, destination)
        sbom = Path(str(archive) + ".sbom.json")
        if not sbom.is_file() or sbom.is_symlink():
            raise ValueError(f"matching SBOM is missing for {archive.name}")
        artifacts.append({
            "id": f"linux-{arch}", "os": "linux", "arch": arch, "kind": "binary",
            "path": filename, "filename": filename, "entrypoint": filename,
            "support": "supported",
            "sbom": {"path": sbom.name}, "provenance": {"digest": provenance},
        })

    spec = {
        "version": version,
        "commit": required_environment("GITHUB_SHA"),
        "channel": "preview" if "-" in version else "stable",
        "built_at": now.isoformat().replace("+00:00", "Z"),
        "expires_at": (now + timedelta(days=90)).isoformat().replace("+00:00", "Z"),
        "minimum_server_schema": 15,
        "minimum_protocol": 1,
        "maximum_protocol": 2,
        "signing_key_id": os.environ.get("BEAT_RELEASE_KEY_ID", "").strip() or "beat-release",
        "release_notes": {"path": notes.name},
        "artifacts": artifacts,
    }
    (directory / "beat-agent-release-spec.json").write_text(
        json.dumps(spec, indent=2) + "\n", encoding="utf-8"
    )


def extract_signed(directory):
    source = directory / "beat-agent-signed.json"
    document = json.loads(source.read_text(encoding="utf-8"))
    import base64
    payload = base64.b64decode(document["payload"], validate=True)
    signature = base64.b64decode(document["signature"], validate=True)
    if len(payload) > MAX_METADATA_BYTES or len(signature) != 64:
        raise ValueError("signed manifest payload or Ed25519 signature is invalid")
    (directory / "beat-agent-manifest.json").write_bytes(payload)
    (directory / "beat-agent-manifest.sig").write_bytes(signature)
    source.unlink()


def main(arguments):
    if len(arguments) != 2 or arguments[0] not in {"prepare", "extract"}:
        raise ValueError("usage: prepare-agent-release.py <prepare|extract> <dist-directory>")
    directory = Path(arguments[1])
    if not directory.is_dir() or directory.is_symlink():
        raise ValueError("release directory must be a real directory")
    if arguments[0] == "prepare":
        prepare(directory)
    else:
        extract_signed(directory)


if __name__ == "__main__":
    try:
        main(sys.argv[1:])
    except (OSError, ValueError, KeyError, json.JSONDecodeError, tarfile.TarError) as error:
        print(f"agent release preparation failed: {error}", file=sys.stderr)
        raise SystemExit(1)
