"""只向私有空目录解包 Beat 冷备份；不恢复归档中的所有者或权限。"""

from contextlib import contextmanager
import gzip
import math
import os
from pathlib import Path, PurePosixPath
import shutil
import stat
import sys
import tarfile


MAX_MEMBERS = 100_000
MAX_EXPANDED_BYTES = 64 << 30
MAX_METADATA_BYTES = 1 << 20
MAX_PATH_BYTES = 4096
RELEASE_FILES = ("beat-server", "beat-agent", "beat-cli", "beat-theme", "static/index.html",
                 "scripts/beatctl.sh", "scripts/state_archive.py",
                 "deploy/systemd/beat-server.service")


class ArchiveReader:
    """在解压之后限制元数据读取，避免依赖 TarInfo 的版本相关内部实现。"""
    def __init__(self, stream):
        self.stream = stream
        self.remaining = MAX_METADATA_BYTES

    def read(self, size=-1):
        if size < 0 or size > MAX_METADATA_BYTES or self.remaining is not None and size > self.remaining:
            raise ValueError("archive metadata exceeds the size limit")
        data = self.stream.read(size)
        if self.remaining is not None:
            self.remaining -= len(data)
        return data

    def seek(self, offset, whence=0):
        return self.stream.seek(offset, whence)

    def tell(self):
        return self.stream.tell()


@contextmanager
def open_archive(source):
    info = Path(source).lstat()
    if not stat.S_ISREG(info.st_mode) or info.st_size > MAX_EXPANDED_BYTES:
        raise ValueError("backup must be a bounded regular file")
    descriptor = os.open(source, os.O_RDONLY | os.O_NOFOLLOW)
    with os.fdopen(descriptor, "rb") as stream, gzip.GzipFile(fileobj=stream) as decoded:
        with tarfile.open(fileobj=ArchiveReader(decoded), mode="r:") as archive:
            yield archive


def validated_members(archive, required=("beat.db", "beat_mts")):
    members, names, expanded = [], set(), 0
    for member in archive:
        path = PurePosixPath(member.name)
        if path.is_absolute() or ".." in path.parts or "\\" in member.name:
            raise ValueError("archive contains an unsafe path")
        if not (member.isdir() or member.isfile()) or member.issparse():
            raise ValueError("archive contains a link or special file")
        if len(member.name.encode()) > MAX_PATH_BYTES or not math.isfinite(member.mtime):
            raise ValueError("archive member metadata is invalid")
        if path == PurePosixPath(".") and not member.isdir():
            raise ValueError("archive root is not a directory")
        if str(path) in names:
            raise ValueError("archive contains duplicate paths")
        names.add(str(path))
        expanded += member.size
        if len(names) > MAX_MEMBERS or expanded > MAX_EXPANDED_BYTES:
            raise ValueError("archive exceeds extraction limits")
        member.pax_headers = {}
        members.append(member)
        archive.fileobj.remaining = MAX_METADATA_BYTES
    if not set(required).issubset(names):
        raise ValueError("archive does not contain its required Beat files")
    return members


def extract_member(archive, member, destination):
    target = destination / PurePosixPath(member.name)
    if member.isdir():
        target.mkdir(mode=0o700, parents=True, exist_ok=True)
        return
    target.parent.mkdir(mode=0o700, parents=True, exist_ok=True)
    source = archive.extractfile(member)
    if source is None:
        raise ValueError("archive member has no file contents")
    descriptor = os.open(target, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    with source, os.fdopen(descriptor, "wb") as output:
        shutil.copyfileobj(source, output, length=64 << 10)
        output.flush()
        os.fsync(output.fileno())


def extract_state(source, destination):
    source, destination = Path(source), Path(destination)
    info = destination.lstat()
    if not stat.S_ISDIR(info.st_mode) or info.st_mode & 0o077 or any(destination.iterdir()):
        raise ValueError("restore destination must be an empty private directory")
    with open_archive(source) as archive:
        members = validated_members(archive)
        archive.fileobj.remaining = None
        for member in members:
            extract_member(archive, member, destination)
    if not destination.joinpath("beat.db").is_file() or not destination.joinpath("beat_mts").is_dir():
        raise ValueError("restored Beat state has invalid file types")


def main(arguments):
    if len(arguments) != 2:
        raise ValueError("usage: python3 state_archive.py <archive.tar.gz> <empty-private-directory>")
    if arguments[0] == "--release":
        with open_archive(arguments[1]) as archive:
            members = validated_members(archive, RELEASE_FILES)
            if any(str(PurePosixPath(member.name)) in RELEASE_FILES and not member.isfile() for member in members):
                raise ValueError("release requires regular program files")
        return
    extract_state(*arguments)


if __name__ == "__main__":
    os.umask(0o077)
    try:
        main(sys.argv[1:])
    except (OSError, ValueError, tarfile.TarError) as failure:
        print(f"restore validation failed: {failure}", file=sys.stderr)
        raise SystemExit(1) from None
