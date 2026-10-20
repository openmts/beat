# Alert evaluation v12

Updated: 2026-09-15

Status: implementation contract for root review. Business semantics were confirmed
in the C08 handoff. Implement in independent files first; do not register v12 in
the main migration ledger until v11 remote operations is ready. Existing v10
routes and notification administration remain compilable and unchanged until the
v12 integration switch.

## Boundaries

Extend the existing rule/event pipeline and v10 durable notification queue.
SQLite owns rule policy, scope, confirmation progress, evaluation health,
incidents and preparation outcomes. Numeric Agent samples and window aggregates
remain in MTS; evaluation state never stores a metric-value cache. Existing
threshold event values remain event evidence under the existing event contract.
NoData/Error events use a separate model and table with no numeric value field.

The production evaluator owns one persisted state per rule/node. Threshold state
and evaluation-health state are independent: an existing CPU incident may remain
active while evaluation becomes Error and later recovers. Recovery of evaluation
means usable evidence returned; it never claims the threshold condition cleared.

## Rule and HTTP contract

Keep existing comparison operators and normalize aliases to
`gt`, `gte`, `lt`, `lte`, `eq`, `ne`. Unknown operators, nonfinite or out-of-domain
thresholds, unknown metric keys/aggregations, duplicate targets and oversized
requests fail before mutation.

The advanced rule response retains the existing rule fields and adds:

```json
{
  "revision": 3,
  "scope": {"kind": "groups", "group_ids": ["group-id"]},
  "window_seconds": 300,
  "aggregation": "avg",
  "recovery_threshold": 75,
  "recovery_duration": 60,
  "no_data_policy": "keep",
  "no_data_delay_seconds": 300,
  "error_policy": "keep",
  "error_delay_seconds": 60
}
```

`duration` remains the trigger confirmation duration. Scope is exactly one of
`all`, `nodes` with node_ids, or `groups` with group_ids. Explicit scopes require
at least one existing target. Membership is dynamic and rechecked at commit.
`window_seconds=0` means current/last evidence and requires aggregation `last`;
positive windows use a catalog-supported aggregation. Operational heartbeat age
and traffic usage keep their documented semantics and initially support current
mode only. A nullable recovery threshold means the trigger threshold.

Keep `/api/v1/alerts/rules` and item paths. Create validates a complete policy;
update/delete require `expected_revision`. Writes recheck the current admin
session/role, domain revision and targets in one transaction with audit. Return
409 stale_revision without discarding the client form, 404 missing, 400 invalid,
429 capacity with Retry-After, and 503 storage unavailable.

Add bounded read APIs:

- `GET /api/v1/alerts/evaluations`: rule/node/phase/status filters and cursor;
- `GET /api/v1/alerts/evaluations/{rule_id}/{node_id}`: state, health epoch,
  evidence/window references and incident/notification links;
- `GET /api/v1/alerts/evaluation-events`: rule/node/kind filters and cursor;
- `POST /api/v1/alerts/rules/preview`: validate and query without storing state,
  events, audit success mutations or notifications;
- report schedule responses expose an optional durable preparation outcome,
  separately from delivery state.

No response converts a query failure or absent value into zero. Current preview
values are optional/null and are not persisted as time series in SQLite.

## Comparison and threshold phases

Trigger uses the configured comparison exactly. Recovery boundaries are:

| Operator | Recovery condition | Allowed recovery threshold |
| --- | --- | --- |
| gt | value <= recovery | recovery <= trigger |
| gte | value < recovery | recovery <= trigger |
| lt | value >= recovery | recovery >= trigger |
| lte | value > recovery | recovery >= trigger |
| eq | value != trigger | absent or equal to trigger |
| ne | value == trigger | absent or equal to trigger |

For gt/gte/lt/lte, the gap between trigger and recovery is the hold band. With no
separate recovery threshold, equality preserves the existing comparison's logical
complement. Eq/ne only support symmetric recovery.

Phases are `normal`, `pending`, `firing`, `recovering`:

1. New usable triggering evidence enters pending, or triggers immediately when
   duration is zero. Confirmed progress advances only on new complete evidence.
2. Triggered state opens one threshold incident and queues its trigger in the
   same durable transaction. Repeated firing evidence creates no duplicate.
3. Recovery evidence enters recovering; recovery_duration independently confirms
   recovery. Leaving the recovery condition returns to firing without closing.
4. An acknowledged incident can only be resolved manually. Numeric recovery may
   update evaluation state/progress but cannot auto-resolve or notify recovered.
5. Rule disable, metric policy disable, deletion or leaving scope preserves an
   existing incident for manual handling, reports disabled/out_of_scope, and
   does not emit a fictitious recovery. It clears unconfirmed numeric progress.
6. A rule revision change invalidates prepared evaluations. The next evaluation
   adopts the new policy with cleared pending/recovery progress; active incident
   identity remains authoritative until a permitted transition/manual action.

While any triggered/acknowledged incident exists, a rule update transaction rejects
changes to metric, operator, aggregation, window and trigger/recovery thresholds or
durations with 409 active_incident_policy. The operator must first handle the
incident or create a new rule. Name/description, scope, enabled and evaluation-health
notification policy remain editable; none can invent recovery of the old incident.

## Evidence, windows and time

For numeric Agent metrics, the evidence identity binds node, selected metric,
latest committed receipt/sample, selected metric timestamp, catalog revision,
aggregation and window bounds. Duplicate receipt polling updates operational
observation time only; it does not advance numeric confirmation or create events.
An identical receipt cannot become fresh evidence because a browser/server timer
or window end was moved forward.
Custom evidence binds actual execution UUID, completion timestamp, configured
cadence and definition version from C04 custom_samples metadata. The production
cache is one-time drain, and both values and metadata are copied into the pending
report. The receipt stores bounded provenance at the same report timestamp and
in the same Sync batch; custom scalar measurements stay unchanged. A newer host
receipt cannot refresh an older custom sample. Custom window observations and
their window end use actual execution completion time; report capture time only
binds transport provenance. Freshness still compares completion with observation
time, and replayed execution IDs cannot add window coverage. V12 must reject old definition
versions and deduplicate repeated execution IDs when reading custom windows.

Current mode reads a finite typed metric plus its timestamp. A window ends at the
latest accepted receipt capture and looks back window_seconds; this also includes
the permitted C04 clock lead without inventing negative age. It must have an end
observation, coverage at the start, at least two samples for a positive window,
and no gap greater than max(2 × effective collection cadence, 30 seconds). Counter
rate needs a preceding baseline and reset-aware arithmetic. Effective cadence
includes the custom collector interval when relevant. Incomplete/empty windows
are NoData with a fixed reason, not zero or a partially computed alert value.
Freshness must still satisfy the C04 receipt and selected-value age policy.

The pure window aggregator sorts a copy of observations and counts each source
identity once. A repeated identity with a different value, or distinct identities
at an ambiguous identical timestamp, is invalid evidence. Gauge aggregates use
only observations inside the inclusive window and require at least two; an older
predecessor proves starting coverage without biasing the mean. Counter rates use
the preceding boundary sample, account for resets, and proportionally allocate
the interval crossing the left boundary. Gaps are never filled or extrapolated.

Heartbeat age is an explicit exception to numeric receipt advancement: each
bounded successful contact-age check is new evidence of elapsed absence, even
when last_seen has not moved. It retains C04 authenticated cadence/offline grace
and skips never-seen provisioning nodes. Storage/policy failures are Error.

Persist confirmed milliseconds separately from started_at timestamps. The
service has a random run ID. Elapsed credit requires the same run, forward server
and source clocks, a new evidence ID and a bounded gap. For a normal live step,
credit at most both source elapsed time and server elapsed time; duplicate polls
provide neither. On a run change, retain confirmed progress and incident/epoch
identity but give the first fresh step zero elapsed credit, establishing a new
baseline. Thus downtime never satisfies a trigger/recovery delay. A live gap
beyond the permitted continuity bound or explicit NoData/Error clears pending
numeric progress; firing incidents are retained.

If server time moves backwards, reset unconfirmed numeric and health-delay
progress, rebase observation time, retain incident IDs, epoch IDs and notification
flags, and expose clock_rollback. Never subtract into negative progress or reuse a
source key. A captured/source time regression cannot advance evidence.

## Independent NoData / Error notifications

NoData means absent/unusable evidence (no receipt/value, stale value, incomplete
window); Error means failed evaluation (storage, timeout, capacity, invalid
numeric result or invalid policy). Both are explicit statuses even with policy
`keep`. Keep is the default for each status; `notify` enables its own delay.

One continuous unhealthy episode has a durable random `health_epoch_id`. It may
move between NoData and Error without claiming recovery. Each kind has separate
notification eligibility within the epoch; changing kind resets that kind's delay
baseline. Repeated successful checks of continued absence/failure may advance
health delay, while duplicate numeric samples never advance threshold progress.
No elapsed health-delay credit is granted across a restart, excessive polling
gap or clock rollback. At most one NoData and one Error notification are emitted
for an epoch, independent of any threshold incident.

Health events have kinds `no_data`, `error`, `recovered`, no Value field, bounded
reason/context and a stable epoch. Source keys are
`alert-evaluation:<epoch UUID>:<kind>`. A full, fresh, new usable observation ends
the epoch and emits `alert.evaluation_recovered` only if that epoch notified;
otherwise it simply records recovered health. This transition may coexist with a
new threshold trigger or an existing firing/acknowledged incident. Disabled or
out_of_scope suspends notification work and preserves epoch/incident evidence;
it does not generate evaluation_recovered.

Routing adds `alert.no_data`, `alert.evaluation_error`, and
`alert.evaluation_recovered` to the reviewed notification event taxonomy. Existing
channel subscriptions are not silently changed; an opted-in rule can truthfully
produce a not_routed receipt when no channel subscribes. Payloads omit metric.value
and metric.formatted_value for health events. The shared notification template
allowlist needs fixed evaluation.status/reason/epoch fields, not arbitrary data.

## Atomic result commit and duplicate handling

Query MTS outside a SQLite write transaction. Carry expected rule revision,
metric policy revisions, scope, state_version, current incident status and the
proposed evidence/action IDs into commit. Under one immediate transaction:

1. Recheck current rule existence/enabled/revision, node existence/current scope,
   custom binding when applicable and current metric enabled/revision.
2. Compare state_version and current incident status. A stale plan is skipped;
   state, audit, incident and outbox all remain unchanged.
3. Store the new state and any threshold/health event transitions; audit exact
   domain changes and enqueue corresponding notifications atomically.

Non-notifying progress uses the same checks/CAS. Do not use node.updated_at as a
scope revision because heartbeats update it; re-evaluate normalized membership
predicates. Rule/scope changes cancel stale work rather than finishing under old
alert policy (unlike already admitted C04 metric ingestion).

One step can produce health recovery plus threshold transition. This requires a
bounded queue batch operation: at most **two** requests, one secret write window,
one SQLite transaction and one business callback. Keep existing Enqueue intact.
All-existing source keys return duplicate receipts; all-new executes once;
partially existing batches are reconciled by the authoritative state CAS or
rolled back as conflict, never partially committed. No nested enqueue/network
inside the callback. The producer interface adjustment must be reviewed before
modifying root-owned alert_notifications.go or trafficreport integration.

Evaluation/event state is the durable duplicate authority even after notification
retention removes a message. Missing message history means unknown/history
unavailable, never queued/success. Database errors remain errors. Restore keeps
state/event/source identity together with v10 outbox semantics; external delivery
remains at least once after an acceptance/response-loss boundary.

## Proposed v12 storage

- Extend alert_rules with revision, deleted_at tombstone, bounded scope/window/aggregation/recovery and
  NoData/Error policies. Backfill existing rules to all/current/last, symmetric
  recovery, zero recovery delay and keep policies.
- Normalized alert_rule_nodes / alert_rule_groups with target indexes. Target IDs
  retain missing/deleted references for audit; rule writes require existing targets.
  Deleting a rule is a tombstone with revision advance, not incident deletion.
  State and health-event tables never cascade on node/rule deletion. A reconcile
  pass marks missing-node state out_of_scope and disabled/tombstoned rule state
  disabled without auto-resolving the retained incident/epoch.
- alert_evaluation_states keyed by rule/node: rule/state versions, phases/status,
  confirmation progress/baselines, run/evidence references, active threshold event,
  health epoch/current-kind progress and notification flags. No metric-value field.
- alert_evaluation_events: UUID identity, rule/node/rule revision, health epoch,
  kind, fixed reason, timestamps and notification source reference. Unique epoch/kind.
- traffic_report_preparations keyed by schedule/period: schedule revision,
  preparing/failed/queued/superseded outcome, attempt count, fixed failure reason,
  last attempt/next retry, lease/owner and resulting source reference. Never store
  an aggregated report body outside the existing encrypted v10 message payload.

Preparation failure does not advance a report period. Successful preparation,
period completion and queue entry share the existing transaction with schedule
revision/enabled/scope recheck. A bounded lease avoids concurrent builders; stale
lease completion is rejected. Manual preview/test remains distinct from automatic
period completion. Delivery retention does not erase preparation truth.

## Numeric budgets

| Resource | Bound |
| --- | --- |
| Rules | 256 persisted rules |
| Explicit scope | 1–1,000 nodes or 1–128 groups |
| Expanded pairs per ruleset | 10,000; reject mutation before expansion exceeds it |
| Rule/preview JSON | 32 KiB; names 128, description 2,048 UTF-8 bytes |
| Window | 0/current or 10–86,400 seconds |
| Trigger/recovery delay | 0–86,400 seconds each |
| NoData/Error delay | 0–86,400 seconds; defaults 300 / 60, policy keep |
| Scheduler | 30-second cadence, non-overlapping 25-second rounds |
| Evaluation workers | 4 default, 8 maximum; one pair lease at a time |
| Round admission | consecutive batches of 256 within the 25-second budget; fair next_due keyset |
| Per evaluation | 5-second MTS query, 2-second metadata commit, 15-second lease |
| Window reading | 100,000 scalar samples; 4,096 shards / 32,768 parts; fail on budget |
| Preview | 8 total / 2 per client, at most 8 nodes, 5-second deadline |
| Notification fanout per step | at most 2 messages, v10 queue/provider limits apply |
| State rows | 100,000; reject growth on exhaustion, no unbounded RAM map |
| Health history | 100,000 rows, 30 days; never purge active epoch evidence |
| List pages | default 50, maximum 200; bounded opaque keyset cursor |
| Report preparation | max 1,000 nodes/build, 30-second build, 45-second lease |
| Preparation retry/history | 30 s–5 min bounded backoff, 30-day terminal retention |

Each round may process multiple batches; 256 is an in-flight selection bound,
not a per-30-second total. Batch latest receipt/value reads and exact shared
metric/window reuse reduce duplicate MTS work. Preserve per-pair evidence/CAS
checks after reuse. Export oldest_due_age and completed/deferred pairs; a due lag
over two scheduler periods explicitly degrades evaluation health. No claim that
10,000 pairs are evaluated every 30 seconds is permitted before measured load
and soak evidence proves it. Unadmitted pairs remain visibly due, never silently
counted as evaluated.

Inactive state with no live incident/epoch may retire after 30 days; UUID epochs
prevent source reuse after retirement. Capacity rejection does not mutate policy
or silently drop an evaluation. Queue backpressure leaves transitions retryable.
Fixed operational outcome/reason metrics exclude node/sample/host/token/value
labels. Provider failures alone do not fail dashboard readiness; inability to
persist evaluation/queue state does.

## Frontend and verification

Extend the existing shadcn rule editor with scope, catalog-driven aggregation,
window, trigger/recovery confirmation and keep/notify controls with separate
delays. Preserve edited values on revision conflict. Show threshold phase and
NoData/Error independently, progress based on confirmed time, last good/window
references, disabled/out_of_scope, health events and durable delivery history.
No silent rule auto-save, no network send from preview, no fabricated value zero.
The root-owned notification channel/delivery pages stay untouched in this stage.

Tests cover fresh and upgraded v12 schema, rollback/failure recovery and no ledger
registration before v11; every operator/equality/hysteresis direction; scopes and
concurrent membership changes; complete/partial/empty windows, resets, duplicate
receipts and future captures; both numeric timers plus each notify delay; restart,
clock rollback and outage-kind changes; ACK/manual-only resolution; disabled and
out_of_scope incident preservation; rule/metric revisions changing after query;
CAS/leases/dual workers; simultaneous health recovery and threshold actions;
queue/SQLite failure atomicity; lost ACK/retention/restore source deduplication;
report preparation failure/retry/lease/revision semantics; pagination/capacity,
HTTP authorization and frontend conflict/status states. Use real temporary
SQLite/MTS, local delivery stubs, race/shuffle, >=90% new logic coverage and
existing CI checks before integrating the runtime or migration ledger.

## Implementation checkpoint (2026-10-07)

Independent evaluator scheduling now selects consecutive bounded batches, with
per-pair durable leases and CAS checks for concurrent acknowledgement. Missing
scope pairs can be admitted in bounded batches under the state/pair budgets.
The independent evaluator now reads real MTS receipt/scalar provenance, handles
custom completion windows, and commits up to two threshold/health messages with
state and audit in the batch transaction. Clock rollback rebases unconfirmed
progress while fencing prepared work and preserving incidents/epochs. Round
results report deferred work, oldest due age, degradation and whether the final
backlog count is complete. Real temporary SQLite/MTS/outbox tests cover duplicate
sources, concurrent ACK, dual-message rollback, custom replays and clock rebasing.
Independent HTTP handlers and route registration now provide revision-aware rule
writes, bounded state/history pages, evidence and notification references, and
read-only previews (8 global / 2 per user, at most 8 nodes). The source runtime now
registers them after the ordered v11/v12 migrations. The frontend provides the advanced rule editor,
independent evaluation/history tabs, evidence/notification detail and cancellable
read-only preview. Numeric policy controls lock while incidents are active, and
revision conflicts preserve the draft. Node creation (managed, heartbeat and
enrollment), group reassignment and group deletion now recheck pair capacity
inside their write transaction once v12 schema is available; rejected enrollment
does not consume its credential. Consolidated acceptance and load verification
remain separate; the 10,000-pair scheduler fixture is not MTS
throughput evidence. The isolated v10 preview has not been modified.

The final targeted race/shuffle run passed across API, handler, service and store;
new implementation coverage was 866/902 statements (96.01%), with evaluator
package coverage at 96.0%. Related package lint and full notification package
race tests passed. These checks do not replace the full repository CI gate.
The subsequent UI/scope batch passed 28 focused frontend tests with 99.34% line
coverage, TypeScript/oxlint checks, and backend race/shuffle tests for capacity,
rollback, concurrency and enrollment retry. Scope/HTTP capacity helpers reached
100% coverage and the extracted node write helper reached 93.3%.
Runtime integration tests verify that v12 does not execute the legacy numeric
loop, contact maintenance keeps authenticated cadence grace, failed pair commits
do not count as successful rounds, and startup failures/shutdown release resources.
The commercial runtime/metrics/contact-wrapper tests passed race/shuffle with
80/83 covered statements (96.39%). Production build verification used a temporary
output directory; the existing preview and live Agent configuration were untouched.

Store race-suite preparation now builds the current production schema once,
checkpoints and closes it, then keeps a read-only byte image for ordinary CRUD
fixtures. Each fixture gets its own `0700` directory, `0600` SQLite file, WAL and
eight-connection pool; migration/reopen tests still use the real constructor.
This also covers ordinary admin/session/dashboard fixtures without sharing their
mutable data. The seed directory is removed immediately and fixture files are
removed by test cleanup.

With shuffle seed `1791358262781736866`, the complete store package passed race
testing in 118.765 s after fixture reuse (the earlier repository-wide gate timed
out at 600 s). The real 100,000-state setup remained intact and took 36.83 s.
V12 also indexes `nodes(group_id)`: the group-scope query plan now seeks that
index instead of scanning unrelated nodes. With 10,000 nodes and 100 matches,
25 identical count queries measured 28.42 ms versus 1.5416 s before the index.
These are targeted measurements, not a replacement for the full concurrent CI
gate or production evaluator load testing.
