# Beat commercial readiness roadmap

Updated: 2026-08-01

Status: active execution baseline

## Completion definition

Beat is not commercially complete merely because its monitoring pages work.
Completion requires all of the following to be demonstrated with current
source, automated tests, release artifacts, operational documentation, and a
deployed acceptance environment:

1. Functional parity with the current public capabilities of Komari and
   DStatus, except where a capability is intentionally rejected for a documented
   security or product reason and an equivalent safer workflow is provided.
2. Reliable operation through restart, partial storage failure, backup/restore,
   network interruption, Agent retry, and upgrade/rollback scenarios.
3. Secure operation behind a standard TLS reverse proxy, with explicit trust
   boundaries, secure cookies, origin checks, rate limits, secret rotation, and
   auditable privileged actions.
4. Operability through health/readiness endpoints, structured logs, metrics,
   release/version reporting, deterministic deployment artifacts, and concise
   runbooks.
5. Local CI-equivalent gates with at least 90 percent backend statement and
   frontend line coverage, race detection, lint, vulnerability scanning, build,
   browser smoke tests, and restore drills.

## Current evidence

The existing implementation already covers public dashboards, live metrics,
resource inventory, traffic accounting, network probes, node identity,
terminal and batch commands, alerts, scheduled reports, local accounts, TOTP,
sessions, audit events, data retention, and validated backup/restore.

The fixed competitor sources add capabilities that were absent from the old
Beat matrix. Server, Agent, and DStatus evidence is pinned to the inspected
commits; Komari Web evidence is pinned to stable release `1.3.2` at
`14f4067e9b69813b24a0255e565f7f49bff0a1bd` because its default branch was
force-rewritten to an older unrelated history:

- Komari Server: configurable OIDC providers, theme packages and a theme
  market, Agent auto-discovery, generic tasks, clipboard, file upload/download,
  GeoIP providers, more notification senders, profiling endpoints, GPU
  telemetry, a metric catalog with per-metric retention, configurable chart
  dashboards, login notifications, chunked backup upload, an administrator API
  key, wallboard presentation, local password/2FA/login recovery commands,
  first-run backup restore, explicit system/all/entity/task history deletion,
  external access/origin/privacy controls, terminal preferences, database
  recovery/migration guides, Docker and installation tooling.
- Komari Web stable `1.3.2`: fleet summaries, search/filter/sort, grid/table
  views, offline ordering, configurable metric dashboards, PWA metadata, and
  English, Simplified/Traditional Chinese, Japanese, and Indonesian catalogs.
- Komari Agent: default six-hour self-update checks and a broader
  Windows/Linux/macOS/FreeBSD architecture build matrix.
- DStatus `main`: Docker image, Compose deployment, installation/update script,
  responsive monitoring, fleet search/filter/sorting, PWA metadata, threshold
  alerts, scheduled/manual Fleet status summaries, SSH operations, multi-server
  management, and optional iperf3/MTR Agent diagnostics.

## P0 commercial foundation and residual blockers

### Reverse proxy and HTTPS correctness (implemented baseline)

Current evidence:

- trusted proxy CIDRs are disabled by default and gate all accepted forwarding
  headers;
- one canonical request identity supplies effective scheme, host, client IP,
  Secure cookies, HSTS, same-origin checks, request logs, and audit sources;
- direct/untrusted forwarding spoofing plus trusted HTTP/HTTPS, IPv4, and IPv6
  paths are covered by tests and deployed acceptance.

Required result:

- explicit trusted-proxy configuration, disabled by default;
- validated `Forwarded` or `X-Forwarded-Proto` handling only from trusted proxy
  addresses;
- one canonical helper for effective scheme, host, and client IP;
- Secure cookies and HSTS derived from the trusted effective scheme;
- spoofed forwarding headers ignored for direct clients;
- HTTP, HTTPS-direct, trusted-proxy, untrusted-proxy, IPv4, and IPv6 tests.

### SQLite invariants and concurrency (implemented baseline)

Current evidence:

- every pooled connection receives foreign keys, a 5-second busy timeout, and
  immediate write transactions through the DSN, while WAL initialization is
  bounded and verified separately;
- default-group deletion, default switching, group creation/sorting, and node
  first-heartbeat upsert use immediate transactions and validate missing or
  duplicate targets;
- node names and the single default group are protected by unique indexes, and
  startup repairs legacy missing/duplicate default-group state before recording
  the migration;
- the versioned ledger, future-version rejection, concurrent startup,
  missing-default rollback, concurrent first heartbeat, and unknown sort ID are
  covered by reliability tests.

The remaining storage-truthfulness gap is not a SQLite invariant: the MTS
engine currently returns an empty successful query after `Close()`, and alert
rule evaluation silently drops metric-query errors. That work belongs to the
reviewed `runtime-resilience`, `agent-ingest-consistency`, and `v8`
metric-catalog contracts rather than a new SQLite migration.

The MTS-only storage boundary was re-audited against source and the deployed
database on 2026-08-01. Every Agent numeric field is derived from
`model.MetricNames()`, traffic deltas and network probes write only to MTS, and
SQLite contains only application, policy, inventory, event, and backup metadata.
Regression tests now reject Agent/probe/derived telemetry columns in SQLite,
require retention cleanup and logical-backup export measurement lists to match,
and require backup validation to recognize every Agent and derived traffic
measurement.

Required result:

- deterministic SQLite connection configuration and busy timeout;
- transactional default-group, group-delete, node-upsert, and sorting
  invariants;
- versioned, restart-safe migrations with downgrade/rollback documentation;
- concurrency and fault-injection tests proving no orphan, duplicate, partial,
  or mixed state.

### Health, readiness, and failure visibility (implemented baseline)

Current evidence:

- unauthenticated `/healthz`, `/readyz`, and `/metrics` routes expose process,
  SQLite, MTS, restore, aggregate scheduler, HTTP, Agent freshness, storage,
  notification, and backup signals with bounded labels;
- request IDs and structured `slog` events cover HTTP, scheduler, alert,
  notification, backup, storage, and WebSocket failure boundaries;
- the operations runbook records initial alert thresholds and incident checks.

The remaining gap is per-worker and MTS-query truthfulness. The aggregate
`schedulers` boolean cannot detect worker exit, stale success, lag, panic, or
repeated failure; alert evaluation currently suppresses metric-query errors.
The reviewed schema-free replacement is
[`runtime-resilience-design.md`](./runtime-resilience-design.md).

Required result:

- unauthenticated minimal liveness endpoint;
- readiness endpoint covering SQLite, MTS, migration/restore state, and
  background scheduler availability without leaking secrets;
- structured `slog` request, audit-failure, scheduler, notification, backup,
  and storage logs with request IDs;
- Prometheus-compatible process, HTTP, Agent freshness, storage, scheduler,
  notification, and backup metrics;
- documented alert thresholds and troubleshooting runbook.

### Release and deployment engineering (implemented baseline)

Current evidence:

- reproducible Server/Agent archives embed version metadata through GoReleaser;
- a rootless multi-stage Dockerfile, hardened Compose definition, systemd unit,
  and versioned install/upgrade/rollback/uninstall script are present;
- CI and release workflows run tests, lint, vulnerability/module checks,
  frontend gates, archive checksums, SBOM generation, and keyless Sigstore
  signing;
- deployed acceptance has exercised backup-before-upgrade, rollback artifacts,
  non-root runtime, health checks, and IPv6 access.

Future migrations and schema-free batches must extend the same upgrade,
rollback, backup compatibility, container, and signed-release gates; they do
not reopen a separate deployment-foundation batch.

Required result:

- reproducible server and Agent builds with embedded version metadata;
- rootless multi-stage container image and health check;
- Compose and hardened systemd examples;
- install, upgrade, backup-before-upgrade, rollback, and uninstall commands;
- SBOM, checksums, release signing, dependency scanning, and CI gates;
- upgrade tests from the last supported release data layout.

## P1 competitor feature parity

### External identity

- Generic OIDC with discovery, PKCE/state/nonce validation, claim allowlists,
  account linking, owner lockout prevention, and local-login fallback.
- Provider management is owner-only and recently authenticated.
- The reviewed protocol, persistence, SSRF, API, frontend, migration, rollback,
  and approval boundaries are recorded in
  [`oidc-authentication-design.md`](./oidc-authentication-design.md).

### Theme packages and market

- Upload/export versioned theme packages with strict file allowlists, size
  limits, checksums, CSP compatibility, preview, rollback, and audit records.
- Remote catalogs require SSRF protection, redirect validation, HTTPS policy,
  pinned checksums, and explicit owner approval before installation.
- The declarative/sandbox runtime, signed market, atomic install, backup,
  rollback, operations, and approval boundaries are recorded in
  [`theme-packages-market-design.md`](./theme-packages-market-design.md).

### Remote operations

- Generic command tasks with node scopes, concurrency limits, timeouts,
  cancellation, output bounds, approvals, and audit events.
- File upload/download and clipboard workflows require explicit path scopes,
  symlink rejection, size limits, malware-integration hooks, and disabled-by-
  default policy controls.
- The reviewed policy intersection, at-most-once start protocol, encrypted
  command library/results/spool, single-file root model, Agent restart
  behavior, UI, backup reconciliation, rollback, and approval boundaries are
  recorded in [`remote-operations-design.md`](./remote-operations-design.md).
- Agent auto-discovery must never bypass per-node credential issuance. Manual
  review, owner-bounded automatic grants, Ed25519 claim continuity, hidden
  pending nodes, hash-only secrets, abuse controls, Agent bootstrap, rollback,
  and approval boundaries are defined in
  [`agent-auto-discovery-design.md`](./agent-auto-discovery-design.md).

### Administrative access and mutation consistency

- Replace generic post-response audit and scattered handler authorization with
  explicit privileged-route role, recent-auth, cache, operation and audit
  metadata. SQLite-only domain writes and success audit must commit in one
  transaction; login, logout, recent authentication, password changes and
  session revocation must not return failure after committed state.
- Keep routine fleet operations available to admins, but require recent identity
  confirmation for administrator creation, TOTP enrollment/replacement,
  Agent/SSH credential changes, terminal commands and other secret-bearing or
  remote actions. TOTP begin must not change durable state before code proof.
- Make authentication/protected/secret responses non-cacheable and close
  privileged WebSockets when the authorizing session/user/role expires or is
  revoked.
- Record accepted and terminal or unknown phases for backup, maintenance, MTS
  erasure, notification tests and terminal/remote work. Use exact HTTP request
  IDs, readable resource snapshots, bounded allowlisted details and no secrets.
- Deploy the schema-free contract before adding more administrator mutations;
  require later durable job migrations to reuse it. The complete audit,
  access, response-secrecy, failure, panic, one-time credential and acceptance
  boundary is reviewed in
  [`administrative-mutation-consistency-design.md`](./administrative-mutation-consistency-design.md).

### Application secret lifecycle

- The deployed TOTP secret is AES-GCM encrypted but has no AAD, envelope
  version, key ID, or rotation. SSH private keys and credential-bearing
  notification configurations remain plaintext in SQLite; response redaction
  does not change that storage risk.
- Migration `v2` first adds the strict root-key envelope and AAD profiles needed
  for OIDC provider secrets and PKCE verifiers; it is the compatibility form
  later converted by `v5`, not a separate OIDC encryption system.
- Migration `v5` must introduce one wrapped data-key registry under the existing
  `0600` root key, resource-bound AAD, typed legacy conversion, data/root-key
  rotation, fail-closed readiness, and full archive key/ciphertext validation.
  It also removes legacy TOTP and SSH plaintext/weak ciphertext before serving.
- Migration `v6` reuses the exact primitive for immutable notification secret
  revisions and clears legacy channel plaintext transactionally. Later features
  register their AAD, rotation enumerator, size, retention, and backup validator
  rather than creating another encryption system.
- Backups contain both the root key and recoverable ciphertext, so they remain
  credential bundles requiring encrypted external storage even after live
  SQLite secrets are encrypted. The complete boundary is reviewed in
  [`application-secret-lifecycle-design.md`](./application-secret-lifecycle-design.md).

### Notification and lifecycle coverage

- Add Bark, ServerChan 3/Turbo, event routing, encrypted channel revisions,
  durable retry/dead-letter history, and a constrained custom HTTP template;
  do not execute arbitrary server-side JavaScript. The provider contracts,
  SSRF boundary, migration v6 model, UI, backup reconciliation, rollback, and
  acceptance gate are reviewed in
  [`notification-provider-breadth-design.md`](./notification-provider-breadth-design.md).
- Add country GeoIP, manual override, price/billing/expiry metadata, public
  filters, calendar date advancement, and durable lifecycle notifications. The
  trusted-source, privacy consent, provider/cache, MMDB supply chain, migration
  v7, backup v3, UI, recovery, and acceptance boundary is reviewed in
  [`geoip-node-lifecycle-design.md`](./geoip-node-lifecycle-design.md).
- Add named daily/weekly Fleet availability summaries with group/node/channel
  scopes, canonical online/offline state, bounded name lists, manual test-run,
  durable outbox delivery, and one missed-period catch-up. Do not send on every
  process restart. The migration `v18`, scheduler, UI, backup, and acceptance
  contract is reviewed in
  [`fleet-status-summary-design.md`](./fleet-status-summary-design.md).

### Metric platform and accelerator coverage

- Replace the fixed metric list with a governed catalog, bounded tag schemas,
  per-metric retention, catalog-driven units, real range-to-axis behavior, and
  configurable node-detail dashboards. SQLite stores policy/layout only and
  MTS remains the sole time-series store. The reviewed design is
  [`metric-catalog-dashboard-design.md`](./metric-catalog-dashboard-design.md).
- Add optional multi-GPU inventory, utilization, memory, temperature, alerts,
  and charts after the catalog foundation. Numeric samples remain MTS-only;
  SQLite stores device inventory and collection policy. The reviewed design is
  [`accelerator-telemetry-design.md`](./accelerator-telemetry-design.md).
- Add owner-only current-cycle traffic calibration and reset as immutable
  application events rather than deleting or rewriting MTS. Directional target
  totals, exact boundary handling, correction/revocation, audit, public/admin
  responses, backup, and rollback are reviewed in
  [`traffic-calibration-design.md`](./traffic-calibration-design.md).
- Add owner-only explicit metric-history erasure as durable fixed-scope jobs.
  Node/task lifecycle deletion must tombstone application state before MTS
  removal, retry safely after restart, compact separately, coordinate with
  backup/retention, and never accept arbitrary measurements, tags, ranges, SQL,
  or paths. The migration `v17` contract is reviewed in
  [`metric-erasure-design.md`](./metric-erasure-design.md).

### Agent lifecycle and automation

- Add stable Agent sample IDs and capture timestamps, one pending in-memory
  sample, deterministic MTS receipt/delta writes, durable acknowledgement, and
  separate contact/telemetry freshness. Do not create a SQLite metric outbox or
  claim cross-store atomicity. The schema-free contract is reviewed in
  [`agent-ingest-consistency-design.md`](./agent-ingest-consistency-design.md).
- Add signed Agent release manifests, canary/wave rollout, pause/abort/rollback,
  atomic replacement, startup watchdog, container external-manager behavior,
  and an evidence-based platform support matrix. The reviewed design is
  [`agent-rollout-platform-design.md`](./agent-rollout-platform-design.md).
- Add hash-only, expiring, revocable service tokens with explicit scopes,
  resource/source constraints, rotation, audit, and no sensitive-operation
  bypass. The reviewed design is
  [`automation-access-design.md`](./automation-access-design.md).
- Add durable, redacted successful-login security events and thresholded
  suspicious-failure events to notification delivery. Do not include passwords,
  TOTP values, session IDs, token values, or raw user-agent strings. The atomic
  login/outbox, privacy, detection, retention, UI, and acceptance contract is
  reviewed in
  [`login-security-notification-design.md`](./login-security-notification-design.md).

### Fleet, backup transfer, and presentation

- Add a no-login fleet summary, all groups as card sections by default,
  optional per-group table view, search, sorting, country/status/expiry filters,
  explicit offline-first/keep/last behavior, and visibility-aware silent
  refresh. Split public Fleet, node-detail charts, administrator pages, and
  terminal/xterm into owning route chunks so anonymous `/` does not download
  the entire backend UI. The complete interaction, delivery-budget, privacy,
  dependency, and acceptance model is reviewed in
  [`public-fleet-experience-design.md`](./public-fleet-experience-design.md).
- Extend restore upload with owner-only resumable chunks, private staging,
  idempotent retries, exact size/digest validation, expiry cleanup, and existing
  full archive validation before staging.
- Add bounded application-history retention and cursor feeds, independent
  session/audit/alert hygiene, and startup reconciliation for backup rows,
  archives, partials, and restore journals. The migration `v19` contract is
  reviewed in
  [`application-data-lifecycle-design.md`](./application-data-lifecycle-design.md).
- Add versioned PWA static-shell caching while keeping every API, WebSocket,
  authentication, administration, health, and metric request network-only.
  Provide a bounded HTML `/wallboard` over existing public REST/WebSocket data
  instead of MJPEG. The offline, update, privacy, resource, and kiosk model is
  reviewed in
  [`public-pwa-wallboard-design.md`](./public-pwa-wallboard-design.md).
- Add iperf3/MTR as a high-risk diagnostics batch after secure Agent operations,
  the metric catalog, signed rollout, and approved target policies. Every
  Agent-reported numeric result remains MTS-only; arbitrary destinations and
  shell input are forbidden. The full dependency, execution, storage, UI, and
  abuse-control boundary is reviewed in
  [`advanced-network-diagnostics-design.md`](./advanced-network-diagnostics-design.md).
- Preserve the user-approved root-filesystem-only telemetry scope and do not
  add per-mount collection without new approval.

### Operator recovery and runtime diagnostics

- Add a stopped-service local recovery CLI that acquires the startup lock,
  inspects bounded state, atomically resets one existing owner password, removes
  TOTP, restores local login, revokes all sessions, records a durable audit, and
  can stage only a fully validated backup on both existing and genuinely empty
  first-run data volumes. No unauthenticated recovery listener or owner creation
  is allowed. The reviewed contract is
  [`operator-recovery-design.md`](./operator-recovery-design.md).
- Add local migration status and backup-copy preflight plus an owner-only
  read-only Operations view. Keep normal startup as the migration executor and
  reject DSN editing, alternate SQL metric backends, online schema mutation,
  and remote restart actions. The schema-free contract is reviewed in
  [`migration-recovery-operations-design.md`](./migration-recovery-operations-design.md).
- Add owner-only, recently authenticated runtime summaries and short-lived
  CPU/trace/heap/goroutine/mutex/block/thread captures with fixed limits,
  private expiring files, one-time download, startup cleanup, and no public
  `/debug/pprof`. The reviewed contract is
  [`runtime-diagnostics-design.md`](./runtime-diagnostics-design.md).
- Keep process restart under systemd, Compose, Kubernetes, or another external
  deployment manager. Restore and migration may request a graceful process exit
  for the manager to replace, but Beat does not expose a general remote restart
  button or API.

### Runtime supervision and realtime delivery

- Replace the aggregate scheduler-started boolean with individually tracked
  alert, traffic-report, maintenance, and metrics-fanout workers. Readiness must
  reflect running state, last success, lag, consecutive failures, unexpected
  return, and controlled fatal shutdown rather than merely successful startup.
- Build one privacy-filtered public metrics snapshot per cadence and fan it out
  through bounded global/per-IP connections. Coalesce slow-client updates,
  apply write deadlines, retain only a bounded last-good snapshot, and make MTS
  query volume independent of browser connection count.
- Register every hijacked connection with the Server lifecycle and close it
  before workers and stores. Remove the non-functional `/ws` and `/metrics/ws`
  legacy routes after explicit API-change approval, retaining only the RFC 6455
  `/api/v1/ws/metrics` route. The schema-free contract is reviewed in
  [`runtime-resilience-design.md`](./runtime-resilience-design.md).

### Ingress resource governance

- Add route-class admission before expensive work. Concurrent login and
  reauthentication must acquire global/per-client KDF leases before Argon2 so
  failure counting cannot be bypassed by a synchronized memory-exhaustion
  burst. Bound limiter memory without evicting active lockouts.
- Apply explicit header/request-target and per-route JSON body/media contracts.
  Public metric history must allow only known public series, valid bounded
  ranges and at most 600 MTS-aggregated points per series; it must never fetch
  unlimited raw rows and downsample after allocation.
- Reuse shared Fleet/current snapshots, singleflight network summaries, bounded
  MTS query concurrency, ETags and response budgets. Add per-node/global Agent
  report admission before heartbeat/MTS mutation, plus hidden-tab pause,
  single-flight polling and cancellation in the frontend. The reviewed
  schema-free contract is
  [`ingress-resource-governance-design.md`](./ingress-resource-governance-design.md).

### Deployment access, privacy, terminal, and localization

- Add one canonical external URL plus route-specific public REST/WebSocket
  origin policy while preserving same-origin cookie administration and scoped
  non-browser automation. Extend public address policy to hidden/masked/full,
  and store only aggregate anonymous interaction counters in MTS. The reviewed
  design is
  [`deployment-access-policy-design.md`](./deployment-access-policy-design.md).
- Add bounded terminal font/size/scrollback/cursor/palette settings to the
  remote-operations batch. Do not accept arbitrary CSS, remote fonts, or an
  unvalidated xterm options object.
- Ship complete English, Simplified/Traditional Chinese, Japanese, and
  Indonesian catalogs with typed keys, locale-aware values, pseudo-localization,
  state-preserving switching, and browser overflow/accessibility coverage. The
  reviewed design is
  [`localization-parity-design.md`](./localization-parity-design.md).
- Keep arbitrary custom Head/Body/terminal CSS out of the authenticated origin.
  Typed declarative themes and no-origin sandboxed public bundles are the safe
  equivalent for customization.

## P2 commercial hardening

- Browser-driven end-to-end coverage for setup, login, TOTP, node management,
  alerts, terminal, backup, restore, and responsive public views.
- Load and soak tests for Agent ingestion, WebSocket fan-out, MTS queries,
  scheduled jobs, erasure/compaction, and backup under continuous writes.
- Implement and load-verify the reviewed ingress and runtime-resilience budgets;
  retain feature-owned limits for notification retries, terminal output,
  uploads, diagnostics, and concurrent jobs.
- Disaster-recovery runbooks with measured RPO/RTO and cold-standby rehearsal.
- Accessibility review, localization completeness, API compatibility policy,
  support matrix, security policy, and release lifecycle documentation.
- A feature-by-feature competitor audit at every stable Komari/DStatus baseline
  update; a route or setting must be implemented, safely substituted, or
  explicitly rejected with acceptance evidence rather than omitted.

## Required approval boundaries

The following must be approved before implementation because they modify
shared behavior, persistence, or deployment contracts:

1. SQLite connection policy, schema migration ledger, and transactional store
   changes.
2. Trusted proxy configuration and effective client/scheme semantics.
3. New public health/readiness/metrics endpoints.
4. OIDC account-linking and login behavior.
5. Theme packages, remote catalogs, Agent auto-discovery, file transfer,
   clipboard, generic remote tasks, notification delivery, GeoIP/lifecycle,
   and external MMDB mounts.
6. Metric catalog/retention/dashboard composition, GPU telemetry, signed Agent
   rollout, platform expansion, scoped service tokens, grouped Fleet
   interaction, resumable backup upload, PWA/HTML-wallboard behavior, and
   advanced diagnostics.
7. Root deployment configuration, container files, CI, release signing, and
   environment templates.
8. Local operator recovery, runtime profiling artifacts, external URL and
   browser-origin policy, public address/visitor telemetry semantics, terminal
   preference persistence, application-wide localization behavior, and billing
   traffic calibration/correction semantics.
9. Explicit metric erasure and entity-deletion lifecycle, Fleet-summary
   schedules/outbox production, empty-instance restore, and migration/recovery
   status/preflight behavior.
10. Background-worker supervision, readiness failure semantics, shared public
    metrics fanout, WebSocket limits, legacy-route removal, and shutdown order.
11. HTTP body/target/media limits, pre-KDF authentication admission, public MTS
    query budgets/cache, Agent ingestion limits, response bounds, and polling
    cancellation.
12. Privileged-route role/recent-auth/cache/audit metadata, atomic
    mutation/audit transactions, TOTP pending setup, bootstrap attribution,
    external-operation phase semantics, long-lived session invalidation,
    sensitive-read selection, and one-time credential response ordering.

## Next implementation batch

The P0 foundation below is complete and deployed at SQLite migration `v1` and
backup archive format `v1`. The single authoritative pending sequence is
maintained in
[`commercial-parity-implementation-sequence.md`](./commercial-parity-implementation-sequence.md).

The canonical SQLite order is:

1. `v2` OIDC;
2. `v3` theme packages, introducing backup format `v2`;
3. `v4` secure Agent enrollment;
4. `v5` shared application-secret lifecycle, audited remote operations, and file
   transfer;
5. `v6` commercial notification delivery;
6. `v7` GeoIP/node lifecycle, introducing backup format `v3`;
7. `v8` metric catalog/dashboard composition, introducing catalog-governed
   backup format `v4`;
8. `v9` GPU/accelerator telemetry;
9. `v10` signed Agent rollout;
10. `v11` scoped automation credentials;
11. `v12` login-security events and notification producer;
12. `v13` resumable restore upload sessions;
13. `v14` controlled MTR/iperf3 diagnostics;
14. `v15` external access, browser origin, public address, and aggregate visitor
    telemetry policy;
15. `v16` immutable billing-traffic calibration, correction, and revocation;
16. `v17` durable fixed-scope metric erasure and node/task deletion lifecycle;
17. `v18` Fleet status-summary schedules and durable outbox production;
18. `v19` application-history lifecycle, pagination, cleanup and backup
    reconciliation.

The schema-free batches are `fleet-public`, `pwa-static-shell`, `wallboard-html`,
`operator-recovery-cli`, `migration-recovery-ops`, `runtime-diagnostics`,
`runtime-resilience`, `ingress-governance`, `agent-ingest-consistency`,
`administrative-mutation-consistency`, and `localization-parity`. Application
secret lifecycle is not another schema-free approval unit: it is part of `v5`
because its wrapped-key registry and SSH migration require that schema. Fleet
precedes both presentation batches and localization; its route-asset isolation
must pass before PWA caches only the public versioned static shell, and PWA is
verified independently before wallboard install testing. Recovery and runtime
diagnostics depend on deployed account security,
while
migration/recovery operations also depend on the local recovery lock and CLI.
Runtime resilience depends on deployed operability and public metrics, composes
with `v15` public WebSocket origin policy, and may later use `v8` catalog batch
queries. Ingress governance depends on deployed trusted-proxy identity,
coordinates with runtime resilience, and is generalized by `v8` query policy
and `v12` durable login security. Agent ingestion consistency coordinates with
both batches, joins the `v8` catalog, and must precede `v17` erasure and
mandatory Agent rollout. Administrative mutation consistency follows deployed
account security, should precede every new privileged route, and supplies the
authorization/audit envelope reused by durable operations. `v19` application lifecycle
follows all earlier persistent history owners. They remain independent approval
units. None of these assignments authorizes implementation. Each batch needs
explicit approval and its full CI, migration/backup where applicable,
rollback, security, MTS-isolation, and deployed IPv4/IPv6 gate.

## P0 implementation status

The approved P0 foundation is implemented in the current workspace:

- trusted proxy CIDR parsing and canonical effective scheme, host, and client
  IP handling now drive Secure cookies, HSTS, same-origin checks, request logs,
  and administrator audit IPs;
- SQLite connections apply foreign keys and busy timeout per connection, use
  immediate write transactions, record versioned migrations, reject future
  schema versions, and protect default-group and first-heartbeat invariants;
- `/healthz`, `/readyz`, and `/metrics` expose bounded base operational signals,
  while HTTP, scheduler, alert, backup, storage, and WebSocket failures use
  structured `slog` records with request IDs where request-scoped; per-worker
  supervision and bounded realtime fanout remain pending;
- Docker, Compose, systemd, GoReleaser, CI, checksum, SBOM, Sigstore, and
  backup-before-upgrade/rollback definitions are present.

Recorded completion evidence on 2026-07-30:

- the full local gate passed: Go tests including race detection, `go vet`,
  `golangci-lint`, `govulncheck`, module verification, 90.1% Go statement
  coverage, and the frontend's 124 tests, lint, production build, and 91.11%
  line coverage;
- the versioned `p0-20260730` Server passed isolated IPv6 acceptance for
  liveness, readiness, Prometheus output, public/admin authorization boundaries,
  trusted HTTPS forwarding, HSTS, Secure cookies, and untrusted forwarding-header
  rejection;
- the Server was deployed on IPv6 after a stopped-service backup. SQLite schema
  migration version 1, MTS readiness, all schedulers, the public dashboard, and
  continued reporting from the unchanged Agent process were verified after
  restart.

The P0 container verification completed on 2026-07-30 after moving to pinned,
reachable official Fedora and Azure Linux image digests. The Docker-format
image built with Go 1.26.5 and Node 22.22.2, retained its healthcheck, ran as
UID/GID 65532 with a read-only root filesystem, all capabilities dropped, and
`no-new-privileges`, and reached `healthy` with every readiness check passing.
Compose configuration and its runtime `curl` healthcheck also parse correctly.

The SQLite first-start security and concurrency hardening completed on
2026-07-30. The Store now creates or tightens the database before opening it,
so the database, WAL, and SHM files remain `0600` under `umask 0022`. WAL mode
is initialized once with bounded BUSY/LOCKED retries rather than being changed
on every pooled connection. Three full race/shuffle passes, 100-round focused
concurrency and permission tests, the 90.0% statement-coverage gate, lint, vet,
module verification, vulnerability scanning, and a Docker first-start test all
passed. Production was deployed from a stopped-service backup; SQLite integrity,
both IPv6 addresses, public/admin authorization, readiness, and resumed reports
from the unchanged Agent process were verified.

## Resolved frontend security finding

GHSA-qwww-vcr4-c8h2 was removed on 2026-07-30 by replacing
`react-router-dom` 7.18.1 with the fixed `react-router` 8.3.0 package and
updating the 15 package imports without changing route definitions. CI and the
Docker frontend build now reject high-severity production dependency findings.

The completed migration passed all 124 frontend tests, 91.11% line coverage,
lint, production build, and npm production audit with zero vulnerabilities. A
Docker-format image also executed the audit during its build, retained its
healthcheck, and reached `healthy` under the hardened non-root runtime. The new
static assets were deployed after a permissions-restricted backup; both public
IPv6 addresses returned `200`, unauthenticated public data remained available,
administrator APIs returned `401`, and the existing Server and Agent processes
continued without restart.
