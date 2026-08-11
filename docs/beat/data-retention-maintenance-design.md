# Data retention and maintenance

Updated: 2026-08-01

## Goal

Keep agent time-series metrics in MTS for a bounded period while SQLite remains
the platform-data store. Administrators can inspect storage usage, update the
retention policy, and start a safe maintenance run without exposing filesystem
paths, measurements, or arbitrary deletion ranges.

This feature expires old data by age. It does not provide explicit node,
network-task, fleet, or all-history erasure. Those destructive lifecycle and
governance semantics are a separate reviewed `v17` batch in
[`metric-erasure-design.md`](./metric-erasure-design.md).

It also does not own SQLite session, audit, alert-event, backup-record, or
temporary-file retention. The current audit cleanup is coupled to this worker;
the reviewed `v19` application lifecycle separates and completes that contract
in [`application-data-lifecycle-design.md`](./application-data-lifecycle-design.md).

## Persisted configuration

SQLite stores one maintenance settings row:

- MTS retention days, default `30`, allowed range `1-3650`.
- Automatic cleanup switch, enabled by default.
- Automatic cleanup hour in UTC, default `3`, allowed range `0-23`.
- Last run trigger, cutoff, timestamps, duration, result, error, and SQLite
  integrity result.

No agent metric samples are stored in SQLite.

## Maintenance flow

Automatic and manual maintenance share a single process-local lock. A run:

1. Persists the `running` state and computes the cutoff from the configured
   retention period.
2. Flushes MTS writes.
3. Deletes data at or before the cutoff from the fixed Beat measurement list.
4. Compacts MTS so tombstoned data can release disk space.
5. Checkpoints the SQLite WAL, runs `integrity_check`, performs `VACUUM`, and
   runs `optimize` without deleting platform rows.
6. Persists success or failure, duration, cutoff, and integrity result.

Manual execution returns `202 Accepted` and runs in the background. A second
manual or automatic request while maintenance is active returns a conflict.
The server waits for an active maintenance run during graceful shutdown.
Backup, restore staging, retention, and the future explicit-erasure worker share
one exclusive MTS operation lock and cannot overlap.

The deployed generic HTTP audit records only request completion and cannot
prove that a background maintenance run completed. The schema-free
[`administrative-mutation-consistency-design.md`](./administrative-mutation-consistency-design.md)
contract records manual acceptance with the exact actor and HTTP request ID,
then records completed, failed, partial, or unknown against the same persisted
run identity. Automatic runs use a fixed system actor and the same terminal
outcome vocabulary.

## API

All endpoints require administrator authorization:

- `GET /api/v1/settings/maintenance`: settings, run status, MTS health, and
  MTS/SQLite/total byte usage.
- `PUT /api/v1/settings/maintenance`: update retention and schedule settings.
- `POST /api/v1/settings/maintenance/run`: start one manual maintenance run.

The API never accepts a path, measurement, or deletion cutoff from clients.

## Acceptance

- Samples newer than the cutoff remain queryable; older samples disappear.
- Core metrics, traffic delta metrics, and network probe history are included.
- Automated tests require the cleanup measurement list to exactly match the
  logical-backup export schemas, including order and duplicate detection.
- SQLite platform tables and rows remain intact after maintenance.
- Automatic and manual runs cannot overlap.
- Restarted servers mark an interrupted persisted run as failed.
- Public routes do not expose maintenance configuration or actions.
- Retention never substitutes for explicit erasure and cannot target a node,
  task, measurement, tag, or caller-supplied cutoff.
- Backend and frontend coverage remain at least 90 percent and all project
  quality gates pass.
