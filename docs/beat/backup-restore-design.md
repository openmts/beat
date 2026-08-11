# Backup and restore

Updated: 2026-08-01

Status: deployed; create/download/validation/staging/startup-apply/rollback were
verified in an isolated acceptance environment

The deployed happy path is verified, but interrupted record/archive/journal
reconciliation, retry-safe deletion, cursor listing, and proof that an
uncommitted restore journal cannot apply are owned by reviewed migration `v19`
in [`application-data-lifecycle-design.md`](./application-data-lifecycle-design.md).

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
`v5`, those values are encrypted under SQLite-wrapped data keys, but the archive
also contains the matching root key; this improves live database-only
disclosure, not archive confidentiality. It is never exposed by a public URL or
written outside the configured backup directory, and external copies require
encrypted storage or independent archive/KMS wrapping. See
[`application-secret-lifecycle-design.md`](./application-secret-lifecycle-design.md).

Future archive evolution is assigned centrally in
[`commercial-parity-implementation-sequence.md`](./commercial-parity-implementation-sequence.md):
theme assets introduce format `v2`, managed GeoIP files introduce `v3`, and the
metric catalog introduces the catalog-governed logical MTS export in `v4`.
Resumable upload migration `v13` changes transport only and continues uploading
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
maintenance, explicit metric erasure after migration `v17`, and backup export
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
owner-only resumable protocol to canonical SQLite migration `v13`, after backup
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
restore, and it clears file and session handles when the dialog closes. Tests
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
  backup or startup restore work. Migration `v19` carries the originating actor,
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
