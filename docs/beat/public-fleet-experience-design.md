# Public fleet experience

Updated: 2026-07-30

Status: reviewed design; implementation requires explicit approval

## Goal and product boundary

Beat will turn the existing no-login dashboard into a fleet exploration surface
for operators and visitors who need to scan many nodes quickly. The page's first
job is to answer which groups and nodes are healthy now, then make a specific
node easy to find without losing the user's group context.

The default presentation remains the user-approved grouped card layout. Every
visible group is a full-width section containing all matching node cards. A
table is an optional compact view inside each group, never the default and never
a single cross-group table that erases the grouping model.

Public fleet reads remain unauthenticated. Administration, configuration, and
all writes remain under `/admin` and require the existing authenticated and
authorized session flow.

## Stable competitor evidence

The reviewed Komari Web release is `1.3.2` at commit
`14f4067e9b69813b24a0255e565f7f49bff0a1bd`. Its stable fleet experience
includes summary totals, search, group and state filters, grid/table modes,
sortable resource and lifecycle columns, configurable offline ordering, result
counts, and background refresh. The evidence is fixed to the stable release
rather than the current Web `main`, whose history was force-rewritten and no
longer shares an ancestor with the previously inspected line:

- stable Web source:
  <https://github.com/komari-monitor/komari-web/tree/14f4067e9b69813b24a0255e565f7f49bff0a1bd>
- stable release tag:
  <https://github.com/komari-monitor/komari-web/releases/tag/1.3.2>
- current Server source inspected at
  `4077201f098774511eaf504f220c5f6be009346b`:
  <https://github.com/komari-monitor/komari/tree/4077201f098774511eaf504f220c5f6be009346b>

DStatus also demonstrates the commercial value of responsive fleet search,
filtering, sorting, and public status views. Beat uses only capability evidence;
DStatus does not declare a repository license and no implementation is copied.

Beat intentionally differs from both products by preserving grouped cards as
the primary information architecture and by treating public visibility and IP
privacy as server-enforced data contracts rather than presentation-only flags.

## Current Beat baseline and gaps

`frontend/src/pages/dashboard.tsx` currently renders one selected group through
tabs, a responsive card grid, a public metrics WebSocket, and a 30-second silent
REST refresh. `frontend/src/components/node-card.tsx` already displays CPU used
and total cores, memory/root-disk/swap used and total capacity, rates, cumulative
traffic, runtime values, tags, remarks, and overflow-safe system text.

The remaining fleet gaps are:

- no whole-fleet summary or result count;
- only one group is visible at a time instead of full grouped sections;
- no global search across public display fields;
- no stable sort, lifecycle filters, or offline ordering policy;
- no optional compact table view;
- WebSocket snapshots can replace REST data without an explicit stale/error
  state or visibility-aware connection policy;
- live metric sorting could move cards while a user is about to select one;
- the production build emits one `1,451.29 kB` JavaScript chunk (`421.40 kB`
  gzip). `App.tsx` synchronously imports the complete administrator route tree,
  so anonymous `/` visits also download terminal/xterm, security, backup,
  network, alert, and node-detail chart code that the public Fleet does not use.

## Information architecture

The public route remains `/`. Its vertical order is:

1. existing site header and public navigation;
2. compact fleet summary band;
3. one fleet toolbar for search, filters, sort, view, and result count;
4. visible group sections in configured group order;
5. the existing optional network-quality band.

The summary derives only from the same server-filtered public node collection:

- visible nodes and online nodes;
- visible groups represented by the current result;
- aggregate current upload and download rates;
- country count only after the reviewed GeoIP/lifecycle batch exists.

There is no separate public analytics store. Summary values are recomputed from
the latest public snapshot and disappear when the underlying field is not
public. Empty collections display zero or unavailable explicitly, never stale
cached totals.

## Visual and component direction

The design stays within the current shadcn `base-nova` system:

- color: existing semantic `background`, `foreground`, `muted`, `border`,
  `primary`, and `destructive` tokens; online/offline/warning meaning uses Badge
  variants and accessible text, not color alone;
- type: Geist for body and headings, tabular numerals for metrics, compact type
  inside cards and tables, and zero custom letter spacing;
- layout: constrained wide content, full-width unframed summary and toolbar
  bands, then independent group sections; only node records are Cards;
- signature: a sticky group rail showing group name, matching/total node count,
  and online count while its cards or rows are in view.

The summary uses a description list separated by `Separator`, not nested Cards.
The toolbar composes shadcn `InputGroup` for search, `Popover` plus `Command` for
multi-select filters when those components are installed, `Select` for a single
sort choice, and `ToggleGroup` with grid/table icons for view mode. Every option
renders a human label and keeps its ID only as the submitted machine value.

Group sections use a semantic heading and stable anchor. Card view reuses the
full `NodeCard` composition. Table view uses `Table`, a keyboard-focusable node
link, `Badge`, and the same formatters as cards. It does not recreate metrics or
formatting logic independently.

## Route asset isolation and delivery budget

The Fleet batch establishes route-level delivery boundaries before PWA static
shell caching:

- `/` loads only the public shell, Fleet page, current-value formatters, and
  components used by that page;
- `/node/:id` loads the node-detail and Recharts history workspace on demand;
- `/admin/*` loads the authenticated shell on demand, then lazy-loads each
  administrator page independently;
- xterm code and CSS load only after entering `/admin/terminal`;
- network-history charts, backup UI, security UI, and other heavy optional
  surfaces are absent from the anonymous Fleet dependency graph.

Use React `lazy`/`Suspense` and statically analyzable imports so Vite emits
stable hashed route chunks. A compact shell-aligned `Skeleton` is allowed only
during first route-module load; navigation never blanks already-rendered Fleet
data. A chunk failure presents a bounded retry action and does not loop reloads.

Node links and the administrator navigation may preload their own route chunk
on intentional hover or keyboard focus. The application does not eagerly
preload every admin page, terminal, chart, or dialog after Fleet hydration.

The initial CI budget is at most `250 kB` gzip JavaScript for a cold anonymous
`/` load and no emitted JavaScript chunk above Vite's `500 kB` raw warning
threshold without an explicit reviewed waiver. Build-manifest tests prove that
the `/` entry graph excludes administrator modules, `@xterm/*`, terminal CSS,
and Recharts. Browser-network tests are authoritative because a small entry
file that immediately preloads every route would still fail the product goal.

## Search, filter, and sort semantics

Search is case-insensitive and matches only public fields: display name, public
remark, public tags, OS/platform, architecture, and public country/region when
available. Raw node ID, group ID, hidden host/IP, private remark, token, and
administrator metadata are never searchable through the public response.

Initial filters are:

- one or more group names;
- online or offline state;
- public tags;
- country and expiry band only after the GeoIP/lifecycle dependency exists.

Sort choices are name, configured presentation order, status, CPU, memory,
root-disk usage, upload rate, download rate, and lifecycle date when available.
Sorting is stable: group order is always primary, the chosen node key is
secondary, configured node order is the tie-breaker, and node ID is the final
deterministic tie-breaker but is never displayed as a label.

Offline ordering is an explicit three-state setting:

- `First`: offline nodes precede online nodes inside each group;
- `Keep`: status does not alter the selected sort order;
- `Last`: offline nodes follow online nodes inside each group.

The default is `Last`. Metric-based sorts update at a bounded cadence rather
than on every WebSocket message. A focused or hovered card/row does not move
until interaction ends, preventing the target from shifting under a pointer or
keyboard user.

Filter and sort state is encoded in bounded URL query parameters so a public
view can be shared. Invalid or unknown values fall back safely. A local view-mode
preference may be remembered, but local storage is never an authorization or
visibility source.

## Card and table content

Cards retain the current operational density and show:

- display name, status, system/architecture, CPU model or public host label;
- CPU used/total cores plus percent;
- memory used/total plus percent;
- root filesystem used/total plus percent;
- swap used/total plus percent when reported;
- current upload/download rate and cumulative traffic;
- uptime, load, processes, connections, public tags and public remark;
- billing-cycle usage and public lifecycle values when configured.

Only the root filesystem is shown. No per-mount collection or presentation is
introduced. IP/host text uses a constrained row, `min-width: 0`, truncation, and
a tooltip only when the value is approved for public display. Hidden or private
IP fields are absent from the DOM, search index, accessible name, and tooltip.

The table keeps the scannable subset: name/status, system, CPU, memory, root
disk, network rate, traffic, and optional lifecycle. Additional details remain
on node detail rather than making the table horizontally unbounded. On narrow
screens the product defaults to cards; explicitly selected table mode uses a
labelled horizontal `ScrollArea` without clipping actions or text.

## Live refresh and stale behavior

The public fleet uses one WebSocket connection per page for current metrics and
a silent REST reconciliation for node/group metadata. The Server-side stream
depends on the shared producer and bounded connection lifecycle reviewed in
[`runtime-resilience-design.md`](./runtime-resilience-design.md). It never
reloads the document or replaces the fleet with skeletons after initial
success.

Required behavior:

1. initial REST data establishes the complete public node and group model;
2. WebSocket snapshots merge current metrics by node ID without inventing new
   visibility or metadata;
3. REST reconciliation runs every 30 seconds while visible and after reconnect;
4. the WebSocket and periodic polling pause when the document is hidden, then
   resume with one immediate reconciliation when visible;
5. reconnect uses capped exponential backoff with jitter and one in-flight
   attempt;
6. stale age is derived from the last successful server snapshot and displayed
   in a compact `Alert` only after a threshold;
7. silent failures keep prior data visible but never label it current; a manual
   retry is available after a sustained failure.

Out-of-order REST or WebSocket responses are rejected by request generation or
server snapshot time. Removed/hidden nodes disappear only after an authoritative
REST response or a versioned complete public snapshot, not because one metric
message omitted them.

## API, security, and privacy

The first implementation should compute search, filtering, sorting, and summary
client-side from the bounded public list to avoid a new public query API. If
fleet size later requires server-side pagination, that contract needs separate
approval and must apply public visibility before counting or filtering.

Public responses continue to omit private nodes and private remarks. IP display
must follow the site/node privacy setting already approved by the Server; the
frontend cannot recover or infer a hidden value. The fleet route never requests
admin endpoints and must behave the same with no cookie, an expired cookie, or
an authenticated admin cookie.

Search text and filter values are not sent to telemetry by default. URL values
have length/count limits and are escaped by React. No public control performs a
write.

## Accessibility and responsive behavior

Search has an explicit accessible label and clear button. Filter counts and
result changes use a polite live region without announcing every metric update.
Grid/table buttons expose pressed state and tooltips. Group headings are real
headings and anchor targets; a skip link can jump to the result list.

Cards preserve fixed internal tracks so changing metric digits does not resize
the grid. Long names, IPv4/IPv6 values, tags, remarks, localized labels, and
large byte values are tested at mobile and wide desktop sizes. Reduced-motion
users receive no animated reordering or view transition.

## Dependencies and delivery order

This schema-free behavior batch is `fleet-public` in
[`commercial-parity-implementation-sequence.md`](./commercial-parity-implementation-sequence.md)
and consumes no SQLite migration number.

The base fleet batch depends only on the current public node/group endpoints,
node-presentation visibility, current metric formatting, and live stream. It can
ship summary, grouped cards, search, state/tag filters, stable sorting, table
mode, visibility-aware refresh, and public/admin route asset isolation without
waiting for the metric catalog.

Production-scale realtime acceptance also depends on `runtime-resilience` so
many browsers share one Server snapshot producer and hidden/reconnecting pages
cannot create overlapping or unbounded connections.

Country and expiry filters depend on the reviewed GeoIP/node-lifecycle batch.
GPU columns depend on accelerator telemetry and remain off by default. A later
server-side pagination contract must be reviewed separately.

## Test and acceptance gates

Frontend tests cover:

- no-login rendering and proof that admin endpoints are never requested;
- all groups rendered in configured order with cards as the default;
- grid/table persistence and group boundaries in both modes;
- search across every allowed field and exclusion of IDs/private fields;
- human-readable group, tag, country, sort, and filter labels after selection;
- every stable sort, tie-breaker, and offline ordering state;
- CPU/memory/root-disk used and total formatting, long IP overflow, and privacy;
- WebSocket merge, REST reconciliation, hidden-tab pause/resume, reconnect,
  stale state, out-of-order responses, and silent failure behavior;
- build-manifest and browser-network proof that anonymous `/` excludes admin,
  xterm, terminal CSS, and Recharts chunks; node detail/admin/terminal chunks
  load only on their owning route, with hover/focus preload and chunk-failure
  recovery covered;
- keyboard navigation, live-region restraint, reduced motion, mobile layout,
  and no focused-item movement during live sorting.

Acceptance also requires at least 90 percent frontend line coverage, lint,
production build, browser smoke tests with large fixture fleets, and IPv4/IPv6
verification that `/` and public data remain `200` without login while every
management endpoint still returns `401` without authorization.

## Approval boundary

This document designs behavior but authorizes no implementation. The grouped
fleet interaction, URL state, refresh lifecycle, optional table mode, route
asset boundaries, loading/error behavior, privacy rules, and any new components
or API response fields require explicit user approval before existing frontend
behavior is changed.
