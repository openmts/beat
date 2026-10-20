import importlib.util
import gzip
import io
import os
from pathlib import Path
import tarfile
import tempfile
import unittest

from deployment_fixture import PROJECT


spec = importlib.util.spec_from_file_location("state_archive", PROJECT / "scripts/state_archive.py")
state_archive = importlib.util.module_from_spec(spec)
spec.loader.exec_module(state_archive)


class StateArchiveTests(unittest.TestCase):
    def setUp(self):
        workspace = tempfile.TemporaryDirectory(prefix="beat-state-archive-test.")
        self.addCleanup(workspace.cleanup)
        self.root = Path(workspace.name)
        self.archive = self.root / "backup.tar.gz"
        self.target = self.root / "restore"
        self.target.mkdir(mode=0o700)

    def write_archive(self, extra=None):
        with tarfile.open(self.archive, "w:gz") as stream:
            entry = tarfile.TarInfo("beat.db")
            entry.size, entry.mode = 4, 0o777
            stream.addfile(entry, io.BytesIO(b"data"))
            directory = tarfile.TarInfo("beat_mts")
            directory.type = tarfile.DIRTYPE
            stream.addfile(directory)
            if extra is not None:
                stream.addfile(extra, io.BytesIO(b"x" * extra.size))

    def test_private_regular_files_are_restored(self):
        self.write_archive()
        state_archive.extract_state(self.archive, self.target)
        self.assertEqual(b"data", self.target.joinpath("beat.db").read_bytes())
        self.assertEqual(0o600, self.target.joinpath("beat.db").stat().st_mode & 0o777)
        self.assertEqual(0o700, self.target.joinpath("beat_mts").stat().st_mode & 0o777)

    def test_unsafe_members_are_rejected_before_any_file_is_written(self):
        for kind in ("absolute", "parent", "symlink", "hardlink", "fifo", "device", "duplicate", "backslash"):
            with self.subTest(kind=kind):
                entry = tarfile.TarInfo("entry")
                if kind == "absolute": entry.name = str(self.root / "outside")
                if kind == "parent": entry.name = "../outside"
                if kind == "symlink": entry.type, entry.linkname = tarfile.SYMTYPE, "outside"
                if kind == "hardlink": entry.type, entry.linkname = tarfile.LNKTYPE, "beat.db"
                if kind == "fifo": entry.type = tarfile.FIFOTYPE
                if kind == "device": entry.type = tarfile.CHRTYPE
                if kind == "duplicate": entry.name = "./beat.db"
                if kind == "backslash": entry.name = "..\\outside"
                self.write_archive(entry)
                with self.assertRaises(ValueError):
                    state_archive.extract_state(self.archive, self.target)
                self.assertEqual([], list(self.target.iterdir()))

    def test_symlink_archive_and_nonprivate_destination_are_rejected(self):
        self.write_archive()
        link = self.root / "linked.tar.gz"
        link.symlink_to(self.archive)
        with self.assertRaises(ValueError): state_archive.extract_state(link, self.target)
        self.target.chmod(0o755)
        with self.assertRaises(ValueError): state_archive.extract_state(self.archive, self.target)

    def test_missing_platform_state_and_duplicate_directories_are_rejected(self):
        with tarfile.open(self.archive, "w:gz") as stream:
            stream.addfile(tarfile.TarInfo("unrelated"))
        with self.assertRaises(ValueError): state_archive.extract_state(self.archive, self.target)
        extra = tarfile.TarInfo("beat_mts")
        extra.type = tarfile.DIRTYPE
        self.write_archive(extra)
        with self.assertRaises(ValueError): state_archive.extract_state(self.archive, self.target)

    def test_member_count_and_size_are_bounded(self):
        self.write_archive()
        original = state_archive.MAX_MEMBERS
        state_archive.MAX_MEMBERS = 1
        try:
            with self.assertRaises(ValueError): state_archive.extract_state(self.archive, self.target)
        finally:
            state_archive.MAX_MEMBERS = original

    def test_large_pax_metadata_is_rejected_before_allocating_or_reading_it(self):
        metadata = tarfile.TarInfo("pax")
        metadata.type = tarfile.XHDTYPE
        metadata.size = state_archive.MAX_METADATA_BYTES + 1
        with gzip.open(self.archive, "wb") as stream:
            stream.write(metadata.tobuf())
        with self.assertRaisesRegex(ValueError, "metadata"):
            state_archive.extract_state(self.archive, self.target)
        self.assertEqual([], list(self.target.iterdir()))


if __name__ == "__main__":
    unittest.main()
