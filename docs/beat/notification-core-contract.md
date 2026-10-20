# Notification v10 integration contract

Status: approved implementation contract, 2026-09-13. SQLite v10 depends on v9;
backup format remains v3. Verification uses local HTTP/SMTP stubs only.

## Ownership and producers

`notificationqueue` owns channels, immutable revisions, messages, deliveries,
attempts, health, settings and ingress budgets. It does not import `store`.
`notification` owns bounded declarative rendering and outbound transports.
The root integration owns alert/report producers and the administration UI.

The queue exposes:

```go
Enqueue(ctx context.Context, request Request, mutation func(*sql.Tx) error) (Receipt, error)
SourceReceipt(ctx context.Context, sourceKey string) (Receipt, error)
```

`Request` has `SourceKey`, `SourceKind`, `SourceID`, `EventKind`, `Subject`,
`Text`, `Data map[string]string`, `ChannelIDs []string`, and
`UseSubscriptions bool`. Data uses the reviewed flat scalar template keys;
`event.kind` comes from `EventKind`. Subject is bounded to 200 UTF-8 bytes,
text to 16 KiB, and the complete rendered transport body to 64 KiB. Oversized
reports require explicit producer budgeting/segmentation; text is not truncated.

One secret write window and one immediate SQLite transaction cover source-key
deduplication, business mutation, routing, payload encryption and delivery rows.
The callback uses its supplied transaction and performs no network or nested
enqueue operation. An existing source key skips the callback and returns the
original message receipt. `ErrSkipMutation` rolls back the transaction and
returns `Skipped: true`; every other callback error also rolls back. No matching
channel still persists `not_routed` with the source mutation.

`Receipt` returns `message_id`, `routing_state`, `state`, `counts`, `deliveries`,
`duplicate`, and `skipped`. Each delivery reference has `id`, `channel_id`,
`channel_revision`, `state`, and `attempt_count`. Counts contain pending, leased,
succeeded, dead and cancelled totals. Aggregate state prioritizes unfinished
pending/leased work, then dead, then cancelled; success requires all effective
deliveries to have succeeded. Source queries follow the current leaf of each
manual retry chain while preserving earlier attempts and dead rows.

Scheduled reports build and validate the complete message before a transaction
advances the period and enqueues delivery. Reports over 1,000 nodes or 16 KiB of
text fail explicitly without consuming that period; use smaller scopes. Report
tests are distinct audited jobs with a one-minute per-schedule admission limit.
The report list distinguishes queued/delivered/partial/failed/not-routed states.
If retention has removed its delivery evidence, it shows history unavailable;
neither enqueue success nor missing history is presented as delivery success.

Deployed v10 categories are alert.triggered, alert.acknowledged, alert.resolved,
availability.offline, availability.recovered, traffic.quota, traffic.report,
and notification.delivery_failed. notification.test is internal. Login/Fleet,
enrollment, remote-operation and node-expiry producers remain in their owning
future migrations and are not advertised as implemented.

## Channels and public API

Existing `/api/v1/alerts/channels` paths remain. `config` is now a typed JSON
object. Writes include name, channel_type, enabled, event_kinds,
expected_revision, config, clear_secrets, template_subject and template_text.
Updates require a matching revision. Sensitive blank/omitted fields retain the
previous value; explicit clear paths remove them. Creation and configuration,
template, routing, deletion, retry and egress changes require current owner and
recent session proof, rechecked transactionally with domain audit.

The full provider document is encrypted. Public channel responses contain only
reviewed behavior fields, configured markers, a sanitized endpoint display,
template strings, the subscription set, revision and operational health.
Recipients and credential-bearing endpoints are never returned after storage.
Custom endpoint displays mask the host as well as its path/query.

Provider write fields:

| Provider | Config |
| --- | --- |
| webhook | endpoint, optional headers |
| telegram | bot_token, chat_id, parse_mode, optional API origin endpoint |
| email | host, port, username, password, from, to, security |
| bark | endpoint, device_key, level, group, icon, sound, url, compatibility |
| serverchan_turbo | send_key, open_id, channel, noip |
| serverchan_3 | endpoint, tags |
| custom_http | endpoint, method, headers, query, body_mode, body, named_secrets, auth, hmac, timeout_seconds, redirects |

Custom method is GET/POST; body mode is json/form/text. Auth is
`{mode:none|basic|bearer,username?,password?,token?}` and HMAC is
`{secret,header?,timestamp_header?}`. Secret substitutions use `{{secret.name}}`.
The complete encrypted config is bounded to 16 KiB, with up to eight named
secrets/recipients, 32 headers and 32 query fields. Templates use scalar
substitution only and never execute code.

Admin operations are list/history, PATCH `.../{id}/enabled`, and bounded POST
`.../{id}/test`. Test returns 202 with a durable receipt and uses the production
worker. Preview endpoints send no network requests. Delivery history lives at
`/api/v1/notifications/deliveries`, with state/channel/offset/limit filters;
detail includes attempts. Owner/recent POST `.../{id}/retry` requires
`confirm_duplicate:true`; POST `.../{id}/cancel` preserves attempt evidence.

## Worker semantics

Claims commit a random hashed lease and attempt before any network I/O.
Completion requires the current token and unexpired lease. Eight workers are
default, 32 maximum; one request per channel is allowed at a time. The network
deadline is 15 seconds by default and 30 seconds maximum. Automatic attempts
are bounded to six, with 5 s / 30 s / 2 min / 10 min / 30 min delays and jitter.
HTTP 408/425/429/5xx, SMTP 4xx and transport/timeouts are retryable; other
provider rejection is terminal. Retry-After is bounded. Five retryable failures
open a persisted five-minute circuit; successful test/delivery resets it.

Disabling a channel pauses pending work and cancels local in-flight requests;
it does not delete evidence. Deletion is soft and cancels unfinished work.
Configuration changes pin new deliveries to the new revision and reset health;
existing deliveries retain their original reviewed revision. Manual retry
creates a new ID against the current reviewed revision and links `retry_of`.
Original fanout is unique on message/channel; each dead row gets at most one
retry successor, and its next dead successor may itself be retried.

Transport never inherits environment proxies, follows unchecked redirects,
disables certificate checks, or returns upstream body/credential-bearing errors.
Every connection resolves and validates its pinned socket address. Owner
exceptions are exact host/port or narrow CIDR with purpose, author and expiry.
Metadata and unsafe ranges remain denied. Stable delivery identifiers accompany
supported provider requests. Recovery remains at least once across external
acceptance followed by a process crash or backup restore.

The runtime constructs `notificationqueue.NewService(Config{Database, Secrets,
Sender, Now, Random, Workers})` only after v10 secret backfill. `Run(ctx)` is
supervised; shutdown stops claims, cancels active requests and waits before
SQLite closes. Readiness checks queue persistence; provider outages alone do
not disable the public dashboard.
