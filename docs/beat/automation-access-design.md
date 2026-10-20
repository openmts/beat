# Scoped automation access

Updated: 2026-10-14

Status: backend core, the owner-only console surface, the Bearer data plane,
data-plane audit and the external `beat-cli` client are implemented. SQLite v16
persistence, token issuance/verification, policy validation, atomic audit, the
management API, server wiring, the `/admin/security` service-token tab (list,
create, rotate, revoke, one-time plaintext, recent-identity guard), the scoped
data endpoints
with per-token and per-source rate limits, and `service`-actor audit rows for
denials and privileged operations, failed-authentication admission control, and
lifecycle/latency metrics are done. Still open: optional security notifications,
restore/rollback revocation drills, and external CLI acceptance over
IPv4/IPv6 execution on a deployed instance.

## Goal and scope

Beat will support machine-to-machine administration through scoped service
tokens while keeping the public dashboard login-free and browser administration
session-based. The result supports backups, inventory synchronization,
read-only reporting, and controlled operational automation without creating one
permanent all-powerful API key.

## Competitor evidence and rejected model

Current Komari accepts a configured Bearer API key as an administrator
principal. Its role model treats that key as equivalent to an admin, including
sensitive-operation bypass.

Reviewed sources:

- API key setting:
  <https://github.com/komari-monitor/komari/blob/4077201f098774511eaf504f220c5f6be009346b/internal/config/settings.go>
- API key principal and admin equivalence:
  <https://github.com/komari-monitor/komari/blob/4077201f098774511eaf504f220c5f6be009346b/pkg/rpc/principal.go>
- authentication compatibility path:
  <https://github.com/komari-monitor/komari/blob/4077201f098774511eaf504f220c5f6be009346b/web/api/Auth.go>

Beat rejects a singleton recoverable full-admin key. Service tokens are
independent principals with least-privilege scopes, bounded resources, expiry,
revocation, and complete audit evidence.

## Security invariants

1. Plaintext tokens are random, shown exactly once, absent from URLs, logs,
   audit details, browser storage, backups, and management reads.
2. SQLite stores SHA-256 token hashes, a non-secret display prefix, metadata,
   scopes, resource constraints, expiry, and revocation only.
3. Token comparison is constant-time after a fixed-format parse. Unknown,
   expired, revoked, source-mismatched, and scope-denied tokens have a uniform
   public error shape.
4. Every service token has an absolute expiry. The default maximum is 90 days;
   only an owner with recent reauthentication may grant a longer bounded term.
5. Scopes are explicit server-defined constants. Tokens cannot request wildcard
   scopes, infer browser roles, create owners, manage TOTP/passwords/sessions,
   mint equal-or-greater tokens, change trust roots, or bypass confirmation
   gates.
6. Resource constraints are intersected with scope: node/group/channel IDs,
   public-versus-private data, allowed source CIDRs, and optional time windows.
7. Public read routes remain anonymous. Adding service tokens never makes the
   frontend require login or changes public visibility rules.
8. Agent tokens, enrollment grants, service tokens, sessions, and bootstrap
   authority remain separate credential types with separate prefixes and
   middleware.
9. Rate limits, body limits, origin behavior, and audit apply to service tokens.
   Browser CSRF/origin checks are not substituted for token authorization.
10. A token cannot use interactive terminal, generic shell task, file transfer,
    restore activation, theme bundle execution, signing-key changes, or rollout
    promotion in the first revision.

## Token format and lifecycle

Tokens use a recognizable versioned prefix:

```text
beat_service_v1_<base64url-random-32-bytes>
```

Creation requires an owner session with recent password/TOTP reauthentication.
The owner provides a name, purpose, expiry, scopes, resource constraints,
source CIDRs, and optional rotation predecessor. The response returns plaintext
once; closing the dialog clears it from React state.

Rotation creates a new independent token. An optional overlap window of at
most 24 hours allows controlled consumer migration. At overlap expiry the old
token is revoked transactionally. Immediate revoke takes effect on the next
request and invalidates any pending long poll; already committed operations are
not undone.

This feature uses canonical SQLite migration `v16` after Agent rollout `v15`,
when the central authorization surface is stable. It retains backup archive
format `v4`. The assignment is maintained in
[`commercial-parity-implementation-sequence.md`](./commercial-parity-implementation-sequence.md).

SQLite stores `service_principals`, `service_token_scopes`,
`service_token_resources`, and bounded usage/audit summary fields. Individual
request evidence belongs in the existing audit table, not an unbounded token
usage table.

## Scope model

The first revision supports narrowly defined scopes:

- `nodes:read` and `nodes:write-presentation`;
- `metrics:read` with explicit public/private and node/group constraints;
- `network-tasks:read` and `network-tasks:run-test`;
- `alerts:read` and `notification-test:send` with channel constraints;
- `traffic-reports:read` and `traffic-reports:run-test`;
- `backups:create`, `backups:read-metadata`, and `backups:download`;
- `audit:read` with owner-only creation and redacted fields;
- `health:read` for authenticated extended readiness details.

No scope permits administrator/user lifecycle, credential management, restore
staging/application, enrollment-grant creation, Agent token display/rotation,
remote terminal/tasks/files, notification secret reads, theme installation,
release publication, rollout promotion, or service-token delegation.

Scope evaluation uses a central authorization service. Handlers declare the
required action and resource; they do not inspect token strings or roles
directly. Browser principals continue through role/recent-reauth checks, while
service principals pass only when scope and resource constraints both match.

## Request authentication

Clients send `Authorization: Bearer <token>` over HTTPS. The middleware parses
the credential prefix before hashing, loads the active principal, validates
time and source policy using the trusted-proxy canonical client IP, applies a
per-token and per-source rate limit, and attaches a service principal to the
request context.

Responses include a request ID and standard authorization errors. They never
echo the token prefix on public failures. CORS is not automatically opened for
service tokens; cross-origin browser use is unsupported. CLI and server clients
must validate TLS normally.

Long-running backup creation records the initiating principal ID and continues
after request completion, but a later download still requires an active token
with matching scope. Revocation stops future polling/downloads without
corrupting an already-created backup.

## Implemented data plane

The Bearer middleware runs on the first-revision endpoints below. Requests
without a service token fall back to the previous browser guard, and public
read routes stay anonymous.

| Endpoint | Scope | Resource constraint |
| --- | --- | --- |
| `GET /api/v1/nodes/manage` | `nodes:read` | none |
| `PUT /api/v1/nodes/{id}` | `nodes:write-presentation` | node |
| `GET /api/v1/nodes/{id}/metrics` | `metrics:read` | node |
| `GET /api/v1/network/tasks`, `GET /api/v1/network/tasks/{task_id}/history` | `network-tasks:read` | none |
| `GET /api/v1/alerts/events`, `GET /api/v1/alerts/rules` | `alerts:read` | none |
| `POST /api/v1/alerts/channels/{id}/test` | `notification-test:send` | channel |
| `GET /api/v1/alerts/traffic-reports` | `traffic-reports:read` | none |
| `POST /api/v1/alerts/traffic-reports/{id}/test` | `traffic-reports:run-test` | none |
| `GET /api/v1/admin/backups` | `backups:read-metadata` | none |
| `POST /api/v1/admin/backups` | `backups:create` | none |
| `GET /api/v1/admin/backups/{id}/download` | `backups:download` | none |
| `GET /api/v1/admin/audit-events` | `audit:read` | none (session prefixes redacted) |
| `GET /api/v1/admin/runtime` | `health:read` | none |

Visibility is enforced after scope and resource constraints. A `public`
token may only read data proven public: node-scoped routes consult the node's
`is_public` flag through `nodeResourceVisibility`, and every non-public endpoint
(managed node inventory, alerts, network tasks, traffic reports, backups,
audit, health) is registered with `RequirePrivate`, so a public token is
rejected with a `visibility` denial. Endpoints without a visibility concept
treat the response as public only when they declare no resolver and no
`RequirePrivate` flag, which keeps the anonymous public routes unchanged. A
failed visibility lookup returns `503` instead of downgrading to an allow.

Prometheus counters record authentication outcomes
(`beat_service_token_authentications_total`), scope and resource denials
(`beat_service_token_denials_total`) and rate limits
(`beat_service_token_rate_limits_total`) with bounded enum labels only.
`network-tasks:run-test` and `network-tasks:read`'s task-level constraint stay
unwired because the task resource kind is not modelled yet.

## Administration experience

The Security area gains a shadcn `Service tokens` tab. Cards or compact rows
show name, display prefix, scopes, resource summary, creator, created/last-used
time, expiry, rotation state, source restriction, and active/revoked status.

Creation uses `FieldGroup`, `Field`, `Checkbox`, `Select`, `Popover`, and an
accessible one-time-secret `Dialog`. Node, group, channel, and scope labels are
human-readable; IDs are submitted internally. Scope groups explain their exact
effect through concise field descriptions, not marketing copy.

Revoke and rotate use icon commands with tooltips and `AlertDialog` impact
confirmation. Plaintext is never displayed again. Loading uses `Skeleton`, no
tokens uses `Empty`, and failures use `Alert` plus `sonner`. The page refreshes
in the background without a full reload and remains usable on mobile.

## Audit, metrics, and notifications

Audit events cover create, rotate, revoke, expiry, authentication failure,
scope denial, resource denial, and every successful privileged operation.
Actor type is `service`, actor ID is the stable principal ID, and the display
prefix may appear only in authenticated audit views. Request paths and resource
IDs are bounded and redacted according to existing audit rules.

### Implemented data-plane audit, 2026-10-14

`admin_audit_events.actor_type` distinguishes `admin` from `service`; the v16
service-principal migration adds it to existing databases with the `admin`
default, and every browser audit write keeps that default. Data-plane rows use
`action=service_token.request`, actor type `service`, the stable principal ID,
the operator-provided name, the resolved resource (or the scope when the route
has no resource) and a bounded detail JSON with `reason`, `scope`, `method` and
path only.

Already-authenticated denials are always recorded with the reason
`scope`, `resource`, `visibility` or `rate_limit`; failed or unavailable
authentication is recorded as `unauthenticated` or `unavailable`. Successful
mutations and administrative reads (`audit:read`, `backups:read-metadata`,
`backups:download`, `health:read`) are recorded after the handler with the real
HTTP outcome, while routine telemetry reads stay covered by `last_used_at` and
Prometheus counters so polling cannot flood the audit table. A handler response
of `400` or above is recorded as a failure with reason `handler_error`.

Audit writes never carry token plaintext, token hashes or session prefixes, and
never block an authorization decision: a failed write is logged and the request
outcome is unchanged. The `/admin/security` audit tab marks `service` actors.

Failed authentication is admitted before it can write a row: a separate source
bucket (default 30 attempts per second per canonical client IP) rejects further
unauthenticated attempts with `429` and a `Retry-After` header, and the rejected
attempt is counted under `beat_service_token_rate_limits_total{kind="source"}`
without an audit row. Legitimate tokens are unaffected because the failure
bucket is independent of both the per-token and per-source success buckets.

Prometheus metrics cover authentication outcomes, scope denials, rate limits,
active/expiring/revoked counts, and operation latency. Scope and reason labels
are bounded enums; token IDs, prefixes, names, IPs, and resource IDs are not
metric labels.

Implemented families are `beat_service_token_authentications_total{outcome}`,
`beat_service_token_denials_total{reason}`,
`beat_service_token_rate_limits_total{kind}`,
`beat_service_token_operation_seconds{scope}` (histogram over the fixed scope
catalog) and `beat_service_tokens{state="active|expiring|revoked"}`. The gauge
is refreshed at scrape time from `service_principals`; active means not revoked
and not expired (including rotation-overlap predecessors that still
authenticate), expiring means active with at most seven days left, and revoked
means `revoked_at` is set. A failed statistics query keeps the previous
snapshot instead of reporting a misleading zero.

Optional security notifications include token created, rotated, revoked,
expiring, repeated authentication failures, and source-policy violations. They
use the durable notification queue and never include token plaintext or hashes.

## External CLI (`beat-cli`)

`cmd/beat-cli` plus `internal/beatcli` implement the external automation client
required by the acceptance gate. It authenticates with a service token only and
never with a browser session.

| Command | Endpoint | Scope |
| --- | --- | --- |
| `beat-cli health` | `GET /api/v1/admin/runtime` | `health:read` |
| `beat-cli nodes list` | `GET /api/v1/nodes/manage` | `nodes:read` |
| `beat-cli nodes metrics <node-id> [--metric --from --to]` | `GET /api/v1/nodes/{id}/metrics` | `metrics:read` |
| `beat-cli backups list` | `GET /api/v1/admin/backups` | `backups:read-metadata` |
| `beat-cli backups create` | `POST /api/v1/admin/backups` | `backups:create` |
| `beat-cli backups download <backup-id> --output <path>` | `GET /api/v1/admin/backups/{id}/download` | `backups:download` |
| `beat-cli audit list [--action --actor-id --limit --offset]` | `GET /api/v1/admin/audit-events` | `audit:read` |

Global flags are `--server` (or `BEAT_SERVER`), `--token-file` (or
`BEAT_SERVICE_TOKEN`; `-` reads standard input), `--ca-file`, `--timeout` and
`--raw`. There is deliberately no `--token` flag, so plaintext tokens never reach
a process list or shell history, and a token file that is readable by group or
others is refused with the `chmod 600` remediation.

### Console-only commands (administrator session)

Metric erasure (`owner(...)`) and remote operation tasks (`router.admin`) are
browser-session routes; a service token cannot reach them by design. The CLI
covers them with a separate credential instead of widening any route:

| Command | Endpoint |
| --- | --- |
| `beat-cli erasure list [--limit]` | `GET /api/v1/admin/metric-erasure/jobs` |
| `beat-cli erasure get <job-id>` | `GET /api/v1/admin/metric-erasure/jobs/{job_id}` |
| `beat-cli erasure create --scope --request-id --confirm [--target]` | `POST /api/v1/admin/metric-erasure/jobs` |
| `beat-cli erasure retry <job-id>` | `POST /api/v1/admin/metric-erasure/jobs/{job_id}/retry` |
| `beat-cli erasure cancel <job-id>` | `POST /api/v1/admin/metric-erasure/jobs/{job_id}/cancel` |
| `beat-cli operations tasks list [--limit --cursor]` | `GET /api/v1/admin/operations/tasks` |
| `beat-cli operations tasks cancel <task-id>` | `POST /api/v1/admin/operations/tasks/{id}/cancel` |

Credential flags are `--admin-user` (`BEAT_ADMIN_USER`), `--admin-password-file`
(`BEAT_ADMIN_PASSWORD`, or `-` for standard input), `--admin-totp-code`
(`BEAT_ADMIN_TOTP`) and `--session-file`. The password file must be `0600`, the
same rule as a token file. `--session-file` caches the session cookie in a
`0600` file and reuses it while `GET /api/v1/auth/session` still succeeds, so
repeat invocations do not open a new console session each time.

Every console command logs in with `POST /api/v1/auth/login` (carrying the
server's own `Origin`, which the login route requires) and then opens the
recent-authentication window with `POST /api/v1/auth/reauthenticate`, because
the erasure routes reject sessions that have not confirmed the password
recently. Nothing about route authorization changes: the same `owner(...)` and
`router.admin` guards still apply, and the console session is never accepted on
the scoped data routes.

Behaviour that matters for automation:

- success prints the envelope's `data` field as indented JSON; `--raw` keeps the
  full envelope and non-JSON payloads pass through unchanged;
- non-2xx responses collapse into one bounded stderr line (status, bounded server
  message, `Retry-After` capped at 24 hours) and exit code 1; the token is never
  echoed;
- redirects are followed only within the same host, so the Bearer token cannot be
  replayed to another origin;
- `backups download` creates the archive with mode `0600`, refuses to overwrite
  unless `--force` is set, removes partial files on failure, and writes its byte
  count to stderr so `--output -` stays pipeable;
- IPv4 and IPv6 targets are both valid (`http://127.0.0.1:8080`,
  `http://[::1]:8080`).

Acceptance smoke, once per address family on the host that serves Beat:

```bash
go build -trimpath -o /tmp/beat-cli ./cmd/beat-cli
install -m 0600 /dev/null /tmp/beat-service-token   # paste the one-time token
/tmp/beat-cli --server http://127.0.0.1:8080 --token-file /tmp/beat-service-token health
/tmp/beat-cli --server 'http://[::1]:8080' --token-file /tmp/beat-service-token nodes list
/tmp/beat-cli --server http://127.0.0.1:8080 --token-file /tmp/beat-service-token backups list
```

Denied scopes must produce the same bounded message and a non-zero exit; the
`beat_service_v1_` prefix, `0600` file rule and missing-flag guidance are
validated before any request is sent.

## Backup, restore, and rollback

Backups contain token hashes and authorization metadata because they are part
of SQLite. They never contain plaintext. Restore preserves revocation and
expiry; startup revokes tokens whose absolute expiry elapsed while offline.
Operators may choose an owner-only restore option that revokes all service
tokens before routes become ready.

Rollback restores the matching SQLite backup and Server version. Tokens created
after that backup disappear; tokens revoked after that backup could reappear,
so rollback requires the same mandatory post-restore option to revoke all
service tokens and issue fresh ones.

## Test and acceptance gates

Tests cover entropy failure, format parsing, hashing, constant-time comparison,
one-time display, scope/resource intersection, role separation, source CIDRs,
trusted/untrusted proxy addresses, expiry boundaries, rate limits, rotation
overlap, revoke races, long jobs, audit redaction, notification redaction,
backup/restore, rollback revocation, and proof that forbidden capabilities
remain unreachable.

Frontend tests cover name-versus-ID display for all resource selectors,
one-time-secret clearing, scope validation, owner authorization, recent reauth,
rotation/revoke impact, expiration states, responsive layout, keyboard access,
and loading/error/empty states.

Acceptance requires an external CLI smoke test over IPv4 and IPv6, public
dashboard regression without credentials, `401/403` evidence for every denied
scope, immediate revocation evidence, no plaintext in SQLite/logs/audit/backups,
at least 90 percent coverage, race/lint/security/build gates, and restore drills.

## Approval boundary

This feature changes authentication, authorization, API, SQLite, audit, backup,
and operational behavior. It is canonical SQLite migration `v16`.
Implementation requires explicit approval of this complete design and a final
scope list; approval of browser authentication or Agent credentials does not
implicitly approve service tokens.
