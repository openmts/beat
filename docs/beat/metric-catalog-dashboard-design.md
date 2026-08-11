# Metric catalog and dashboard composition

Updated: 2026-07-30

Status: reviewed design; implementation requires explicit approval

## Goal and product boundary

Beat will replace its fixed metric/history presentation with a governed metric
catalog, per-metric retention policies, and a configurable node-detail chart
workspace. This directly fixes the current class of problems where changing a
time range does not materially change the x-axis and where y-axis values are
shown in raw, unfriendly units.

SQLite stores definitions, display policy, dashboard layouts, and retention
configuration. MTS remains the only store for Agent samples, derived numeric
series, network-probe samples, and all historical values. Beat will not add an
alternate SQL metric DSN.

## Competitor evidence and intentional rejection

Current Komari registers named metric definitions with type, unit,
description, and per-metric retention. Its node chart workspace supports
custom ranges, aggregation choices, user-added/removed/reordered charts,
multiple metrics per chart, chart sizes, tagged GPU series, and a global
template.

Reviewed sources:

- metric definitions and retention:
  <https://github.com/komari-monitor/komari/blob/4077201f098774511eaf504f220c5f6be009346b/internal/metricstore/definitions.go>
- generic metric definition and tags:
  <https://github.com/komari-monitor/komari/blob/4077201f098774511eaf504f220c5f6be009346b/pkg/metric/types.go>
- metric administration in stable Web `1.3.2`:
  <https://github.com/komari-monitor/komari-web/blob/14f4067e9b69813b24a0255e565f7f49bff0a1bd/src/pages/admin/settings/metrics.tsx>
- chart composition and custom ranges in stable Web `1.3.2`:
  <https://github.com/komari-monitor/komari-web/blob/14f4067e9b69813b24a0255e565f7f49bff0a1bd/src/pages/instance/LoadChart.tsx>

The Web evidence is fixed to the stable release because the current Web `main`
was force-rewritten to an older unrelated history after the original review.

Komari also permits alternate SQL metric backends. Beat explicitly rejects
that capability because the product contract requires every time-series value
to reside in MTS.

## Catalog invariants

1. Every writable measurement is registered before ingestion. Unknown metric
   names are rejected rather than auto-created.
2. A definition has a stable machine key, human label, description, semantic
   type, base unit, value domain, allowed aggregations, public visibility,
   default retention, and bounded tag schema.
3. Metric names, tag keys, tag values, display labels, and units have strict
   length and count limits. Arbitrary Agent input cannot expand catalog or MTS
   cardinality.
4. Built-in metric keys are immutable. Administrators may change retention,
   visibility, label overrides, and chart defaults, not ingestion semantics.
5. Retention zero disables future persistence only after an explicit destructive
   confirmation. Existing data deletion follows the catalog retention job;
   explicit node/task/fleet/all-history erasure is the separate reviewed `v17`
   contract in [`metric-erasure-design.md`](./metric-erasure-design.md).
6. The query layer validates requested metrics, tags, range, aggregation,
   resolution, point count, node visibility, and compatible units before
   constructing MTS queries.
7. MTS failures return a storage error and observable degraded state; they are
   never represented as an empty successful chart.
8. Public dashboard configuration cannot expose a private metric or hidden
   node. Administrator-only metrics remain absent from public catalog and data
   responses.

## Metric definition model

This foundation uses canonical SQLite migration `v8` after GeoIP/lifecycle
`v7` and introduces backup archive format `v4`, whose logical MTS export is
governed by the persisted catalog. The assignments are maintained in
[`commercial-parity-implementation-sequence.md`](./commercial-parity-implementation-sequence.md).

SQLite adds `metric_definitions` with:

- immutable `metric_key` and source (`builtin`, `derived`, or approved plugin
  namespace in a future revision);
- type (`gauge` or `counter` in the first revision);
- base unit (`percent`, `bytes`, `bytes_per_second`, `seconds`, `celsius`,
  `watts`, `count`, `cores`, or `number`);
- default aggregation and a bounded allowed-aggregation set;
- minimum/maximum domain and whether negative values are valid;
- public visibility, enabled state, retention days, and display order;
- localized label/description overrides in strict JSON;
- bounded tag schema defining keys, display labels, and maximum values;
- revision, created/updated actor, and timestamps.

Definitions are seeded from code with an immutable catalog version. Startup
compares seed and persisted revisions, adds new built-ins transactionally, and
fails readiness on an incompatible semantic change. It never silently changes
a unit or counter/gauge type for existing MTS data.

The initial catalog covers all current `NodeMetrics`, traffic deltas and billing
aggregates, network probe metrics, and the reviewed GPU batch. Current values
remain derived from the latest MTS points; SQLite has no `latest_value` column.

## Retention and maintenance

Each metric has a retention period from 1 through 3650 days. A system-wide
default applies only to definitions that have no explicit override. Changes
create a policy revision and a durable cleanup job; they do not run a long
delete inside the HTTP request.

Cleanup groups definitions by cutoff, deletes only registered MTS
measurements/tags, compacts MTS after successful deletion, and records cursor,
rows/segments affected, bytes reclaimed, and error state in SQLite. Retention
jobs are restart-safe and mutually exclusive with backup logical export and
restore. SQLite platform rows are never deleted by metric retention.

The `v17` erasure worker reuses the catalog inventory and the same exclusive
MTS operation lock, but it has fixed owner-confirmed scopes and lifecycle
semantics. Retention policy changes never impersonate an explicit erasure job.

Increasing retention affects future availability but cannot recreate deleted
points. The UI states the earliest currently available timestamp returned by
MTS rather than implying the configured policy is fully populated.

## Query contract

The public/admin history endpoint accepts:

- node ID;
- one to eight registered metric series;
- explicit `from` and `to` UTC timestamps;
- optional bounded tag filters;
- requested aggregation and maximum point count.

The Server selects a deterministic bucket interval so the inclusive x-axis
contains no more than the configured maximum points. The response includes the
effective range, bucket interval, per-series base unit, aggregation, tags,
available-data start, and points. Missing buckets are `null`, not zero.

The query service first admits the operation against the MTS wrapper lifecycle.
A closed, closing, unhealthy, canceled, or engine-failed query returns a typed
storage error and degraded metadata; only a healthy query with no matching rows
returns an empty series. REST, the shared realtime producer, alerts, and reports
reuse this distinction instead of inspecting empty maps independently.

Preset ranges are relative to one captured Server time. Custom ranges must have
`from < to`, remain within retention, and respect maximum duration and query
cost. Counter rates handle reset explicitly. Used and total capacity series may
share a bytes axis; percent, bytes, rates, temperature, and counts may not share
one y-axis.

Caching keys include authorization visibility, node, metric keys, tag filters,
effective range, interval, aggregation, and catalog revision. Live updates
append only when the chart remains in a real-time range; historical custom
ranges do not drift forward in the background.

HTTP admission, canonical client identity, global/per-client MTS query
concurrency, request-target and encoded-response budgets compose with this
contract through
[`ingress-resource-governance-design.md`](./ingress-resource-governance-design.md).
Catalog policy may make a query invalid or more expensive, but cannot bypass
the ingress ceiling.

## Dashboard configuration

SQLite stores:

- one owner-managed public default template;
- one administrator default template;
- optional per-administrator preferences;
- optional per-node template override.

A dashboard contains ordered chart definitions with stable ID, title, size,
series list, aggregation, legend state, and unit-group policy. Server validation
rejects unknown/private metrics, incompatible units, duplicate series, excessive
charts, excessive series, or unsafe text. Templates are versioned and retain a
bounded previous revision for rollback.

The public template is read-only to anonymous users. Browser-only personal
changes may be held in memory for the visit, but public local storage is not an
authoritative configuration source. Administrator preferences are persisted
through authenticated APIs and audited.

## Frontend experience

The node detail remains a quiet operational surface. A compact toolbar uses:

- shadcn `ToggleGroup` for preset ranges;
- `Popover` plus date/time fields for custom range;
- `Select` with `{ value, label }` items for aggregation and metric choices;
- icon buttons with tooltips for add, reorder, resize, reset, and save;
- `Chart`, `Card`, `Badge`, `Skeleton`, `Empty`, and `Alert` for data states.

Selecting a range immediately updates query parameters and the visible x-axis
domain. The toolbar shows the effective absolute range and bucket interval.
While refetching, existing charts remain visible with a subtle pending state;
the page never reloads and layout dimensions remain stable.

The unit formatter is catalog-driven:

- bytes use B through PiB;
- rates use B/s through PiB/s;
- duration uses ms, s, min, h, and d according to magnitude;
- counts and cores use locale-aware decimals;
- temperature uses degrees Celsius;
- percent is clamped for display only when the definition says 0-100.

Ticks choose one consistent scale per axis and tooltips retain useful precision.
The response base unit remains unchanged, so formatting cannot corrupt data.
Long metric and tag labels wrap or truncate with a tooltip and never resize the
chart grid.

Desktop uses a stable 12-column grid with small, medium, and large chart spans.
Mobile renders one chart per row and moves editing actions into a menu. Charts
are individual cards, never cards nested inside cards. Drag-and-drop has
keyboard move controls and reduced-motion behavior.

## API and authorization

Public endpoints expose the public catalog, public template, and data for
visible nodes/metrics without login. Administrator endpoints manage catalog
policy and templates. Retention deletion, global template publication, and
public visibility changes require recent reauthentication; ordinary personal
layout changes do not.

Every response uses human-readable labels for display while IDs/keys remain
machine values. Frontend regression tests cover every catalog and template
select to prevent raw IDs from appearing after selection.

## Observability, backup, and rollback

Prometheus metrics cover query duration, scanned points, returned points,
cache outcomes, rejected query cost, catalog drift, retention jobs, and MTS
errors. Metric names and node IDs are not Prometheus labels.

Backup includes the SQLite catalog/templates/jobs and the registered MTS
measurements. Restore validates catalog version before importing MTS and fails
closed on unknown metric keys, units, or tags. Dashboard templates referencing
unavailable metrics are restored but quarantined until repaired; they never
break public rendering.

Rollback restores the previous Server/frontend and matching SQLite backup.
MTS data remains compatible because this batch adds governed measurements and
query metadata rather than moving time-series values to SQLite.

## Test and acceptance gates

Backend tests cover seed migration, immutable semantics, tag bounds, retention
jobs, query-cost limits, counter reset, bucketing, null gaps, closed/closing and
injected MTS errors versus genuine no-data results, visibility, incompatible
units, template revisions, concurrency, backup/restore, and fault injection.

Frontend tests cover range-to-query mapping, x-axis domain changes, dynamic
units, custom ranges, live/historical behavior, add/remove/reorder/resize,
keyboard operation, raw-ID prevention, responsive layout, stale data,
cancellation, and loading/error/empty states.

Acceptance requires proving that every Agent and derived sample remains MTS-only,
all existing ranges return correct absolute domains, y-axis units are readable,
MTS storage failure is visible, coverage remains at least 90 percent, CI-aligned
tests/lint/build pass, backup/restore succeeds, and IPv4/IPv6 public/admin smoke
tests preserve the no-login public contract.

## Approval boundary

This foundation is canonical SQLite migration `v8` and backup format `v4` and
must be approved before GPU telemetry `v9`. Implementation requires explicit
approval of this entire design, including schema, API, retention,
public-template, logical MTS export, and backup changes.
