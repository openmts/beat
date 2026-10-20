# Single-command Agent enrollment

Updated: 2026-09-13

Status: implemented, verified, and deployed on 2026-09-13. The workspace server
is enabled under systemd and its database has migrated to v7; see
[local deployment notes](local-systemd-deployment.md).

## Operator workflow

The authenticated `/admin/agent-setup` page issues a fresh, random enrollment
token on each explicit generation. An operator copies one command to a Linux
machine that already has the matching `beat-agent` executable:

```bash
sudo ./beat-agent join --server https://beat.example.com --token '<join-token>'
```

The command registers a new node, writes its configuration, installs a stable
agent binary and systemd unit, enables startup at boot, and starts the service.
It does not require manually creating a node or editing JSON. Defaults:

- One token admits one machine, with a 30-minute lifetime.
- Node name is the local hostname; `--name` overrides it. An existing name is
  a conflict, never authority to replace or merge an existing node.
- The server's trusted client IP becomes the advertised host unless `--host`
  explicitly supplies one. SSH port uses the existing local discovery policy
  or `--ssh-port`; reporting interval is `5s`. Manual `report_interval` values
  must be Go durations from `1s` through `24h`, inclusive.
- Nodes use the existing default group and have no SSH public key assigned.
- HTTPS verifies certificates. HTTP requires explicit `allow_http` when
  issuing a command and `--allow-http` on the agent; the page explains its
  use on a trusted development network. Requests never follow redirects.

The earlier full discovery design remains future work: manual approval inbox,
multi-machine grants, policies and rollout management are outside this change.
This implementation preserves the existing per-node reporting identity.

## API and credentials

`POST /api/v1/admin/agent-enrollment-tokens` uses the existing complete admin
session, same-origin mutation and audit boundary. Its request is
`{server_url, allow_http}` and response data is
`{id, token, server_url, expires_at, command}`. Only token hashes are persisted;
the command and plaintext token are shown in the successful response once.

`POST /api/v1/agents/join` accepts the join token in the Authorization header
and a bounded JSON body:

```json
{
  "request_id": "stable-installation-request-id",
  "agent_token": "independent-agent-generated-credential",
  "node_name": "node-one",
  "advertised_host": "",
  "ssh_port": 22
}
```

Before the first network request, the agent generates and durably saves a
stable request ID and independent random reporting credential using the
existing `agentcredential` generator. The server stores only its hash. This
allows secure recovery without caching plaintext credentials on the server.

The response data is `{node_id, config}`; `config` contains the existing six
agent JSON fields. Server URL, effective node name, advertised host and SSH
port are validated before activating the configuration.

Enrollment token consumption and node creation share one SQLite write
transaction. A used enrollment token permits recovery only for the same
request ID and reporting credential, while it remains unexpired and the node
credential remains active. Other clients cannot reuse it. Repeated requests
return the same node without creating a duplicate or rotating a credential.
Deleting a node, rotating or revoking its credential must not allow a used
join token to restore access. Name conflicts and failed transactions leave
an unused enrollment token unconsumed.

If a pending installation outlives its invitation, a newly issued valid token
can resume it with the same saved request ID and reporting credential. The
server either creates the still-unregistered identity, or returns the same
active node's current configuration without changing it. A durable hash-only
identity binding distinguishes unregistered credentials from credentials that
were rotated, revoked or deleted; the latter cannot be registered again.

Strict input limits, no-store responses, bounded admission, trusted proxy
handling and generic credential failures apply to the new endpoints. Join
credentials never authorize monitoring reports or operations.

## Local installation and recovery

`join` checks Linux, root, a usable systemd manager, destination permissions,
file types and existing installation conflicts before consuming credentials.
The managed paths are:

| Resource | Default path | Mode |
| --- | --- | --- |
| Configuration | `/etc/beat-agent/agent.json` | `0600` |
| Enrollment and installation state | `/etc/beat-agent/` | files `0600`, directory `0700` |
| Stable executable | `/opt/beat-agent/beat-agent` | `0700` |
| Unit | `/etc/systemd/system/beat-agent.service` | `0600` |

Private state and configuration use same-directory temporary files, sync and
atomic rename. An installation lock excludes concurrent joins. Unknown
configurations, units and symlinks are not overwritten. A local receipt is
persisted before installing the final configuration, so interrupted writes or
failed systemctl calls can resume without consuming another token.

The unit runs as root with `UMask=0077` and fixed systemctl arguments, without
shell command interpolation. A successful CLI result confirms registration
and an active systemd service; continuing heartbeat and metrics are verified
separately. Service failures retain recoverable state and never delete a node
that might already be reporting.

`beat-agent service install --config /etc/beat-agent/agent.json` provides local
installation/recovery from an existing valid configuration, or restores a
missing default configuration from a complete valid local enrollment receipt.
It never overwrites an existing configuration with that receipt. The original
`beat-agent --config ...` foreground command remains supported.

## Persistence and verification

The new invitation and identity-binding tables are schema v7, following
deployed schema v6. Ordinary
server restarts preserve unexpired invitations. Backup and restore handle
invitations in private copies so restored snapshots cannot revive used
invitations; identity bindings remain with the node database. No metric
storage or archive-format change is required.

Acceptance covers credential uniqueness and boundaries, concurrent single
consumption, same-client retries, expired/revoked tokens, name conflicts,
lost responses, safe file writes, conflicting installations, systemd failures,
CLI validation, UI generation and secret lifetime. New code must meet the
repository's 90% coverage requirement. Final checks include backend tests,
race/coverage/lint/format/build, frontend test/coverage/lint/build, and an
isolated enrollment-to-reporting smoke test.

## Execution

- [x] Inspect existing authentication, reporting, schema and installation rules.
- [x] Specify the single-command protocol and retry behavior.
- [x] Implement server issuance, atomic enrollment and persistence lifecycle.
- [x] Implement agent CLI, private state and systemd installation.
- [x] Replace the default guide with command generation and update help.
- [x] Complete integrated review and functional runtime verification.
- [x] Resolve existing dependency audit findings and pass the full CI gate（2026-10-18 复核：
  `govulncheck ./...` 报告 0 个可触达漏洞、`npm audit --omit=dev --audit-level=high` 报告 0 个漏洞；
  完整 backend 门禁与覆盖率恢复见 `commercial-delivery-plan.md` 的 v191 章节）。
- [x] Deploy after approval, create the server systemd unit, back up data and migrate to v7.

Backend race/shuffle tests pass with 90.1% total statement coverage;
golangci-lint reports no issues. Frontend tests pass (257 tests, 90.39% line
coverage), with a production build and real-browser checks against an isolated
server. Real agent heartbeats and metrics were verified, including lost-response
recovery without duplicate nodes. Temporary systemd operations were tested
through a command runner, and generated units were checked with systemd-analyze.
The server subsequently passed a real systemd start and restart with the
retained workspace data; credentials and settings were preserved and the
original agent continued reporting.

The complete CI gate remains blocked by existing dependencies: govulncheck
reports GO-2026-6354 and GO-2026-6355 in `golang.org/x/crypto@v0.54.0`;
npm audit reports two high and two moderate findings in transitive shadcn
dependencies. Dependency versions were not changed in this implementation.
