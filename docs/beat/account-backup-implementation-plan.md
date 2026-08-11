# Account security and backup implementation plan

Updated: 2026-07-30

Status: phases 1-10 completed; production is deployed in bootstrap mode and is
waiting for the first owner setup

Source designs:

- [`account-security-audit-design.md`](./account-security-audit-design.md)
- [`backup-restore-design.md`](./backup-restore-design.md)

## Constraints discovered during planning

- `internal/api/router.go` is 285 lines and
  `internal/store/sqlite.go` is 291 lines. New routes and DDL cannot be
  added directly without violating the 300-line production-file limit.
- `frontend/src/lib/api.ts` and `frontend/src/types/index.ts` already exceed 300
  lines. New security and backup contracts must use focused modules.
- The embedded MTS public `RowIterator` exposes measurement, tags, timestamp,
  and typed fields, so logical export is feasible without internal APIs.
- The installed SQLite 3.51.2 and the modernc driver support `VACUUM INTO`.
- TOTP dependency candidate `github.com/pquerna/otp v1.5.0` is maintained,
  Apache-2.0, and not currently in `go.mod`.
- The live deployment must remain in token migration mode until an owner chooses
  credentials through the setup page; tests and an isolated acceptance server
  can fully verify account mode before that user action.

## Phase 1: behavior-preserving decomposition

Backend:

- Keep `Router` construction in `router.go`; move public, authentication,
  operational admin, security admin, and backup route registration into focused
  files under `internal/api`.
- Move table definitions from `sqlite.go` into focused schema statement files;
  keep initialization order and existing SQL unchanged.
- Split middleware into authentication, web security, request identity, logging,
  and recovery files.

Frontend:

- Keep the shared Axios instance and interceptors in `lib/api-client.ts`.
- Move resource calls into `lib/api/{nodes,alerts,network,settings,security,backup}.ts`.
- Move security and backup interfaces into `types/security.ts` and
  `types/backup.ts`; re-export from `types/index.ts` only while existing imports
  are migrated.

Verification checkpoint:

- Existing Go and frontend tests, public IPv6 responses, token authentication,
  Agent reporting, terminal WebSocket, and coverage remain unchanged.

## Phase 2: account, session, and audit persistence

Add model files:

- `internal/model/admin_user.go`
- `internal/model/admin_session.go`
- `internal/model/audit_event.go`

Add store files:

- `internal/store/admin_user.go`
- `internal/store/admin_session.go`
- `internal/store/audit_event.go`
- focused schema statements and indexes

Schema invariants:

- unique normalized username;
- role allowlist `owner|admin`;
- enabled and TOTP boolean checks;
- hash-only session token storage and unique hash;
- indexed active-session expiry and audit timestamp/action/actor;
- service transaction prevents disabling or deleting the last enabled owner.

Tests start with validation, uniqueness, not-found, closed-database, transaction
rollback, concurrent last-owner attempts, expiry, revocation, bounded pagination,
and secret-redaction cases.

## Phase 3: cryptography and authentication service

Add packages:

- `internal/adminauth`: Argon2id hashing, token generation/hashing, login,
  bootstrap, session validation, reauthentication, and bounded rate limiting.
- `internal/secretbox`: AES-256-GCM key lifecycle and TOTP secret encryption.
- `internal/audit`: allowlisted event builder and persistence facade.

This phase describes the deployed baseline. Its nil-AAD TOTP ciphertext and
single direct root key are intentionally superseded for `v2+` recoverable
secrets by
[`application-secret-lifecycle-design.md`](./application-secret-lifecycle-design.md);
later migrations must not copy the baseline format into another subsystem.

Security rules:

- random input always comes from `crypto/rand`;
- password comparisons use Argon2id plus constant-work unknown-user handling;
- session and bootstrap token comparisons use constant-time hash comparison;
- decrypted TOTP material is scoped to one operation and never logged;
- generated key directory/file permissions are `0700`/`0600`;
- rate-limiter maps are size-bounded and periodically pruned.

Tests cover deterministic random readers, malformed hashes/ciphertext, key-file
permissions, wrong key, nonce uniqueness, login enumeration resistance at the
behavior level, lockout expiry, cancellation, and concurrent session revocation.

## Phase 4: HTTP authentication and web security

Add handlers and middleware for bootstrap, login, logout, current session,
reauthentication, roles, session cookies, same-origin checks, and security
headers.

Routing behavior:

1. Public monitoring routes remain open.
2. With zero users, the existing token authorizes admin routes and bootstrap.
3. After the first owner exists, normal admin routes accept valid session
   cookies only.
4. Owner-only routes enforce role and recent reauthentication.
5. Terminal WebSocket uses the session cookie and same-origin validation.

HTTP tests assert exact cookie flags for HTTP and HTTPS, malformed/expired/
revoked sessions, CORS separation, CSRF-origin rejection, role denial, bootstrap
transition, WebSocket origin and session handling, body limits, generic errors,
and all security headers.

## Phase 5: audit integration

Create route metadata for action and resource classification. Record:

- authentication success, failure, lockout, logout, and reauthentication;
- account, password, TOTP, and session lifecycle;
- every existing administrator mutation;
- sensitive reads such as SSH key material, Agent installation config, terminal
  access, and backup download;
- backup and restore lifecycle.

Handler tests verify one redacted event per covered path. Tests explicitly search
stored detail JSON for passwords, OTP codes, bearer/session/Agent tokens, private
keys, SMTP credentials, webhook secrets, and command output.

## Phase 6: security frontend

Replace the token form with a state-driven setup/login flow:

- bootstrap owner setup;
- username/password login;
- conditional six-digit TOTP step;
- loading, lockout, expired-session, and server-error states.

Add `/admin/security` with shadcn tabs for Account, Two-factor authentication,
Sessions, Administrators, and Audit. Use readable labels in every Select and
icon actions with tooltips. Owner controls are omitted for admins but remain
server-protected.

Frontend tests cover setup-to-login transition, no Web Storage credentials,
cookie-session refresh, TOTP setup/disable, password change, session revoke,
role-gated controls, audit pagination/filter labels, and `401` recovery.

## Phase 7: backup creation

Add `internal/backup` with focused files for manifest, archive writing, SQLite
snapshot, MTS export, repository, service, and scheduler-safe locking.

Creation sequence:

1. Acquire the shared maintenance/backup lock.
2. Create a private temporary directory and destination file.
3. Generate the SQLite snapshot with `VACUUM INTO` and run integrity check.
4. Lock MTS writes, flush, capture cutoff, stream fixed measurements through
   `RowIterator` into gzip JSONL, then release the lock.
5. Write manifest and checksums, close and fsync every file, validate the newly
   built archive, and atomically rename it into the backup repository.
6. Persist result metadata and audit outcome.

Tests use mixed field types, tags, old/new timestamps, concurrent Agent writes,
cancellation, disk/write failures, duplicate backup starts, and archive
round-trips. No production test writes outside a test-owned temporary directory.

## Phase 8: restore validation and startup apply

Validation is a streaming, exact-allowlist pipeline. It rejects traversal,
absolute paths, symlinks, duplicate or unknown entries, checksum mismatch,
unsupported format/schema versions, oversized compression/expansion, invalid
SQLite integrity, invalid key length, and malformed MTS records.

Startup restore runs before `NewSQLiteStore` and `NewMTSStore`:

1. Read and revalidate the pending journal and archive.
2. Materialize a new SQLite file and new MTS directory beside target paths.
3. Run integrity, health, count, and sample-query checks.
4. Execute journaled renames with rollback after every possible failure point.
5. Open normal stores only after the swap succeeds.

Fault-injection tests cover every rename boundary and prove either the old pair
or restored pair is complete; a mixed SQLite/MTS pair is never accepted.

## Phase 9: backup frontend and operational documentation

Add an owner-only Backup and restore view with create progress, archive list,
download/delete actions, upload validation report, recent-reauth dialog, exact
confirmation phrase, pending-restart state, and rollback availability.

README documents:

- bootstrap and account migration;
- cookie/TLS expectations;
- backup sensitivity and permissions;
- staging, restart, success verification, rollback, and pending-restore removal;
- required external environment values not present in the archive.

## Phase 10: gates and deployment

Required backend gates:

- `goimports-reviser -rm-unused -format -company-prefixes github.com/beat/backend ./...`
- `golangci-lint run ./...`
- `go test -race ./...`
- statement coverage at least 90 percent
- `go vet ./...`
- `govulncheck ./...`
- targeted fuzz seeds for archive paths, manifest parsing, Argon2 encoding, and
  cookie/origin parsing

Required frontend gates:

- all Vitest tests;
- line coverage at least 90 percent;
- oxlint without new warnings;
- TypeScript and Vite production build;
- desktop/mobile inspection of setup, login, security, sessions, audit, backup,
  restore-validation, and confirmation states.

Deployment sequence:

1. Build and stage binary/static assets.
2. Preserve Agent process and back up live binary, static files, SQLite, MTS,
   environment file, and generated data key.
3. Deploy account migration mode and verify existing token access.
4. Exercise full account mode on an isolated acceptance database.
5. Verify production setup page, public no-login behavior, security headers,
   backup creation/download, corrupt-backup rejection, and IPv6 access.
6. Leave production in migration mode until the owner completes setup; after
   setup, verify token rejection and cookie-session HTTP/WebSocket access.

## Completion evidence

The competitive matrix can be marked Verified only when source, tests, built
assets, deployed API behavior, production audit records, a generated backup,
an isolated restore drill, rollback evidence, and IPv6 browser/API responses all
agree with both design acceptance sections.
