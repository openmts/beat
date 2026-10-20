import base64
import hashlib
import io
import json
import os
from pathlib import Path
import subprocess
import tarfile
import tempfile
import unittest

from deployment_fixture import PROJECT, private_file, run_shell


class ReleasePreparationTests(unittest.TestCase):
    def setUp(self):
        directory = tempfile.TemporaryDirectory(prefix="beat-release-preparation-test.")
        self.addCleanup(directory.cleanup)
        self.root = Path(directory.name)
        self.dist = self.root / "dist"
        self.dist.mkdir(mode=0o700)
        self.bin = self.root / "bin"
        self.log = self.root / "signer.log"
        signer = '#!/bin/bash\nprintf "%s\\n" "$1" >>"$BEAT_SIGNER_LOG"\n[[ ${BEAT_SIGNER_FAIL:-} != "$1" ]] || exit 1\nif [[ "$1" == sign-blob ]]; then printf "{}" >"$4"; fi\n'
        private_file(self.bin / "cosign", signer, True)
        self.env = dict(os.environ, PATH=f"{self.bin}:{os.environ['PATH']}", GITHUB_REPOSITORY="example/beat",
                        GITHUB_REF="refs/tags/v1.0.0", GITHUB_REF_NAME="v1.0.0",
                        GITHUB_SHA="a" * 40, BEAT_SIGNER_LOG=str(self.log),
                        BEAT_RELEASE_SIGNING_KEY=base64.b64encode(bytes(range(32))).decode(),
                        BEAT_RELEASE_KEY_ID="fixture-release")
        self.archives = []
        for arch in ("amd64", "arm64"):
            archive = self.dist / f"beat_1.0.0_linux_{arch}.tar.gz"
            with tarfile.open(archive, "w:gz") as stream:
                for name in ("beat-server", "beat-agent", "beat-cli", "beat-theme", "static/index.html",
                          "scripts/beatctl.sh", "scripts/state_archive.py",
                          "deploy/systemd/beat-server.service"):
                    member = tarfile.TarInfo(name)
                    member.size = 4
                    stream.addfile(member, io.BytesIO(f"{arch}-agent".encode() if name == "beat-agent" else b"data"))
            private_file(Path(str(archive) + ".sbom.json"), json.dumps({"spdxVersion": "SPDX-2.3"}))
            self.archives.append(archive)
        checksums = "".join(f"{hashlib.sha256(archive.read_bytes()).hexdigest()}  {archive.name}\n" for archive in self.archives)
        private_file(self.dist / "checksums.txt", checksums)

    def prepare(self, **extra):
        return run_shell(PROJECT / "scripts/prepare-release.sh", [self.dist], dict(self.env, **extra))

    def test_signature_is_verified_before_becoming_publishable(self):
        result = self.prepare()
        self.assertEqual(0, result.returncode, result.stderr)
        self.assertEqual(["sign-blob", "verify-blob", "sign-blob", "verify-blob"], self.log.read_text().splitlines())
        for name in ("beat-agent-manifest.json", "beat-agent-manifest.sig", "beat-agent-manifest.sigstore.json",
                     "beat-agent_linux_amd64", "beat-agent_linux_arm64", "checksums.txt.bundle"):
            self.assertTrue(self.dist.joinpath(name).is_file(), name)
        self.assertEqual(64, self.dist.joinpath("beat-agent-manifest.sig").stat().st_size)
        self.assertIn(".sbom.json", self.dist.joinpath("checksums.txt").read_text())
        self.assertFalse(list(self.dist.glob(".release-prepare.*")))

    def test_signature_failure_cannot_leave_a_publishable_bundle(self):
        for operation in ("sign-blob", "verify-blob"):
            with self.subTest(operation=operation):
                original = self.dist.joinpath("checksums.txt").read_bytes()
                result = self.prepare(BEAT_SIGNER_FAIL=operation)
                self.assertNotEqual(0, result.returncode)
                self.assertFalse(self.dist.joinpath("checksums.txt.bundle").exists())
                self.assertEqual(original, self.dist.joinpath("checksums.txt").read_bytes())
                self.assertFalse(list(self.dist.glob(".release-prepare.*")))

    def test_missing_sbom_or_changed_archive_never_reaches_signer(self):
        Path(str(self.archives[0]) + ".sbom.json").unlink()
        result = self.prepare()
        self.assertNotEqual(0, result.returncode)
        self.assertFalse(self.log.exists())
        self.archives[0].write_bytes(b"changed")
        result = self.prepare()
        self.assertNotEqual(0, result.returncode)
        self.assertFalse(self.log.exists())


if __name__ == "__main__":
    unittest.main()
