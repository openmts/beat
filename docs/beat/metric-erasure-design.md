# Metric history erasure

Updated: 2026-07-30

Status: reviewed design; implementation requires explicit approval

## Goal and evidence

Beat needs explicit, auditable removal of metric history in addition to normal
age-based retention. Komari exposes administrator actions that clear system
history, clear all system and ping history, erase one client's metric series
when that client is deleted, and erase one ping task's history.

Evidence:

- <https://github.com/komari-monitor/komari/blob/4077201f098774511eaf504f220c5f6be009346b/web/rpc/jsonrpc/admin.client.go>
- <https://github.com/komari-monitor/komari/blob/4077201f098774511eaf504f220c5f6be009346b/web/rpc/jsonrpc/admin.misc.go>
- <https://github.com/komari-monitor/komari/blob/4077201f098774511eaf504f220c5f6be009346b/internal/metricstore/deletion.go>

Beat currently has fixed-list retention cleanup and synchronously deletes MTS
probe rows before deleting a network task. Deleting a node removes only its
SQLite row, leaving unreachable MTS series. A task deletion can also erase MTS
successfully and then fail its SQLite deletion, leaving a live task with lost
history. This design replaces both unsafe lifecycle paths and adds explicit
owner-controlled erasure.

## Invariants

1. Agent, probe, traffic, derived, accelerator, and diagnostic numeric rows are
   deleted only from MTS. SQLite stores job, policy, entity snapshot, audit, and
   progress metadata, never metric samples or calculated metric totals.
2. HTTP requests never perform a long MTS delete or compaction. They commit one
   durable job and return `202`; a leased worker performs idempotent steps.
3. The API accepts a fixed scope enum and existing entity IDs only. It never
   accepts a measurement name, tag map, time range, filesystem path, MTS query,
   SQL fragment, or arbitrary predicate.
4. Every mutation requires an owner session, recent reauthentication, the
   exact display-name or fixed all-history confirmation, and an audit event.
5. MTS tombstones make rows immediately unqueryable. Disk reclamation is a
   separate compaction phase and a compaction failure never makes deleted rows
   visible again.
6. A node or network task being deleted becomes disabled and hidden in the same
   SQLite transaction that creates its erasure job. It cannot receive new
   reports, assignments, probes, public exposure, or management actions.
7. Backup, restore staging, retention cleanup, explicit erasure, and MTS logical
   export share one exclusive operation lock. They cannot observe or publish a
   partially erased logical snapshot.
8. Raw billing traffic is erased only by an explicit applicable scope. Traffic
   calibration events do not rewrite or protect the corresponding MTS rows. A
   successful erasure cutoff becomes an accounting boundary: older calibration
   events remain as audit history but no longer affect later resolved totals;
   the owner may create a new calibration after the cutoff.

## Fixed scope model

The first revision supports only these scopes:

| Scope | Target | MTS effect | SQLite effect |
| --- | --- | --- | --- |
| `node_history` | one existing node | every registered measurement tagged with that node, including assigned probe rows | keep node active |
| `network_task_history` | one existing task | every `network_probe` series tagged with that task | keep task active |
| `fleet_telemetry` | none | all registered Agent/system/traffic/derived measurements, preserving `network_probe` | none |
| `all_metric_history` | none | every registered MTS measurement, including `network_probe` | none |
| `delete_node` | one existing node | same as `node_history` | tombstone immediately; remove entity application rows after MTS success |
| `delete_network_task` | one existing task | same as `network_task_history` | tombstone immediately; remove task application rows after MTS success |

The registered-measurement inventory comes from the deployed metric catalog
after migration `v8`. Until that migration is deployed, an implementation must
use the same closed built-in list as backup and retention and prove that every
owned measurement is present. Unknown MTS measurements fail the job closed and
surface an operator error; they are never silently skipped.

All scopes erase from the Unix epoch through a cutoff captured after the worker
has acquired the exclusive MTS operation lock and flushed pending writes. Node
and task deletion first revokes their write authority, so no later row can
reappear. Fleet-wide scopes pause ingestion only while the cutoff and
tombstones are published; normal writes resume before compaction.

For scopes containing traffic measurements, successful job metadata is also the
canonical calibration resolver boundary. Current-cycle quota and alert totals
use only raw rows and calibration events after that cutoff. Historical traffic
reports naturally show no erased rows. The append-only calibration ledger is
not deleted or rewritten.

## Durable state and migration

Canonical SQLite migration `v17 metric-erasure` adds:

- `metric_erasure_jobs`: random ID, scope, optional target type/ID and immutable
  display-name snapshot, state, cutoff, registered catalog digest, next
  measurement cursor, attempt count, lease owner/expiry, request idempotency
  key, requester/audit linkage, timestamps, sanitized error code, and result;
- node and network-task lifecycle state needed for `active`, `deleting`, and
  finalization, with indexes that keep normal lists restricted to active rows;
- uniqueness preventing two unfinished lifecycle jobs for the same entity and
  preventing more than one fleet-wide erasure at a time.

States are `queued`, `claimed`, `deleting`, `compacting`, `finalizing`,
`succeeded`, `failed`, and `canceled`. Cancellation is allowed only while a job
is still `queued`; after the first MTS tombstone it must run to a terminal state
or be retried. Expired leases return to `queued` with bounded exponential
backoff. Each measurement delete is idempotent, and the next cursor advances
only after that delete succeeds.

The complete SQLite snapshot already includes these application records, so
backup format remains `v4`. Backup cannot start while a deletion owns the MTS
operation lock. A restore containing `claimed`, `deleting`, `compacting`, or
`finalizing` jobs normalizes them to `queued` and resumes them; a coherent
pre-erasure backup intentionally restores the older history and job state.

## Entity deletion transaction

`DELETE /api/v1/nodes/{id}` and
`DELETE /api/v1/network/tasks/{task_id}` become asynchronous lifecycle
requests. Each endpoint:

1. verifies owner role, recent authentication, target existence, and exact
   confirmation;
2. begins an immediate SQLite transaction;
3. marks the entity `deleting`, disables credentials/scheduling/public access,
   snapshots its display name, creates the erasure job, and writes the audit;
4. commits and returns `202` with the job ID and state.

After MTS deletion and compaction succeed, finalization removes dependent
application rows and the active entity row in one transaction while retaining
the bounded erasure job and audit history. A failed job leaves the entity
hidden and disabled with a visible retry action. There is no force-delete path
that discards the durable job or makes an entity active after partial erasure.

This is an intentional behavior change from the current synchronous `204`
node/task deletion contract and therefore requires explicit approval and API,
frontend, runbook, and client test updates in the same batch.

## API

Owner-only routes under `/api/v1/admin/metric-erasure`:

- `GET /jobs`: list bounded recent jobs and active failures;
- `GET /jobs/{id}`: return state, scope, target display name, timestamps,
  progress, and sanitized failure code;
- `POST /jobs`: create one explicit history-only job;
- `POST /jobs/{id}/retry`: retry a failed job after recent authentication;
- `POST /jobs/{id}/cancel`: cancel a queued job only.

Create requests submit `scope`, an optional `target_id`, an idempotency key, and
the confirmation value. Responses resolve and return display names alongside
IDs. Public, Agent, service-token, and ordinary admin principals cannot invoke
or inspect erasure jobs in the first revision.

## Administrator experience

`/admin/settings` adds an unframed `Data and storage` section with separate
`Retention` and `Erase history` tabs. Retention remains the routine automatic
policy. Erasure is presented as exceptional and irreversible.

The shadcn/ui `base-nova` surface uses `Tabs`, `FieldGroup`, `Select`, `Alert`,
`AlertDialog`, `Progress`, `Badge`, and `Table`. Scope and entity selectors
always show names while submitting IDs. The confirmation dialog states exactly
which node/task/history classes are affected, whether the entity will also be
removed, whether a recent ready backup exists, and that restoring an older
backup is the only recovery path. Fleet-wide erasure requires the fixed phrase
`ERASE ALL METRIC HISTORY` or `ERASE FLEET TELEMETRY`.

When no ready backup was created in the last 24 hours, the dialog offers a
direct Create backup action and requires a separate acknowledgement before the
owner may continue. It does not silently create a potentially large backup.
Running jobs show phase and registered-measurement progress without estimating
deleted row counts that MTS cannot prove cheaply.

Node and network-task delete dialogs use the same job component. The entity
disappears from ordinary lists after the request commits, while a compact
Operations link exposes retry status. No raw IDs, measurement keys, tag maps,
MTS paths, or SQL appear in user-facing text.

## Observability and failure handling

Structured logs include job ID, fixed scope, target type, phase, attempt,
duration, and sanitized error code. They exclude target addresses, metric
values, tags, confirmation text, archive paths, and MTS internals.

Prometheus metrics include jobs by state/scope, phase duration, retries,
failures, tombstone operations, and compaction outcomes. Readiness adds a
`metric_erasure` check: queued/running work remains `ok`; an exhausted job is
reported as degraded operational detail and an unavailable MTS lock/worker is
an error. Monitoring reads remain available unless MTS itself is unhealthy.

Worker shutdown releases its lease and leaves the cursor committed. A restart
resumes at the next measurement. If final SQLite cleanup fails after MTS
success, the job retries only finalization and never reruns an unrelated
history scope. If audit persistence or the initial entity tombstone fails, no
job is published and the entity remains unchanged.

## Tests and acceptance

Tests cover:

- every scope-to-measurement/tag mapping, including future catalog entries;
- proof that SQLite contains job/application metadata but no metric values;
- owner/admin/public/Agent/service-token and recent-auth boundaries;
- exact name/phrase confirmation, ID/name rendering, and idempotency conflicts;
- node/task tombstone transaction rollback at every write;
- concurrent Agent reports, task results, backup, retention, restore staging,
  two workers, Server shutdown, lease expiry, retry, and compaction failure;
- MTS immediate query invisibility, restart persistence, partial tag matching,
  network-task isolation, fleet telemetry preserving probes, and all-history
  removal;
- fresh `v17`, upgrade from every `v1-v16` fixture, future-version rejection,
  backup `v4` create/restore, and interrupted-job reconciliation;
- shadcn dialog accessibility, keyboard flow, responsive overflow, names rather
  than IDs, progress/error states, and browser authorization tests;
- at least 90 percent backend statement and frontend line coverage plus race,
  shuffle, `go vet`, `goimports-reviser`, `golangci-lint`, vulnerability,
  frontend lint/build/audit, container, and IPv4/IPv6 acceptance gates.

Acceptance requires node deletion to leave no queryable node MTS series, task
deletion to be restart-safe, fleet-only erasure to preserve probes, all-history
erasure to remove every registered series, and every failure to remain visible
and retryable without exposing an unauthenticated or arbitrary-delete surface.

## Rollback and approval boundary

Before the first erasure request, rollback may restore the pre-`v17` SQLite
snapshot. After any erasure has started, an old binary must not be used against
the newer schema; rollback requires the matching pre-migration SQLite and MTS
backup. MTS tombstones are not reversible in place.

Implementation requires explicit approval for migration `v17`, the asynchronous
`202` node/task deletion behavior, owner-only/recent-auth enforcement, durable
leases, MTS tag-scoped tombstones and compaction, lifecycle tombstones, backup
coordination, confirmation phrases, and the absence of arbitrary measurement,
tag, time-range, SQL, or filesystem inputs.
