"""运行无系统副作用的部署测试，汇总 Python 与 Bash 执行行覆盖率。"""

import dis
import importlib.util
import json
import os
from pathlib import Path
import re
import tempfile
import types
import unittest


ROOT = Path(__file__).resolve().parents[2]
SCRIPTS = ROOT / "scripts"


def python_lines(code):
    lines = {line for _, line in dis.findlinestarts(code) if line is not None and line > 0}
    for value in code.co_consts:
        if isinstance(value, types.CodeType):
            lines.update(python_lines(value))
    return lines


def shell_lines(source):
    lines, continued = set(), False
    for index, raw in enumerate(source.splitlines(), 1):
        line = raw.strip()
        if not line or line.startswith("#"):
            continue
        eligible = not continued and not line.endswith("() {")
        eligible = eligible and not re.match(r"^(?:\}|\)|;;|fi|else|elif|esac|done|case\b)", line)
        if eligible:
            lines.add(index)
        continued = line.endswith("\\")
    return lines


def coverage_reports(directory):
    hits = {}
    for report in directory.glob("*.json"):
        for name, lines in json.loads(report.read_text()).items():
            hits.setdefault(name, set()).update(lines)
    successful = True
    for name in ("beatctl.sh", "check.sh", "prepare-release.sh", "state_archive.py", "soak.py"):
        path = SCRIPTS / name
        source = path.read_text()
        expected = python_lines(compile(source, str(path), "exec")) if path.suffix == ".py" else shell_lines(source)
        covered = expected & hits.get(str(path), set())
        percentage = len(covered) * 100 / len(expected)
        print(f"{name}: {len(covered)}/{len(expected)} executed lines ({percentage:.2f}%)")
        missing = sorted(expected - covered)
        if percentage < 90:
            print(f"  uncovered: {missing}")
            successful = False
    return successful


def main():
    os.umask(0o077)
    with tempfile.TemporaryDirectory(prefix="beat-script-coverage.") as directory:
        os.environ["BEAT_SCRIPT_COVERAGE_DIR"] = directory
        spec = importlib.util.spec_from_file_location("coverage_hook", SCRIPTS / "tests/trace_hooks/sitecustomize.py")
        hook = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(hook)
        suite = unittest.defaultTestLoader.discover(str(SCRIPTS / "tests"), pattern="test_*.py")
        result = unittest.TextTestRunner(verbosity=2).run(suite)
        hook.flush()
        measured = coverage_reports(Path(directory))
        return result.wasSuccessful() and measured


if __name__ == "__main__":
    raise SystemExit(0 if main() else 1)
