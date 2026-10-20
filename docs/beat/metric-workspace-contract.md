# Built-in metric workspace contract

Updated: 2026-10-15. Status: implementation in progress, not deployed.

This contract implements the user's Grafana-style experience inside Beat, using
the existing self-hosted owner/admin model. Numeric history stays exclusively
in MTS. The version registry assigns SQLite `v8` and backup format `v3`; the collaboration batch is SQLite `v25` on the unchanged format `v4`.

## Catalog and queries

- `GET /api/v1/metrics/catalog`: public enabled definitions.
- `GET /api/v1/admin/metrics/catalog`: all definitions for administrators.
- `PUT /api/v1/admin/metrics/catalog/{key}`: owner/recent-auth policy update,
  revision checked and audited. Semantics and machine keys remain immutable.
- `POST /api/v1/metrics/query`: bounded read-only public query.
- `POST /api/v1/admin/metrics/query`: the equivalent administrator query.

Use Beat's usual `{ "data": ... }` response envelope. Catalog data contains
`version`, `revision`, and `metrics`. Each metric has `key`, `label`,
`description`, `source` (`builtin`, `derived`, `custom`), `kind` (`gauge`,
`counter`), `unit`, `aggregations`, `default_aggregation`, `public`, `enabled`,
`retention_days`, and `revision`. Built-ins cover the existing NodeMetrics;
derived traffic and network data retain their typed inventory. Custom metric
definitions retain their inventory after unbinding/deletion so historical rows
remain governed and included in backup and retention. Public definitions never
contain collection commands or node credentials.

Example query data:

```json
{
  "from": "2026-09-13T00:00:00Z",
  "to": "2026-09-13T01:00:00Z",
  "max_points": 600,
  "series": [
    { "ref_id": "A", "node_id": "node-uuid", "metric": "cpu", "aggregation": "avg" }
  ]
}
```

Limits: one to eight series per request, unique bounded `ref_id`, 2–600 points
per series, range greater than zero and at most 31 days, five-second query
deadline, eight concurrent server queries and two per client. Invalid input,
unsupported aggregation, unknown/private metrics and invisible nodes must be
rejected before scanning MTS. One request is evaluated at one fixed range.
Capacity rejection returns `429` with `Retry-After`; storage failure is `503`,
never a successful empty result. Caller cancellation stops work and frees the
admission slot.

Ranges use millisecond precision and the half-open interval `[from, to)`;
buckets are anchored to the requested `from`. Each MTS scan is bounded to one
million samples, 4096 shards and 32768 parts. The legacy node history endpoint
keeps its raw-point response shape but shares admission and the five-second /
31-day limits, with at most 64 requested fields and 100,000 total raw points.
It rejects excess cost instead of silently truncating points.

Response data contains `from`, `to`, `interval_ms`, and `series`. Each series
contains `ref_id`, `node_id`, `metric`, `label`, `unit`, `aggregation`,
`available_from` (nullable ISO timestamp), `state` (`ok` or `empty`) and `points`.
Each point is `{ "timestamp": <Unix milliseconds>, "value": <number or null> }`.
Use deterministic aligned buckets with no more than the requested point count;
every missing bucket is explicit `null`. `from`/`to` are the effective UTC chart
domain and must not silently drift during historical viewing. Values remain in
base units. Counter `rate` handles resets; its returned unit reflects the rate.
Other aggregations are `avg`, `min`, `max`, `sum`, and `last`, restricted by the
definition. The response must identify the actual aggregation used.

All persisted custom values must belong to enabled definitions currently bound
to that authenticated node. Validate the complete custom batch before heartbeat
or metric mutation; reject unknown, unbound, oversized and non-finite values.
Migration and startup reconcile existing custom definitions without admitting
arbitrary new measurement names from agent input.

Implementation details for v8:

- Logical custom keys retain the existing `custom_<name>` measurement name.
  Deleting or renaming a collection definition preserves its earlier catalog
  entry for historical queries, backup and retention. `network_probe` keeps its
  existing task-aware API and typed `task/node/type` inventory; it is not mixed
  into node-only scalar queries.
- Catalog `enabled` controls persistence and query availability independently of
  the custom collection switch. A policy update queues the new agent snapshot in
  the same transaction. Already cached values for a still-bound, known disabled
  definition are acknowledged and discarded, allowing system heartbeats to
  continue while the stop task arrives. Unknown or unbound values reject the
  complete report before heartbeat or metric mutation.
- Policy updates contain `revision`, `label`, `description`,
  `default_aggregation`, `public`, `enabled`, and `retention_days`. A null
  retention value inherits the global maintenance policy; catalog responses
  expose both effective `retention_days` and nullable `retention_override_days`.
  Labels are at most 128 bytes
  and descriptions at most 2048 bytes. Revisions use compare-and-swap updates.
- `metric_catalog_state` stores the catalog version and revision.
  `metric_definitions` stores semantic metadata, bounded aggregation/tag JSON,
  nullable value bounds and retention override, visibility, enabled state,
  display order, per-definition revision and actor/timestamp metadata.
  `metric_retention_jobs` stores one coalesced, restartable work item per metric
  with revision, queued/running/complete state, attempt scheduling and outcome.
  No numeric monitoring samples are stored in these tables.
- Policy transactions recheck the current user's owner/enabled state and the
  active session's local reauthentication deadline before writing. Revocation
  or demotion after the HTTP guard cannot commit a policy change. Source traffic
  counters cannot be disabled while their derived delta remains enabled.
- Explicit retention work runs independently of the global automatic-cleanup
  setting, with a 30-second attempt budget and a one-minute retry delay. Queue
  revision checks preserve newer policy work; interrupted claims are replayed.
  MTS cleanup and logical export share an exclusive operation lock, while query
  cancellation can stop waiting for that lock. Backup v3 uses the catalog in its
  SQLite snapshot and retains typed network-probe history and retired custom
  names; recovered pre-catalog custom names start private.

## Dashboard persistence and access

SQLite `metric_dashboards` contains platform metadata only:

- `id` primary key;
- `scope`: `personal`, `public`, `admin` or `node`;
- nullable `owner_user_id` for personal dashboards, referencing `admin_users`
  with cascading deletion;
- nullable `node_id` for node templates, referencing `nodes` with cascading
  deletion;
- `revision` (positive), `definition_json`, nullable `previous_definition_json`;
- `created_at`, `updated_at`, `updated_by` (actor ID, no secret material).

Only personal scope has an owner ID and only node scope has a node ID. Default
templates use stable IDs `public-default` and `admin-default`; a missing saved
default falls back to a valid built-in template. Personal dashboards use UUIDs.
Node template IDs are their node UUID prefixed with `node-`. Root owns the
dashboard model/store/handlers; the catalog migration creates this table.

- `GET /api/v1/metrics/dashboards/default?node_id=...`: visible public template.
- `GET /api/v1/admin/metrics/dashboards`: administrator defaults and the current
  user's saved dashboards.
- `POST /api/v1/admin/metrics/dashboards`: create a personal dashboard.
- `PUT /api/v1/admin/metrics/dashboards/{id}`: compare-and-swap revision update.
- `POST /api/v1/admin/metrics/dashboards/{id}/rollback`: restore previous
  definition with a new revision.
- `DELETE /api/v1/admin/metrics/dashboards/{id}`: delete a personal dashboard.

SQLite `dashboard_versions` (schema v27) stores the version history:

- primary key `(dashboard_id, revision)`;
- `title` and `summary` (at most 200 bytes each) for the list view;
- `definition_json` with the same 64 KiB bound as `metric_dashboards`;
- `created_at` and `created_by` (actor ID; the API resolves it to a username).

Every dashboard write records a version inside the same transaction, and each
dashboard keeps at most `MaxDashboardVersions = 20` versions (oldest pruned
first). The v27 migration backfills the current definition as the current
revision and `previous_definition_json` as the prior revision; earlier history
has no source and is not invented.

- `GET /api/v1/admin/metrics/dashboards/{id}/versions`: version metadata,
  newest first, definitions omitted.
- `GET /api/v1/admin/metrics/dashboards/{id}/versions/{revision}`: one full
  version definition.
- `POST /api/v1/admin/metrics/dashboards/{id}/versions/{revision}/restore`:
  body `{revision, summary}` where `revision` is the caller's current revision
  (compare-and-swap). Restoring writes a new revision, so history is never
  overwritten and the restore itself can be restored.

Version reads use the write authority model (personal dashboards are owner-only,
shared dashboards require the owner role) without demanding recent auth;
restores keep the recent-auth requirement of publication.

Public and node template publication requires owner/recent-auth and rejects
private series and hidden nodes. Personal writes require ownership. Responses
and mutations are audited; conflicts return `409` rather than overwriting edits.
Ordinary public visitors can view without login. They do not persist server
templates. Dashboard JSON has a 64 KiB bound and strict field/shape validation.

Definition schema version 1:

```json
{
  "version": 1,
  "title": "Node overview",
  "description": "",
  "time_range_seconds": 3600,
  "refresh_seconds": 15,
  "max_points": 600,
  "now_delay_seconds": 0,
  "tags": ["prod", "hk-1"],
  "variables": [{ "name": "node", "type": "node", "label": "Node", "multi": true, "regex": "^hk-[0-9]+$" }],
  "panels": [{
    "id": "cpu",
    "title": "CPU usage",
    "type": "timeseries",
    "size": "medium",
    "repeat": "node",
    "time_from": "now-6h",
    "time_to": "now",
    "time_shift": "1d",
    "line_width": 3,
    "fill_opacity": 20,
    "gradient_mode": "hue",
    "line_interpolation": "smooth",
    "line_pattern": "dashed",
    "connect_nulls": "always",
    "show_points": "auto",
    "stack": "normal",
    "soft_min": -10,
    "soft_max": 90,
    "max_points": 300,
    "legend": true,
    "thresholds": [{ "value": 80, "color": "amber" }],
    "value_mappings": [{ "type": "value", "value": 0, "text": "offline", "color": "slate" }],
    "series": [{
      "ref_id": "A", "node_id": "$node", "metric": "cpu",
      "aggregation": "avg", "label": "CPU", "color": "blue"
    }]
  }]
}
```

At most 24 panels, eight series per panel, four variables and four thresholds.
Supported panel types are `timeseries`, `barchart`, `stat`, `gauge`, `bargauge`, `piechart`,
`table`, `heatmap`, `histogram`, `statetimeline`, `geomap`, `text` and `row`. Sizes are
`small`, `medium`, `large` (4, 6, 12 desktop grid columns; one column on mobile).

A `table` panel also accepts `table_orientation`: `long` (the behaviour when the
field is absent) is the historical three-column table — one row per sample, with
timestamp, series and value columns; `wide` is Grafana's default table layout —
one column per series and one row per timestamp, with an empty cell wherever a
series has no sample at that timestamp. `wide` sorts by the time axis or by any
series column (gaps always last), keeps the same 100-row cap and CSV export, and
runs every cell through the same value mapping and per-series threshold path as
the long layout. The field is at most a display switch: it never changes the
query, the transformations or the sampled values. Unknown values, and the field
on any panel that is not a `table` panel, are rejected by the server and by the
JSON importer; the editor clears it when the panel type changes, keeps `long`
out of the saved payload because it is the default, and the field survives
export/import.
Every panel may carry an optional `description` (Grafana's Panel description): up to 512 bytes of valid
UTF-8 without NUL. It is shown behind the info icon in the panel header in the admin workspace and in the
read-only views, never in the query, and the dashboard bundle rejects an over-long, invalid or non-string
value as an invalid file.

`tags` are optional search labels: at most eight lowercase keys of 64 bytes
each (`a-z`, `0-9`, `-`, `_`, starting with a letter or digit), unique within the
dashboard, and never part of the query. The workspace filters its dashboard list
by title and tags; tags carry no permission or query meaning.
Colors use a fixed accessible palette. A variable selects one option by default;
`multi` lets the viewer pick several, and every selection still expands only
within the nodes and groups the viewer may query. A node variable expands to the
selected nodes, a group variable to the visible nodes of the selected groups,
and expansion never bypasses the query budgets: a panel that exceeds eight
expanded series reports `workspace.series_limit` instead of querying more.
`regex` filters the option labels before they are offered, uses RE2 semantics,
is limited to 64 bytes, and is validated by the server. It only narrows an
already authorized option list, so it can never reveal a node or group the
viewer could not query, and an empty or unusable filter means "no filtering"
rather than an error.

`stat`, `gauge`, `bargauge`, `piechart` and `table` panels accept up to eight
Grafana-style value mappings. A mapping is `value` (exact number), `range` (open or closed numeric
bounds) or `special` (empty samples), and carries display `text` (<=32 bytes) and
a palette `color`; at least one of the two must be set, otherwise the mapping
would change nothing and is rejected. The first match wins, and a match replaces
the numeric text and threshold colour. Mappings on a `timeseries` or `barchart` panel are
rejected, and the fields survive export/import.

`stat` panels accept Grafana's Stat styles: `stat_color_mode` (`value` by default,
or `background`/`none`) and `stat_text_mode` (`auto` by default, plus `value`,
`name` and `none`).
`value` paints the value text with the threshold colour, `background` fills the
panel with that colour and picks a readable foreground (`#f8fafc` or `#0f172a`)
from its relative luminance, and `none` leaves both unstyled so the sparkline
falls back to the current text colour. Text mode picks the main text: `auto` keeps
the value plus the series label and the last-sample caption, `value` drops the two
captions, `name` promotes the series label to the main text, and `none` hides the
text entirely. `stat_text_size` (`auto` by default, plus `sm`, `md`, `lg`, `xl`)
overrides the main text size; `auto` and `lg` keep the original `text-4xl`
(36px), `sm`/`md`/`xl` are 24px/30px/48px.
`stat_orientation` (`auto` by default, plus `horizontal` and `vertical`) arranges
the series label and the main value: `auto` keeps the previous order (value first,
label underneath), `vertical` puts the label above the value like Grafana, and
`horizontal` places both on one baseline row.
The panel title stays in every mode.
All four fields are rejected on any panel type other than `stat`, unknown values
are rejected, and the fields survive export/import.

Time series panels accept Grafana's line style fields: `line_width` (1-4, default
2), `fill_opacity` (0-100 percent, default 0), `gradient_mode` (`none` by default,
or `opacity`/`hue`), `line_interpolation` (`linear` by default, or `smooth`,
`step_before`, `step_after`), `line_pattern` (`solid` by default, or `dashed`/`dotted`),
`connect_nulls` (`none` by default, or `always`),
`show_points` (`auto`, `always` or `never`) and `stack`
(`none`, `normal` or `percent`). `percent` is Grafana's percentage stacking: every
timestamp is normalised so the absolute values of its series sum to 100%, the y-axis
switches to a 0..100 percent scale, and the tooltip still reports the original values
and unit by dividing the plotted coordinate by the timestamp's total. Null samples stay
null and a timestamp whose total is zero keeps its original values instead of dividing by
zero. Percent stacking is rejected together with a logarithmic axis
(`percent stacking needs a linear axis`), because a log axis has no place for 0 and its
ticks stop being meaningful; the panel editor and the dashboard import apply the same
rule. `point_size` (an integer 1-12) sets the dot radius when
points are drawn; without it the renderer keeps its own radius (2.6 for `always`, 1.8 for `auto`).
 Interpolation only selects one of the four curves the renderer
already draws, so it never carries a custom path; the server rejects Grafana's
camelCase spelling and unknown names. `line_pattern` maps to a fixed dash array
(`dashed` = `6 4`, `dotted` = `1 3`) on both the line and the area stroke. Bar charts
share the fields that apply to bars — `fill_opacity` (default 100 percent),
`gradient_mode` and `stack` (`none`, `normal` or `percent`) — and reject `line_width`,
`show_points`, `line_interpolation`, `line_pattern` and `connect_nulls`, which a bar renderer would ignore.
`point_size` on any other panel type is rejected, as is a point size combined
with `show_points: never`, where the renderer would draw no dots to size.
`none` keeps the sampling gaps explicit, `always` draws the line across them (Grafana's never/always); the
Grafana-only `threshold` value is rejected rather than silently treated as one of the two. Any
line-style field on a `stat`, `gauge` or `table` panel is rejected, so a saved
panel can never carry options its renderer ignores. `gradient_mode` needs a
non-zero fill, otherwise there would be nothing to fade: `opacity` fades the fill
from the series colour to transparent, `hue` rotates the hue while keeping the
fill opacity, and the renderer writes the opacity into the gradient stops instead
of the fill layer. Stacking uses absolute values and keeps gaps explicit, and the
same fields survive export/import and the panel editor.

`barchart` panels render Grafana's Bar chart: every series becomes one set of bars
on the shared time axis, stacked per timestamp when `stack` is `normal`, with the
y-axis scaled to the summed bar height so a stack never overflows the plot. Like
`timeseries` they keep their raw samples, so they reject `calculation` and value
mappings, and they draw no threshold lines, threshold bands or annotation markers.
Dragging across the plot zooms the dashboard to the selected window, and the
legend, tooltip, `min`/`max` bounds, decimals and unit behave as they do on a line
panel. Switching the panel type in the editor clears the options the new type
cannot render.

`bar_width` (4-40 px, default 12) caps one bar and `bar_radius` (0-10 px, default 0)
rounds the bar top while the bar foot stays on the axis; both are bar-chart only, are
rejected on every other panel type, and survive export/import. Buckets where every
series is empty are dropped before the bars are laid out, because the point budget
always returns a fixed grid (a one-minute window still yields 600 buckets) and
reserving a category for each of them would squeeze every bar into a sub-pixel line.

`bar_alignment` mirrors Grafana's Bar alignment for bar-chart panels: `auto` (the
behaviour when the field is absent) is treated as `centered`, which keeps the bar
centred on its timestamp, `before` shifts the bar half a bar width to the left so
it ends on the timestamp, and `after` shifts it half a bar width to the right so it
starts there; `centered` may also be declared explicitly. Unknown values and every
other panel type are rejected, and the editor clears the field when the panel type
changes. `auto` is normalised back to "not declared" on export/import, because the
two are equivalent. Line series, `bar_width`, `bar_radius` and the row/text panel
guards are unchanged.

`centered_zero` mirrors Grafana's Axis → Centered zero: the axis opens symmetrically around zero, with the
half range taken from the largest absolute value among the data and any soft limit. It is accepted on
`timeseries` and `barchart` only and is rejected together with a fixed `min` or `max`, because a fixed limit
would decide the axis on its own; dashboards whose samples are all zero or missing keep their normal axis
instead of collapsing to `[0, 0]`.

`grid` and `axis_placement` mirror Grafana's Axis → Show grid lines and Placement: `grid: false` hides the
horizontal grid lines (an absent value keeps them) and `axis_placement` moves the vertical axis to `left`
(the default when absent) or `right`. Both are accepted on `timeseries` and `barchart` only, and the contract
rejects the options on any other panel type; an unknown placement is rejected as well. The fields survive
export/import, which normalises `grid: true` and `left` back to their absent defaults.

`axis_label` mirrors Grafana's Axis → Label: a short caption (40 bytes at most, valid UTF-8, no control
characters) drawn rotated beside the vertical axis; an absent or empty value draws nothing. It is accepted on
`timeseries` and `barchart` only, other panel types are rejected, and the value survives export/import and the
panel editor.

`scale` mirrors Grafana's Axis → Scale: an absent value and `linear` mean the usual linear axis,
while `log` selects a base-10 logarithmic axis. It is accepted on `timeseries` and `barchart` only, unknown
values are rejected, and a logarithmic axis rejects a `min` or `soft_min` of zero or below. Zero and negative
samples are treated as missing on a logarithmic axis — they have no position on it — and the percentage
default of 0..100 is replaced by an automatic positive range, so the axis never starts at zero.

`soft_min` and `soft_max` mirror Grafana's Axis → Soft min / Soft max: the axis starts at the
soft value and only opens further when the data leaves that range, so a soft maximum below the
data peak never clips a sample. They are accepted on `timeseries` and `barchart` only, must be
finite numbers, and `soft_min` must stay below `soft_max` when both are set. A side that already
carries a fixed `min`/`max` rejects the soft limit on that side, because the fixed value would
always win there. Fixed limits keep priority, the percentage default of 0..100 is unchanged when
neither is set, and the fields survive export/import and the panel editor, which clears them
when the panel type changes.

Table panels render one row per sample and drop timestamps where every series is
empty, because the point budget always returns a fixed grid and the row cap would
otherwise be consumed by empty buckets; a null cell at a timestamp another series
sampled is kept, and the inspector and Explore tables still show the raw grid.
The panel's CSV export shares those rows.

`threshold_mode` selects Grafana's threshold display: `line` (the behaviour when
the field is absent) draws a dashed line per threshold, `area` fills the band
between neighbouring thresholds up to the top of the plot, `background` tints the
plot with the colour of the highest threshold the visible series reach, and `off`
keeps the values while dropping both the lines and threshold colouring, so `stat`,
`gauge` and `bargauge` fall back to the series colour. Only time series panels can
draw lines, bands or a background; every other type accepts `off` alone. Any other
combination is rejected instead of saving a panel whose renderer would ignore the
mode, and switching the panel type in the editor clears a mode the new type cannot
render. The field survives export/import, where `line` is normalised back to "not
declared" because the two are equivalent.

`gauge_threshold_markers` and `gauge_threshold_labels` mirror Grafana Gauge's show
threshold markers / show threshold labels: the former (default true) draws a tick on
the gauge bar at each threshold position using the threshold colour, and the latter
(default false) prints the threshold value under its tick. Only gauge panels accept
the two fields, and labels additionally require the markers switch to stay on
(`gauge threshold labels need threshold markers`) and at least one threshold
(`gauge threshold labels need thresholds`), so a panel that could never draw a label
is rejected. Thresholds outside the panel's `min`..`max` scale are skipped instead of
overflowing the bar. Both fields survive export/import and the editor, which clears
them when the panel type changes.

`calculation` implements Grafana's Value options → Calculation for the panels that
show one number per series: `last` (the behaviour when the field is absent), `first`,
`min`, `max`, `mean`, `sum`, `count`, `range` (max − min), `delta` (last − first) and
`percent_change` ((last − first) / |first| × 100). The window-derived reducers need two
finite samples, and `percent_change` also needs a non-zero base; without that basis the
panel shows no data instead of a fabricated `0`. The reduction runs after the transformation chain
and before value mappings and thresholds, so a mapped or thresholded colour always
describes the same number the panel prints. A series without a finite sample stays
empty for every reducer: `sum` is never rendered as `0`, and `count` is only shown
for series that actually have samples, so "no data" stays distinguishable from a
real zero. `timeseries`, `barchart` and `table` keep the raw samples and reject the field, and
switching the panel type in the editor clears a reducer the new type cannot render.
The field survives export/import, where `last` is normalised back to "not declared"
because the two are equivalent.
The same one-pass statistics back the legend: `legend_values` now offers `last`,
`min`, `max`, `mean`, `sum` and `count`, and a panel may display at most four of
them at once (`MaxDashboardLegendValues`). An empty series shows no statistic rather
than zero, so a legend never turns "no data" into a real value. The same six
statistics are also the `sort_by` transform's ordering keys.

`legend_placement` is `bottom` (the behaviour when the field is absent) or
`right`, and only time series and bar chart panels accept it while the legend is
on. `right` turns the panel into a horizontal layout: the plot shrinks, the
legend keeps a fixed 40-unit column and stacks its entries vertically, and the
table-style legend still scrolls horizontally inside that column. Unknown
placements (including Grafana's `top`), a hidden legend and every other panel
type are rejected, and the field survives export/import.

`legend_width` mirrors Grafana's Legend → Width for a right-hand legend: an
integer pixel width between 120 and 480. The field is only accepted when the
legend is visible, the panel is a time series or bar chart, and
`legend_placement` is `right`; otherwise the save is rejected rather than storing
a width no renderer would apply. Leaving it unset keeps each renderer's built-in
column width (160px for the list legend, 224px for the table legend), so existing
dashboards are unchanged. The editor shows the input only for a right-hand
legend, clears it when the legend is hidden or moved back to the bottom, and the
field survives export/import.

Bar gauge panels render Grafana's bar gauge: one bar per series, so unlike `stat`
and `gauge` they accept one to eight series. `bar_orientation` is `auto`
(default), `horizontal` or `vertical`, where auto resolves to vertical for a
single series and horizontal for several; `bar_display` is `basic` (default),
`gradient` or `lcd`, with LCD lighting fixed tenths of the filled range so the
same value lights the same number of cells at any panel size. `bar_unfilled`
(default true) keeps or drops the unfilled track, `min`/`max` bound the scale
following the same defaults as `stat`, and `decimals`, `unit`, thresholds and
value mappings apply as they do on `stat`. Any set bar-gauge field on another
panel type is rejected, and switching the panel type in the editor clears them.

Pie chart panels render Grafana's pie chart: one slice per series, so like
`bargauge` they accept one to eight series. `pie_chart_type` is `pie` (default) or
`donut`, and `pie_label_mode` is `percent` (default), `value` or `both`. Shares are
computed from the positive latest values only: zero, negative and non-finite
samples keep their legend entry but add no share, so one bad reading cannot skew
the remaining slices, and an all-zero panel draws an empty chart instead of
dividing by zero. Slices use each series' authored colour, or the mapped colour
when a value mapping matches; a mapped `text` replaces the numeric label in the
`value` and `both` modes. Any set pie-chart field on another panel type is
rejected, and switching the panel type in the editor clears them.

`repeat` optionally names a declared `node` or `group` variable. A repeated panel
renders one instance per node in that variable's current selection, binds
`$variable` inside the instance to exactly that node, and substitutes the node
label into the title (appending it when the title has no `$variable` token). At
most 12 instances render per panel and the workspace states how many were
omitted. Instances share the base panel identity, so editing, reordering,
duplicating, panel alerts and the panel library always target the saved panel.
Repeat, like every other field, is preserved by export/import and rejected when
the variable is undefined.

`time_shift` is an optional Grafana-style duration (`1d`, `12h`, ... between one
second and 31 days) that moves the resolved window back as a whole, so a panel
can compare against the previous period. It composes after the panel time
override, so `now-6h` plus `1d` queries the six hours ending a day ago, and it is
preserved by export/import.

`max_points` is the per-series point budget and mirrors the query API bound of
2..600; the dashboard value is the default, a panel value overrides it, and an
absent value means the server cap. Requesting fewer points yields a coarser,
cheaper panel and never bypasses the series or range budgets. `now_delay_seconds`
(0..3600) shifts the resolved `now` into the past for relative ranges only, so a
dashboard can exclude samples that have not been persisted yet; absolute ranges
are untouched. Both fields are optional, validated by the server, preserved by
export/import, and live inside the existing `v8` dashboard JSON, so backup format
stays `v4`.

`time_from`/`time_to` are an optional Grafana-style relative panel time override,
written as `now` or `now-<number><unit>` with `s`, `m`, `h` or `d`. Both empty
means the panel follows the dashboard range; an end without a start is rejected,
and the resolved window must stay between one minute and 31 days. The override is
preserved by export/import and never widens the server-side range, series or
point budgets. A panel uses compatible
units only. A missing selection, removed metric or unavailable node yields an
actionable panel state, not an exception or a zero value.

## User experience and verification

Multi-select variables open a searchable checkbox picker and summarise the
current selection on the trigger, so a long node list stays usable. The metric
workspace provides saved dashboard selection, explicit dirty state,
save/conflict handling, custom and preset times, refresh pause, panel add/edit/
duplicate/remove/reorder/resize, keyboard movement, variables, thresholds and
readable axes. Historical ranges stay fixed; live refresh is canceled on route
change, range change or hidden document. Refresh failure retains last good data
with its timestamp and a visible retry action.

Each panel supports inspection of the effective query and returned data, CSV or
JSON download, and node drilldown. Export escapes spreadsheet formula prefixes.
An editable panel also offers a one-click path into alert-rule creation that is
prefilled with that panel's metric, aggregation, window, selected node or device,
threshold and title. The rule itself is still owned, validated and stored by the
alerting module. Migration `v25` adds the durable panel-to-rule association:
`dashboard_panel_alerts` binds a panel to at most four rules, and the workspace
renders the aggregated evaluation state as a badge that drills into the existing
alert history. The workspace reads and links rules but never writes them, so the
alerting module stays authoritative.
Dashboard annotations are drawn on the panel time axis: each annotation whose
window intersects the visible range renders as a clipped band with a start line,
dashboard-scoped rows appear on every panel and panel-scoped rows only on their
own panel, and the same markers are published as an accessible list. Creating an
annotation chooses between the whole dashboard and a single panel.
All labels are localized and select controls show labels rather than IDs.

Panels carry Grafana's Transform equivalent as an ordered `transformations` chain
of at most four steps. `rename_by_regex` rewrites series labels with an RE2
pattern (at most 128 bytes) and a printable replacement (at most 120 bytes);
`filter_by_value` keeps series whose latest finite sample satisfies `gt`, `gte`,
`lt`, `lte`, `eq` or `ne` against a finite threshold, and drops series without a
sample; its optional `column` (at most 64 printable bytes) turns the step into a
row filter on the result frame instead — see the frame rules below; `sort_by`
orders series by the `last`, `min`, `max` or `mean` statistic with `asc` or
`desc` and always leaves missing samples last, and its optional `field` (at most
64 printable bytes) names a result frame column directly, including group-by key
columns, in which case the statistic may be omitted; `limit` keeps the
first one to eight series of the current order, so "the largest three" is
`sort_by` followed by `limit`. `binary_operation` picks one series per side by
exact label match, or by the first label matching a `/re/` selector, aligns the
two by timestamp and applies `+`, `-`, `*` or `/`, appending the result as a new
series whose label is `alias` or `left <operator> right`; samples missing on
either side and divisions by zero are dropped instead of becoming NaN or
infinity, and a selector that matches nothing leaves the chain unchanged. Each
step accepts only its own fields, and the server rejects mixed payloads, unknown
kinds, non-compiling patterns or selectors, out-of-range counts and chains
longer than four. `reduce` takes the same
calculation set as the panel-level value calculation (`last`, `first`, `min`, `max`,
`mean`, `sum`, `count`, `range`, `delta`, `percent_change`) and has two modes mirroring
Grafana's Reduce transform. The default `reduce_fields` mode accepts exactly one
calculation (the historical `calculation` field, or a one-entry `calculations` list),
collapses each series into a single sample, and places the result on the timestamp of the
series' last finite sample so a table panel shows one row per series. The `series_to_rows`
mode accepts one to four `calculations` and expands each series into one row per
calculation (`ref_id` like `reduce_0_min`, label `A · min`), so a table panel can read
every calculation separately instead of only the first one. A series without any finite
sample keeps an empty sample list so the renderer still reports "no data" instead of a
fabricated zero. `calculation` and `calculations` are mutually exclusive, duplicate or
unknown calculations are rejected, and more than one calculation requires
`series_to_rows` so a payload never looks like it renders more columns than the series
model can carry. `group_by` merges every series that shares
one identity field — `ref_id`, `node_id`, `metric` or `aggregation` — aggregating the group's
finite samples per timestamp with `sum`, `mean`, `min`, `max` or `count`. It accepts the
historical single `calculation`, or a `calculations` list of one to four entries where each
calculation produces its own merged series (ref ids `group_by_<index>_<calculation>`), so one
authoring step can show both the sum and the maximum of a group. The optional `alias` names
the merged series and defaults to the group key; with more than one calculation the label
becomes `<name> (<calculation>)`, matching Grafana's `Field (calculation)` column naming.
A timestamp no member reports stays missing and a group without any finite sample keeps an
empty sample list, so the renderer keeps reporting "no data" instead of a fabricated zero.

`reduce` with `series_to_rows` and `group_by` are also the two steps whose Grafana result is a
table frame rather than a set of time series, so they additionally produce one. The reduce
frame is Grafana's Series to rows: one row per field, one column per calculation, named after
the calculation (`Field | Minimum | Maximum`). The group-by frame is Grafana's Group by: one
row per group and one column per calculation, named `<group field> (calculation)`, with the
calculation applied to every finite sample of the group across the whole window (a `count`
column counts samples and is unit-less). A `table` panel renders that frame as-is — key
columns as text, value columns through the same value-mapping and per-series threshold path
as every other table, gaps as an empty cell, sorted by any column and exported to CSV with
the localized headers. A frame stays authoritative for the rest of the chain as long as the
later steps also act on rows: `limit` keeps the first one to eight rows; `sort_by` reorders
them by the column named in `field` when it is given (any column, key or value) and otherwise
by the column named after the statistic (`calculation_<statistic>`, so `mean` sorts by
the mean column of a Series-to-rows or Group-by frame). When the statistic names no column and
the frame has exactly one value column, that column is used; with several value columns the
produced order is kept and a `field` that names no column leaves the order untouched.
`filter_by_value` with a `column` keeps only the rows whose cell in that column satisfies the
comparison; it addresses value columns only, an unknown column or a key column leaves the rows
untouched, and an empty cell never matches. Without `column` the step stays a series filter
and the frame is not used, exactly as before. Frame rows use the same comparison as the
interactive header sort — text for key columns, numbers for value columns, missing values last,
ties stable — so a default order always matches what clicking that header would show. Any other
later step (`rename_by_regex`, `filter_by_label`, `binary_operation`, another `reduce`
or `group_by`) rewrites series identity or values, so the panel falls back to the series
rendering (`long` or `wide`) and the frame is not used. A `column` on a chain without a frame
is a no-op on the series: the filter only exists for frame rows. The series output itself is
unchanged, so charts keep rendering the same data they rendered before this rule existed.

`binary_operation` appends a series, `group_by`
merges series, and `reduce` and `group_by` are the only steps that rewrite sample values;
the other steps only rename, reorder or drop series, so a chain composes
predictably, an empty chain renders the query result unchanged, and a chain that removes
every series renders the standard "no samples" empty state. The editor applies the same rules before saving, and the
JSON bundle importer keeps the chain but rejects unknown kinds, mixed fields and
out-of-range parameters.

Panels also carry Grafana's Overrides tab as an optional `overrides` list of at most
16 rules. Every rule is `{ "matcher": { "type": "by_name" | "by_regex" | "by_ref_id" |
"by_type" | "by_value", "value": ... }, "color", "unit", "hide_legend" }`: `by_name`
matches a series label exactly, `by_regex` matches a printable RE2 expression of at most
128 bytes, `by_ref_id` matches a query's series id (`A`, `B`, ...) which is how Grafana's
"Fields returned by query" selector pins one query, `by_type` matches the series' unit
family (`none`, `number`, `percent`, `bytes`, `rate`, `duration`, `other`) which is how
Grafana's "Fields with type" reads in a model whose series are always numbers, and
`by_value` matches a finite number against the series' latest sample, which is Grafana's
"Fields with values". A `by_type` value outside that list and a `by_value` value that does
not parse are rejected by the server, the importer and the editor, so a saved rule can
never be inert by construction. Matched series take the covered colour (panel palette
key), display unit (at most 32 bytes) or legend visibility.
Overrides only restyle a series — samples, queries and ordering stay untouched, all
matching rules apply in order with the later one winning, and a rule without any property
is rejected so a saved rule is never silently inert. Row and text panels cannot carry
overrides because they render no series. The editor, the JSON bundle importer and the
server all apply the same limits.

Overrides also reach the per-series line style through the optional `line_width` (integer
1–4) and `fill_opacity` (0–100 percent) properties. Both follow the panel-level
availability: `line_width` is accepted on `timeseries` panels, `fill_opacity` on
`timeseries` and `barchart`, and every other panel type rejects them so a saved rule is
never ignored by the renderer; the editor hides the two inputs on those types as well.
A series with neither an override nor a declared colour takes the next palette key in
render order, so the derived series produced by `binary_operation` and `group_by` are not
all painted the same default blue.
An overridden series draws its own stroke width, and a series whose override sets
`fill_opacity` to 0 stops filling and returns to a plain line, while gradient stops use
the per-series opacity so one series can fade differently from the rest. Switching a panel
to a type that cannot render them drops the incompatible properties — and the whole rule
when nothing else remained — matching how the panel-level line width and fill are cleaned
up on a type switch.

The optional `axis_placement` (`"left"` or `"right"`) override moves a matched series onto
the other value axis, which is Grafana's Overrides → Axis → Placement and the switch that
turns a panel into a dual-axis chart. It follows the same availability rule as the
panel-level axis placement: only `timeseries` and `barchart` panels accept it, and any
other type is rejected rather than saved with a property the renderer would ignore. A
series without the override stays on the side the panel declares, so adding the first
override is what creates the second axis: the renderer draws the right axis only once both
sides hold at least one series, and each axis is scaled and formatted from the series
bound to it (the panel-level fixed/soft limits, logarithmic scale and centered zero still
apply to both). Stacking is per axis as well — each side owns its own stack group and its
own 100% normalization in percent-stacked panels, while the tooltip keeps showing the
original values.

`axis_label`, `axis_soft_min`, `axis_soft_max` and `axis_width` are the rest of Grafana's
Overrides → Axis and apply to the value axis the matched series sits on: the label replaces
the panel-level axis title for that side, the soft limits start the axis at the given
value while still expanding when data exceeds them (the same Soft min / Soft max semantics
as the panel-level fields), and the width in pixels replaces the derived axis width so a long
unit or a rotated title can get the room it needs. Several overrides matching series on the
same axis stack in order with the later one winning, so a rule can pin one series and a later
rule rename the whole side. They follow the same availability rule as `axis_placement` (only
`timeseries` and `barchart` panels) and the soft limits are rejected when the panel itself
declares a fixed `min`/`max`, because the fixed panel limit wins on both axes and would make
the soft value silently dead. `axis_width` accepts whole numbers from 0 to 500, where 0 and an
absent value both mean "derive the width from the unit and the label"; out-of-range values are
rejected by the API, the importer and the editor.

The optional `thresholds` override gives a matched series its own colour band, which is
Grafana's Overrides → Thresholds → Standard options. The band follows the panel-level
rules — at most four entries, palette colours, strictly ascending values — and it replaces
the panel band for that series instead of extending it; a series without the override keeps
using the panel band, and an empty list means the panel band applies. Only the panels that
colour a single value from thresholds consume it: `stat`, `gauge`, `bargauge`, `table` and
`statetimeline`. `timeseries`, `barchart` and `piechart` colour by series colour instead, so
they reject the property (and the editor hides the input) rather than saving a rule the
renderer would ignore. Switching a panel to one of those types drops the bands, and the
rule disappears entirely when the band was its last property.

The optional `fill_below_to` override turns the matched series' fill into a band that ends on
another series, which is Grafana's Overrides → Graph styles → Fill below to. Its value is the
printable name (at most 128 bytes) of the target series: the renderer prefers a series with the
same node and otherwise the first series carrying that name, and a name that matches nothing
leaves the plain line untouched. Only unstacked `timeseries` panels accept it — stacked panels
draw layered areas, other types draw no per-series fill — so the server and the JSON importer
reject the combination and the editor hides the input; turning stacking on clears the bands and
a rule left without any property disappears. The band breaks wherever either series has no
reading, and the tooltip keeps reporting the covered series' own value instead of the
difference. A band whose target sits on the other value axis is not drawn, because the two axes
are scaled independently.

Overrides also carry Grafana's Overrides → Standard options as the optional
`display_name` (printable, at most 128 bytes), `decimals` (whole numbers 0-6), `min` and
`max` properties. `display_name` replaces the series label everywhere it is shown — the
legend, the tooltip, the value panels, the table headers and the state timeline rows —
while `decimals` replaces the panel-level decimal places for that series alone, so one
covered series can print `50.5%` next to neighbours printing `51%`; the inspector's Stats
table and the CSV export follow the same resolution. `min`/`max` replace the panel value
range for a single series and exist where a single value is mapped onto a range, which is
Grafana's own availability rule: `stat`, `gauge` and `bargauge`. `display_name` and
`decimals` are accepted on the eight panel types that render a series name and a formatted
value (`timeseries`, `barchart`, `stat`, `gauge`, `bargauge`, `table`, `statetimeline`,
`piechart`), and every other type rejects them, so the editor never saves a property the
renderer ignores. Switching a panel to a type that cannot render them drops them — and the
whole rule when nothing else remained — exactly like the line-style properties.

The panel-level Standard options → No value text is the optional `no_value` string
(printable, at most 32 bytes): it replaces the renderer's default `—` wherever a field has
no sample, which is the value panels, the table cells (both long and wide orientations),
the state timeline and the pie chart. Panels without such a placeholder reject it, the
limit is enforced by the API, the JSON importer and the editor, and an unknown panel type
keeps the default dash.

Panels and the dashboard itself carry Grafana-style links. A panel accepts up to
five `links` and a dashboard definition up to eight; each link is `{ "title",
"url", "new_tab" }` with a 1-40 byte title and a URL of at most 1 KiB. URLs must be
absolute `http://`/`https://` or same-origin paths beginning with a single `/`, so
`javascript:`, `data:` and protocol-relative `//host` targets are rejected; control
characters and spaces are rejected too, and row panels never carry links. Link
URLs may contain placeholders that the client substitutes at render time: panel
links support `${node}`, `${node_name}`, `${group}`, `${group_name}`,
`${metric}`, `${aggregation}`, `${from}`, `${to}` and `${__url_time_range}`;
dashboard links support `${from}`, `${to}`, `${__url_time_range}` and
`${__dashboard}`, because they have no series context. Both kinds also accept
`${var-<name>}` for a declared dashboard variable, joining multi-select values
with commas. Unknown placeholders are left untouched so a typo stays visible.
Rendering is a centered link bar in the panel (Grafana's panel links) and a
navigation strip under the toolbar for the dashboard; `new_tab` adds
`target="_blank"` with `rel="noopener noreferrer"`. The JSON bundle carries links
in both directions and rejects invalid protocols or an out-of-range count. A dashboard
definition may also set the optional boolean `hide_links`, which drops the dashboard link bar and every panel
data link in read-only views (public node pages and wallboards) while the admin workspace keeps rendering them;
absent or `false` keeps the historical behaviour, and the JSON bundle rejects a non-boolean value.

A `stat` panel may set the optional `sparkline` boolean (Grafana's Value options → Sparkline) to draw the
recent trend of its series under the value. The field is rejected on every other panel type, including `text`
and `row` panels, and an absent field keeps the historical rendering. The sparkline splits on null gaps,
keeps only segments of at least two samples and draws a flat series on the midline; it inherits the threshold
colour of the value it summarises.

The sparkline takes three optional options of its own: `sparkline_line_width` (integer 1–4, default 2),
`sparkline_fill_opacity` (0–100 percent, default 0 for a bare line) and `sparkline_gradient_mode`
(`none`/`opacity`/`hue`, default `none`). All three need `sparkline` set to `true`: an enabled stat panel
accepts them, a disabled or absent switch rejects them with
`sparkline line width, fill opacity and gradient mode require an enabled sparkline`, and every other panel
type rejects them with the stat-only message. Fill opacity draws a closed area path under each visible
segment, and the line width is rendered with `vector-effect="non-scaling-stroke"` so the stroke stays constant
while the panel resizes. A gradient other than `none` also needs a non-zero fill opacity, otherwise it would
be invisible: `opacity` fades the series colour into transparency and `hue` rotates the hue while keeping the
opacity, and both reuse the same SVG gradient ids as the time series panel. The dashboard bundle imports and
exports all three fields and rejects out-of-range, non-stat or gradient-without-fill values as an invalid file.

Panels of type `text` render a Markdown subset instead of querying data: headings (`#`-`######`),
unordered and ordered lists, fenced code blocks, inline code, bold, italic, links and tables whose delimiter row
controls per-column alignment (`:---` left, `:---:` centre, `---:` right, `---` default). The body lives in the
optional `text` field, is limited to 4096 bytes, must be valid UTF-8 and may not contain control characters
other than newline, carriage return and tab. The renderer emits React elements, so raw HTML is escaped and
never executed, and links only become anchors for absolute `http://`/`https://` URLs or same-origin paths
beginning with a single `/`; `javascript:` and `data:` targets stay plain text. Text panels carry a title,
size, markdown and up to five panel links, and are rejected when any query or visualization field (series,
repeat, legend, thresholds, unit, transformations and the rest) is set. Conversely, a non-text panel that
declares `text` is rejected. Text panels never enter the query queue, so a dashboard full of notes costs no
metric queries. The editor offers the same fields, and the JSON bundle imports `text` in both directions.

Acceptance includes migration/backfill, public/admin access, policy/binding
validation, aggregation/null/counter reset and query-budget tests; dashboard
ownership/publication/conflict/rollback tests; MTS-only backup/restore and
retention; at least 90% coverage; and real browser edit/save/reload/range/export/
error/mobile checks. Keep the commercial goal active until the full register is
verified, not just this workspace.
