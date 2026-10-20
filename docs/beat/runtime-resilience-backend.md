# Runtime resilience backend

This working-tree batch wires the shared metrics core into the Server. It does
not register v15, deploy the workspace, or change the retained preview instance.
The public payload and existing node, metric-catalog, location, lifecycle and GPU
privacy filtering remain unchanged.

## Ownership and stream

`/api/v1/ws/metrics` consumes one supervised `metrics_fanout` producer. It builds
immediately, then every five seconds, with sequential bounded storage reads and
one JSON encoding per generation. Connections never initiate a build. Missing,
closed or unhealthy MTS dependencies fail the generation, including an empty
Fleet; they cannot become a successful empty snapshot.

The handler uses `middleware.ClientIP` after the trusted-proxy middleware and
retains the previous `x/net/websocket` Origin parsing/presence policy. It does
not change the public Origin policy reserved for the later ingress batch.
`/ws` and `/metrics/ws` return explicit no-store 404 responses before the SPA
fallback. Full connection, frame, deadline, IP and rate limits are documented in
[the shared core contract](metrics-fanout-core.md).

Last-good bytes retain their original timestamp. Their age and stale state are
observable, and the fixed thirty-second window governs new delivery. Queues are
bounded to one pending generation per connection; each writer shares immutable
frame bytes. Expired generations are dropped without inventing current data.

## Supervision and shutdown

Each worker receives its actual owning context and one tracked completion path.
Ordinary iteration failures remain in the workers' existing bounded scheduling
and retry loops; consecutive failures and total failures are recorded, and a
success resets the consecutive count. Cancellation is not a failure. Scheduler
lag follows attempt progress; a failed but timely iteration does not turn the
age of an older success into a fictitious scheduling delay.

An unexpected return or panic records a terminal failure, logs a fixed reason
without the panic payload/stack, and cancels the context used by the HTTP server.
There are no in-process worker restarts. Startup grace is the greater of thirty
seconds or two expected intervals; four consecutive errors or a last-success
age beyond four expected intervals degrades worker readiness.

Normal shutdown marks readiness unavailable, rejects new upgrades, cancels the
producer and waits for all active/pending WebSocket connections. It then performs
bounded HTTP shutdown, cancels and joins background workers, waits for the
existing service operations, and finally closes the stores. The listener goroutine
is joined; a failed HTTP shutdown forces socket closure. Planned producer closure
is distinguished from an unexpected worker exit. Failed startup unwinds the same
owned resources through deferred cleanup.

## Readiness, metrics and owner DTO

`/healthz` remains liveness. `/readyz` uses fixed `worker_alerts`,
`worker_traffic_reports`, `worker_maintenance`, and `worker_metrics_fanout` checks,
plus checks for configured additional workers. The aggregate `schedulers` check
is removed. Public details are fixed reasons and never include raw storage errors.
Status and metrics responses are no-store. Restore readiness semantics remain
unchanged.

Prometheus includes `beat_worker_up`, last-success age, consecutive and total
iteration failures, scheduler lag and unexpected exits, plus snapshot build/age,
WebSocket admission/connection/drop series. Worker and reason labels are fixed.
`beat_mts_query_failures_total{consumer="metrics_fanout",class="read_failed"}`
counts actual snapshot MTS read failures; SQLite/settings failures are not
mislabelled as MTS failures. Existing scheduler worker series remain available.

Owner-only `GET /api/v1/admin/runtime` requires a current session but no recent
proof. The standard success envelope has this `data` shape:

```text
server_time: RFC3339
stopping: boolean
workers: [{name, configured, started, running, started_at, stopped_at,
           last_attempt, last_success, last_failure, consecutive_failures,
           failures, restarts, interval_seconds, scheduler_lag_seconds,
           state, failure}]
metrics: {generation, timestamp, age_seconds, stale, available,
          connections, reserved, tracked_ips, builds, failures, dropped,
          build_duration_seconds, skipped_cadences, closed}
```

Worker states are `starting`, `healthy`, `degraded`, `failed`, and `stopped`.
Failure values are empty, `iteration_failed`, `unexpected_exit`, or `panic`.
Times before a recorded event are Go zero timestamps; consumers should use
started/available/state flags and avoid displaying these as sampled events.
The endpoint exposes no node IDs, addresses, Origin values, raw errors, paths,
connection records or control operations. UI integration remains a separate task.

## Verification

The batch tests real RFC 6455 clients sharing one generation, trusted proxy IP
resolution, unchanged public shape/privacy, old-route 404, active socket cleanup,
closed-store rejection, stale readiness/status, owner permissions without recent
proof, bounded metric labels and worker exit/panic cancellation of the actual HTTP
context. The isolated core also tests frame limits, coalescing, slow writers,
clock rollback and bounded admission.

On 2026-10-09, fixed-seed race verification used `env -u GOROOT GOMAXPROCS=4`
with `go test -race -shuffle=20261009 -count=1 -p=2 -timeout=10m` across
`./cmd/server ./internal/api ./internal/api/handler ./internal/metricsfanout`.
All packages passed. The final Server rerun after lag calculation/refactoring
also passed. Recorded package results were:

| Package | Elapsed | Statement coverage |
| --- | --- | --- |
| cmd/server | 58.015s | 93.0% |
| internal/api | 156.990s | 87.1% |
| internal/api/handler | 130.390s | 93.6% |
| internal/metricsfanout | 1.240s | 94.4% |

Additional storage-error regression tests passed under race (3.396s). Combining
current Server coverage, the full remaining package run and those additional
handler branches gives **93.0%**. The modified metrics handler, new API runtime
bindings, worker supervisor and runtime telemetry functions reach **100%**;
the API package aggregate includes existing areas below 90%.

`go vet`, `golangci-lint` (zero issues), `goimports-reviser`, production function
size/complexity checks and diff checks passed. Temporary profiles, logs and
checker programs were removed. These checks cover this backend batch; the root
integration still owns the whole-repository gate and deployment decision.
