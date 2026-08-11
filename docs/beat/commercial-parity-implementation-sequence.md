# Commercial parity implementation sequence

Updated: 2026-08-01

Status: reviewed execution baseline; every implementation batch still requires
explicit approval

## Purpose and authority

This document is the single version and dependency registry for Beat's reviewed
but unimplemented competitor-parity work. Individual design documents remain
authoritative for feature behavior, security invariants, API contracts, storage
boundaries, tests, and rollback. When a design's previous draft migration or
backup number conflicts with this registry, this registry is authoritative and
the design must be corrected before coding.

The current deployed baseline is SQLite schema migration `v1` and backup archive
format `v1`. No row, table, API, dependency, or runtime behavior described here
is authorized merely by assigning a version.

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
| `v2` | `identity-oidc` | OIDC providers, external identities, transactions, session attribution, and resource-bound root-key envelope format 1 | deployed account security | `v1` |
| `v3` | `theme-packages` | Theme packages, activation history, and signed market sources | `v2` | introduces `v2` |
| `v4` | `agent-enrollment` | Secure manual/bounded automatic Agent enrollment and pending-claim state | `v2`, per-node identity; ordered after `v3` for one consecutive release train | `v2` |
| `v5` | `remote-operations` | Shared wrapped-key secret lifecycle, TOTP/SSH/OIDC conversion, durable Agent operation queue, command library, and single-file transfer metadata | `v4`, `v2` compatibility envelope, administrative-mutation consistency | `v2` |
| `v6` | `notification-delivery` | Encrypted channel revisions, durable outbox, leases, retries, and delivery history | deployed `v5` application-secret lifecycle | `v2` |
| `v7` | `geoip-lifecycle` | GeoIP sources/cache, node lifecycle, reminders, and lifecycle events | `v6` notification routing | introduces `v3` |
| `v8` | `metric-catalog` | Governed metric definitions, per-metric retention, query policy, and dashboard templates | deployed MTS, `v7` | introduces `v4` |
| `v9` | `accelerator-telemetry` | Accelerator inventory/policy and MTS-only GPU series | `v8` | `v4` |
| `v10` | `agent-rollout` | Signed Agent releases, target snapshots, waves, attempts, and health gates | `v4`, per-node identity, release signing | `v4` |
| `v11` | `automation-access` | Scoped expiring service principals and token constraints | stable central authorization after `v10` | `v4` |
| `v12` | `login-security` | Durable login security events, persisted thresholds, fingerprints, and outbox producer | `v6`; ordered after `v7` for optional trusted GeoIP and after `v11` for final security vocabulary | `v4` |
| `v13` | `resumable-restore-upload` | Restart-safe owner upload sessions and chunk state | current backup validation and archive format `v4` | `v4` |
| `v14` | `advanced-diagnostics` | Approved diagnostic targets, jobs, attempts, budgets, cursors, and encrypted hop metadata | `v4`, `v5`, `v8`, `v10` | `v4` |
| `v15` | `edge-access-policy` | Canonical external access policy, public address privacy modes, and aggregate visitor telemetry policy | `v2`, `v3`, `v8`, `fleet-public`; ordered after `v14` for one consecutive release train | `v4` |
| `v16` | `traffic-calibration` | Append-only per-node billing traffic calibration, correction, revocation, and attribution events | deployed account security and MTS traffic aggregation; ordered after `v15` for one consecutive release train | `v4` |
| `v17` | `metric-erasure` | Durable fixed-scope MTS erasure jobs, entity tombstones, retryable compaction, and node/task deletion finalization | `v8` metric catalog, deployed per-node identity/network tasks, MTS operation lock; ordered after `v16` | `v4` |
| `v18` | `fleet-status-summary` | Fleet summary schedules, scoped runs, catch-up cursors, and durable notification-outbox production | `v6` notification delivery, deployed availability state, node/group model; ordered after `v17` | `v4` |
| `v19` | `application-data-lifecycle` | Bounded application-history retention, cursor feeds, session hygiene, and crash-safe backup record/archive/journal reconciliation | `v13` upload lifecycle, deployed account audit/backup, `v18`; ordered after all prior history owners | `v4` |

Migrations are never squashed in released environments. A development-only
fresh-database bootstrap may use an equivalent schema initializer, but tests
must also upgrade a real `v1` fixture through every consecutive migration and
compare its schema/invariants with a fresh `v19` database.

## Backup archive sequence

### Format `v1` - deployed baseline

Contains manifest, complete SQLite snapshot, logical fixed-list MTS export,
administrator data key, and checksums.

Migration `v2` keeps these payloads but requires readers to authenticate every
OIDC envelope against its archived root key and reconstructed AAD. This is a
validation strengthening, not a new payload or format number.

### Format `v2` - theme payloads

Introduced by migration `v3`. Adds content-addressed theme package assets and
manifest inventory. Readers for later formats continue accepting `v1` when the
restored database does not reference theme assets.

### Format `v3` - managed GeoIP database

Introduced by migration `v7`. Adds managed current/previous MMDB files and
manifest metadata. External read-only MMDB mounts remain external requirements.

### Format `v4` - catalog-governed MTS export

Introduced by migration `v8`. Replaces the fixed measurement allowlist in the
logical export with a catalog-versioned typed inventory. It includes every
registered built-in/derived measurement and bounded tag schema, including later
GPU and diagnostic measurements without another archive-format increment.

Format `v4` validation requires:

- catalog version and digest in the manifest;
- one canonical metric-definition inventory with key, type, unit, field/tag
  schema, and visibility class;
- every exported MTS row to match a registered measurement and bounded tags;
- restored SQLite catalog semantics to match the manifest before MTS replay;
- unknown measurements or incompatible unit/type changes to fail closed.

Migration `v9`, `v14`, `v15`, `v16`, `v17`, `v18`, and `v19` extend registered
application/catalog or scheduling state and backup inventory but do not change
the `v4` envelope. Migration `v13` changes upload transport only; it does not
change archive contents. A backup cannot overlap an active `v17` erasure, so its
SQLite job state and logical MTS export remain coherent.

Migration `v5` does not change the archive payload: its wrapped data-key registry
lives inside `platform.sqlite`, and the existing `secrets/admin-data.key` entry
remains the matching 32-byte root key. Every `v5+` reader must nevertheless
validate the complete root-key/registry/ciphertext relationship defined in
[`application-secret-lifecycle-design.md`](./application-secret-lifecycle-design.md),
not only key length and checksums.

## Schema-free release batches

These batches do not consume a SQLite migration number:

| Batch ID | Capability | Dependency and ordering |
| --- | --- | --- |
| `fleet-public` | Grouped-card-first fleet summary, search, filters, stable sorting, optional tables, visibility-aware refresh, and public/admin/heavy-route asset isolation | may ship after current baseline; country/expiry controls activate only after `v7` |
| `pwa-static-shell` | Manifest, versioned public-route static-shell cache, explicit offline state, and controlled update activation | after `fleet-public` bundle isolation; admin/terminal/chart chunks and all API/auth/WebSocket/metric requests remain outside the public precache |
| `wallboard-html` | Bounded HTML wallboard over existing public REST/WebSocket | after `fleet-public` and independent PWA cache verification |
| `operator-recovery-cli` | Stopped-service owner unlock, all-session revocation, empty-instance inspection, and validated first-run/existing-instance restore staging | after deployed account security; OIDC local-login restoration activates after `v2` |
| `migration-recovery-ops` | Local migration status/preflight, private failure marker, and owner-only read-only operations guidance without DSN editing | after `operator-recovery-cli`; tracks the then-current canonical migration and backup readers |
| `runtime-diagnostics` | Owner-only bounded runtime summary and ephemeral profile capture | after deployed account security; independent of Agent diagnostics `v14` |
| `runtime-resilience` | Per-worker supervision, truthful readiness and MTS lifecycle errors, one shared public metrics snapshot, bounded WebSocket fanout, and shutdown-safe connection ownership | after deployed operability and public metrics; composes with `agent-ingest-consistency`, `v15` origin policy and may use `v8` batch-latest queries |
| `ingress-governance` | Bounded HTTP bodies/targets, pre-KDF authentication admission, public snapshot/history query budgets, Agent ingestion limits, and cancellation-safe polling | after deployed trusted-proxy identity; coordinate with `runtime-resilience`, generalized by `v8` query policy and `v12` durable login security |
| `agent-ingest-consistency` | Stable Agent sample identity/time, deterministic MTS retry and traffic deltas, durable receipt acknowledgement, and contact-versus-telemetry freshness | after deployed per-node identity/MTS; coordinate with `ingress-governance` and `runtime-resilience`, register in `v8`, deploy before `v17` and mandatory Agent rollout |
| `administrative-mutation-consistency` | Explicit role/recent-auth/cache/audit route policy, safe pending TOTP, atomic SQLite mutation/audit writes, long-lived session invalidation, and truthful external phases | after deployed account security; deploy before adding more administrator mutations and require `v5`, `v6`, `v12`, `v17`, and `v19` to reuse its authorization/audit envelope and operation linkage |
| `localization-parity` | Complete English, Simplified/Traditional Chinese, Japanese, and Indonesian frontend catalogs | after `fleet-public`; every later frontend batch adds all locale keys atomically |

Theme runtime implementation is part of migration `v3`; it must not be confused
with the later static-shell service worker. Persisted wallboard presets would
require a new design and a migration after `v19`; the reviewed first
version uses bounded URL/in-memory presentation settings only.

## Approval and delivery units

Each migration row is an independent approval unit unless the user explicitly
approves a larger contiguous range. An approval for design work, a previous
generic `approve`, or one batch never authorizes later rows.

The recommended execution cadence is:

1. approve exactly one batch and freeze its design revision;
2. update migration/backup compatibility constants and fixtures;
3. implement with focused behavioral tests and at least 90 percent coverage;
4. perform code review, CI-aligned local gate, backup/upgrade/rollback drill,
   and IPv4/IPv6 acceptance;
5. deploy behind disabled/default-safe policy where applicable;
6. observe readiness, audit, MTS, scheduler, and Agent behavior;
7. record deployed evidence in the matrix before approving the next migration.

Schema-free batches follow the same gate but do not change the migration ledger.
They may be approved separately when their predecessors are deployed. High-risk
Agent capabilities remain disabled until both Server and Agent policies are
explicitly enabled after acceptance.

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

Migration `v5` is also the only owner of the application-secret primitive.
Later secret-bearing migrations register AAD/resource enumerators with it and
must not add another key file, envelope, or rotation implementation.

Backup readers accept the current format and explicitly tested earlier formats.
Writers emit only the current format. Restore never silently drops unsupported
payloads or MTS measurements. Agent protocol changes use advertised min/max
versions and a rolling compatibility window defined by the owning design.

MTS additions are append-only across a failed feature rollback. The older
binary may leave new registered measurements unreachable; deletion is the
separate owner-approved `v17` erasure operation after rollback evidence is safe.

## Change control

Before implementation starts, every referenced design must show the migration
and backup-format assignment in this registry. If new competitor evidence or a
dependency changes ordering, update this file and every affected design in one
reviewed documentation change before allocating code.

After `v19`, the next SQLite migration is `v20`. No design may claim `v20`
without first adding its dependency, backup impact, approval unit, and rollback
contract here.
