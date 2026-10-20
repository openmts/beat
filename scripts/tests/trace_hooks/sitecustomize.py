"""仅测试子进程使用的标准库行覆盖记录；生产安装不会加载该模块。"""

import atexit
import json
import os
from pathlib import Path
import sys
import time


targets = {str(Path(__file__).resolve().parents[2] / name) for name in ("state_archive.py", "soak.py")}
lines = {}


def record(frame, event, _argument):
    if frame.f_code.co_filename not in targets:
        return None
    if event == "line":
        lines.setdefault(frame.f_code.co_filename, set()).add(frame.f_lineno)
    return record


def flush():
    root = os.environ.get("BEAT_SCRIPT_COVERAGE_DIR")
    if root and lines and Path(root).is_dir():
        path = Path(root) / f"python-{os.getpid()}-{time.monotonic_ns()}.json"
        path.write_text(json.dumps({name: sorted(values) for name, values in lines.items()}))
        lines.clear()


if os.environ.get("BEAT_SCRIPT_COVERAGE_DIR"):
    sys.settrace(record)
    atexit.register(flush)
