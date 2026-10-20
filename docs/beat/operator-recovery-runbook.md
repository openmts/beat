# Local operator recovery

Use the same release binary and OS user as the stopped server. Recovery is
local only; there is no browser upload or unauthenticated recovery endpoint.
This procedure changes security state. Take a stopped-service backup first,
including SQLite/WAL, metrics and application keys. Leave the shared parent
lifecycle lock file in place.
Do not copy credentials into command arguments, environment values or logs.

## Preconditions

- Stop every server replica and verify the supervisor will not restart it.
  The command also refuses an occupied server lifecycle lock.
- Use a supported local filesystem (ext4, XFS, Btrfs, tmpfs, overlayfs, ramfs
  or F2FS); unknown types, NFS and SMB are rejected. Keep the data directory
  and all subdirectories at `0700`, files at `0600`, owned by the invoking user.
  Input archives must also be regular, owned `0600` files. Symlinked paths are
  rejected. A read-only backup mount is supported without changing its modes.
- The data directory's parent must be writable by that user: it holds the
  shared lifecycle lock and temporary validation copies. For containers, mount
  the parent volume, with the application data in its `data` child directory.
- An existing database must use this binary's complete current migration ledger,
  pass SQLite integrity/reference checks, and authenticate all registered secrets
  with its root key. Recovery does not upgrade or repair an invalid database.

## Inspect and unlock

Paths below are examples. Run under the deployment's own UID; invoking as root
against another UID's files is rejected.

```sh
beat-server admin-recovery inspect --data-dir /srv/beat/data
beat-server admin-recovery unlock-owner --data-dir /srv/beat/data --username owner
```

For a different database filename add `--db-path /srv/beat/data/custom.db`.
The database must be directly inside the configured data directory.
Inspection prints JSON containing the schema, key/integrity status, site,
owner names and enabled/TOTP state, active-session count and pending restore.
An empty directory reports `empty_instance`; its next action is validated
restore staging or normal first-run startup.

Unlock prints the database/site/schema/owner and affected actions, then reads
and verifies a replacement password twice. Enter the displayed confirmation
`UNLOCK "<site>" / <normalized username>` exactly, including the site quotes
and any displayed backslash escapes.
TTY input has echo disabled. For automation, use `--secret-fd 3` with an already
open private descriptor containing three newline-terminated values: password,
repeat password, confirmation. The caller's descriptor remains open. Do not
construct these values in shell command strings or environment variables.

Success resets that owner's password, enables the owner, removes TOTP, revokes
**all** administrator sessions and pending OIDC transactions, and writes
`security.break_glass_recovery` atomically. Local password login remains
available. No owner is created and no identity link or node token is changed.
After restart, verify readiness, log in with the replacement password, check
the recovery audit in Security, and enroll TOTP again. Every old session must
require a new login.

## Migration status without a working browser

`inspect` requires a complete current schema. When the deployment failed during
an upgrade, run the read-only migration report first:

```sh
beat-server admin-recovery migration-status --data-dir /srv/beat/data
```

It prints one JSON object: whether the database exists with private modes, the
integrity result, the current schema, the supported minimum/maximum, the ordered
pending migrations, the fixed codes (`database_absent`, `lock_conflict`,
`pending_restore`, `future_schema`, `ledger_invalid`, `integrity_failed`,
`insecure_mode`, `database_unreadable`) and the last sanitized
`migration.status.json` marker plus the `backups/` archive count. It takes the
same lifecycle lock as the server and reads a private copy, so it never changes
live bytes; a running server therefore reports `lock_conflict`. Use the reported
pending list with the pre-upgrade backup to decide between retrying the
deployment, restoring the matching previous release, or restoring the archive.

## Stage a complete restore

```sh
beat-server admin-recovery stage-restore \
  --data-dir /srv/beat/data --archive /srv/recovery/backup.zip
```

The command can create the exact missing `data` directory under an existing
parent. It refuses unknown files in a database-free directory, invalid existing
state, and a pending restore. It validates the entire archive before publishing
the normal pending journal. `restore_staged` means staging succeeded; the data
swap happens only on normal server startup. Inspect then reports pending state.

Restart the server with the same data/database paths. Startup revalidates and
applies the journal through its existing rollback procedure and clears restored
sessions. A restored owner suppresses first-run bootstrap; a backup with no owner
continues through normal bootstrap rather than inventing an owner during recovery.
Verify readiness, restored data and owner login. Preserve rollback paths recorded
in the applied journal until verification and the retention period are complete.
If startup fails, stop it again and follow the existing
[operations runbook](operations-runbook.md); do not delete a pending journal or
manually mix database, MTS and key generations.

## Deployment examples

For systemd, stop the actual unit, run the commands as its `User`, then start
that unit and verify its readiness endpoint. For example, where the configured
unit and account are both `beat`:

```sh
sudo systemctl stop beat
sudo -u beat /usr/local/bin/beat-server admin-recovery inspect --data-dir /srv/beat/data
sudo systemctl start beat
```

For Compose, stop the server service and run a one-off instance of the same image
with the parent data volume writable and backup directory read-only:

```sh
docker compose stop beat
docker compose run --rm --no-deps --entrypoint /usr/local/bin/beat-server \
  -v /srv/recovery:/recovery:ro beat admin-recovery stage-restore \
  --data-dir /var/lib/beat/data --archive /recovery/backup.zip
docker compose up -d beat
```

Use the actual service name, image binary path, container UID and volume paths.
Rootless Docker/Podman must use matching mapped ownership; do not loosen the
private modes to work around UID mappings. The archive must be readable by the
mapped container UID. Recovery is tested as an unprivileged UID.

For Kubernetes, scale the deployment to zero and wait for all server Pods to
terminate. Run a short-lived Pod with the same image and `runAsUser`/`runAsGroup`,
mount the parent PVC read/write plus the backup read-only, and set its command to
`beat-server admin-recovery stage-restore` with the paths above adapted to those
mounts. Delete the completed recovery Pod, restore replicas, and verify readiness
and login. Use a local/block-backed PVC; RWX NFS/SMB storage is unsuitable.

## Failure and recovery drills

A nonzero exit is failure. Diagnostics identify the phase without including
passwords, SQL or archive contents. Correct filesystem authority, stop the server,
resolve pending work, or supply a valid same-release database/archive as indicated.
Rejected unlock input never writes the original database. Inspect validates a
private temporary database/WAL copy and removes it on completion.

Practice on isolated volumes with a locked owner, disabled owner, expired/lost
TOTP, valid backup, truncated archive, missing root key, occupied lifecycle lock,
and interrupted startup. Record backup age (RPO), staging/startup/login durations
(RTO), audit presence, and old-session rejection. No production operation is
performed by the automated tests.
