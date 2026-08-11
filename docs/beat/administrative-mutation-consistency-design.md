# Administrative access, mutation, and audit consistency

Updated: 2026-07-30

Status: reviewed schema-free design; implementation requires explicit approval

## Goal

Make every privileged Beat action enforce one explicit role, recent-auth,
response-secrecy, and audit policy. Authorized work must produce truthful,
redacted, attributable evidence without reporting failure after a committed
mutation, reporting success before asynchronous or external work finishes, or
allowing an audit-store failure to silently remove evidence.

This batch fixes the deployed account-audit integration and establishes the
contract that every later migration and schema-free feature must follow. It
does not put metric values in SQLite and does not claim atomicity across SQLite,
MTS, the filesystem, notification providers, or remote machines.

## Current evidence

The reviewed implementation diverges from the route-metadata and one-event-per-
covered-path contract in
[`account-backup-implementation-plan.md`](./account-backup-implementation-plan.md):

- `sessionAdmin` calls the handler first and inserts a generic audit event after
  the response has already been written. An insert failure is logged and the
  successful response is unchanged.
- Every state-changing route becomes `admin.mutation`; every authenticated read
  becomes `admin.read`. The resource is the route pattern rather than the real
  node, group, user, schedule, channel, backup, or session. Every deployed detail
  document is currently `{}`.
- `auth.bootstrap` and `auth.login` ignore audit errors. Their generated audit
  request IDs are different from the HTTP request IDs in structured logs.
- Logout and reauthentication mutate a session and then write audit separately.
  An audit failure makes the API return an error after logout or recent-auth
  state has already changed.
- Login creates a session, updates last-login in a second statement, ignores a
  failed compensating revoke, and writes audit separately. Password change also
  updates the password before revoking other sessions.
- Recovery wraps the router outside the post-handler observer. A panic unwinds
  past the observer, so the recovered `500` has no administrator audit event.
- While setup is required, the legacy bearer path bypasses principal context and
  the audit observer for normal administrator routes.
- Route registration distinguishes only public, Agent, session administrator,
  and session WebSocket access. Owner and recent-auth checks are scattered in a
  few account/backup services, so a newly registered sensitive route can omit
  its required privilege without a registry test failing.
- An owner can create another `admin` or `owner` without recent authentication.
  Beginning TOTP setup also requires only a live session: it immediately
  replaces the stored encrypted secret and sets `totp_enabled_at` to null before
  a code is verified. A direct request can therefore disable an existing second
  factor or leave an unverified replacement in durable account state.
- Node creation/token rotation, SSH-key import/generation/deletion, notification
  endpoint or credential changes, interactive SSH, and batch commands require
  only an ordinary owner/admin session. They issue credentials, alter egress, or
  execute remote commands without a recent identity confirmation.
- Terminal WebSocket authorization is checked only at upgrade. Revoking or
  expiring the session, disabling/demoting the user, or changing the password
  does not close an already-open terminal connection.
- Except for backup downloads, authenticated and secret-bearing JSON responses
  do not set `Cache-Control: no-store`. The deployed `/api/v1/auth/state`,
  protected `401` responses, and the shared response helper confirm that there
  is no central private-response cache policy for TOTP setup, one-time Agent
  credentials, sessions, audit, private node data, or management collections.

The deployed read-only sample on 2026-07-30 contained 18 audit rows: 16 generic
`admin.read` rows and one each for successful bootstrap and login. All 18 had an
empty detail document. The bootstrap audit request ID
`a5fd4d05-a8a6-4d88-93ca-c838bd7798fc` did not match HTTP request ID
`f3c99409-b417-4040-ae62-147402c88f0a`; the login pair also differed. This is a
traceability defect, not a current database-capacity incident.

The same static inventory found 64 HTTP routes behind generic `sessionAdmin`
and one terminal route behind `sessionWebSocketAdmin`. Production owner/recent-
auth helper calls exist only in account and backup code, and the only production
`Cache-Control: no-store` write is the backup download. Live read-only probes
confirmed no cache directive on `/api/v1/auth/state` or protected `401`
responses. The deployed database had one owner, no admin-role account, TOTP
disabled, and two unrevoked sessions, so role-differential production tests were
deliberately not created. Relevant adminauth/API/service tests pass but do not
enumerate route privilege/cache policy, TOTP replacement safety, or live socket
invalidation.

## Complete current mutation inventory

| Domain | Current routes | Current access, commit, and evidence risk |
| --- | --- | --- |
| Authentication | bootstrap, login, logout, reauthenticate | Split session/user/audit writes; ignored or post-commit audit errors; authenticated responses are cacheable by default |
| Administrators | create/update/delete user, change password, begin/enable/disable TOTP, revoke one/other sessions | Domain writes are separate from audit; create lacks recent-auth; TOTP begin durably disables/replaces before verification |
| Site and maintenance | update site settings, update maintenance settings, run maintenance | Settings commit without audit; a manual run audits only HTTP acceptance, not completion |
| Backup and restore | create, validate upload, delete, stage restore | SQLite, archive and journal effects are not one transaction; accepted/completed phases are not linked to the actor |
| Nodes and credentials | create/update/delete/sort node, rotate/revoke Agent token | Credential issuance lacks recent-auth/no-store; create/rotate can commit a one-time token and then fail while building an MTS-backed response |
| Groups | create/update/delete/sort/set default | SQLite transactions do not include audit and route patterns hide the actual group |
| Network tasks | create/update/delete/sort | Delete removes MTS history before deleting SQLite, so a later SQLite failure leaves a live task with erased history |
| SSH keys | import/generate/delete | Any administrator session can change remote credentials; mutation and audit are separate |
| Alerts | create/update/delete rule or channel, test channel | Channel endpoints/secrets can change without owner/recent-auth; test-send ignores delivery error and returns `200` |
| Traffic reports | create/update/delete/test schedule | Test delivery may return `200` with a failed delivery state; generic audit does not record that result |
| Terminal | open interactive WebSocket, execute batch | No recent-auth; an admitted socket survives session invalidation; no WebSocket audit; all-target batch failure still returns `200` |

Agent reports and probe-result ingestion are authenticated machine writes, not
administrator mutations. Their SQLite/MTS consistency is owned by
[`agent-ingest-consistency-design.md`](./agent-ingest-consistency-design.md).

## Invariants

1. A successful SQLite-only administrator mutation and its success audit event
   commit in the same SQLite transaction. If either write fails, neither commits.
2. No handler writes a success response until the authoritative SQLite
   transaction commits and all response data that can fail has already been
   prepared.
3. An HTTP status is transport evidence, not an operation result. Asynchronous,
   filesystem, MTS, notification, terminal, and remote operations record
   accepted and terminal phases separately.
4. A returned error never asserts that an external side effect did not happen.
   Ambiguous work is exposed as `unknown` or `partial`, with a retry or reconcile
   action owned by the feature's durable state machine.
5. Every audit event uses the exact request ID from request context, a concrete
   action, resource type, opaque resource ID, readable name snapshot, actor,
   session prefix, canonical client IP, bounded user agent, outcome, and stable
   reason code.
6. Passwords, OTP codes, bearer/session/Agent tokens, private keys, commands,
   command output, notification secrets, webhook query strings, archive paths,
   restore confirmation text, and metric values never enter audit, logs,
   metrics, URLs, or client errors.
7. Normal high-frequency administration reads are not inserted as security
   evidence. Only allowlisted sensitive reads are audited.
8. Route registration fails tests when a privileged mutation or sensitive read
   has no explicit audit policy. There is no generic fallback action.
9. The setup bearer is represented as a fixed `bootstrap_authority` actor with
   no token-derived value. Its authorized mutations follow the same audit rule.
10. Audit failure for an authenticated authorized mutation fails closed before
    commit or before an external side effect starts. Attacker-driven anonymous
    failures use bounded aggregation rather than one unbounded row per request.
11. Role, recent-auth, target scope, and response secrecy are resolved from the
    registered route policy before a secret is read, a body is committed, or an
    external side effect is admitted. Handler-local checks may strengthen but
    never replace that policy.
12. Authentication, session-protected, owner-only, and secret-bearing responses
    use `Cache-Control: private, no-store`, `Pragma: no-cache`, and appropriate
    `Vary` values on success and failure. Public metric/static caching remains
    governed separately and cannot cache a cookie-authenticated representation.
13. Long-lived privileged connections remain bound to the authorizing user and
    session. Revocation, expiry, user disablement, role loss, or policy loss
    closes affected connections within the documented bound.

## Explicit route policy

Each privileged route registers an immutable policy containing:

- action, resource type, operation class and sensitivity;
- required role and recent-auth requirement;
- response cache class, secret-bearing response flag, and long-lived-session
  revalidation policy;
- target resolver for ID and human-readable snapshot;
- allowlisted detail-field builder and redaction test corpus;
- whether success must be atomic with SQLite, is only `accepted`, or is an
  external result with `partial` and `unknown` states;
- rate/admission class and whether an idempotency or domain operation ID is
  mandatory.

Handlers receive the resolved policy and audit context. Middleware remains
responsible for authentication, same-origin enforcement and request context,
but it no longer fabricates mutation success from the final HTTP status.

Concrete actions include `node.create`, `node.update`, `node.delete.requested`,
`node.agent_token.rotate`, `group.default.set`, `alert.channel.test`,
`maintenance.run.accepted`, `maintenance.run.completed`, `backup.create.accepted`,
`backup.create.completed`, `terminal.open`, `terminal.close`,
`terminal.batch.completed`, `auth.login`, `auth.logout`, and
`admin.password.change`. Versioned action constants prevent ad hoc spelling.

## Authorization and sensitive-response policy

The policy registry is the authorization source for browser principals. It
keeps ordinary fleet operation available to both `owner` and `admin`, while
placing additional checks only on actions that create authority, reveal or
replace credentials, alter outbound trust, restore data, or execute remotely:

- ordinary owner/admin: node/group presentation, site settings, maintenance
  settings/run admission, network task/rule/schedule management, alert history,
  redacted channel metadata/test, and the administrator's own sessions;
- owner: administrator and backup metadata plus existing security policy;
- owner with recent authentication: create/update/delete an administrator,
  backup download/stage restore, and current combined notification-channel
  create/update/delete routes because they can change endpoints and credentials;
- owner or admin with recent authentication: create a node with its first Agent
  token, rotate/revoke an Agent token, import/generate/delete an SSH key, open an
  interactive terminal, and submit a batch command;
- self with recent authentication: begin, enable, replace, or disable TOTP and
  any future secret reveal. Password change continues to verify the current
  password and enabled TOTP directly rather than trusting only the window.

Audit list access remains available to both administrator roles with redacted
fields. A future export, expanded sensitive detail, or decrypted operation
content is owner-only and recent-authenticated. Migration `v6` may split
notification enable/test from endpoint/secret updates so admins retain routine
operations while owners control outbound trust. Migration `v5` may replace the
current SSH commands, but it cannot weaken the recent-auth admission rule.

The legacy setup bearer receives only the explicitly allowlisted migration
operations needed before the first owner exists. It never receives a synthetic
owner role and cannot use owner-only backups, account security, audit export,
secret reveal, or terminal admission.

All `/api/v1/auth/*` responses and every session-protected representation are
non-cacheable. This includes error responses and one-time TOTP/Agent credential
payloads. Public endpoints retain their own cache contract; the middleware must
not add `private` or `Vary: Cookie` to anonymous metrics/static responses.

## TOTP setup state

Beginning TOTP setup requires recent authentication and creates one bounded,
encrypted, session-bound pending setup in memory with a ten-minute expiry. It
does not change `admin_users`, disable the current TOTP, or replace the durable
secret. Repeating begin replaces only that session's pending setup.

Enable verifies a code against the pending secret and then commits the encrypted
secret, enabled timestamp, and audit event in one SQLite transaction. After
`v5`, durable encryption uses the user-bound shared envelope from
[`application-secret-lifecycle-design.md`](./application-secret-lifecycle-design.md);
this schema-free batch does not create another key or ciphertext format. Cancel,
expiry, logout, session revocation, user disablement, password change, process
restart, or successful enable clears the pending setup. If TOTP is already
enabled, replacement first proves recent authentication using the current
factor; an unverified new secret can never weaken login.

Pending setup count and bytes are globally bounded, secret values never enter
logs/metrics/audit, and the response is `no-store`. A process restart merely
cancels enrollment and requires the administrator to begin again.

## SQLite-only transaction pattern

Store commands accept a domain input and an already-redacted `AuditEnvelope`.
They begin one write transaction, validate current state, apply the mutation,
insert the audit event through a transaction-aware helper, and commit. The
handler then returns the committed resource or a response assembled from data
that was acquired before the transaction.

This applies to accounts, sessions, TOTP, site settings, maintenance settings,
groups, node presentation, node sorting, alert rules, alert channels, traffic
report schedules, SSH-key metadata, network-task definitions, and Agent-token
lifecycle state. Existing multi-row stores keep their domain invariants and add
the audit insert to the same transaction rather than opening a nested one.

Successful login moves session creation, last-login update and successful audit
into one store transaction. Migration `v12 login-security` later extends that
same transaction with security-event, source/device and notification-outbox
rows. Failed login aggregation remains owned by `v12` so an unauthenticated
attacker cannot grow the audit table without bound.

Logout, reauthentication and password change similarly become atomic. A
password change commits the password hash, other-session revocation and audit
together. The current session remains usable only according to the documented
password-change policy.

## One-time credential responses

Node create and Agent-token rotation must not perform a fallible MTS query after
the credential transaction commits. For a new node, the response uses the known
empty metric state. For rotation, any optional presentation snapshot is loaded
before mutation; the committed response contains SQLite identity/credential
metadata and the one-time plaintext token only.

If the response is lost, the token is never recovered from storage. Retrying
rotation issues a new token and immediately invalidates a possibly delivered
one. Retrying create for the same unique node resolves the existing node and
requires an explicit rotate action; it does not create a duplicate. The UI
states this ambiguity and never claims a failed transport means the old token
remains valid.

Credential issuance first enforces the route's owner/admin recent-auth policy.
Every success and error response is non-cacheable. The frontend holds plaintext
only in the active dialog, clears it on close/unmount/navigation, and never
persists it in browser storage, URL state, query caches, toasts, or error logs.

## Asynchronous and cross-boundary operations

This schema-free batch does not invent a generic cross-store transaction.
Instead it requires the existing or planned durable owner to carry the actor,
request and audit linkage:

- `v19 application-data-lifecycle` owns backup archive/row/journal states and
  startup reconciliation. Accepted, completed, failed and reconciled audit
  events reference the backup ID and restore generation.
- `v17 metric-erasure` replaces synchronous node/task MTS deletion with
  tombstones and retryable jobs. SQLite request state and accepted audit commit
  together; terminal audit follows durable MTS completion.
- `v6 notification-delivery` owns test-send and delivery attempts. HTTP success
  means the test was accepted or a final delivery result is returned; a provider
  failure cannot be silently converted to audit success.
- `v5 remote-operations` owns command/task/file attempt state. Until it ships,
  synchronous SSH batch results use `succeeded`, `failed`, `partial` or
  `unknown`; interactive terminal records durable open admission before SSH and
  a redacted close result without command or stream content.
- Maintenance uses its persisted run status. Manual acceptance records the
  actor/request before starting the goroutine; completion records success,
  failure or partial state linked to the same run identity.

An operation whose external effect is complete but whose SQLite completion
transaction fails remains nonterminal or `unknown`. Reconciliation may advance
it, but neither the API nor audit may fabricate rollback.

Interactive terminal admission is additionally tied to the exact user/session
and recent-auth deadline. A connection registry closes local sessions
immediately on logout, revocation, password change, user disablement, or role
loss; a bounded periodic store check covers expiry or out-of-band state changes.
Connections never outlive the recent-auth window and require a new upgrade after
reauthentication. Runtime-resilience owns connection/shutdown accounting while
this batch owns authorization invalidation.

## Failure, panic and availability semantics

Validation and authorization failures occur before mutation and receive stable
reason codes. Authenticated mutation failures are recorded with bounded details
when the audit store is available. An audit write failure is itself exposed by
structured logs, fixed-label metrics and readiness degradation, but the log
contains no resource name, body or secret.

Panic recovery owns a final failure observer with the already-resolved route
policy and principal. For SQLite-only work, a panic before commit rolls back and
records failure separately. A panic after an external action was admitted marks
the operation `unknown`; it does not claim failure or success solely from the
recovered `500`.

Selected sensitive reads are Agent install configuration, SSH-key metadata or
future secret reveal, backup download, audit export, terminal admission, and
future decrypted operation content. Ordinary list polling is excluded. Read
evidence is rate-limited or coalesced by actor, resource and time window to
avoid turning a valid session into an audit-storage denial of service.

## Audit detail and frontend

`detail_json` remains an allowlisted object and uses existing storage. Examples
are changed field names, role transition, enabled transition, target count,
delivery state, terminal duration bucket, node-name snapshot, key fingerprint,
backup state and stable reason code. It never stores before/after secret values.

The shadcn/ui `base-nova` audit view shows translated action/outcome labels,
actor display name, readable resource snapshot, time, source, request ID and a
bounded detail drawer. IDs remain copyable technical values, never the primary
display label. Filters use the stable action catalog and cursor pagination from
`v19`; they do not show raw route patterns as user-facing resources.

Sensitive actions use one shared reauthentication dialog in the current
workflow instead of requiring navigation to the Account tab. A `428` response
opens that dialog, retries only the same still-confirmed action once after
success, and never retains passwords or OTP codes. Owner-only controls remain
hidden for ergonomics, but API policy is the security boundary.

## Tests and acceptance

Backend tests cover:

- a generated inventory proving every privileged mutation and sensitive read
  has one explicit role/recent-auth/cache/audit policy and no policy is attached
  to a public route;
- table-driven owner/admin/setup-bearer tests for every privileged route,
  including stale/missing recent-auth, target mismatch, and proof that ordinary
  admin operations remain available;
- fault injection before domain write, between domain statements, during audit
  insert, at commit, after commit and while encoding the response;
- exact atomic rollback for every SQLite-only mutation and exact request/actor/
  session/resource attribution;
- bootstrap-authority mutation evidence without token material;
- login, logout, reauthentication, password/TOTP and session-revocation split-
  failure regression tests;
- TOTP replacement tests proving begin/invalid/expired/canceled setup cannot
  disable or replace an enabled factor, pending state is bounded/session-bound,
  and restart cancels rather than commits setup;
- node create/rotate with unavailable MTS and lost-response recovery by explicit
  rotation, with no plaintext token at rest;
- backup, maintenance, notification, MTS erasure, terminal and panic phase/
  ambiguity behavior using their owning state machines;
- redaction search over every credential, private key, command, output, channel
  secret, URL query, restore phrase, path and metric field;
- response-header tests for every authentication/session/secret route and proxy
  cache tests proving a protected response is never served publicly or to a
  different administrator;
- live terminal tests proving logout, revoke, expiry, password change, disable,
  demotion and recent-auth expiry close the exact session's connections without
  interrupting unrelated sessions;
- authenticated read floods, failed-auth floods, retention and cancellation;
- race/shuffle, at least 90 percent statement coverage, `go vet`,
  `goimports-reviser`, `golangci-lint`, vulnerability/module verification,
  frontend test/lint/build/audit, browser smoke, container and IPv4/IPv6 gates.

Acceptance requires that no sensitive route is reachable with a weaker role or
staler authentication than its registry policy, no protected/secret response is
cacheable, and no invalidated session retains a privileged connection beyond
its bound. No committed SQLite-only mutation lacks its success event, no audit
failure yields a false successful mutation, no committed mutation is reported
as unambiguously absent, every external action has truthful accepted/terminal
or unknown evidence, and deployed audit request IDs match HTTP request IDs.

## Delivery, rollback and approval

This is schema-free batch `administrative-mutation-consistency` in
[`commercial-parity-implementation-sequence.md`](./commercial-parity-implementation-sequence.md).
It consumes no SQLite migration, leaves the next migration at `v20`, and keeps
backup format `v4`. It should deploy before adding more administrator mutations.
Later `v5`, `v6`, `v12`, `v17` and `v19` implementations must use this
authorization/audit envelope and operation linkage rather than introducing
parallel vocabularies.

Rollback restores the prior binary/frontend and creates no schema conversion.
Audit events written with the expanded action vocabulary remain valid rows and
may appear as unknown labels in an old frontend; rollback does not delete them.

Implementation requires explicit approval because it changes transaction
boundaries, role/recent-auth requirements, TOTP enrollment, private response
headers, long-lived terminal invalidation, failure status semantics, route
metadata, audit actions/details, legacy-bootstrap attribution, sensitive-read
selection, terminal evidence and some response preparation order. This reviewed
design alone authorizes none of those behavior changes.
