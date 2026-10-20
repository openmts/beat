import io
import json
import os
from pathlib import Path
import tarfile
import unittest

from deployment_fixture import DeploymentFixture, PROJECT, private_file


class DeploymentTests(DeploymentFixture):
    def test_static_account_and_read_only_program_tree(self):
        template = (PROJECT / "deploy/systemd/beat-server.service").read_text()
        self.assertIn("User=beat\n", template)
        self.assertIn("Group=beat\n", template)
        self.assertNotIn("DynamicUser=yes", template)
        self.assertIn("ReadOnlyPaths=/opt/beat", template)
        self.assertIn("ProtectSystem=strict", template)

    def test_failed_release_preparation_does_not_stop_existing_service(self):
        original = self.installed()
        result = self.run_script("upgrade", self.release("2.0.0"), "2.0.0", BEAT_TEST_FAIL_COPY="yes")
        self.assertNotEqual(0, result.returncode)
        self.assert_not_stopped()
        self.assertEqual(original, self.prefix.joinpath("current").resolve())

    def test_failed_upgrade_restores_matching_binary_and_data(self):
        original = self.installed()
        result = self.run_script("upgrade", self.release("2.0.0"), "2.0.0", BEAT_TEST_FAIL_START="2.0.0")
        self.assertNotEqual(0, result.returncode)
        self.assertEqual(original, self.prefix.joinpath("current").resolve(), result.stderr)
        self.assertEqual("database-1.0.0", self.data.joinpath("beat.db").read_text())
        self.assertEqual("metrics-1.0.0", self.data.joinpath("beat_mts/points").read_text())
        self.assertTrue(self.root.joinpath("active").exists())

    def test_unsafe_restore_is_rejected_before_downtime(self):
        self.installed()
        archive = self.root / "unsafe.tar.gz"
        with tarfile.open(archive, "w:gz") as stream:
            entry = tarfile.TarInfo("../outside")
            entry.size = 4
            stream.addfile(entry, io.BytesIO(b"test"))
        result = self.run_script("rollback", "1.0.0", archive)
        self.assertNotEqual(0, result.returncode)
        self.assert_not_stopped()
        self.assertEqual("database-1.0.0", self.data.joinpath("beat.db").read_text())

    def test_unsafe_existing_account_is_not_adopted(self):
        self.installed()
        private_file(self.root / "account.json", json.dumps(self.account(uid=0)))
        result = self.run_script("upgrade", self.release("2.0.0"), "2.0.0")
        self.assertNotEqual(0, result.returncode)
        self.assert_not_stopped()


if __name__ == "__main__":
    unittest.main()
