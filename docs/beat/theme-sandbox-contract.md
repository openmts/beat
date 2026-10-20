# Beat sandbox theme contract

This document describes the implemented public theme bridge **protocol 2**.
ZIP manifest `schema_version` remains **1**. These are separate versions.
Declarative layout/settings schemas and author-triggered metric queries are not
part of this bridge contract.

## Package and resources

- Put `beat-theme.json` at the ZIP root; keep its existing required metadata and
  complete `{path,size,sha256}` inventory. The manifest does not inventory itself.
- A sandbox package declares `runtime: "sandboxed"` and contains exactly one
  `.html` file. That file is the entry, including when nested under `dist/`.
- `themes/pulse` is the shipped reference package for this contract: copy it to
  start a package, and keep its `dist/beat-sdk.js` byte-identical to
  `themes/sdk/bridge.js`.
- Use relative resource URLs. HTML script/style/image references must resolve to
  inventoried package files. ES modules and relative module imports are supported.
- External HTML resource URLs, inline scripts/handlers, forms, frames, base tags,
  redirects, SVG files and active HTML namespaces are rejected. The entry may
  use external packaged scripts, styles and raster images. Theme navigation
  uses bridge messages; static anchors may only reference page fragments.
- Allowed resources: UTF-8 JSON, JS, CSS, the entry HTML, PNG/JPEG/WebP, WOFF2.
  File signatures must match raster/font extensions. Preview must be a raster.
- Limits: ZIP 25 MiB, extracted 64 MiB, 512 entries, manifest 256 KiB,
  text/code 8 MiB each, raster/font 12 MiB each, paths 240 UTF-8 bytes / 8 levels.
  Paths must be relative, with no hidden/dot/parent segments, controls,
  backslashes, drive prefixes or links. File inventory and hashes are exact.
- Installed resources are public, immutable and read without credentials.
  Only inventoried files with matching size/SHA-256 are served. The asset routes
  provide wildcard CORS without `Access-Control-Allow-Credentials`, allowing
  modules/fonts to load inside an opaque-origin iframe.
- The asset prefix lives outside `/api/v1`: the server mounts it on the
  outermost mux through `api.Router.MountThemeAssets` (`cmd/server/runtime_http.go`).
  Registering it on the API mux makes it unreachable, because that mux is only
  served under `/api/v1/` and the SPA handler would answer with 404.
- HTML responses themselves enforce CSP `sandbox allow-scripts`, including
  direct top-level visits. Iframes also use `sandbox="allow-scripts"`.
  No parent DOM, cookies, storage, forms, popups, workers or direct API/network
  access is granted. Administrator/authentication pages remain built-in.

## Handshake and lifecycle

After each iframe `load`, Beat generates a fresh 128-bit random `instance_id` and
sends this message to that frame using `postMessage(..., "*")` because its
security origin is opaque:

```json
{"beat":{"version":2,"instance_id":"...","type":"init"}}
```

Register the message listener during module startup, before `load`. Validate
`event.source === parent` and the expected Beat origin, then acknowledge `init`:

```js
let instance;
const hostOrigin = new URL(location.href).origin;
addEventListener("message", (event) => {
  if (event.source !== parent || event.origin !== hostOrigin) return;
  const message = event.data?.beat;
  if (message?.version !== 2) return;
  if (message.type === "init" && /^[0-9a-f]{32}$/.test(message.instance_id)) {
    instance = message.instance_id;
    parent.postMessage({beat: {version: 2, instance_id: instance, type: "ready"}}, hostOrigin);
  } else if (message.type === "snapshot" && message.instance_id === instance) {
    renderPublicSnapshot(message.payload); // Use textContent/React; do not inject HTML.
  }
});
```

Beat sends a snapshot immediately after `ready`, on public data changes, and
with a stable 5-second heartbeat. Re-renders do not restart the heartbeat.
Instances from other frames or older document loads are ignored. A theme must
be ready within 5 seconds; repeated document reloads are bounded. Timeout,
invalid current-instance messages, theme errors and data-budget failures remove
the iframe and show the built-in view. This affects the current visitor only;
a theme cannot change the server's global activation through this bridge.

## Messages from a theme

The outer object contains only `beat`. The envelope contains only the fields
shown below; unknown fields, unsupported versions/types, wrong instances and
invalid payloads are rejected. Input is limited to 16 KiB, depth 4 and 12 keys.

```json
{"beat":{"version":2,"instance_id":"...","type":"ready"}}
{"beat":{"version":2,"instance_id":"...","type":"navigate","payload":{"path":"/node/visible-id"}}}
{"beat":{"version":2,"instance_id":"...","type":"error","payload":{"code":"render_failed"}}}
```

- `navigate.path` is `/` or `/node/<id>` for a node currently present in the
  public snapshot. Query strings, fragments, administration routes, external
  URLs and hidden nodes are disallowed. Beat uses its normal browser router.
- `error.code` is `render_failed`, `unsupported_data`, or `runtime_error`.
  Do not send exception stacks, credentials, HTML or arbitrary error strings.
- `ready` has no payload. A new `init` always requires a new acknowledgement.

## Snapshot payload

Only these public fields are exported. Missing/invalid numeric samples are
omitted; zero is a valid value. Nothing is forwarded by spreading API objects.

| Field | Content |
| --- | --- |
| `timestamp` | Snapshot creation time, ISO-8601 UTC |
| `route` | `{kind:"dashboard"}` or `{kind:"node",node_id?:string}`; the ID is omitted until the requested node is publicly visible |
| `site` | `site_title`, `site_description`, `logo_url`, `favicon_url`, `show_ip_addresses`, `show_network_quality` |
| `locale`, `theme` | Current Beat locale and resolved `light` / `dark` appearance |
| `configuration` | Public configuration JSON object from the active descriptor; currently defaults to `{}` |
| `groups` | Groups associated with visible nodes: `id`, `name`, `sort_order` |
| `nodes` | Public nodes in the current page's scope; fields listed below |
| `network` | `{visible,tasks}`; no task/result data when public network quality is disabled |
| `history` | Detail page only: `{from,to,series,series_info?}` from successful public workspace queries for the current node |
| `state` | `nodes`, `metrics`, `network`, each `loading`, `ready`, or `error`; no raw server errors |

Node fields: `id`, `name`, `alias`, `group_id`, `status`, `tags`, `public_remark`,
`last_seen`, `system`, `metrics`, optional `traffic`, and `host` only when IP
visibility is enabled. Detail snapshots never include a different/hidden node.

When present, `telemetry_at`, `telemetry_age_seconds`, and `telemetry_state`
describe confirmed metric freshness separately from `last_seen` contact time.
`unavailable` requires null time/age; `fresh`, `delayed`, and `stale` require a
valid timestamp and non-negative integer age. Invalid combinations reject the
snapshot. Raw node responses without snapshot metadata may omit all three.

`system` contains only `cpu_model`, `os`, `platform`, `os_version`, `kernel`,
`arch`, `virtualization`, `agent_version`.

Metric IDs and units:

- Percent (0–100): `cpu`, `memory`, `disk`, `swap`.
- CPU cores: `cpu_used`, `cpu_total`.
- Bytes: `memory_used`, `memory_total`, `disk_used`, `disk_total`, `swap_used`,
  `swap_total`, `net_recv_total`, `net_sent_total`.
- Bytes/second: `disk_read`, `disk_write`, `net_recv`, `net_sent`.
- Counts/numbers: `load1`, `load5`, `load15`, `processes`, `tcp_connections`,
  `udp_connections`. `uptime` is seconds.
- History series include only keys actually queried by the active public/node
  dashboard template and allowed by the public enabled catalog, including
  `custom_<name>` metrics. Collection commands are never sent.
- Points use the separate `ThemeHistoryPoint` shape `{timestamp,value}`;
  timestamps and `from/to` are Unix seconds, while `value` is a finite number
  or **null** for a missing bucket. Null gaps must not become zero or connect
  neighboring samples. The query API's millisecond timestamps are converted
  before publishing the snapshot; the older global metric point type is unchanged.
- `series_info[metric]` contains the actual `{aggregation,unit}` from the
  successful response. A `rate` unit may differ from the catalog's counter unit.
  For repeated metric keys, the first successful series in template order wins.
  Only results with the same actual successful `from/to` enter one history;
  responses from different refresh rounds are never assigned one invented range.
- Failed refreshes retain available successful history with its original range
  and `state.metrics: "error"`. In-flight work uses `loading`. A metric not yet
  queried has no history key; a successful empty query may contain null buckets.
  A template containing only other visible nodes produces no current-node history.
  The theme cannot initiate a new query through protocol 2.

Public node pages load the default/node template and public catalog before
querying. They support time controls, refresh/pause, inspection, CSV/JSON export
and links to public node details. Editing remains in the administrator workspace.
Template variables use a public nodes/groups index, including explicit public
comparison nodes. The outer page owns the two-concurrent-query poller, so history
continues to update when a sandbox replaces the built-in view. Node summary reads
also refresh with cancellation, visibility handling and a bounded request timeout.

Traffic fields: `sent`, `received`, `used`, `limit`, `remaining`, `percentage`,
`reset_day`, `limit_type`, `period_start`, `next_reset`, `tracked_since`, `status`.
Byte quantities and nullable unlimited values keep the public API's semantics.

Network tasks contain only `id`, `name`, `type`, `nodes`; each node has `node_id`
and `latest` (null, or `timestamp`, `success`, optional finite `latency_ms` and
`status_code`). Only enabled public tasks and visible node results are included.
Probe targets, internal errors, agent credentials and administration data are
never included.

## Budgets and privacy

Snapshots are capped at 1 MiB including the envelope, 1000 visible nodes,
200 groups/tasks, 128 tags per node, 64 history series, and 1024 points per series.
The JSON tree is bounded as well. Exceeding a budget explicitly falls back to
the built-in view; fleets/series are not silently truncated. Future pagination
or query controls require a separately documented contract.

Configuration is public: never place secrets in it. It must be an object,
no more than 16 KiB / 256 keys / depth 5. Prototype keys and known credential,
private-remark and SSH-key fields are rejected, including nested values.
Snapshots exclude hidden nodes, suppressed hosts, private remarks, SSH keys,
agent tokens, sessions and custom-metric commands. Public site settings must
load successfully before the sandbox starts.


## Repeatable browser verification

The optional `TestThemeBrowserFixture` starts an isolated loopback server with
its own temporary SQLite/theme files and synthetic public data. It does not
open the running deployment's configuration. Build `webui` first, then run:

```sh
BEAT_THEME_BROWSER_FIXTURE=1 go test -timeout=30m -run '^TestThemeBrowserFixture$' -v ./internal/api/handler
node webui/scripts/theme-sandbox-smoke.mjs --server <THEME_FIXTURE_URL> --output <private-artifact-directory> --playwright <playwright-module-path> --chromium <chromium-executable>
```

The script verifies the fixture identity, raw ZIP upload from the real admin
UI, opaque-origin module/font loading, public navigation, history, heartbeats,
privacy, fallback, mobile rendering and direct HTML isolation. It uses a fresh
browser context with synthetic cookies and shuts down that fixture afterward.
The fixture's fake authentication is test-only. Normal test runs skip it.
