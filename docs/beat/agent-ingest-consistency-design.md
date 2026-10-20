# Agent ingestion consistency

Updated: 2026-09-15

Status: approved C04 implementation in the current worktree; release and deployment gates remain part of the integration batch.

## Contract

SQLite owns identity, credentials, inventory and authenticated contact. MTS owns
all metric values, traffic deltas and receipt history. `last_seen` records accepted
contact; `telemetry_at` records the capture time of the latest committed receipt.
A contact-only result is explicit and never claims a transaction across stores.

The coordinated Server/Agent release uses report protocol **2**. Legacy report
envelopes receive `426 unsupported_protocol`; legacy shared credentials, where
still configured for an existing legacy node, do not bypass the v2 envelope.
The envelope contains a canonical UUID `sample_id`, nanosecond `captured_at`,
`report_interval_ms` (1 second–24 hours), endpoint/inventory, a required validated
metric object, and up to 64 custom values. Bodies are limited to 32 KiB; inventory,
metric keys and all numeric values are bounded and validated before admission.
Clock tolerance is 2 minutes ahead; the retry window is 15 minutes behind.

The Agent holds one immutable report in memory, including a copied custom-value
map. Retryable transport, `429` and server errors use jittered exponential delay
from 0.5 seconds to 1 minute, honoring bounded `Retry-After` up to 15 minutes.
The original sample expires after 15 minutes of local elapsed time. A remaining
`Retry-After` also delays the next sample. A success ACK must match protocol,
sample ID and captured time and affirm both contact and metrics acceptance.
Nonretryable responses discard that sample; restart discards the memory-only
pending report. There is no metric spool in SQLite or on disk.

While pending, the runner calls neither system `Collect` nor custom `Sample` and
does not advance the system counter baseline. The independent, bounded custom
collector continues scheduling; cache updates and task revocation cannot mutate
an already copied report. Every actual custom completion has its own sample UUID,
completion time, configured cadence and a hash of the executed definition/revision.
The report's custom_samples map must exactly match custom_metrics keys. The
server validates UUIDs, timestamps, definition hashes, configured cadence and the
shared 32 KiB body budget. The receipt persists this provenance in the same Sync
batch; no extra measurement or tag is introduced. Readers locate the receipt at
the custom scalar's own timestamp, so a newer host-only report cannot refresh it.

A version/cadence change after local capture but before server admission returns
nonretryable 409 custom_source_changed with neither contact nor metrics accepted.
The Agent discards that unaccepted sample and collects again at the next interval;
it does not retry an invalidated payload for 15 minutes. Already admitted immutable
plans retain the approved C04 policy boundary. The live HTTP reconfiguration test
changes both command and cadence and proves prompt recovery with a new execution.
After ACK, the next collection uses the latest cache
and runs immediately if its interval elapsed, with rates spanning real elapsed
time. Network probing and the task channel remain independent.

## Admission, identity and durable commit

1. Report authentication is read-only, including token last-used state.
2. Decode, protocol/clock/value checks and admission precede any persistent
   mutation. Limits are 32 active reports globally, 8 per client and one per node.
   Requests have a 10-second processing deadline and context-aware store waits.
3. Admission acquires the MTS operation read guard and node lock, validates custom
   bindings/persistence policy, checks existing receipts/order and freezes all
   points. This is the policy linearization point. No SQLite contact changes
   occur when preparation, admission, storage availability or ordering fails.
4. An immediate SQLite transaction rechecks the authenticated credential generation
   and node binding, then commits contact/inventory and throttled token last-used.
   A rotated/revoked credential cannot reuse a prepared plan. An older committed
   duplicate refreshes contact without rewinding current inventory.
5. MTS derives traffic from the latest cumulative counter strictly **before** the
   capture nanosecond, then writes numeric fields, accepted custom values, deltas
   and one typed `agent_ingest_receipt` in a single `Write(..., Sync:true)`.
   All points share database `beat`, retention policy and timestamp, hence one
   shard and one framed WAL batch in the pinned MTS version.
6. Only after that durable boundary does the response affirm metrics acceptance.
   An identical committed receipt returns a matching duplicate ACK without another
   numeric write. Conflicting identity/payload/credential or a new out-of-order
   sample is rejected. UUID reuse checks cover the bounded retry window; older
   requests are expired before admission. An older committed duplicate may ACK
   within that window even when a newer sample exists.

The receipt has only the stable node tag; sample ID is a bounded string field,
not a tag. Its six typed fields are sample ID, payload SHA-256, credential
fingerprint, protocol version, report interval and canonical custom_samples JSON.
The bounded JSON contains only actual custom execution UUID, completion timestamp,
configured cadence and definition version per custom name; numeric values remain
in their scalar measurements. It is reserved typed history,
not a selectable numeric catalog metric. Every decode/restore checks that schema.

Prepared plans retain the originally admitted metric policy for at most 15
minutes from server admission time, with at most 128 entries. They can resolve a
known pre-write cancellation without policy or delta drift. They never provide a
success ACK without MTS evidence. Capacity and cancellation release active
admission; abandoned unwritten plans are removed. Unknown write failures require
recovery before any plan can run again. Authorization is rechecked on every new
HTTP attempt; the contact transaction is the credential authorization boundary.

## Uncertain storage is fail-stop

The pinned MTS WAL can leave a partial frame after a short write. Appending a later
successful Sync behind that frame could make an acknowledged sample unreachable
on replay. Beat serializes MTS writes and marks the engine poisoned on an unknown
write failure. It rejects subsequent MTS reads/writes and ACKs, and `Health` makes
`/readyz` return 503. The process stays up for diagnosis; there is no automatic
reopen, file repair or restart loop.

After the operator repairs the underlying storage problem, a supervised restart
opens/replays MTS and flushes recovery to a durable boundary before accepting
traffic. Duplicate ACKs also confirm durability. A complete previously unacked
WAL frame may recover; a partial frame must never become a receipt. If contact
committed before failure, the typed `503` response has `contact_accepted=true`,
`metrics_accepted=false`, and a retryable fixed error code. No raw storage errors,
identifiers, addresses, credentials or metric values enter report error messages.

## Freshness and snapshots

REST and the production `/api/v1/ws/metrics` snapshot share the public catalog
filter and `QueryAgentSnapshot`, which serializes metric/receipt reads against
per-node ingestion. Fields are:

- `telemetry_at`: captured time, or null without a committed receipt;
- `telemetry_age_seconds`: a nonnegative integer, or null;
- `telemetry_state`: `fresh`, `delayed`, `stale`, or `unavailable`.

Fresh means age ≤ max(2 × interval, 30 seconds), delayed means age ≤ max(6 ×
interval, 90 seconds), and older data is stale. Future captures have age zero.
Last good values remain available even beyond one hour; read failure remains an
error, never an empty or zero snapshot. Cards label telemetry independently of
contact state. Resource/traffic rules fail evaluation when telemetry is delayed,
stale, absent, or unreadable; they cannot resolve an alert using fabricated data.

Contact offline grace is max(3 × authenticated report interval, 90 seconds).
Greater-than heartbeat-age rules respect that minimum grace. Nodes without a
receipt use the minimum; unreadable receipt policy is an evaluation failure.
The separate `/metrics/ws` hub currently has no production publisher; its exposed
framing supports extended lengths, but no duplicate publisher was introduced.

## Backup, retention and migration boundaries

C04 consumes no SQLite migration and keeps backup envelope v3. Metric catalog v8
continues governing numeric history; the typed receipt joins the managed export,
restore and retention inventory. Future erasure v22 must remove receipts along
with applicable node/fleet/all-history scopes.

Backup lock order is secret lifecycle guard → MTS exclusive snapshot guard →
SQLite snapshot. The MTS guard is acquired before the SQLite snapshot and remains
held while choosing the server `SnapshotCutoff` and exporting MTS. Export uses an
internal locked method, without reacquiring the operation lock. New reports cannot
commit either contact or metrics during that window. Queries, cleanup, competing
snapshots and report waits honor cancellation. ZIP creation happens after release.

Because accepted captures may lead the server clock, v3 metric export/validation
uses `SnapshotCutoff + MaximumAgentClockSkew` as its event-time upper bound. The
unchanged cutoff denotes the server freeze; the held guard excludes later writes,
including later samples that fall inside that expanded timestamp range. Old v1/v2
archives retain their original event-time cutoff. Overflowing cutoffs are invalid.

## Evidence and remaining release gates

Tests use real temporary SQLite/MTS databases and live HTTP/WebSocket connections.
A test-only link to the pinned MTS filesystem fault controller injects real WAL
write, Sync and half-frame failures. Child processes exit without Close/Flush;
restart verifies that acknowledged receipt/numeric data survives, incomplete
frames do not produce receipts, and no new write passes the poisoned boundary.

Other tests cover ACK/response encoding loss, one pending report, rate baselines,
cache update/revocation isolation, bounded Retry-After across expiration, clocks,
conflicts, ordering, two samples in one second, no duplicate traffic, credential
revocation/rotation, contact rollback, admission/cache bounds and cancellation.
They also exercise typed receipt backup/retention, the exact future-skew restore
boundary, writes blocked during backup, REST/live WS privacy/freshness parity,
long-interval contact grace, and paged task replay beyond 16 queued tasks.

The operator signals use fixed labels: `beat_agent_reports_total`, commit count
and duration, retry age, prepared count/age and recovery-required state. No node,
sample, host or custom-value labels are emitted. Full integration still owns the
fleet load/soak, container, IPv4/IPv6 and coordinated release gates.

## Rollback

Before the first v2 report, restore matching previous Server/Agent/frontend
artifacts. After typed receipts exist, keep a matching reader or restore the
matching pre-release backup; an older binary must not reinterpret receipts.
See the operations runbook for fail-stop recovery. This worktree change does not
restart or deploy the existing service.
