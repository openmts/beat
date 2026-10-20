import json
import os
from pathlib import Path
import subprocess
import tempfile
import re
import time
import unittest


PROJECT = Path(__file__).resolve().parents[2]
SCRIPT = PROJECT / "scripts/beatctl.sh"
os.umask(0o077)


def private_file(path, content, executable=False):
    path.parent.mkdir(mode=0o700, parents=True, exist_ok=True)
    mode = 0o700 if executable else 0o600
    descriptor = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, mode)
    with os.fdopen(descriptor, "w") as stream:
        stream.write(content)
    path.chmod(mode)


def run_shell(script, arguments, environment, timeout=30):
    coverage = environment.get("BEAT_SCRIPT_COVERAGE_DIR")
    if not coverage:
        return subprocess.run(["bash", str(script), *map(str, arguments)], env=environment,
                              text=True, capture_output=True, timeout=timeout)
    environment = dict(environment, PYTHONPATH=str(PROJECT / "scripts/tests/trace_hooks"))
    # 单引号保留动态位置；source 返回后 BASH_SOURCE 可能未设置。
    tracing = "PS4='+${BASH_SOURCE:-test-shell}:${LINENO}: '; set -x; source \"$1\" \"${@:2}\""
    command = ['bash', '-c', tracing, 'script-test', str(script), *map(str, arguments)]
    result = subprocess.run(command, env=environment, text=True, capture_output=True, timeout=timeout)
    hits = {}
    for source, line in re.findall(r"^\++([^:\n]+):(\d+): ", result.stderr, flags=re.MULTILINE):
        hits.setdefault(source, set()).add(int(line))
    report = Path(coverage) / f"shell-{os.getpid()}-{time.monotonic_ns()}.json"
    report.write_text(json.dumps({name: sorted(lines) for name, lines in hits.items()}))
    result.stderr = re.sub(r"^\++[^:\n]+:\d+: .*\n?", "", result.stderr, flags=re.MULTILINE)
    return result


class DeploymentFixture(unittest.TestCase):
    def setUp(self):
        self.workspace = tempfile.TemporaryDirectory(prefix="beat-deployment-test.")
        self.addCleanup(self.workspace.cleanup)
        self.root = Path(self.workspace.name)
        self.uid = os.geteuid() or 65534
        self.gid = os.getegid() if os.geteuid() else 65534
        if os.geteuid() == 0:
            os.chown(self.root, self.uid, self.gid)
        self.bin = self.root / "bin"
        self.prefix = self.root / "opt/beat"
        self.prefix.parent.mkdir(mode=0o700)
        if os.geteuid() == 0:
            os.chown(self.prefix.parent, self.uid, self.gid)
        self.data = self.root / "data"
        self.units = self.root / "units"
        self.config = self.root / "config"
        self.backups = self.root / "backups"
        self.log = self.root / "calls.jsonl"
        self.env = dict(os.environ, BEAT_PREFIX=str(self.prefix), BEAT_STATE_DIR=str(self.data),
                        BEAT_CONFIG_DIR=str(self.config), BEAT_BACKUP_DIR=str(self.backups),
                        BEAT_UNIT_DIR=str(self.units), BEAT_READY_ATTEMPTS="1", BEAT_TEST_ROOT=str(self.root),
                        BEAT_TEST_UID=str(self.uid), BEAT_TEST_GID=str(self.gid), PYTHONDONTWRITEBYTECODE="1")
        self.env["PATH"] = f"{self.bin}:{os.environ['PATH']}"
        for command in ("systemctl", "useradd", "getent", "id", "ps", "chown", "curl", "install", "rm", "sleep"):
            private_file(self.bin / command, f'#!/bin/sh\nexec /usr/bin/python3 "{PROJECT}/scripts/tests/fake_system.py" {command} "$@"\n', True)
        private_file(self.config / "beat.env", "BEAT_ADMIN_TOKEN=fixture-bootstrap-token\n")

    def release(self, version):
        release = self.root / f"release-{version}"
        for binary in ("beat-server", "beat-agent"):
            private_file(release / binary, f"#!/bin/sh\nprintf '%s\\n' '{binary} {version}'\n", True)
        private_file(release / "static/index.html", f"<html>{version}</html>")
        private_file(release / "deploy/systemd/beat-server.service", (PROJECT / "deploy/systemd/beat-server.service").read_text())
        private_file(release / "deploy/beat.env.example", "BEAT_ADMIN_TOKEN=replace-with-a-long-random-bootstrap-token\n")
        return release

    def installed(self, version="1.0.0"):
        release = self.release(version)
        target = self.prefix / "releases" / version
        target.mkdir(mode=0o700, parents=True, exist_ok=True)
        subprocess.run(["/usr/bin/cp", "-R", f"{release}/.", str(target)], check=True)
        self.prefix.joinpath("current").symlink_to(target)
        private_file(self.data / "beat.db", f"database-{version}")
        private_file(self.data / "beat_mts/points", f"metrics-{version}")
        if os.geteuid() == 0:
            for path in (self.prefix, self.data):
                os.chown(path, self.uid, self.gid)
                for child in path.rglob("*"):
                    if not child.is_symlink():
                        os.chown(child, self.uid, self.gid)
        unit = (PROJECT / "deploy/systemd/beat-server.service").read_text()
        unit = unit.replace("/opt/beat", str(self.prefix)).replace("/etc/beat", str(self.config)).replace("/var/lib/beat", str(self.data))
        private_file(self.units / "beat-server.service", unit)
        private_file(self.root / "active", "yes")
        private_file(self.root / "account.json", json.dumps(self.account()))
        return target

    def account(self, **overrides):
        values = dict(uid=self.uid, gid=self.gid, home=str(self.data), shell="/usr/sbin/nologin", comment="Beat monitoring service")
        return dict(values, **overrides)

    def run_script(self, *arguments, **environment):
        return run_shell(SCRIPT, arguments, dict(self.env, **environment))

    def calls(self):
        return [json.loads(line) for line in self.log.read_text().splitlines()] if self.log.exists() else []

    def assert_not_stopped(self):
        self.assertFalse(any(call[0] == "systemctl" and "stop" in call for call in self.calls()), self.calls())
