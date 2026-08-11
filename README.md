# Beat

Beat is a small server monitoring and operations service. It includes a public
React dashboard, an authenticated admin panel, a Go server, and `beat-agent`.

Commercial readiness and current competitor-parity gaps are tracked in
[`docs/beat/commercial-readiness-roadmap.md`](docs/beat/commercial-readiness-roadmap.md).

## Requirements

- Go 1.26.5+
- Node.js 20+

## Run the server

Build the frontend and server:

```bash
cd frontend
npm install
npm run build

cd ../backend
go build -o beat-server ./cmd/server
```

Set the one-time administrator bootstrap token and, during Agent migration only,
the legacy shared Agent token:

```bash
export BEAT_ADMIN_TOKEN='replace-with-an-admin-token'
export BEAT_AGENT_TOKEN='replace-with-the-legacy-agent-token'
export BEAT_TRUSTED_PROXIES=''
export BEAT_LOG_LEVEL='info'

./beat-server \
  -db-path ./data/beat.db \
  -mts-path ./data/beat_mts \
  -static-dir ../frontend/dist \
  -listen-addr :8080
```

The public dashboard and read-only monitoring APIs do not require login. The
admin panel is available at `/admin`; on a fresh database, use
`BEAT_ADMIN_TOKEN` once to create the first `owner` account. Normal admin access
then uses username/password, optional TOTP, and a server-side session carried by
an HttpOnly SameSite cookie. Terminal WebSockets use the same session and
same-origin checks. No administrator credential is stored in Web Storage.

`BEAT_TRUSTED_PROXIES` is disabled when empty. Configure it only with the CIDRs
of reverse proxies that connect directly to Beat; trusted HTTPS forwarding then
drives Secure cookies, HSTS, origin checks, and audit client IPs.

Operational endpoints are public and automation-friendly: `/healthz` provides
liveness, `/readyz` checks SQLite, MTS, restore state, and schedulers, and
`/metrics` emits bounded-cardinality Prometheus metrics. See the
[`operations runbook`](docs/beat/operations-runbook.md).

`BEAT_ADMIN_TOKEN` is bootstrap authority, not a long-lived browser credential.
After the first owner exists, it no longer authorizes normal admin operations.
Create nodes from `/admin/nodes`; each node receives an independent Agent
token whose plaintext is shown only once. SQLite stores only the token hash and
display prefix. `BEAT_AGENT_TOKEN` may be removed after all legacy nodes have
been migrated, without disabling administrator authentication.

Server data directories are secured to `0700`; the SQLite database, WAL/SHM
sidecars, and SSH `known_hosts` file are secured to `0600`. SSH host keys use
trust on first use (TOFU); a changed host key is rejected.

SQLite stores platform configuration, node identity, and static system assets.
All numeric samples reported by agents, traffic deltas, and billing-cycle
aggregations are stored or calculated only in MTS.
Site identity and presentation are managed from `/admin/settings`. The public
site title, description, logo, favicon, default theme, IP visibility, and
network-quality visibility are persisted in SQLite and exposed through a
public read-only endpoint; updates remain administrator-only. Disabling public
IP display also redacts node hosts from REST and live WebSocket responses. See
[`docs/beat/site-theme-settings-design.md`](docs/beat/site-theme-settings-design.md).
The same page provides administrator-only data maintenance. MTS metric history
defaults to 30 days and can be configured from 1 to 3650 days; automatic and
manual runs share one lock, delete only expired MTS measurements, compact MTS,
and checkpoint, verify, vacuum, and optimize SQLite without deleting platform
rows. Storage usage and the latest maintenance result are shown in the admin
panel. See
[`docs/beat/data-retention-maintenance-design.md`](docs/beat/data-retention-maintenance-design.md).
Scheduled ICMP, TCP, and HTTP checks follow the same boundary: SQLite stores
task definitions and node assignments, while every probe sample and history
aggregation is stored in MTS. Public quality views require no login; task
management remains under the authenticated admin panel at `/admin/network`.
Alert rules can target billing-cycle traffic usage percentage; active events
suppress duplicate notifications while a threshold remains exceeded.
Alert rules can also target Agent heartbeat age. Nodes become offline after 90
seconds without a report, availability rules add a configurable debounce, and
the same enabled channels receive both triggered and resolved notifications.
The admin alert page supports webhook, Telegram Bot API, and SMTP email
channels. Each channel can be test-sent before use and shows its latest delivery
result. Telegram and SMTP secrets are currently plaintext in the existing
SQLite channel config but are never returned by management APIs; leaving a
secret blank while editing keeps the stored value. Delivery status is runtime
state and resets when the server restarts. The reviewed encrypted replacement is
[`docs/beat/application-secret-lifecycle-design.md`](docs/beat/application-secret-lifecycle-design.md).

The same admin page manages daily, weekly, and monthly traffic reports. Each
schedule selects an IANA time zone, local send time, nodes, and notification
channels. SQLite stores only the schedule, execution cursor, and latest result;
the report body is aggregated from MTS for the completed local period. The
scheduler claims each period before delivery to prevent duplicate sends after a
restart, while manual test-runs leave the automatic execution cursor unchanged.
See [`docs/beat/scheduled-traffic-report-design.md`](docs/beat/scheduled-traffic-report-design.md).

Node presentation is managed from `/admin/nodes`: administrators can order
nodes within a group, add tags, control public visibility, and maintain separate
public and private remarks. Hidden nodes continue reporting and remain available
to authenticated operations, but are excluded from every public node, metrics,
WebSocket, and network-quality response.

## Backup and restore

Owners manage backups from the Backup tab under `/admin`. Creating, downloading,
validating, deleting, or staging a restore requires an authenticated owner;
sensitive actions also require recent password/TOTP confirmation. Each archive
contains a consistent SQLite snapshot, a typed logical MTS export, the root data
key, a manifest, and SHA-256 checksums. Backup files are stored with mode `0600`
but are not independently encrypted because the archive contains both
recoverable credentials and their key.

Uploaded archives are validated before they can be staged. To stage a restore,
enter the exact confirmation phrase `RESTORE BEAT`. The live database is not
modified immediately: restore is applied before SQLite and MTS open on the next
Server start, with a journaled rollback set retained if replacement fails.
Backups contain authentication material and SSH private keys, so handle them as
credential bundles and keep external copies on independently encrypted storage.
See [`docs/beat/backup-restore-design.md`](docs/beat/backup-restore-design.md).

## Run beat-agent

Build the agent:

```bash
cd backend
go build -o beat-agent ./cmd/agent
```

Create a node in `/admin/nodes`, then use the one-time JSON configuration with
mode `0600`:

```json
{
  "server_url": "http://127.0.0.1:8080",
  "agent_token": "beat_agent_v1_replace-with-the-per-node-token",
  "node_name": "node-one",
  "advertised_host": "10.0.0.10",
  "ssh_port": 22,
  "report_interval": "15s"
}
```

```bash
chmod 0600 ./agent.json
./beat-agent --config ./agent.json
```

The agent reports CPU percentage and logical core usage, memory usage and
capacity, root filesystem capacity, disk I/O, and network I/O metrics. Its
`agent_token` is bound to exactly one pre-created node. The admin node card
shows the Agent version and supports configuration display, token rotation,
and revocation.

## Container and systemd deployment

The repository includes a rootless multi-stage `Dockerfile`, hardened
`compose.yaml`, a systemd unit, and versioned install/upgrade/rollback tooling.

```bash
install -m 0600 deploy/beat.env.example deploy/beat.env
docker compose up -d --build
```

Release archives are produced by GoReleaser with checksums and SBOMs; the
release workflow signs the checksum manifest with Sigstore. Deployment and
rollback commands are documented in the
[`operations runbook`](docs/beat/operations-runbook.md).

## SSH operations

- The current PoC connects as the `root` SSH user.
- A node must be assigned a managed SSH key containing a private key.
- Public-only imported keys can be assigned to nodes but cannot authenticate a
  terminal or batch command session.
- Batch commands support up to 50 online nodes, with five concurrent workers
  and a 30-second request timeout.

## Development

Run the frontend development server:

```bash
cd frontend
npm run dev
```

It proxies `/api` to `http://localhost:8080`.

Run the project quality gates:

```bash
cd backend
goimports-reviser -rm-unused -format -company-prefixes github.com/beat/backend ./...
golangci-lint run ./...
go test -race ./...
go test -coverprofile=coverage.out ./...
go tool cover -func=coverage.out
go vet ./...
govulncheck ./...
go mod verify

cd ../frontend
npm audit --registry=https://registry.npmjs.org --omit=dev --audit-level=high
npm test -- --maxWorkers=1
npm run test:coverage -- --maxWorkers=1
npm run lint
npm run build
```

The repository requires at least 90% Go statement coverage and 90% frontend
line coverage.

## License

MIT
