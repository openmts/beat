# Commercial notification delivery

Status: Reviewed design; implementation requires explicit approval

## Goal

Beat must match the useful notification-integration surface of current Komari
and DStatus while improving the security and reliability boundary expected of a
commercial monitoring service.

The proposed implementation, once explicitly approved, adds:

- Bark, ServerChan Turbo, and ServerChan 3 senders;
- event-category routing and safe per-channel message presentation;
- a declarative custom HTTP sender with the integration value of Komari's
  custom Webhook and JavaScript senders, without executing administrator code;
- encrypted, revisioned channel credentials;
- a durable SQLite delivery queue with bounded retries, leases, delivery
  history, and dead-letter handling;
- outbound request policy, DNS-rebinding resistance, response bounds, audit,
  metrics, and an operator-focused shadcn/ui management surface.

Agent metrics remain in MTS. Channel definitions, delivery metadata, routing,
encrypted notification payloads, and application audit records are platform
application data and therefore belong in SQLite.

## Current evidence

Beat currently supports Webhook, Telegram Bot API, and SMTP email. It validates
typed configurations, redacts Telegram and SMTP secrets from API responses,
supports authenticated test sends, and records delivery counters. The current
implementation still has four commercial gaps:

1. credentials remain inside the plaintext SQLite `alert_channels.config`
   value even though management responses are redacted;
2. Webhook destinations are checked only for an HTTP(S) scheme and host, so
   private-address access, DNS rebinding, redirects, and metadata endpoints are
   not controlled;
3. alert delivery is synchronous and failed sends are logged but not durably
   retried;
4. the latest channel result is process-local and disappears after restart.

The current files establishing that boundary are:

- `internal/notification/config.go`;
- `internal/notification/service.go`;
- `internal/alerter/notification.go`;
- `internal/store/alert_channel.go`;
- `frontend/src/pages/admin/alert-channel-dialog.tsx`.

## Competitor audit

The public upstream snapshots were inspected on 2026-08-01. Komari Server's
current `main` runtime is `91cbeb3f829e8044f37083d1105d9c66e0cd7c10`;
the provider behavior links below remain pinned to the prior inspected commit
so the original provider comparison remains reproducible.

Komari registers Bark, email, JavaScript, ServerChan 3, ServerChan Turbo,
Telegram, and Webhook providers. It also has a global event message template.
Its custom Webhook supports GET or POST, custom headers, Basic authentication,
content type, and `{{message}}` / `{{title}}` replacement. Its JavaScript
sender loads administrator code into goja with `require`, `fetch`, XHR,
timers, and Promise support. The current runtime refactor adds explicit
module/API compatibility documentation, rejects unsupported methods instead of
silently treating them as no-ops, and bounds HTTP bodies, child-process output,
file roots, execution, and listen permissions. Those controls improve the
competitor's implementation but still do not turn it into Beat's reviewed
no-administrator-code custom HTTP boundary.

That breadth is useful, but the inspected implementation also permits arbitrary
outbound destinations without an SSRF policy, reads some response bodies
without a bound, retries synchronously three times, and has no durable delivery
lease or dead-letter state. A JavaScript timeout returns to the caller but does
not provide a process-isolation boundary for the script or its network work.
Beat treats these as competitor risks, not compatibility requirements.

DStatus primarily routes Telegram notifications. It exposes event-category
switches, test notification, multiple chat IDs, simple deduplication, and
monthly JSON-lines notification logs. The inspected code logs message content
and result details, and its bot diagnostics log token fragments. Beat preserves
the category-control and operator-history value without logging credentials,
full rendered payloads, or recipient secrets.

Evidence:

- Komari provider registration:
  <https://github.com/komari-monitor/komari/blob/4077201f098774511eaf504f220c5f6be009346b/utils/messageSender/all.go>
- Komari Bark sender:
  <https://github.com/komari-monitor/komari/blob/4077201f098774511eaf504f220c5f6be009346b/utils/messageSender/bark/bark.go>
- Komari ServerChan 3 sender:
  <https://github.com/komari-monitor/komari/blob/4077201f098774511eaf504f220c5f6be009346b/utils/messageSender/serverchan3/serverchan3.go>
- Komari ServerChan Turbo sender:
  <https://github.com/komari-monitor/komari/blob/4077201f098774511eaf504f220c5f6be009346b/utils/messageSender/serverchanturbo/serverchanturbo.go>
- Komari custom Webhook sender:
  <https://github.com/komari-monitor/komari/blob/4077201f098774511eaf504f220c5f6be009346b/utils/messageSender/webhook/webhook.go>
- Komari JavaScript sender and injected HTTP APIs:
  <https://github.com/komari-monitor/komari/blob/4077201f098774511eaf504f220c5f6be009346b/utils/messageSender/javascript/javascript.go>
  and
  <https://github.com/komari-monitor/komari/blob/4077201f098774511eaf504f220c5f6be009346b/utils/messageSender/javascript/apis.go>
- Komari current JavaScript runtime boundary:
  <https://github.com/komari-monitor/komari/blob/91cbeb3f829e8044f37083d1105d9c66e0cd7c10/pkg/jsruntime/README.md>
- Komari retry and global event template:
  <https://github.com/komari-monitor/komari/blob/4077201f098774511eaf504f220c5f6be009346b/utils/messageSender/sender.go>
- DStatus notification manager:
  <https://github.com/fev125/dstatus/blob/4afc9e43c9df28096352c05ae924fcadbc830a2f/modules/notification/index.js>
- DStatus Telegram settings:
  <https://github.com/fev125/dstatus/blob/4afc9e43c9df28096352c05ae924fcadbc830a2f/database/setting.js>
- Bark v2 API:
  <https://github.com/Finb/bark-server/blob/3df8990fcbc407a3f5638eea8cedc3289d1a405d/docs/API_V2.md>
- ServerChan Turbo and ServerChan 3 product endpoints:
  <https://sct.ftqq.com/sendkey/> and <https://sc3.ft07.com/>

## Product boundary

### Supported channel types

The first commercial notification revision supports these channel types:

| Type | Transport contract | Secret values |
| --- | --- | --- |
| `webhook` | Existing Beat JSON event or message envelope over POST | Full endpoint and optional auth values |
| `telegram` | Telegram `sendMessage` JSON API | Bot token |
| `email` | STARTTLS, implicit TLS, or loopback-only plaintext SMTP | Password |
| `bark` | JSON POST to `/push` with title, body, level, group, icon, sound, and click URL | Device key; non-default endpoint credentials |
| `serverchan_turbo` | JSON POST with `title`, `desp`, and bounded optional channel fields | SendKey and OpenID values |
| `serverchan_3` | JSON POST with `title`, `desp`, and optional tags | Full API endpoint because its path contains the send key |
| `custom_http` | One policy-checked GET or POST request built from a declarative template | Named header, query, body, Basic, Bearer, or HMAC secrets |

Komari exposes one active provider. Beat intentionally retains its stronger
multi-channel model: an event can fan out to every matching enabled channel,
and scheduled reports can continue selecting explicit channel sets.

### Event categories

Each channel owns an explicit subscription set. The initial vocabulary is:

- `alert.triggered` and `alert.resolved`;
- `availability.offline` and `availability.recovered`;
- `fleet.status_summary`;
- `traffic.quota` and `traffic.report`;
- `enrollment.pending` and `enrollment.approved`;
- `operation.completed` and `operation.failed`;
- `node.expiring`, `node.expired`, and `node.renewed`;
- `security.login_succeeded`, `security.login_suspicious`, and
  `security.service_token_changed`;
- `notification.delivery_failed`.

Categories whose producer is not deployed yet remain selectable only after the
owning feature migration is installed. Existing enabled channels migrate with
all currently deployed event categories selected, preserving current routing.
The schedule, scope, fixed payload, catch-up, and restart semantics for
`fleet.status_summary` are owned by
[`fleet-status-summary-design.md`](./fleet-status-summary-design.md) and become
available only after migration `v18`.

`notification.delivery_failed` never routes back to the failed channel. It is
deduplicated per failed channel and error class for 15 minutes. When every
notification path is unhealthy, logs and metrics remain the authoritative
signal and no recursive notification loop is created.

`security.login_succeeded` is created in the same transaction as the session
and last-login update, after those mutations have validated and before commit.
It contains the administrator display name, authentication method, event time,
canonical source address in a privacy-controlled form, and coarse trusted
GeoIP result when available. It never contains a password, TOTP value, session
ID/token, OIDC token/claims, authorization header, or raw user-agent string. A
normalized device-family summary may be included only after strict length and
control-character validation.

Ordinary failed logins remain audit-only to avoid creating an attacker-driven
notification flood. `security.login_suspicious` is emitted after a persisted,
bounded threshold such as a rate-limit lockout or a material source/device
change; its deterministic key deduplicates actor/source/reason windows. The
event does not confirm whether a username exists. `security.service_token_changed`
is available only after the scoped-automation feature migration and covers
create, rotate, revoke, and imminent expiry without credential material.

The complete login-event producer, source/device privacy, threshold,
transaction, API, UI, retention, and verification contract is maintained in
[`login-security-notification-design.md`](./login-security-notification-design.md).

## Safe templates

Beat does not execute JavaScript, Go templates, shell commands, WASM, or dynamic
plugins in the Server process. Functional equivalence is provided through two
declarative layers.

### Message presentation

A channel may use the built-in localized subject and text, or owner-approved
subject/text templates. Templates allow only scalar substitutions from a fixed
schema:

- `event.kind`, `event.status`, `event.severity`, `event.message`, and
  `event.occurred_at`;
- `node.id`, `node.name`, `node.group_name`, and public node labels;
- `rule.id`, `rule.name`, `metric.name`, `metric.value`, and
  `metric.formatted_value`;
- `site.title` and an optional configured public base URL;
- `security.authentication_method`, `security.source_summary`,
  `security.location_summary`, and `security.device_summary`;
- `delivery.id` and `delivery.attempt`.

There are no loops, includes, reflection, filesystem access, environment
access, network calls, or user-defined functions. Unknown variables, malformed
expressions, control characters in headers, or output beyond the provider
limit fail validation before saving. Missing optional values render as an empty
string; required-field validation then decides whether the result is usable.

Default limits are 200 UTF-8 bytes for a subject, 16 KiB for text, and 64 KiB
for the complete rendered provider payload. Provider-specific lower limits win.

### Custom HTTP request

The custom sender describes one request:

- a static HTTPS endpoint with no template variables in scheme, authority, or
  port;
- GET or POST;
- at most 32 headers and 32 query fields;
- JSON, form, or UTF-8 text body;
- named secret references in approved header, query, or body leaf values;
- optional Basic or Bearer authentication;
- optional HMAC-SHA-256 signing over a fixed composition of timestamp, method,
  path, and rendered body.

JSON templates are parsed as a JSON value and substitutions occur only in
string leaves before normal JSON encoding. Form and query values use the
standard URL encoder. Header names are token-validated, values reject CR/LF,
and hop-by-hop or routing headers such as `Host`, `Connection`,
`Content-Length`, `Transfer-Encoding`, and `Proxy-Authorization` are forbidden.

Secret references are stored separately and render only during dispatch. They
never appear in preview responses, delivery errors, request logs, audit values,
or the browser after creation. The preview endpoint uses redacted sentinels and
shows the exact method, sanitized URL, headers, and body that would be sent.

This covers ordinary custom notification integrations, including signed
webhooks, without inheriting Komari's arbitrary-code and arbitrary-multi-request
execution surface. A provider that requires general computation must be added
as reviewed native Go code or called through an external gateway controlled by
the operator.

## Outbound network policy

All HTTP notification providers share one hardened transport. Configuration
validation and every actual connection enforce the same policy.

Default policy:

- HTTPS is required for non-loopback endpoints;
- URLs with user info, fragments, invalid ports, or non-HTTP schemes are
  rejected;
- loopback, private, link-local, carrier-grade NAT, documentation, benchmark,
  multicast, unspecified, and cloud metadata address ranges are denied;
- DNS is resolved for each connection and the selected socket address is
  checked immediately before dialing, preventing DNS-rebinding bypass;
- redirects are disabled by default; an owner may allow at most three
  same-origin redirects, with full validation on every hop;
- proxy environment variables are not inherited by the notification client;
- connect, TLS handshake, response-header, and whole-request deadlines are
  bounded, and response bodies are capped at 64 KiB;
- TLS verification cannot be disabled.

An owner-managed egress allowlist can permit an exact host and port or a narrow
CIDR for a self-hosted Bark or internal Webhook endpoint. Each exception records
its purpose, creator, recent-reauth evidence, and expiry. Broad private ranges,
wildcard domains, and `0.0.0.0/0` or `::/0` are rejected. Changing egress policy
or an endpoint requires owner role and recent reauthentication.

SMTP uses the same resolved-address classification. Remote plaintext SMTP
remains prohibited. Approved internal SMTP endpoints can be permitted by exact
host/port policy without enabling arbitrary TCP access.

## Provider contracts

### Bark

The default base URL is `https://api.day.app`; self-hosted bases require the
outbound policy above. Beat sends the official JSON `/push` contract. Device key
is required and encrypted. Level is limited to `active`, `timeSensitive`,
`passive`, or `critical`. Icon and click URLs must be HTTPS and are validated as
data shown to Bark, not fetched by Beat.

A 2xx response with the documented JSON code `200` succeeds. Empty or non-JSON
2xx responses are accepted only when the owner enables the channel's explicit
`http_2xx` compatibility mode for a self-hosted server.

### ServerChan

ServerChan Turbo stores a default fixed API origin and an encrypted SendKey,
rather than exposing the credential-bearing URL in normal configuration. It
supports bounded `channel`, `noip`, and encrypted OpenID fields.

ServerChan 3 accepts the documented HTTPS API URL because the account-specific
host and secret path are part of the service contract. The entire endpoint is
encrypted and API responses expose only a sanitized origin plus
`has_endpoint: true`. Tags are split, trimmed, deduplicated, and bounded.

Both senders require a 2xx response, drain only the bounded response body, and
never include that body or a credential-bearing URL in a user-visible error.

### Existing providers

Telegram keeps the fixed official endpoint by default. A custom Telegram API
origin is an owner-only egress exception. The bot token never enters a log field
or error string.

Email keeps current STARTTLS and implicit TLS behavior. Delivery errors expose
only a stable error code and sanitized SMTP stage, not recipient lists,
credentials, or remote server text.

The existing Webhook type remains a simple Beat JSON envelope for easy
integrations. Operators needing custom method, fields, signing, or credentials
use `custom_http`.

## Persistence and migration

This batch uses canonical SQLite migration `v6` after remote operations `v5`,
whose shared application-secret lifecycle it reuses. The authoritative
envelope, AAD, wrapped-key rotation, readiness, and backup contract is
[`application-secret-lifecycle-design.md`](./application-secret-lifecycle-design.md).
The assignment is maintained in
[`commercial-parity-implementation-sequence.md`](./commercial-parity-implementation-sequence.md).
It also reuses the explicit owner/admin, recent-auth, response-cache and audit
envelope from
[`administrative-mutation-consistency-design.md`](./administrative-mutation-consistency-design.md)
rather than implementing handler-local credential authorization.

Migration v6 rebuilds the channel model around immutable configuration
revisions and adds durable delivery state:

### `alert_channels`

- `id`, `name`, `channel_type`, `enabled`, `created_at`, `updated_at`;
- `current_revision`;
- `event_kinds_json` as a validated sorted set;
- `deleted_at` for history-preserving soft deletion.

### `alert_channel_revisions`

- `(channel_id, revision)` primary key;
- `channel_type` and canonical non-secret `public_config_json`;
- `secret_ciphertext` and `secret_present_json`;
- `template_subject`, `template_text`, and compiled template schema version;
- `created_by`, `created_at`, and a sanitized change reason.

All secret documents and credential-bearing endpoints use AES-GCM through the
`v5` versioned envelope and active wrapped data key. Associated data binds
channel ID, revision, channel type, field, and schema version. Updates create a
new revision; blank secret inputs preserve the previous value, while explicit
owner-only clear actions remove it. Deleting a channel does not erase revisions
referenced by retained delivery evidence.

Every HTTP endpoint is encrypted regardless of whether it appears to contain a
credential. The public revision stores only the canonical scheme, sanitized
host/port display, and non-secret behavior fields. Beat never attempts to infer
whether a query value, path component, subdomain, or username-like token is
secret.

### `notification_messages`

- immutable message ID, source kind/ID, event kind, and creation time;
- encrypted subject, text, and structured data;
- payload schema version and retention deadline;
- routing state (`routed` or `not_routed`) and bounded selected-channel count;
- a unique deterministic source key that prevents duplicate enqueue.

### `notification_deliveries`

- delivery ID, message ID, channel ID, and channel revision;
- state: `pending`, `leased`, `succeeded`, `dead`, or `cancelled`;
- attempt count, next attempt time, lease token hash, lease expiry, and worker;
- sanitized error code/stage, HTTP or SMTP status class, and completion time;
- created/updated timestamps and uniqueness on `(message_id, channel_id)`.

### `notification_delivery_attempts`

- delivery ID and monotonically increasing attempt number;
- started/completed timestamps, duration, outcome, sanitized error code, and
  response status class;
- no rendered body, remote response body, secret, full recipient, or
  credential-bearing URL.

### `notification_channel_health`

- channel ID primary key and current channel revision;
- consecutive retryable failure count and last success/failure time;
- circuit state and `open_until` timestamp;
- last sanitized error code/stage and update time.

Health rows are mutable operational state and remain separate from immutable
channel revisions. Updating a channel revision resets its circuit and failure
counter without erasing retained delivery attempts.

### `notification_settings`

- success-history retention, default 30 days, range 7-365;
- dead-delivery retention, default 90 days, range 30-730;
- maximum global workers, default 8 and hard maximum 32;
- the reviewed outbound allowlist and redirect policy.

Schema migration creates the tables and marks notification secret backfill as
pending. Before the HTTP listener starts, Server startup initializes the `v5`
root/wrapped-key registry, parses every legacy typed config, writes revision 1,
encrypts its complete secret subset, clears the legacy plaintext value in one
immediate transaction, and marks backfill complete. Startup and readiness fail
if any credential cannot be parsed, encrypted, or proven removed from the
legacy column. The migration never silently discards a channel.

The backup format needs no new payload beyond the reviewed format v2: SQLite
contains ciphertext plus wrapped data keys and `secrets/admin-data.key` contains
the matching root key. Archive validation must unwrap every referenced data key
and authenticate every channel/message ciphertext with reconstructed AAD before
staging. Restore resets expired leases to `pending`, preserves terminal states,
and records a restore reconciliation audit event. A delivery accepted externally
after the snapshot cutoff may be repeated after restore; the UI and runbook
state this unavoidable at-least-once recovery property.

Migration v6 is forward-only. Rollback to an older binary requires restoring
the pre-migration backup because older binaries reject a newer schema version.

## Durable delivery semantics

Alert trigger/resolve persistence and its notification message/outbox rows must
commit in the same immediate SQLite transaction. Traffic-report completion and
its scoped delivery rows use the same rule. A deterministic source key and
unique constraints make producer retries harmless.

Successful-login session creation, user last-login mutation, the
`security.login_succeeded` message, and every delivery row selected by the
installed routing rules must likewise commit in one immediate transaction. If
no channel subscribes, the transaction persists the message as `not routed`
rather than silently dropping the security event. Threshold state,
`security.login_suspicious`, and its selected delivery rows follow the same
rule. Authentication must fail closed if the session transaction fails; a
notification worker failure after commit never invalidates the valid session.

Workers claim due rows using an immediate transaction, replace the lease with a
cryptographically random token, commit, and then perform network I/O. A lease
holder may complete only when the token still matches. Shutdown stops new
claims, cancels in-flight requests, and waits for bounded completion. Expired
leases return to `pending` at startup.

External notification APIs generally do not provide exactly-once delivery.
Beat therefore guarantees durable at-least-once attempts with these duplicate
controls:

- stable `X-Beat-Delivery-ID` and `Idempotency-Key` headers for Webhook and
  custom HTTP requests;
- stable delivery ID in the provider payload where supported;
- no retry after a confirmed 2xx/provider success;
- retry only for transport failure, timeout, HTTP 408/425/429/5xx, or SMTP 4xx;
- HTTP 429 `Retry-After` honored within configured bounds;
- other HTTP 4xx and SMTP 5xx move directly to `dead`.

Default retry delays are 5 seconds, 30 seconds, 2 minutes, 10 minutes, and 30
minutes with cryptographic jitter, for at most six total attempts. Per-channel
single flight preserves order and prevents a failing endpoint from consuming
the global worker pool. A persisted circuit opens after five consecutive
retryable failures, pauses the channel for five minutes, and is reset by a
successful test or delivery.

The owner can retry a dead delivery. Manual retry creates a new delivery ID
linked to the original and requires confirmation that the receiver may observe
a duplicate. No failed attempt is silently deleted.

## Resource and abuse limits

- maximum endpoint: 2 KiB; public config: 32 KiB; encrypted secret document:
  16 KiB;
- maximum subject: 200 bytes; text: 16 KiB; complete request body: 64 KiB;
- maximum response body read: 64 KiB;
- maximum 32 headers, 32 query values, eight named secrets, and eight recipients
  per channel;
- per-request timeout: 15 seconds default, 30 seconds hard maximum;
- eight global workers default, one active request per channel;
- test sends: five per channel per ten minutes and 30 per administrator per
  hour;
- preview endpoints perform no network request;
- queue depth and SQLite free-space pressure are observable, but Beat never
  drops a critical pending row to make room.

When SQLite cannot persist an outbox row, the source operation returns a stable
error and records a high-severity operational log. Reconciliation scans for
source records missing their deterministic message key. Delivery-provider
unavailability does not make the public dashboard unavailable; inability to
read or write the durable notification queue makes readiness degraded.

## API and authorization

Existing channel list/create/update/delete/test routes retain their public
shape where practical, but channel `config` remains sanitized and never
contains a secret. New endpoints provide:

- template validation and redacted preview;
- event-category vocabulary and route editing;
- paginated delivery/attempt history with filters;
- dead-delivery retry and cancellation of pending deliveries;
- owner-managed outbound policy.

`POST /api/v1/alerts/channels/{id}/test` creates a durable test message and
delivery, then returns `202 Accepted` with its delivery ID and initial state.
The UI follows that record until it succeeds or becomes dead. Test delivery
uses the same worker, network policy, retry classification, history, and secret
handling as production delivery; it does not bypass the queue with a separate
synchronous code path.

Public routes remain unchanged and expose no channel or delivery information.

Authorization:

- owner: create or change endpoints, secrets, templates, event routes, and
  outbound policy; retry dead deliveries; delete channels;
- admin: list channels/history, enable or disable an existing reviewed channel,
  and issue a bounded test send;
- recent reauthentication: endpoint, credential, custom template, outbound
  policy, delete, or dead-letter retry changes.

Audit values include channel ID/name/type, revision, changed field names,
delivery ID, action, actor, and outcome. They exclude endpoint query/path
secrets, recipients, rendered payloads, response bodies, and credentials.

## Administration UI

The existing `/admin/alerts` area remains a compact shadcn/ui operations
workspace. It gains three views rather than introducing decorative nested
cards:

1. **Channels**: a dense table with readable provider names, enabled state,
   subscribed event labels, last success/failure, queue depth, and actions.
2. **Delivery log**: state tabs, filters, a delivery timeline, sanitized error
   stage, attempt history, and explicit retry/cancel commands.
3. **Routing**: an event-by-channel matrix for repeated bulk changes, displaying
   channel names and event labels rather than database IDs.

The channel dialog uses provider-specific fields, password controls with
`configured` markers, a fixed event-category multi-select, and separate
template/request preview. Bark level uses a select, binary options use switches,
and provider types always render localized labels. Credential-bearing endpoints
are shown as sanitized origins. Custom HTTP exposes structured method, URL,
headers, query, body mode, authentication, and HMAC controls; it is not a free
JavaScript editor.

Background delivery changes update through query invalidation or the existing
authenticated live-update mechanism without a full-page refresh. Error copy
names the failed stage and an operator action, and never echoes a secret.

## Observability and maintenance

Low-cardinality Prometheus metrics include:

- delivery attempts by provider type and outcome;
- queue depth by state;
- oldest pending age;
- retry and dead-letter totals;
- open circuit count;
- dispatch duration histogram;
- secret-backfill and queue readiness state.

Channel IDs, URLs, recipients, node IDs, rule IDs, and error messages are not
metric labels. Structured logs use delivery ID, provider type, attempt, error
code, duration, and state. The maintenance scheduler deletes expired terminal
delivery history and unreferenced encrypted messages in bounded batches; it
never deletes pending or leased work.

## Test and verification gate

Implementation is not accepted until all of the following pass:

1. table-driven provider contract tests for Bark, both ServerChan variants,
   existing providers, and custom HTTP;
2. template parser, escaping, size, unknown-variable, CRLF, JSON/form/query,
   HMAC, and secret-redaction tests plus fuzzing of structured templates;
3. SSRF tests for IPv4/IPv6 loopback, private/link-local/CGNAT/metadata ranges,
   encoded hosts, DNS rebinding, redirects, proxy environment variables, and
   exact allowlist exceptions;
4. encryption/AAD, blank-secret preservation, explicit clear, revision,
   mixed-key rotation, plaintext-zero backfill, migration rollback, full
   backup-key pairing validation, restore, and wrong-key tests;
5. concurrent lease, crash/restart, expired lease, retry classification,
   `Retry-After`, circuit breaker, duplicate producer, manual retry, shutdown,
   and queue reconciliation tests;
6. frontend tests proving every select renders labels rather than IDs, secrets
   never reappear, preview is redacted, route matrix keyboard use works, and
   delivery rows update without page reload;
7. browser tests for owner/admin authorization, recent reauthentication,
   create/test/fail/retry/disable flows, mobile layout, and accessibility;
8. Go statement and frontend line coverage at or above 90%, `go test -race`,
   repository tests, frontend tests/build, `goimports-reviser`, and
   `golangci-lint`;
9. a load test with at least 10,000 queued deliveries, bounded worker
   concurrency, restart during dispatch, and no loss or duplicate database row;
10. pre-deployment backup, migration validation, SQLite integrity, MTS
    isolation proof, IPv4/IPv6 public/admin authentication smoke tests, and a
    credential-free controlled receiver test.

## Approval boundary

Implementation requires one explicit approval because it changes SQLite schema,
channel secret storage, delivery semantics, administrator permissions, API
responses, background workers, maintenance retention, and backup/restore
reconciliation. It also depends on the complete shared application-secret
lifecycle planned in remote operations and may not weaken its AAD, rotation,
readiness, or backup-validation rules.

Approval text:

```text
批准按 notification-provider-breadth-design.md 完整实施通知扩展批次
```
