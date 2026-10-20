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
                        GITHUB_REF="refs/tags/v1.0.0", BEAT_SIGNER_LOG=str(self.log))
        self.archive = self.dist / "beat_1.0.0_linux_amd64.tar.gz"
        with tarfile.open(self.archive, "w:gz") as stream:
            for name in ("beat-server", "beat-agent", "beat-cli", "beat-theme", "static/index.html",
                      "scripts/beatctl.sh", "scripts/state_archive.py",
                      "deploy/systemd/beat-server.service"):
                member = tarfile.TarInfo(name)
                member.size = 4
                stream.addfile(member, io.BytesIO(b"data"))
        private_file(Path(str(self.archive) + ".sbom.json"), json.dumps({"spdxVersion": "SPDX-2.3"}))
        digest = hashlib.sha256(self.archive.read_bytes()).hexdigest()
        private_file(self.dist / "checksums.txt", f"{digest}  {self.archive.name}\n")

    def prepare(self, **extra):
        return run_shell(PROJECT / "scripts/prepare-release.sh", [self.dist], dict(self.env, **extra))

    def test_signature_is_verified_before_becoming_publishable(self):
        result = self.prepare()
        self.assertEqual(0, result.returncode, result.stderr)
        self.assertEqual(["sign-blob", "verify-blob"], self.log.read_text().splitlines())
        self.assertTrue(self.dist.joinpath("checksums.txt.bundle").is_file())
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
        Path(str(self.archive) + ".sbom.json").unlink()
        result = self.prepare()
        self.assertNotEqual(0, result.returncode)
        self.assertFalse(self.log.exists())
        self.archive.write_bytes(b"changed")
        result = self.prepare()
        self.assertNotEqual(0, result.returncode)
        self.assertFalse(self.log.exists())


if __name__ == "__main__":
    unittest.main()
