# Beat operations runbook

Updated: 2026-08-01

## Runtime contract

Beat exposes three unauthenticated operational endpoints:

- `GET /healthz`: process liveness and build version;
- `GET /readyz`: SQLite schema, MTS, staged restore, and scheduler readiness;
- `GET /metrics`: Prometheus text metrics with bounded labels.

The deployed `schedulers` readiness check currently confirms only that the
background startup path ran. It does not prove individual alert,
traffic-report, or maintenance worker liveness or recent success. Until the
reviewed [`runtime-resilience-design.md`](./runtime-resilience-design.md) is
implemented, operators must correlate readiness with scheduler error logs and
delivery/maintenance timestamps rather than treating `schedulers=ok` as a
per-worker health guarantee.

The metrics endpoint intentionally avoids usernames, node names, node IDs,
hosts, and request URLs. Restrict `/metrics` at the reverse proxy when the
monitoring network is private.

Recommended alerts:

- `/readyz` is non-200 for 2 minutes;
- `beat_readiness_check == 0` for 2 minutes;
- `beat_agents_online < beat_agents_total` for 3 minutes;
- `beat_agent_max_heartbeat_age_seconds > 90` for 3 minutes;
- `beat_notification_deliveries_total{result="failed"}` increases;
- `beat_backups_total{state="failed"} > 0`;
- SQLite or MTS storage grows faster than the configured retention policy.

After the separately approved runtime-resilience batch is deployed, replace the
aggregate scheduler alert with per-worker up, last-success-age, failure, lag,
snapshot-build, active-WebSocket, rejected-upgrade, and dropped-snapshot alerts
using the fixed metric names in that design. These metrics are not part of the
current deployed contract yet.

The current deployment also lacks unified HTTP/KDF/history-query/Agent
admission metrics. Reverse-proxy rate limits can reduce exposure but do not
replace application-aware node/session identity or MTS query-cost controls. The
future metric and alert contract is reviewed in
[`ingress-resource-governance-design.md`](./ingress-resource-governance-design.md)
and must not be treated as deployed until that batch passes its load gate.

The current Agent report path can update SQLite contact/inventory and then fail
its MTS write. Treat `last_seen` as contact evidence only; confirm current MTS
points and MTS health before concluding telemetry is fresh. Stable report
receipts, deterministic retry, and explicit telemetry age are reviewed in
[`agent-ingest-consistency-design.md`](./agent-ingest-consistency-design.md).

Current MTS read failure visibility is not uniform. Node REST/history and
traffic reports propagate engine query errors, while alert evaluation silently
skips a failed resource metric. The MTS engine also returns an empty successful
query after its store is closed, so an empty chart or absent alert sample alone
does not prove there was genuinely no data. During an incident, correlate MTS
readiness, storage logs, traffic-report failures, and the age of the last known
point; do not resolve an alert or declare recovery from an empty result. The
explicit lifecycle guard and per-consumer failure metrics are owned by
[`runtime-resilience-design.md`](./runtime-resilience-design.md) and the typed
no-data/error query contract by
[`metric-catalog-dashboard-design.md`](./metric-catalog-dashboard-design.md).

The current application cleanup is also incomplete: expired sessions are not
cleaned in production, alert events and backup records are unpaged, audit
cleanup depends on MTS maintenance, and interrupted backup state is not
reconciled. Until reviewed migration `v19` is deployed, monitor SQLite/WAL and
backup-directory growth, inspect failed/running backup records after restart,
and never assume a failed stage/delete request had no filesystem or journal
effect. The durable replacement is
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

The current session boundary is not yet a complete sensitive-operation policy.
Recent authentication is enforced for selected account and backup actions, but
not for administrator creation, Agent/SSH credential changes, notification
endpoint/secret changes, terminal admission, or batch commands. Beginning TOTP
setup writes an unverified replacement and disables the current factor in
SQLite; do not initiate setup casually on an enabled account. Revoking a
session or disabling/demoting a user prevents the next HTTP/upgrade request but
does not terminate an already-open terminal WebSocket. Protected JSON responses
also lack a central `no-store` header. Until the reviewed route-policy batch is
deployed, restrict reverse-proxy caching for `/api/v1/auth/**` and every
authenticated `/api/v1/**` route, limit backend access to trusted operators,
and explicitly close terminal sessions after account/session changes.

The current static-secret boundary is also incomplete. TOTP ciphertext uses a
separate `0600` AES-GCM key, but has no AAD, envelope version, key ID, or rotation;
SSH private keys and Telegram/SMTP/Webhook credential-bearing configuration are
plaintext in SQLite and WAL. API redaction does not protect a copied database.
Restrict read access to the complete data directory, avoid copying a live
SQLite/WAL file without the corresponding root key and backup procedure, and
rotate affected remote/channel credentials after any suspected database
disclosure. Migration `v5` and then `v6` replace this with the reviewed shared
[`application-secret-lifecycle-design.md`](./application-secret-lifecycle-design.md).

Generated Beat backups contain both recoverable secret ciphertext and the
matching root key. Mode `0600` prevents ordinary local users from reading them,
but the ZIP is not an independent encryption boundary. Store off-host copies on
an encrypted volume/object store or wrap them with the organization's backup
KMS before transport or retention. Never separate or independently replace the
SQLite snapshot and root key during restore.

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
key, wrapped data-key registry after `v5`, and restore/rotation journals as one
versioned unit. Always create a stopped-service data backup before changing
versions or rotating the root key.

## Container deployment

Create the environment file with mode `0600` before starting Compose:

```bash
install -m 0600 deploy/beat.env.example deploy/beat.env
docker compose up -d --build
curl --fail http://127.0.0.1:9180/readyz
```

The image runs as UID/GID `65532`, drops Linux capabilities, uses a read-only
root filesystem under Compose, and writes only to the `beat-data` volume.

## systemd deployment

Extract a release archive, edit its environment template, then use the
versioned installer:

```bash
sudo ./scripts/beatctl.sh install "$PWD" 1.0.0
sudoedit /etc/beat/beat.env
sudo systemctl restart beat-server.service
curl --fail http://127.0.0.1:8080/readyz
```

Upgrade stops the service, archives `/var/lib/beat`, installs the new release,
switches `/opt/beat/current`, and starts the service:

```bash
sudo ./scripts/beatctl.sh upgrade "$PWD" 1.1.0
```

Rollback requires both the previous version and the exact state archive printed
by the upgrade command:

```bash
sudo ./scripts/beatctl.sh rollback 1.0.0 /var/backups/beat/beat-state-TIMESTAMP.tar.gz
```

Uninstall disables the unit but deliberately retains binaries, configuration,
state, and backups for manual recovery:

```bash
sudo ./scripts/beatctl.sh uninstall
```

## Release verification

Release archives include `beat-server`, `beat-agent`, static assets, deployment
examples, SHA-256 checksums, an SBOM, and a keyless Sigstore bundle. Verify the
checksum and signature before installation:

```bash
sha256sum --check checksums.txt
cosign verify-blob --bundle checksums.txt.bundle checksums.txt
```
