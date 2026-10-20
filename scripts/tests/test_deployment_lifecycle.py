import json
import hashlib
import os
from pathlib import Path
import subprocess
import tarfile
import unittest

from deployment_fixture import DeploymentFixture, private_file


class DeploymentLifecycleTests(DeploymentFixture):
    def test_install_uses_private_static_account_files(self):
        result = self.run_script("install", self.release("1.0.0"), "1.0.0")
        self.assertEqual(0, result.returncode, result.stderr)
        target = self.prefix / "releases/1.0.0"
        self.assertEqual(target, self.prefix.joinpath("current").resolve())
        self.assertEqual(self.uid, target.joinpath("beat-server").stat().st_uid)
        self.assertEqual(0o700, target.stat().st_mode & 0o777)
        self.assertEqual(0o600, target.joinpath("static/index.html").stat().st_mode & 0o777)
        self.assertEqual(0o600, self.units.joinpath("beat-server.service").stat().st_mode & 0o777)
        unit = self.units.joinpath("beat-server.service").read_text()
        self.assertIn(f"ReadOnlyPaths={self.prefix}", unit)
        self.assertIn(f"ReadWritePaths={self.data}", unit)
        lock = self.data.parent / f".beat-lifecycle-{hashlib.sha256(str(self.data.resolve()).encode()).hexdigest()}.lock"
        self.assertIn(f"ReadWritePaths={lock}", unit)
        self.assertEqual(self.uid, lock.stat().st_uid)
        self.assertEqual(0o600, lock.stat().st_mode & 0o777)

    @unittest.skipUnless(os.geteuid() == 0, "separate UID execution needs root in this isolated test")
    def test_published_binary_can_run_as_the_service_uid(self):
        result = self.run_script("install", self.release("1.0.0"), "1.0.0")
        self.assertEqual(0, result.returncode, result.stderr)
        def drop_identity():
            os.setgroups([])
            os.setgid(self.gid)
            os.setuid(self.uid)
        executed = subprocess.run([str(self.prefix / "current/beat-server"), "--version"],
                                  preexec_fn=drop_identity, env={"PATH": "/usr/bin:/bin"}, capture_output=True, text=True, check=False)
        self.assertEqual(0, executed.returncode, executed.stderr)
        self.assertIn("1.0.0", executed.stdout)

    def test_successful_upgrade_retains_a_cold_recovery_checkpoint(self):
        self.installed()
        result = self.run_script("upgrade", self.release("2.0.0"), "2.0.0")
        self.assertEqual(0, result.returncode, result.stderr)
        self.assertEqual("2.0.0", self.prefix.joinpath("current").resolve().name)
        archives = list(self.backups.glob("*/state.tar.gz"))
        self.assertEqual(1, len(archives))
        self.assertEqual(0o600, archives[0].stat().st_mode & 0o777)
        self.assertIn("1.0.0", archives[0].parent.joinpath("previous-target").read_text())
        self.assertEqual("database-1.0.0", self.data.joinpath("beat.db").read_text())

    def test_failed_readiness_restores_old_unit_and_data(self):
        original = self.installed()
        previous_unit = self.units.joinpath("beat-server.service").read_bytes()
        result = self.run_script("upgrade", self.release("2.0.0"), "2.0.0", BEAT_TEST_FAIL_READY="2.0.0")
        self.assertNotEqual(0, result.returncode)
        self.assertEqual(original, self.prefix.joinpath("current").resolve())
        self.assertEqual(previous_unit, self.units.joinpath("beat-server.service").read_bytes())
        self.assertEqual("database-1.0.0", self.data.joinpath("beat.db").read_text())
        self.assertTrue(self.root.joinpath("active").exists())

    def test_failed_stop_does_not_replace_current_files(self):
        original = self.installed()
        unit = self.units.joinpath("beat-server.service").read_bytes()
        result = self.run_script("upgrade", self.release("2.0.0"), "2.0.0", BEAT_TEST_FAIL_SYSTEMCTL="stop")
        self.assertNotEqual(0, result.returncode)
        self.assertEqual(original, self.prefix.joinpath("current").resolve())
        self.assertEqual(unit, self.units.joinpath("beat-server.service").read_bytes())
        self.assertEqual("database-1.0.0", self.data.joinpath("beat.db").read_text())

    def test_interactive_or_shared_accounts_are_rejected_before_stopping(self):
        self.installed()
        for change in (dict(shell="/bin/bash"), dict(shell="/tmp/nologin"), dict(comment="Another service")):
            private_file(self.root / "account.json", json.dumps(self.account(**change)))
            result = self.run_script("upgrade", self.release("2.0.0"), "2.0.0")
            self.assertNotEqual(0, result.returncode)
            self.assert_not_stopped()
        private_file(self.root / "account.json", json.dumps(self.account()))
        for change in (dict(BEAT_TEST_OTHER_USER="beat"), dict(BEAT_TEST_PROCESS_UNIT="other.service"), dict(BEAT_TEST_GROUPS="beat sudo")):
            result = self.run_script("upgrade", self.release("2.0.0"), "2.0.0", **change)
            self.assertNotEqual(0, result.returncode)
            self.assert_not_stopped()

    def test_unit_symlink_and_invalid_version_are_not_overwritten(self):
        self.installed()
        self.units.joinpath("beat-server.service").unlink()
        victim = self.root / "victim"
        private_file(victim, "preserved")
        self.units.joinpath("beat-server.service").symlink_to(victim)
        result = self.run_script("upgrade", self.release("2.0.0"), "../escape")
        self.assertNotEqual(0, result.returncode)
        self.assertEqual("preserved", victim.read_text())
        self.assert_not_stopped()

    def test_first_install_failure_does_not_leave_a_runnable_unit(self):
        result = self.run_script("install", self.release("2.0.0"), "2.0.0", BEAT_TEST_FAIL_START="2.0.0")
        self.assertNotEqual(0, result.returncode)
        self.assertFalse(self.units.joinpath("beat-server.service").exists())
        self.assertFalse(self.prefix.joinpath("current").exists())
        self.assertTrue(list(self.backups.glob("*/failed-data-path")))

    def test_rollback_prepares_data_before_downtime(self):
        self.installed()
        archive = self.root / "old-state.tar.gz"
        with tarfile.open(archive, "w:gz") as stream:
            stream.add(self.data, arcname=".")
        result = self.run_script("upgrade", self.release("2.0.0"), "2.0.0")
        self.assertEqual(0, result.returncode, result.stderr)
        private_file(self.data / "beat.db", "database-2.0.0")
        result = self.run_script("rollback", "1.0.0", archive)
        self.assertEqual(0, result.returncode, result.stderr)
        self.assertEqual("1.0.0", self.prefix.joinpath("current").resolve().name)
        self.assertEqual("database-1.0.0", self.data.joinpath("beat.db").read_text())

    def test_uninstall_retains_programs_data_and_account(self):
        target = self.installed()
        result = self.run_script("uninstall")
        self.assertEqual(0, result.returncode, result.stderr)
        self.assertFalse(self.units.joinpath("beat-server.service").exists())
        self.assertTrue(target.joinpath("beat-server").is_file())
        self.assertTrue(self.data.joinpath("beat.db").is_file())
        self.assertTrue(self.root.joinpath("account.json").is_file())

    def test_releases_symlink_cannot_redirect_installation(self):
        outside = self.root / "outside"
        outside.mkdir(mode=0o700)
        self.prefix.mkdir(mode=0o700)
        self.prefix.joinpath("releases").symlink_to(outside)
        result = self.run_script("install", self.release("1.0.0"), "1.0.0")
        self.assertNotEqual(0, result.returncode)
        self.assertEqual([], list(outside.iterdir()))
        self.assert_not_stopped()

    def test_unknown_unit_and_overlapping_directories_are_rejected(self):
        self.installed()
        private_file(self.units / "beat-server.service", "[Service]\nExecStart=/unrelated/program\n")
        result = self.run_script("upgrade", self.release("2.0.0"), "2.0.0")
        self.assertNotEqual(0, result.returncode)
        self.assert_not_stopped()
        result = self.run_script("upgrade", self.release("2.0.0"), "2.0.0", BEAT_BACKUP_DIR=str(self.data))
        self.assertNotEqual(0, result.returncode)
        self.assert_not_stopped()

    def test_failed_rollback_preserves_the_version_it_started_from(self):
        self.installed()
        archive = self.root / "old-state.tar.gz"
        with tarfile.open(archive, "w:gz") as stream:
            stream.add(self.data, arcname=".")
        result = self.run_script("upgrade", self.release("2.0.0"), "2.0.0")
        self.assertEqual(0, result.returncode, result.stderr)
        private_file(self.data / "beat.db", "database-2.0.0")
        private_file(self.data / "beat_mts/points", "metrics-2.0.0")
        result = self.run_script("rollback", "1.0.0", archive, BEAT_TEST_FAIL_START="1.0.0")
        self.assertNotEqual(0, result.returncode)
        self.assertEqual("2.0.0", self.prefix.joinpath("current").resolve().name)
        self.assertEqual("database-2.0.0", self.data.joinpath("beat.db").read_text())
        self.assertEqual("metrics-2.0.0", self.data.joinpath("beat_mts/points").read_text())
        self.assertTrue(self.root.joinpath("active").exists())

    def test_version_collision_is_rejected_before_downtime(self):
        target = self.installed()
        original = target.joinpath("beat-server").read_bytes()
        release = self.release("1.0.0")
        private_file(release / "beat-server", "#!/bin/sh\necho changed\n", True)
        result = self.run_script("upgrade", release, "1.0.0")
        self.assertNotEqual(0, result.returncode)
        self.assertEqual(original, target.joinpath("beat-server").read_bytes())
        self.assert_not_stopped()

    def test_first_install_generates_a_private_unprinted_bootstrap(self):
        self.config.joinpath("beat.env").unlink()
        result = self.run_script("install", self.release("1.0.0"), "1.0.0")
        self.assertEqual(0, result.returncode, result.stderr)
        environment = self.config.joinpath("beat.env").read_text()
        token = environment.splitlines()[0].split("=", 1)[1]
        self.assertEqual(64, len(token))
        self.assertNotIn(token, result.stdout)
        self.assertNotIn(token, result.stderr)
        self.assertEqual(0o600, self.config.joinpath("beat.env").stat().st_mode & 0o777)

    def test_custom_listen_address_and_account_reach_the_unit(self):
        result = self.run_script("install", self.release("1.0.0"), "1.0.0",
                                 BEAT_LISTEN_ADDR="127.0.0.1:18081", BEAT_SERVICE_ACCOUNT="beatsvc")
        self.assertEqual(0, result.returncode, result.stderr)
        unit = self.units.joinpath("beat-server.service").read_text()
        self.assertIn("User=beatsvc", unit)
        self.assertIn("Group=beatsvc", unit)
        self.assertIn(" -listen-addr 127.0.0.1:18081", unit)
        self.assertNotIn("-listen-addr :8080", unit)
        self.assertEqual("beatsvc", json.loads(self.root.joinpath("account.json").read_text())["name"])
        self.assertIn(["getent", "passwd", "beatsvc"], self.calls())
        self.assertIn(["useradd", "--system", "--user-group", "--no-create-home", "--home-dir", str(self.data),
                       "--shell", "/usr/sbin/nologin", "--comment", "Beat monitoring service", "beatsvc"], self.calls())

    def test_custom_listen_address_drives_the_readiness_probe(self):
        result = self.run_script("install", self.release("1.0.0"), "1.0.0", BEAT_LISTEN_ADDR="0.0.0.0:18081")
        self.assertEqual(0, result.returncode, result.stderr)
        probes = [call for call in self.calls() if call[0] == "curl"]
        self.assertTrue(probes, self.calls())
        self.assertIn("http://127.0.0.1:18081/readyz", probes[0])

    def test_wildcard_hosts_fall_back_to_loopback_for_readiness(self):
        for index, address in enumerate((":18081", "[::]:18081", "*:18081")):
            # 每个地址都是一次全新的部署环境：第二次安装会走进 state 归档校验，与这里要验证的映射无关。
            if index:
                self.setUp()
            with self.subTest(address=address):
                result = self.run_script("install", self.release("1.0.0"), "1.0.0", BEAT_LISTEN_ADDR=address)
                self.assertEqual(0, result.returncode, f"{address}: {result.stderr}")
                probes = [call for call in self.calls() if call[0] == "curl"]
                self.assertIn("http://127.0.0.1:18081/readyz", probes[-1], address)

    def test_unsafe_listen_address_is_rejected_before_any_change(self):
        for address in ("8080", "127.0.0.1:70000", "127.0.0.1:0", "127.0.0.1:8080 -x", "$(id):8080", "127.0.0.1:8080;"):
            result = self.run_script("install", self.release("1.0.0"), "1.0.0", BEAT_LISTEN_ADDR=address)
            self.assertNotEqual(0, result.returncode)
            self.assertFalse(self.units.joinpath("beat-server.service").exists(), address)
            self.assertNotIn("useradd", [call[0] for call in self.calls()], address)

    def test_unsafe_service_account_is_rejected_before_any_change(self):
        for account in ("Root", "beat ops", "-bad", "root"):
            result = self.run_script("install", self.release("1.0.0"), "1.0.0", BEAT_SERVICE_ACCOUNT=account)
            self.assertNotEqual(0, result.returncode)
            self.assertFalse(self.units.joinpath("beat-server.service").exists(), account)

    def test_reinstall_rejects_a_unit_from_another_configuration(self):
        result = self.run_script("install", self.release("1.0.0"), "1.0.0")
        self.assertEqual(0, result.returncode, result.stderr)
        result = self.run_script("upgrade", self.release("2.0.0"), "2.0.0", BEAT_LISTEN_ADDR="127.0.0.1:18081")
        self.assertNotEqual(0, result.returncode)
        self.assertIn("existing unit is not managed by this installer", result.stderr)

    def test_usage_does_not_change_the_machine(self):
        result = self.run_script()
        self.assertEqual(2, result.returncode)
        self.assertIn("Python 3", result.stderr)
        self.assertEqual([], self.calls())


if __name__ == "__main__":
    unittest.main()
