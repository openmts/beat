# Application data lifecycle

Updated: 2026-07-30

Status: reviewed design; implementation requires explicit approval

## Goal and current evidence

Keep SQLite application history bounded, queryable, and recoverable without
turning it into a metric store. Agent, probe, traffic, derived, accelerator,
and diagnostic time-series values remain exclusively in MTS.

The deployed database is currently small, but the growth paths are not bounded:

- `alert_events` loads the complete history and has no retention cleanup;
- `admin_sessions` has an expiry cleanup method, but production never calls it,
  and the security page loads every session for the current user;
- `admin_audit_events` is paginated and has a 180-day delete method, but cleanup
  runs only as a side effect of MTS maintenance and stops when automatic MTS
  cleanup is disabled;
- `admin_backups` loads every record and has no startup reconciliation between
  SQLite records, archives, partial files, and the pending-restore journal;
- backup deletion removes the archive before deleting its row, so a SQLite
  failure leaves a record that cannot be retried, while restore staging writes
  the journal before the final row update, so an API error can leave a restart
  side effect unless startup verifies committed state.

Configuration collections such as groups, nodes, rules, channels, tasks,
schedules, SSH keys, users, and site settings are intentionally not classified
as history merely because their current list APIs return all rows. They remain
complete collections for fleet rendering and selectors, subject to their own
feature cardinality and response budgets.

Komari's pinned Server includes explicit expired-session cleanup and bounded
metric-history result counts. Beat requires the stronger application-history
contract below as a commercial durability and operability baseline.

## Invariants

1. SQLite stores application entities, policy, inventory, security/audit
   evidence, backup metadata, and non-numeric job state. It never stores Agent
   samples, probe values, traffic deltas, chart points, or calculated metric
   totals.
2. Historical APIs are server-filtered and cursor-paginated. No handler loads
   an unbounded history and filters it in the browser.
3. Expired sessions and temporary artifacts are hygiene, not optional data
   retention. Their cleanup continues even when MTS retention is disabled.
4. Active alert incidents, ready/validated/staged backup archives, a pending
   restore, and active security sessions are never removed by age cleanup.
5. Cleanup uses bounded batches, short transactions, cancellation, and a
   resumable cursor. It never holds a transaction during filesystem I/O,
   archive validation, `VACUUM`, MTS work, or network I/O.
6. A backup record, archive, and restore journal expose one reconciled state.
   A returned error must not hide an automatically applied restore.
7. Filesystem reconciliation accepts only generated basenames and regular
   files below the private backup root. It never follows symlinks or accepts a
   path from SQLite, an HTTP request, a manifest, or an environment value as a
   deletion target.
8. Cleanup failures remain visible and retryable. They do not fabricate
   success, silently discard a valid archive, or make old history look empty.

## Canonical migration

Canonical SQLite migration `v19 application-data-lifecycle` follows `v18`
Fleet status summaries and adds:

- one `application_retention_settings` row containing alert-event, audit-event,
  and terminal backup-record retention, cleanup hour, last run/result, bounded
  progress, and updated time;
- immutable `rule_name_snapshot`, `node_name_snapshot`, and metric display
  snapshot fields on `alert_events`, backfilled from current entities where
  possible and from the existing message otherwise;
- composite alert-event indexes for `(triggered_at DESC, id DESC)`, status,
  rule, and node cursor filters;
- backup lifecycle states `interrupted`, `missing`, `staging`, and `deleting`,
  plus update/reconciliation time and a random restore generation used to bind
  a committed row to the pending journal;
- indexes required by cleanup batches and backup cursor pagination.

Defaults are 180 days for resolved alert events, 180 days for administrator
audit events, 30 days for revoked sessions, and 90 days for terminal
failed/interrupted/missing backup records. Alert and audit retention are owner
configurable in bounded ranges; session and temporary-file hygiene limits are
fixed security policy. Ready, validated, staged, and deleting backup records
are not age-expired.

The complete SQLite snapshot already includes the new application fields, so
backup archive format remains `v4`. Restoring an older supported archive runs
normal forward migration and then reconciliation before HTTP starts.

## Query contracts

The first revision changes these authenticated list contracts:

- `GET /api/v1/alerts/events`: `limit`, opaque cursor, status, rule, node, and
  bounded time filters; returns items, resolved display snapshots, and
  `next_cursor`;
- `GET /api/v1/admin/audit-events`: replaces offset traversal with an opaque
  `(created_at,id)` cursor while retaining action and actor filters;
- `GET /api/v1/admin/backups`: cursor-paginated records with reconciled state,
  never filesystem paths;
- `GET /api/v1/admin/sessions`: active and recently revoked sessions only,
  with an explicit bounded page and current-session marker.

Default page size is 50 and the hard maximum is 200. Cursors are versioned,
authenticated opaque values bound to the normalized filter. Invalid, expired,
or cross-filter cursors return `400`; a cursor never contains a path, token,
username, archive filename, or secret.

Configuration/fleet list routes remain collection responses in this batch.
Their size and cache budgets belong to ingress governance and their owning
feature designs, not to application-history retention.

## Cleanup worker

A supervised `application_gc` worker runs independently of MTS maintenance at
the configured UTC hour and may also be started manually from Data and storage.
Each pass repeatedly deletes at most 500 eligible rows per short transaction,
commits its cursor/counts, yields, and stops promptly on cancellation.

It performs these fixed operations:

1. remove expired sessions and revoked sessions older than 30 days;
2. remove resolved alert events older than policy while preserving every
   triggered event;
3. remove audit events older than policy;
4. reconcile backup records, archives, partials, work directories, and the
   pending journal;
5. remove terminal failed/interrupted/missing backup records only after proving
   no archive or pending journal references them;
6. remove generated partial/work artifacts older than 24 hours when no active
   backup, upload, validation, restore, or operation lock owns them.

Successful-login creation enforces at most 32 active sessions per user in the
same immediate transaction, revoking the oldest excess sessions and recording
an audit event through the transaction-aware envelope from
[`administrative-mutation-consistency-design.md`](./administrative-mutation-consistency-design.md).
This prevents successful authenticated traffic from growing the session table
without bound between cleanup runs without splitting login/session/audit
commit evidence.

MTS retention continues to own only MTS rows and compaction. Manual maintenance
may start both operations, but their results and enablement remain distinct so
disabling MTS age deletion cannot disable security/application hygiene.

## Backup reconciliation

Startup reconciliation runs after the data directory is locked and before HTTP
serves:

- a `running` record becomes `interrupted`, unless its final archive exists and
  passes complete validation, in which case it becomes `ready`;
- a ready/validated record whose regular archive is missing becomes `missing`;
- an allowlisted orphan archive is moved to a private quarantine inventory and
  never silently imported or deleted;
- stale generated partial/work paths are removed only through validated
  root-relative handles;
- a `deleting` record idempotently removes an existing regular archive and then
  deletes the row; missing is success for this state;
- a pending restore is applied only when the live SQLite record is `staged` and
  its random generation matches the journal. A `staging` row or mismatched
  journal is rolled back to its previous ready/validated state and cannot apply.

Restore staging first commits `staging` plus a generation, validates and writes
the private journal, then commits `staged` with the same generation. Startup
uses SQLite as the authority before replacing live stores. A request retry uses
an idempotency key and returns the committed state; the unavoidable case where
the final commit succeeds but the response is lost is resolved by `GET`, not by
creating a second restore request.

Backup deletion first commits `deleting`, then removes the archive, then
deletes the record. Any failure leaves a visible retryable state rather than a
ready row pointing to a missing file.

Every manual cleanup, backup reconciliation, deletion, and restore transition
carries the originating actor, exact HTTP request ID, audit linkage, backup ID,
and restore generation where applicable. Accepted and terminal or `unknown`
events reuse the administrative mutation envelope; reconciliation uses a fixed
system actor and never claims cross-SQLite/filesystem atomicity.

## Frontend and operations

The shadcn/ui `base-nova` administration uses server-side filters, compact
pagination controls, readable node/rule names, status badges, and explicit
empty/error states. It never falls back to showing a node, rule, session,
backup, or actor ID as display text.

Data and storage separates MTS retention from Application history. Operators
see policy, last successful cleanup, failed category, counts, and the number of
quarantined or missing backup artifacts without seeing server paths. Backup
rows expose Retry cleanup, Validate, Download, Delete, or Resolve actions only
when valid for their reconciled state.

Prometheus and structured logs expose cleanup runs, deleted rows by fixed
category, backlog age, reconciliation outcomes, retries, and sanitized failure
codes. They exclude usernames, session IDs, node/rule IDs, filenames, paths,
messages, addresses, audit details, and archive contents. Runtime resilience
marks the worker degraded after repeated failure or excessive backlog and not
ready when the worker exits or cannot acquire its required store ownership.

## Tests and acceptance

Tests cover:

- fresh `v19`, sequential `v1-v19` upgrade equivalence, future-version
  rejection, backup `v4` creation/restore, and pre-migration rollback;
- proof that SQLite contains no Agent/probe/traffic/chart time-series rows;
- cursor order, duplicate timestamps, filter binding, maximum limits, malformed
  cursors, deleted node/rule name snapshots, and no raw-ID display fallback;
- active versus resolved alerts, audit cutoff boundaries, session expiry,
  32-session enforcement, cancellation, batch restart, and cleanup races;
- every backup record/archive/journal state pair, crash point, missing file,
  symlink, directory, orphan, partial, update failure, delete retry, stage retry,
  and response-loss ambiguity;
- automatic cleanup with MTS retention enabled and disabled, maintenance
  overlap, backup/restore operation locks, worker supervision, readiness, logs,
  and metrics redaction;
- at least 90 percent backend statement and frontend line coverage plus race,
  shuffle, `go vet`, `goimports-reviser`, `golangci-lint`, vulnerability,
  frontend lint/test/build/audit, container, and IPv4/IPv6 gates.

Acceptance requires bounded historical responses, independent daily hygiene,
preserved active incidents and backup archives, retry-safe backup lifecycle,
names rather than IDs, and restart tests proving an uncommitted restore journal
cannot modify live data.

## Approval and rollback

Implementation requires explicit approval for migration `v19`, application
retention settings, alert display snapshots and indexes, cursor API response
changes, the active-session cap, the independent cleanup worker, new backup
states, startup filesystem/SQLite reconciliation, restore generation checks,
and delete/stage ordering changes.

Before the first `v19` write, rollback may restore the pre-migration backup.
After migration, old binaries must not open the newer schema; rollback restores
the matching pre-`v19` SQLite, MTS, backup directory, journal, Server, Agent,
and frontend artifacts.
