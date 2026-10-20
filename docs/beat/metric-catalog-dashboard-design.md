# Metric catalog and dashboard composition

Updated: 2026-09-13

Status: authorized implementation contract; acceptance remains open

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

The Grafana reference describes the built-in Beat experience: dashboards and
panels, time and refresh controls, a visual query builder, variables, thresholds,
inspection and export. No external Grafana platform or PromQL compatibility is
required. The existing self-hosted owner/admin model remains the deployment
assumption.

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
   explicit node/task/fleet/all-history erasure is the separate reviewed `v22`
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

This foundation uses the next canonical SQLite migration `v8` after deployed
agent enrollment `v7` and introduces backup archive format `v3`, whose logical MTS export is
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

The `v22` erasure worker reuses the catalog inventory and the same exclusive
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

## Dashboard collaboration (v25)

Migration `v25` completes the Grafana-style surfaces this document left open and
is registered in
[`commercial-parity-implementation-sequence.md`](./commercial-parity-implementation-sequence.md).

- **Annotations** overlay range-scoped notes on a dashboard or a single panel.
  Each row carries title, text, tags, a palette color, and an explicit
  `[from, to]` window. Owner or dashboard-owning administrators create, edit and
  delete them; a dashboard-scoped annotation is visible to every reader who can
  see that dashboard, while a personal annotation stays private to its creator.
  The workspace draws each in-range annotation on the panel time axis as a
  clipped colour band plus a start line, filtering by panel scope, and exposes the
  same markers as an accessible list; the creation dialog chooses between the
  whole dashboard and one panel.
- **Snapshots** freeze one dashboard definition under a random share token. A
  snapshot is read-only, keeps the original revision and dashboard id, may carry
  an expiry, and is revoked by the creator or an owner. The public route serves
  the frozen definition and never exposes live query credentials.
- **The panel library** stores reusable single-panel definitions per
  administrator so a panel can be saved once and inserted into any dashboard.
  Library rows are personal, bounded, validated with the same panel rules, and
  copied at insert time so later library edits never mutate saved dashboards.
- **Panel alert links** persist the panel-to-rule association the workspace
  previously carried only as a deep link. A panel binds at most four rules; the
  workspace renders the aggregated evaluation state (firing, pending, recovering,
  no-data, error or normal) as a badge and drills into the existing alert history.
  The workspace never writes rule rows and the alert module remains authoritative.

Limits are bounded per owner and per dashboard. Every mutation is revision-free
but audited under the `metrics.dashboard.*` action family, and all four tables are
covered by the `v4` backup snapshot, so no archive-format increment is required.

## Repeated panels (2026-10-15)

Panels accept an optional `repeat` field naming a declared `node` or `group`
variable. The workspace renders one instance per node in that variable's current
selection, binds the instance to that node instead of the dashboard-wide
selection, and labels the instance with the node alias or name. The instance
count is capped at 12 per panel with an explicit truncation note, instances share
the saved panel identity for edit/duplicate/reorder/alerts/library actions, and
the server rejects a `repeat` that does not reference a declared variable. This
is a schema-free addition inside the existing `v8` dashboard JSON and keeps
backup format `v4`.

## Multi-select variables and option filters (2026-10-15)

Variables accept an optional `multi` flag and an optional `regex` option filter.
`multi` turns the picker into a searchable checkbox list and expands a panel
series to the union of the selected nodes (or the visible nodes of the selected
groups); the eight-series panel budget still applies, so a wide selection reports
`workspace.series_limit` instead of querying more. `regex` filters the labels of
options the viewer may already query, uses RE2 semantics, and is bounded to 64
bytes by the server. Both fields are additive inside the existing `v8` dashboard
JSON: dashboards without them keep the single-selection behaviour, and backup
format stays `v4`.

## Dashboard duplication and panel library search (2026-10-15)

The workspace offers a Grafana-style dashboard copy that names the result before
writing it. "Duplicate dashboard" opens a dialog prefilled with the current title
plus a localised suffix, trims the submitted name, rejects an empty one and
truncates at the server's 120-character title limit; confirming creates a new
personal dashboard through the existing create endpoint and selects it. The
definition is copied, not shared, so later edits never touch the original, and
the previous "save a copy" button keeps its silent behaviour. No schema, API,
permission or backup-format change is involved.

The panel library gained an incremental search over saved panel names, panel
titles and visualisation types, with an explicit empty-result message. Filtering
is presentation-only: it never reorders rows, hides the save action or bypasses
the per-owner bounds enforced by the server.

Evidence: `duplicate.test.tsx` (prefilled suffix, trimming, empty-name block,
dialog close, title limit) and the panel-library case in
`collaboration.test.tsx` (matches by name and panel title, empty-result copy),
plus the menu assertion in `toolbar.test.tsx`.

## Panel time override (2026-10-15)

A panel can carry its own relative time window, the way Grafana's query options
do. `time_from` and `time_to` are optional Grafana-style tokens (`now` or
`now-<number><unit>` with `s`, `m`, `h` or `d`); an empty end means `now`, and
both empty means the panel follows the dashboard range. The server rejects an
end without a start and any resolved window outside one minute to 31 days, so a
panel override can never exceed the dashboard's own bounds. The editor shows the
resolved error inline, saving is blocked while the window is invalid, the panel
header shows the active tokens, and export/import carries the fields. This is a
schema-free addition inside the existing `v8` dashboard JSON, so backup format
stays `v4` and no migration number is consumed.

## Query budget and now delay (2026-10-15)

Grafana exposes the per-panel point budget and a dashboard-level now delay in its
query and time options; Beat now mirrors both. `max_points` (2..600, matching the
query API) can be set on the dashboard as the default and overridden per panel;
an absent value keeps the server cap, so existing dashboards are unchanged.
`now_delay_seconds` (0..3600) shifts the resolved `now` for relative ranges into
the past, so a dashboard can ignore samples that have not been persisted yet;
absolute ranges keep the requested window. The panel editor states the inherited
default, the definition editor reports an inline error and blocks saving while a
value is out of range, and export/import carries both fields. Server-side
`ValidateDashboard` is authoritative for both. This is a schema-free addition
inside the existing `v8` dashboard JSON; backup format stays `v4` and no
migration number is consumed.

## Panel time shift (2026-10-15)

Grafana's query options also offer a time shift for comparing with the previous
period. A panel can now declare `time_shift` as a duration (`1d`, `12h`, ...,
bounded to one second through 31 days). The resolved window, whether it comes
from the dashboard range or from the panel time override, moves back by the whole
duration, and the server rejects malformed or out-of-range durations through
`ValidateDashboard`. The editor reports the error inline, saving is blocked while
the duration is invalid, the panel header shows the active tokens, and
export/import carries the field. Annotation fetches widen their window by the
largest panel shift, so a shifted panel still shows its markers instead of
silently losing them. This is a schema-free addition inside the existing `v8`
dashboard JSON; backup format stays `v4`.

## Dashboard search tags and picker search (2026-10-15)

Dashboards accept optional search tags the way Grafana's dashboard browser does.
`tags` is an additive field inside the existing `v8` dashboard JSON: at most
eight unique lowercase keys of 64 bytes each (`a-z`, `0-9`, `-`, `_`, starting
with a letter or digit), validated by `ValidateDashboard` and reused from the
annotation-tag convention. The definition editor normalises comma- or
whitespace-separated input to lowercase, reports an inline error and blocks
saving while a tag is malformed, duplicated or beyond the budget; the toolbar
gains a search box that filters the dashboard list by title and tags, keeping
the current selection visible so the picker never goes blank. Export/import
carries the field and treats malformed or unbounded tags as an invalid file, and
tags never influence queries or permissions. Backup format stays `v4` and no
migration number is consumed.

Evidence: `internal/model/dashboard_tags_test.go` (accepted labels, nine rejection
cases), `workspace-dashboard-tags.test.ts` (parsing, server-equivalent bounds,
title/tag matching), the tag cases in `workspace-portable.test.ts`, the tags case
in `editors.test.tsx` and the picker case in `toolbar.test.tsx`.

## Time series line style (2026-10-15)

Time series panels accept Grafana's line style options: `line_width` (1-4, default
2, matching the previous fixed width), `fill_opacity` (0-100 percent, default 0),
`show_points` (`auto` keeps the existing 1.8px markers, `always` enlarges them,
`never` removes them) and `stack` (`none` or `normal`). Filling or stacking
switches that series to a filled area, so `fill_opacity: 0` plus `stack: none`
renders exactly the previous line. The editor shows the fields for time series
panels only, clears them when the panel becomes a `stat`, `gauge` or `table`
panel, and raises the fill to a readable default when stacking is switched on. All
four fields are additive inside the existing `v8` dashboard JSON, are validated by
`ValidateDashboard` (including the timeseries-only rule), survive export/import and
keep backup format `v4`.

Evidence: `internal/model/dashboard_line_style_test.go` (accepted boundaries and
seven rejection cases), `workspace-line-style.test.ts` (resolved defaults,
server-equivalent bounds, type patch, stacking fill), the line-style cases in
`workspace-portable.test.ts` and `workspace-state.test.ts`, the panel editor case
in `editors.test.tsx` and the chart case in `chart-style.test.tsx`.

## Value mappings (2026-10-15)

`stat`, `gauge` and `table` panels accept up to eight Grafana-style value
mappings. Each mapping matches an exact `value`, a `range` with open or closed
bounds, or `special: null` for empty samples, and supplies display `text` and/or a
palette `color`; a mapping that sets neither is rejected because it would change
nothing. The first match wins and replaces the numeric text and the threshold
colour, so a panel can show "offline" instead of `0` in the mapped colour. Panels
without mappings render exactly as before, `timeseries` panels reject the field
(mirroring the timeseries-only line-style rule), the inspector keeps raw numbers
for data verification, and export/import carries the field. This is a schema-free
addition inside the existing `v8` dashboard JSON, so backup format stays `v4`.

Evidence: `internal/model/dashboard_value_mapping_test.go` (accepted shapes for all
three kinds and fourteen rejection cases), `workspace-value-mapping.test.ts`
(first-match, open bounds, empty samples, bounds and type rules, type switching),
the mapping cases in `workspace-portable.test.ts` and `editors.test.tsx`, and the
stat/table rendering case in `presentation.test.tsx`.

## Bar gauge panels (2026-10-15)

Beat's visualization list was `timeseries`, `stat`, `gauge` and `table`; Grafana's
bar gauge was missing, so a fleet comparison had to be read off a table or a
stat. The new `bargauge` type draws one bar per series, which makes it the first
non-`timeseries` panel that accepts the full one-to-eight series budget. `bar_orientation`
is `auto` (default), `horizontal` or `vertical`, with auto resolving to vertical
for a single series and horizontal for several; `bar_display` is `basic`
(default), `gradient` or `lcd`, and LCD lights fixed tenths of the filled range so
the same value lights the same number of cells at any panel size. `bar_unfilled`
(default true) keeps or drops the unfilled track. Scale, decimals, unit, thresholds
and value mappings behave exactly as they do on `stat`, including the mapped text
and colour taking precedence over the numeric value.

Server-side, `ValidateDashboard` is authoritative: unknown orientation or display
values are rejected, as is any bar-gauge field on another panel type, and the
`stat`/`gauge` single-series rule deliberately does not cover `bargauge`. The
panel editor shows the group only for bar gauge panels, the panel type switch
clears the fields, and the fields survive export/import (unknown values make the
file invalid). All of it is additive inside the existing `v8` dashboard JSON, so
no migration is spent and backup format stays `v4`.

Evidence: `internal/model/dashboard_bar_gauge_test.go` (accepted defaults and
boundaries, six rejection cases, auto orientation), `workspace-bar-gauge.test.ts`
(resolved defaults, server-equivalent enums, orientation resolution, fill and LCD
segments, type patch), `bar-gauge.test.tsx` (one meter per series, scale defaults,
LCD cells, unfilled track, multi-series layout and value mapping), the bar-gauge
cases in `workspace-portable.test.ts`, `editors.test.tsx` and
`presentation.test.tsx`.

## Pie chart panels (2026-10-15)

After time series, stat, gauge, table and bar gauge, Grafana's pie chart was the
next visualization Beat's workspace lacked, so a share-of-total question had to
be answered from raw numbers. The new `piechart` type draws one slice per series
and therefore accepts the same one-to-eight series budget as `bargauge`.

`pie_chart_type` is `pie` (default) or `donut`; `pie_label_mode` is `percent`
(default), `value` or `both`. Shares come from the positive latest values: zero,
negative and non-finite samples keep their legend entry but contribute no share,
and an all-zero panel renders an empty chart rather than dividing by zero. Slices
reuse the authored series colour, or the mapped colour when a value mapping
matches; mapped text replaces the numeric label in `value` and `both` modes. The
server rejects unknown enum values and any pie-chart field on another panel type,
and the panel type switch clears the fields. Everything stays additive inside the
existing `v8` dashboard JSON, so no migration is spent and backup format stays
`v4`.

Evidence: `internal/model/dashboard_pie_chart_test.go` (accepted defaults and
boundaries, six rejection cases, documented defaults for the console),
`workspace-pie-chart.test.ts` (resolved defaults, server-equivalent enums, positive
share arithmetic, start offsets, wedge and ring geometry, type patch),
`pie-chart.test.tsx` (one slice per series, percent/value/combined labels,
full-circle donut ring, dropped zero shares, value-mapping text and colour), the
pie cases in `workspace-portable.test.ts`, `editors.test.tsx`,
`presentation.test.tsx` and `workspace-line-style.test.ts`.

## Approval boundary

The dashboard foundation is canonical SQLite migration `v8` and backup format
`v3`, implemented before GPU telemetry `v14`. Dashboard annotations, snapshots,
the panel library and panel-to-rule associations are canonical SQLite migration
`v25` on the unchanged backup format `v4`. The current commercial delivery
request authorizes the categorical schema, API, retention, public-template,
logical MTS export and backup work plus the `v25` collaboration batch. Completion
still requires the verification gates above.
