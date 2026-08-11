# Traffic calibration

Updated: 2026-07-30

Status: reviewed design; implementation requires explicit approval

## Goal and competitor evidence

DStatus exposes both a destructive traffic reset endpoint and editable traffic
calibration date/value fields. Its monthly calculation adds the configured
calibration value to stored daily totals. Beat already derives upload/download
deltas from Agent cumulative counters in MTS and calculates the active billing
cycle, but cannot correct an imported, incomplete, or provider-reported opening
balance without changing metric history.

Beat will provide the useful accounting outcome through immutable calibration
events. This calibration feature never deletes, rewrites, or backfills raw MTS
traffic history. Separately approved explicit history erasure may remove those
rows under the fixed `v17` contract in
[`metric-erasure-design.md`](./metric-erasure-design.md).

Evidence fixed to the reviewed DStatus commit
`4afc9e43c9df28096352c05ae924fcadbc830a2f`:

- `modules/servers/index.js`: `POST /admin/servers/:sid/reset-traffic` deletes
  DStatus's stored traffic aggregates;
- `database/servers.js`: editable `traffic_calibration_date` and
  `traffic_calibration_value` application fields;
- `modules/stats/monthly-traffic.js`: billing usage plus a calibration value.

The upstream date is not used to delimit its daily aggregation consistently.
Beat therefore adopts the product capability, not that implementation.

## Product semantics

A calibration states the accounted upload and download totals immediately after
one effective MTS second inside the node's current billing cycle. For a query
period containing that second, the resolved totals are:

```text
sent     = calibrated_sent     + raw_sent_after_effective_second
received = calibrated_received + raw_received_after_effective_second
```

The existing `up`, `down`, `sum`, `min`, and `max` quota modes are then applied
to those directional totals. A reset is only a shortcut that creates a
calibration with both target totals set to zero.

Rules:

1. Events form a per-cycle state machine. If its latest state is `set`, that
   calibration wins; if it is `revoke`, accounting returns to raw MTS. Revoking
   never reactivates an older event.
2. A calibration never carries across the next configured reset boundary.
3. No calibration means the existing raw MTS cycle aggregation is unchanged.
4. Raw history charts, cumulative host counters, and daily/weekly/calendar-
   monthly operational reports remain unadjusted and visibly factual.
5. Current-cycle public/admin summaries, quota percentage, remaining allowance,
   threshold alerts, and any future explicitly named billing-cycle report use
   the same calibrated resolver.
6. Changing a node's reset day does not move an event. The resolver reevaluates
   whether its fixed effective instant belongs to the newly active cycle.
7. Negative targets, future instants, and targets above 8 PiB per direction are
   rejected. API byte values use canonical base-10 strings to avoid JavaScript
   integer truncation.

## MTS boundary and concurrency

MTS remains the sole store for every Agent-reported counter, derived delta, and
numeric traffic time series. SQLite calibration targets are administrator-
declared accounting metadata, not copied metric samples.

The current MTS points use second precision, so calibration and ingestion must
share one per-node traffic-accounting lock. Ingestion assigns its Server receipt
second only after acquiring this lock; a timestamp captured while waiting may
not be written after a calibration. The calibration service:

1. acquires the same node lock used to derive and write traffic deltas;
2. flushes the node's pending write path and selects a committed effective
   second no earlier than the billing start and no later than server time;
3. queries the raw directional totals through that second for preview and
   validation, without storing those observed totals in SQLite;
4. commits the calibration event and redacted administrator audit in one
   immediate SQLite transaction;
5. prevents a later report from being stamped into the effective second before
   releasing the lock, so subsequent deltas are strictly after the boundary.

The implementation may wait for the next second or use a persisted monotonic
traffic-write watermark. It must not future-date a sample silently. Restart and
restore must preserve the boundary; an in-memory-only timestamp guard is
insufficient. Beat's embedded SQLite/MTS deployment remains a single-writer
Server protected by the startup lock. A second process sharing the data
directory must fail before serving, not attempt cross-process metric writes.

Queries use explicit half-open intervals. Raw totals through the effective
second are replaced by the target; only deltas with timestamps strictly after
that second are added. Tests must prove no double count or gap when ingestion,
calibration, backup, alerts, and public refresh run concurrently.

If MTS read/flush fails, no calibration is committed. If the SQLite transaction
fails, ingestion resumes with no visible adjustment. The API never reports a
successful reset while either store is uncertain.

## Persistence and migration

Canonical SQLite migration `v16`, release batch `traffic-calibration`, as
assigned in
[`commercial-parity-implementation-sequence.md`](./commercial-parity-implementation-sequence.md),
adds an append-only `traffic_calibration_events` ledger. Each row contains:

- UUID, `set` or `revoke` kind, node UUID and node-name snapshot;
- effective UTC second for `set` events;
- exact target sent/received byte strings normalized to checked signed integers;
- mandatory reason, actor UUID/username snapshot, session/request IDs, and UTC
  creation time;
- optional referenced event UUID for `revoke`;
- caller-generated request UUID with a uniqueness constraint for idempotency.

Database checks enforce the kind-specific fields, non-negative bounded targets,
valid references, and immutable creation data. A `revoke` can reference only an
active `set` event for the same node. A correction atomically appends one revoke
and one replacement set; it never updates or deletes the original event.

The node UUID is a nullable `ON DELETE SET NULL` reference so deletion cannot
erase accounting history. The event ledger is retained with security audit
policy. Normal metric
retention and MTS compaction do not touch it. Traffic-delta retention may not
delete any point in an active billing cycle; maintenance derives the oldest
active cycle start across nodes before deletion. Node deletion preserves the
redacted node-name snapshot and event history but makes the event ineligible for
future resolution. No background job copies resolved totals into SQLite.

A successful `v17` erasure containing traffic measurements is a new accounting
boundary. Calibration events at or before its cutoff remain immutable audit
history but do not influence later current-cycle totals; an owner may append a
new calibration after the cutoff. The erasure worker never edits this ledger.

Migration `v16` depends on the deployed account-security audit and current MTS
traffic aggregation. It is ordered after `v15` solely to keep one canonical
release train; it does not depend behaviorally on edge access policy.

## Authorization and API

Calibration changes customer-visible accounting and alert behavior. Every
mutation is owner-only, requires recent password/TOTP authentication, enforces
same-origin cookie policy, uses a bounded JSON body, and emits both success and
failure audit events without target values in generic logs.

Admin routes:

- `GET /api/v1/admin/nodes/{node_id}/traffic-calibrations` returns a bounded,
  cursor-paginated event history with actor display names and resolved state;
- `POST /api/v1/admin/nodes/{node_id}/traffic-calibrations` appends a set event;
- `POST /api/v1/admin/nodes/{node_id}/traffic-calibrations/{id}/revoke` appends
  a revoke event;
- `POST /api/v1/admin/nodes/{node_id}/traffic-calibrations/{id}/replace`
  atomically appends revoke and replacement events.

The create body accepts a request UUID, optional effective UTC second, target
sent/received byte strings, and a 1-500 character reason. Omitting the instant
uses the latest safe committed boundary selected under the accounting lock.
Explicit instants must be inside the active billing cycle and not after that
safe boundary. Duplicate identical request UUIDs return the original result;
conflicting reuse returns `409`.

Public REST and WebSocket traffic summaries add only:

- `accounting_mode`: `raw` or `calibrated`;
- `calibrated_at`: nullable UTC timestamp.

They never expose actor, reason, event ID, raw-versus-target difference, or
revoked history. Authenticated node responses may expose the active event ID
and reason only to authorized administrators. MTS/query failures fail the
traffic summary instead of silently returning zero or a stale calibration.

## Administration experience

The node card overflow menu adds a gauge-adjustment icon action named
`Calibrate traffic`. The owner dialog uses shadcn/ui `Dialog`, `FieldGroup`,
`Field`, `Input`, `Textarea`, `Alert`, and `Button` components:

- a `ToggleGroup` selects `Set accounted usage` or `Reset to zero`;
- upload and download fields use human-readable binary units while preserving
  exact decimal byte strings at the API boundary;
- a read-only comparison shows current accounted totals, raw cycle totals, and
  the effective instant with names and labels rather than IDs;
- the mandatory reason explains the correction for later operators;
- recent authentication occurs through the existing reauthentication dialog;
- submission disables controls, shows `Spinner`, and is idempotent on retry.

An `AlertDialog` confirms reset, revoke, or replacement because each changes
public quota and alert behavior. A history `Sheet` uses `Table`, `Badge`, and
`Empty` for active/revoked/corrected events. It shows node and actor names, not
machine IDs. Long names and IPv6 addresses truncate with accessible full-value
tooltips, and no card is nested inside another card.

The public traffic component adds a quiet `Adjusted` badge and the effective
time when calibrated. It does not interrupt background refresh, reset selected
groups/ranges, or display the private reason.

## Backup, restore, retention, and rollback

Backup archive format remains `v4`. The complete SQLite snapshot already
contains calibration and later erasure-boundary application events, while MTS
logical export retains every non-erased raw delta. Backup holds the global
accounting-mutation barrier from the MTS cutoff through its SQLite snapshot, so
an event cannot be included without the corresponding raw-row/erasure state.
Restore validation checks event constraints, references, node linkage, request
UUID uniqueness, erasure boundaries, and that every effective instant is
representable by the restored MTS precision.

After restore, current billing summaries are recomputed from the restored
ledger and MTS; no cached adjusted total is trusted. A restore lacking a valid
referenced event fails closed. Earlier supported backup formats restore with an
empty calibration ledger and preserve raw behavior.

Rollback restores the pre-`v16` SQLite backup and matching binary. MTS needs no
rollback because the batch never mutates its historical rows or adds a new
measurement. There is no down migration and no destructive cleanup command.

## Tests and acceptance

Backend tests cover all quota modes, reset-day month ends, zero/nonzero targets,
multiple/revoked/replaced events, exact-boundary intervals, missing data,
overflow, malformed decimal strings, idempotency, authorization, recent auth,
audit redaction, pagination, and response privacy.

Concurrency and recovery tests cover ingestion at the same second, process
restart, second-writer startup rejection, MTS/SQLite failures, backup cutoff,
restore, retention, node deletion, reset-day changes, alerts, and WebSocket/REST
agreement. They prove raw MTS rows and operational reports are byte-for-byte
unchanged by create, replace, revoke, and rollback.

Frontend tests cover exact unit conversion, reset/set modes, labels rather than
IDs, keyboard/focus behavior, pending/error/idempotent retry states, long text,
mobile overflow, silent public refresh, and all supported locales.

Completion requires at least 90 percent backend statement and frontend line
coverage, race/shuffle, `go vet`, `goimports-reviser`, `golangci-lint`, module
and vulnerability verification, frontend audit/test/lint/build, browser smoke,
backup/upgrade/rollback drills, and IPv4/IPv6 public/admin isolation.

## Approval boundary

Implementation requires explicit approval because it adds SQLite migration
`v16`, owner-only APIs, an append-only accounting ledger, recent-auth actions,
new public response fields, shared ingestion/calibration locking, alert behavior,
and administration UI. Approval must explicitly preserve immutable MTS history,
SQLite-only application metadata, raw historical reports, and login-free public
monitoring.
