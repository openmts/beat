# Agent ingestion consistency

Updated: 2026-07-30

Status: reviewed design; implementation requires explicit approval

## Goal and current evidence

Make an authenticated Agent report retryable and observable without attempting
a transaction across SQLite and MTS and without storing metric values in
SQLite.

The deployed handler first commits heartbeat, online state, host, inventory,
and `last_seen` to SQLite, then writes metric samples to MTS. If MTS fails, the
request returns `500` after SQLite has changed. The node can therefore appear
online while its telemetry is stale, with no explicit freshness field.

The Agent collects a new sample on every interval and drops a failed report. It
does not preserve a stable sample timestamp or ID for retry. Rate metrics for a
failed interval are lost, while cumulative network counters are later converted
to MTS traffic deltas. MTS writes WAL before applying the in-memory batch, so an
error can also be ambiguous: a retry may follow a write that becomes visible
after recovery. The current server-generated second timestamp and inclusive
"latest counter" lookup do not make such a retry deterministic.

Read-side failure handling is also incomplete. REST and traffic reports return
MTS query errors, but resource alert evaluation silently skips a failed metric,
and the current MTS wrapper accepts the engine's empty successful query after
the store has been closed. Both states can masquerade as missing telemetry even
though the reviewed contract requires an explicit storage failure.

## Invariants

1. SQLite remains authoritative for node identity, authorization, inventory,
   contact state, and policy. MTS remains authoritative for every Agent metric,
   traffic delta, sample receipt, and telemetry timestamp.
2. `last_seen` means last authenticated Agent contact accepted by SQLite. It
   never means that the corresponding metric batch reached MTS.
3. `telemetry_at` means the captured time of the latest committed MTS receipt.
   Public/admin views and alert evaluation distinguish contact freshness from
   telemetry freshness.
4. The Server never claims cross-store atomicity. A report may have heartbeat
   accepted and telemetry retryable, but this state is explicit, observable,
   and idempotent.
5. Capacity admission occurs before either store is mutated. A `429` or
   pre-admission `503` guarantees no heartbeat, inventory, or metric write.
6. A retried sample has the same ID, captured time, inventory, counters, rates,
   and values. Reprocessing it cannot double traffic or create a second logical
   chart sample.
7. No retry spool containing numeric metrics is written to SQLite or Server
   files. The Agent holds at most one pending sample in memory.
8. MTS errors propagate as storage errors. They are never converted to empty
   charts, zero values, a healthy rule evaluation, or a successful report.

## Agent protocol

The schema-free `agent-ingest-consistency` batch replaces the report envelope
with a versioned sample:

- `protocol_version`;
- random `sample_id` generated once per collection;
- nanosecond `captured_at` from the Agent;
- node endpoint/inventory fields;
- the existing validated metric object.

The Agent serializes reporting per node and retains one immutable pending
sample until it receives a matching acknowledgement. Retryable network,
`429`, `503`, and `5xx` failures use bounded exponential backoff with jitter and
honor `Retry-After`. While a sample is pending, the Agent does not advance the
collector baseline or replace it with a newer sample. After acknowledgement it
collects immediately if an interval was missed, so rate calculations span the
actual elapsed collection period instead of silently discarding intervals.

The pending sample is memory-only and disappears on Agent restart. A sample too
far in the future, older than the bounded retry window, malformed, or from an
unsupported protocol receives a non-retryable typed response. Clock-skew and
protocol errors are logged without metric values, tokens, node IDs, hostnames,
or addresses.

## Server write contract

After authentication, body validation, clock/retry-window validation, and
ingress admission, the Server:

1. updates SQLite contact/inventory state in one immediate transaction;
2. acquires the per-node MTS ingestion lock;
3. derives traffic deltas using only the latest cumulative counter strictly
   before `captured_at`;
4. writes every metric, both traffic deltas, and one
   `agent_ingest_receipt` point in a single MTS batch at nanosecond precision;
5. returns an acknowledgement containing the same `sample_id`, `captured_at`,
   contact time, `metrics_accepted=true`, and protocol version.

The receipt uses only the stable node tag and bounded typed fields including
the sample ID and protocol version. It is not tagged by sample ID and therefore
does not create unbounded series cardinality. It joins the managed measurement
inventory used by retention, backup/export, restore, metric catalog, and metric
erasure.

All values for one retry are deterministic. MTS last-write-wins deduplication at
the same node, measurement, field, and nanosecond replaces an ambiguous prior
attempt rather than adding a second logical point. Strictly-before counter
lookup recomputes the same traffic delta on every retry instead of replacing a
correct delta with zero or counting it twice.

The first implementation uses a durability mode whose acknowledgement means
the MTS batch has reached its configured durable WAL boundary. The load gate
must prove that boundary at the target Agent count; it may use a reviewed MTS
group-commit capability, but it may not acknowledge a Server-only volatile
queue or spill metric payloads into SQLite.

If SQLite succeeds and MTS fails, the response is a typed retryable `503` with
`contact_accepted=true` and `metrics_accepted=false`. The Agent retries the same
sample. If the response is lost after both stores succeed, the same retry is
idempotent. If SQLite fails, MTS is not attempted. A response encoding or
connection failure after commit is resolved by the stable sample ID, never by
inventing a new timestamp.

## Freshness and alert semantics

Latest MTS queries return values plus the latest receipt timestamp. Node API
models expose:

- contact state and `last_seen` from SQLite;
- `telemetry_at`, telemetry age, and a fixed `fresh`, `delayed`, `stale`, or
  `unavailable` state derived from MTS and the configured report interval.

A node can correctly be online with delayed metrics. Cards keep the last good
values, label their age, and never substitute zero. WebSocket and REST snapshots
carry the same freshness contract. Resource and traffic alert rules treat MTS
query failure as evaluation failure; availability rules continue to use
authenticated contact age. Repeated telemetry failure is observable separately
from node offline state.

The runtime-resilience batch supplies the explicit MTS lifecycle guard and
worker failure accounting. This ingestion batch supplies receipt freshness and
the alert semantics. Metric-catalog `v8` supplies the typed history/current
query error envelope. None may weaken the shared no-empty-masquerade invariant.

## Coordination and storage

This batch consumes no SQLite migration and does not change the backup archive
envelope. It adds the receipt measurement to the current fixed managed list;
metric catalog migration `v8` later registers it, and metric erasure `v17`
must remove it for applicable node/fleet/all-history scopes.

It depends on deployed per-node identity and MTS storage, coordinates with
`ingress-governance` for pre-mutation admission and retry status codes, and
coordinates with `runtime-resilience` for shared fresh/stale snapshots and
truthful storage readiness. It must deploy before `v17` erasure and before any
Agent rollout policy treats the new protocol as mandatory.

The project does not require legacy compatibility in the PoC phase. Server,
Agent, protocol range, fixed measurement list, backup/restore tests, and
frontend freshness handling therefore ship as one coordinated release; an old
Agent is rejected explicitly rather than silently accepted under weaker retry
semantics.

## Observability and operations

Structured logs and Prometheus metrics expose reports by outcome, contact-only
partial accepts, retryable failures, retry age, duplicate receipts, out-of-order
samples, MTS commit duration, telemetry age buckets, and stale nodes. They do
not expose sample IDs, node IDs, names, addresses, tokens, counters, metric
values, inventory text, or raw MTS errors.

Readiness reports MTS unavailability immediately and ingestion degradation when
durable commits repeatedly fail or the oldest retry age exceeds policy. A
contact-only accept does not make the MTS check healthy. The operations runbook
uses receipt age and MTS health to distinguish Agent connectivity, ingestion,
query, and browser-fanout incidents.

## Tests and acceptance

Tests cover:

- exact retry of every metric and both traffic deltas after failures before
  WAL, after WAL, after MTS application, after SQLite commit, during response
  encoding, and after response loss;
- duplicate sample ID/time, conflicting payload, out-of-order samples, clock
  skew, Agent restart, Server restart, MTS restart/replay, counter reset, and
  multiple samples in one wall-clock second;
- one pending Agent sample, backoff/jitter, `Retry-After`, cancellation, no
  collector-baseline advancement on failure, and immediate catch-up collection;
- pre-admission rejection proving zero SQLite/MTS mutation, per-node
  serialization, global concurrency, shutdown, backup, retention, restore, and
  erasure operation-lock interaction;
- receipt inclusion in managed measurements, retention, logical backup/restore,
  catalog registration, node erasure, fleet erasure, and all-history erasure;
- online-with-stale, offline, unavailable, last-good WebSocket/REST parity,
  alert evaluation failure, and no zero/empty-data masquerade;
- all numeric Agent samples and deltas existing only in MTS;
- at least 90 percent backend statement and frontend line coverage plus race,
  shuffle, `go vet`, `goimports-reviser`, `golangci-lint`, vulnerability,
  frontend lint/test/build/audit, load/soak, container, and IPv4/IPv6 gates.

Acceptance requires deterministic retry without duplicate traffic, explicit
contact-versus-telemetry freshness, no SQLite metric values or disk spool, and
fault injection proving that every acknowledged sample is queryable after the
configured MTS durability boundary and restart.

## Approval and rollback

Implementation requires explicit approval for the Agent protocol change,
sample IDs and captured timestamps, one-pending-sample retry behavior,
nanosecond MTS writes, receipt measurement, strict-before traffic derivation,
durable acknowledgement boundary, typed partial-success response, freshness
API fields, alert semantics, coordinated Server/Agent deployment, and rejection
of legacy reports.

Before the first new-protocol report, rollback may restore the previous Server,
Agent, frontend, and fixed measurement list. After receipt rows exist, an older
binary may leave them unreachable but must not reinterpret them; rollback uses
the matching pre-release backup or retains the new reader until explicit
`v17` erasure is available.
