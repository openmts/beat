# Advanced network diagnostics

Updated: 2026-07-30

Status: reviewed design; implementation requires explicit approval

## Goal and product boundary

Beat will add controlled one-off path and bandwidth diagnostics from managed
Agents without turning an Agent token into an arbitrary network attack relay.
The first revision provides:

- MTR-style path diagnostics with per-hop latency and loss;
- iperf3 client tests to pre-approved endpoints with bounded direction,
  protocol, duration, bitrate, and concurrency;
- durable job state, cancellation, streamed progress, retained results, audit,
  and operator-visible capability/error states.

This is an authenticated administrator operation, never a public feature. It is
separate from scheduled ICMP/TCP/HTTP quality tasks: quality tasks continuously
measure known services, while diagnostics are explicit, high-cost incident
investigation jobs.

## Current Beat evidence and gap

Beat already has:

- public/admin scheduled ICMP, TCP, and HTTP probes;
- per-node Agent identity and revocation;
- an authenticated SSH terminal and bounded synchronous SSH batch command;
- reviewed durable Agent-operation lease, cancellation, policy, encryption, and
  audit semantics in `remote-operations-design.md`;
- MTS-only storage for every Agent-reported numeric sample.

It does not have path discovery, bandwidth measurement, approved diagnostic
targets, diagnostic capability advertisement, durable diagnostic jobs, or a
cancelable Agent protocol.

## Competitor evidence and correction

DStatus commit `4afc9e43c9df28096352c05ae924fcadbc830a2f` contains full-build
routes for iperf3 and MTR behind its global API-key middleware:

- route registration and API-key middleware:
  <https://github.com/fev125/dstatus/blob/4afc9e43c9df28096352c05ae924fcadbc830a2f/neko-status/main.go>
- iperf3 HTTP/WebSocket handlers:
  <https://github.com/fev125/dstatus/blob/4afc9e43c9df28096352c05ae924fcadbc830a2f/neko-status/iperf3.go>
- iperf3 process execution/parser:
  <https://github.com/fev125/dstatus/blob/4afc9e43c9df28096352c05ae924fcadbc830a2f/neko-status/iperf3/iperf3.go>
- MTR implementation:
  <https://github.com/fev125/dstatus/blob/4afc9e43c9df28096352c05ae924fcadbc830a2f/neko-status/mtr/mtr.go>
- simple build that replaces both capabilities with successful disabled
  responses:
  <https://github.com/fev125/dstatus/blob/4afc9e43c9df28096352c05ae924fcadbc830a2f/neko-status/main_simple.go>

The full implementation accepts caller-selected destinations and parameters,
permits the API key in a query string, accepts every WebSocket origin, does not
use process contexts or reliable cancellation, ignores multiple parse/process
errors, and has weak bounds on duration, parallelism, output, and target class.
The simple build returns HTTP success while saying the features are disabled.
Beat treats these as evidence that the workflow is useful, not as a safe or
reliably shipped contract to copy.

Komari Server commit `4077201f098774511eaf504f220c5f6be009346b` declares v2
protocol names and detailed trace structures for `networkTest.iperf3`,
`networkTest.nextTrace`, and mesh trace, but the inspected Server tree does not
provide a complete end-to-end administrator workflow:

- protocol definitions:
  <https://github.com/komari-monitor/komari/blob/4077201f098774511eaf504f220c5f6be009346b/protocol/v2/networktest.go>

Beat therefore requires deployed workflow evidence, not protocol names alone,
before claiming parity.

## Security and correctness invariants

1. Diagnostics are disabled by default in both Server policy and Agent-local
   `0600` configuration. Both must allow the exact job kind and target.
2. An administrator cannot enter an arbitrary hostname or IP when launching a
   job. The destination is an owner-approved target record or another eligible
   managed node endpoint.
3. Target approval resolves and validates every IPv4/IPv6 address, port,
   protocol, direction, and purpose. Execution repeats validation after DNS
   resolution to prevent rebinding.
4. Loopback, unspecified, multicast, link-local, documentation, benchmark,
   carrier-grade NAT, cloud metadata, and private ranges are denied by default.
   Narrow private targets require an explicit owner and matching Agent-local
   allowlist.
5. The Agent executes only approved absolute binaries with fixed arguments and
   no shell, environment inheritance, working-directory input, or arbitrary
   command field.
6. Every process is owned by a context deadline and process group/job object;
   cancellation and Agent shutdown terminate the full child tree and reap it.
7. Duration, bitrate, parallel streams, packet count, hop count, output bytes,
   active jobs, daily bytes, and target concurrency have hard implementation
   maxima that corrupted database values cannot exceed.
8. Reverse iperf3, UDP, private destinations, and node-to-node mesh expansion
   are separate policy capabilities, all disabled by default.
9. Only the target node's active per-node Agent credential can lease, start,
   stream, or complete its diagnostic attempt.
10. Every Agent-reported numeric diagnostic value is written only to MTS.
    SQLite stores job/policy/audit state and bounded non-numeric metadata.
11. Diagnostic targets, path hops, job state, and numeric results are never
    exposed through public APIs, public WebSockets, node cards, or public MTS
    queries.
12. A job never reports simulated or fixed success. Missing binaries,
    permissions, unsupported platforms, parse failures, and partial results are
    explicit failure/partial states.

## Capability and dependency model

Diagnostics reuse the reviewed Agent-operations lease/start/cancel protocol.
They do not add a second task poller or reuse the synchronous SSH endpoint.

Required dependencies are:

1. per-node Agent credentials and revocation;
2. secure Agent enrollment for new installations;
3. the durable remote-operations queue, at-most-once start, cancellation, and
   local policy intersection;
4. the metric catalog before new diagnostic measurements are registered;
5. signed Agent rollout before production enables new diagnostic binaries or
   capabilities fleet-wide.

The Agent advertises a bounded capability document:

- supported job kinds and schema version;
- binary name, validated absolute path, version, and JSON capability;
- supported IP families, MTR protocols, and iperf3 protocol/direction features;
- effective Agent-local maxima and approved target-policy digest;
- required privilege state and last self-test result.

The Server can reduce these capabilities but cannot expand them. A stale,
missing, unsupported, or policy-mismatched capability prevents job creation.

## Diagnostic target model

Owners manage named target records after recent reauthentication. A target has:

- UUID and human-readable name/purpose;
- exact DNS name or IP literal and fixed port where applicable;
- allowed job kinds, IP families, protocols, and directions;
- public/private address policy and optional exact CIDR/address set;
- maximum bitrate, duration, parallel streams, concurrent sources, and daily
  transferred-byte budget;
- optional TLS/name metadata for future diagnostics, disabled in this revision;
- enabled state, expiry, creator, revision, and timestamps.

DNS targets pin the resolved address set for a short owner-reviewed window.
Every Agent resolves independently at execution and must find an address inside
the approved set and its local allowlist. A changed set pauses the target for
owner review rather than silently testing a new host.

Managed-node targets use a separately approved diagnostic endpoint advertised
by the destination Agent. Node display names appear in the UI; node IDs are only
submitted values. Agent report IPs are not automatically trusted as reachable
diagnostic targets.

## MTR-style path diagnostics

Beat uses the maintained system `mtr` binary in JSON report mode on supported
platforms, invoked by absolute path with fixed arguments. There is no shell and
no parsing of localized human output. An unsupported or incompatible version
advertises no MTR capability.

Initial limits are:

- one destination;
- IPv4 or IPv6 selected explicitly/automatically within target policy;
- ICMP by default; TCP/UDP only when both policies permit;
- 10 probes per hop default, range 3-20;
- 32 hops default, hard maximum 64;
- one-second per-probe timeout, bounded total at 30 seconds;
- reverse DNS disabled by default;
- raw text output disabled and never retained.

The Agent parses JSON into hop index, reached state, sent/received count, loss,
last/average/best/worst latency, and a bounded address identity. It validates
finite ranges, hop uniqueness, count consistency, maximum hop count, and target
identity before reporting.

Hop addresses are sensitive path metadata. SQLite stores an encrypted bounded
hop identity document keyed by job and hop index for administrator detail only.
The notification, audit, logs, and metrics contain no hop IP or hostname.
Numeric hop values are stored in MTS.

## iperf3 bandwidth diagnostics

Beat invokes a supported `iperf3` binary with JSON streaming/final output,
absolute path, fixed argument construction, cleared proxy environment, and a
private working directory. The target must be an approved iperf3 server; Beat
does not start a public iperf3 listener on monitored nodes in the first version.

Defaults and hard maxima are:

- TCP forward, one stream, 10 seconds, and 100 Mbps requested pacing;
- duration range 3-30 seconds, hard maximum 60 seconds;
- parallel streams default 1, hard maximum 4;
- configured bitrate hard maximum 1 Gbps and never above target/Agent policy;
- UDP disabled by default; when enabled, explicit bitrate is mandatory and
  limited to 100 Mbps by default;
- reverse direction disabled by default;
- one active iperf3 job per Agent and target, four globally by default;
- daily transferred-byte budget enforced before lease and reconciled from the
  final actual-byte result.

TCP pacing is not treated as a perfect network shaper. Production policy must
use dedicated controlled endpoints and conservative limits; operators are
warned that an iperf3 test intentionally consumes bandwidth. Reverse direction
is allowed only when the approved server and source Agent both opt in.

The parser accepts only the expected iperf3 JSON schema/version and finite
values. It captures per-interval bytes/bitrate, final sender/receiver bitrate,
retransmits, congestion window when present, and UDP jitter/loss when enabled.
Stderr is bounded and mapped to fixed error codes, not retained raw.

## Durable job protocol and state

Creating a diagnostic resolves a named source node and named approved target,
then stores an immutable target/policy/capability snapshot. Owner/admin creation
requires recent reauthentication and an explicit risk confirmation for iperf3.

States are:

```text
queued -> leased -> running -> succeeded | partial | failed | timed_out
                    |       -> cancelled
                    +-------> interrupted | unknown
```

The Agent validates the lease, writes a private durable journal, obtains the
Server's at-most-once start acknowledgement, then launches exactly once.
Progress frames contain sequence numbers and bounded structured numeric batches.
The Server acknowledges the highest durable sequence after MTS publication.
Reconnect resumes after that sequence; duplicates are idempotent.

Cancellation is available immediately to the creating admin or an owner and
does not require a second reauthentication prompt. It cancels context, kills
the process group/job object, flushes any already validated partial numeric
samples to MTS, and completes with `cancelled` or `unknown` if termination
cannot be proven.

An attempt that reached `running` is never automatically executed again after
a lost completion. An explicit retry creates a new attempt and job-run ID.

## Storage boundary

Advanced diagnostics use canonical SQLite migration `v14` after secure
enrollment `v4`, remote operations `v5`, metric catalog `v8`, and Agent rollout
`v10`. Diagnostic measurements extend the catalog-governed backup format `v4`
without creating a new archive envelope. The assignment is maintained in
[`commercial-parity-implementation-sequence.md`](./commercial-parity-implementation-sequence.md).
Encrypted diagnostic metadata registers its AAD, size, rotation enumerator, and
backup validator with the deployed `v5`
[`application-secret-lifecycle-design.md`](./application-secret-lifecycle-design.md)
and does not add another key or envelope.

SQLite stores application state only:

- diagnostic targets and immutable revisions;
- Server policies and Agent capability snapshots;
- jobs, attempts, leases, state transitions, actor, confirmation, limits,
  target/node name snapshots, timestamps, and fixed error codes;
- encrypted bounded MTR hop identities and result schema metadata;
- cancellation, acknowledgement cursor, audit links, retention, and daily
  budget reservations/actuals.

MTS registers bounded measurements such as:

- `diagnostic_mtr_latency_ms` with `job`, `node`, `hop`, and `stat` tags;
- `diagnostic_mtr_loss_percent` and packet counts with the same bounded tags;
- `diagnostic_iperf_bytes_per_second` with `job`, `node`, `direction`,
  `protocol`, and `role` tags;
- `diagnostic_iperf_bytes`, retransmits, jitter, and loss when applicable.

No hostname, IP, target name, user input, error text, or Agent label becomes an
MTS tag. Job and node IDs are bounded existing identities; hop is 1-64 and all
other tags are fixed enums. MTS writes are batch-atomic per progress sequence.
SQLite never contains latency, loss, throughput, jitter, byte, or retransmit
values.

Result APIs join authorized SQLite metadata with MTS queries. An MTS failure is
an explicit storage error, not an empty successful result. Deleting a job first
deletes its registered MTS series by job tag, then removes encrypted metadata;
a failed MTS delete leaves the SQLite job addressable for retry.

## API and authorization

Authenticated administrator APIs provide target management, capability views,
job create/list/detail/cancel/retry, progress stream, and result queries. There
are no public routes.

Owners create/change/delete targets and policies. Owners and admins may run an
already-approved job when their role and recent reauthentication permit it.
Private-range targets, UDP, reverse direction, policy expansion, target address
changes, and retrying an unknown iperf3 attempt are owner-only.

Agent endpoints reuse per-node operation lease/start/progress/complete/cancel
routes and validate the attempt kind, node context, nonce, sequence, capability
digest, target revision, and policy snapshot on every call.

Request bodies have strict limits and reject unknown fields. Every response
includes human node/target labels alongside IDs. Select triggers display names
and never raw IDs.

## Administration UI

Diagnostics live under `/admin/network` as a work-focused view beside scheduled
quality tasks. The audience is an operator investigating one incident; the
page's single job is to launch a constrained test and understand its result.

The visual direction stays with shadcn `base-nova`, semantic status colors,
Geist, tabular metrics, and the existing admin sidebar. The signature element is
a single left-to-right job timeline from authorization through Agent start,
measurement, MTS persistence, and completion. It encodes real state and is not
decorative.

The create `Dialog` uses `FieldGroup` and `Field`, name-rendering `Select` or
`Command` controls for source node and approved target, `ToggleGroup` for
diagnostic kind, bounded inputs/sliders for safe numeric choices, and a required
iperf3 bandwidth-impact confirmation. The Dialog always has a title and never
accepts a free-form destination.

History uses `Table`, `Badge`, filters, and stable background updates. Detail
uses unframed summary bands plus shadcn `Chart` for MTS series:

- MTR: hop table and latency/loss chart with privacy-controlled addresses;
- iperf3: interval throughput chart and final sender/receiver summary;
- fixed unit groups, dynamic human-readable axes, explicit effective range,
  partial/cancelled state, and no raw console output.

Running jobs update without a full-page reload and preserve scroll/focus. The
cancel icon has a tooltip and remains reachable by keyboard. Mobile uses one
column and moves secondary actions into a menu; charts and tables never overlap.

## Resource, audit, and abuse controls

The Server reserves global, per-node, per-target, per-actor, and daily-byte
budgets transactionally before queueing. Expired queued jobs release
reservations. Completion reconciles actual bytes without allowing a negative
budget. Repeated rejected requests are rate-limited and audited.

Audit records include actor, source node/target name snapshots, kind, approved
limits, target revision, policy/capability digests, lifecycle transitions,
cancel/retry decisions, and outcome. They exclude raw hop addresses, binary
output, full destination secrets, and numeric sample payloads.

Prometheus metrics use fixed job kind/state/error/capability labels. Node IDs,
target IDs/hosts, actor IDs, job IDs, and error text are not labels. Readiness
degrades only when enabled diagnostic queue/MTS publication or reconciliation
cannot operate; a missing optional Agent binary is a node capability state.

## Backup, restore, and rollback

SQLite backup includes targets, policies, jobs, encrypted hop metadata, cursors,
budgets, wrapped data-key references, and audit. Logical MTS export includes
every registered diagnostic measurement and bounded tag. Restore authenticates
diagnostic ciphertext with reconstructed AAD, pauses leasing, expires old
leases, marks restored running attempts `unknown`, validates MTS/SQLite job
references, and requires explicit operator retry.

Rollback first disables diagnostics, cancels queued jobs, waits for bounded
running shutdown, backs up, restores the matching pre-migration Server/Agent and
SQLite backup, and leaves new MTS measurements unreachable. Deletion of those
measurements requires separate owner approval.

## Test and acceptance gates

Backend and Agent tests cover target parsing/resolution, IPv4/IPv6 forbidden
ranges, DNS rebinding, private allowlist intersection, capability/version
checks, fixed arguments, no shell, environment clearing, context cancellation,
process-tree termination, output bounds, malformed JSON, NaN/range checks,
partial results, sequence idempotency, MTS batch failure, SQLite metric absence,
lease/start/reconnect/retry/unknown semantics, concurrency, budgets, retention,
backup/restore, and redaction.

Frontend tests cover every node/target selector showing names rather than IDs,
no free-form destination, risk confirmation, safe defaults/maxima, running and
cancelled jobs, MTS errors, human-readable units, MTR hop privacy, responsive
charts/tables, background updates, keyboard access, and loading/error/empty
states.

Acceptance requires real controlled iperf3 and MTR evidence for IPv4 and IPv6,
proof that a forbidden target cannot receive traffic, proof that cancellation
terminates the child process, proof that SQLite has no diagnostic numeric
samples, MTS restart/backup/restore survival, at least 90 percent coverage, race
tests, `goimports-reviser`, `golangci-lint`, production build, browser tests,
resource/load tests, and deployed administrator/public authorization checks.

## Approval boundary

This design authorizes no implementation. It is canonical SQLite migration
`v14` and adds high-impact network traffic,
Agent capabilities, external binary dependencies, SQLite application tables,
MTS measurements, administrator APIs, and deployment requirements. It depends
on the approved remote-operations, metric-catalog, signed-rollout, and secure-
enrollment foundations and requires explicit approval of this complete design.
