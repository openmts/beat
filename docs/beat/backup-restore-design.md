# Backup and restore

Updated: 2026-10-15

Status: deployed; create/download/validation/staging/startup-apply/rollback were
verified in an isolated acceptance environment

Owner approval, 2026-10-15: the resumable-upload extension (`v18`) and its
approval boundary below are approved for implementation, together with all other
pending approval items.

The deployed happy path is verified. Interrupted record/archive/journal
reconciliation, retry-safe deletion, cursor listing, and proof that an
uncommitted restore journal cannot apply are specified by reviewed migration
`v24` in [`application-data-lifecycle-design.md`](./application-data-lifecycle-design.md)
and implemented in the worktree (`internal/backup`, `internal/store`,
`cmd/server`); they are not yet deployed and the host CI gate has not run.

## Goal

Provide administrator-controlled, testable backups of both platform data in
SQLite and metric history in MTS. Restore validation must never modify live data;
applying a restore occurs before stores open and preserves an automatic rollback
copy.

Komari exposes authenticated ZIP backup upload and validates archive type,
marker, size, entry count, and expanded size before staging a startup restore.
Beat will preserve those operational capabilities while using a Beat-specific
logical MTS format because the embedded public MTS Engine does not expose its
server runtime's filesystem snapshot and restore APIs.

## Archive format

Archives are named `beat-backup-v1-<UTC timestamp>.zip`, mode `0600`, and contain
only these allowlisted entries:

- `manifest.json`: format version, Beat version, creation time, snapshot cutoff,
  counts, sizes, required external settings, and SHA-256 checksums.
- `platform.sqlite`: consistent SQLite snapshot created with `VACUUM INTO`.
- `metrics.jsonl.gz`: typed logical MTS rows for Beat's fixed measurement list.
- `secrets/admin-data.key`: the matching 32-byte application root key when one
  exists.
- `checksums.sha256`: checksum list covering every payload file.

The archive contains SSH private keys and authentication material from SQLite
and is therefore explicitly treated as a recoverable credential bundle. After
`v9`, those values are encrypted under SQLite-wrapped data keys, but the archive
also contains the matching root key; this improves live database-only
disclosure, not archive confidentiality. It is never exposed by a public URL or
written outside the configured backup directory, and external copies require
encrypted storage or independent archive/KMS wrapping. See
[`application-secret-lifecycle-design.md`](./application-secret-lifecycle-design.md).

Future archive evolution is assigned centrally in
[`commercial-parity-implementation-sequence.md`](./commercial-parity-implementation-sequence.md):
theme assets introduce format `v2`, the metric catalog introduces typed MTS export in `v3`, and managed GeoIP
files extend that payload in `v4`.
Resumable upload migration `v18` changes transport only and continues uploading
the then-current archive format without creating `v5`.

## MTS logical snapshot

The embedded MTS dependency does not publish a stable data snapshot/restore API
on `mts.Engine`. Beat therefore exports its owned schema rather than copying
live engine files:

- all `model.MetricNames()` measurements;
- `net_recv_delta` and `net_sent_delta`;
- `network_probe` with all typed fields and tags.

Automated tests require the MTS export schemas to match the retention cleanup
list exactly. Backup validation also iterates every `model.MetricNames()` entry,
recognizes both derived traffic measurements, and keeps `network_probe` on its
separate exact typed-field validation path.

The schema-free Agent-ingestion consistency batch adds
`agent_ingest_receipt` to this managed inventory without changing the archive
envelope; see
[`agent-ingest-consistency-design.md`](./agent-ingest-consistency-design.md).

The pinned MTS public `RowIterator` exposes measurement, tags, timestamp, and
typed fields, so the logical format does not require internal APIs. `MTSStore`
gains an operation read/write lock. Metric and network writes hold a read lock;
maintenance, explicit metric erasure after migration `v22`, and backup export
hold the write lock. Backup flushes MTS, captures a UTC cutoff, streams all rows
at or before that cutoff through the public row iterator, and releases the lock
after the gzip stream is complete. Queries remain available. This trades a
bounded reporting pause for a coherent logical snapshot without relying on
internal MTS files.

Restore creates a fresh MTS directory and replays validated rows in bounded
batches through public typed-write APIs, then flushes, closes, and runs sample
queries before the directory can replace the live target.

## Backup lifecycle

Backups live under `<data-dir>/backups` with directory mode `0700` and file mode
`0600`. A single process lock prevents backup, maintenance, explicit metric
erasure, and restore staging from overlapping.

Authenticated owner routes:

- `GET /api/v1/admin/backups`: list ID, timestamp, size, counts, and state.
- `POST /api/v1/admin/backups`: start an asynchronous backup, returning `202`.
- `GET /api/v1/admin/backups/{id}/download`: attachment download after recent
  reauthentication.
- `DELETE /api/v1/admin/backups/{id}`: remove a selected generated archive.
- `POST /api/v1/admin/backups/validate`: upload and validate an archive.
- `POST /api/v1/admin/backups/{id}/stage-restore`: require recent
  reauthentication and an exact confirmation phrase, then stage for restart.

The API accepts no filesystem path. IDs are server-generated and resolved
against the fixed backup root. Listing ignores symlinks and non-regular files.

`GET /api/v1/admin/backups` is cursor-paged: it accepts `state`, `cursor` and
`limit` (default 50, maximum 200) and returns `{items, next_cursor}`. The cursor
is an RFC3339Nano `(created_at, id)` pair bound to the requested state, so records
created inside the same millisecond are never skipped and a cursor cannot be
replayed under a different filter. An unparseable cursor, an out-of-range limit
or an unknown state returns `400`.

Records move through `running -> ready|failed`, upload validation
`-> validated`, restore staging `staging -> staged|failed`, and deletion
`any state except running|staging|staged -> deleting`. `interrupted` and
`missing` are only reachable from reconciliation. `failed`, `interrupted` and
`missing` are terminal for retention purposes and are deleted 90 days after
their last update. Deleting a record
first commits `deleting` together with `previous_state`, then removes the archive
and the row, so the request is retry-safe after a crash: a missing archive is
idempotent, and a symlink or directory in its place is never followed or removed
and is reported as a deferred reconcile outcome instead.

`GET /api/v1/admin/backups/quarantine` lists (owner or `backups:read-metadata`
service token) the archive files that reconciliation moved aside, with the
recorded reason and timestamp, plus a total count.

## Upload validation

- Maximum compressed upload: 4 GiB.
- Maximum expanded payload: 8 GiB.
- Exact entry allowlist and maximum ten entries.
- Reject absolute paths, traversal, duplicate entries, symlinks, encrypted ZIP
  entries, unknown compression methods, and unsupported format versions.
- Verify every SHA-256 checksum, manifest count and size, SQLite integrity, table
  compatibility, root-key type/length, wrapped-key registry, every registered
  ciphertext with reconstructed AAD, JSONL schema, tags, fields, timestamps,
  and maximum row count before staging.
- Stream validation with bounded buffers; never read an archive or MTS export
  wholly into memory.

Validation extracts into a random `0700` staging directory using files created
with `0600`. Failure removes only that generated staging directory and leaves
live data untouched.

## Resumable upload extension

The single-request upload is deployed, but current Komari supports chunked
backup upload with client retry and server-side merge. Beat assigns the
owner-only resumable protocol to canonical SQLite migration `v18`, after backup
archive format `v4` is deployed. It changes upload transport and persistent
session state but does not introduce a new archive format. The assignment is
maintained in
[`commercial-parity-implementation-sequence.md`](./commercial-parity-implementation-sequence.md).
This extension requires separate explicit approval because it adds persistent
upload state, staging files, cleanup, and API behavior.

An approved revision uses a random upload-session ID and returns a fixed chunk
size, archive size limit, expiry, and maximum concurrency. SQLite stores the
owner, declared filename/size/digest, received-chunk bitmap and digest, expiry,
state, and audit linkage. Chunk bytes live only in a session-specific `0700`
private staging directory as `0600` files. Session IDs are never filesystem
paths.

Each chunk request carries its index, exact byte range, size, and SHA-256. A
retry with identical bytes is idempotent; conflicting bytes fail and quarantine
the session. The Server rejects gaps at finalization, duplicate conflicting
indices, excess declared size, sparse-file abuse, symlinks, path traversal,
too many chunks, expired sessions, or a different owner. Concurrency and total
staging bytes are bounded globally and per owner.

Finalization fsyncs and atomically assembles the archive, verifies exact size
and whole-archive digest, then invokes the same complete ZIP, checksum, SQLite,
MTS, and schema validation described above. A successful upload is only
`validated`; it never stages or applies a restore automatically. Cancellation
and expiry remove only the exact generated session directory through a bounded
cleanup worker. Server restart resumes committed chunks from SQLite and private
staging state.

Frontend upload uses bounded parallel workers, per-chunk progress, retry with
jitter, cancel, and explicit resume. It never treats a network retry as a new
restore, and it clears file and session handles when the dialog closes.

Implementation status, 2026-10-15: the extension is implemented in the worktree.
Migration `v18` now creates `backup_upload_sessions` and `backup_upload_chunks`;
`internal/backup/upload_session*.go` owns staging, chunk verification, idempotent
retries, quarantine, resume, assembly and maintenance; the owner-only API lives
under `/api/v1/admin/backups/uploads`; and `/admin/settings` uploads through the
resumable dialog. This section describes the approved contract, so the paragraph
above stays the acceptance reference. Tests
cover restart, duplicate/conflicting chunks, digest mismatch, missing chunks,
limits, expiry, cleanup, owner isolation, IPv4/IPv6 interruption, and proof that
no incomplete upload reaches restore staging.

## Startup restore and rollback

`stage-restore` writes a small `restore.pending.json` journal beside the archive.
On the next server start, before SQLite or MTS opens:

1. Revalidate archive checksums and manifest.
2. Materialize and validate a new SQLite file, matching root key, and MTS
   directory beside their configured targets.
3. Move current targets to `restore-rollback-<timestamp>` paths.
4. Atomically rename staged targets into place.
5. If any swap fails, reverse completed renames and keep the pending restore for
   diagnosis.
6. Open both stores, initialize the archived root/wrapped-key registry, verify
   every registered ciphertext, and run SQLite integrity and MTS health/sample
   checks.
7. Mark the journal `applied`; retain one rollback set until the administrator
   explicitly removes it.

The application never overwrites current data in place. Arbitrary custom SQLite
or MTS paths inside the archive are ignored; command-line target paths remain
authoritative.

Staging is three-phase and generation-bound. `stage-restore` writes a random
`restore_generation` to the record (`staging`), writes the journal, then commits
`staged`; a failure in either later phase aborts the staging directory and rolls
the record back. At startup the pending journal is applied only when it is an
offline journal or when the database has a `staged` record with the same
generation; an unconfirmed journal never replaces live data. A conflicting
pending journal for another backup returns `409 ErrRestoreStaged` instead of
overwriting it.

## Reconciliation and quarantine

`cmd/server` runs `Service.Reconcile` after stores open and before the HTTP
listener starts. It is idempotent and never fails startup: CAS conflicts are
counted as deferred. In order it

1. Loads and validates the pending journal.
2. Walks every backup record through the bounded cursor page.
3. Rolls back `staged` records whose generation or archive no longer matches,
   marks `running` records `ready` when their archive fully validates and
   `interrupted` otherwise, and turns `ready`/`validated` rows whose archive is
   no longer a regular file into `missing`.
4. Finishes `deleting` removals, retrying or deferring unsafe paths.
5. Discards orphan online journals without a matching staged record; offline
   journals are kept for the next supervised start.
6. Sweeps the backup directory: `*.partial` files are removed, orphan
   `beat-*.zip` archives are moved into `backups/quarantine/` with their reason
   recorded, and stale `.restore-journal-*` files are removed. Known filenames,
   the quarantine directory and non-regular files are left alone.
7. Expires terminal records older than 90 days and audits each transition.

Outcomes are exported as `beat_backup_reconcile_outcomes{outcome=...}` alongside
`beat_backups_total{state=...}` (all nine states) and
`beat_backups_quarantined`.

## Frontend

Add a Backup and restore section to `/admin/settings` or `/admin/security`:

- Storage summary and last backup/restore result.
- Generated-backup list with Download and Delete icon actions.
- Create backup command with live progress polling.
- Upload, validation report, explicit destructive confirmation, and
  "apply on next restart" state.
- Rollback availability and documented restart/rollback commands.

The interface never displays server filesystem paths or archive secrets.

## Failure handling

- Backup failure leaves no visible partial archive; temporary files are renamed
  only after checksum completion.
- Restore validation failure cannot modify live data.
- Startup restore failure rolls back renamed targets and fails startup rather
  than running with a partial restore.
- A canceled backup releases locks and removes only generated temporary files.
- The deployed generic HTTP audit is not completion evidence for asynchronous
  backup or startup restore work. Migration `v24` carries the originating actor,
  HTTP request ID, audit linkage, backup ID, and restore generation through its
  durable state machine; accepted, completed, failed, reconciled, and unknown
  events reuse the envelope from
  [`administrative-mutation-consistency-design.md`](./administrative-mutation-consistency-design.md)
  without recording archive contents or filesystem paths.

## Acceptance

- A backup made while Agents report contains a valid SQLite snapshot and all MTS
  rows at or before its declared cutoff.
- Restoring into empty temporary paths reproduces platform records, current
  node metrics, traffic totals, and network-probe history.
- Corrupt checksums, zip traversal, zip bombs, unknown entries, incompatible
  schemas, missing/wrong root or data keys, invalid AAD/ciphertext, invalid typed
  fields, and interrupted swaps are rejected by tests.
- Public and non-owner requests cannot list, create, download, delete, validate,
  or stage backups.
- Restore keeps a usable rollback set and documents the exact recovery command.
- Full coverage, race, lint, vulnerability, build, deployment, and IPv6
  acceptance gates pass.

## Approval boundary

Implementation requires explicit approval for backup metadata tables, the
owner-only API, the MTS write lock and logical export format, sensitive ZIP
downloads, the `4 GiB`/`8 GiB` limits, generated backup storage, startup restore
journaling, and atomic replacement of configured SQLite/MTS targets.
