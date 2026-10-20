"""soak.py 的定向用例：口令、HTTP 采样、systemd/RSS、容量、统计、报告与整轮运行。"""

import contextlib
import importlib.util
import io
import json
import os
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
import socket
import sys
import tempfile
import threading
import types
import unittest
from unittest import mock

from deployment_fixture import PROJECT, private_file

SOAK = PROJECT / "scripts" / "soak.py"
# 回一个活着的 PID，让采样器能在 /proc/<pid>/status 上读到真实 VmRSS。
LIVE_PID_SCRIPT = '#!/bin/sh\nprintf "%s\\n" "$PPID"\n'


def load_soak():
    spec = importlib.util.spec_from_file_location("beat_soak_under_test", SOAK)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


class StubConsole:
    def __init__(self):
        self.calls = []

    def call(self, method, path, payload=None):
        self.calls.append((method, path, payload))
        return 200, {"data": {}}


class _Handler(BaseHTTPRequestHandler):
    responses = {}

    def do_GET(self):
        self._respond()

    def do_POST(self):
        length = int(self.headers.get("Content-Length") or 0)
        if length:
            self.rfile.read(length)
        self._respond()

    def _respond(self):
        code, body = self.responses.get(self.path, (404, {"error": "not found"}))
        payload = body if isinstance(body, bytes) else json.dumps(body).encode()
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(payload)))
        self.end_headers()
        if payload:
            self.wfile.write(payload)

    def log_message(self, *_args):
        return


class SoakFixture(unittest.TestCase):
    def setUp(self):
        directory = tempfile.TemporaryDirectory(prefix="beat-soak-test.")
        self.addCleanup(directory.cleanup)
        self.root = Path(directory.name)
        self.soak = load_soak()

    def start_server(self, responses):
        handler = type("_BoundHandler", (_Handler,), {"responses": responses})
        server = ThreadingHTTPServer(("127.0.0.1", 0), handler)
        threading.Thread(target=server.serve_forever, daemon=True).start()
        self.addCleanup(server.shutdown)
        self.addCleanup(server.server_close)
        return f"http://127.0.0.1:{server.server_address[1]}"

    def closed_port_base(self):
        with socket.socket() as probe:
            probe.bind(("127.0.0.1", 0))
            return f"http://127.0.0.1:{probe.getsockname()[1]}"

    def fake_systemctl(self, script=LIVE_PID_SCRIPT):
        bin_dir = self.root / "bin"
        private_file(bin_dir / "systemctl", script, True)
        return mock.patch.dict(os.environ, {"PATH": f"{bin_dir}:{os.environ['PATH']}"})

    def write_private(self, name, content):
        path = self.root / name
        private_file(path, content)
        return path


class PasswordTests(SoakFixture):
    def test_read_password_accepts_secret_and_access_json(self):
        self.assertEqual("s3cret", self.soak.read_password(str(self.write_private("secret", "s3cret\n"))))
        access = self.write_private("access.json", json.dumps({"username": "preview", "password": "pw"}))
        self.assertEqual("pw", self.soak.read_password(str(access)))

    def test_read_password_rejects_unsafe_empty_and_broken_files(self):
        world = self.write_private("world", "pw")
        os.chmod(world, 0o644)
        with self.assertRaises(SystemExit):
            self.soak.read_password(str(world))
        for index, content in enumerate(("", "{broken", "{}")):
            path = self.write_private(f"bad-{index}", content)
            with self.assertRaises(SystemExit):
                self.soak.read_password(str(path))


class ConsoleTests(SoakFixture):
    def test_call_reads_json_bodies_and_survives_raw_bodies(self):
        base = self.start_server({"/api/v1/probe/ok": (200, {"data": {"value": 1}}),
                                  "/api/v1/probe/raw": (500, b"<html>boom</html>"),
                                  "/api/v1/probe/empty": (204, b"")})
        console = self.soak.Console(base, "preview", "pw")
        self.assertEqual((200, {"data": {"value": 1}}), console.call("GET", "/probe/ok"))
        self.assertEqual((500, {}), console.call("GET", "/probe/raw"))
        self.assertEqual((204, {}), console.call("GET", "/probe/empty"))

    def test_call_reports_transport_failures(self):
        console = self.soak.Console(self.closed_port_base(), "preview", "pw")
        status, body = console.call("POST", "/metrics/query", {"series": []})
        self.assertEqual(0, status)
        self.assertIn("error", body)

    def test_login_accepts_and_rejects_credentials(self):
        self.soak.Console(self.start_server({"/api/v1/auth/login": (200, {"data": {}})}), "preview", "pw").login()
        denied = self.start_server({"/api/v1/auth/login": (401, {"error": "invalid credentials"})})
        with self.assertRaises(SystemExit):
            self.soak.Console(denied, "preview", "pw").login()


class SamplingTests(SoakFixture):
    def test_ready_status_reads_codes(self):
        self.assertEqual(200, self.soak.ready_status(self.start_server({"/readyz": (200, {"status": "ready"})})))
        self.assertEqual(503, self.soak.ready_status(self.start_server({"/readyz": (503, {"status": "not_ready"})})))
        self.assertEqual(0, self.soak.ready_status(self.closed_port_base()))

    def test_unit_pid_parses_systemctl_output(self):
        with self.fake_systemctl():
            self.assertEqual(os.getpid(), self.soak.unit_pid("beat-preview-server.service"))
        for script in ('#!/bin/sh\nprintf "0\\n"\n', '#!/bin/sh\nprintf "nope\\n"\n', '#!/bin/sh\nexit 1\n'):
            with self.fake_systemctl(script):
                self.assertIsNone(self.soak.unit_pid("beat-preview-server.service"))

    def test_process_rss_reads_proc_status(self):
        self.assertIsNone(self.soak.process_rss_kib(None))
        self.assertIsNone(self.soak.process_rss_kib(999999999))
        value = self.soak.process_rss_kib(os.getpid())
        self.assertIsInstance(value, int)
        self.assertGreater(value, 0)


class CapacityTests(SoakFixture):
    def test_capacity_and_database_bytes(self):
        state = self.root / "state"
        (state / "data").mkdir(parents=True)
        private_file(state / "beat.db", "x" * 10)
        private_file(state / "beat.db-wal", "y" * 5)
        private_file(state / "data" / "beat.db", "z" * 3)
        [entry] = self.soak.capacity([str(state)])
        self.assertEqual(18, entry["database_bytes"])
        self.assertGreater(entry["total_bytes"], 0)
        self.assertGreaterEqual(entry["used_percent"], 0)
        [missing] = self.soak.capacity([str(self.root / "missing")])
        self.assertEqual("FileNotFoundError", missing["error"])
        self.assertEqual(1, len(self.soak.capacity([])))


class LoadTests(SoakFixture):
    def test_load_once_probes_every_endpoint(self):
        console = StubConsole()
        statuses = self.soak.load_once(console, "node-1")
        self.assertEqual({"report": 200, "latest": 200, "catalog": 200, "nodes": 200, "runtime": 200}, statuses)
        self.assertEqual(["/metrics/query", "/metrics/query/latest", "/metrics/catalog", "/nodes", "/admin/runtime"],
                         [call[1] for call in console.calls])
        self.assertEqual(100, console.calls[0][2]["max_points"])


class StatisticsTests(SoakFixture):
    def test_rss_statistics_handles_empty_and_windowed_samples(self):
        self.assertEqual({}, self.soak.rss_statistics([], 60.0, 60.0))
        self.assertEqual({}, self.soak.rss_statistics([{"at_seconds": 1.0, "server_rss_kib": None}], 0.0, 10.0))
        samples = [{"at_seconds": float(index), "server_rss_kib": 100 + index} for index in range(10)]
        stats = self.soak.rss_statistics(samples, 0.0, 2.0)
        self.assertEqual(10, stats["samples"])
        self.assertEqual(101, stats["baseline_kib"])
        self.assertEqual(108, stats["final_kib"])
        self.assertEqual(109, stats["peak_kib"])
        self.assertEqual(7, stats["growth_kib"])
        fallback = self.soak.rss_statistics(samples, 1000.0, 2.0)
        self.assertEqual(104, fallback["baseline_kib"])


class ReportTests(SoakFixture):
    def soak_args(self, **overrides):
        values = {"base": "http://127.0.0.1:1", "username": "preview", "minutes": 1.0, "interval": 1.0,
                  "warmup_minutes": 0.0, "rss_window_minutes": 1.0, "growth_limit": 0.10,
                  "rss_ceiling_mib": 256.0, "server_unit": "unit", "agent_unit": "unit",
                  "capacity_path": [str(self.root)], "report": str(self.root / "report.json")}
        values.update(overrides)
        return types.SimpleNamespace(**values)

    def samples(self, first, last, count=2):
        step = (last - first) / (count - 1)
        return [{"at_seconds": 60.0 * index, "readyz": 200, "server_rss_kib": first + step * index,
                 "agent_rss_kib": 500, "statuses": {}} for index in range(count)]

    def test_build_report_passes_clean_sampling(self):
        report = self.soak.build_report(self.soak_args(), StubConsole(), self.samples(1000, 1050), [], 0, "node-1", 0.0)
        self.assertTrue(all(check["ok"] for check in report["checks"]), report["checks"])
        self.assertEqual("node-1", report["node_id"])
        self.assertEqual(60.0, report["checks"][3]["detail"])

    def test_build_report_flags_failures_growth_and_ceiling(self):
        dirty = self.soak.build_report(self.soak_args(), StubConsole(), self.samples(1000, 1400, count=11),
                                       [["readyz", 503, 1], ["report", 500, 2]], 9, "node-1", 0.0)
        flags = {check["name"]: check["ok"] for check in dirty["checks"]}
        self.assertFalse(flags["readyz_clean"])
        self.assertFalse(flags["no_server_errors"])
        self.assertFalse(flags["rate_limit_within_tolerance"])
        self.assertFalse(flags["rss_growth_within_limit"])
        self.assertTrue(flags["rss_growth_within_ceiling"])
        heavy = self.soak.build_report(self.soak_args(), StubConsole(), self.samples(1000, 300000, count=11), [], 0,
                                       "node-1", 0.0)
        self.assertFalse(heavy["checks"][5]["ok"])

    def test_write_report_creates_private_files(self):
        nested = self.root / "nested" / "report.json"
        self.soak.write_report(str(nested), {"checks": []})
        self.assertEqual(0o600, nested.stat().st_mode & 0o777)
        self.assertEqual({"checks": []}, json.loads(nested.read_text()))
        flat = self.root / "flat.json"
        os.chdir(self.root)
        self.addCleanup(os.chdir, PROJECT)
        self.soak.write_report("flat.json", {"checks": [1]})
        self.assertEqual({"checks": [1]}, json.loads(flat.read_text()))

    def test_parse_args_defaults_and_overrides(self):
        with mock.patch.object(sys, "argv", ["soak.py", "--password-file", "pw"]):
            args = self.soak.parse_args()
        self.assertEqual(1440.0, args.minutes)
        self.assertEqual([], args.capacity_path)
        with mock.patch.object(sys, "argv", ["soak.py", "--password-file", "pw", "--base", "http://example",
                                             "--minutes", "2", "--capacity-path", "/a", "--capacity-path", "/b"]):
            args = self.soak.parse_args()
        self.assertEqual(("http://example", 2.0, ["/a", "/b"]), (args.base, args.minutes, args.capacity_path))


class RunTests(SoakFixture):
    def responses(self, ready=200):
        return {"/readyz": (ready, {"status": "ready" if ready == 200 else "not_ready"}),
                "/api/v1/auth/login": (200, {"data": {}}),
                "/api/v1/nodes": (200, {"data": [{"id": "node-1"}]}),
                "/api/v1/metrics/query": (200, {"data": {"state": "ok"}}),
                "/api/v1/metrics/query/latest": (200, {"data": {"state": "ok"}}),
                "/api/v1/metrics/catalog": (200, {"data": []}),
                "/api/v1/admin/runtime": (200, {"data": {"workers": []}})}

    def run_soak(self, base):
        password = self.write_private("secret", "pw")
        report = self.root / "report.json"
        argv = ["soak.py", "--base", base, "--password-file", str(password), "--minutes", "0.02", "--interval", "0.01",
                "--warmup-minutes", "0", "--rss-window-minutes", "0.02", "--report", str(report),
                "--capacity-path", str(self.root)]
        with self.fake_systemctl(), mock.patch.object(sys, "argv", argv), contextlib.redirect_stdout(io.StringIO()) as out:
            code = self.soak.main()
        return code, json.loads(report.read_text()), out.getvalue()

    def test_main_runs_a_short_soak_and_writes_a_passing_report(self):
        code, report, stdout = self.run_soak(self.start_server(self.responses()))
        self.assertEqual(0, code)
        self.assertEqual("node-1", report["node_id"])
        self.assertGreater(len(report["samples"]), 3)
        self.assertTrue(all(check["ok"] for check in report["checks"]), report["checks"])
        self.assertEqual(0, report["rate_limited"])
        self.assertIn("t=", stdout)

    def test_main_returns_failure_when_the_instance_is_unhealthy(self):
        code, report, _ = self.run_soak(self.start_server(self.responses(ready=503)))
        self.assertEqual(1, code)
        flags = {check["name"]: check["ok"] for check in report["checks"]}
        self.assertFalse(flags["readyz_clean"])
        self.assertTrue(report["failures"])


if __name__ == "__main__":
    unittest.main()
