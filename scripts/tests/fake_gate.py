import json
import os
from pathlib import Path
import re
import sys


PROJECT = Path(__file__).resolve().parents[2]
PUBLIC_THEME_ASSETS = ("beat-theme-sdk.js", "theme-author-guide.md",
                       "theme-sandbox-contract.md", "theme-market-publishing.md")
RACE_HEAVY_PACKAGES = ("applicationsecrets", "agentrollout", "store", "backup",
                       "notificationqueue", "remoteoperations", "geoip", "theme",
                       "api", "api/handler", "adminauth")


def write_frontend_output(arguments):
    """模拟 Vite 构建产物：主题门禁依赖 index.html 存在（浏览器 smoke 的静态根）。"""
    if "--outDir" not in arguments:
        return
    output = Path(arguments[arguments.index("--outDir") + 1])
    output.mkdir(parents=True, exist_ok=True)
    (output / "index.html").write_bytes(b"<!doctype html>")


def write_theme_output(arguments):
    """模拟前端构建产物：门禁断言的主题分发包必须真的出现，缺一个就要失败。"""
    if "--outDir" not in arguments:
        return
    output = Path(arguments[arguments.index("--outDir") + 1])
    themes = output / "themes"
    themes.mkdir(parents=True, exist_ok=True)
    examples = (PROJECT / "webui/src/lib/theme-examples.ts").read_text()
    missing = os.environ.get("BEAT_GATE_MISSING_THEME", "")
    for identifier in re.findall(r'id: *"([^"]+)"', examples):
        if identifier == missing:
            continue
        (themes / f"{identifier}.theme.zip").write_bytes(b"zip")
        (themes / f"{identifier}.preview.png").write_bytes(b"png")
    for asset in PUBLIC_THEME_ASSETS:
        (themes / asset).write_bytes(b"doc")


command, *arguments = sys.argv[1:]
with open(os.environ["BEAT_GATE_LOG"], "a") as stream:
    stream.write(json.dumps([command, *arguments]) + "\n")
if " ".join([command, *arguments]).startswith(os.environ.get("BEAT_GATE_FAIL", "not-a-command")):
    raise SystemExit(1)
if command == "npm" and "build" in arguments:
    write_frontend_output(arguments)
    write_theme_output(arguments)
if command == "go" and "test" in arguments and os.environ.get("BEAT_THEME_BROWSER_FIXTURE"):
    # 主题 fixture 用例在真实运行里打印 FIXTURE=<url>；这里给出同样的握手信号，
    # 让 run-theme-browser-gates.sh 能继续走到冒烟脚本（冒烟脚本本身由 node 假件替代）。
    print("FIXTURE=http://127.0.0.1:1/fixture")
if command == "go" and arguments[:1] == ["list"]:
    for package in RACE_HEAVY_PACKAGES:
        print(PROJECT / "internal" / package)
    print(PROJECT / "internal/testutil")
if command == "go" and arguments[:2] == ["tool", "cover"]:
    print("total: (statements) " + os.environ.get("BEAT_GATE_COVERAGE", "95.0%"))
if command == "docker" and arguments[0] == "run":
    print("isolated-gate-container")
if command == "docker" and arguments[0] == "exec":
    if arguments[2:] == ["id", "-u"]:
        print("65532")
    elif os.environ.get("BEAT_GATE_UNHEALTHY"):
        raise SystemExit(1)
