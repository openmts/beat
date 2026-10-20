#!/usr/bin/env python3
"""A16/B10 浸泡与容量证据：对运行中的 Beat 服务端只读加压，采样资源与健康度。

用法示例：
  python3 scripts/soak.py --base http://127.0.0.1:8081 --password-file /etc/beat-preview/access.json \
      --minutes 1440 --report /var/tmp/beat-soak/report.json

判定口径（所有者批准的 A16 建议值）：服务端 RSS 用「两端窗口中位数」比较——基线取预热结束后
第一个 `--rss-window-minutes` 窗口的中位数，终值取最后一个同长窗口的中位数，增长需同时满足
「相对值 ≤10%」与「绝对值 ≤256 MiB」两个条件。取窗口中位数而不是最小/最大单点，是为了不把 Go 堆的
GC 锯齿当成泄漏；`--rss-window-minutes` 与 `--interval` 决定每个窗口的样本数。
报告同时记录驻留峰值本身（`footprint_kib`），便于判断实例是否本来就偏大。
脚本只写报告文件，不修改目标实例的任何状态。
"""
import argparse
import http.cookiejar
import json
import os
import statistics
import subprocess
import sys
import time
import urllib.error
import urllib.request
from datetime import datetime, timedelta, timezone

DEFAULT_BASE = "http://127.0.0.1:8081"
DEFAULT_SERVER_UNIT = "beat-preview-server.service"
DEFAULT_AGENT_UNIT = "beat-preview-agent.service"
MAXIMUM_RATE_LIMITED = 5
RATE_LIMIT_STATUS = 429
REQUEST_TIMEOUT = 20.0
REPORT_MODE = 0o600


def parse_args():
    parser = argparse.ArgumentParser(description="Beat soak and capacity evidence")
    parser.add_argument("--base", default=DEFAULT_BASE)
    parser.add_argument("--username", default="preview")
    parser.add_argument("--password-file", required=True, help="0600 file holding the console password")
    parser.add_argument("--minutes", type=float, default=1440.0)
    parser.add_argument("--interval", type=float, default=15.0)
    parser.add_argument("--warmup-minutes", type=float, default=5.0)
    parser.add_argument("--rss-window-minutes", type=float, default=5.0,
                        help="median window used at the start and end of the soak")
    parser.add_argument("--growth-limit", type=float, default=0.10)
    parser.add_argument("--rss-ceiling-mib", type=float, default=256.0)
    parser.add_argument("--server-unit", default=DEFAULT_SERVER_UNIT)
    parser.add_argument("--agent-unit", default=DEFAULT_AGENT_UNIT)
    parser.add_argument("--capacity-path", action="append", default=[])
    parser.add_argument("--report", default="/var/tmp/beat-soak/report.json")
    return parser.parse_args()


def read_password(path):
    """读取控制台口令：既接受只含口令的文件，也接受预览实例的 access.json。"""
    info = os.stat(path)
    if info.st_mode & 0o077:
        raise SystemExit(f"password file must not be readable by group or others: chmod 600 {path}")
    with open(path, encoding="utf-8") as handle:
        content = handle.read().strip()
    if content.startswith("{"):
        try:
            content = str(json.loads(content).get("password", "")).strip()
        except ValueError:
            raise SystemExit(f"password file is neither a secret nor JSON: {path}")
    if not content:
        raise SystemExit("password file is empty")
    return content


class Console:
    def __init__(self, base, username, password):
        self.base = base.rstrip("/")
        self.username = username
        self.password = password
        self.jar = http.cookiejar.CookieJar()
        self.opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(self.jar))

    def call(self, method, path, payload=None):
        data, headers = None, {"Origin": self.base}
        if payload is not None:
            data, headers["Content-Type"] = json.dumps(payload).encode(), "application/json"
        request = urllib.request.Request(self.base + "/api/v1" + path, data=data, method=method, headers=headers)
        try:
            with self.opener.open(request, timeout=REQUEST_TIMEOUT) as response:
                body = response.read()
                return response.status, (json.loads(body) if body else {})
        except urllib.error.HTTPError as error:
            try:
                return error.code, json.loads(error.read())
            except ValueError:
                return error.code, {}
        except Exception as error:  # 连接被拒或超时都算失败采样
            return 0, {"error": type(error).__name__}

    def login(self):
        status, body = self.call("POST", "/auth/login", {"username": self.username, "password": self.password})
        if status != 200:
            raise SystemExit(f"login failed: HTTP {status} {body}")


def ready_status(base):
    try:
        with urllib.request.urlopen(base.rstrip("/") + "/readyz", timeout=REQUEST_TIMEOUT) as response:
            return response.status
    except urllib.error.HTTPError as error:
        return error.code
    except Exception:
        return 0


def unit_pid(unit):
    result = subprocess.run(["systemctl", "show", "-p", "MainPID", "--value", unit],
                            capture_output=True, text=True, check=False)
    value = result.stdout.strip()
    return int(value) if value.isdigit() and value != "0" else None


def process_rss_kib(pid):
    if pid is None:
        return None
    try:
        with open(f"/proc/{pid}/status", encoding="utf-8") as handle:
            fields = dict(line.split(":", 1) for line in handle if ":" in line)
        return int(fields["VmRSS"].strip().split()[0])
    except (OSError, KeyError, ValueError):
        return None


def capacity(paths):
    report = []
    targets = paths or ["/var/lib/beat-preview"]
    for path in targets:
        try:
            stats = os.statvfs(path)
        except OSError as error:
            report.append({"path": path, "error": type(error).__name__})
            continue
        total = stats.f_blocks * stats.f_frsize
        free = stats.f_bavail * stats.f_frsize
        report.append({"path": path, "total_bytes": total, "free_bytes": free,
                       "used_percent": round((total - free) * 100.0 / total, 2) if total else None,
                       "database_bytes": database_bytes(path)})
    return report


def database_bytes(path):
    total = 0
    for base in (path, os.path.join(path, "data")):
        for name in ("beat.db", "beat.db-wal", "beat.db-shm"):
            try:
                total += os.path.getsize(os.path.join(base, name))
            except OSError:
                continue
    return total


def load_once(console, node_id):
    now = datetime.now(timezone.utc)
    window = {"from": (now - timedelta(minutes=12)).strftime("%Y-%m-%dT%H:%M:%SZ"),
              "to": now.strftime("%Y-%m-%dT%H:%M:%SZ"), "max_points": 100,
              "series": [{"ref_id": "A", "node_id": node_id, "metric": "cpu", "aggregation": "avg"},
                         {"ref_id": "B", "node_id": node_id, "metric": "memory", "aggregation": "avg"}]}
    requests = (("report", "POST", "/metrics/query", window),
                ("latest", "POST", "/metrics/query/latest",
                 {"series": [{"ref_id": "A", "node_id": node_id, "metric": "cpu"}]}),
                ("catalog", "GET", "/metrics/catalog", None),
                ("nodes", "GET", "/nodes", None),
                ("runtime", "GET", "/admin/runtime", None))
    results = {}
    for label, method, path, body in requests:
        results[label] = console.call(method, path, body)[0]
    return results


def main():
    args = parse_args()
    console = Console(args.base, args.username, read_password(args.password_file))
    console.login()
    _, nodes = console.call("GET", "/nodes")
    node_id = nodes["data"][0]["id"]
    started = time.time()
    deadline = started + args.minutes * 60.0
    warmup = args.warmup_minutes * 60.0
    samples, failures, rate_limited = [], [], 0
    while True:
        elapsed = time.time() - started
        statuses = load_once(console, node_id)
        ready = ready_status(args.base)
        if ready != 200:
            failures.append(["readyz", ready, round(elapsed)])
        for label, code in statuses.items():
            if code == RATE_LIMIT_STATUS:
                rate_limited += 1
            elif code == 0 or code >= 500:
                failures.append([label, code, round(elapsed)])
        samples.append({"at_seconds": round(elapsed, 1), "readyz": ready,
                        "server_rss_kib": process_rss_kib(unit_pid(args.server_unit)),
                        "agent_rss_kib": process_rss_kib(unit_pid(args.agent_unit)), "statuses": statuses})
        if len(samples) == 1 or len(samples) % 20 == 0:
            print(f"t={elapsed:.0f}s ready={ready} rss={samples[-1]['server_rss_kib']} "
                  f"statuses={statuses} failures={len(failures)}", flush=True)
        if time.time() >= deadline:
            break
        time.sleep(args.interval)
    report = build_report(args, console, samples, failures, rate_limited, node_id, started)
    write_report(args.report, report)
    print(json.dumps({"checks": report["checks"], "rss": report["rss"],
                      "capacity": report["capacity"], "report": args.report}, ensure_ascii=False, indent=2))
    return 0 if all(check["ok"] for check in report["checks"]) else 1


def rss_statistics(samples, warmup, window):
    """用「浸泡开局窗口」与「收官窗口」的 RSS 中位数对比，抵消 Go 堆的锯齿。

    单点最小/最大对比会把 GC 锯齿当成泄漏，所以两端各取一段窗口的中位数：
    基线 = 预热结束后第一个窗口，终值 = 最后一个窗口，增长 = 终值 - 基线。
    """
    steady = [sample for sample in samples
              if sample["server_rss_kib"] and sample["at_seconds"] >= warmup]
    if not steady:
        steady = [sample for sample in samples if sample["server_rss_kib"]]
    if not steady:
        return {}
    last = steady[-1]["at_seconds"]
    opening = [sample["server_rss_kib"] for sample in steady if sample["at_seconds"] <= warmup + window] \
        or [steady[0]["server_rss_kib"]]
    closing = [sample["server_rss_kib"] for sample in steady if sample["at_seconds"] >= last - window] \
        or [steady[-1]["server_rss_kib"]]
    baseline = int(statistics.median(opening))
    final = int(statistics.median(closing))
    return {"samples": len(steady), "opening_samples": len(opening), "closing_samples": len(closing),
            "baseline_kib": baseline, "final_kib": final, "peak_kib": max(sample["server_rss_kib"] for sample in steady),
            "growth_kib": final - baseline, "footprint_kib": max(sample["server_rss_kib"] for sample in steady),
            "growth": round((final - baseline) / max(baseline, 1), 4)}


def build_report(args, console, samples, failures, rate_limited, node_id, started):
    rss = rss_statistics(samples, args.warmup_minutes * 60.0, args.rss_window_minutes * 60.0)
    ceiling_kib = args.rss_ceiling_mib * 1024.0
    _, runtime = console.call("GET", "/admin/runtime")
    checks = [
        {"name": "readyz_clean", "ok": not [item for item in failures if item[0] == "readyz"],
         "detail": [item for item in failures if item[0] == "readyz"][:5]},
        {"name": "no_server_errors", "ok": not [item for item in failures if item[0] != "readyz"],
         "detail": failures[:5]},
        {"name": "rate_limit_within_tolerance", "ok": rate_limited <= MAXIMUM_RATE_LIMITED,
         "detail": rate_limited},
        {"name": "covered_duration", "ok": samples[-1]["at_seconds"] >= args.minutes * 60.0 - args.interval,
         "detail": samples[-1]["at_seconds"]},
        {"name": "rss_growth_within_limit", "ok": bool(rss) and rss["growth"] <= args.growth_limit,
         "detail": {"growth": rss.get("growth"), "limit": args.growth_limit}},
        {"name": "rss_growth_within_ceiling", "ok": bool(rss) and rss["growth_kib"] <= ceiling_kib,
         "detail": {"growth_kib": rss.get("growth_kib"), "ceiling_kib": ceiling_kib,
                    "footprint_kib": rss.get("footprint_kib")}},
    ]
    return {"started_at": datetime.fromtimestamp(started, timezone.utc).isoformat(),
            "finished_at": datetime.now(timezone.utc).isoformat(),
            "base": args.base, "node_id": node_id, "minutes": args.minutes, "interval": args.interval,
            "warmup_minutes": args.warmup_minutes, "rss_window_minutes": args.rss_window_minutes,
            "thresholds": {"growth_limit": args.growth_limit,
            "rss_ceiling_mib": args.rss_ceiling_mib}, "rss": rss, "checks": checks,
            "failures": failures[:20], "rate_limited": rate_limited,
            "capacity": capacity(args.capacity_path), "samples": samples}


def write_report(path, report):
    directory = os.path.dirname(path)
    if directory:
        os.makedirs(directory, mode=0o700, exist_ok=True)
    payload = json.dumps(report, ensure_ascii=False, indent=2).encode()
    descriptor = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, REPORT_MODE)
    with os.fdopen(descriptor, "wb") as handle:
        handle.write(payload)
    os.chmod(path, REPORT_MODE)


if __name__ == "__main__":
    sys.exit(main())
