# Shared metrics fanout core

`internal/metricsfanout` provides one periodic snapshot producer and bounded
RFC 6455 connections. Runtime, routes, MTS privacy filtering, worker registry and
Prometheus integration are owned by the caller; this package registers nothing.

## Integration contract

- Construct with `New(Config)`. `Build`, `ClientIP` and `AllowOrigin` are required.
  The IP callback must apply the application's trusted proxy policy; the core
  never reads forwarding headers. Origin policy remains explicit.
- Supervise one blocking `Run(ctx)`. The first build is immediate, subsequent
  builds use a five-second cadence. Builds never overlap or catch up missed
  ticks. HTTP connections never initiate builds.
- `Build(ctx)` returns the existing public payload in `Snapshot.Value` and its
  original timestamp in `Snapshot.Timestamp`. The two timestamps must agree.
  Keep public-node filtering and bounded MTS queries in this callback, propagate
  query failures, and honor cancellation. The build deadline is 30 seconds;
  Go cannot forcibly terminate a callback that ignores its context.
- The core encodes each successful generation once and shares immutable frame
  bytes across writers. A connection holds one active write and at most one
  pending generation; newer generations replace the pending one.
- Failures preserve last-good bytes and their original timestamp. Snapshot age
  uses monotonic elapsed time and a wall-clock high-water mark, so clock rollback
  cannot rejuvenate old data. Duplicate/regressing timestamps are rejected.
  Last-good data is available for at most 30 seconds; no placeholder is emitted.
  `Stats().Generation` exposes age, stale and available state without changing
  the public JSON payload. `Latest()` returns a defensive copy for diagnostics;
  WebSocket writers do not use it.
- Call `Close()` during shutdown before releasing MTS dependencies. It rejects
  new admissions, cancels production, requests close code 1001 and waits for the
  producer, pending handshakes, readers and writers. It is idempotent. Canceling
  only the `Run` parent stops production; the owner must still close the service.
- Callbacks must be concurrency-safe. `Observer` must return quickly and must
  not synchronously call `Close` (cancel the owner context instead). Its events
  contain fixed reason enums, counts and durations, never IPs or raw errors.
  Builder/encoding panic and observer panic return fixed terminal errors for
  the supervisor. Ordinary build failures are recorded and retried.

## Fixed limits

| Resource | Limit |
| --- | --- |
| Connections including pending handshakes | 1024 global, 16 per canonical IP |
| Tracked IP entries | 4096; inactive entries retained up to 5 minutes |
| Handshake token buckets | global burst 64 / 32 per second; IP burst 8 / 1 per second |
| Encoded snapshot | 8 MiB |
| Request headers / inbound message | 16 KiB / 4 KiB |
| Inbound frames | burst 32 / 16 per second per connection |
| Pending control frames | 4 |
| Write / read deadlines | 5 seconds / 75 seconds |
| Server ping cadence | 30 seconds |

Writes are also bounded by the generation's remaining stale window. Client
frames require masking, valid RSV/opcode/length/control fields and valid text
fragmentation. Protocol, message size and frame-rate violations use close codes
1002, 1009 and 1008 respectively. IP cache exhaustion rejects admission rather
than allocating another entry.

## Verification

On 2026-10-08, isolated package verification passed:

```sh
env -u GOROOT go test -race -shuffle=on -count=1 -timeout=2m \
  -coverprofile=/tmp/beat-fanout.cover ./internal/metricsfanout
env -u GOROOT go vet ./internal/metricsfanout/...
env -u GOROOT golangci-lint run ./internal/metricsfanout/...
```

Coverage was **94.4%**, with zero lint issues. Tests cover shared construction,
last-good expiry and rollback, slow writers/coalescing, strict frame parsing,
handshake/IP/rate rejection, real WebSocket interoperability, ping/read timeout
and shutdown accounting. `goimports-reviser` formatting and production function
size/complexity checks passed. Runtime integration requires its own regression
and whole-repository gates.
