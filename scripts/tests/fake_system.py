import json
import os
from pathlib import Path
import subprocess
import sys


ROOT = Path(os.environ["BEAT_TEST_ROOT"])
COMMAND, *ARGS = sys.argv[1:]
with (ROOT / "calls.jsonl").open("a") as log:
    log.write(json.dumps([COMMAND, *ARGS]) + "\n")


def account():
    path = ROOT / "account.json"
    return json.loads(path.read_text()) if path.exists() else None


def getent():
    entry = account()
    if entry is None:
        return 2
    # A9 起账户名可由 BEAT_SERVICE_ACCOUNT 覆盖，fixture 必须回显被查询的名字而不是写死 beat。
    name = ARGS[1]
    if ARGS[0] == "passwd":
        print(f"{name}:x:{entry['uid']}:{entry['gid']}:{entry['comment']}:{entry['home']}:{entry['shell']}")
    else:
        print(f"{name}:x:{entry['gid']}:")
    return 0


def useradd():
    (ROOT / "account.json").write_text(json.dumps(dict(name=ARGS[-1], uid=int(os.environ["BEAT_TEST_UID"]),
        gid=int(os.environ["BEAT_TEST_GID"]), home=os.environ["BEAT_STATE_DIR"],
        comment="Beat monitoring service", shell="/usr/sbin/nologin")))
    return 0


def current_version():
    path = Path(os.environ["BEAT_PREFIX"]) / "current"
    return path.resolve().name if path.exists() else "none"


def systemctl():
    args = [arg for arg in ARGS if arg != "--system"]
    operation = args[0]
    if operation == "list-unit-files":
        # 真实主机一定会列出模板单元（getty@.service 等），systemctl show 对模板直接失败。
        print("beat-server.service enabled\nother.service disabled\ngetty@.service static")
    elif operation == "show":
        if args[1].endswith("@.service"):
            sys.stderr.write(f"Failed to get properties: Unit name {args[1]} is neither a valid invocation ID nor unit name.\n")
            return 1
        print("beat" if os.environ.get("BEAT_TEST_OTHER_USER") == "beat" else "root")
    elif operation == "is-active":
        return 0 if (ROOT / "active").exists() else 3
    elif operation == "stop" or operation == "disable":
        unit = next((arg for arg in reversed(args) if not arg.startswith("-")), "")
        unit_dir = os.environ.get("BEAT_UNIT_DIR")
        if unit_dir and not Path(unit_dir, unit).exists() and not (ROOT / "active").exists():
            # 真实 systemd 对未加载单元返回 5（stop）／1（disable），fixture 必须保持同样的失败语义。
            sys.stderr.write(f"Failed to {operation} {unit}: Unit {unit} not loaded.\n")
            return 5 if operation == "stop" else 1
        (ROOT / "active").unlink(missing_ok=True)
    elif operation in ("start", "restart", "enable"):
        if current_version() == os.environ.get("BEAT_TEST_FAIL_START"):
            Path(os.environ["BEAT_STATE_DIR"], "beat.db").write_text("failed-migration")
            return 1
        (ROOT / "active").write_text("yes")
    if operation == os.environ.get("BEAT_TEST_FAIL_SYSTEMCTL"):
        return 1
    return 0


def safe_destination(path):
    target = Path(path).absolute()
    if not target.is_relative_to(ROOT):
        raise RuntimeError(f"test refused a mutation outside temporary workspace: {target}")


def filesystem_command():
    safe_destination(ARGS[-1])
    if COMMAND == "install" and os.environ.get("BEAT_TEST_FAIL_COPY") and ARGS[-1].endswith("beat-server"):
        return 1
    if COMMAND == "chown":
        if os.geteuid() != 0:
            return 0
        flags = [arg for arg in ARGS if arg.startswith("-")]
        paths = [arg for arg in ARGS if arg.startswith("/")]
        owner = "0:0" if "root:root" in ARGS else f"{os.environ['BEAT_TEST_UID']}:{os.environ['BEAT_TEST_GID']}"
        args = flags + [owner, *paths]
    else:
        args = ARGS
    return subprocess.run([f"/usr/bin/{COMMAND}", *args], check=False).returncode


def main():
    if COMMAND == "id":
        if ARGS[:1] == ["-nG"] and len(ARGS) == 2:
            print(os.environ.get("BEAT_TEST_GROUPS", ARGS[1]))
        elif ARGS == ["-un"]:
            print("root")
        else:
            print("0")
        return 0
    if COMMAND == "getent":
        return getent()
    if COMMAND == "useradd":
        return useradd()
    if COMMAND == "systemctl":
        return systemctl()
    if COMMAND == "ps":
        print(os.environ.get("BEAT_TEST_PROCESS_UNIT", ""))
        return 0
    if COMMAND in ("chown", "install", "rm"):
        return filesystem_command()
    if COMMAND == "curl":
        return int(current_version() == os.environ.get("BEAT_TEST_FAIL_READY"))
    return 0


raise SystemExit(main())
