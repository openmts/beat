# Beat

Beat is a small server monitoring and operations service. It includes a public
React dashboard, an authenticated admin panel, a Go server, and `beat-agent`.

Commercial readiness and current competitor-parity gaps are tracked in
the [delivery plan](docs/beat/commercial-delivery-plan.md) and
[roadmap](docs/beat/commercial-readiness-roadmap.md).

## Requirements

- Go 1.26.10+
- Node.js 22.12+
- Bash and Python 3 for the Linux systemd installer and cold-backup tooling

`npm run build` also packs the bundled example themes, so the frontend build
expects the Go toolchain on `PATH`.

## Run the server

Build the frontend and server:

```bash
cd webui
npm ci
npm run build

cd ..
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
  -static-dir ./webui/dist \
  -listen-addr :8080
```

`-static-dir` is read from disk on every request, so a rebuilt `webui/dist` is picked up without restarting the server; hard-refresh the browser afterwards because asset hashes change.

The public dashboard and read-only monitoring APIs do not require login. The
admin panel is available at `/admin`; on a fresh database, use
`BEAT_ADMIN_TOKEN` once to create the first `owner` account. Normal admin access
then uses username/password, optional TOTP, and a server-side session carried by
an HttpOnly SameSite cookie. Creating, editing, and deleting administrator accounts
requires an owner with recent identity confirmation. Role changes and disablement
revoke account sessions; password changes retain only the current session.
TOTP setup is session-bound and expires after five minutes. Failed factor updates
preserve the current factor and pending setup for retry.
Administrators can additionally link OpenID
Connect identities: an owner configures providers (issuer, client ID, secret,
scopes) in `/admin/security`, and any administrator can link their own external
identity once; login then shows one button per enabled provider. Provider
secrets and PKCE verifiers are stored as AES-GCM envelopes bound to their
resource, and provider management, linking, and unlinking require recent local
reauthentication. Terminal WebSockets use the same session and same-origin
checks. Logout, session revocation, and account disablement close active
terminals; expiry and externally changed sessions are checked every second
without extending idle time. Each administrator can run up to four concurrent terminal or batch operations,
with a server-wide limit of 32. Active terminal sessions are exposed as
`beat_terminal_sessions_active`, and terminated before storage closes during
graceful shutdown. No administrator credential is stored in Web Storage.
TOTP setup requires recent identity confirmation and stays pending for five
minutes; the existing factor remains active until the new code is verified.
Local bootstrap, login, logout and recent identity confirmation commit together
with their audit event. Failed audit writes leave accounts and sessions unchanged;
login rechecks account credentials before granting the session.

SQLite v9 encrypts TOTP, OIDC and SSH credentials with resource-bound data keys.
Owners can inspect key health and rotate data keys under `/admin/security` →
Application keys; rotation requires fresh password/TOTP confirmation and resumes
after interruption. After stopping the server, root can rotate the root key with
`beat-server secrets rotate-root --data-dir /var/lib/beat`. See the
[key lifecycle guide](docs/beat/application-secret-lifecycle-design.md).

`BEAT_TRUSTED_PROXIES` is disabled when empty. Configure it only with the CIDRs
of reverse proxies that connect directly to Beat; trusted HTTPS forwarding then
drives Secure cookies, HSTS, origin checks, and audit client IPs.

Deployment access is owned by three environment variables and is never stored in
SQLite: `BEAT_PUBLIC_BASE_URL` is the canonical external HTTPS origin (loopback
HTTP is allowed for development), `BEAT_PUBLIC_API_ORIGINS` is a comma-separated
origin allowlist or the single value `*` (the default, preserving login-free
public reads), and `BEAT_PUBLIC_WS_ORIGINS` restricts the public metrics stream
and defaults to the canonical origin when one is configured. Malformed,
duplicated, wildcard-mixed or path-bearing values fail startup. Public node
addresses follow the `public_address_mode` site setting: `hidden` (default),
`masked` keeps only the first IPv4 octet or the first 32 IPv6 bits, and `full`
publishes the stored value; administration always returns the full address.

Anonymous interaction telemetry is off by default and owner-controlled at
`/admin/settings`. When enabled, the browser may post one fixed
`{event, surface}` pair from the fixed event and surface vocabulary to
`POST /api/v1/public/interactions`; the server enforces the deployment origin
policy, a 512-byte body, and 60 requests per minute per source. Counts live only
in MTS (`public_interaction_total`, `public_interaction_rejected_total`) and are
pruned by the policy retention, while SQLite stores the policy alone. The
browser keeps a per-session ceiling, sends no credentials, and never queues or
retries a failed report. See the
[deployment access design](docs/beat/deployment-access-policy-design.md).

Operational endpoints are public and automation-friendly: `/healthz` provides
liveness, `/readyz` checks SQLite, MTS, application keys, restore state, and individual workers, and
`/metrics` emits bounded-cardinality Prometheus metrics. See the
[`operations runbook`](docs/beat/operations-runbook.md).
The public `/api/v1/ws/metrics` stream uses one shared producer; legacy `/ws` and
`/metrics/ws` return 404. Owners can read `/api/v1/admin/runtime`; see the
[runtime backend guide](docs/beat/runtime-resilience-backend.md).
This workspace's installed systemd unit, retained data path, and cold backup
are recorded in the [local deployment notes](docs/beat/local-systemd-deployment.md).

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

The built-in **Metric workspace** at `/admin/metrics` provides saved dashboards,
panel editing, node/group variables, dashboard search tags, custom time ranges,
aggregation, thresholds, line width/fill/points/stacking, value mappings,
query/data inspection with CSV/JSON downloads, and a one-click path from any
panel into a prefilled alert rule. Dashboards carry shared
annotations with private/public visibility, frozen read-only snapshots behind a
revocable share link, a personal panel library, per-panel alert-rule
associations that render live firing/pending/no-data state, and a version
history that keeps the last 20 revisions per dashboard and restores any of them
as a new revision. Personal layouts
use revision checks; owners can publish public or per-node defaults after recent
identity confirmation. The metric catalog controls visibility and retention.
Panels cover time series, stat, gauge, bar gauge and table. Queries are bounded
and preserve missing samples as gaps. This is a Beat
experience inspired by Grafana; no Grafana server or PromQL service is required.
Queries interrupted by a hidden tab resume on return, even with auto refresh off.
Public node details use the published public/node template with read-only time,
refresh, inspection and download controls; sandbox themes receive the same
successful history with explicit gaps and aggregation metadata.
See the [workspace contract](docs/beat/metric-workspace-contract.md).

Notification channels support Webhook, Telegram, Email, Bark, ServerChan Turbo,
ServerChan 3 and custom HTTP. Channel configuration is encrypted; queued delivery,
attempt history, bounded retries and owner-controlled destination exceptions are
managed from `/admin/alerts`. Tests return a queued receipt; delivery history
shows the actual outcome. See the [notification contract](docs/beat/notification-core-contract.md).

Site identity and presentation are managed from `/admin/settings`. The public
site title, description, logo, favicon, default theme, IP visibility, and
network-quality visibility are persisted in SQLite and exposed through a
public read-only endpoint; updates remain administrator-only. Disabling public
IP display also redacts node hosts from REST and live WebSocket responses. See
[`docs/beat/site-theme-settings-design.md`](docs/beat/site-theme-settings-design.md).
The same page provides administrator-only data maintenance, a theme library
with upload, preview, layout settings, activation, recovery and export, and signed HTTPS market sources whose
catalogs are verified with Ed25519, show bounded cache status, and install the
selected version/digest without automatic activation. Sources must be public HTTPS
by default; set `BEAT_THEME_MARKET_ALLOW_PRIVATE=true` at server startup to allow
private, loopback or reserved addresses for self-hosted markets in offline networks. Themes apply only
to the public dashboard and node detail; the administrator interface always uses the built-in
shadcn surface. Seven editable declarative themes, one sandboxed reference theme and the
pack/validate CLI are documented in the [theme author guide](docs/beat/theme-author-guide.md);
signing and market publication use the
[market publisher CLI](docs/beat/theme-market-publishing.md).
Theme packages are stored under the writable data directory and
included in backup archives. MTS metric history
defaults to 30 days and can be configured from 1 to 3650 days; automatic and
manual runs share one lock, delete only expired MTS measurements, compact MTS,
and checkpoint, verify, vacuum, and optimize SQLite without deleting platform
rows. Storage usage and the latest maintenance result are shown in the admin
panel. MTS also compacts overlapping parts during flush with single-worker
concurrency; full maintenance keeps the backup/retention operation lock. An
on-disk owner lock makes a second server or offline tool fail at startup when
it points at a live MTS directory instead of writing alongside the running
process. See
[`docs/beat/data-retention-maintenance-design.md`](docs/beat/data-retention-maintenance-design.md).
Application history (resolved alert events and administrator audit logs) has its
own retention policy, default 180 days each, with an independent daily UTC
cleanup worker that also removes expired and revoked sessions; it runs even when
MTS age deletion is disabled. See
[`docs/beat/application-data-lifecycle-design.md`](docs/beat/application-data-lifecycle-design.md).
Scheduled ICMP, TCP, and HTTP checks follow the same boundary: SQLite stores
task definitions and node assignments, while every probe sample and history
aggregation is stored in MTS. Public quality views require no login; task
management remains under the authenticated admin panel at `/admin/network`.
The advanced alert editor supports node/group scopes, windows, recovery confirmation,
and separate NoData/Error policies. Evaluation and history tabs keep threshold phases
separate from evaluation health; previews query without sending notifications.
See the [v12 contract and integration checkpoint](docs/beat/alert-evaluation-design.md).

Owners can append an immutable traffic calibration when an imported or
provider-reported opening balance must be corrected: a `set` event declares the
accounted upload/download totals immediately after one effective second inside
the node's current billing cycle, and a `revoke` returns that cycle to raw MTS.
Corrections append one revoke plus one replacement set and never edit history.
Raw MTS rows, charts and daily/weekly/monthly reports stay unadjusted; only the
current-cycle public/admin summary, quota percentage and traffic alerts follow
the calibration, which the public summary exposes as `accounting_mode`
(`raw`/`calibrated`) plus a nullable `calibrated_at`. Calibration is owner-only,
requires recent reauthentication and is audit-logged without target values;
rejected attempts are audited as failures with a reason category only. Traffic
retention never deletes raw deltas inside an active billing cycle, and restore
validation replays the ledger before an archive is accepted.
See [`docs/beat/traffic-calibration-design.md`](docs/beat/traffic-calibration-design.md).

Beat also sends scheduled fleet status summaries from
`/admin/alerts` → `Status summaries`. Each schedule declares a daily or weekly
local time in an IANA time zone, optional ISO-week weekday, an explicit
group/node/channel scope and a name-list threshold (0-50, default 10). Counts
come from the same persisted availability state as availability alerts at the
schedule cutoff, names are rendered as display names only, and manual test
sends never advance the automatic cursor. Recovery is deterministic: a restart
claims at most one missed period per schedule and never sends a summary just
because the Server process started. Every delivery flows through the durable
notification outbox, so disabled channels are skipped and recorded instead of
being silently dropped.
See [`docs/beat/fleet-status-summary-design.md`](docs/beat/fleet-status-summary-design.md).

Owners can also erase metric history explicitly from
`/admin/settings` → `Data and storage` → `Erase history`. Erasure uses a closed
scope list (one node, one quality task, fleet telemetry, all metric history,
delete node, delete quality task), commits one durable job and answers `202`
before any MTS delete runs. Node and quality-task deletion therefore remove the
entity only after its history is gone. Fleet-wide scopes require the fixed
phrase `ERASE FLEET TELEMETRY` or `ERASE ALL METRIC HISTORY`, entity scopes
require the exact display name, and the dialog states the affected history
classes, whether the entity is removed and whether a ready backup from the last
24 hours exists; a missing recent backup forces an explicit create-backup action
plus a separate acknowledgement. Restoring an older backup is the only recovery
path, a successful erasure cutoff becomes the new accounting boundary for
traffic calibration, and every rejected request is audited with a reason
category only. See
[`docs/beat/metric-erasure-design.md`](docs/beat/metric-erasure-design.md).

Alert rules can target billing-cycle traffic usage percentage; active events
suppress duplicate notifications while a threshold remains exceeded.
Alert rules can also target Agent heartbeat age. The offline grace period follows
the trusted reporting interval with a 90-second minimum; availability rules add a configurable debounce, and
the same enabled channels receive both triggered and resolved notifications.
Alert rules can target custom metrics by selecting a custom metric name; the
custom metric value (measured by the agent) is evaluated against the rule's
threshold.
Alert events follow an SRE acknowledgement and resolution flow: a triggered
event can be acknowledged (moved to `acknowledged`) to signal that an operator
is handling it, and an `acknowledged` event is resolved manually. Acknowledged
events are not re-triggered while the condition persists and are not
auto-resolved when the metric recovers; acknowledgement and resolution both
push a notification to the enabled channels.
The admin alert page supports webhook, Telegram Bot API, and SMTP email
channels. Each channel can be test-sent before use and shows its latest delivery
result. Channels can define a custom push template using `{{rule}}`, `{{node}}`,
`{{metric}}`, `{{severity}}`, `{{value}}`, `{{status}}`, `{{time}}` and
`{{message}}` placeholders; leaving the template empty uses the default message.
Webhook channels can also set custom HTTP headers (one `Key: value` per line)
for authentication or routing, such as `Authorization` or `X-Api-Key`.
A webhook template is detected as JSON when the rendered template parses as
valid JSON, and is then sent as an `application/json` body; otherwise it is sent
as `text/plain`. This lets webhook channels wrap structured services (for
example a JSON telegram proxy) while still supporting plain text templates.
Telegram channels can pick a parse mode (`HTML`, `Markdown` or `MarkdownV2`);
dynamic placeholder values are escaped for `MarkdownV2` so characters like `.`
or `_` in rule or node names do not break message parsing.
Delivery failures contain sanitized status categories, never token-bearing
URLs or upstream response bodies. HTTP redirects are rejected.
Telegram and SMTP secrets are currently plaintext in the existing
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
Node, group and site changes commit with their audit evidence. Issuing or revoking
Agent credentials and changing an assigned SSH public key require recent identity
confirmation; token changes disconnect connections using the old credential.

## Backup and restore

Owners manage backups from the Backup tab under `/admin`. Creating, downloading,
validating, deleting, or staging a restore requires an authenticated owner;
sensitive actions also require recent password/TOTP confirmation. Each archive
contains a consistent SQLite snapshot, a typed logical MTS export, the root data
key, a manifest, and SHA-256 checksums. Backup files are stored with mode `0600`
but are not independently encrypted because the archive contains both
recoverable credentials and their key.

Uploaded archives are validated before they can be staged, including SQLite
integrity, migration history, foreign keys, and authentication of archived
TOTP/OIDC/SSH ciphertext and the wrapped key registry with the matching root key. To stage a restore,
enter the exact confirmation phrase `RESTORE BEAT`. The live database is not
modified immediately: restore is applied before SQLite and MTS open on the next
Server start, with a journaled rollback set retained if replacement fails. Only a
record left in `staged` state with the journal's random generation is applied.
The listing is cursor-paginated, and every Server start reconciles records with
their archives: interrupted runs, missing or quarantined archives, and
unconfirmed restore journals are resolved before HTTP serves, while terminal
records expire after a fixed 90-day window.
Backups contain authentication material and SSH private keys, so handle them as
credential bundles and keep external copies on independently encrypted storage.
See [`docs/beat/backup-restore-design.md`](docs/beat/backup-restore-design.md).
Stopped-service owner recovery and empty-volume restore are available through
`beat-server admin-recovery`; see the [local recovery runbook](docs/beat/operator-recovery-runbook.md).
Optional Agent operations use explicit local policy and dedicated roots; see the
[Agent operations runbook](docs/beat/agent-operations-runbook.md).

## Run beat-agent

Open **Node setup** at `/admin/agent-setup` to generate a one-command join for a
Linux machine with systemd and an existing `beat-agent` executable:

```bash
sudo ./beat-agent join --server https://beat.example.com --token '<join-token>'
```

Each generated token is different, expires after 30 minutes, and admits one
machine. Join creates the node, saves `/etc/beat-agent/agent.json` with mode
`0600`, installs the binary at `/opt/beat-agent/beat-agent`, and enables and
starts `beat-agent.service`. It uses the hostname and detected SSH port;
`--name`, `--host`, and `--ssh-port` override them. Use a server address reachable
from the target; `localhost` refers to the target itself. HTTP requires explicit
permission in the page and `--allow-http` in the generated command.

After an interruption, rerun the same command on the same machine to resume;
an unfinished join can use a fresh command after its token expires. For an
existing valid configuration or a saved enrollment receipt, recover with
`sudo ./beat-agent service install --config /etc/beat-agent/agent.json`.
To remove the service again, run
`sudo ./beat-agent service uninstall`; it stops and disables both
`beat-agent.service` and `beat-agent-update.service`, deletes only unit files
carrying the Beat managed marker plus the installed binaries, and keeps the
configuration and identity under `/etc/beat-agent`. Add `--purge` to delete that
directory's config and identity files as well.
Check `systemctl status beat-agent`, `journalctl -u beat-agent`, and the node's
heartbeat and metrics in the admin panel. Field reference and manual setup
remain available on the Node setup page. See the
[join protocol and recovery notes](docs/beat/agent-join-design.md).

**Node enrollment** at `/admin/enrollment` adds policy grants for fleets. An owner
creates a grant with an approval mode, capacity, expiry, default group, and
optional source CIDRs and hostname pattern, then copies the `beat-agent enroll`
command the dialog prints beside the one-time secret. The secret travels on
standard input, as a `0600` file, or through a protected environment variable. Discovered machines wait in *Pending approval* until an administrator
approves them with the final node fields, or are approved automatically by the
grant policy; each machine still receives its own node credential when it claims.
See the [enrollment design](docs/beat/agent-auto-discovery-design.md).

To build the agent from source:

```bash
go build -o beat-agent ./cmd/agent
```

Credential creation, rotation and revocation return node metadata independently
of metric storage. Lost responses cannot reveal a previous token; rotate again
to obtain a new one and invalidate the previous credential.

For manual foreground setup, create a node in `/admin/nodes`, then use the
one-time JSON configuration with mode `0600`:

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

Server and Agent use report protocol 2 and should be upgraded together. Failed
reports retry one immutable sample for up to 15 minutes; the UI distinguishes
accepted contact from the age of committed metrics. Unknown MTS write failures
require storage repair and a supervised server restart; see the
[recovery runbook](docs/beat/operations-runbook.md).

The agent reports CPU percentage and logical core usage, memory usage and
capacity, root filesystem capacity, disk I/O, and network I/O metrics. Its
`agent_token` is bound to exactly one pre-created node. The admin node card
shows the Agent version and supports configuration display, token rotation,
and revocation.

## Automate with beat-cli

`beat-cli` drives the data plane with a scoped service token created in
**Security → Service tokens**; it never uses a browser session. Release
archives ship the compiled `beat-cli`, so operators can extract it instead of
building from source.

```bash
go build -o beat-cli ./cmd/beat-cli
install -m 0600 /dev/null /etc/beat/service-token   # paste the one-time token
./beat-cli --server https://beat.example.com --token-file /etc/beat/service-token health
./beat-cli --server https://beat.example.com --token-file /etc/beat/service-token backups create
./beat-cli --server 'http://[::1]:8080' --token-file /etc/beat/service-token nodes list
```

Commands: `health`, `nodes list`, `nodes metrics <id>`,
`backups list|create|download` and `audit list`. Tokens come from
`--token-file`, `BEAT_SERVICE_TOKEN` or standard input, token files must be
`0600`, responses print the unwrapped `data` field (`--raw` keeps the envelope),
and denials exit non-zero with one bounded line. See the
[automation access design](docs/beat/automation-access-design.md).

The Agent version page discovers signed releases from `openmts/beat` first and
keeps manual signed import as an offline fallback. Synchronization is an explicit
owner action; importing a version never starts an upgrade. Configure
`BEAT_AGENT_RELEASE_REPOSITORY`, `BEAT_AGENT_RELEASE_API_URL` and the optional
read-only `BEAT_AGENT_RELEASE_TOKEN` when the defaults do not fit. See
[remote release management](docs/beat/agent-remote-release-management.md).

`beat-cli release keygen` and `beat-cli release sign` create matching
Ed25519 Agent/Server trust files and sign a canonical manifest. Tagged CI
releases additionally produce the canonical payload, raw Ed25519 signature,
Sigstore bundle and direct Linux Agent binaries consumed by the remote catalog.
The manifest remains subject to the existing size, digest, compatibility and
trust-policy checks. See the
[Agent update runtime](docs/beat/agent-update-runtime.md).

## Container and systemd deployment

The repository includes a rootless multi-stage `Dockerfile`, hardened
`compose.yaml`, a systemd unit, and versioned install/upgrade/rollback tooling.

```bash
install -m 0600 deploy/beat.env.example deploy/beat.env
docker compose up -d --build
```

Container state uses `/var/lib/beat/data`; the volume root retains the stable
lifecycle lock. Existing flat-layout volumes require a stopped-service backup
and explicit migration before startup. The [runbook](docs/beat/operations-runbook.md)
describes both layouts. Local builds identify as `0.2.0-dev`; release builds
replace that version with their signed release version.

Release archives are produced by GoReleaser with checksums and SBOMs; every
archive carries `beat-server`, `beat-agent`, `beat-cli` and `beat-theme`, and
the release workflow signs the checksum manifest with Sigstore. The tagged
release packs the validated frontend bundle from the quality workflow instead
of rebuilding it in GoReleaser. The release runner and GoReleaser version are
pinned. Deployment and rollback commands are documented in the
[`operations runbook`](docs/beat/operations-runbook.md).

## SSH operations

SSH key import, generation, deletion and private-key reveal require a recent
owner/admin identity confirmation. Writes and their audit evidence commit together;
failed audit writes cannot change credentials or disclose a private key.

- The current PoC connects as the `root` SSH user.
- A node must be assigned a managed SSH key containing a private key.
- Public-only imported keys can be assigned to nodes but cannot authenticate a
  terminal or batch command session.
- Batch commands support up to 50 online nodes, with five concurrent workers
  and a 30-second request timeout.

## Agent tasks

Agents keep a WebSocket channel open to the server (`/api/v1/agents/ws`) on top
of the heartbeat reports. The server enqueues tasks in SQLite and pushes them
to the agent over this channel; offline agents receive pending tasks after
reconnecting.

- When a node's assigned SSH key changes, the server enqueues an
  `authorized_key` task. The agent writes the key's public key into its own
  `~/.ssh/authorized_keys` idempotently, so repeated updates never duplicate
  entries.
- Tasks are persisted and replayed until the agent reports a result; failed
  tasks are not retried.
- Task results are sent back over the same WebSocket and recorded by the
  server; a transient failure to record one result is logged and the channel
  stays connected (the task remains pending and is redelivered on reconnect).
- The server pings the channel every 30 seconds and the agent answers with a
  pong. Both sides enforce read/write deadlines, so a half-open connection is
  detected within about 90 seconds and re-established automatically.
- On every heartbeat the agent also checks that its own `~/.ssh/authorized_keys`
  still contains the node's assigned key and re-installs it if it was removed
  externally, so a lost key self-heals automatically.

## Custom metrics

Custom metrics let operators collect arbitrary numeric values by running a
shell command on nodes.

- Define a metric with a name and a shell command whose stdout prints a single
  number, plus a collection interval (5s–24h).
- Bind metrics to nodes. The server sends a `custom_metric_register` task over
  the agent channel (and on every reconnect), so the agent registers and
  periodically runs the commands.
- Collected values are reported with the heartbeat and stored under the
  `custom_<name>` measurement, viewable through the node metrics API and the
  custom metrics page.

Custom commands require Linux and run as the agent's service account. Each node
supports 64 enabled definitions and four concurrent commands, independently of
system heartbeats. Each command has a 10-second deadline and a combined 64 KiB
stdout/stderr limit. Failed commands back off for 5–60 seconds; only finite,
completed samples are reported, once each and within one minute of collection.
Changing, disabling, deleting or unbinding a definition cancels its active
command and invalidates cached values. Metadata and full replacement tasks are
persisted atomically; failed deliveries remain queued for reconnect, while
superseded tasks retain their terminal state and audit history.

The metric catalog governs units, visibility, persistence and retention. Only
registered custom values bound to the authenticated node are accepted; cached
values for known disabled definitions are acknowledged and discarded. Owners
edit catalog policies with recent authentication and revision checks. Numeric
history stays in MTS, including history for deleted custom definitions; backup
format 3 exports and validates it against the frozen SQLite catalog.

Use `/api/v1/metrics/query` (or `/api/v1/admin/metrics/query`) for up to
eight series and 600 points per series, with aggregation and explicit null gaps.
Queries have a 31-day range, a five-second deadline, and shared global/client
concurrency limits. See the [metric workspace contract](docs/beat/metric-workspace-contract.md).

## Development

Run the frontend development server:

```bash
cd webui
npm run dev
```

It proxies `/api` to `http://localhost:8080`.

Run the project quality gates:

```bash
goimports-reviser -rm-unused -format -company-prefixes github.com/beat/backend ./...
golangci-lint run ./...
go test -race ./...
go test -coverprofile=coverage.out ./...
go tool cover -func=coverage.out
go vet ./...
govulncheck ./...
go mod verify

cd webui
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

Remote operations use `/admin/operations` for policies, scripts, command tasks and
file transfers. Set `BEAT_OPERATIONS_ENABLED=true` to enable the server feature;
Agents require their own local operations configuration. Policies requiring file
scanning also require `BEAT_OPERATIONS_SCANNER_EXECUTABLE` (absolute path) and
optional `BEAT_OPERATIONS_SCANNER_ARGS` (JSON string array) at server startup.
Policies and shared-version approvals are owner-only. Only a snippet's author
may edit or delete it; content reads and dispatch require recent authentication.
Controlled path and bandwidth diagnostics reuse approved targets and MTS-only
numeric results. Set `BEAT_DIAGNOSTICS_ENABLED=true` at server startup and opt in
per node from the Agent configuration; loopback, metadata and other forbidden
destinations are never reachable. See the
[advanced diagnostics design](docs/beat/advanced-network-diagnostics-design.md).

```json
{
  "diagnostics": {
    "enabled": true,
    "mtr_path": "/usr/bin/mtr",
    "iperf3_path": "/usr/bin/iperf3",
    "state_dir": "/var/lib/beat-agent/diagnostics",
    "allowed_targets": ["203.0.113.10", "10.0.0.0/8"],
    "allow_private_targets": true,
    "maxima": { "probes": 10, "hop_limit": 32, "duration_seconds": 10, "parallel_streams": 1, "bitrate_mbps": 100 }
  }
}
```

The Agent runs only these absolute binaries with fixed arguments, never a shell,
and only sends traffic to a destination that the server approved **and** that
falls inside `allowed_targets`; hostnames are never resolved locally. Private,
CGNAT and documentation ranges additionally require `allow_private_targets`.
Every field is documented on the admin node-setup page and checked by
`internal/agent/config_reference_test.go`.
Advanced alert rules use persisted scope/window/hysteresis and NoData/Error state
under SQLite v12. See the [Agent operations runbook](docs/beat/agent-operations-runbook.md).
GeoIP privacy defaults, licensed MMDB updates and recovery are documented in the
[GeoIP runbook](docs/beat/geoip-runbook.md).
The [signed Agent rollout foundation](docs/beat/agent-rollout-foundation.md) is a
disabled library; v15 runtime and Agent self-update integration remain pending.
Rollout backups declare excluded artifacts; restore pauses unfinished work,
invalidates old download credentials and requires verified artifact re-import.
Owners can review signed releases, artifacts and frozen rollout targets at
`/admin/agent-releases`; controls remain unavailable until the server reports ready.
Release retention is opt-in: set `BEAT_AGENT_ROLLOUT_RETENTION_DAYS` and/or
`BEAT_AGENT_RELEASE_RETENTION_DAYS` at server startup to delete expired terminal
rollouts and revoked, unreferenced releases. Both default to keeping history.
Optional Linux NVIDIA/AMD telemetry uses native sample cadence and MTS-only GPU
values; see the [accelerator runbook](docs/beat/accelerator-runbook.md).

The public Fleet supports URL-based search, filters and grouped card/table views.
HTTPS enables optional PWA installation with a static-only offline shell and
explicit updates. No metrics are cached.
Manage country/expiry/price visibility at `/admin/lifecycle`; scheduled status
summaries are configured at `/admin/alerts` → `Status summaries`.

The generated React components follow shadcn/ui. Its MIT-licensed Tailwind style
rules are kept locally under `webui/src/styles/shadcn`; the unused component CLI
is not installed as an application dependency. The distribution includes its
license notice under `static/licenses/shadcn.txt`.

SQLite v13 adds opt-in GeoIP and node lifecycle metadata. GeoIP providers default
to disabled; remote lookups require explicit disclosure consent. Backup format v4
includes managed MMDB assets, while an external `BEAT_GEOIP_MMDB_PATH` is recorded
as a dependency and is never copied or modified by restore.

SQLite v14 adds opt-in Linux NVIDIA/AMD GPU inventory and MTS history. Configure
node collection or new-node defaults at `/admin/accelerators`. An explicit node
disable overrides Agent installation opt-in. GPU collection requires per-node
Agent credentials and existing vendor tools; missing samples are never zero-filled.

GPU alert windows and incidents are isolated by rule, node and device. Empty
inventories have no targets; confirmed removal resolves only that device’s incident.

The fixed local v14 preview and Agent remain available while later batches are
integrated in the working tree. The preview includes the navigation fix; refresh an already
open tab once to load it. See [preview access and rollback](docs/beat/local-preview.md).
