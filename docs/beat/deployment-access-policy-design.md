# Deployment access policy

Updated: 2026-07-30

Status: reviewed design; implementation requires explicit approval

## Goal and competitor evidence

Current Komari exposes a canonical Agent connection address, configurable REST
and WebSocket origin allowlists, partial public IP display, and optional public
visitor event recording. Beat already has trusted-proxy handling, wildcard CORS
for public REST reads, strict same-origin browser administration, and a binary
public IP switch, but these deployment and privacy choices are not one governed
contract.

Evidence:

- <https://github.com/komari-monitor/komari/blob/4077201f098774511eaf504f220c5f6be009346b/internal/config/settings.go>
- <https://github.com/komari-monitor/komari/blob/4077201f098774511eaf504f220c5f6be009346b/web/security/cors.go>
- <https://github.com/komari-monitor/komari/blob/4077201f098774511eaf504f220c5f6be009346b/web/security/origin.go>
- <https://github.com/komari-monitor/komari/blob/4077201f098774511eaf504f220c5f6be009346b/web/rpc/jsonrpc/public.audit.go>

Beat will add the useful deployment controls while preserving the user's
requirements that public monitoring remains login-free and administration
requires authentication.

## Security invariants

1. Browser administration, setup, OIDC callbacks, restore, terminal, theme
   management, and every cookie-authenticated route remain same-origin. No
   setting can enable credentialed wildcard CORS.
2. Service tokens remain intended for CLI/server clients. An allowed browser
   origin never grants scope, bypasses token checks, or exposes a token to the
   frontend.
3. Public REST may use `*` or an explicit HTTPS origin allowlist. Public
   WebSockets use exact normalized origins; an absent Origin is accepted only
   for documented non-browser clients on routes that require no cookie.
   Connection and handshake limits use the same canonical trusted-proxy client
   identity defined by the runtime-resilience batch.
4. A canonical external URL is trusted only when explicitly configured. Host,
   `Forwarded`, and `X-Forwarded-*` input never rewrite it. The value contains
   scheme and authority only, with no userinfo, query, fragment, or path prefix
   in version one.
5. Public node address exposure is `hidden`, `masked`, or `full`. `hidden` is the
   default. IPv4 masking exposes only the first octet; IPv6 exposes only the
   first 32 bits. Invalid addresses become hidden, never pass through raw.
6. Unauthenticated clients cannot write arbitrary strings into the security
   audit log. Public interaction telemetry uses a fixed event vocabulary and
   numeric counters in MTS only; SQLite stores policy, not event samples.
7. Origin, URL, and privacy policy changes are owner-only, recently
   authenticated, audited, validated before commit, and applied atomically.
8. Theme packages, PWA, wallboard, notification templates, and Agent install
   output consume the same resolved public URL and public-data policy instead of
   inventing route-specific fallbacks.

## Configuration model

Deployment-owned environment settings:

- `BEAT_PUBLIC_BASE_URL`: optional canonical external HTTPS origin. HTTP is
  accepted only for loopback development.
- `BEAT_PUBLIC_API_ORIGINS`: comma-separated exact origins or the single value
  `*`; default `*` preserves current login-free public REST integration.
- `BEAT_PUBLIC_WS_ORIGINS`: comma-separated exact origins; default is the
  effective same origin plus `BEAT_PUBLIC_BASE_URL` when configured.

Startup rejects malformed, duplicate-after-normalization, wildcard mixed with
exact, userinfo, opaque, non-HTTP(S), path-bearing, control-character, and
oversized values. Readiness exposes only whether configuration is valid and
which policy mode is active, never the origin list.

SQLite migration `v15` changes the current site address boolean into
`public_address_mode` with `hidden`, `masked`, and `full`, preserving the old
value exactly (`false` to `hidden`, `true` to `full`). It also stores bounded
public interaction telemetry policy:

- enabled flag, default false;
- fixed allowed event set such as `page_view`, `node_open`, `range_change`, and
  `wallboard_open`;
- per-session/client sampling ceiling and aggregate retention reference;
- updated actor and timestamp.

No origin list or external URL secret is stored in SQLite. They remain
deployment configuration and are excluded from browser management.

## Public telemetry boundary

The frontend may send only a fixed enum plus coarse page kind. It never sends
free-form detail, URL/query, referrer, user agent, IP, node name/ID, search text,
filter value, screen contents, account state, or a stable visitor identifier.
The Server validates origin, content type, exact body size, enum, and a bounded
anonymous rate key, then increments catalog metrics such as:

- `public_interaction_total` tagged by fixed `event` and `surface`;
- `public_interaction_rejected_total` tagged by fixed `reason`.

These numeric series exist only in MTS after metric catalog migration `v8`.
Public telemetry failure never blocks page use and is never retried offline.
The service worker never queues telemetry. The security audit remains reserved
for trusted Server/account/operation events.

## API and response behavior

`GET /api/v1/settings/site` returns `public_address_mode` and a display-safe
boolean indicating whether anonymous telemetry is enabled. Public node REST and
WebSocket serializers call one shared address formatter. Admin node responses
retain the full address regardless of public mode.

`POST /api/v1/public/interactions` accepts one bounded event only when enabled.
It returns `204` for accepted events and a uniform bounded error for rejected
input. It never returns identifiers or current counters.

Public CORS applies only to the documented public read routes and optional
interaction endpoint. Preflight for cookie/admin/Agent routes remains rejected.
WebSocket origin policy is route-specific: metrics/fleet may use the public
allowlist, while terminal and future authenticated streams remain same-origin.
Origin authorization composes with, and never replaces, global/per-client
connection limits, slow-client eviction, and shutdown ownership in
[`runtime-resilience-design.md`](./runtime-resilience-design.md).

## Administration and deployment experience

`/admin/settings` adds a compact `Public access` section using existing shadcn
components:

- a three-option `ToggleGroup` for hidden/masked/full address display;
- a `Switch` for anonymous aggregate interaction telemetry;
- an `Alert` explaining that raw visitor identity and activity are not logged;
- read-only deployment status rows for external URL, public REST origin mode,
  and public WebSocket origin mode, with values summarized as `same origin`,
  `all public origins`, or `N configured origins` rather than listing them.

Environment-managed values link to the runbook and cannot be edited in the
browser. Forms use `FieldGroup`/`Field`, status uses `Badge` plus text, and
changes use the existing recent-auth dialog. No IDs or raw origin arrays appear
in selects or confirmation text.

Agent install dialogs and notification previews show the resolved external host
and fail closed when no trustworthy externally reachable URL can be derived.
They never default an HTTPS deployment to an untrusted forwarded host.

## Tests and acceptance

Tests cover URL/origin parsing, Unicode/control input, default ports, IPv4/IPv6
authorities, loopback HTTP, trusted/untrusted proxy headers, wildcard rules,
preflight, missing/malformed Origin, route separation, cookie denial, and public
WebSocket reconnects.

Address tests cover every IPv4/IPv6 mask, invalid/zone-scoped addresses,
hidden/full migration equivalence, REST/WebSocket consistency, public cache
variation, and admin full-address retention. Telemetry tests cover enum/body
limits, spoofed detail rejection, rate/sampling bounds, disabled behavior,
MTS failure visibility, no SQLite sample rows, no service-worker queue, and
bounded metric tags.

Frontend tests cover human-readable modes, no ID/raw-origin rendering, recent
auth, background refresh, responsive text/overflow, keyboard use, and all
loading/error states. Completion requires 90 percent backend statement and
frontend line coverage, race/shuffle, `go vet`, `goimports-reviser`,
`golangci-lint`, vulnerability/module verification, build, browser smoke,
reverse-proxy tests, and deployed IPv4/IPv6 public/admin isolation.

## Backup, rollback, version, and approval

Migration `v15` is `edge-access-policy` in
[`commercial-parity-implementation-sequence.md`](./commercial-parity-implementation-sequence.md).
It depends on metric catalog `v8`, OIDC `v2`, theme packages `v3`, and the public
Fleet batch. It retains backup archive format `v4`: SQLite snapshots contain
policy and catalog state, while logical MTS export contains registered aggregate
telemetry measurements.

Rollback restores the pre-migration database and matching binary. New MTS
aggregate series are append-only and may remain unreachable to the older
binary; deletion requires a separate approved maintenance action.

Implementation requires explicit approval because it changes public response
shape, CORS/WebSocket origin behavior, deployment configuration, site settings,
anonymous telemetry, MTS catalog entries, and SQLite migration state. Approval
must confirm that public monitoring remains no-login, administration stays
same-origin/session-protected, visitor data is aggregate-only, and `hidden`
remains the default address mode.
