# OIDC authentication and account linking

Updated: 2026-07-30

## Goal and scope

Beat will add standards-based OpenID Connect login without weakening the
existing local administrator security model. The public dashboard and public
read APIs remain available without authentication. Only `/admin` and
administrator operations require authentication.

This batch implements OIDC Authorization Code flow with PKCE. It does not add
generic OAuth userinfo login, social-provider-specific protocols, automatic
account creation, passwordless owner accounts, SAML, LDAP, or an arbitrary
OAuth endpoint editor. A provider must expose valid OIDC discovery metadata and
signed ID tokens.

The existing Argon2id password, optional TOTP, server-side session, recent
reauthentication, trusted-proxy, same-origin, audit, and `secretbox` mechanisms
remain authoritative. OIDC creates the same `admin_sessions` used by local
login; it does not introduce a second session implementation.

## Security invariants

1. OIDC login succeeds only for an enabled provider and an existing explicit
   mapping identified by `(provider_id, subject)`.
2. Beat never links or merges an account by email, username, display name, or
   another mutable claim. `email_verified` may be displayed but is never an
   account key.
3. Provider management is owner-only and requires recent local
   password/TOTP reauthentication.
4. Each administrator may link or unlink only their own external identities,
   also after recent local reauthentication.
5. Every owner retains a working local password. This batch does not permit
   deleting local credentials, so OIDC cannot remove the emergency login path.
6. A future credential-removal feature must reject removal of the last usable
   credential. OIDC does not relax the existing last-enabled-owner invariant.
7. Authorization state is single-use, expires after five minutes, is bound to
   the initiating browser, and is invalid after provider changes.
8. Provider secrets, PKCE verifiers, and any other recoverable temporary secret
   use envelope format 1 and resource-bound AAD from
   [`application-secret-lifecycle-design.md`](./application-secret-lifecycle-design.md).
   This migration extends the existing AES-GCM `secretbox` without creating an
   OIDC-only cipher; `v5` later converts retained envelopes to wrapped data keys.
   Raw state, browser-binding tokens, and nonces are never stored.
9. Discovery, JWKS, and token calls use bounded, SSRF-resistant HTTP clients;
   the application never uses `http.DefaultClient` for provider traffic.
10. Client-facing callback errors are generic. Detailed upstream errors are
    logged with a request ID and without codes, tokens, client secrets, ID
    tokens, or claim payloads.

## Protocol flow

### Provider discovery

Providers are configured by issuer URL, client ID, client secret, display name,
and enabled state. Beat obtains authorization, token, JWKS, supported signing
algorithms, and other protocol endpoints from OIDC discovery. It does not let
an administrator independently mix endpoints from different issuers.

The issuer must:

- use HTTPS, with an exception only for injected loopback test servers;
- contain no user information, fragment, or query string;
- match the ID token `iss` claim exactly after discovery;
- resolve only to publicly routable addresses in production;
- remain immutable after creation. A different issuer is a new provider.

Discovery metadata and keys are cached for a bounded period and refreshed on
key rotation. Provider create/update validates discovery before committing the
configuration. Disabling a provider immediately blocks new login and linking
flows without deleting existing identity mappings.

### Begin login or linking

Beat generates independent cryptographically random values for:

- OAuth `state`;
- OIDC `nonce`;
- PKCE `code_verifier` and its S256 `code_challenge`;
- a browser-binding cookie.

Only SHA-256 hashes of state, nonce, and browser binding are stored. The PKCE
verifier is stored encrypted because it must be recovered for token exchange.
The browser-binding cookie is `HttpOnly`, `Secure` when the effective scheme is
HTTPS, `SameSite=Lax`, scoped to the OIDC callback path, and expires with the
five-minute transaction. `Lax` is required for the top-level redirect from the
identity provider; the normal administrator session cookie remains
`SameSite=Strict`.

The redirect URI is constructed from Beat's trusted effective scheme and host
plus the fixed OIDC callback path. The exact redirect URI is stored in the
transaction and reused during token exchange. Only fixed internal return paths
such as `/admin` and `/admin/security` are accepted; arbitrary return URLs are
rejected to prevent open redirects.

Linking requires an authenticated administrator session and recent local
reauthentication before the transaction is created. The transaction records
the initiating user ID and purpose, so the callback does not depend on the
Strict administrator cookie being sent during the cross-site redirect.

### Callback and token validation

The callback requires both state and the browser-binding cookie. Beat atomically
marks the matching, unexpired transaction consumed before exchanging the code;
a second callback loses the race and fails. A failed upstream exchange requires
starting a new flow instead of replaying the callback.

Token exchange uses the stored redirect URI and PKCE verifier. A successful
response must include an ID token. Validation requires:

- a signature from the discovered JWKS and no `none` algorithm;
- exact issuer match;
- audience containing the configured client ID and valid authorized-party
  semantics when multiple audiences are present;
- unexpired token time claims with a small bounded clock skew;
- constant-time comparison of the returned nonce hash;
- a non-empty, bounded `sub` claim.

Beat uses only the verified ID token claims required for identity and display.
It does not call a generic userinfo endpoint as an authentication substitute.
Access and refresh tokens are not persisted.

For login, the verified `(provider_id, sub)` must already map to an enabled
administrator. Beat creates a normal server-side session and records the login
method. An unknown identity receives a generic not-linked result and cannot
bootstrap or create an account.

For linking, the transaction user must still exist and be enabled. Beat rejects
a subject already linked to another user and rejects a second identity for the
same provider/user pair. The verified identity is committed before reporting
success.

## Outbound HTTP and SSRF controls

OIDC endpoints are privileged outbound destinations. The provider client will:

- validate discovery, authorization, token, and JWKS URLs as HTTPS URLs;
- resolve and validate every dial target, not only the initially entered host;
- reject loopback, private, link-local, multicast, unspecified, and other
  non-global addresses in production, including after DNS rebinding;
- reject URL user information and non-HTTP(S) schemes;
- disable redirects by default, or allow at most three HTTPS redirects whose
  destinations pass the same validation;
- use explicit connect, TLS handshake, response-header, and overall request
  timeouts;
- cap each provider response body at 1 MiB and reject trailing oversized data;
- close all response bodies and propagate cancellation from request contexts.

The first batch intentionally does not support private-network identity
providers. Supporting an internal IdP later requires a separate, explicit
destination allowlist rather than weakening the global-address rule.

## Persistence model

Canonical SQLite migration `v2` adds the following SQLite-owned platform data,
as assigned by
[`commercial-parity-implementation-sequence.md`](./commercial-parity-implementation-sequence.md).
Metric samples remain exclusively in MTS.

### `admin_oidc_providers`

- `id`: UUID primary key.
- `slug`: stable, unique URL-safe identifier.
- `display_name`: administrator-facing and login-button label.
- `issuer_url`: immutable canonical issuer URL.
- `client_id`: OIDC client identifier.
- `client_secret_encrypted`: strict envelope-format-1 AES-GCM ciphertext bound
  to provider ID and configuration version; never returned by APIs.
- `scopes_json`: validated list containing `openid`; defaults to
  `openid profile email`.
- `enabled`: whether login and linking may begin.
- `configuration_version`: incremented on credential or client changes and
  copied into transactions so outstanding flows become invalid.
- `sort_order`, `created_at`, and `updated_at`.

Indexes enforce unique slug and issuer. API responses expose only
`client_secret_configured`, never ciphertext or plaintext.

### `admin_external_identities`

- `id`: UUID primary key.
- `provider_id`: foreign key to `admin_oidc_providers` with restricted delete.
- `user_id`: foreign key to `admin_users` with cascade delete.
- `subject`: exact verified OIDC `sub` value.
- minimal display metadata: `email`, `email_verified`, `preferred_username`,
  and `display_name`;
- `last_login_at`, `created_at`, and `updated_at`.

Unique constraints cover `(provider_id, subject)` and
`(provider_id, user_id)`. Provider deletion fails while identities are linked;
normal operation uses disable instead of destructive deletion.

### `admin_oidc_transactions`

- `id`: UUID primary key.
- `state_hash`: unique SHA-256 state hash.
- `browser_binding_hash` and `nonce_hash`.
- `code_verifier_encrypted`.
- `provider_id`, `provider_configuration_version`, and `purpose` (`login` or
  `link`).
- `user_id` for link transactions only.
- fixed `redirect_uri` and `return_path`.
- `created_at`, `expires_at`, and `consumed_at`.

The store exposes an atomic consume operation using Beat's immediate-write
transaction pattern. Expired and consumed rows are removed by the existing
maintenance scheduler. The table contains no metric data.

### Session attribution

Migration version 2 also adds `authentication_method` (`local` or `oidc`) and
nullable `external_identity_id` to `admin_sessions`. Existing rows default to
`local`. Session API responses can therefore explain how a session was created
without exposing provider secrets or tokens.

## API surface

Public authentication routes:

- `GET /api/v1/auth/oidc/providers`: enabled provider ID, slug, and display
  name only.
- `POST /api/v1/auth/oidc/providers/{id}/begin`: create a login transaction,
  set the browser-binding cookie, and return the authorization URL.
- `GET /api/v1/auth/oidc/callback`: consume the transaction, validate the ID
  token, create/link as directed, and redirect to a fixed administrator path.

Authenticated identity routes:

- `GET /api/v1/admin/oidc/identities/me`: list the current user's linked
  identities with provider names.
- `POST /api/v1/admin/oidc/providers/{id}/link`: begin a recently
  reauthenticated link transaction.
- `DELETE /api/v1/admin/oidc/identities/{id}`: unlink the current user's
  identity after recent reauthentication and credential-invariant checks.

Owner-only, recently reauthenticated provider routes:

- `GET /api/v1/admin/oidc/providers`.
- `POST /api/v1/admin/oidc/providers`.
- `PUT /api/v1/admin/oidc/providers/{id}`.
- `DELETE /api/v1/admin/oidc/providers/{id}`, allowed only when no identities
  reference the provider.

Provider create/update performs bounded discovery validation. Errors exposed to
the browser identify invalid configuration categories without returning raw
upstream responses. Existing public monitoring APIs and administrator route
authorization do not change.

## Audit events

The following actions produce redacted audit records:

- `auth.oidc.login` success/failure;
- `auth.oidc.link` success/failure;
- `auth.oidc.unlink` success/failure;
- `auth.oidc.provider.create`, `.update`, `.enable`, `.disable`, and `.delete`.

Audit detail may contain provider ID, provider display name, external identity
ID, and failure category. It must not contain subject, email, authorization
code, state, nonce, verifier, token, client secret, discovery body, or raw
provider error text.

## Frontend behavior

The existing compact shadcn login card remains the entry point. When setup is
complete and enabled providers exist, it shows a quiet separator below the
local password form followed by one full-width provider button per provider.
The button uses a Lucide login/key icon and the configured display name. The
first-owner setup view never offers OIDC.

The `/admin/security` page adds:

- an `External identities` tab for every administrator, showing provider name,
  verified display metadata, last login, link, and unlink actions;
- an owner-only `Identity providers` tab with a compact provider list and a
  create/edit dialog for name, issuer, client ID, client secret, scopes, order,
  and enabled state.

Provider status is expressed by text and status icon, not color alone. Secret
inputs never rehydrate stored values. Update forms use an explicit “replace
secret” control. Destructive unlink/delete actions use the existing confirmation
and recent-reauthentication patterns. Mobile layouts stack fields and actions;
long issuers and account values wrap or truncate with a tooltip instead of
overflowing.

The visual language remains the existing neutral Geist/shadcn administrator
surface: restrained borders, compact spacing, maximum 8 px card radius, visible
keyboard focus, and no decorative or marketing layout. All new text is added to
the existing locale dictionaries.

## Test strategy and quality gates

This is a high-risk authentication and persistence change, so implementation
uses behavior-first regression tests.

Backend tests cover:

- issuer, endpoint, redirect, scope, claim, and response-size validation;
- DNS/IP SSRF rejection, redirects, timeouts, cancellation, and body closing;
- state, nonce, browser binding, PKCE S256, expiry, and single consumption;
- concurrent duplicate callbacks where exactly one succeeds;
- discovery/JWKS rotation and invalid signature, issuer, audience, nonce, and
  subject cases using injected local TLS test providers;
- explicit linking, identity collision, disabled user/provider, immutable
  issuer, provider deletion conflict, and local-owner fallback invariants;
- envelope/AAD swap rejection for provider secrets and PKCE verifiers, plus
  redacted responses, logs, and audit details;
- version 1 to version 2 migration, restart idempotence, future-version
  rejection, backup/restore validation, and transaction cleanup;
- cookie attributes, same-origin begin requests, fixed callback redirects,
  generic callback failures, local login regression, and public/admin route
  boundaries.

Frontend tests cover:

- local login and setup behavior remaining unchanged;
- provider names rather than IDs displayed on every selector/list/button;
- provider begin navigation and callback success/failure states;
- provider create/edit/enable/disable/delete flows and secret redaction;
- link/unlink, recent-reauthentication prompts, owner-only controls, overflow,
  keyboard, and mobile behavior.

Completion requires at least 90% Go statement coverage and 90% frontend line
coverage, plus the existing race, shuffle, `go vet`, `goimports-reviser`,
`golangci-lint`, `govulncheck`, module verification, frontend test, audit,
lint, and production-build gates.

## Dependencies

The proposed implementation pins the current stable releases available during
design:

- `github.com/coreos/go-oidc/v3 v3.20.0` for discovery and ID-token
  verification;
- `golang.org/x/oauth2 v0.36.0` for Authorization Code and PKCE exchange.

Beat wraps their network access with its own bounded SSRF-safe client and keeps
state, nonce, account-linking, authorization, storage, and audit policy in Beat.
Dependency addition requires `go mod tidy`, `go mod verify`, license review,
`govulncheck`, and container/SBOM regeneration.

## Migration, deployment, and rollback

1. Stop only Beat Server for the production cutover; keep the Agent running.
2. Back up the server binary, static assets, SQLite database including WAL/SHM,
   MTS data, environment files, and `admin-data.key`; verify SQLite integrity,
   backup checksums, and full provider/transaction ciphertext decryption with
   reconstructed AAD before accepting the archive.
3. Deploy the migration version 2 Server and matching frontend, then configure
   a provider while authenticated as owner.
4. Verify discovery, local login, OIDC linking, OIDC login, logout, session
   attribution, public access, unauthenticated admin rejection, readiness, and
   both published IPv6 addresses.
5. Confirm Agent reports continue and no metric data is written to SQLite.

The old Server rejects a version 2 database as newer than supported. Rollback
therefore restores the pre-migration SQLite backup together with the old binary
and static assets. Disabling every provider is the immediate non-destructive
feature rollback while retaining the new Server.

## Approval boundary

Implementation requires explicit approval for all of the following as one
coherent batch:

1. SQLite schema migration version 2, including three OIDC tables and session
   attribution columns.
2. The new `go-oidc/v3` and `x/oauth2` dependencies.
3. The new public OIDC provider/begin/callback API and administrator identity
   and provider-management APIs.
4. The login-page behavior change and the new Security-page identity/provider
   tabs.
5. Owner-only provider administration, per-user explicit linking/unlinking,
   no email auto-linking, public-network issuer restriction, and permanent
   local owner password fallback.

No implementation, dependency, schema, production configuration, or deployment
change is authorized by this design document alone.
