# Public PWA, kiosk, and wallboard

Updated: 2026-07-30

Status: reviewed design; implementation requires explicit approval

## Goal and product boundary

Beat will make the public monitoring surface installable and provide a dedicated
wallboard route for unattended displays without weakening the product's core
boundary: public monitoring needs no login, while every administrative action
requires authenticated authorization.

Installability and wallboard presentation are separate deliverables. The PWA
batch adds install metadata, versioned static-shell caching, explicit offline
state, and controlled update activation. The wallboard batch adds a resource-
bounded `/wallboard` view that consumes existing public REST/WebSocket data. It
does not create an image/video stream or an alternate data path.

## Stable competitor evidence and intentional differences

Komari Web `1.3.2`, fixed at
`14f4067e9b69813b24a0255e565f7f49bff0a1bd`, uses `vite-plugin-pwa`, a
standalone manifest, static asset caching, and automatic service-worker updates:

- stable Web source:
  <https://github.com/komari-monitor/komari-web/tree/14f4067e9b69813b24a0255e565f7f49bff0a1bd>
- stable Vite configuration:
  <https://github.com/komari-monitor/komari-web/blob/14f4067e9b69813b24a0255e565f7f49bff0a1bd/vite.config.ts>

The reviewed configuration also applies a long-lived `NetworkFirst` cache to
matching API origins. Beat rejects that policy: cached monitoring or
authentication responses can present stale data as live and can retain
sensitive response content longer than intended.

Komari Server exposes public `/api/mjpeg_live`, and DStatus publishes PWA
metadata. Beat will not reproduce MJPEG. Encoding and serving repeated full
frames duplicates the existing data stream, consumes CPU and bandwidth per
viewer, limits accessibility, and creates another privacy and authorization
surface. The safer equivalent is a responsive HTML wallboard backed by the same
public data contracts as `/`.

The Web evidence is pinned to the stable release. The current Komari Web `main`
was force-rewritten to a different older history, so it is not treated as a
stable supply-chain or product baseline. Stable Komari release workflows build
the matching Web tag, while snapshot workflows that clone the default Web
branch can produce a different UI and must not be used as Beat's release model.

## PWA manifest and installability

The manifest contains only public presentation metadata:

- stable application name and short name derived from bounded site settings;
- `start_url` on `/` with no token or sensitive query value;
- `scope` `/`, display `standalone`, and explicit text direction/language;
- semantic background/theme colors aligned with the active public shell;
- same-origin 192x192 and 512x512 icons plus maskable variants;
- optional same-origin screenshots that contain fixture/demo data only.

Icons are versioned build assets. Uploaded site logos are not silently promoted
to install icons because arbitrary dimensions, formats, transparency, or content
can make an invalid or misleading install package. Generated icon derivatives,
if later supported, require bounded decoding and a separate approval.

The app does not show a custom install promotion on first visit. Browser-native
installation remains available; an optional install command may appear in an
existing overflow menu only after the browser raises the install event. Dismissal
is respected and never blocks monitoring.

## Service-worker cache policy

The service worker has one narrow responsibility: make the versioned public
application shell load predictably. It precaches only the build-manifest graph
for `/`, its build-owned hashed JS/CSS/fonts/icons, and a minimal public
HTML/offline shell associated with the same release manifest. Administrator,
terminal/xterm, backup, security, and node-detail chart chunks are not in the
public precache merely because Vite emitted them.

The following are always network-only and never written to Cache Storage:

- `/api/**`, including node lists, history, site settings, backups, and health;
- all WebSocket handshakes and stream data;
- `/admin/**`, login, logout, setup, session, TOTP, OIDC, and reauthentication;
- `/healthz`, `/readyz`, `/metrics`, and future debugging/profiling routes;
- responses carrying `Set-Cookie`, `Cache-Control: no-store`, authorization
  headers, or any non-GET method;
- user-uploaded backup, theme, logo, or restore content unless a future explicit
  immutable-public-asset contract approves it.

Public navigations may fall back to the matching cached shell only when the
network is unavailable. The shell then displays an explicit offline/unavailable
state and no node values. It never renders the last metric snapshot, last node
list, last readiness result, or cached authenticated content as current.

Cache names include the exact Beat build version. Activation removes only old
Beat-owned cache names after no active client depends on them. Cache keys ignore
no query parameters unless each ignored parameter is proven presentation-only.
Service-worker code, manifest, and HTML use revalidation/no-cache headers;
hashed assets may be immutable.

## Update lifecycle

Beat does not use forced `autoUpdate` reload behavior. A newly installed worker
downloads in the background and reports that an update is ready. Activation is:

- immediate only when there is no active client;
- offered through a compact, dismissible `Alert` when a public page is idle;
- deferred while a user is selecting a time range, reading a node, editing an
  admin form, using terminal/remote operations, uploading/restoring a backup, or
  while any unsaved state exists;
- never triggered merely by a service-worker timer.

The update command keeps its action name throughout the UI and announces the
new version. Reload happens only after explicit acceptance or a later clean app
start. A failed worker install leaves the current version active and observable.

## Offline and degraded states

The cached shell distinguishes three states:

- offline: browser networking is unavailable;
- service unavailable: a network exists but the Beat API cannot be reached;
- stale: the last successful live snapshot exceeds the fleet stale threshold.

Offline shows no historical metrics unless they are fetched live after network
recovery. Service unavailable may keep already-rendered in-memory values visible
for continuity, but labels them stale with the last successful server time. A
page reload while offline starts empty, not from persisted monitoring data.

Recovery triggers one REST reconciliation and one WebSocket connection. It does
not replay queued requests because public monitoring has no offline writes and
admin writes must never be background-synced.

## Wallboard route and presentation

`/wallboard` is a public, read-only route. Its job is to make current fleet
health legible from a distance on a fixed display. It uses the public Fleet data
model, visibility rules, formatters, stale handling, and one shared WebSocket
connection.

The screen contains:

1. a slim site title, current connection state, and server-synchronized time;
2. a fleet status strip with visible/online counts and aggregate rates;
3. a stable grid of grouped node tiles for the current page;
4. a minimal page/group progress indicator when rotation is required.

There is no hero, feature copy, setup instruction, nested Card, admin shortcut,
or pointer-dependent control in the normal wallboard frame. Configuration opens
through a small icon button with a tooltip and uses a shadcn `Sheet` with an
accessible title.

The wallboard tile is not a second full `NodeCard`. At distance it shows display
name, group name, online/stale state, CPU, memory, root disk, and current network
rate. Used/total values remain available where they fit at the selected density;
percent alone is not the sole capacity concept. Raw IP/host is omitted by
default and may appear only when the existing public IP policy explicitly
permits it. Private nodes and fields never reach the client.

Only the root filesystem is presented. No per-mount or per-interface telemetry
is introduced.

## Kiosk configuration without new persistence

The first wallboard version uses bounded URL and in-memory settings, avoiding a
new database schema:

- one or more public group IDs, always displayed as names;
- density (`comfortable`, `compact`, or `distance`);
- sort and offline ordering from the Fleet design;
- rotation interval within a safe bounded range;
- optional browser fullscreen request initiated by a user gesture;
- light, dark, or system theme using existing public theme support.

Unknown IDs are ignored and never displayed raw. The URL contains no admin
credential, share token, IP allowlist, or private node identifier. Local storage
may remember purely presentational settings on that device but is neither
authoritative nor synchronized. Owner-managed persisted wallboard presets would
be a separate SQLite/API batch requiring approval and audit.

## Pagination, refresh, and layout stability

The browser calculates deterministic pages from the visible result after
filtering and grouping. Defaults are capped by viewport and density, with an
absolute maximum of 24 rendered tiles per page and 200 visible nodes accepted
by the client before it requires narrower filters. Only the current page is
mounted; an optional next page may be prepared without opening another stream.

Rotation defaults to 15 seconds, pauses while the configuration Sheet is open,
and resumes from the current page. Data refresh does not reset the rotation
timer. Nodes keep stable slots during a page interval; status changes update the
tile but do not reshuffle it until the next page boundary. Removed or newly
visible nodes are reconciled at that boundary.

Every tile has a stable aspect ratio and fixed metric tracks. Long localized
names truncate with accessible full text and never expand the grid. The screen
adapts across 16:9, 16:10, ultrawide, portrait signage, and ordinary mobile
without overlapping text.

## Resource and abuse controls

Wallboard reuses one public WebSocket and the same visibility-aware REST
reconciliation as Fleet. It creates no per-node connections, MJPEG encoder,
canvas animation loop, or unbounded chart history query.

Required limits include:

- one live metrics socket per wallboard client;
- bounded REST response size and node count;
- server connection/origin/rate limits shared with the public dashboard;
- visibility-aware pause when the document is hidden;
- capped reconnect backoff with jitter;
- no background polling while offline;
- no full chart series on wallboard tiles;
- aggregate Prometheus counters with bounded reason labels and no node IDs.

If a deployment needs hundreds or thousands of visible nodes, it requires a
reviewed server-side fleet pagination/aggregation contract rather than raising
browser and WebSocket limits.

## Privacy and authorization invariants

The service worker and wallboard do not alter Server authorization. Public
responses are identical whether the browser has no cookie or an unrelated admin
cookie. `/wallboard` never calls an admin endpoint, and an unauthenticated
request to any management endpoint remains `401`.

Node visibility is applied before summary counts, filtering, pagination, and
streaming. Hidden node counts cannot be inferred from page totals. Private
remarks, private IPs, Agent tokens, admin identities, alert internals, and
storage health details are absent from wallboard data and logs.

The service worker does not intercept OIDC redirects, login callbacks, logout,
or setup. CSP, same-origin checks, secure cookies, and trusted-proxy behavior
remain unchanged. PWA installation grants no additional capability.

## Accessibility and burn-in controls

The wallboard remains usable as HTML: headings and status text have semantic
structure, status is never color-only, contrast meets the normal public theme,
and the configuration Sheet is keyboard accessible. Automatic rotation pauses
when focus is inside an interactive control. Reduced-motion preference removes
slide/fade transitions and uses an immediate page change.

Burn-in mitigation is deliberately restrained:

- page rotation changes the occupied pixels for multi-page fleets;
- the status clock and live values update naturally;
- an optional bounded one-to-two-pixel layout phase shift occurs no more than
  every 30 minutes and never clips content;
- an optional scheduled blank/dim overlay may be configured locally, but Beat
  does not claim to control physical panel brightness or replace display-side
  power management.

No ambient gradient, flashing status, continuous ticker, or decorative motion
is used.

## Delivery order and dependencies

The schema-free release batches are `pwa-static-shell` and `wallboard-html` in
[`commercial-parity-implementation-sequence.md`](./commercial-parity-implementation-sequence.md).
They consume no SQLite migration number.

1. Public Fleet behavior and stale-state model are approved and implemented.
2. Public/admin/node-detail/terminal route chunks are isolated and the
   anonymous Fleet bundle budget passes before a service worker can precache
   the public build graph.
3. PWA manifest, static-shell cache rules, offline state, and update lifecycle
   are implemented and verified independently.
4. `/wallboard` reuses the approved Fleet selectors, formatters, stream, and
   privacy model.
5. Persisted wallboard presets, if desired, are reviewed later as an
   authenticated settings/API/schema change.

The PWA does not depend on the metric catalog. Wallboard country/lifecycle or
GPU values depend on their respective reviewed batches and remain absent until
those batches are approved and deployed.

## Test and acceptance gates

PWA tests cover manifest validity, icon dimensions, exact public-route build
graph and cache allowlist, exclusion of admin/xterm/chart route chunks,
network-only route classes, no-store responses, versioned cache cleanup,
offline empty state, recovery, failed install, update deferral, OIDC/admin
bypass, and proof that API/auth/metric data never enters Cache Storage.

Wallboard tests cover no-login rendering, zero/one/many groups, page boundaries,
stable slots, rotation pause/resume, hidden-tab behavior, one-socket invariant,
reconnect, stale/offline states, long labels and IPv6 privacy, raw-ID prevention,
root-filesystem-only metrics, responsive fixtures, keyboard access, reduced
motion, and 200-node rejection/filter guidance.

Acceptance requires at least 90 percent frontend line coverage, lint, production
build, service-worker browser tests in a production origin, cold/offline/recovery
smoke tests, cache inspection proving no dynamic response persistence, resource
measurement for multiple wallboard clients, and IPv4/IPv6 verification that
public routes remain available without login while admin routes stay protected.

## Approval boundary

This document authorizes no service worker, manifest dependency, route, caching
behavior, or server limit change. PWA and wallboard implementation may begin
only after explicit user approval of the cache allowlist, update lifecycle,
public route behavior, privacy model, and any dependency or deployment changes.
