# Signed Agent rollout and platform support

Updated: 2026-10-08

Status: authorized implementation under the commercial delivery registry; acceptance pending

## Goal and scope

Beat will provide a controlled Agent release and rollout system instead of
requiring operators to replace every binary manually. The design prioritizes
signature verification, bounded blast radius, observable health, atomic local
replacement, and recoverable rollback.

It also defines an explicit platform support matrix. A platform is not marked
supported merely because `go build` succeeds.

## Competitor evidence and rejected model

The inspected Komari Agent checks for updates at startup and every six hours,
updates from GitHub releases, and exits for its service manager to restart. Its
build script targets Windows, Linux, macOS, and FreeBSD across amd64, arm64,
386, arm, and Linux loong64, with exclusions.

Reviewed sources:

- automatic update loop:
  <https://github.com/komari-monitor/komari-agent/blob/e5aefd4f25f261e3b337671e1d71c1f4c4842b8f/update/update.go>
- Agent startup behavior:
  <https://github.com/komari-monitor/komari-agent/blob/e5aefd4f25f261e3b337671e1d71c1f4c4842b8f/cmd/root.go>
- platform build matrix:
  <https://github.com/komari-monitor/komari-agent/blob/e5aefd4f25f261e3b337671e1d71c1f4c4842b8f/build_all.sh>

Beat will not copy a direct, uncoordinated GitHub self-update model. A release
asset appearing upstream is not sufficient authorization to replace a fleet.

## Trust and safety invariants

1. Only an owner can publish, promote, pause, abort, or roll back a rollout;
   every action requires recent reauthentication and a durable audit event.
2. Agents accept only a signed Beat manifest whose trusted signing key was
   installed out of band or pinned in the current trusted binary/configuration.
3. The manifest binds version, channel, platform, architecture, exact size,
   SHA-256 digest, minimum protocol/database compatibility, release time, and
   expiry. Redirected or mirror content cannot change those fields.
4. The downloaded binary digest and signature are verified before any local
   state changes. TLS is required but is not the artifact trust root.
5. Rollouts progress through explicit canary and wave assignments with health
   gates. There is no fleet-wide default auto-promotion.
6. Pause and abort stop new assignments immediately. They do not kill a binary
   during atomic replacement.
7. The Agent keeps one verified previous binary and state metadata until the
   new version passes its watchdog window.
8. Replacement is same-filesystem and atomic. The updater never overwrites the
   running binary in place and never follows symlinks.
9. Container-managed Agents are `external_manager`; Beat reports the available
   update but never modifies the container filesystem or restarts the workload.
10. A rollout cannot rotate Agent identity, modify monitoring scope, enable
    remote operations, change Server trust, or alter unrelated configuration.

## Release manifest

CI produces one canonical JSON manifest per release and signs its canonical
bytes with Sigstore plus an offline-capable Ed25519 release key. The manifest
contains:

- schema version, release version, commit, build time, and channel;
- supported Server and Agent protocol range;
- artifacts keyed by OS/architecture/package kind;
- exact filename, size, SHA-256, SBOM/provenance references, and executable
  entry point;
- release notes digest, revocation state, and manifest expiry;
- signing-key ID and optional next-key transition proof.

The Server imports a manifest by verified local upload or hardened HTTPS fetch
using the existing SSRF-safe transport pattern. It stores the verified manifest
and artifact metadata in SQLite, but artifacts live in a private `0700` release
directory as `0600` files. Executable permission is applied only to the final
verified Agent staging file.

## Rollout model

This feature uses canonical SQLite migration `v15` after secure enrollment `v7`
and the release-signing foundation. It retains backup archive format `v4`. The
assignment is maintained in
[`commercial-parity-implementation-sequence.md`](./commercial-parity-implementation-sequence.md).

SQLite stores releases, rollout policy, immutable target snapshots, per-node
assignment state, attempt history, health observations, pause/abort reason, and
audit linkage. Assignment uses the per-node Agent identity from the deployed
token model and, when implemented, the secure enrollment identity.

A rollout defines:

- source and target versions;
- channel and eligible platform matrix;
- explicit node/group/tag filters resolved to an immutable target snapshot;
- canary count or percentage;
- ordered waves, maximum concurrent downloads, and minimum observation window;
- health thresholds for report freshness, crash/restart count, protocol errors,
  metric validation failures, and optional task failures;
- maintenance window, bandwidth limit, retry policy, and rollback policy.

Nodes with unknown current versions, unsupported platforms, stale heartbeats,
active remote tasks, or insufficient writable space are held with a visible
reason. They are not silently skipped as successful.

Promotion is manual by default. An optional owner-approved automatic promotion
may proceed only after the complete observation window and all thresholds pass.
Any threshold breach pauses the rollout; it never automatically widens the
wave.

### Transient error budget after a target turns healthy

Owner decision (2026-10-18, option A): a target that already passed the health
gate keeps one transient error allowance for the counter thresholds (restarts,
protocol errors, metric errors, task failures). A single observation that
exceeds a threshold by one, for example one `429` on `POST /nodes/report`
recorded as one metric error, no longer fails a healthy canary and no longer
pauses the rollout. The tolerance is recorded as an `assignment.healthy`
event with reason `transient_error_tolerated`, and the target keeps its
`healthy_since`. A second breach past the allowance still fails the target
(`metric_error_threshold` and friends) and pauses the rollout with
`health_gate_failed`. Targets that are still inside the observation window
keep the strict gate, and explicit `failed` or `rolled_back` outcomes from the
Agent ignore the allowance entirely.

## Agent update protocol

The Agent periodically asks the authenticated Server for its assignment and
sends current version, platform, update capability, service-manager kind,
available space, and previous update outcome. The interval is jittered and the
Server can return a bounded next-check time.

For an assigned release the Agent:

1. validates manifest signature, expiry, target identity, compatibility, and
   downgrade/rollback authorization;
2. downloads to a newly created `0700` private staging directory with resumable
   range requests, size cap, and incremental SHA-256;
3. verifies exact size, digest, executable format, and optional platform
   signature;
4. writes an fsynced update journal and atomically publishes the staged binary;
5. asks the external service manager or a tiny fixed updater helper to restart;
6. reports startup success only after configuration parse, identity load,
   Server authentication, one valid sample, and watchdog observation;
7. atomically restores the previous verified binary when the watchdog fails.

The Agent never executes a downloaded installer script or Server-supplied shell
command. Windows replacement uses a signed helper with fixed operations;
Unix-like systems use rename within the installation directory. Service-manager
integration is explicit for systemd, Windows Service Control Manager, launchd,
and rc.d. Unknown managers are manual-only.

## Platform support matrix

Release states are:

- `supported`: CI build, unit/race tests where available, packaging, install,
  upgrade, rollback, service-manager, telemetry, and IPv4/IPv6 acceptance pass;
- `preview`: CI build and core tests pass, but one or more operational drills
  remain incomplete;
- `build-only`: artifact compiles but receives no runtime support commitment;
- `unsupported`: no artifact is published.

The initial commercial target is:

- Linux amd64/arm64: supported;
- Windows amd64: preview, promoted after service/update/telemetry acceptance;
- macOS amd64/arm64: preview, promoted after launchd and permission acceptance;
- FreeBSD amd64/arm64: preview, promoted after rc.d and telemetry acceptance;
- Linux 386/arm/loong64 and Windows arm64: build-only until explicit demand and
  full test infrastructure exist.

The matrix is release metadata and administrator-visible. Unsupported nodes
never receive an assignment.

## Administration experience

The authenticated shadcn `Agent releases` workspace uses `Tabs` for Releases,
Rollouts, and Platform support. Release rows show verification, channel,
protocol range, artifact coverage, and revocation. Rollout cards show target,
current wave, succeeded/held/failed/rolled-back counts, observation timer, and
bounded failure reasons.

Creation uses a `Sheet` with named group/tag filters, canary/wave controls,
maintenance window, bandwidth limit, and health gates. Human-readable node,
group, release, and channel names are shown in every select; IDs remain request
values. Pause, abort, promote, and rollback are icon commands with tooltips and
confirmation dialogs containing exact impact.

The page updates in the background without reload. Stable card/table dimensions,
loading skeletons, empty/error states, keyboard focus, reduced motion, and
mobile overflow menus are required.

## Observability, backup, and rollback

Prometheus metrics cover assignment states, downloads, bytes, digest/signature
failures, update duration, watchdog outcomes, rollbacks, held reasons, and
rollout age. Labels are bounded release channel/platform/reason enums; node IDs
and versions are not unbounded labels.

Readiness covers the assignment scheduler and release store, not remote Agent
success. A paused or failed rollout is an operational alert. Revoked manifests
immediately stop new assignments and raise an owner-visible security event.

Backup includes manifest metadata, rollout state, audit evidence, and signing
trust configuration. Cached artifacts may be excluded only when the backup
manifest records their digests and restore requires verified re-import before
resuming rollouts. Restore always pauses active rollouts.

Server rollback restores its matching SQLite backup. Agent rollback is
per-node and uses the locally retained verified binary. Once a release has
changed the Agent configuration or protocol beyond the old Server range, the
rollout must be blocked unless a tested downgrade path exists.

## Release retention

Release objects are never removed implicitly by default. Retention is an explicit
opt-in and only ever touches objects the operator has already retired:

- `BEAT_AGENT_ROLLOUT_RETENTION_DAYS` removes terminal rollouts
  (`completed`/`aborted`) whose last update is older than the window.
- `BEAT_AGENT_RELEASE_RETENTION_DAYS` removes releases that are already revoked,
  are not referenced by any rollout, and were revoked before the window.

Both default to `0`, which keeps all history; a malformed or out-of-range value
fails startup instead of silently degrading. With at least one window configured,
the supervised `agent_rollout_retention` worker checks every six hours and runs
its first pass at startup. Deletion reuses the manual delete path: dependent
targets/attempts/events go with the rollout, and an artifact file is removed only
when no release references its digest any more. System-initiated deletions are
audited as `rollout.delete`/`release.delete` with `reason=retention` and
`actor_username=system`. In-flight rollouts, unrevoked releases and releases still
referenced by a rollout are never touched.

## Test and acceptance gates

Tests cover canonical signatures, key rotation/revocation, manifest expiry,
digest/size mismatch, wrong target, redirects, interrupted/resumed downloads,
disk exhaustion, symlinks, atomic replacement, power-loss journal recovery,
service-manager failures, watchdog rollback, pause/abort races, immutable
target snapshots, wave gates, restore-paused state, and container external
management.

Each `supported` platform requires native or virtualized install, upgrade,
failed-upgrade rollback, service restart, identity preservation, telemetry,
network interruption, and IPv4/IPv6 acceptance evidence. CI-aligned coverage,
race/lint/security/build gates and signed artifact verification must pass before
publishing or promoting a rollout.

## Cross-platform build verification, 2026-10-14

`CGO_ENABLED=0 go build ./cmd/agent` passes for linux/amd64, linux/arm64,
linux/386, linux/arm, linux/loong64, windows/amd64, windows/arm64, darwin/amd64,
darwin/arm64, freebsd/amd64 and freebsd/arm64; `./cmd/server`, `./cmd/theme` and
`./cmd/beat-cli` build for linux/amd64 and linux/arm64. `GOOS=windows|darwin|freebsd
go vet` passes for `./cmd/agent`, `./internal/agent/...`, `./internal/agentjoin/...`,
`./internal/agentinstall/...`, `./internal/agentoperations/...`,
`./internal/agentupdate/...` and `./internal/agentrollout/...`.

Two defects blocked that matrix. The 32-bit Linux builds compared statfs magic
numbers as `int64` on an `int32` field, so `BTRFS_SUPER_MAGIC` and `RAMFS_MAGIC`
overflowed; `safeTransferFilesystem` now compares 32-bit bit patterns and a
regression test pins the sign extension. The `cmd/agent` test package used
Unix-only `syscall.Kill`, so the signal case moved to `app_signal_test.go` behind
`//go:build !windows`.

Linux 386/arm/loong64 and Windows arm64 keep `build-only` state: the artifacts
compile, but install/upgrade/service-manager/telemetry acceptance still has to run
on comparable targets before promotion. Server-side fixtures keep their own
constraints: `internal/testutil/secrets.go` and
`internal/agentinstall/enrollment_integration_test.go` compile on Linux only
because they exercise the Linux-only application secret store.

## Approval boundary

This design changes release, Agent, SQLite, API, and deployment behavior. The
commercial delivery registry records the current implementation authorization.
Canonical SQLite migration `v15` follows `v14`; rollout routes and local binary
replacement remain disabled until their signing, authorization, restart, rollback
and platform acceptance gates are met. A signature-verifier foundation alone
does not satisfy the complete rollout contract.


Implementation scope and remaining integration are tracked in [agent-rollout-foundation.md](./agent-rollout-foundation.md).
