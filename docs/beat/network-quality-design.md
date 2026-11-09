# Network quality tasks

Status: Implemented through schema `v33` in the working tree on 2026-11-09.
The extension adds `agent_to_target` / `server_to_agent` directions,
multi-packet ICMP quality sampling, and reusable target groups for Agent-side
probes. It has not been deployed yet.

Deployed acceptance evidence:

- Public IPv6 homepage, quality API, and admin SPA shell returned HTTP 200.
- Public quality reads succeeded without a token; admin and Agent endpoints
  returned HTTP 401 without their required bearer tokens.
- One enabled public task of each probe type produced successful latest values
  and four historical points at acceptance time; the HTTP probe returned 200.
- SQLite contained only `network_tasks` and `network_task_nodes` for this
  capability and had no metric-, probe-, or time-series-like objects.
- The MTS catalog and current shard WAL contained the `network_probe`
  measurement.

## Goal

Add scheduled ICMP, TCP, and HTTP quality checks comparable to Komari and
DStatus while preserving Beat's storage boundary:

- SQLite stores task definitions, target groups, and node assignments only.
- MTS stores every probe sample and performs history aggregation.
- Public reads require no login.
- Task management requires administrator authentication.
- Agent execution uses bounded concurrency and context cancellation.

Community references:

- Komari ping task model and scheduler:
  <https://github.com/komari-monitor/komari/blob/4077201f098774511eaf504f220c5f6be009346b/database/models/pingTask.go>
  and
  <https://github.com/komari-monitor/komari/blob/4077201f098774511eaf504f220c5f6be009346b/utils/pingSchedule.go>
- DStatus MTR capability:
  <https://github.com/fev125/dstatus/blob/4afc9e43c9df28096352c05ae924fcadbc830a2f/neko-status/mtr/mtr.go>

Advanced one-off path and bandwidth jobs are intentionally separate from this
scheduled quality model and are reviewed in
[`advanced-network-diagnostics-design.md`](./advanced-network-diagnostics-design.md).
MTR is not used for every scheduled sample: it requires the system `mtr`
binary, raw-socket capability, and considerable output. It remains an explicit
path diagnostic, while scheduled quality uses bounded ICMP/TCP/HTTP probes.

## Product behavior

An administrator can create, edit, enable, disable, sort, publish, and delete
a task. A task either applies to all nodes or to an explicit node set. New
nodes automatically receive tasks configured for all nodes.

Each task has one direction:

- `agent_to_target`: the Agent probes one target or a target group, then
  reports each result through the API. A three-carrier group keeps Mobile,
  Unicom, and Telecom as independent series.
- `server_to_agent`: the Beat Server probes each assigned node using a target
  template such as `{{host}}`, `{{host_port}}`, or `{{ssh_port}}`. The Server
  writes the result directly to MTS. This direction remains a single-target
  template and is not grouped.

Target groups expose stable per-target IDs, labels, carrier, region, IP family,
sample count, enabled state, and an explicit address-visibility switch. Public
responses hide the configured address unless `visible_address` is enabled.

Agents poll their current assignment set every 30 seconds. A successful poll
returns assignments with a 90-second expiry. If refreshes fail, the Agent may
finish work already running but must stop starting expired assignments.

Public pages show only enabled tasks marked public. Admin pages show all tasks,
assignments, latest state, and history.

## SQLite application data

### `network_tasks`

| Field | Constraint |
| --- | --- |
| `id` | UUID text primary key |
| `name` | 1-100 Unicode characters |
| `direction` | `agent_to_target` or `server_to_agent` |
| `type` | `icmp`, `tcp`, or `http` |
| `target_mode` | `single` or `group`; Server→Agent always uses `single` |
| `target` | First target kept for compatibility, maximum 2048 bytes |
| `ip_family` | `auto`, `ipv4`, or `ipv6` |
| `interval_seconds` | 10-86400 |
| `timeout_milliseconds` | 100-30000 and no greater than the interval |
| `packet_count` | 1-20; only multi-packet ICMP uses values above one |
| `all_nodes` | Boolean assignment mode |
| `enabled` | Boolean execution state |
| `is_public` | Boolean public visibility |
| `sort_order` | Non-negative integer |
| `created_at` | UTC timestamp |
| `updated_at` | UTC timestamp |

### `network_task_nodes`

| Field | Constraint |
| --- | --- |
| `task_id` | Foreign key to `network_tasks`, cascade delete |
| `node_id` | Foreign key to `nodes`, cascade delete |

The composite primary key is `(task_id, node_id)`. Assignment rows are ignored
when `all_nodes` is true. Task writes and assignment replacement occur in one
SQLite transaction.

### `network_task_targets`

| Field | Constraint |
| --- | --- |
| `id` | UUID text primary key |
| `task_id` | Foreign key to `network_tasks`, cascade delete |
| `label` | 1-100 Unicode characters |
| `carrier` | `mobile`, `unicom`, `telecom`, or `custom` |
| `region` | Optional region label, at most 100 Unicode characters |
| `target` | Type-specific target, maximum 2048 bytes |
| `ip_family` | `auto`, `ipv4`, or `ipv6` |
| `packet_count` | 1-20 probes for this target |
| `visible_address` | Whether public responses may expose the address |
| `enabled` | Whether this target is scheduled |
| `sort_order` | Stable display and scheduling order |

The target ID is part of the Agent scheduling key and the MTS `target` tag, so
each carrier is stored, queried, and charted independently. Existing
`agent_to_target` tasks are migrated to one hidden-address custom target.

No latency, availability, status code, error, rollup, or latest-value column or
table may be added to SQLite. The SQLite schema whitelist test must include the
three application tables and reject metric-like columns.

## MTS data

All results use one `network_probe` measurement.

Tags:

- `task`: task UUID
- `node`: node UUID
- `target`: stable target UUID
- `carrier`: `mobile`, `unicom`, `telecom`, or `custom`
- `region`: optional region label
- `ip_family`: selected IP family
- `type`: `icmp`, `tcp`, or `http`
- `direction`: `agent_to_target` or `server_to_agent`

Fields:

- `latency_ms`: elapsed milliseconds, including failed attempts
- `success`: `1.0` for success and `0.0` for failure
- `status_code`: HTTP status or zero for non-HTTP checks
- `error_code`: stable low-cardinality error code
- `sent_packets` and `received_packets`: ICMP sample counts
- `loss_percent`: ICMP packet loss percentage
- `jitter_ms`: mean absolute consecutive RTT difference
- `min_latency_ms` and `max_latency_ms`: ICMP RTT range
- `attempt_count` and `success_count`: attempted and successful probes
- `success_percent`: successful attempts divided by attempted probes
- `p95_latency_ms` and `stddev_latency_ms`: latency distribution statistics

ICMP exposes packet loss from received replies. TCP and HTTP do not fabricate
packet loss or jitter fields; their quality view uses success rate from repeated
attempts. Multi-packet ICMP also exposes RTT variation. TCP and HTTP default to
three attempts per schedule, while ICMP defaults to five packets.

The point timestamp is the Agent's finish time with nanosecond precision. The
server accepts it only when it is within five minutes of server time. MTS
last-write-wins behavior makes a retried result with the same task, node, and
timestamp idempotent.

Stable error codes:

- `none`
- `dns`
- `timeout`
- `permission`
- `connection_refused`
- `network_unreachable`
- `tls`
- `http_status`
- `invalid_target`
- `protocol`
- `io`
- `internal`

Canceled probes caused by Agent shutdown are not reported.

History endpoints choose an MTS group-by window that returns at most 600 points
per target. Each target is queried independently and returned in `series[]`;
the response includes the target ID, label, carrier, region, visibility flag,
and points. Each point contains average latency, success percentage, optional
loss percentage, optional p95, and sample count. Public responses never include
the real address when `visible_address` is disabled. Query ranges default to 24
hours and are limited to 31 days.

Deleting a task first deletes matching `network_probe` series by the `task`
tag. SQLite deletion proceeds only after MTS accepts the deletion. This avoids
unaddressable result history. A SQLite failure leaves the task definition in
place so deletion can be retried.

## HTTP API

### Public

- `GET /api/v1/network/quality`
  - Lists enabled, public tasks with assigned nodes and latest MTS state.
- `GET /api/v1/network/quality/{task_id}/history`
  - Requires `node_id`, `from`, and `to` query parameters.
  - Returns data only when the task is enabled, public, and assigned to the
    requested node.

### Administrator

- `GET /api/v1/network/tasks`
- `POST /api/v1/network/tasks`
- `PUT /api/v1/network/tasks/{task_id}`
- `DELETE /api/v1/network/tasks/{task_id}`
- `PUT /api/v1/network/tasks/sort`
- `GET /api/v1/network/tasks/{task_id}/history`

All administrator routes require the existing admin bearer token. JSON
responses contain node and task labels alongside IDs so every Select trigger
can render a human-readable value.

### Agent

- `GET /api/v1/network/assignments?node_name={name}`
- `POST /api/v1/network/results`

Both routes require the existing Agent bearer token. Assignment lookup uses
the registered node name until per-node credentials replace the shared token.
The results endpoint rejects disabled, expired, unknown, or unassigned task and
node combinations. A request contains at most 64 results and 256 KiB of JSON.

Assignment response:

```json
{
  "expires_at": "2026-07-29T12:01:30Z",
  "tasks": [
    {
      "id": "task-uuid",
      "target_id": "target-uuid",
      "name": "Primary API",
      "target_label": "Mobile",
      "type": "http",
      "carrier": "mobile",
      "region": "Shanghai",
      "target": "https://status.example.com/health",
      "ip_family": "auto",
      "interval_seconds": 60,
      "timeout_milliseconds": 3000
    }
  ]
}
```

Result request:

```json
{
  "node_name": "node-one",
  "results": [
    {
      "task_id": "task-uuid",
      "target_id": "target-uuid",
      "finished_at": "2026-07-29T12:00:01.123456789Z",
      "latency_ms": 23.4,
      "success": true,
      "status_code": 204,
      "error_code": "none"
    }
  ]
}
```

## Agent execution

The Agent owns three long-running components under the process context:

1. Assignment refresher, every 30 seconds.
2. Scheduler, which enqueues due and unexpired tasks without overlapping the
   same task.
3. Four fixed probe workers plus one result batch reporter.

The work queue is bounded to 128 entries. The result queue is bounded to 256
entries and flushes at 32 results or two seconds. Backpressure never creates
new goroutines. Server failures retain a bounded retry batch with exponential
backoff; the oldest result is dropped only after the queue reaches its fixed
limit and the failure is logged.

Every probe derives a timeout context from the Agent process context. Shutdown
cancels DNS, dial, TLS, ICMP, and report operations and waits for all owned
goroutines.

The Beat Server runs an equivalent bounded scheduler for `server_to_agent`
tasks: it refreshes enabled tasks every 30 seconds, schedules each task/node
pair without overlap, and uses four probe workers. Server results bypass the
Agent result endpoint and are written directly to MTS.

## Probe semantics

### ICMP

- Target is an IP literal or DNS host without a port.
- Resolve according to `ip_family`; `auto` prefers IPv6 when available and
  falls back to IPv4.
- Use `golang.org/x/net/icmp`; never execute a shell command.
- Use unprivileged `udp4` or `udp6` ping sockets where supported.
- A platform or container without permission reports `permission`.
- Send `packet_count` echo requests (default 5, maximum 20) at a bounded
  interval and match replies by sequence number.
- A task succeeds when at least one reply arrives; partial loss remains visible
  through `loss_percent`, and total loss reports `timeout`.
- Latency is the average RTT of received replies; jitter and the minimum/maximum
  RTT range are computed from the received samples.

Current production host evidence:

- Agent runs as root.
- `net.ipv4.ping_group_range` is `0 2147483647`.
- IPv4 loopback ICMP succeeds at approximately 0.03 ms.
- IPv6 loopback ICMP succeeds at approximately 0.01 ms.

No `CAP_NET_RAW` file capability is required for the current deployment.

### TCP

- Target must parse as `host:port`; IPv6 uses `[address]:port`.
- Resolve and dial exactly one validated address per attempt.
- Successful connection establishment constitutes success.
- Close the connection immediately after timing the handshake.

### HTTP

- Target must be an absolute `http` or `https` URL.
- Reject user information, fragments, missing hosts, unsupported schemes,
  unspecified addresses, multicast addresses, and link-local addresses.
- Private and loopback destinations are allowed because authenticated
  administrators must be able to monitor internal and node-local services.
- Resolve once per execution, validate every selected address, and dial that
  address directly while preserving the original host for HTTP and TLS. This
  prevents DNS rebinding between validation and connection.
- Do not use environment proxies, redirects, cookies, credentials, or custom
  request headers.
- Send `GET`, disable compression, cap response headers at 64 KiB, and close the
  body without downloading it.
- Status 200-399 is success. Other statuses report `http_status` and retain the
  actual status code.

## Frontend

The frontend remains a quiet operational interface using the existing shadcn
Base Nova system.

Admin `/admin/network`:

- Task cards grouped by enabled and disabled state.
- Each card shows type icon, task name, target, interval, timeout, assignment
  summary, public state, latest latency, and success rate.
- Create and edit use a Dialog with `FieldGroup` and `Field` composition.
- Type and IP family use Select controls with localized labels.
- Assignment mode uses a segmented ToggleGroup. Explicit nodes use a searchable
  label-based multi-select; IDs never appear in triggers or cards.
- Enabled and public state use Switch controls.

Public dashboard:

- A full-width network-quality band appears after node groups.
- Each public task is one compact row with assigned-node status cells and a
  small latency rail. It is not nested inside another card.
- Node cards aggregate one public result per node. ICMP is preferred over TCP
  and HTTP, and Server→Agent is preferred within the same probe type. When a
  public task is configured but no sample exists yet, the card still shows
  latency and packet loss as `--`.
- Selecting a task and node opens history with 1h, 6h, 24h, and 7d ranges.
- A three-carrier task renders three independently colored latency lines
  (Mobile, Unicom, Telecom) on a shared timeline. The same chart can switch to
  ICMP packet loss or TCP/HTTP success rate.
- Failed samples break the latency line instead of rendering a misleading zero.
- The horizontal domain matches the selected range even when samples are
  sparse. The vertical axis formats sub-second values as milliseconds and
  values at or above 1000 ms as seconds.
- Data refreshes in the background without a page reload.

## Test and acceptance matrix

Backend tests must cover:

- Old SQLite migration, repeated migration, constraints, transactions, foreign
  key cascades, sort order, assignment replacement, and context cancellation.
- Admin authentication for every write and private read; public read access for
  published tasks only.
- Validation boundaries for every task field and target type, including target
  group count, duplicate IDs, carrier, region, IP family, and sample count.
- Assignment behavior for all nodes, explicit nodes, new nodes, disabled
  tasks, expiry, and unknown node names.
- Exact MTS fields and tags, per-target isolation, duplicate result idempotency,
  latest state, 600-point aggregation, range limits, task deletion, and MTS
  error paths.
- IPv4 and IPv6 target parsing, DNS errors, timeout, refused connections, TLS
  errors, HTTP status handling, redirects, response header limits, and DNS
  rebinding protection.
- Worker bounds, non-overlap, refresh failure expiry, retry backpressure,
  shutdown, goroutine leak detection, and race detection.

Frontend tests must cover:

- Labels rather than IDs in every task, node, type, and IP-family control.
- Create, edit, delete, sort, enable, publish, assignment, empty, loading, and
  error states.
- Public filtering, address hiding, three-carrier series and legend colors,
  metric switching, background refresh, time-range domains, latency units, and
  desktop/mobile overflow.

Runtime acceptance requires:

- One ICMP, one TCP, and one HTTP task executed by the deployed Agent.
- All results visible in MTS and absent from SQLite.
- Public quality views accessible without login.
- Admin task writes rejected without a token and accepted with a token.
- Desktop and mobile screenshots with no overlap or clipped targets.
- Backend and frontend coverage at or above 90%, backend race tests, lint,
  production build, and an IPv6 public smoke test.
