# Fleet status summaries

Updated: 2026-07-30

Status: reviewed design; owner approved implementation on 2026-10-15 (blanket approval of all pending items)

## Goal and evidence

Beat needs scheduled and on-demand fleet availability summaries in addition to
per-node offline/recovery alerts. DStatus sends online/offline counts at 08:00
and 20:00, includes node names when each list is small, sends an initial summary
after startup, and exposes an administrator trigger.

Evidence:

- <https://github.com/fev125/dstatus/blob/4afc9e43c9df28096352c05ae924fcadbc830a2f/modules/autodiscovery/index.js>
- <https://github.com/fev125/dstatus/blob/4afc9e43c9df28096352c05ae924fcadbc830a2f/modules/notification/index.js>

The Beat equivalent reuses the canonical availability evaluator and future
durable notification outbox. It does not infer status from ad hoc metric rows
or send a notification on every process restart.

## Product contract

- Cadence is `daily` or `weekly`; multiple daily times are represented by
  multiple schedules, so 08:00 and 20:00 require two explicit rows.
- Every schedule has an IANA time zone, local hour/minute, enabled state,
  group/node scope, channel scope, and a bounded name-list threshold from 0 to
  50. The default threshold is 10.
- Daily period keys use the configured local calendar date. Weekly schedules
  also select ISO weekday 1-7.
- Counts use the same heartbeat-age, debounce, and persisted online/offline
  transition state as availability alerts at the schedule cutoff.
- The summary always contains online, offline, and total counts. Online and
  offline node display names are included independently only when that list is
  nonempty and does not exceed the configured threshold.
- Node and group selectors display names and stable labels, never raw IDs.
- Manual test runs use current status, are visibly marked as tests, and do not
  advance automatic schedule cursors.

Private nodes may be included because delivery is an authenticated management
configuration, but the administrator must explicitly scope each channel. The
payload includes display name, group display name when useful, status, and
summary timestamp. It excludes IP addresses, hostnames, tokens, prices,
remarks, metric values, and raw heartbeat timestamps.

## Startup and recovery behavior

DStatus sends once 45 seconds after each process start. Beat rejects that exact
behavior because crash loops, rolling deployments, and orchestrator restarts
can create a notification storm.

The safe equivalent is:

1. durable schedules continue from their committed `next_run_at` cursor;
2. a missed due period is claimed once after readiness returns and enqueued with
   its original period key;
3. an administrator may send a current test summary at any time;
4. Beat does not create a new event solely because the Server process started.

This preserves scheduled and immediate operator visibility while making restart
recovery deterministic and deduplicated. Any future distinct deployment or
incident-recovery notification requires a separate event design and approval.

## Persistence and migration

Canonical SQLite migration `v23 fleet-status-summary` adds:

- `fleet_summary_schedules`: ID, name, cadence, time zone, local time, optional
  weekday, name-list threshold, enabled state, `next_run_at`, claimed period
  key, last run/result counts, creator/updater, and timestamps;
- `fleet_summary_groups`, `fleet_summary_nodes`, and
  `fleet_summary_channels`: explicit scope joins with foreign keys;
- `fleet_summary_runs`: bounded execution metadata, schedule/name snapshots,
  period key, status counts, outbox correlation, delivery result, timestamps,
  and sanitized error code.

These tables contain application scheduling and delivery metadata only. They do
not store metric samples, heartbeat series, historical availability points, or
rendered notification bodies. Numeric Agent data remains exclusively in MTS.

Migration `v23` depends on commercial notification delivery migration `v10`,
the deployed availability state machine, and the node/group model. It is ordered
after `v22` to preserve one global consecutive migration line. The complete
SQLite snapshot includes the new tables, so backup format remains `v4`.

Restore normalizes an in-progress scheduler lease and outbox claim through the
existing notification reconciliation rules. A restored `next_run_at` in the
past produces at most one catch-up period per schedule before advancing to the
next future occurrence; it never floods every missed period.

## Scheduler and delivery

The scheduler calculates the next occurrence with the standard time-zone
database. Ambiguous daylight-saving times choose the first occurrence and
nonexistent local times advance to the first valid instant on that date. The
resolved UTC time and local period key are persisted and tested.

When due, one immediate SQLite transaction:

1. verifies the row is still enabled and due;
2. snapshots the configured node/group/channel scope and current availability
   counts/names;
3. creates one `fleet_summary_runs` row;
4. enqueues one typed `fleet.status_summary` outbox event per enabled channel
   with a unique schedule/period/channel key;
5. advances `next_run_at` and commits the claimed period.

The notification worker performs provider retries and dead-letter handling.
External providers may still deliver more than once after an ambiguous network
failure, but the durable outbox never intentionally enqueues the same automatic
schedule/period/channel twice. Disabling a channel prevents new automatic
deliveries and does not rewrite completed runs.

The event uses fixed localized templates. Webhook receives a typed payload with
schedule display name, period key, cutoff, counts, and bounded display-name
lists. Telegram, email, Bark, ServerChan, and safe custom HTTP use the same
canonical summary model when their provider batch is deployed. No free-form
JavaScript, server-side template execution, or arbitrary status query exists.

## API and UI

Admin-only routes under `/api/v1/alerts/fleet-summaries` expose list, create,
update, delete, and `POST /{id}/test-run`. Ordinary schedule edits follow the
same authority as traffic reports; they do not require recent reauthentication.
Deleting a schedule leaves bounded run/outbox audit history with a schedule-name
snapshot.

`/admin/alerts` adds a `Status summaries` tab beside availability rules,
channels, and traffic reports. The shadcn/ui `base-nova` form uses `FieldGroup`,
`Field`, `Select`, `Combobox`, `Switch`, and numeric input/stepper controls.
Schedule rows use `Table`, `Badge`, `Progress`, `DropdownMenu`, `AlertDialog`,
and `Empty`; controls submit IDs but render schedule, group, node, channel, time
zone, and weekday names.

The list emphasizes next run, local schedule, scope summary, last result, and
delivery state. The test action uses a Send icon and reports queued/success/
partial/failed through the shared delivery history rather than blocking the
dialog on external providers. Loading uses `Skeleton`, failures use `Alert`, and
feedback uses `sonner`.

## Observability and failures

Structured logs include schedule/run IDs, period key, phase, scoped counts,
channel count, result, duration, and fixed error code. They exclude node IPs,
provider secrets, raw rendered bodies, and internal SQL.

Prometheus metrics expose schedules enabled, runs by result, catch-up claims,
outbox enqueue failures, delivery outcomes, and scheduler lag. Readiness extends
the scheduler check so an unavailable fleet-summary loop is visible without
making public monitoring data private or unavailable.

If availability state cannot be read, no partial summary is sent and the run is
marked failed for retry/inspection. If one channel is disabled or missing at
claim time, it is skipped and recorded; if one delivery fails after enqueue,
the durable outbox owns retry. A schedule edit races safely through compare-and-
swap on its current version and cannot overwrite an already claimed period.

## Tests and acceptance

Tests cover:

- online/offline parity with the availability evaluator at exact heartbeat and
  debounce boundaries;
- all/group/node scopes, private nodes, deleted/hidden nodes, zero nodes, and
  independent 0/10/50 name-list thresholds;
- daily/weekly schedules, two schedules on one day, IANA zones, DST gaps and
  repeats, month/year boundaries, restart catch-up, and one-period flood bounds;
- transaction failure at every schedule/run/outbox/cursor write, concurrent
  schedulers, lease expiry, disabled channels, provider retry, and restore;
- admin/public/Agent/service-token authorization and CSRF/origin handling;
- every Select/Combobox showing names rather than IDs, responsive overflow,
  keyboard access, validation, empty/loading/error states, and test-run feedback;
- fresh `v23`, sequential `v1-v23` upgrade equivalence, backup `v4` restore,
  future-version rejection, and proof that no metric values enter SQLite;
- at least 90 percent backend statement and frontend line coverage plus race,
  shuffle, `go vet`, `goimports-reviser`, `golangci-lint`, vulnerability,
  frontend lint/build/audit, browser, container, and IPv4/IPv6 gates.

Acceptance requires two independently configured daily summaries to deliver at
their local times, a restart to produce at most one missed-period catch-up and
no startup-only event, manual test-run to leave the cursor unchanged, all
selectors and payloads to use display names, and every delivery to flow through
the durable notification outbox.

## Rollback and approval boundary

Before the first `v23` write, rollback may restore the pre-migration backup.
After schedules or runs exist, rollback requires the matching pre-`v23` SQLite
snapshot; an old binary must reject the newer schema. Outbox events already
accepted by external providers cannot be recalled.

Implementation requires explicit approval for migration `v23`, schedule and run
tables, the `fleet.status_summary` event category, admin schedule authority,
group/node/channel scoping, catch-up semantics, fixed templates, name-list
thresholds, and the intentional no-notification-on-every-start behavior.

## Implementation status, 2026-10-15

The admin control plane is implemented in the working tree:

- migration `v23` (`internal/store/sqlite_migrations_v23.go`) adds
  `fleet_summary_schedules` with revision/name-threshold/cursor columns plus the
  explicit `fleet_summary_nodes`, `fleet_summary_groups`,
  `fleet_summary_channels` scope joins and `fleet_summary_runs` audit rows with a
  `(schedule_id, period_key, kind)` uniqueness key;
- `internal/store/fleet_summary*.go` owns list/get/create/update/delete with
  compare-and-swap on `revision`, scope replacement, the single claim transaction
  that writes the run row and advances the cursor together with the outbox
  message, the test-run audit insert, and
  `ReconcileFleetSummaryRestore` (queued runs older than 7 days settle as
  `failed`/`delivery_unknown`, run audit is trimmed to 180 days);
- `internal/fleetsummary` owns validation, next-occurrence calculation, period
  keys, scoped availability snapshots, the `fleet.status_summary` request and the
  30-second scheduler loop. Catch-up is one claimed period per schedule: the
  cursor advances to the first occurrence after "now" instead of replaying every
  missed period, and nothing is enqueued solely because the process started;
- `internal/api/handler/fleet_summary.go` and
  `internal/api/routes_operations.go` expose the admin-only
  `/api/v1/alerts/fleet-summaries` list/create/update/delete and
  `POST /{id}/test-run` routes. `cmd/server` registers the `fleet_summaries`
  worker plus its readiness check, and `internal/backup/restore_reconcile.go`
  runs the restore reconciler;
- `fleet.status_summary` was added to the notification event allowlist, the
  delivery-history filter, the webhook template data keys
  (`fleet.schedule_name`, `fleet.period_key`, `fleet.cutoff`,
  `fleet.online_count`, `fleet.offline_count`, `fleet.total_count`,
  `fleet.online_names`, `fleet.offline_names`, `fleet.group_names`) and the
  channel subscription UI in both English and Simplified Chinese.

Deviations from the reviewed design, recorded for review:

- one outbox message per schedule/period carries the frozen scoped channel set
  instead of one message per channel, because the shared queue batch limit is
  two messages and a per-channel fan-out would need one transaction per channel;
  the message still yields at most one delivery per channel and keeps the
  schedule/period key deduplication;
- `weekday` follows the already shipped frontend contract `0 = Sunday .. 6 =
  Saturday` (JavaScript `getDay`) rather than ISO 1-7. The UI already renders
  localized weekday names, so no raw number reaches the notification payload;
- run rows record the enqueue outcome (`queued`) and the plan row exposes the
  live delivery outcome from the durable outbox receipt at read time instead of
  rewriting the run row after provider retries.

Sandbox evidence: `go build ./...`, `go vet ./internal/... ./cmd/...`,
`goimports-reviser` and `golangci-lint` (0 issues) are clean; new store,
service and handler tests pass, including the single-claim cursor advance, a
replayed claim that writes no second run, the test-run insert that leaves the
cursor untouched, restore reconciliation, DST gap/repeat resolution, ISO week
and cross-year period keys, scope selection with private nodes, and the bounded
name-list thresholds. Frontend gates are green: `tsc -b`, 168 files / 867 cases,
`oxlint` (0 errors) and `vite build`.

Remaining: the host-only race/coverage/browser and rollback drills, plus the
deployed multi-provider delivery drill.
