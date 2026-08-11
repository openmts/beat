# Operator recovery

Updated: 2026-07-30

Status: reviewed design; implementation requires explicit approval

## Goal and evidence

Beat needs a local recovery path when every browser administrator is locked out
or normal startup cannot reach the authenticated restore UI. An empty first-run
instance must also be able to restore a valid backup before creating a new
owner. Komari exposes local `chpasswd`, `disable-2fa`, and `permit-login`
commands plus install and database-recovery guides. Beat currently supports
authenticated password/TOTP changes and staged restore, but has no stopped-
service break-glass command or no-owner first-run restore path.

Evidence:

- <https://github.com/komari-monitor/komari/blob/4077201f098774511eaf504f220c5f6be009346b/cmd/chpasswd.go>
- <https://github.com/komari-monitor/komari/blob/4077201f098774511eaf504f220c5f6be009346b/cmd/disable2FA.go>
- <https://github.com/komari-monitor/komari/blob/4077201f098774511eaf504f220c5f6be009346b/cmd/permitPasswordLogin.go>
- <https://github.com/komari-monitor/komari/blob/4077201f098774511eaf504f220c5f6be009346b/web/install/install.go>
- <https://github.com/komari-monitor/komari/blob/4077201f098774511eaf504f220c5f6be009346b/web/recovery/recovery.go>

This design provides equivalent recovery without adding a remotely reachable
unauthenticated recovery page.

## Security invariants

1. Recovery is local-only and requires filesystem authority over the private
   Beat data directory. No HTTP, WebSocket, Agent, or service-token principal
   can invoke it.
2. The normal Server must be stopped. The command acquires the same exclusive
   data-directory lock as startup and refuses to continue when another process
   holds it.
3. Passwords and confirmation secrets are read from an attached TTY or an
   explicitly supplied file descriptor. They are never accepted as command-line
   flags, environment values, URLs, logs, audit details, or shell history.
4. Every account mutation uses the existing password hasher, model validation,
   store methods, immediate SQLite transaction, and audit vocabulary. Recovery
   never assembles ad hoc SQL.
5. Password reset, TOTP removal, local-login restoration, and session revocation
   commit atomically for one selected owner. A partial recovery is failure.
6. The command refuses a future schema, failed integrity check, missing or
   mismatched application root/wrapped key, invalid registered ciphertext,
   non-private files, a symlinked path, or an ambiguous owner selector.
7. Restore staging validates the complete archive before publishing the pending
   restore marker. It cannot selectively extract tables or bypass the existing
   startup restore journal.
8. Recovery never changes node tokens, Agent policy, service tokens, theme
   trust, notification secrets, or metric data.
9. Restore staging may run against a genuinely empty private data directory
   without an owner or SQLite database. It still validates the complete archive
   and never exposes a network upload route.

## Command surface

The Server binary gains an explicit `admin-recovery` command group. It does not
change normal Server flags.

```text
beat-server admin-recovery inspect --db-path PATH --data-dir PATH
beat-server admin-recovery unlock-owner --db-path PATH --data-dir PATH --username NAME
beat-server admin-recovery stage-restore --data-dir PATH --archive PATH
```

`inspect` is read-only and prints bounded status: instance state, schema version,
integrity, owner display names/usernames, enabled/TOTP/local-login state,
active-session count, key availability, and pending-restore state. A missing
database in an otherwise empty private data directory returns `empty_instance`
and valid next actions rather than failing. It does not print hashes,
ciphertext, token material, internal row IDs, raw audit details, or backup keys.

`unlock-owner` requires an exact normalized username and an interactive
confirmation that repeats the site name and owner name. It:

- validates and hashes a replacement password supplied twice;
- removes TOTP from that owner;
- re-enables the owner if disabled;
- restores local password login after OIDC migration `v2`;
- revokes every administrator session, not only the selected owner's sessions;
- writes `security.break_glass_recovery` with actor kind `local_operator`, the
  target username snapshot, operation flags, timestamp, and no secret values.

The first revision does not create owners, rename accounts, change roles, or
delete external identity links. If no owner exists, recovery stops and directs
the operator to validated backup restore instead of silently bootstrapping a new
security authority over an existing database.

`stage-restore` accepts one explicit regular file. It applies existing archive
size, path, checksum, SQLite, MTS, root/wrapped-key, full AAD-ciphertext,
catalog, and version validation, writes only into a random `0700` staging
directory with `0600` files, and atomically publishes the existing
pending-restore journal. Normal startup performs the actual swap and rollback
procedure.

## First-run restore

For a new installation, `stage-restore` may create the exact configured data
directory with mode `0700`, acquire its startup lock, and stage a validated
archive before any owner or application row exists. It refuses a nonempty
unrecognized directory, an existing database that fails integrity, an existing
pending restore, or live Server lock ownership. Starting the normal Server then
applies the restore before deciding whether bootstrap setup is required.

The browser never receives an unauthenticated `/install/restore` equivalent.
The runbook provides systemd, Compose, Kubernetes, and rootless-container
examples that mount the backup read-only and the data volume read/write, run the
same release binary as the deployment, and verify readiness plus restored owner
login afterward. Archive paths are operational inputs and never credentials;
passwords remain TTY/file-descriptor only.

This is the safe equivalent to Komari's empty-instance browser upload:
possession of network access alone is insufficient, while an operator with
data-volume authority can restore before creating conflicting security
authority.

## Locking and filesystem contract

Normal Server startup and every recovery command share a lock file under the
resolved data directory. The lock file is `0600`, opened without following
symlinks, and locked through an operating-system advisory exclusive lock.
Recovery refuses network filesystems when reliable locking cannot be proven.

All input paths are resolved once, checked to remain under their expected
roots, and reopened through scoped filesystem handles where available. The
command never uses globs, unresolved environment variables, or a recursive
delete target supplied by the operator. Generated staging is the only cleanup
scope.

## Operator experience

There is no web recovery form. `/admin/security` adds a quiet `Recovery` section
that explains the stopped-service command names, links to the local runbook, and
shows the most recent `security.break_glass_recovery` audit event after login.
It never renders a ready-to-run command containing absolute private paths or
credentials.

The CLI uses plain status lines and exits nonzero on every rejected invariant.
Before mutation it prints the exact database path, site name, schema version,
target owner, and actions. Success names the actions completed and states that
all sessions were revoked. Errors identify the corrective condition without
printing SQLite statements or sensitive values.

## Observability and operations

Recovery writes structured local logs to stderr with operation, phase, result,
and fixed error code. Secret inputs and path contents are redacted. A successful
database mutation writes the durable audit event in the same transaction. A
staged restore is represented by the existing restore journal and manifest.

The runbook covers stopped-service backup, lock verification, inspect, owner
unlock, restore staging, Server restart, readiness, login, audit verification,
rollback, and empty-volume first-run restore. RPO/RTO drills must exercise both
valid and corrupted inputs.

Migration status, dry-run upgrade validation, and the read-only Operations view
are defined separately in
[`migration-recovery-operations-design.md`](./migration-recovery-operations-design.md).
They reuse this lock and CLI authority but never add DSN editing or a network
migration action.

## Tests and acceptance

Tests cover:

- TTY and file-descriptor password input without argv/env/log disclosure;
- exact owner selection, normalization collisions, zero/multiple owners, and
  disabled owners;
- password validation/hash failures, TOTP/key loss, future schema, integrity
  failure, read-only files, symlinks, wrong ownership/mode, and lock conflict;
- one atomic password/TOTP/enabled/local-login/session/audit transaction with
  injected failure at every write;
- all sessions rejected immediately after recovery;
- archive traversal, digest, version, catalog, SQLite, MTS, key, size, and
  interrupted-stage failures using the existing restore validator;
- empty private directory, nonempty unknown directory, no-owner backup,
  existing pending restore, first startup apply, bootstrap suppression, and
  rootless volume ownership;
- fresh and upgraded databases, IPv4/IPv6 deployment paths, systemd, Compose,
  rootless container limitations, and recovery from an actual stopped release;
- at least 90 percent backend statement coverage plus race/shuffle, `go vet`,
  `goimports-reviser`, `golangci-lint`, vulnerability, release, and runbook
  gates aligned with CI.

Acceptance requires a locked-out owner to regain local login without exposing a
network recovery route, every old session to fail, the recovery audit to exist,
and a valid archive to be staged and applied through the existing rollback-safe
startup path on both empty and existing instances. Invalid input must leave live
state byte-for-byte unchanged.

## Version and approval boundary

This is schema-free release batch `operator-recovery-cli` in
[`commercial-parity-implementation-sequence.md`](./commercial-parity-implementation-sequence.md).
It uses the deployed administrator, session, audit, backup, and restore schema.
The local-login restoration action activates only after OIDC migration `v2`.
It does not change the backup archive format.

Implementation requires separate explicit approval because it adds a privileged
offline mutation surface, a shared startup lock contract, empty-directory
creation, and release/runbook behavior. Approval must confirm the stopped-
service requirement, all-session revocation, no owner creation, no network
recovery listener, full archive validation before restore staging, and first-run
restore before bootstrap setup.
