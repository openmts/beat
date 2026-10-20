# Commercial parity implementation sequence

Updated: 2026-10-20

Status: the working tree and 8081 preview both run schema `v30` with backup
format `v4` (see "Deployed schema ledger" at the end)

## Purpose and authority

This document is the single version and dependency registry for Beat's reviewed
but unimplemented competitor-parity work. Individual design documents remain
authoritative for feature behavior, security invariants, API contracts, storage
boundaries, tests, and rollback. When a design's previous draft migration or
backup number conflicts with this registry, this registry is authoritative and
the design must be corrected before coding.

The original 8080 deployment baseline is SQLite migration `v7` and backup writer format
`v2` (readers accept `v1` and `v2`). Migration `v6` installed theme-package
persistence; `v7` implements the one-command, single-machine join contract in
[`agent-join-design.md`](./agent-join-design.md), not the older full-discovery
proposal. Unimplemented batch numbers below are allocations, never evidence of
completion. The separate 8081 preview now runs SQLite `v29` with backup format
`v4`; its current verification and rollback are recorded below and in local-preview.md.
The ledger at the end of this document is the authoritative deployed-versus-proposed
split as of 2026-10-18.

The current user has authorized implementation across the commercial-readiness
scope. Grafana alignment means Beat's own metric interaction experience. The
metric catalog and dashboard foundation is therefore the next migration, `v8`;
it does not depend on GeoIP or notification delivery. The shared-secret,
notification and GeoIP batches occupy `v9`, `v10` and `v13`; source implementation
and verification status is recorded separately in the delivery plan.
Durable remote operations are allocated separately to `v11`; advanced alert
evaluation is `v12`. These allocations describe distinct delivery units. Deployed
migration status is recorded below. Detailed acceptance and current evidence live in the
[delivery plan](./commercial-delivery-plan.md).

## Separate version lines

Beat maintains three independent sequences:

1. **SQLite schema migration**: one global consecutive ledger. Every migration
   changes platform application persistence and old binaries reject newer
   versions.
2. **Backup archive format**: increments only when the archive payload or
   logical MTS export contract changes. Adding SQLite tables alone does not
   require an archive-format increment because `platform.sqlite` is already a
   complete snapshot.
3. **Release batch**: covers frontend, protocol, Agent, deployment, or behavior
   changes whether or not they need a SQLite migration. Batch IDs are planning
   identifiers, not database versions.

Every released batch embeds the Beat version, maximum SQLite version, supported
backup-format range, Agent protocol range, and frontend build ID. Readiness and
backup manifests expose those bounded compatibility values without revealing
secrets.

## Canonical SQLite sequence

| Migration | Release batch | Capability | Required predecessors | Backup format |
| --- | --- | --- | --- | --- |
| `v1` | `p0-foundation` | Deployed migration ledger and SQLite reliability invariants | none | `v1` |
| `v2` | `agent-task-channel` | Agent task WebSocket channel (pre-registry) | `v1` | `v1` |
| `v3` | `custom-metric-tasks` | Custom metric collection tasks (pre-registry) | `v2` | `v1` |
| `v4` | `alert-acknowledgement` | Alert event acknowledgement columns (pre-registry) | `v3` | `v1` |
| `v5` | `identity-oidc` | OIDC providers, external identities, transactions, session attribution, and resource-bound root-key envelope format 1 | deployed account security | `v1` |
| `v6` | `theme-packages` | Theme packages, activation history, and signed market sources | `v5` | introduces `v2` |
| `v7` | `agent-enrollment` | Secure manual/bounded automatic Agent enrollment and pending-claim state | `v5`, per-node identity; ordered after `v6` for one consecutive release train | `v2` |
| `v8` | `metric-catalog` | Governed metric definitions, retention/query policy, and dashboard templates | deployed `v7` and MTS | introduces `v3` |
| `v9` | `application-secrets` | Shared wrapped-key lifecycle, resource-bound TOTP/SSH/OIDC conversion and rotation/recovery | `v5`, ordered after `v8` | `v3` |
| `v10` | `notification-delivery` | Encrypted channel revisions, durable outbox, leases, retries, and delivery history | `v9` application-secret lifecycle | `v3` |
| `v11` | `remote-operations` | Durable Agent operations, command library, scoped policy, encrypted transfer metadata/spool and session invalidation | `v7`, `v9` secret lifecycle, `v10` notifications and administrative mutation consistency | `v3` |
| `v12` | `alert-evaluation` | Scoped windowed rules, hysteresis, persisted pending/recovery state, NoData/Error policies and report preparation outcomes | `v8` governed metrics, `v10` outbox; ordered after `v11` | `v3` |
| `v13` | `geoip-lifecycle` | GeoIP sources/cache, node lifecycle, reminders, and lifecycle events | `v10` notification routing | introduces `v4` |
| `v14` | `accelerator-telemetry` | Accelerator inventory/policy and MTS-only GPU series | `v8` | `v4` |
| `v15` | `agent-rollout` | Signed Agent releases, target snapshots, waves, attempts, and health gates | `v7`, per-node identity, release signing | `v4` |
| `v16` | `automation-access` | Scoped expiring service principals, token constraints, and `service`-actor audit rows | stable central authorization after `v15` | `v4` |
| `v17` | `login-security` | Durable login security events, persisted thresholds, fingerprints, and outbox producer | `v10`; ordered after `v13` for optional trusted GeoIP and after `v16` for final security vocabulary | `v4` |
| `v18` | `resumable-restore-upload` | Restart-safe owner upload sessions and chunk state | current backup validation and archive format `v4` | `v4` |
| `v19` | `advanced-diagnostics` | Approved diagnostic targets, jobs, attempts, budgets, cursors, and encrypted hop metadata | `v7`, `v8`, `v11`, `v15` | `v4` |
| `v20` | `edge-access-policy` | Canonical external access policy, public address privacy modes, and aggregate visitor telemetry policy | `v5`, `v6`, `v8`, `fleet-public`; ordered after `v19` for one consecutive release train | `v4` |
| `v21` | `traffic-calibration` | Append-only per-node billing traffic calibration, correction, revocation, and attribution events | deployed account security and MTS traffic aggregation; ordered after `v20` for one consecutive release train | `v4` |
| `v22` | `metric-erasure` | Durable fixed-scope MTS erasure jobs, entity tombstones, retryable compaction, and node/task deletion finalization | `v8` metric catalog, deployed per-node identity/network tasks, MTS operation lock; ordered after `v21` | `v4` |
| `v23` | `fleet-status-summary` | Fleet summary schedules, scoped runs, catch-up cursors, and durable notification-outbox production | `v10` notification delivery, deployed availability state, node/group model; ordered after `v22` | `v4` |
| `v24` | `application-data-lifecycle` | Bounded application-history retention, cursor feeds, session hygiene, and crash-safe backup record/archive/journal reconciliation | `v18` upload lifecycle, deployed account audit/backup, `v23`; ordered after all prior history owners | `v4` |
| `v25` | `dashboard-collaboration` | Dashboard annotations, frozen read-only snapshots, a personal panel library, and persisted panel-to-alert-rule associations that render rule state on panels | `v8` dashboard templates, `v12` alert evaluation, `v10` notification routing for rule state; ordered after `v24` | `v4` |
| `v26` | `agent-discovery` | Bounded Agent auto-discovery: enrollment grants/challenges/requests state plus the node `credential_state` column | `v7` enrollment, `v15` Agent vocabulary; ordered after `v25` | `v4` |
| `v27` | `dashboard-versions` | Dashboard revision history with restore points | `v8` dashboard templates, `v25` collaboration state; ordered after `v26` | `v4` |
| `v28` | `agent-task-output` | Persisted stdout/stderr `agent_tasks.output` and failure text for remote-operation results | `v11` remote operations, `v24` application data lifecycle, `v25` collaboration history; ordered after `v27` | `v4` |
| `v29` | `agent-release-retention` | Owner-configurable release/rollout/artifact retention windows for the agent release platform (`agent_release_retention` table) | `v14` agent rollout, deployed account settings; ordered after `v28` | `v4` |
| `v30` | `accelerator-public-visibility` | Site-level and per-node accelerator visibility switches for metric pages (`show_accelerators` and `node_accelerator_visibility`) | `v14` accelerator telemetry, deployed site settings; ordered after `v29` | `v4` |

Migrations are never squashed in released environments. A development-only
fresh-database bootstrap may use an equivalent schema initializer, but tests
must also upgrade a real `v1` fixture through every consecutive migration and
compare its schema/invariants with a fresh current-version database.

Source keeps that ledger consecutive with explicit `reserved_<batch>` no-op
entries for allocated but unimplemented numbers, so a `v1` fixture can still
walk the full consecutive history while the real feature DDL lands later. As of
2026-10-18 every allocated number `v8`–`v29` is a real migration, so the tree
contains no `reserved_*` entries (`sqlite_migrations.go` verified); the next
reservation would be `v29`. A
reservation is valid only while no released binary has distributed it: the
implementing batch must write its DDL into that same entry, and the reservation
must never ship in a release, or already-migrated instances would skip the new
DDL through `migrationApplied`.

## Backup archive sequence

### Format `v1` - original baseline

Contains manifest, complete SQLite snapshot, logical fixed-list MTS export,
administrator data key, and checksums.

Migration `v5` keeps these payloads but requires readers to authenticate every
OIDC envelope against its archived root key and reconstructed AAD. This is a
validation strengthening, not a new payload or format number.

### Format `v2` - theme payloads

Introduced by migration `v6`. Adds content-addressed theme package assets and
manifest inventory. Readers for later formats continue accepting `v1` when the
restored database does not reference theme assets.

### Format `v3` - catalog-governed MTS export

Introduced by migration `v8`. Replaces the fixed measurement allowlist with a
catalog-versioned typed inventory. Every registered built-in, derived and custom
measurement and its bounded tag/field schema is included.

### Format `v4` - managed GeoIP database

Introduced by migration `v13`. Extends format `v3` with managed current/previous
MMDB files and manifest metadata. External read-only MMDB mounts remain external
requirements.

Formats `v3` and `v4` require:

- catalog version and digest in the manifest;
- one canonical metric-definition inventory with key, type, unit, field/tag
  schema, and visibility class;
- every exported MTS row to match a registered measurement and bounded tags;
- restored SQLite catalog semantics to match the manifest before MTS replay;
- unknown measurements or incompatible unit/type changes to fail closed.

Migration `v14`, `v19`, `v20`, `v21`, `v22`, `v23`, `v24`, `v25` and `v26` extend
registered application/catalog or scheduling state and backup inventory but do
not change the `v4` envelope. Migration `v18` changes upload transport only; it does not
change archive contents. A backup cannot overlap an active `v22` erasure, so its
SQLite job state and logical MTS export remain coherent.

Migration `v9` does not change the archive payload: its wrapped data-key registry
lives inside `platform.sqlite`, and the existing `secrets/admin-data.key` entry
remains the matching 32-byte root key. Every `v9+` reader must nevertheless
validate the complete root-key/registry/ciphertext relationship defined in
[`application-secret-lifecycle-design.md`](./application-secret-lifecycle-design.md),
not only key length and checksums.

## Schema-free release batches

These batches do not consume a SQLite migration number:

| Batch ID | Capability | Dependency and ordering |
| --- | --- | --- |
| `fleet-public` | Grouped-card-first fleet summary, search, filters, stable sorting, optional tables, visibility-aware refresh, and public/admin/heavy-route asset isolation | may ship after current baseline; country/expiry controls activate only after `v13` |
| `pwa-static-shell` | Manifest, versioned public-route static-shell cache, explicit offline state, and controlled update activation | after `fleet-public` bundle isolation; admin/terminal/chart chunks and all API/auth/WebSocket/metric requests remain outside the public precache |
| `wallboard-html` | Removed in v228; `/wallboard`, wallboard-only controls, localization, PWA navigation and frontend telemetry mappings were deleted | no longer a future deliverable |
| `operator-recovery-cli` | Stopped-service owner unlock, all-session revocation, empty-instance inspection, and validated first-run/existing-instance restore staging | after deployed account security; OIDC local-login restoration activates after `v5` |
| `migration-recovery-ops` | Local migration status/preflight, private failure marker, and owner-only read-only operations guidance without DSN editing | after `operator-recovery-cli`; tracks the then-current canonical migration and backup readers |
| `runtime-diagnostics` | Owner-only bounded runtime summary and ephemeral profile capture | after deployed account security; independent of Agent diagnostics `v19` |
| `runtime-resilience` | Per-worker supervision, truthful readiness and MTS lifecycle errors, one shared public metrics snapshot, bounded WebSocket fanout, and shutdown-safe connection ownership | after deployed operability and public metrics; composes with `agent-ingest-consistency`, `v20` origin policy and may use `v8` batch-latest queries |
| `ingress-governance` | Bounded HTTP bodies/targets, pre-KDF authentication admission, public snapshot/history query budgets, Agent ingestion limits, and cancellation-safe polling | after deployed trusted-proxy identity; coordinate with `runtime-resilience`, generalized by `v8` query policy and `v17` durable login security |
| `agent-ingest-consistency` | Stable Agent sample identity/time, deterministic MTS retry and traffic deltas, durable receipt acknowledgement, and contact-versus-telemetry freshness | after deployed per-node identity/MTS; coordinate with `ingress-governance` and `runtime-resilience`, register in `v8`, deploy before `v22` and mandatory Agent rollout |
| `administrative-mutation-consistency` | Explicit role/recent-auth/cache/audit route policy, safe pending TOTP, atomic SQLite mutation/audit writes, long-lived session invalidation, and truthful external phases | after deployed account security; deploy before adding more administrator mutations and require `v8`, `v9`, `v10`, `v11`, `v12`, `v17`, `v22`, and `v24` to reuse its authorization/audit envelope and operation linkage |
| `localization-parity` | Complete English, Simplified/Traditional Chinese, Japanese, and Indonesian frontend catalogs | after `fleet-public`; every later frontend batch adds all locale keys atomically |

Theme persistence was deployed in migration `v6`; runtime hardening and author tooling reuse it; it must not be confused
with the later static-shell service worker. The HTML wallboard and its URL-based presentation settings were removed in v228; persisted wallboard presets are no longer planned.

The `v20` anonymous interaction endpoint, its two MTS counters and the
owner-only telemetry policy panel were approved and implemented on 2026-10-15
(see [deployment-access-policy-design.md](./deployment-access-policy-design.md)).

## Approval and delivery units

The commercial delivery request authorizes the implementation scope in the
[acceptance register](./commercial-delivery-plan.md). On 2026-10-15 the owner
approved all pending approval items in one decision: SQLite batches `v18`
through `v24`, localization parity (`zh-TW`, `ja`, `id`), gating the theme build
script, and further public-shell splitting or budget changes if needed. `v22`
metric erasure is a destructive operation and still requires the operator to
enable it explicitly in the product before any history is deleted. The existing explicit
permission covers stopped-service backup, migration and systemd deployment of
the completed build. It does not authorize remote messages, publication, git
history changes or destructive history erasure. User-enabled operational
policies retain their own product confirmations.

Implement each batch with failure-reproducing tests, update version and backup
fixtures, review its changes, and run the mandatory gate below. Record evidence
before claiming completion. Keep high-risk optional Agent capabilities disabled
until explicitly enabled by the operator. No partial source build is deployed.

The owner repeated the same blanket decision on 2026-10-15 for every design
that still read "implementation requires explicit approval": `fleet-public`,
`runtime-resilience`, `ingress-governance`, `agent-auto-discovery`,
`runtime-diagnostics`, `migration-recovery-ops`, `pwa-static-shell`, and
`administrative-mutation-consistency`. The `wallboard-html` batch was later
removed in v228. The `v11` remote operations and `v13` GeoIP/lifecycle batches
were already implemented and keep their recorded evidence.

### Registered contracts for the 2026-10-15 schema-free approvals

| Batch | Dependency | Backup impact | Rollback contract |
| --- | --- | --- | --- |
| `fleet-public` | deployed public shell and node presentation | none; format `v4` unchanged | restore the previous binary/frontend; no data conversion |
| `runtime-resilience` | deployed operability foundation and public metrics stream | none; the producer keeps snapshots in memory | restore the previous binary/frontend |
| `ingress-governance` | deployed trusted-proxy identity and `v17` durable login security | none; limiter state is in memory | restore the previous binary; rejection budgets reset |
| `agent-auto-discovery` | `v7` enrollment, approved Agent identity and policy storage | standard `platform.sqlite` snapshot only | restore the previous binary plus the matching SQLite backup; unclaimed reservations are lost |
| `runtime-diagnostics` | owner-only operations surface and `v19` diagnostics | none; profiles and summaries are ephemeral | restore the previous binary; no stored profile is orphaned |
| `migration-recovery-ops` | `operator-recovery-cli` and the canonical migration ledger | none; the status marker is `0600` operational state outside SQLite | restore the previous binary; delete the marker or let the next start replace it |
| `pwa-static-shell` | deployed public shell and PWA manifest | none | restore the previous static shell |
| `wallboard-html` | removed in v228 with no database dependency | none | code and deployment rollback only |
| `administrative-mutation-consistency` | deployed account security and `v17` login security | none; no new envelope or key format | restore the previous binary |

`ingress-governance` landed its admission-controller batch and the public
snapshot reuse batch on 2026-10-15 (`internal/ingress`, route-class mapping,
per-node Agent-write gate, Prometheus counters, the owner-only read-only Ingress
view, and shared encoded fleet/node snapshots with ETag/`304` reuse). The shared
bounded JSON decode helper has since landed on every JSON write route, and
public node history is now bucketed in the storage layer under a 600-point
ceiling; frontend polling now honours `Retry-After` across every loop; in-flight
public history reads are coalesced per input key (`internal/api/handler/history_share.go`);
public network quality is now served from a bounded, write-invalidated snapshot with an
`ETag`/`304` path (`internal/api/handler/network_quality_snapshot.go`), and the fleet-level
summary needs no second cache because the public fleet page derives it from the shared public
node snapshot. Host load evidence and the unified test batch remain open in
[commercial-delivery-plan.md](./commercial-delivery-plan.md).

None of these consumes a SQLite migration: the next free migration is `v25`
(`dashboard-collaboration`), and backup readers/writers stay at format `v4`.

### `migration-recovery-ops` contract

- **Dependency**: the local recovery CLI and the canonical `schema_migrations`
  ledger. No Agent, MTS or notification change.
- **Schema**: none. `migration.status.json` is a bounded `0600` operational file
  inside the private `0700` data directory, not application data.
- **Backup impact**: none beyond the standard `platform.sqlite` snapshot; the
  `v4` archive envelope, logical MTS export and manifest inventory are unchanged.
- **Approval unit**: `migration-status`, `migration-check`, the startup marker,
  the owner-only read-only Operations section and the runbook ship as one
  reviewed batch; the first two parts landed on 2026-10-15.
- **Rollback contract**: restore the previous binary. An older binary ignores
  the marker file, which the next start replaces atomically; no live database
  byte depends on the marker.

## Per-batch mandatory gate

No batch is complete or eligible for commit/push/deployment until all applicable
evidence exists:

- upgraded and fresh-database schema equivalence, constraints, indexes,
  idempotent startup, concurrent first start, and future-version rejection;
- backup creation, validation, restore, wrong-version rejection, pre-migration
  rollback, and previous-supported-format compatibility;
- root/wrapped-key registry integrity, full AAD-bound ciphertext validation,
  plaintext-zero backfill, rotation restart recovery, and matching-key rollback
  for every secret-bearing batch;
- proof that Agent, probe, derived, GPU, and diagnostic numeric time series are
  only in MTS and SQLite contains only application/policy/inventory metadata;
- public no-login and authenticated admin boundaries over IPv4 and IPv6;
- every select/combobox shows names or labels while submitting IDs/keys;
- error handling, cancellation, restart recovery, bounded queues/concurrency,
  retention, audit redaction, observability, and readiness degradation;
- backend statement and frontend line coverage at or above 90 percent;
- Go race tests, `go vet`, `goimports-reviser`, `golangci-lint`, vulnerability
  and module verification, frontend audit/test/lint/build, browser smoke/E2E,
  container/release checks, and temporary-artifact cleanup;
- README/runbook and relevant design/matrix evidence updated concisely.

The exact commands come from the then-current CI workflow. Partial tests or a
successful build cannot substitute for the complete gate when preparing a
commit, push, release, or deployment.

## Compatibility and rollback policy

Each Server binary supports exactly its declared maximum SQLite version and
fails before serving when the database is newer. Forward migration occurs only
after a verified stopped-service backup. There are no in-place down migrations;
rollback restores the matching pre-migration SQLite/data/Agent artifacts.

Migration `v9` is also the only owner of the application-secret primitive.
Later secret-bearing migrations register AAD/resource enumerators with it and
must not add another key file, envelope, or rotation implementation.

Backup readers accept the current format and explicitly tested earlier formats.
Writers emit only the current format. Restore never silently drops unsupported
payloads or MTS measurements. Agent protocol changes use advertised min/max
versions and a rolling compatibility window defined by the owning design.

MTS additions are append-only across a failed feature rollback. The older
binary may leave new registered measurements unreachable; deletion is the
separate owner-approved `v22` erasure operation after rollback evidence is safe.

## Change control

Before implementation starts, every referenced design must show the migration
and backup-format assignment in this registry. If new competitor evidence or a
dependency changes ordering, update this file and every affected design in one
reviewed documentation change before allocating code.

After `v24`, the next SQLite migration is `v25`. No design may claim `v25`
without first adding its dependency, backup impact, approval unit, and rollback
contract here. `v26` (`agent-discovery`) is allocated in the same way and is
documented directly below.

### `v25` dashboard collaboration contract

- **Dependency**: `v8` metric dashboards and `v12` alert evaluation. Panel-alert
  rendering reads the `v12` evaluation state and never writes alert-owned rows.
- **Schema**: one migration adds `dashboard_annotations`, `dashboard_snapshots`,
  `dashboard_panel_library` and `dashboard_panel_alerts`. All four are
  application-owned SQLite tables; no MTS measurement, unit or tag changes.
- **Backup impact**: none beyond the standard `platform.sqlite` snapshot. The
  `v4` archive envelope, logical MTS export contract and manifest inventory are
  unchanged, so writers stay at format `v4` and restore keeps working.
- **Approval unit**: annotations, snapshots, the personal panel library and
  persisted panel-to-rule associations ship as one reviewed batch. The user
  approved this batch explicitly.
- **Rollback contract**: dropping the four tables returns a `v25` database to
  the `v24` shape without touching dashboards, rules or metric history. Old
  binaries reject schema `v25` before serving traffic, so rollback requires the
  previous binary plus the matching SQLite backup; frozen snapshot definitions
  and panel library entries are lost if that backup predates them, which the
  operator is told before restoring.

### `v26` agent auto-discovery contract

- **Dependency**: deployed per-node Agent tokens, the narrow single-command join
  workflow in [`agent-join-design.md`](./agent-join-design.md), and the admin
  session/recent-reauth envelope. No MTS measurement, unit or tag change.
- **Schema**: one migration adds `agent_enrollment_grants`,
  `agent_enrollment_challenges` and `agent_enrollment_requests`, plus an
  explicit `nodes.credential_state` column backfilled to
  `legacy`/`active`/`revoked`. All four are application-owned SQLite objects.
- **Backup impact**: none beyond the standard `platform.sqlite` snapshot. The
  `v4` archive envelope, logical MTS export contract and manifest inventory are
  unchanged, so writers stay at format `v4` and restore keeps working.
- **Approval unit**: grants, challenge/request/claim protocol, hidden
  `pending_claim` reservations and the single-use Ed25519 continuity contract
  ship as one reviewed batch. The user approved all pending items on
  2026-10-15.
- **Rollback contract**: dropping the three tables and the `credential_state`
  column returns a `v26` database to the `v25` shape without touching nodes,
  dashboards or metric history. Old binaries reject schema `v26` before serving
  traffic, so rollback requires the previous binary plus the matching SQLite
  backup; enrollment grants, requests and pending reservations created after
  that backup are lost, and Agents that already claimed keep working only if
  their node rows survive in the restored snapshot.

### `v18` resumable-restore-upload contract

- **Dependency**: deployed backup validation and archive format `v4`, plus the
  existing owner-only `/api/v1/admin/backups` surface. No Agent or MTS change.
- **Schema**: one migration adds `backup_upload_sessions` (owner, declaration,
  fixed chunk geometry, progress counters, private staging name, state, backup
  linkage, expiry, staging-cleanup flag) and `backup_upload_chunks` (index,
  exact offset, size, SHA-256). Both are application-owned SQLite tables.
- **Backup impact**: none beyond the standard `platform.sqlite` snapshot. The
  `v4` envelope, manifest inventory and logical MTS export are unchanged.
- **Approval unit**: owner-approved with the 2026-10-15 blanket decision. Chunk
  transport, session state, staging cleanup and the console dialog ship as one
  batch. `v22` erasure is a separate destructive operation and keeps its own
  product confirmation.
- **Rollback contract**: dropping the two tables returns a `v18` database to the
  `v17` shape. Only in-flight upload sessions are lost, no recorded backup is
  deleted, and the operator repeats the upload with the single-request
  `/admin/backups/validate` endpoint that ships in the same binary.

## Current source registration

Source registers consecutive v11 remote operations, v12 alert evaluation, v13
GeoIP/lifecycle and v14 accelerator inventory/per-device alert state. The fixed
v14 r2 candidate passed complete backend/frontend gates, script checks, GPU
archive/integration tests and Podman/browser verification. After a cold backup,
the 8081 preview was deployed as `0.2.0-dev.v14` with schema14 and subsequently
received the navigation and MTS readiness fixes as `0.2.0-dev.v14.1`; the original
8080/v7 instance is unchanged. The GPU and C02 metadata/SSH transaction batches
are included. Source now registers v15 signed Agent rollout (schema 15, central
routes, Agent update runtime, console pages) and the v16 service-principal core
(schema 16: `service_principals`, `service_token_scopes`,
`service_token_resources`). No runtime carries schema 15 or 16 yet: the 8081
preview stays v14 and the original 8080 instance stays v7.

The fixed preview remains available for real-machine testing. Continued worktree
integration has completed the v15 archive/restore boundary and schema-free
runtime-resilience backend with their affected-package gates. They are not
deployed. The v16 backend now ships token issuance/verification, policy
validation, persistence with atomic audit, the owner-only
`/api/v1/admin/service-tokens` management API and server wiring. Bearer
middleware on data endpoints with scope enforcement and rate limits, the console
Security tab, Prometheus metrics, restore-time revocation and external CLI
evidence remain open, as do v15 deployment acceptance and the overall commercial
acceptance register.

The v16 data plane now also writes `service`-actor audit rows for denials and
privileged operations through the existing `admin_audit_events` table; the v16
migration adds `actor_type` (default `admin`) so no new migration number is
claimed. Per the user's resumed-work instruction, implement a coherent feature
batch first, then add and run consolidated tests. Necessary compile/smoke checks
continue during development; all release and final coverage gates still apply.

Source now registers v17 login security (schema 17): `security_events`,
`security_login_fingerprints`, `security_login_thresholds` and
`security_notification_settings`. Successful password, bootstrap and OIDC
sessions commit the session row, last-login time, audit event, security event,
known source/device fingerprints and notification outbox rows in one
transaction, so a failed commit issues no usable session cookie. Failed
attempts persist bounded source/username thresholds with a lock expiry, a
deduplicated window marker and one suspicious event per crossing. Source and
device comparison uses keyed HMAC fingerprints with a persisted epoch, source
addresses are truncated to /24 or /48 (or replaced by a class label), and raw
user agents never enter events, payloads or metric labels. Administrator
endpoints live under `/api/v1/admin/security`, the owner policy writes require
recent authentication, and maintenance purges expired events, fingerprints and
threshold windows in bounded batches. The `/admin/security` events console is delivered in the same batch: an events tab with
kind/severity/reason/source/delivery filters, a dense event table, a detail Sheet carrying
the sanitized delivery rows and the acknowledgement form, and an owner-only policy editor
with a privacy preview. Console text lives in the admin chunk
(`webui/src/pages/admin/security-events-text.ts`) so the 250 KiB public-shell gzip budget is
unaffected. The console also carries the owner-only source reveal: the detail Sheet shows the
canonical address behind a confirmation only while privacy is `full_source`, and
`GET /api/v1/admin/security/events/{id}/source` answers `403` for coarse privacy, `404` for an
unknown event and `409` when no protected record exists, writing `security.event.source_reveal`
in the same transaction. Consolidated tests, host evidence and the
deployment of schemas 15 to 17 remain open.

Two supporting items from the same approval also landed. The theme build script
is now part of the quality gate: `scripts/check.sh themes` runs `bash -n
scripts/build-themes.sh` and `go test ./cmd/theme/...`, which re-packs every
source theme, validates each archive, and byte-compares published distribution
assets when present; CI runs that stage in the deployment job, and
`scripts/check.sh all` includes it. Public-shell pressure was relieved by
splitting the console-only translation catalogs into
`webui/src/context/admin-catalogs.ts`, registered by the admin chunk and by the
test setup instead of the shared `locale.tsx`; the public graph dropped from
256,175 B to 229,727 B gzip against the unchanged 256,000 B budget, so no budget
increase was needed.

The v20 `edge-access-policy` batch registers real DDL at version 20
(`MigrateEdgeAccessPolicy`): the `site_settings` rebuild that replaces the
address boolean with `public_address_mode` while preserving the old value
exactly, plus the bounded `public_telemetry_policy` row. The deployment
configuration contract, origin enforcement, the shared public address formatter
and the admin deployment status are implemented in the working tree; the
anonymous interaction endpoint and its MTS counters remain open. See the
[design status](./deployment-access-policy-design.md#implementation-status-2026-10-15).

The v18 resumable upload now registers real DDL at version 18
(`backup_upload_sessions`, `backup_upload_chunks`) with per-owner open-session
and history limits, idempotent chunk retries, quarantine on conflicting bytes,
expiry, bounded staging cleanup and pruning. Chunk bytes live only in a
`0700` session directory as `0600` files, session identifiers are never file
paths, and finalization fsyncs an atomically assembled archive, re-runs the same
complete ZIP, checksum, SQLite, MTS and schema validation, and records a
`validated` backup without ever staging or applying a restore. The owner-only
API adds `GET/POST /api/v1/admin/backups/uploads`,
`GET /api/v1/admin/backups/uploads/{session}/status`,
`PUT .../chunks/{index}`, `POST .../finalize` and `DELETE .../{session}` (the
status endpoint carries a trailing literal so it cannot collide with the
deployed `GET /api/v1/admin/backups/{id}/download` pattern); the
console replaces the single-request upload with a dialog that hashes locally,
uploads with bounded parallel workers, retries with jitter, cancels and resumes
explicitly from the server-reported received indexes. Consolidated tests, host
evidence and deployment remain open.

### `v19` advanced-network-diagnostics contract

- **Dependency**: deployed `v11` remote-operations semantics (per-node
  credentials, at-most-once start, cancellation, local policy intersection),
  the `v8` metric catalog and the `v9` application-secret lifecycle. The user
  approved this batch with the 2026-10-15 blanket decision.
- **Schema**: migration `v19` adds `diagnostic_policies`, `diagnostic_targets`,
  `diagnostic_jobs`, `diagnostic_attempts`, `diagnostic_hop_identities`,
  `diagnostic_capabilities` and `diagnostic_budget_ledger`, and widens the
  deployed `application_secret_state` cursor bound from 14 to 15 so the new
  `diagnostic_hop` rotation source can complete. All of it is application
  state: no latency, loss, throughput, jitter, byte or retransmit column exists
  in SQLite.
- **MTS boundary**: `diagnostic_mtr_latency_ms`, `diagnostic_mtr_loss_percent`,
  `diagnostic_mtr_sent_packets`, `diagnostic_mtr_received_packets`,
  `diagnostic_iperf_bytes_per_second`, `diagnostic_iperf_bytes`,
  `diagnostic_iperf_retransmits`, `diagnostic_iperf_jitter_ms` and
  `diagnostic_iperf_loss_percent` are registered non-public builtins with the
  fixed `node/job/hop/stat` and `node/job/direction/protocol/role` tag sets.
  Nothing else may become a tag, and every name/tag is an enum or a bounded
  identifier.
- **Backup impact**: none beyond the standard `platform.sqlite` snapshot plus
  the already-registered logical MTS export; hop identities use
  `secretbox.DiagnosticHopAAD(jobID, hopIndex)` and therefore participate in
  the deployed rotation, backfill and restore validation.
- **Approval unit**: target/policy administration, job lifecycle, Agent lease
  and progress protocol, MTS persistence and result reads ship as one reviewed
  batch. Private destinations, UDP, reverse direction, policy expansion and
  unknown-attempt retry stay owner-only.
- **Rollback contract**: dropping the seven tables and restoring the previous
  `application_secret_state` shape returns a `v19` database to the `v18`
  shape. Diagnostics are disabled by default in both the deployment flag and
  the server policy, so a rollback needs no product confirmation; new MTS
  measurements simply become unreachable until an explicit operator decision.

### `v19` implementation status, 2026-10-15

The server control plane is implemented and verified in the working tree:

- `internal/model/diagnostic*.go` owns the policy, target, job, attempt and
  measurement contract, including hard maxima and the fixed tag vocabulary.
- `internal/diagnostics` owns policy and target administration (IPv4/IPv6
  approval with DNS pinning, forbidden-range denial, private allowlists),
  durable job creation with transactional global/target daily-byte
  reservations, the `queued -> leased -> running -> terminal` state machine,
  attempt nonces with at-most-once start, sequence-ordered progress whose
  batches reach MTS before the cursor is acknowledged, budget settlement,
  cancel/retry/reconcile, encrypted hop identities and MTS result reads.
- `internal/store/mts_diagnostic.go` is the only writer of diagnostic numeric
  values; `DeleteDiagnosticJob` removes the registered series before SQLite
  metadata so a failed delete leaves the job addressable.
- `internal/api/routes_diagnostics.go` registers the owner/admin control plane
  under `/api/v1/admin/diagnostics/*` and the per-node Agent endpoints under
  `/api/v1/agent-diagnostics/*`; no public route exists.
  `BEAT_DIAGNOSTICS_ENABLED=true` is required at startup, and a
  `diagnostics` worker reconciles expired queues, unauthorised leases and
  lost completions every 15 seconds.
- `internal/applicationsecrets/diagnostic_sources.go` registers the rotation
  source, with a composite `job#hop` cursor because the hop table has no `id`
  column.

An end-to-end fixture test covers policy defaults, forbidden destinations,
owner-only writes, target creation, capability advertising, job creation,
lease/start idempotency, progress deduplication, hop decryption, completion,
cancel/retry/reconcile and result reads.

The Agent runtime and the wire contract land in the same working tree:

- `internal/diagnosticprotocol` is the single source of wire truth: the
  capability document with canonical bytes and digest, the lease/job payload,
  samples, progress and completion frames, and the shared destination policy
  (`HardForbiddenAddress`, `RestrictedAddress`, `NormalizeAddress`,
  `FamilyMatches`). The server delegates its address decisions to the same
  functions, so server approval and the Agent allowlist cannot drift.
- `internal/agentdiagnostics` owns the Agent side: local configuration with
  hard maxima, an address/CIDR allowlist that never resolves DNS, private
  0700/0600 state, per-attempt journals, a fixed-argument `mtr`/`iperf3`
  executor with an emptied environment and a killed process group, the
  per-node HTTP client, and a runner that advertises capability, leases one
  job at a time, journals *before* acknowledging start, streams bounded
  sample batches, cancels on heartbeat, reports the terminal state, and
  leaves unprovable leftovers for a startup `unknown` verdict.
- `internal/agent/config.go` accepts an optional `diagnostics` section and
  `cmd/agent` starts the runner beside the existing runtime components. The
  admin node-setup page documents every field, and
  `internal/agent/config_reference_test.go` fails if the struct and that page
  drift apart.

A wire-level test drives the real Agent client against the real server service
(only socket binding is skipped, so paths and envelopes still come from
production code): advertise, create, lease, start, heartbeat, three ordered
progress batches with encrypted hop identities, completion and result reads,
plus cancellation and stale-nonce rejection. Writing it exposed and fixed a
server defect where `settleBudget` used a bare `UPDATE`, so completing a job
without a reservation row (MTR reports no reserved bytes) failed with
`ErrConflict`; settlement and release now upsert.

The `/admin/network` diagnostics workspace now ships in the same working tree:

- the page keeps the reviewed scheduled-quality-task editor behind its own tab
  and adds an advanced-diagnostics tab that renders the real workspace;
- `diagnostics-panel.tsx` composes state/node/target filters (name labels, ID
  values), a manual refresh, the create dialog, the dense job table and the
  detail sheet; the create dialog offers only approved targets, bounds every
  numeric field by the policy/target maxima before submitting, and requires the
  explicit bandwidth acknowledgement for iperf3;
- the detail sheet derives the timeline from real job fields, masks MTR hop
  addresses behind an explicit reveal, and shows the final sender/receiver
  throughput and byte counts for iperf3;
- diagnosing never asks for a free-form destination, and every selectable
  option displays a name while submitting an identifier.

Frontend evidence in this sandbox: `tsc -b` clean, `oxlint` adds no warnings,
and the ten diagnostics test files (60 cases) pass. New frontend statements are
93.4 percent and lines 94.9 percent covered.

Remaining for this batch: the consolidated coverage, race and deployment
evidence on the host, including the browser smoke/E2E run that this sandbox
cannot execute.

### `v21` traffic-calibration implementation status, 2026-10-15

The accounting spine is implemented in the working tree:

- `internal/model/traffic_calibration*.go`, `internal/store/sqlite_migrations_v21.go`,
  `internal/store/traffic_calibration*.go` own the append-only ledger, its
  kind-specific checks, request-id idempotency, revoke/replace corrections,
  cursor pagination and redacted audit evidence.
- `internal/store/mts_traffic_boundary.go` adds the durable per-node
  traffic-write boundary (`TrafficAccountingBoundary`) and
  `QueryTrafficUsageAfter`; `internal/store/traffic_resolver.go` resolves the
  calibration-aware cycle totals.
- `internal/api/routes_traffic_calibration.go` registers the four owner-only
  endpoints with recent authentication and the metadata audit actor; the
  public and admin node summaries add only `accounting_mode` and
  `calibrated_at`; `internal/alerter` resolves `traffic_usage_percent` through
  the same resolver. Raw MTS rows, charts and operational reports are
  untouched.
- `internal/api/metadata_management.go` gains `metadataOwner`, reusing the
  existing audit-actor injection for owner-only mutations instead of a second
  authorization path.

- Administration UI: `TrafficCalibrationDialog` (set/reset, binary-unit input
  converted to canonical decimal bytes, mandatory reason, retry-stable request
  id, `AlertDialog` confirmation), `TrafficCalibrationHistory` (`Sheet`/`Table`,
  paginated, revoke only on the active set) and the public `Adjusted` badge; the
  new `traffic.*` keys exist in both locales.
- Failure-path auditing: every rejected mutation appends a
  `traffic_calibration.reject` row with outcome `failure` and a reason category
  only (`invalid`/`conflict`/`missing`/`failed`), never target values, reason
  text or request bodies; an audit write failure is logged and cannot turn a
  400/409 into a 500 or mask the original rejection.
- Retention: `MTSStore.WithTrafficCycleSource` clamps traffic-delta deletion to
  the oldest active cycle start in both `CleanupBefore` and
  `ApplyMetricRetention`, and fails closed when the node snapshot cannot be
  read, so an active cycle can never lose the raw deltas its calibration targets build on.
- Restore: archive validation replays `traffic_calibration_events` (the row
  limit, event shape, whole-second effective instants, node linkage and the
  active-set reference chain); a `v20`-or-earlier snapshot restores with an
  empty ledger and raw accounting. The erasure-boundary assertion is added with
  `v22`.

Sandbox evidence: `go build ./...`, `go vet`, `goimports-reviser` and
`golangci-lint` (0 issues) are clean; store, backup, model, alerter and the new
handler/route tests pass, including an end-to-end owner-API append that surfaces
`accounting_mode: "calibrated"` on the public summary, a rejection that leaves
two failure audits behind without leaking request content, an in-cycle traffic
delta that survives an aggressive cleanup, and five corrupted-archive cases that
each fail restore. The frontend gates are green: `tsc -b`, 165 files / 856 cases,
`oxlint` (0 errors) and `vite build`.

Remaining: the host-only race/coverage/browser and rollback drills.

### `v22` metric-erasure implementation status, 2026-10-15

The erasure control plane is implemented in the working tree:

- `internal/model/metric_erasure.go` owns the closed scope enum, the 8-state
  machine, the fixed fleet phrases, redacted error codes and request validation;
  `internal/store/sqlite_migrations_v22.go` adds `metric_erasure_jobs` with the
  request-id, one-active-target and one-active-fleet indexes plus the
  `nodes`/`network_tasks` `lifecycle_state` columns.
- `internal/store/metric_erasure*.go` owns create/replay/conflict handling, the
  in-transaction entity tombstone, read/status projections, durable leases
  (2 minutes, 6 attempts, capped backoff), cancel/retry/finalize administration
  and the restore reconciler (`claimed`/`deleting`/`compacting`/`finalizing`
  return to `queued`; an exhausted queued job fails with `lease_expired`).
- `internal/store/mts_erasure.go` takes the shared exclusive MTS operation lock,
  flushes pending writes, freezes the registered-measurement inventory, cutoff
  and catalog digest, and fails closed on any unregistered measurement; entity
  rows are removed only after MTS success, and compaction is a separate phase.
- `internal/metricserasure` runs the leased worker loop and resumes at the
  committed cursor after a restart.
- `internal/backup/metric_erasure_validation.go` replays the archived ledger
  (scope/state enums, target and request-id shape, attempt bounds, frozen
  cutoff, completion-time consistency, entity linkage and the
  one-active-target/one-active-fleet invariant) and rejects a corrupted
  snapshot; a `v21`-or-earlier archive is accepted as an empty ledger.
- `internal/api/handler/metric_erasure.go` plus
  `internal/api/routes_metric_erasure.go` expose the owner-only, recent-auth job
  API. Node and network-task `DELETE` now commit one durable job and return
  `202`; `internal/api/handler/async_delete.go` shares the exact-name
  confirmation contract. `internal/store/traffic_erasure_boundary.go` makes a
  successful erasure cutoff the calibration resolver boundary.
- `cmd/server` registers the `metric_erasure` worker, readiness check and
  Prometheus gauges (`beat_metric_erasure_jobs`,
  `beat_metric_erasure_active_jobs`).

Administration: `/admin/settings` now renders one unframed `Data and storage`
section with `Retention` and `Erase history` tabs. The erasure tab shows the
affected history class, entity removal, recent-backup readiness and the only
recovery path; fleet-wide scopes require the fixed phrase and entity scopes the
exact display name, and a missing recent ready backup forces a create-backup
action plus a separate acknowledgement. Node and task deletion dialogs reuse the
same shadcn `AlertDialog` confirmation.

Sandbox evidence: `go build ./...`, `go vet ./internal/... ./cmd/...`,
`goimports-reviser` and `golangci-lint` (0 issues) are clean; store, backup,
model, alerter and `internal/metricserasure` tests pass, including replay and
conflict, tombstone plus cancel restore, lease recovery with cursor
continuation, exhausted-attempt failure and retry, the full transition chain
with entity finalization, status aggregation, restore reconciliation and the
exclusive shared lock that blocks a backup snapshot. Frontend gates are green:
`tsc -b`, 168 files / 867 cases, `oxlint` (0 errors) and `vite build`.

Remaining: the host-only race/coverage/browser and rollback drills, plus the
deployed-MTS deletion drill.

### `v23` fleet-status-summary implementation status, 2026-10-15

- migration `v23` became real DDL (`internal/store/sqlite_migrations_v23.go`):
  `fleet_summary_schedules`, `fleet_summary_nodes`, `fleet_summary_groups`,
  `fleet_summary_channels`, `fleet_summary_runs` plus the due index and the
  `(schedule_id, period_key, kind)` unique index. Only `v24` remains a reserved
  no-op entry, so the consecutive `v1`-`v25` ledger and `V1`-fixture upgrade
  equivalence still hold;
- `internal/store/fleet_summary*.go` owns revision compare-and-swap CRUD, scope
  replacement, the single claim transaction that writes the run audit and
  advances the cursor, the test-run audit insert and the restore reconciler;
- `internal/fleetsummary` owns validation, next-occurrence and period-key
  calculation, scoped availability snapshots, the `fleet.status_summary`
  request, the 1-per-minute test budget and the 30-second scheduler. Catch-up
  claims at most one missed period per schedule and the process start enqueues
  nothing;
- `internal/api/handler/fleet_summary.go` + `internal/api/routes_operations.go`
  expose admin-only list/create/update/delete and
  `POST /{id}/test-run`; `cmd/server` registers the `fleet_summaries` worker and
  readiness entry, and `internal/backup/restore_reconcile.go` runs
  `ReconcileFleetSummaryRestore`;
- the notification layer accepts `fleet.status_summary`, exposes the
  `fleet.*` template data keys, lists the kind in the delivery filter and in the
  channel subscription UI (en + zh-CN).

Recorded deviations: one outbox message per schedule/period with the frozen
scoped channel set (the shared batch limit is two messages); `weekday` follows
the shipped frontend `0 = Sunday .. 6 = Saturday` contract instead of ISO 1-7;
run rows keep the enqueue outcome while the schedule row exposes the live
delivery outcome from the outbox receipt.

Sandbox evidence: `go build ./...`, `go vet`, `goimports-reviser` and
`golangci-lint` (0 issues) are clean; `internal/store`, `internal/backup`,
`internal/fleetsummary`, `internal/notificationqueue`, `internal/trafficreport`,
`internal/model` and the new handler/router cases pass. Frontend gates are
green: `tsc -b`, 168 files / 867 cases, `oxlint` (0 errors) and `vite build`.

Remaining: the host-only race/coverage/browser and rollback drills, plus the
`v24` and `v25` batches.

### `v24` application-data-lifecycle batch 1 implementation status, 2026-10-15

- migration `v24` became real DDL (`internal/store/sqlite_migrations_v24.go`):
  the single-row `application_retention_settings` table (policy, cleanup hour,
  last run/result/duration, deleted counts), four `alert_events` cursor indexes,
  and the `rule_name_snapshot` / `node_name_snapshot` columns with an idempotent
  backfill that keeps an empty string for already-deleted entities instead of
  falling back to an ID. The reserved no-op helper is deleted, so every
  `v1`-`v25` entry is now real DDL and the ledger stays consecutive;
- `internal/store/application_retention.go` owns the bounded batch deletes plus
  the `running`/`interrupted` recovery used at startup;
- `internal/applicationgc` supervises the `application_gc` worker (15-minute
  tick, at most one automatic run per UTC day, 500-row batches with a 25 ms
  yield, 10-minute run budget, fixed `timeout`/`cancelled`/`storage` failure
  codes, manual runs rejected with `409` while one is in flight);
- `internal/api/handler/application_retention.go` +
  `internal/api/routes_core.go` expose admin-only
  `GET`/`PUT /api/v1/settings/application-retention` and
  `POST /api/v1/settings/application-retention/run`; `cmd/server` registers the
  worker and readiness entry, and `internal/backup/restore_reconcile.go` runs
  `RecoverInterrupted`;
- Data and storage gained an "Application history" tab
  (`webui/src/pages/admin/application-retention-panel.tsx`) with its own API
  client, types, en/zh-CN catalog, and route-auth assertions.

Also shipped: `GET /api/v1/alerts/events` is cursor-paginated
(`internal/store/alert_event_list.go`, default 50 / maximum 200, opaque
`(triggered_at,id)` cursors bound to the normalized status/rule/node/time
filter). `CreateActiveEventTx` writes the immutable `rule_name_snapshot` /
`node_name_snapshot`, the list response returns them, and the admin table shows
"Deleted node" / "Deleted rule" instead of an ID. Frontend follows through
`useApiPage` + `useAlertEvents(status)`, so status filtering and paging happen on
the server.

`GET /api/v1/admin/audit-events` also moved to the cursor contract
(`internal/store/audit_event.go`): opaque `(created_at,id)` cursors bound to the
retained `action` / `actor_id` filters, `limit` 1-200 (default 50), response
`{events, next_cursor}` instead of `total`/`limit`/`offset`, and `400` for a
malformed, oversized, out-of-range, or cross-filter query. The admin audit panel
pages forward through `listAuditEvents({cursor, limit})`.

Sessions are also bounded now: `GET /api/v1/admin/sessions` returns only active
or recently revoked (24 h) sessions with an explicit `limit` (1-200, default 50)
and the current-session marker, and successful login enforces the
32-active-session cap inside the same transaction, revoking the oldest excess
sessions and writing one `session.cap_revoke` audit event.

Recorded deviation: the alert-event cursor is versioned and filter-bound but
not HMAC-signed, because it carries only values the caller already sent behind
an admin/service-token route.

Sandbox evidence: `go build ./...`, `go vet`, `goimports-reviser` and
`golangci-lint` (0 issues) are clean; the store, `internal/applicationgc`,
`internal/alerter`, `internal/notificationqueue`, handler, router and `internal/api`
suites pass. Frontend gates are green: `tsc -b`, 170 files / 880 cases, `oxlint`
(0 errors) and `vite build`.

### `v24` application-data-lifecycle batch 2 implementation status, 2026-10-15

The backup contract that was still open is implemented in the same `v24`
migration (`internal/store/sqlite_migrations_v24_backup.go`):

- `admin_backups` is rebuilt in the migration transaction with `staging`,
  `staged`, `interrupted`, `missing`, and `deleting` added to the state check,
  plus `restore_generation`, `previous_state`, `updated_at`, and
  `reconciled_at`. Historical rows are copied unchanged and start at
  `updated_at = created_at`; the baseline schema is updated to the same final
  shape, and a re-run is skipped once `restore_generation` exists;
  `admin_backup_quarantine` inventories quarantined orphan archives;
- `internal/store/backup.go` / `backup_page.go` / `backup_transition.go` /
  `backup_quarantine.go` own the lifecycle: `GET /api/v1/admin/backups` is
  cursor-paginated (default 50, maximum 200, opaque nanosecond
  `(created_at,id)` cursors bound to an optional `state` filter, response
  `{items, next_cursor}`), `BackupTransition` commits the state CAS and its
  audit event in one transaction, and `ExpireTerminal` applies the fixed
  90-day window to `failed`/`interrupted`/`missing` rows;
- `internal/backup/reconcile.go` and `reconcile_files.go` run the startup
  reconciliation between the data-directory lock and HTTP: `running` becomes
  `interrupted` or `ready`, `ready`/`validated` without a regular archive become
  `missing`, `deleting` completes idempotently, `staging` and unconfirmed
  `staged` rows roll back to `previous_state`, unconfirmed online journals are
  discarded, orphan archives are quarantined, and generated partials or stale
  journal temporaries are removed. Only regular files inside the backup
  directory are ever touched, and unsafe deletions stay visible as `deleting`
  instead of blocking startup;
- restore staging is a three-phase commit (staging + random generation, private
  journal, staged), `ApplyPending` applies only when the live row is `staged`
  with the journal's generation, and deletion commits `deleting` before the
  archive and row. A second concurrent staging attempt is rejected with `409`.

Recorded deviations: no restore idempotency-key table (a retry reads the
committed state through `GET` and is rejected with `409`); the local operator
recovery command writes `origin: "offline"` journals that stay applicable
without a live database; the backup retention window is a fixed constant.

Sandbox evidence for batch 2: `go build ./...`, `go vet`, `goimports-reviser`
and `golangci-lint` (0 issues) are clean; `internal/store`, `internal/backup`,
`internal/adminauth` and `internal/api` (listen-bound cases skipped) pass.
Frontend gates are green: `tsc -b`, vitest, `oxlint` and `vite build`.

Remaining: the host-only race/coverage/browser and rollback drills, the
`scripts/check.sh` gate, and the `v25` batch.

### Backup upload session status path, 2026-10-15

`GET /api/v1/admin/backups/uploads/{session}` collided with the deployed
`GET /api/v1/admin/backups/{id}/download` pattern: `net/http` refuses to
register two overlapping patterns where neither is a strict subset
(`/backups/uploads/download` matched both), which panicked at startup. The
unshipped resumable-upload session status endpoint moved to
`GET /api/v1/admin/backups/uploads/{session}/status`; the deployed download
path and every other upload endpoint are unchanged. The frontend
(`webui/src/lib/backup-api.ts`) and the handler test follow the new path.

## Deployed schema ledger, 2026-10-18

The allocation table above lists reviewed batches. It is **not** a status board:
every number from `v8` to `v30` now exists as a real migration, and the 8081
preview database has all of them applied. Evidence:

- `internal/store/sqlite_migrations.go` — `currentSchemaVersion = 30`, with the
  inventory `v1`…`v30` in order (`application_schema_baseline` …
  `accelerator_public_visibility`).
- Live preview database (`/var/lib/beat-preview/data/beat.db`, opened read-only):
  `schema_migrations` holds 30 contiguous rows, `max(version) = 30`, with
  `accelerator_public_visibility` as the latest migration.
- The v225 deploy probe printed `schema 30`, SQLite `integrity ok`, and
  `fk violations 0`; `/readyz` reported every worker and storage check `ok`.

Deployed migration names (authoritative; `sqlite_migrations.go` is the source):

| Version | Migration name | Registry batch |
| --- | --- | --- |
| `v1`–`v4` | `application_schema_baseline`, `agent_task_channel`, `custom_metric_tasks`, `alert_event_acknowledgement` | pre-registry baseline |
| `v5`–`v7` | `oidc_identity`, `theme_packages`, `agent_enrollment` | pre-registry baseline |
| `v8` | `metric_catalog_dashboards` | `v8` metric-catalog |
| `v9`–`v12` | `application_secrets`, `notification_delivery`, `remote_operations`, `alert_evaluation` | `v9`–`v12` |
| `v13`–`v17` | `geoip_node_lifecycle`, `accelerator_telemetry`, `agent_rollout`, `service_principals`, `login_security` | `v13`–`v17` |
| `v18`–`v21` | `resumable_restore_upload`, `advanced_diagnostics`, `edge_access_policy`, `traffic_calibration` | `v18`–`v21` |
| `v22`–`v25` | `metric_erasure`, `fleet_status_summary`, `application_data_lifecycle`, `dashboard_collaboration` | `v22`–`v25` |
| `v26`–`v28` | `agent_discovery`, `dashboard_versions`, `agent_task_output` | `v26`–`v28` (added here; previously unlisted) |
| `v29` | `agent_release_retention` | `v29` agent-release-retention |
| `v30` | `accelerator_public_visibility` | `v30` accelerator-public-visibility |

Backup envelope: the writer emits `format_version: 4`
(`internal/backup/archive_create.go`) and the validator accepts `1`–`4`
(`internal/backup/manifest.go`), so `v26`–`v28` extend the archive payload
through `platform.sqlite` without changing the envelope. Next free number is
schema `v31`; backup format `v5` remains unallocated until a design needs it.

Rule that keeps this ledger honest: a number is *deployed* only when the
migration exists in `sqlite_migrations.go` and the preview database reports it;
everything else in the allocation table stays a proposal.
