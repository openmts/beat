# Beat operations runbook

Updated: 2026-10-09

## Retained deployment runtime contract

Beat exposes three unauthenticated operational endpoints:

- `GET /healthz`: process liveness and build version;
- `GET /readyz`: SQLite schema, MTS, staged restore, and scheduler readiness;
- `GET /metrics`: Prometheus text metrics with bounded labels.

The retained deployment uses the `schedulers` readiness check to supervise each background worker
individually: alert evaluation, traffic-report scheduling, and maintenance
scheduling report every iteration result through an observer hook. A worker
that exits unexpectedly (including panic) is restarted with exponential
backoff up to 3 attempts before the supervisor gives up. `/readyz` names any
unhealthy worker with a reason — `stopped` (exited and restarts exhausted),
`failing` (4 or more consecutive failed iterations), or `stale` (no successful
iteration for 4 expected intervals). Prometheus gauges
`beat_scheduler_worker_running`, `beat_scheduler_worker_last_success_unix_seconds`,
`beat_scheduler_worker_consecutive_failures{worker=...}`, and
`beat_scheduler_worker_restarts_total` expose the same signals. Treat any
non-ok scheduler check as a page: the process stays alive but that schedule no
longer makes progress.

The metrics endpoint intentionally avoids usernames, node names, node IDs,
hosts, and request URLs. Restrict `/metrics` at the reverse proxy when the
monitoring network is private.

## Ingress admission budgets

The server builds one immutable ingress capacity configuration at startup and
rejects zero, negative, overflowing or internally inconsistent overrides instead
of silently disabling protection. Route classes are fixed: `public_static`,
`public_snapshot`, `public_history`, `auth_kdf`, `agent_read`, `agent_write`,
`admin_read`, `admin_write` and `admin_expensive`. Agent WebSocket budgets belong
to the metrics fanout, not this controller.

Defaults are tuned for a single small deployment; a measured deployment can
override a class with `BEAT_INGRESS_<CLASS>_GLOBAL_RATE`, `_GLOBAL_BURST`,
`_IDENTITY_RATE`, `_IDENTITY_BURST`, `_GLOBAL_INFLIGHT`, `_IDENTITY_INFLIGHT`
plus `BEAT_INGRESS_IDENTITY_ENTRIES` and `BEAT_INGRESS_IDENTITY_TTL`. Class names
are upper-cased in the variable, for example `BEAT_INGRESS_AUTH_KDF_...`.
Changes require a restart.

Rate rejection returns `429` with a bounded `Retry-After`; a saturated global
in-flight budget returns `503`, which is a transient condition and must not
remove the instance from the load balancer. The bundled UI reads `Retry-After`
and backs off its polling loops (capped at five minutes) instead of retrying at
a fixed interval, so a throttled deployment is not hammered by open dashboards. Limiter state is in memory only, so
rollback or restart simply resets the budgets.

Recommended alerts (add to the list below):

- `beat_ingress_rejected_total{class="auth_kdf"}` increases for 5 minutes;
- `beat_ingress_rejected_total{reason="table_full"}` is non-zero;
- `beat_ingress_in_flight{class="admin_expensive"}` stays at its limit;
- `beat_ingress_identity_entries` approaches `beat_ingress_identity_entries_limit`.

The owner-only Remote operations page shows the same values read-only.

JSON writes are decoded through one bounded helper. A request that declares a
non-JSON media type is rejected with `415`, a body over the budget with `413`,
and a second JSON value after the first with `400`. Management and
authentication payloads additionally reject unknown fields. Uploads, archives
and theme packages are not affected: they keep their own route budgets.


Public node history is bucketed in the storage layer: the bucket width is
derived from the requested range (whole seconds, at least one second) with a
hard ceiling of 600 buckets, so the response size depends on the budget rather
than on how many raw samples were stored. Each metric uses the aggregation from
its catalog definition, falling back to the built-in default when no catalog is
configured, and empty buckets are omitted.

Public fleet and node reads reuse the same encoded snapshot generation the
metrics producer already built. Within the reuse window (metrics fanout cadence
plus one second) those routes serve shared bytes and the same `ETag`, so a
client that sends `If-None-Match` gets a `304` without touching MTS. Node views
recompute relative fields such as telemetry age per request, and a node absent
from the snapshot returns `404` instead of falling back to a direct query; the
direct path is only used before the first build or after the window expires.

Recommended alerts:

- `/readyz` is non-200 for 2 minutes;
- `beat_readiness_check == 0` for 2 minutes;
- `beat_agents_online < beat_agents_total` for 3 minutes;
- `beat_agent_max_heartbeat_age_seconds > 90` for 3 minutes;
- `beat_notification_deliveries_total{result="failed"}` increases;
- `beat_backups_total{state="failed"} > 0`;
- SQLite or MTS storage grows faster than the configured retention policy.

The working tree implements the separately approved runtime-resilience backend;
see [its ownership, shutdown and DTO guide](runtime-resilience-backend.md).
After that batch is deployed, replace the
aggregate scheduler alert with per-worker up, last-success-age, failure, lag,
snapshot-build, active-WebSocket, rejected-upgrade, and dropped-snapshot alerts
using the fixed metric names in that design. These metrics are not part of the
current deployed contract yet.

### Runtime diagnostics captures

An owner session can capture bounded Go runtime evidence from
`Admin → Remote operations → Runtime diagnostics` without enabling the public
`/debug/pprof` mux. The tab shows one summary band (build, uptime, Go version,
heap/stack memory, goroutines/threads, GC) and a table of this owner's artifacts.

- `cpu` runs 5-30 seconds and `trace` runs 1-5 seconds; `heap`, `allocs`,
  `goroutine`, `mutex`, `block` and `threadcreate` are instantaneous profiles at
  fixed debug level 0. Only these kinds exist; a request cannot name another
  profile or pass a runtime knob.
- At most one CPU/trace capture runs globally, one capture per session is
  consumed by the first complete download, and an artifact is deleted after 15
  minutes or by `Delete`. A partially transferred download keeps the file and
  can be retried.
- Artifacts live in `runtime-diagnostics/` inside the data directory with `0700`
  directory and `0600` random file names. They are never written to SQLite, MTS,
  backups, logs or metrics. Restarting the server deletes every orphan file
  before serving.
- `beat_runtime_capture_events_total{kind,result}`,
  `beat_runtime_capture_rejections_total{reason}`,
  `beat_runtime_captures_active`, `beat_runtime_capture_storage_bytes` and
  `beat_runtime_capture_cleanup_failures_total` are the bounded operational
  signals. A cleanup failure marks the summary `degraded` and rejects new
  captures until the process restarts.
- Audit rows use the fixed actions `runtime.capture.start`,
  `runtime.capture.download` and `runtime.capture.cancel`. Terminal
  `complete`/`expire`/`fail` outcomes currently appear only in the capture table
  and metrics, not as separate audit rows.

Starting, downloading and deleting a capture require a recent local
reauthentication; listing and the summary only require the owner session. No
Agent, service token, wallboard, theme or ordinary administrator principal can
read profiles.

The current deployment also lacks unified HTTP/KDF/history-query/Agent
admission metrics. Reverse-proxy rate limits can reduce exposure but do not
replace application-aware node/session identity or MTS query-cost controls. The
future metric and alert contract is reviewed in
[`ingress-resource-governance-design.md`](./ingress-resource-governance-design.md)
and must not be treated as deployed until that batch passes its load gate.

The current worktree uses Agent report protocol 2 with one immutable pending
sample and a 15-minute retry window. `last_seen` is accepted contact;
`telemetry_at` and `telemetry_state` reflect the committed MTS receipt. A node can
be online with delayed/stale telemetry. Contact offline grace is at least 90
seconds and at least three authenticated report intervals. REST and the production
`/api/v1/ws/metrics` snapshots use the same privacy and freshness contract.
In the retained deployment, the separate `/metrics/ws` hub has no snapshot
publisher. The working tree explicitly returns 404 for `/ws` and `/metrics/ws`.

On an uncertain MTS write, the engine is poisoned: `/readyz` returns 503,
`beat_agent_ingestion_recovery_required` is 1, and further reads/writes/ACKs fail.
A contact-only `503` never confirms metric durability. The process remains up;
it does not silently reopen WAL or restart itself. First diagnose and repair disk
capacity, filesystem health and permissions. Then perform one supervised restart:

```bash
systemctl restart beat-server
systemctl status beat-server --no-pager
curl --fail http://127.0.0.1:8080/readyz
```

Startup replays WAL and establishes a durable recovery boundary. If startup or
readiness still fails, stop repeated restarts and repair the underlying problem;
restarting cannot fix a persistent disk fault. Do not manually edit WAL files.
Confirm fresh receipts after recovery; an Agent restart discards its memory-only
pending sample. `beat_agent_reports_total` distinguishes contact-only, duplicate,
capacity, ordering and storage outcomes without high-cardinality identity labels.
See [`agent-ingest-consistency-design.md`](./agent-ingest-consistency-design.md).

If only MTS readiness fails while reports and history still work, inspect the
authenticated `GET /api/v1/settings/maintenance` response's
`storage.mts_health_reasons`. `compaction degraded on shard ...` identifies
overlapping persisted parts awaiting compaction; it does not by itself prove
WAL corruption. The 2026-10-09 preview incident confirmed this for 17 shards:
the upstream defaults disable automatic compaction.

Beat enables compaction during flush with one compaction worker. Existing daily
maintenance retains its exclusive operation guard for full compaction. The
engine's independent background timer stays disabled so it cannot bypass Beat's
backup/maintenance locking. Startup's existing recovery flush also compacts
previously overlapping parts. Validate readiness and fixed-range history after
upgrading, and retain the matching cold backup before touching live data.

Flush-time compaction is skipped while a shard still has in-flight readers, and
nothing retries it afterwards: on the 2026-10-19 preview incident the current
hour's shard kept two overlapping parts because every flush ran under active
reads, and `/readyz` stayed 503 for hours while reports, history and instant
queries kept returning 200. The lighter remedy is to run the guarded maintenance
cycle instead of restarting the process:

```bash
curl --fail -X POST http://127.0.0.1:8080/api/v1/settings/maintenance/run \
  -H "Origin: http://127.0.0.1:8080" -b session.cookies -c session.cookies
curl --fail http://127.0.0.1:8080/readyz
```

Since v222 the maintenance scheduler self-heals this state on its own: every
cycle it reads the same health reasons, and when `compaction degraded on shard
...` appears it runs one guarded flush + full compaction — the same primitive as
the manual call above — without deleting retention data, without changing
readiness semantics and without a restart. Each repair logs
`repaired degraded MTS compaction` with the triggering reasons. If the reason
returns within the same hour after a repair, treat it as sustained reader
contention and escalate before relying on further automatic repairs.

That drops only data outside the retention cutoff and ends with the same full
compaction the daily job runs. A supervised restart also clears the overlap via
the recovery flush, at the cost of a short outage. Either way, confirm
`storage.mts_health_reasons` is empty and that one shard no longer holds
overlapping parts afterwards; if it comes back within the same hour, escalate
before scheduling further restarts.

Backup v3 freezes MTS before its SQLite snapshot/cutoff and exports within that
same guard. Its event-time upper bound includes the permitted 2-minute Agent clock
lead, preserving already accepted future samples while excluding later writes.
Waiting reports/queries/cleanup cancel normally, and ZIP creation releases the
guard. Restore keeps the typed receipt and numeric samples together.

MTS read failures are fail-closed: node REST/history, traffic reports, and
alert evaluation all propagate engine query errors instead of presenting empty
results as data, and every MTS operation after store close returns
`ErrMTSStoreClosed` rather than a successful empty query. An empty chart or
absent alert sample therefore indicates either genuinely no data or a logged
MTS error; correlate `/readyz` MTS status and storage logs during incidents.
Per-consumer failure metrics and the typed no-data/error query contract remain
owned by [`runtime-resilience-design.md`](./runtime-resilience-design.md) and
[`metric-catalog-dashboard-design.md`](./metric-catalog-dashboard-design.md).

Application cleanup is now partly closed in the worktree. Alert events and
backup records are cursor-paged (`{items, next_cursor}`, default 50, max 200),
and startup reconciliation runs before the HTTP listener opens: it rolls back
unconfirmed staged restores, resolves interrupted `running` records and
archive-less rows into `interrupted`/`missing`, retries `deleting` removals,
quarantines orphan `beat-*.zip` archives, drops
orphan online restore journals, and expires terminal records after 90 days.
Retry-safe deletion commits `deleting` with the previous state before touching
files, so a failed stage/delete request must still be checked - inspect
`GET /api/v1/admin/backups`, `GET /api/v1/admin/backups/quarantine`, the
`beat_backups_total`, `beat_backups_quarantined` and
`beat_backup_reconcile_outcomes` metrics, and the backup directory after any
restart. Two gaps remain: expired sessions are not cleaned in production, and
administrator audit cleanup still runs only inside the MTS maintenance cycle.
The durable replacement for those gaps is
[`application-data-lifecycle-design.md`](./application-data-lifecycle-design.md).

Administrator audit evidence is not yet a transactional record of deployed
mutations. Ordinary protected routes currently emit generic post-response
`admin.mutation` or `admin.read` rows with route templates and empty details;
bootstrap/login audit request IDs do not match their HTTP request IDs, audit
insert failure can leave a successful mutation without evidence, and backup,
maintenance, notification, terminal, or other external work may be recorded as
successful before its final result is known. During incident review, correlate
the audit table with structured HTTP, scheduler, backup, notification, and
terminal logs, and verify authoritative SQLite/MTS/filesystem state. Do not use
the current audit row alone to prove that a mutation did or did not occur. The
reviewed replacement is
[`administrative-mutation-consistency-design.md`](./administrative-mutation-consistency-design.md).

The current worktree enforces Origin, audit and `no-store` for the common
administrator/owner route boundary. TOTP setup requires recent proof and keeps
one session-bound candidate in memory for five minutes; the active factor is
unchanged until confirmation succeeds. Logout, session revocation, password
change, and account disablement/deletion/role changes cancel the affected live
terminal and batch-operation contexts. A bounded background check also detects
expiry or external revocation without extending idle lifetime. SSH handshake
and session setup honor cancellation. These changes still require deployment
verification; the installed build is identified in the local deployment notes.

Sensitive-operation coverage remains tracked by the commercial acceptance
register. Selected older credential, notification and remote-operation routes
still need their final scoped policy and transactional audit integration. A
recent-auth guard must be revalidated inside new privileged SQLite mutations;
metric dashboard and catalog writes already do this and commit domain audit
rows with their state changes.

SQLite v9 encrypts TOTP, OIDC and SSH credentials with one resource-bound data-key
registry. Startup completes or resumes migration before serving. Owners inspect
health and rotate data keys from `/admin/security` → Application keys, after
password/TOTP confirmation. Rotation runs in bounded batches; a degraded result
requires a server restart to resume the saved cursor. Backup and restore staging
refuse to overlap rotation. Notification channel secrets remain plaintext until
their v10 consumer migration.

Root-key rotation is a root-only stopped-server operation:
`/opt/beat/current/beat-server secrets rotate-root --data-dir /var/lib/beat`.
It shares the server's stable parent-directory lease and recovers a private
journal before backup restore or ordinary migrations. The installer precreates
the `0600` lock with the Beat service owner; never move or remove this lock when
replacing the data directory. See the
[key lifecycle contract](./application-secret-lifecycle-design.md).

Generated Beat backups contain both recoverable secret ciphertext and the
matching root key. Mode `0600` prevents ordinary local users from reading them,
but the ZIP is not an independent encryption boundary. Store off-host copies on
an encrypted volume/object store or wrap them with the organization's backup
KMS before transport or retention. Never separate or independently replace the
SQLite snapshot and root key during restore.

## Agent enrollment operations

Automatic Agent discovery exposes bounded Prometheus counters plus one aggregate
snapshot. Every label value is a fixed enum; grant, request, node, hostname and
source-address values never become labels.

Counters, with outcomes `served`, `denied`, `rejected`, `limited`, `unavailable`
or `failed`:

- `beat_agent_enrollment_challenges_total{outcome}`;
- `beat_agent_enrollment_requests_total{outcome}`;
- `beat_agent_enrollment_polls_total{outcome}`;
- `beat_agent_enrollment_claims_total{outcome}`;
- `beat_agent_enrollment_cleanup_total{result="completed|failed"}`;
- `beat_agent_enrollment_request_resumes_total`.

Gauges and histograms:

- `beat_agent_enrollment_requests{status="pending|approved|rejected|expired|claimed"}`;
- `beat_agent_enrollment_grants{state="active|expired|disabled|revoked"}`;
- `beat_agent_enrollment_pending_age_seconds` is the age of the oldest pending
  or approved request and is zero for an empty queue;
- `beat_agent_enrollment_approval_seconds` and
  `beat_agent_enrollment_claim_seconds` histogram the approval and claim waits.

A failed snapshot query keeps the previous values, so a broken scrape never
resets a gauge to zero. Readiness still covers enrollment migrations and the
`agent_enrollment` cleanup worker; a non-empty queue is an operational alert
rather than a liveness failure.

Recommended alerts (add to the list above):

- `beat_agent_enrollment_cleanup_total{result="failed"}` increases;
- `beat_agent_enrollment_pending_age_seconds > 3600` for 15 minutes;
- `rate(beat_agent_enrollment_challenges_total{outcome="denied"}[10m])` stays above
  the expected registration rate for 15 minutes (repeated bad signatures);
- `beat_agent_enrollment_grants{state="active"} == 0` while new registrations are
  expected (capacity exhaustion);
- `beat_agent_enrollment_claims_total{outcome="unavailable"}` increases (claim
  recovery spike);
- `beat_agent_enrollment_polls_total{outcome="limited"}` increases (throttled
  Agents).

The owner-only enrollment workbench writes redacted `agent.enrollment.*` audit
rows for grant create, update, rotate and revoke, request create, resume, approve,
reject and policy conflict, plus claim and claim recovery. Public protocol paths
record an anonymous actor with the coarse source address and the request ID.

## Trusted reverse proxies

Forwarding headers are ignored by default. Set `BEAT_TRUSTED_PROXIES` to a
comma-separated CIDR allowlist only when Beat is directly behind those
proxies, for example:

```bash
BEAT_TRUSTED_PROXIES=127.0.0.1/32,::1/128,10.20.0.0/16
```

Beat accepts `Forwarded` first and otherwise uses `X-Forwarded-For`,
`X-Forwarded-Proto`, and `X-Forwarded-Host`. Scheme and host use the rightmost
value added by the nearest trusted proxy. Client IP walks
`X-Forwarded-For` from right to left across trusted hops. Direct or untrusted
clients cannot influence Secure cookies, HSTS, origin checks, or audit IPs.

Nginx should overwrite, not append, scheme and host headers:

```nginx
location / {
    proxy_pass http://[::1]:8080;
    proxy_set_header Host $host;
    proxy_set_header X-Forwarded-Host $host;
    proxy_set_header X-Forwarded-Proto $scheme;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
}
```

Caddy's standard `reverse_proxy [::1]:8080` behavior is compatible. Keep the
CIDR list limited to the actual Caddy address or network.

## SQLite and migrations

Startup enables and verifies persistent WAL mode with bounded lock retries.
Every SQLite connection enables foreign keys, a 5-second busy timeout, and
immediate write transactions without attempting to change the persistent
journal mode again. The database, WAL, and shared-memory sidecar are all
secured to `0600`, including first startup under a permissive process umask.
Applied migrations are recorded in `schema_migrations`. Startup is idempotent,
supports concurrent initialization, and rejects a database whose schema version
is newer than the binary.

SQLite is the platform application-data store only. Agent resource samples,
derived traffic deltas, and network-probe numeric history are stored in MTS.
To audit a stopped copy or a read-only live database without changing it:

```bash
sqlite3 -readonly /var/lib/beat/beat.db '.tables'
sqlite3 -readonly /var/lib/beat/beat.db \
  "SELECT m.name, p.name, p.type FROM sqlite_master AS m \
   JOIN pragma_table_info(m.name) AS p WHERE m.type = 'table' ORDER BY m.name, p.cid"
```

Expected metric-like SQLite fields are application records only:
`alert_rules.metric` and `threshold` define policy, `alert_events.value` records
the triggering business event, `admin_backups.metrics_bytes` and `metric_rows`
describe backup artifacts, and `nodes.cpu_model` is inventory text. A metric
sample table, a latest-value cache, or columns named after `model.MetricNames()`
violate the storage boundary and must block release.

Migrations are forward-only. There is no in-place downgrade. Upgrade and
rollback must treat the binary, SQLite database, MTS directory, root encryption
key, wrapped data-key registry after `v9`, and restore/rotation journals as one
versioned unit. Always create a stopped-service data backup before changing
versions or rotating the root key.

### Local migration status before an upgrade

`beat-server admin-recovery migration-status --data-dir /var/lib/beat` prints one
JSON report and changes nothing: database presence, private modes, integrity,
current schema, the supported range, the ordered pending migrations, the fixed
`future_schema`/`ledger_invalid`/`integrity_failed`/`lock_conflict`/
`pending_restore`/`insecure_mode` codes, the last sanitized migration marker and
the `backups/` archive count. It takes the same data-directory lock as startup,
so a running server reports `lock_conflict` instead of reading live bytes, and it
inspects a private copy rather than the live database. Startup publishes the
bounded atomic `migration.status.json` (`0600`) so the state survives a failed
migration with no recovery listener: `idle`, `checking`, `applying`, `failed` or
`completed`. `rollback_required` is set only for `future_schema`; a failed
migration transaction leaves the previous binary matching the source schema.

### Backup and upgrade preflight before a version change

`beat-server admin-recovery migration-check --data-dir /var/lib/beat --backup
/var/lib/beat/backups/<archive>.zip` proves eligibility without touching live
bytes. It holds the same data-directory lock as startup (a running server
reports `lock_conflict`), validates the archive and its checksums, the archived
SQLite integrity and ledger, the supported source/target range and a free-space
floor of twice the archive size plus 256 MiB. It replays the canonical migrations
on a private `0700` copy, checks integrity, foreign keys, ledger continuity and
table/index inventory equivalence against a freshly created database, compares
live size/mtime/inode fingerprints before and after, and removes only its own
staging. The single JSON report uses fixed codes (`ok`, `archive_unreadable`,
`archive_incompatible`, `insufficient_space`, `schema_mismatch`,
`migration_failed`, `lock_conflict`, `pending_restore`, `future_schema`,
`ledger_invalid`, `integrity_failed`) and never prints SQL, paths, digests or
archive contents. Success proves the upgrade path; startup remains the only
component that migrates production data.

The owner-only `Migration and recovery` section in the Remote operations surface
shows the same bounded state read-only (current and supported schema, latest
completed migration, pending migrations, restore staged/pending, application and
MTS health, last sanitized failure code). It has no start, rollback, database
switch or restart control; recovery stays a local, filesystem-authorised action.

### Live upgrade rehearsal, 2026-10-14 (schema v7 to v16)

The deployed instance still ran schema v7. Before touching it, the worktree binary
(`0.2.0-dev+worktree.20261014`, commit `14fae14+dirty`) migrated a consistent
snapshot of the live database taken through the SQLite backup API:

- `schema_migrations` reached 16, `PRAGMA integrity_check` returned `ok`, and
  `PRAGMA foreign_key_check` reported no violations.
- 54 tables were added, none removed, and no shared table lost rows.
- `metric_definitions` seeded all 34 built-in metrics. An empty
  `metric_dashboards` table is expected: the API falls back to
  `model.DefaultMetricDashboard(scope)` until an administrator saves a layout.

Staged release for that upgrade (binaries, `static/`, unit template, 149-file
`SHA256SUMS`): `/tmp/beat-release-worktree-v16-20261014T1447Z`, with the
stop, backup, swap, verify and rollback sequence in its `DEPLOY.md`.

## MTS time-series store ownership

Every `NewMTSStore` call takes an exclusive `flock` on a persistent
`.beat-mts-owner-<sha256>.lock` file in the parent of the MTS directory before
the engine opens any shard. The lock file is `0600`, owned by the service
account, and is deliberately never deleted: releasing a file descriptor while
removing the path would open a window where a second process could lock a fresh
inode. A second server instance, or any offline tool, pointed at a live MTS
directory fails at startup with `initialize mts store: MTS store is already
open (held by pid N)` instead of writing next to the running process.

`/readyz` verifies that the lock path still resolves to the inode this process
opened. Deleting or replacing the lock file (for example with a cleanup script
that prunes unknown files from the data directory) makes the `mts` check report
`error` and metric APIs fail closed; restart the service to recreate the lock.
Do not prune `*.lock` files from the data directory, and stop the server before
running offline maintenance that needs to open the same MTS directory. The
data-directory lifecycle lock already makes the `beat-server` recovery commands
refuse to run while the service is up.

## Container deployment

Create the environment file with mode `0600` before starting Compose:

```bash
install -m 0600 deploy/beat.env.example deploy/beat.env
docker compose up -d --build
curl --fail http://127.0.0.1:9180/readyz
```

The image runs as UID/GID `65532`, drops Linux capabilities, uses a read-only
root filesystem under Compose, and writes only to the `beat-data` volume.
Application state is under `/var/lib/beat/data`; the parent volume directory
contains the stable lifecycle lease. This layout lets an unprivileged server
retain its lease while restore replaces files or directories below `data`.

Volumes from the previous flat layout are rejected before server startup when
old SQLite/WAL/SHM, MTS, key, backup/theme or restore/rotation state is found at
the volume root. Stop every container using that volume, make a complete cold
copy, and move all application state together into a private `data/` directory
owned by UID/GID `65532`. Preserve SQLite, MTS, the matching key, themes, backups,
SSH trust files and pending recovery journals as one set. Do not initialize a
fresh database over a partially migrated volume. Keep the cold copy for rollback.

For offline root-key rotation, use a temporary container with the same image
version and the same volume mounted at `/var/lib/beat`; pass
`secrets rotate-root --data-dir /var/lib/beat/data` and run that explicit offline
command as root. The normal server must be stopped first. A host alias for the
volume is not interchangeable: the lease name binds the canonical data path,
so the CLI must see the same container path as the server.

## systemd deployment

For the current workspace instance and its existing data path, use the
[local deployment notes](local-systemd-deployment.md). The following examples
describe a fresh deployment with the standard `/var/lib/beat` layout.

Verify and extract a release, then run its installer on Linux with systemd,
Bash, Python 3, GNU tar/coreutils, curl and flock available. Keep
`scripts/state_archive.py` beside `beatctl.sh`; Python uses only its standard
library and is checked before the service is stopped.

```bash
sudo ./scripts/beatctl.sh install "$PWD" 1.0.0
curl --fail http://127.0.0.1:8080/readyz
```

The installer creates a dedicated non-login `beat` account. It rejects root,
interactive, shared, or unrelated existing accounts and custom unit files.
Every layout knob is an optional environment variable; the defaults reproduce
the deployment above exactly:

| Variable | Default | Effect |
| --- | --- | --- |
| `BEAT_PREFIX` | `/opt/beat` | release root and `current` symlink |
| `BEAT_CONFIG_DIR` | `/etc/beat` | private configuration and deployment lock |
| `BEAT_STATE_DIR` | `/var/lib/beat` | database, metric store and account home |
| `BEAT_BACKUP_DIR` | `/var/backups/beat` | cold recovery checkpoints |
| `BEAT_UNIT_DIR` | `/etc/systemd/system` | installed unit directory |
| `BEAT_SERVICE_NAME` | `beat-server.service` | managed unit name |
| `BEAT_LISTEN_ADDR` | `:8080` | server `-listen-addr`; readiness probes the same port on loopback |
| `BEAT_SERVICE_ACCOUNT` | `beat` | dedicated unprivileged account and group |

```bash
sudo BEAT_LISTEN_ADDR=127.0.0.1:8081 BEAT_SERVICE_ACCOUNT=beatsvc \
  ./scripts/beatctl.sh install "$PWD" 1.0.0
```

The account name must be a lowercase system account name; `root` and other
system accounts are rejected. The listen address must be `host:port` (an empty
host, `0.0.0.0`, `[::]` or `*` means all interfaces) and its port must be
between 1 and 65535; the readiness probe always targets `127.0.0.1` with that
port. Install, upgrade and uninstall all read the same variables, so a
deployment must be upgraded and removed with the values it was installed with.
Programs and directories use mode `0700` and belong to `beat`; static files
use `0600`. The service's mount namespace makes the program tree read-only;
only `/var/lib/beat` is writable application state. Configuration and recovery
checkpoints remain root-private. On first installation, a random bootstrap
token is saved in `/etc/beat/beat.env` with mode `0600`; it is never printed.
Existing environment values are preserved.

Upgrade verifies and stages the release before stopping the service. It then
creates a cold checkpoint, switches the data directory and `/opt/beat/current`,
and checks systemd plus `/readyz` before completing:

```bash
sudo ./scripts/beatctl.sh upgrade "$PWD" 1.1.0
```

Rollback requires both the previous version and the exact state archive printed
by the upgrade command:

```bash
sudo ./scripts/beatctl.sh rollback 1.0.0 /var/backups/beat/beat-state-TIMESTAMP.SUFFIX/state.tar.gz
```

Rollback fully validates and prepares its archive before stopping the service.
Absolute/traversal paths, links, special/sparse files and duplicate members are
rejected; extraction is bounded to 100,000 members and 64 GiB. Failed upgrades
or rollbacks restore the previous program pointer, unit and matching data.
The checkpoint records `previous-target`, `previous.service` and the cold
archive; failed data is retained separately for diagnosis. If stopping or
recovery itself fails, preserve the reported checkpoint and directory paths
and resolve the systemd failure before retrying. Do not pair an old binary
with data already migrated by a newer one.

Uninstall disables and removes the managed unit while retaining the account,
programs, configuration, state and backups:

```bash
sudo ./scripts/beatctl.sh uninstall
```

## Release verification

CI and tag releases share `scripts/check.sh`; the complete local gate is:

```bash
./scripts/check.sh all
```

Its `backend`, `frontend`, `scripts` and `container` stages are also available
for diagnosis. Formatting and module checks are read-only, so existing local
changes are allowed; any additional normalization difference fails the gate.
The complete gate includes race/coverage/lint/security/build checks, isolated
installer tests, workflow validation and a rootless container startup check.
Partial stages do not authorize a commit or release.

Release creation waits for that complete workflow. GoReleaser first prepares
archives and SBOMs without publishing; checksums and the Sigstore signature
are validated before assets are uploaded to a draft. The draft becomes public
only after all uploads succeed. Installer tests use fake system commands and
signers; they never modify live accounts/services or contact signing services.

Each release publishes archives, matching SBOMs, SHA-256 checksums and a keyless
Sigstore bundle. Use the trusted repository and exact tag when verifying:

```bash
BEAT_RELEASE_REPOSITORY='OWNER/beat'
BEAT_RELEASE_TAG='v1.0.0'
cosign verify-blob --bundle checksums.txt.bundle \
  --certificate-identity "https://github.com/$BEAT_RELEASE_REPOSITORY/.github/workflows/release.yml@refs/tags/$BEAT_RELEASE_TAG" \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com checksums.txt
sha256sum --check checksums.txt
```


### Authentication after restore

A restored instance requires a fresh login. Before activating the restored
SQLite copy, Beat revokes archived administrator sessions and recent identity
proofs and removes pending OIDC transactions. If that cleanup cannot persist,
restore stops before replacing the existing database. Owner accounts, password
hashes, MFA configuration and external identity links remain from the backup.
