# Ingress resource governance

Updated: 2026-07-30

Status: reviewed design; implementation requires explicit approval

## Goal and current evidence

Beat must remain available when anonymous browsers, authenticated Agents, or
administrators create concurrent or oversized work. Existing controls are
useful but route-specific: the Server has read/write timeouts, login failures
eventually lock an IP and username, authentication bodies are limited to 16
KiB, network-result bodies to 256 KiB, and backup uploads have their own archive
limit. They do not form a complete admission or cost policy.

The confirmed gaps are:

- concurrent login requests pass `Allowed` before any failure is recorded, so
  one source can start many Argon2id verifications at approximately 64 MiB each;
- public node history accepts an arbitrary comma-separated metric count and any
  RFC3339 range, then runs one unbounded MTS query and retains every point for
  each metric;
- public Fleet list/detail and network-quality summary rebuild MTS-derived data
  for every request without shared cache, singleflight, rate, or concurrency
  limits;
- a valid Agent token can submit unbounded node-report JSON and repeat reports
  without a per-node or global ingestion budget;
- most management JSON uses an unbounded decoder, accepts trailing objects and
  does not consistently validate request content type;
- node-detail and network polling can overlap, continue while the document is
  hidden, and do not cancel obsolete requests;
- common JSON response helpers deliberately ignore encoder/write errors.

This schema-free batch supplies one enforceable capacity model while preserving
public no-login reads, authenticated administration, and MTS-only time series.

## Security and storage invariants

1. Public monitoring remains available without login. Admission control never
   becomes an authentication mechanism or a private-site gate.
2. Administration remains session/authorization protected. A trusted proxy or
   allowed Origin does not grant more budget or permission by itself.
3. Agent, probe, derived, and other numeric samples remain exclusively in MTS.
   SQLite stores no request counters, rate-limit samples, query points, or
   transient admission state.
4. Client identity uses the deployed canonical trusted-proxy resolver. Forged
   forwarding headers cannot create new limit keys.
5. Raw IPs, usernames, node IDs, session IDs, paths, queries, Origins, and error
   strings never become Prometheus labels.
6. Limits reject work before expensive password, SQLite, MTS, notification, or
   SSH operations. Rejection does not enqueue unbounded delayed work.
7. This batch consumes no SQLite migration and keeps backup format `v4`.
8. WebSocket connection/fanout budgets remain owned by
   [`runtime-resilience-design.md`](./runtime-resilience-design.md); this batch
   owns HTTP, authentication, history-query, and Agent-ingestion admission.

## Capacity configuration

The Server constructs one immutable, validated capacity configuration before
opening listeners. Code-owned safe defaults are published in the runbook; a
small bounded set of environment overrides supports measured deployment sizes.
Startup rejects zero, negative, overflowing, internally inconsistent, or
unsafe-high values instead of silently disabling protection.

The initial envelope includes:

- explicit `MaxHeaderBytes` of 32 KiB and request-target maximum of 8 KiB;
- 16 KiB authentication JSON, 64 KiB ordinary/admin JSON, and 64 KiB node
  reports;
- the existing 256 KiB network-result limit and independent backup/diagnostic
  limits;
- global and per-identity token-bucket rates for route classes;
- global and per-identity in-flight limits for expensive route classes;
- maximum history series, duration, points per series, scanned-cost score, and
  encoded response bytes;
- zero admission wait for every ingress class; Agents retry with bounded jitter
  instead of occupying request goroutines in an internal queue.

The authenticated Operations status exposes effective numeric limits and
saturation state without listing client identities. Environment changes require
restart; the browser cannot edit them.

## Admission controller

Routes declare a fixed class rather than constructing ad hoc limiters:

- `public_static`: site/group metadata and static shell;
- `public_snapshot`: Fleet/node/network-quality current views;
- `public_history`: metric and network history;
- `auth_kdf`: login, bootstrap password creation, reauthentication, and
  password change;
- `agent_read`: assignment fetches;
- `agent_write`: node reports and network-result batches;
- `admin_read`, `admin_write`, and `admin_expensive`;
- upload/diagnostic classes owned by their feature designs.

Admission is layered: one global bucket, one global in-flight semaphore, then a
canonical identity bucket and in-flight lease. Identity is client IP for public
and pre-auth requests, authenticated node ID for Agent work, and administrator
user/session for protected work. Agent routes retain a coarse per-IP ceiling so
one compromised NAT/source cannot consume the whole global budget, while the
per-node key avoids penalizing normal fleets behind NAT.

Limiter state is bounded and TTL-based. Active lockouts or leases are never
arbitrarily evicted to make room for attacker-created keys. Expired entries are
removed first; if the table is full, unseen identities fail closed under the
global class limit until capacity returns. All lease releases are deferred at
the request boundary and execute on success, error, panic, or cancellation.

Rejected rate work returns `429` with a bounded integer `Retry-After`; global
dependency saturation returns `503`. Oversized body/request target and invalid
media return `413`, `414`, or `415`. Responses use one generic message and do
not disclose bucket capacity, account existence, node existence, or global
load. CORS and same-origin behavior remain route-specific and unchanged.

## Authentication work factor protection

Every password-hash operation acquires the `auth_kdf` admission lease before
database lookup or Argon2 work. The default allows one in-flight KDF per client
IP and a small global count derived from the documented memory envelope. A
burst of concurrent requests from one IP therefore cannot reserve multiples of
the 64 MiB work factor before failure accounting catches up.

Login uses global, IP, and normalized-username failure keys. Failure reservation
and release are atomic with the in-flight lease. A success clears only the
applicable identity state. A full limiter table never deletes an active victim
lockout. Unknown, disabled, wrong-password, and wrong-TOTP outcomes retain
constant password work and uniform public errors.

Reauthentication and password changes use the same KDF gate plus per-session
and per-user failure limits. Bootstrap has a separate low-rate class, remains
same-origin, verifies the bootstrap authority before hashing, and is disabled
after setup. The durable threshold/security-event behavior from migration
`v12` composes with these in-memory availability controls.

Password hash parsing also enforces supported upper bounds for memory,
iterations, parallelism, salt, and output length before allocation. A corrupt or
restored hash cannot request unbounded Argon2 resources. Unsupported parameters
fail closed and surface an owner recovery condition without echoing the hash.

## Request body and JSON contract

Body limits are attached to routes before authentication or decoding, with a
smaller resolver limit retained for legacy Agent migration. JSON endpoints
require `application/json` with an optional UTF-8 charset, decode exactly one
value, reject trailing non-whitespace, and return stable errors. Management and
authentication payloads reject unknown fields; versioned Agent protocols may
allow explicitly documented forward fields only within the body limit.

Field-level validation remains mandatory after decoding. Node report inventory
strings gain byte/rune limits; sort arrays, target lists, tags, URLs, commands,
notification configuration, and every repeated field have count and element
limits. A body cap is not a substitute for semantic validation.

Handlers use one bounded decode helper that distinguishes malformed JSON,
oversize, unsupported media, cancellation, and internal failure without
returning decoder internals. Tests enumerate every write route so a new handler
cannot bypass a declared body policy.

## Public snapshot and history budgets

`public_snapshot` routes do not independently rebuild identical current data.
Fleet list/detail reads consume the immutable last-good snapshot created by the
runtime-resilience producer, apply server-side visibility before filtering, and
reuse its encoded generation/ETag. Network-quality current views use a bounded
singleflight cache with one refresh owner, fixed freshness, stale annotation,
and no request-triggered fanout of concurrent MTS work.

Cache keys include visibility, effective public settings, route parameters, and
generation. Private/admin content never shares a public cache entry. Conditional
GET returns `304` without touching MTS. Cache failure keeps a bounded last-good
response only within its stale window and never labels it current.

Until metric catalog `v8` generalizes the query model, node history accepts only
one to eight known public metrics, `from < to`, a maximum 31-day range, and a
fixed maximum of 600 returned points per series. Unknown/duplicate metrics,
future-only ranges, excessive request targets, and incompatible combinations
return `400` before MTS access.

The Server selects deterministic metric-specific aggregation and a bucket width
from the requested range so MTS returns at most the point budget; fetching all
raw rows and downsampling in Go is forbidden. A global MTS-query semaphore,
per-IP history lease, deadline, scanned-cost ceiling, and encoded response cap
apply to both current and future catalog endpoints. The `v8` contract later
adds catalog units/tags/aggregations without weakening these ingress controls.

Network history retains its existing 31-day and 600-point contract but enters
the same admission class. Public responses never reveal private nodes/tasks and
do not accept arbitrary tag expressions or measurement names.

## Agent ingestion governance

Agent authentication still resolves one concrete node before any write. The
node-report body is capped at 64 KiB, inventory strings are bounded, metrics are
finite/non-negative and root-filesystem-only, and the authenticated node ID is
the only MTS tag identity.

Each node has at most one active report write and a token-bucket rate compatible
with the supported minimum report interval. A global Agent-write semaphore
protects SQLite/MTS, while a coarse source ceiling contains a compromised
network. Excess reports are rejected before heartbeat or MTS mutation; they are
not buffered in memory. Accepted work has a context deadline and releases every
lease on cancellation.

Per-node serialization preserves traffic-delta ordering. The node-lock registry
has lifecycle cleanup when a node is tombstoned/deleted so node churn cannot
grow it forever. Network-result batches retain their 64-result/body bounds and
also receive per-node/global admission. Duplicate or replay semantics remain
owned by the Agent protocol and metric-erasure designs, not by client IP.

## Response and frontend behavior

Small JSON responses encode into a bounded buffer before headers are committed.
Shared snapshots reuse pre-encoded immutable bytes. Bounded history responses
either encode successfully or return a generic error; response write/flush
errors are always captured, classified, and observed. Handlers never ignore an
encoder error and never retry a partial response.

Frontend polling for node history and network views is visibility-aware, keeps
one request in flight, cancels obsolete generations with `AbortController`, and
uses capped jittered retry. A range/node change aborts the prior request. `429`
honors bounded `Retry-After`; `503` uses normal backoff. Existing data remains
visible with a stale/error indicator instead of being blanked or repeatedly
re-requested. Public pages never respond to rejection by attempting login.

## Observability and operations

Prometheus adds bounded series for admitted/rejected requests, in-flight work,
limiter entries, KDF work, body/target rejection, snapshot cache outcomes,
history query duration/scanned/returned points, response bytes/errors, and
Agent report outcomes. Labels are fixed class/result/reason enums only.

Transient client overload does not fail readiness and must not cause a load
balancer to remove every instance. Readiness fails only if admission machinery
is uninitialized/corrupt or required snapshot/query dependencies are unhealthy.
Sustained global saturation, rejection ratio, KDF queue pressure, MTS query
deadline, and Agent rejection alerts are documented in the runbook.

The authenticated Operations view adds a read-only `Ingress` tab using shadcn
`Table`, `Badge`, `Progress`, and `Alert`. It shows fixed route classes,
effective limits, current/global utilization, rejection rate, and cache state.
It exposes no identities, arbitrary reset controls, IP allowlists, or remote
tuning form.

## Tests and acceptance

Backend tests cover route-policy enumeration, header/target/media/body/trailing
limits, unknown fields, every release path, proxy identity, TTL/full-table
behavior, deterministic no-eviction lockouts, and generic error responses.

Authentication tests launch synchronized same-IP and distributed bursts and
prove KDF concurrency/memory work never exceeds its gate, user enumeration does
not change responses, reauthentication is bounded, stored hash parameters are
upper-bounded, and cancellation releases leases.

Public tests prove one shared snapshot/cache build serves many callers, ETags
avoid MTS work, private visibility cannot cross cache keys, one to eight known
metrics and 31 days/600 points are enforced, aggregation occurs in MTS, cost and
response limits reject before excessive allocation, and slow/disconnected
writers are observed.

Agent tests cover 64 KiB reports, inventory limits, per-node/global/source
bursts, one active write per node, no mutation after rejection, traffic ordering,
deadline/cancellation, lock cleanup, many-node NAT behavior, and MTS-only values.
Frontend tests cover one in-flight poll, abort on range/node change, hidden-tab
pause/resume, `Retry-After`, backoff, preserved stale data, and no login redirect.

Load/soak acceptance measures anonymous snapshot/history traffic, synchronized
login bursts, expected Agent fleets, slow clients, MTS latency, memory ceiling,
and recovery after saturation over IPv4 and IPv6. Completion also requires 90
percent coverage, race/shuffle, `go vet`, `goimports-reviser`, `golangci-lint`,
vulnerability/module verification, frontend audit/lint/build, and browser smoke.

## Delivery, rollback, and approval

This is schema-free batch `ingress-governance` in
[`commercial-parity-implementation-sequence.md`](./commercial-parity-implementation-sequence.md).
Pre-mutation Agent admission composes with, but does not define, stable sample
identity, retry acknowledgement, traffic-delta idempotency, or telemetry
freshness. Those semantics are owned by
[`agent-ingest-consistency-design.md`](./agent-ingest-consistency-design.md).
It depends on deployed trusted-proxy identity, composes with
`runtime-resilience`, and is generalized by metric catalog `v8` and durable
login-security migration `v12`. It consumes no SQLite migration, leaves the
next migration at `v20`, and keeps backup format `v4`.

Privileged route registration must also compose one admission class with one
explicit audit policy from
[`administrative-mutation-consistency-design.md`](./administrative-mutation-consistency-design.md).
Admission rejection happens before mutation or external admission and uses the
same request context, but it does not fabricate a domain success/failure audit
event or permit a generic route-policy fallback.

Rollback restores the previous binary/frontend and creates no persistent data
conversion. It also removes the new rejection/status metrics, so rollback
alerts must be version-aware.

Implementation requires explicit approval because it changes accepted request
sizes/media, public history validation/aggregation, overload status codes,
authentication and Agent admission, response headers, polling lifecycle, Server
configuration, and operations output. This design alone authorizes none of
those changes.
