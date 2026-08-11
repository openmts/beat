# Migration and recovery operations

Updated: 2026-07-30

Status: reviewed design; implementation requires explicit approval

## Goal and competitor classification

Komari exposes authenticated database-migration progress and a temporary
authenticated recovery guide for correcting a failed metric-store DSN. Beat
uses one embedded MTS engine and one versioned SQLite ledger, so arbitrary DSN
selection, alternate SQL metric backends, and online database switching are not
valid parity targets.

Evidence:

- <https://github.com/komari-monitor/komari/blob/4077201f098774511eaf504f220c5f6be009346b/web/migration/migration.go>
- <https://github.com/komari-monitor/komari/blob/4077201f098774511eaf504f220c5f6be009346b/web/recovery/recovery.go>

Beat already has transactional consecutive migrations, future-version
rejection, backup-before-upgrade policy, restore journaling, readiness checks,
and deployment-manager restart guidance. The remaining parity gap is bounded
operator visibility and recovery when normal startup cannot serve the
authenticated application.

## Security and architecture decision

Beat provides a local stopped-service control surface plus a read-only normal
admin view. It does not expose a temporary unauthenticated listener, an online
schema-mutation endpoint, a DSN editor, a database driver picker, a history
discard shortcut, or a general process restart API.

The local command requires filesystem authority over the private data directory
and the shared startup lock defined in
[`operator-recovery-design.md`](./operator-recovery-design.md). Browser users
cannot invoke a migration. Normal administration can only inspect sanitized
state and follow the local runbook.

## Local command surface

The schema-free `migration-recovery-ops` release batch extends the recovery CLI:

```text
beat-server admin-recovery migration-status --db-path PATH --data-dir PATH
beat-server admin-recovery migration-check --db-path PATH --data-dir PATH --backup PATH
```

`migration-status` is read-only. It reports:

- database existence, private mode/ownership, integrity result, and current
  schema version;
- the binary's minimum/maximum supported schema and ordered pending migration
  numbers/names;
- future-version, missing-ledger, interrupted-restore, and lock conflicts using
  fixed error codes;
- whether a verified compatible stopped-service backup is available;
- the last sanitized migration failure marker when one exists.

It never prints SQL, table contents, internal row IDs, secrets, hashes, DSNs,
MTS paths, or archive payloads.

`migration-check` validates the exact backup archive, SQLite integrity,
supported source/target versions, free-space floor, file modes, data-directory
lock, restore state, and every migration precondition without mutating the live
database. It opens a private temporary copy, applies the canonical migrations
there, verifies fresh/upgraded schema invariants, and removes only its generated
staging. Success proves eligibility but does not migrate production data.

Normal startup remains the only forward-migration executor in the first
revision. It already applies each SQLite migration atomically. A failed
migration exits nonzero for the external deployment manager; the operator uses
`migration-status`, `migration-check`, the pre-upgrade backup, and the rollback
runbook. Adding a separate local apply command or changing startup to require a
manual migration is a future workflow change requiring new approval.

## Failure marker and progress

Startup writes a bounded `migration.status.json` under the `0700` data directory
as a `0600` file using temporary-file fsync and atomic rename. It contains only:

- state: `idle`, `checking`, `applying`, `failed`, or `completed`;
- source/target/current migration numbers and the current migration name;
- started/updated/completed timestamps;
- fixed phase and sanitized error code;
- Beat version/build ID and whether rollback is required.

The file contains no SQL, path supplied by a request, database content, stack
trace, DSN, secret, or raw error. It is operational state, not application data,
and consumes no SQLite migration number. Successful startup retains the latest
completed marker; a new attempt replaces it atomically.

Readiness exposes only `migration: ok` after normal routes are serving. During a
failed startup there is no recovery web listener; the local marker, structured
stderr log, process exit status, and deployment-manager status are authoritative.

## Normal administrator experience

An owner-only read-only `Migration and recovery` section in the Operations
surface shows current schema, supported maximum, latest completed migration,
restore pending/applied state, last sanitized failure code, application/MTS
health, and links to the local runbook. It does not show filesystem paths or
render a copy-ready command containing private paths.

The shadcn/ui `base-nova` view uses `Tabs`, `Badge`, `Alert`, `Table`,
`Accordion`, and `Separator`. A failed state names the corrective decision:
validate backup, stop the Server, run local status/check, retry the deployment,
or restore the matching pre-upgrade artifact. There is no Start, Discard,
Change database, or Restart button.

Public, Agent, service-token, and ordinary admin principals cannot read this
detail. The public `/readyz` response remains bounded and never exposes schema
names, paths, or failure text.

## Empty-instance relationship

First-run backup restore is owned by `operator-recovery-cli`, not this batch.
An empty data directory may stage a fully validated archive before owner setup,
then normal startup applies it before exposing bootstrap. Migration check can
validate that archive's SQLite schema and the current binary's upgrade path,
but it cannot create an owner or bypass restore validation.

## Operations and tests

The runbook covers pre-upgrade backup, version inspection, migration check,
normal startup application, progress marker interpretation, failed transaction,
future schema, corrupt database, insufficient space, rollback, restored startup,
and systemd/Compose/Kubernetes/rootless-container command examples.

Tests cover:

- no database, clean current database, every supported source version through
  the then-current maximum, future version, missing/duplicate ledger entries,
  corruption, BUSY/LOCKED, read-only files, symlinks, wrong modes, lock conflict,
  low space, and pending restore;
- atomic marker publication, crash at every phase, stale marker replacement,
  redaction, bounded fields, and failure to create/write/fsync/rename;
- migration check using a private copy, complete cleanup, source database byte
  identity, schema equivalence, invariant checks, and incompatible backup;
- owner-only normal UI, public readiness redaction, no network recovery route,
  no mutation endpoint, names rather than IDs, and responsive accessibility;
- systemd, Compose, Kubernetes, IPv4/IPv6, stopped release, and actual
  backup/migrate/fail/rollback/retry drills;
- at least 90 percent backend statement and frontend line coverage plus race,
  shuffle, `go vet`, `goimports-reviser`, `golangci-lint`, vulnerability,
  frontend lint/build/audit, container, and release gates.

Acceptance requires an operator to diagnose a failed migration without a
working browser application, validate the exact backup and upgrade path without
changing live bytes, recover through retry or rollback, and prove that no DSN,
alternate metric backend, unauthenticated listener, online mutation endpoint,
or general restart action was introduced.

## Version and approval boundary

This is schema-free batch `migration-recovery-ops` after
`operator-recovery-cli`. It works with the then-current canonical migration
list and backup reader and does not change backup format `v4`.

Implementation requires separate explicit approval for the local status/check
commands, the private migration status marker, owner-only read-only UI, startup
marker writes, backup-copy preflight, deployment/runbook behavior, and the
explicit decision to keep arbitrary DSN/driver changes and online migration
actions out of Beat.
