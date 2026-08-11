# Account security and audit

Updated: 2026-07-30

Status: authentication is deployed; audit/mutation consistency is partial and
requires the reviewed schema-free remediation

## Goal

Replace the browser-held, long-lived administrator token with local
administrator accounts, revocable server-side sessions, optional TOTP, recent
authentication for sensitive operations, and a persistent audit trail. Public
monitoring remains available without authentication.

Community capability evidence:

- Komari has local accounts, persistent sessions, OAuth providers, TOTP,
  sensitive-operation TOTP checks, and persistent audit logs.
- DStatus has password-based administration.
- Beat currently has one environment token stored in browser `sessionStorage`,
  no account or session lifecycle, no login rate limit, no MFA, and no audit
  records.

Beat will use stronger primitives than the observed community implementations;
no community authentication code will be copied.

## Authentication model

### Accounts

SQLite gains `admin_users` with:

- UUID, normalized unique username, display name, and `owner` or `admin` role.
- Argon2id password hash with per-password random salt and encoded parameters.
- Enabled state, password-changed time, last-login time, and timestamps.
- Encrypted TOTP secret and TOTP-enabled time.

Passwords must contain 12 to 128 Unicode characters. Usernames contain 3 to 64
ASCII letters, digits, dots, underscores, or hyphens. At least one enabled owner
must always remain.

### Bootstrap migration

When no administrator account exists, the admin route opens a one-time setup
flow. The existing `BEAT_ADMIN_TOKEN` proves bootstrap authority and creates the
first owner. During this migration-only state the token continues to authorize
existing administrator routes. Immediately after the first owner is created,
the token stops authorizing normal admin routes and can only bootstrap a fresh
database. This removes the long-lived browser token without locking out the
current deployment.

### Sessions

SQLite gains `admin_sessions`. Login creates 32 random bytes using
`crypto/rand`; only a SHA-256 token hash and a short display prefix are stored.
Each session records user, creation, last activity, absolute expiry, IP, user
agent, and revocation time.

The browser receives `beat_admin_session` with:

- `HttpOnly`, `SameSite=Strict`, `Path=/`, and no `Domain`.
- `Secure=true` when the request is HTTPS.
- Two-hour idle expiry and seven-day absolute expiry.

The frontend no longer stores administrator credentials in Web Storage.
Terminal WebSockets authenticate with the same cookie and strict same-origin
validation. Logout revokes the server-side session and expires the cookie.

### Login protection

- Constant-work password verification with an Argon2id dummy hash for unknown
  users.
- Per-IP and per-username failed-login limits: five failures in 15 minutes,
  then a 15-minute lockout. Limits are held in a bounded in-memory cache; audit
  events persist across restarts.
- Generic invalid-credential responses and bounded JSON request bodies.
- Password changes revoke every other session.

### TOTP and sensitive operations

TOTP uses `github.com/pquerna/otp/totp` from maintained Apache-2.0 release
`v1.5.0`. The deployed secret is AES-256-GCM encrypted by the random `0600`
`<data-dir>/admin-data.key`, but its legacy ciphertext has no AAD, version, key
ID, or rotation. Migration `v5` converts it to the shared user-bound envelope
and wrapped data-key lifecycle in
[`application-secret-lifecycle-design.md`](./application-secret-lifecycle-design.md).
Administrator backup archives include the root key and must be handled as
recoverable credentials.

Enabling TOTP requires verifying the first code. Disabling TOTP, resetting
another administrator, changing roles, deleting an administrator, exporting a
backup, or staging a restore requires a recent reauthentication. Reauth submits
the current password and TOTP when enabled, and marks the current session for
ten minutes; sensitive requests never carry passwords or OTP codes themselves.

## Authorization

- `owner`: account management, backup restore, security settings, and all
  existing administrator operations.
- `admin`: all existing operational management, own password/TOTP, and own
  session management; cannot manage accounts or restore backups.
- Bootstrap token: migration-only behavior described above.

Authorization is checked server-side for every route. The role and session
identity are placed in request context by middleware.

## Audit trail

SQLite gains append-only `admin_audit_events` with:

- event UUID, request ID, actor UUID and username snapshot;
- action, resource type and resource ID;
- success or failure outcome and an allowlisted JSON detail object;
- remote IP, user agent, session display prefix, and UTC timestamp.

Audit coverage includes login success/failure/lockout, logout, session revoke,
password and TOTP changes, account lifecycle, backup actions, terminal access,
agent-token lifecycle, SSH-key reads/writes, and every administrator mutation.
Secrets, passwords, OTP values, tokens, command output, and private keys are
never recorded. Audit records are retained for 180 days and cleaned by the
maintenance scheduler.

The deployed audit cleanup exists, but expired-session cleanup is not wired into
production and audit cleanup currently stops when automatic MTS maintenance is
disabled. Independent hygiene, cursor pagination, session bounds, and cleanup
supervision are reviewed in
[`application-data-lifecycle-design.md`](./application-data-lifecycle-design.md).

The deployed mutation integration also writes generic audit rows after handlers
have completed, ignores selected authentication audit failures, does not use the
HTTP request ID for explicit authentication events, and can return an error
after logout or reauthentication has already changed session state. The atomic
route policy, failure semantics, bootstrap attribution, redaction and external-
operation linkage are reviewed in
[`administrative-mutation-consistency-design.md`](./administrative-mutation-consistency-design.md).

The deployed access policy is also incomplete. Owner/recent-auth checks are
handler/service-local rather than mandatory route metadata; administrator
creation, Agent/SSH credential changes, terminal commands and notification
credential changes can proceed without recent authentication. TOTP begin
persists a replacement secret and clears the enabled timestamp before code
verification, and an admitted terminal WebSocket is not closed when its session
or user is invalidated. Authentication and protected JSON responses also lack a
central `Cache-Control: no-store` policy. The same schema-free design owns these
access, pending-TOTP, connection-invalidation and response-secrecy corrections.

## Web security changes

- Replace wildcard CORS on admin routes with same-origin behavior. Public API
  reads may retain wildcard CORS without credentials.
- Require a matching `Origin` for cookie-authenticated state-changing requests
  and WebSocket upgrades.
- Add CSP, `X-Content-Type-Options`, `X-Frame-Options`, `Referrer-Policy`, and
  `Permissions-Policy`; emit HSTS only on HTTPS.
- Mark authentication, session-protected, owner-only, and secret-bearing
  responses `Cache-Control: private, no-store` with legacy `Pragma: no-cache`
  and appropriate `Vary` values on both success and failure.
- Keep generic client errors and detailed server logs without credentials.

## API surface

Public authentication routes:

- `GET /api/v1/auth/state`
- `POST /api/v1/auth/bootstrap`
- `POST /api/v1/auth/login`
- `POST /api/v1/auth/logout`
- `GET /api/v1/auth/session`
- `POST /api/v1/auth/reauthenticate`

Authenticated security routes:

- `GET/POST /api/v1/admin/users`
- `PUT/DELETE /api/v1/admin/users/{id}`
- `PUT /api/v1/admin/users/me/password`
- `POST/DELETE /api/v1/admin/users/me/totp`
- `GET /api/v1/admin/sessions`
- `DELETE /api/v1/admin/sessions/{id}`
- `DELETE /api/v1/admin/sessions/others`
- `GET /api/v1/admin/audit-events` with bounded pagination and filters

## Frontend

- Replace token entry with setup, username/password login, and conditional TOTP
  steps using shadcn form controls.
- Add `/admin/security` with Account, Two-factor authentication, Sessions,
  Administrators, and Audit tabs. Owner-only controls are hidden for admins but
  remain server-protected.
- On `401`, refresh authentication state and return to login without exposing a
  stored credential.

## Threat model and controls

| Threat | Primary control |
| --- | --- |
| Credential stuffing | Argon2id, generic errors, per-IP and per-user lockout |
| Session theft through XSS | HttpOnly cookie, CSP, no browser credential storage |
| CSRF | SameSite Strict plus same-origin checks |
| Database-only TOTP disclosure or row substitution | `v5` versioned AES-GCM envelope, user-bound AAD, wrapped data key, separate `0600` root key |
| Session replay | Server-side hash lookup, idle/absolute expiry, revocation |
| Privilege escalation | Owner/admin route authorization and last-owner invariant |
| Repudiation | Persistent actor/session/request audit records |
| Secret leakage | Redacted APIs, allowlisted audit details, generic errors |

## Migration and rollback

1. Add new tables without changing public routes.
2. Deploy in bootstrap migration mode; existing admin token still works.
3. Create the first owner through the setup page.
4. Verify password, TOTP, session, WebSocket, and logout flows.
5. Account creation automatically disables token authorization for normal admin
   routes.

Rollback before step 5 restores the previous binary and database backup. After
step 5, rollback also requires restoring the pre-migration database or keeping
the existing token available in `server.env`.

## Acceptance

- Public dashboard and public read APIs remain login-free.
- Admin login uses username/password and optional TOTP; no credential is stored
  in LocalStorage or SessionStorage.
- Sessions can be listed and individually revoked; expired/revoked sessions
  fail immediately for HTTP and terminal WebSocket access.
- Login rate limits, last-owner rules, role checks, and recent reauthentication
  are covered by concurrency and error-path tests.
- Every security-sensitive and administrator mutation produces truthful,
  redacted evidence using the atomic or durable-operation contract in
  [`administrative-mutation-consistency-design.md`](./administrative-mutation-consistency-design.md).
- Security headers, cookie attributes, same-origin checks, full quality gates,
  deployment, and IPv6 behavior are verified.

## Approval boundary

Implementation requires explicit approval for the new SQLite tables, the
authentication/API behavior change, the `pquerna/otp` dependency, the generated
`admin-data.key`, role semantics, and disabling normal `BEAT_ADMIN_TOKEN`
authorization after owner bootstrap.
