//go:build linux

package agentupdate

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestFilesystemBoundaries(t *testing.T) {
	f := newFixture(t)
	inst := f.open(t)
	for _, name := range []string{"", "..", ".", "a/b", "a\\b", "a\x00b"} {
		_, e := inst.state.open(name, os.O_RDONLY, 0)
		wantError(t, ErrUnsafePath, e)
	}
	statePath := filepath.Join(filepath.Dir(f.u.config.Executable), stateDirectory)
	must(t, os.Symlink(f.u.config.Executable, filepath.Join(statePath, "link")))
	_, e := inst.state.open("link", os.O_RDONLY, 0)
	if e == nil {
		t.Fatal("symlink accepted")
	}
	must(t, os.Link(f.u.config.Executable, filepath.Join(statePath, "hard")))
	_, e = inst.state.open("hard", os.O_RDONLY, 0)
	wantError(t, ErrUnsafePath, e)
	must(t, os.Remove(filepath.Join(statePath, "hard")))
	must(t, os.WriteFile(filepath.Join(statePath, "writable"), []byte("x"), 0o600))
	must(t, os.Chmod(filepath.Join(statePath, "writable"), 0o666))
	_, e = inst.state.open("writable", os.O_RDONLY, 0)
	wantError(t, ErrUnsafePath, e)
	must(t, os.Mkdir(filepath.Join(statePath, "directory"), 0o700))
	_, e = inst.state.open("directory", os.O_RDONLY, 0)
	wantError(t, ErrUnsafePath, e)
	foreign := *inst.state
	foreign.device++
	_, e = foreign.openRelative("lock", os.O_RDONLY, 0)
	wantError(t, ErrCrossDevice, e)
	wantError(t, ErrCrossDevice, renameRelative(fileRef{inst.state, "lock"}, fileRef{&foreign, "lock"}))
	_, e = openChildDirectory(&foreign, "missing", true)
	wantError(t, ErrCrossDevice, e)
	must(t, os.Mkdir(filepath.Join(statePath, "bad-mode"), 0o755))
	must(t, os.Chmod(filepath.Join(statePath, "bad-mode"), 0o755))
	_, e = openChildDirectory(inst.state, "bad-mode", false)
	wantError(t, ErrUnsafePath, e)
	_, e = openChildDirectory(inst.state, "link", false)
	wantError(t, ErrUnsafePath, e)
	_, e = openChildDirectory(inst.state, "absent/child", true)
	wantError(t, ErrStorage, e)
	link := filepath.Join(t.TempDir(), "symlink")
	must(t, os.Symlink(filepath.Dir(f.u.config.Executable), link))
	_, e = openDirectory(link)
	wantError(t, ErrUnsafePath, e)
	dir := t.TempDir()
	must(t, os.Chmod(dir, 0o777))
	_, e = openDirectory(dir)
	wantError(t, ErrManualOnly, e)
	if os.Geteuid() == 0 {
		must(t, os.WriteFile(filepath.Join(statePath, "foreign"), []byte("x"), 0o600))
		must(t, os.Chown(filepath.Join(statePath, "foreign"), 65534, 65534))
		_, e = inst.state.open("foreign", os.O_RDONLY, 0)
		wantError(t, ErrUnsafePath, e)
	}

}

func TestPrivateFileErrorsAndTemporaryRecovery(t *testing.T) {
	f := newFixture(t)
	inst := f.open(t)
	must(t, writePrivate(inst.state, "data", []byte("hello")))
	_, e := readPrivate(inst.state, "data", 2)
	wantError(t, ErrJournal, e)
	_, e = readPrivate(inst.state, "missing", 2)
	if e == nil {
		t.Fatal("missing accepted")
	}
	must(t, inst.state.remove("missing"))
	must(t, privateCopy(bytes.NewReader([]byte("copy")), inst.state, "copy"))
	if e = privateCopy(bytes.NewReader(nil), inst.state, "copy"); e == nil {
		t.Fatal("overwrote private file")
	}
	wantError(t, errInjected, privateCopy(failedReader{}, inst.state, "failed-copy"))
	must(t, writePrivate(inst.state, journalFile+".tmp", []byte("interrupted")))
	must(t, cleanupJournalTemporary(inst))
	must(t, writePrivate(inst.state, "data.tmp", []byte("busy")))
	if e = writePrivate(inst.state, "data", nil); e == nil {
		t.Fatal("overwrote temporary")
	}
	must(t, os.Mkdir(filepath.Join(filepath.Dir(f.u.config.Executable), stateDirectory, "is-dir"), 0o700))
	if e = writePrivate(inst.state, "is-dir", []byte("x")); e == nil {
		t.Fatal("renamed over directory")
	}
	if e = safeRemove(inst.state, "is-dir"); e == nil {
		t.Fatal("removed directory")
	}
	if e = saveJournal(inst, &journal{Reason: string(bytes.Repeat([]byte{'x'}, maximumJournalBytes))}); e == nil {
		t.Fatal("oversized journal")
	}
	closed, e := openDirectory(filepath.Dir(f.u.config.Executable))
	must(t, e)
	must(t, closed.file.Close())
	if _, e = availableSpace(closed); e == nil {
		t.Fatal("statfs closed dir")
	}
	if e = writePrivate(closed, "x", nil); e == nil {
		t.Fatal("write closed dir")
	}
	if _, e = openChildDirectory(closed, "x", true); e == nil {
		t.Fatal("mkdir closed dir")
	}
}

func TestInterruptedDownloadResumeAndCorruption(t *testing.T) {
	for _, kind := range []string{"resume", "tail", "short", "corrupt", "fetch-error", "no-fetcher", "canceled", "expires"} {
		t.Run(kind, func(t *testing.T) {
			f := newFixture(t)
			f.next = testELF(int(ChunkBytes)+512, 2)
			f.resign(t, "v2.0.0")
			f.crashAt("download_chunk")
			_, e := f.u.Apply(context.Background(), f.assignment)
			wantError(t, errInjected, e)
			if f.read(t).Downloaded != ChunkBytes {
				t.Fatal("checkpoint not persisted")
			}
			path := filepath.Join(filepath.Dir(f.u.config.Executable), stateDirectory, downloadFile)
			switch kind {
			case "tail":
				file, e := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
				must(t, e)
				_, e = file.Write([]byte("uncommitted"))
				must(t, e)
				must(t, file.Close())
			case "short":
				must(t, os.Truncate(path, 10))
			case "corrupt":
				file, e := os.OpenFile(path, os.O_WRONLY, 0)
				must(t, e)
				_, e = file.WriteAt([]byte{0}, 0)
				must(t, e)
				must(t, file.Close())
			case "fetch-error":
				f.fetcher.fail = true
			case "no-fetcher":
				f.u.config.Fetcher = nil
			case "expires":
				f.now = f.assignment.ExpiresAt
			}
			f.u.fault = nil
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if kind == "canceled" {
				cancel()
			}
			s, e := f.u.Apply(ctx, f.assignment)
			if kind == "resume" || kind == "tail" {
				wantPhase(t, "published", s, e)
				f.checkCurrent(t, f.next)
				if f.fetcher.offsets[len(f.fetcher.offsets)-1] != ChunkBytes {
					t.Fatal("restarted full download")
				}
			} else {
				if e == nil {
					t.Fatal("accepted invalid resume")
				}
				f.checkCurrent(t, f.old)
			}
		})
	}
}

func TestStagingAdmissionAndJournalErrors(t *testing.T) {
	for _, name := range []string{"state-link", "journal-link", "lock-link", "download-link", "previous-link", "temporary-link", "bad-journal", "different-assignment"} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			state := filepath.Join(filepath.Dir(f.u.config.Executable), stateDirectory)
			if name == "state-link" {
				must(t, os.Symlink(t.TempDir(), state))
			} else {
				inst, e := openInstallation(f.u.config.Executable, true)
				must(t, e)
				inst.close()
				file := map[string]string{"journal-link": journalFile, "lock-link": "lock", "download-link": downloadFile, "previous-link": "previous.next", "temporary-link": journalFile + ".tmp"}[name]
				if file != "" {
					if file == "lock" {
						must(t, os.Remove(filepath.Join(state, file)))
					}
					must(t, os.Symlink(f.u.config.Executable, filepath.Join(state, file)))
				}
				if name == "bad-journal" {
					must(t, os.WriteFile(filepath.Join(state, journalFile), []byte("{"), 0o600))
				}
				if name == "different-assignment" {
					f.crashAt("prepared")
					_, e = f.u.Apply(context.Background(), f.assignment)
					wantError(t, errInjected, e)
					f.u.fault = nil
					f.assignment.ID = "new"
				}
			}
			_, e := f.u.Apply(context.Background(), f.assignment)
			if e == nil {
				t.Fatal("unsafe staging accepted")
			}
			f.checkCurrent(t, f.old)
		})
	}
}

func TestSpaceAndReadonlyEntrypoint(t *testing.T) {
	f := newFixture(t)
	must(t, os.Chmod(f.u.config.Executable, 0o600))
	_, e := f.u.Apply(context.Background(), f.assignment)
	wantError(t, ErrManualOnly, e)
	inst := f.open(t)
	j := &journal{Assignment: f.assignment, Previous: Image{Size: 1 << 62}}
	wantError(t, ErrStorage, checkSpace(inst, j))
	file, e := inst.root.open(inst.name, os.O_RDONLY, 0)
	must(t, e)
	must(t, file.Close())
	if e = imageVerifier("amd64").makeExecutable(context.Background(), file, f.u.config.Installed); e == nil {
		t.Fatal("closed image accepted")
	}
}
