# Local Beat systemd deployment

Verified: 2026-09-13. The current workspace server is managed by systemd and
starts at boot. Its existing data directory is retained.

| Item | Location |
| --- | --- |
| Service | `beat-server.service` |
| Unit | `/etc/systemd/system/beat-server.service` |
| Private environment | `/etc/beat/beat.env` |
| Active release | `/opt/beat/current` |
| Current release | `/opt/beat/releases/agent-join-20260913T025921Z` |
| Data | `/root/projects/beat/data` |
| Server | `http://127.0.0.1:8080` |
| Node setup | `/admin/agent-setup` |
| Agent executable for new installations | `/opt/beat/current/beat-agent` |

The service runs as root, matching the existing data ownership. It uses
`UMask=0077`, a read-only root filesystem and home directories, and an explicit
write allowance for the Beat data directory. Directories and executables use
`0700`; ordinary configuration files use `0600`. Logs are in the journal.

```bash
sudo systemctl status beat-server.service
sudo systemctl restart beat-server.service
sudo journalctl -u beat-server.service -f
curl --fail http://127.0.0.1:8080/readyz
```

After changing the unit, run `sudo systemctl daemon-reload` before restarting.
Environment-file changes take effect on restart. Future updates should install
an immutable release under `/opt/beat/releases`, stop this service, back up
`/root/projects/beat/data`, change the `current` symlink, then start and verify.
The generic `beatctl.sh` deployment example defaults to `/var/lib/beat`; this
workspace uses the data path above and its own installed unit.

## Backup and rollback

The verified cold backup before migration is:

`/var/backups/beat/pre-agent-join-20260913T025921Z/workspace-before-v7.tar.gz`

Its directory also contains `SHA256SUMS`, the exact previously running server
executable, private runtime/environment records and deployment verification.
The archive includes the complete data directory, previous workspace binaries,
agent configuration and frontend assets. Keep the backup directory private.

For rollback, stop the systemd service first, preserve the failed state, and
restore the matching v6 data and old server/static assets together. The old
binary cannot run against the migrated v7 database. Adapt the installed unit
to the restored paths and verify readiness before resuming service.

## Verification

A temporary instance passed startup, restart and clean shutdown under the same
systemd restrictions. The live service passed a real systemd restart and is
`enabled` / `active`. SQLite migrated from v6 to v7; integrity and foreign-key
checks pass. Node credentials, administrator credentials, SSH keys and site
settings match the cold backup. The original independently running agent is
online and its heartbeat and 25 reported metrics continue updating. The new
frontend matches the release; enrollment issuance rejects anonymous requests.
