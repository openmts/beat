import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest

from deployment_fixture import PROJECT, private_file, run_shell


class QualityGateTests(unittest.TestCase):
    def setUp(self):
        directory = tempfile.TemporaryDirectory(prefix="beat-quality-gate-test.")
        self.addCleanup(directory.cleanup)
        self.root = Path(directory.name)
        self.bin = self.root / "bin"
        self.log = self.root / "calls"
        self.temporary = self.root / "temporary"
        self.temporary.mkdir(mode=0o700)
        for command in ("go", "goimports-reviser", "golangci-lint", "govulncheck", "npm", "node", "docker", "actionlint", "python3", "sleep"):
            private_file(self.bin / command, f'#!/bin/sh\nexec /usr/bin/python3 "{PROJECT}/scripts/tests/fake_gate.py" {command} "$@"\n', True)
        self.environment = dict(os.environ, PATH=f"{self.bin}:{os.environ['PATH']}",
                                BEAT_GATE_LOG=str(self.log), TMPDIR=str(self.temporary))

    def run_gate(self, stage="all", **extra):
        return run_shell(PROJECT / "scripts/check.sh", [stage], dict(self.environment, **extra))

    def calls(self):
        return [json.loads(line) for line in self.log.read_text().splitlines()]

    def test_all_stages_use_the_complete_gate_without_requiring_clean_git(self):
        result = self.run_gate()
        self.assertEqual(0, result.returncode, result.stderr)
        calls = self.calls()
        self.assertIn(["go", "mod", "tidy", "-diff"], calls)
        # A19 起 race 段改成限并发 + 显式超时，断言跟随门禁脚本的真实参数。
        self.assertIn(["go", "test", "-race", "-shuffle=on", "-p", "2", "-timeout", "20m", "./..."], calls)
        self.assertIn(["govulncheck", "./..."], calls)
        self.assertIn(["npm", "audit", "--registry=https://registry.npmjs.org", "--omit=dev", "--audit-level=high"], calls)
        formatter = next(call for call in calls if call[0] == "goimports-reviser")
        self.assertIn("-set-exit-status", formatter)
        self.assertIn("stdout", formatter)
        self.assertFalse(any(call[0] == "git" for call in calls))
        self.assertEqual([], list(self.temporary.iterdir()))

    def test_backend_failure_prevents_build_and_later_stages(self):
        result = self.run_gate(BEAT_GATE_FAIL="govulncheck")
        self.assertNotEqual(0, result.returncode)
        self.assertFalse(any(call[:2] == ["go", "build"] or call[0] == "npm" for call in self.calls()))
        self.assertEqual([], list(self.temporary.iterdir()))

    def test_insufficient_coverage_fails_before_lint_and_build(self):
        result = self.run_gate("backend", BEAT_GATE_COVERAGE="89.9%")
        self.assertNotEqual(0, result.returncode)
        self.assertFalse(any(call[0] == "golangci-lint" for call in self.calls()))

    def test_frontend_audit_failure_prevents_build(self):
        result = self.run_gate("frontend", BEAT_GATE_FAIL="npm audit")
        self.assertNotEqual(0, result.returncode)
        self.assertFalse(any(call[:3] == ["npm", "run", "build"] for call in self.calls()))

    def test_unhealthy_container_is_removed_without_publishing(self):
        result = self.run_gate("container", BEAT_GATE_UNHEALTHY="yes")
        self.assertNotEqual(0, result.returncode)
        self.assertIn(["docker", "rm", "--force", "isolated-gate-container"], self.calls())
        self.assertTrue(any(call[:3] == ["docker", "image", "rm"] for call in self.calls()))
        self.assertFalse(any("push" in call for call in self.calls()))

    def test_frontend_gate_runs_the_theme_browser_smokes(self):
        result = self.run_gate("frontend")
        self.assertEqual(0, result.returncode, result.stderr)
        calls = self.calls()
        self.assertIn(["node", "node_modules/playwright/cli.js", "install", "--only-shell", "chromium"], calls)
        smokes = [call[1] for call in calls if call[0] == "node" and len(call) > 1
                  and str(call[1]).startswith("scripts/theme-") and str(call[1]).endswith("-smoke.mjs")]
        self.assertEqual(["scripts/theme-declarative-smoke.mjs", "scripts/theme-sandbox-smoke.mjs",
                          "scripts/theme-market-smoke.mjs"], smokes)

    def test_missing_theme_asset_fails_the_frontend_gate(self):
        result = self.run_gate("frontend", BEAT_GATE_MISSING_THEME="slate")
        self.assertNotEqual(0, result.returncode)
        self.assertIn("missing theme archive: slate", result.stderr)
        self.assertTrue(any(call[:3] == ["npm", "run", "build"] for call in self.calls()))
        self.assertEqual([], list(self.temporary.iterdir()))


if __name__ == "__main__":
    unittest.main()
