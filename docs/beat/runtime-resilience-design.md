# Runtime resilience and realtime fanout

Updated: 2026-07-30

Status: reviewed design; implementation requires explicit approval

## Goal and current evidence

Beat has deployed liveness, readiness, Prometheus metrics, background alert,
traffic-report and maintenance loops, plus a public metrics WebSocket. The
current implementation does not yet prove that those long-lived workers remain
healthy, and every connected public browser independently rebuilds the complete
Fleet snapshot from MTS every five seconds.

The confirmed current behavior is:

- `startBackgroundTasks` starts three goroutines and immediately stores one
  aggregate `schedulers=true` flag;
- `/readyz` reports only that aggregate flag, not individual worker liveness,
  last success, lag, consecutive failures, panic, or unexpected return;
- each `/api/v1/ws/metrics` client lists every public node, queries every known
  latest metric per node, and aggregates current-cycle traffic independently;
- REST node/current/history and traffic-report consumers propagate MTS query
  errors, but alert evaluation returns silently when a metric query fails;
- the MTS wrapper propagates errors returned by the engine, but the engine
  currently answers queries after `Close()` with an empty result and no error,
  and a regression test explicitly accepts that false-empty state;
- hijacked WebSocket connections are not registered with the Server shutdown
  lifecycle and can outlive `http.Server.Shutdown`;
- legacy `/ws` and `/metrics/ws` routes use a non-RFC WebSocket accept digest;
  `/metrics/ws` has no production broadcaster and `/ws` closes immediately.

This schema-free batch makes background execution and public realtime delivery
bounded, observable, and shutdown-safe without changing public login policy,
metric persistence, or the public snapshot payload.

## Non-goals and invariants

1. Public Fleet and node metrics remain available without login. Every
   administration or runtime-diagnostics action remains authenticated.
2. Agent, probe, derived, and other numeric time series remain exclusively in
   MTS. SQLite stores no worker samples, realtime snapshots, connection events,
   or metric values.
3. The first version changes no SQLite schema and keeps backup format `v4`.
4. Only `/api/v1/ws/metrics` remains the public metrics WebSocket. Removing
   `/ws` and `/metrics/ws` is an intentional API behavior change and requires
   explicit approval and release-note evidence.
5. The public JSON snapshot keeps its reviewed fields and privacy filtering.
   No private node, address, remark, identifier label, or administrator data is
   introduced by fanout.
6. Runtime state is in-memory and reconstructed on every process start. It is
   operational evidence, not durable business state.
7. Process restart remains owned by systemd, Compose, Kubernetes, or another
   external manager. The supervisor exposes failure and initiates controlled
   shutdown where required; it does not provide a remote restart API.
8. HTTP body, authentication KDF, public history-query, REST cache, and Agent
   ingestion admission are owned by
   [`ingress-resource-governance-design.md`](./ingress-resource-governance-design.md).
9. A closed, closing, unavailable, or failed MTS lifecycle state is a storage
   error. No REST response, WebSocket generation, alert evaluation, or report
   may reinterpret that state as an empty series, missing metric, zero value,
   healthy rule, or successful report.

## Worker registry and ownership

The Server owns one registry for these long-lived workers:

- `alerts`;
- `traffic_reports`;
- `maintenance`;
- `metrics_fanout`.

Each worker has exactly one parent context, one tracked goroutine, and one
completion path. The registry starts before readiness can become healthy and
uses a `WaitGroup` or equivalent structured concurrency primitive to wait for
all workers. Workers do not create untracked ticker goroutines, and every timer,
channel, and connection is released on cancellation.

Each registry entry exposes a lock-safe snapshot containing:

- configured/started/running state;
- last loop start and last clean stop time;
- last attempt, last success, and last failure time;
- consecutive failure count and bounded failure class;
- expected interval and current scheduler lag where meaningful;
- restart count only when the owning policy permits an in-process restart;
- terminal state: `starting`, `healthy`, `degraded`, `failed`, or `stopped`.

Raw errors, SQL/MTS queries, node IDs, channel IDs, paths, addresses, and secret
material never enter public readiness details or metric labels. Structured logs
may include a fixed worker name, failure class, attempt count, and request-free
error text subject to the existing redaction policy.

## Failure and supervision policy

Ordinary operation errors are handled inside the worker loop with bounded
exponential backoff and jitter. A successful iteration resets the consecutive
failure counter. Cancellation is not recorded as a failure.

An alert evaluation iteration collects metric-query failures instead of
returning silently per rule/node. It may continue evaluating independent
availability rules, but the iteration result remains failed, the worker
registry records the fixed `storage_query` class, and no affected resource or
traffic rule is resolved, reset, or treated as healthy. Traffic-report and
snapshot workers preserve their existing fail-closed query behavior.

Readiness becomes degraded when any required worker:

- has not completed initial startup within its fixed grace period;
- is no longer running before parent cancellation;
- exceeds its worker-specific last-success age or scheduler-lag threshold;
- reaches the configured consecutive transient-failure threshold; or
- reports a terminal dependency or invariant error.

An unexpected worker return or panic is not hidden by an endless recover loop.
The registry records the terminal state, makes readiness fail immediately, logs
one structured fatal worker event, cancels the Server root context, and lets the
external process manager restart the process after graceful shutdown. Panic
tests use a controlled test worker; production errors are never induced merely
to exercise supervision.

Transient store, notification, or network failures do not cause a restart on
their first occurrence. The worker continues using its bounded retry policy
while readiness and metrics expose age and failure progression. The exact
thresholds are constants owned by each worker and are covered by deterministic
clock tests rather than administrator-editable arbitrary values.

## Shared metrics snapshot producer

`metrics_fanout` is the only periodic producer of the public realtime snapshot.
It builds one privacy-filtered snapshot at a fixed five-second cadence and
publishes the immutable encoded result to all current clients.

The producer contract is:

1. perform one public-node/settings read for the generation;
2. use bounded query concurrency and the MTS/query APIs available at the time;
3. build and JSON-encode the complete snapshot once;
4. atomically publish a generation number, server timestamp, payload, and build
   result to the fanout hub;
5. never start a second build while the prior generation is still running;
6. record skipped cadence as scheduler lag rather than accumulating work.

This batch removes multiplication by WebSocket client count. It does not claim
that the current per-metric MTS query shape is optimal; metric-catalog `v8` may
later add a bounded batch-latest query. The shared producer must remain correct
with either query implementation.

The MTS wrapper owns an explicit lock-safe lifecycle state. `Close()` first
prevents new operations, waits for admitted operations under the existing
operation lock, closes the engine once, and leaves every later query/write with
a stable closed-store error even if the underlying engine would otherwise
return an empty result. Readiness and worker consumers use this wrapper state;
they do not infer health from an empty map.

On a failed build, the last successful payload remains available with its
original timestamp and explicit stale age. A newly connected client may receive
that last-good payload only within the fixed stale window. Beyond the window,
the connection receives no fabricated current snapshot and readiness for
`metrics_fanout` is degraded. One broken node or malformed measurement follows
the public serializer's reviewed all-or-explicit-partial policy; silent removal
of a visible node is forbidden.

## Bounded WebSocket fanout

The public stream applies both global and per-client-address limits before
upgrade. Client address resolution uses the canonical trusted-proxy helper; an
untrusted forwarding header cannot split or evade the limit key. IPv4 and IPv6
addresses are normalized before comparison, and raw addresses never appear in
Prometheus labels.

The fixed controls include:

- global active-connection limit;
- per canonical client-IP active-connection limit;
- global and per-IP token-bucket handshake limits;
- maximum header size and existing route-specific origin policy;
- one writer goroutine per accepted connection;
- outbound buffer capacity of one generation with replacement/coalescing;
- fixed write deadline and maximum encoded snapshot size;
- ping/pong or read-deadline handling sufficient to detect abandoned clients;
- immediate rejection before expensive MTS work because clients never trigger
  snapshot construction directly.

If a client cannot consume the latest generation before the next publish, the
pending older generation is replaced and a dropped/coalesced counter increases.
The Server never creates an unbounded queue and never blocks the shared producer
on one client. Repeated write timeout, protocol failure, shutdown, or limit
revocation closes and unregisters the client exactly once.

Rejected upgrades return a bounded HTTP status without client-count details.
Limits must account for NAT concentration without becoming ineffective against
one source; initial constants and production tuning evidence belong in the
release runbook, not a browser-editable setting.

## Shutdown order

The Server owns an explicit runtime lifecycle with this order:

1. mark the instance not ready and stop accepting new WebSocket upgrades;
2. stop the `metrics_fanout` producer and prevent new snapshot publication;
3. close all registered WebSocket clients with a bounded close deadline;
4. wait for every connection writer/reader goroutine to exit;
5. stop accepting new HTTP work and complete bounded HTTP shutdown;
6. cancel background workers and wait for their tracked goroutines;
7. wait for already-started maintenance and backup operations under their
   existing bounded shutdown contract;
8. flush and close SQLite/MTS only after no worker or connection can access
   them.

Startup failure unwinds acquired resources in the reverse ownership order.
Tests prove that a canceled or failed startup leaves no hub, ticker, worker,
connection, or store-using goroutine behind.

## Readiness, metrics, and logging

`/readyz` replaces the aggregate `schedulers` check with fixed check names:

- `worker_alerts`;
- `worker_traffic_reports`;
- `worker_maintenance`;
- `worker_metrics_fanout`.

Public detail is limited to `starting`, `stale`, `lagging`, `failing`,
`stopped`, or `unavailable`. `/healthz` remains process liveness and does not
fail for a degraded dependency. Restore readiness behavior remains unchanged.

Prometheus adds bounded-label series such as:

- `beat_worker_up{worker}`;
- `beat_worker_last_success_age_seconds{worker}`;
- `beat_worker_consecutive_failures{worker}`;
- `beat_worker_failures_total{worker,class}`;
- `beat_worker_unexpected_exits_total{worker,reason}`;
- `beat_worker_scheduler_lag_seconds{worker}`;
- `beat_metrics_snapshot_build_duration_seconds`;
- `beat_metrics_snapshot_builds_total{result}`;
- `beat_metrics_snapshot_age_seconds`;
- `beat_mts_query_failures_total{consumer,class}`;
- `beat_metrics_ws_connections`;
- `beat_metrics_ws_upgrades_total{result}`;
- `beat_metrics_ws_dropped_snapshots_total{reason}`.

Worker, result, class, and reason values come from fixed enums. No node, group,
account, IP, Origin, user-agent, URL, or error string becomes a label. Logs use
stable event names for worker start/stop/failure, snapshot build failure,
upgrade rejection, slow-client eviction, and shutdown timeout.

## Operations UI and frontend behavior

The authenticated `/admin/operations` surface adds a read-only `Runtime`
section using the existing shadcn `base-nova` system. It uses `Tabs` for runtime
categories, a compact `Table` for fixed worker rows, `Badge` plus text for
state, `Progress` only for bounded age/lag thresholds, `Alert` for degraded
summary, and `Skeleton` only during initial load. It does not nest cards or
offer start, stop, restart, arbitrary threshold, or WebSocket-disconnect
controls.

Selects are unnecessary for the first version. Any later filter renders a
human worker label and keeps the enum key only as its submitted value. Raw IDs,
IP addresses, error strings, and connection-level records are not displayed.

The public frontend keeps its no-reload merge behavior, capped reconnect with
jitter, silent REST reconciliation, stale indication, and hidden-document
pause/resume contract. It opens at most one metrics connection per page. A
visibility transition cannot create overlapping connections, and a rejected
or stale stream falls back to bounded REST reconciliation without blanking the
last successful Fleet state.

## Tests and acceptance

Backend tests use deterministic clocks and cover:

- worker startup, success, transient failure, backoff, recovery, cancellation,
  unexpected return, controlled panic, readiness thresholds, and sanitized
  output;
- one producer build serving many clients without client-proportional MTS
  calls, no overlapping builds, last-good/stale behavior, encode failure, and
  bounded query concurrency;
- global/per-IP connection and handshake limits, trusted/untrusted proxy
  resolution, IPv4/IPv6 normalization, Origin policy, slow clients, buffer
  coalescing, write timeout, protocol close, and exact unregister behavior;
- RFC 6455 interoperability for `/api/v1/ws/metrics` and `404` for removed
  `/ws` and `/metrics/ws`, including the standard accept-key test vector;
- shutdown ordering with active HTTP, WebSocket, worker, maintenance, backup,
  SQLite, and MTS fakes; no store access after close;
- closed/closing and injected MTS query failures returning explicit errors
  through REST, shared snapshot, alert, and traffic-report consumers; no empty
  chart, zero value, healthy evaluation, false recovery, or successful report;
- goroutine-leak, race, repeated start/stop, partial-startup unwind, metrics
  label allowlists, and failure-log redaction.

Frontend tests cover one connection per page, visibility pause/resume, capped
reconnect, REST fallback, stale state, preserved data, and human-readable
authenticated runtime status. Load and soak acceptance proves that MTS query
volume follows snapshot cadence and Fleet size rather than client count, memory
remains bounded with slow clients, and shutdown completes within its deadline.

Completion also requires at least 90 percent backend statement and frontend
line coverage, race/shuffle tests, `go vet`, `goimports-reviser`,
`golangci-lint`, vulnerability/module verification, frontend audit/lint/build,
browser WebSocket smoke tests, and deployed IPv4/IPv6 public/admin isolation.

## Delivery, rollback, and approval

This is the schema-free `runtime-resilience` batch in
[`commercial-parity-implementation-sequence.md`](./commercial-parity-implementation-sequence.md).
It consumes no SQLite migration, leaves the next migration at `v20`, and keeps
backup archive format `v4`.

It depends on the deployed operability foundation and public metrics stream.
The external public WebSocket origin policy from `v15` composes with, but is not
required to implement, connection lifecycle and fanout limits. Metric catalog
`v8` may optimize latest-value retrieval without changing this ownership model.
HTTP recovery for privileged mutations also composes with
[`administrative-mutation-consistency-design.md`](./administrative-mutation-consistency-design.md):
a recovered panic must preserve the exact request identity and record failure,
or `unknown` after admitted external work, rather than deriving audit success
from the final HTTP status.
The same design owns terminal authorization invalidation. Runtime resilience
owns the connection registry, timers and exact unregister/shutdown behavior and
must expose a close-by-session/user hook without independently deciding roles or
recent-auth policy.

Rollback restores the previous binary/frontend. It creates no persistent data
requiring conversion. Rollback also restores the legacy routes only if the old
binary is intentionally redeployed; release notes must state that those routes
were non-functional and are not compatibility endpoints.

Implementation requires explicit approval because it changes readiness detail,
background failure propagation, WebSocket resource policy, shutdown order, and
removes two public routes. This design alone authorizes none of those changes.
