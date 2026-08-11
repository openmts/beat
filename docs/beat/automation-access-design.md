# Scoped automation access

Updated: 2026-07-30

Status: reviewed design; implementation requires explicit approval

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

This feature uses canonical SQLite migration `v11` after Agent rollout `v10`,
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

Prometheus metrics cover authentication outcomes, scope denials, rate limits,
active/expiring/revoked counts, and operation latency. Scope and reason labels
are bounded enums; token IDs, prefixes, names, IPs, and resource IDs are not
metric labels.

Optional security notifications include token created, rotated, revoked,
expiring, repeated authentication failures, and source-policy violations. They
use the durable notification queue and never include token plaintext or hashes.

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
and operational behavior. It is canonical SQLite migration `v11`.
Implementation requires explicit approval of this complete design and a final
scope list; approval of browser authentication or Agent credentials does not
implicitly approve service tokens.
