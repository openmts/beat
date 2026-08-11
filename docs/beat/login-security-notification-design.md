# Login security notifications

Updated: 2026-07-30

Status: reviewed design; implementation requires explicit approval

## Goal and product boundary

Beat will create durable, privacy-controlled security events for successful
administrator authentication and material suspicious-login conditions, then
route them through the commercial notification queue. The feature helps an
owner notice unauthorized access without making login depend on an outbound
email, webhook, Telegram, Bark, or ServerChan request.

Public monitoring remains available without login. Security events, policies,
delivery history, exact audit data, and management controls are administrator-
only. This batch does not change the requirement that every management action
must be authenticated and authorized.

## Current Beat evidence and gap

`internal/adminauth/service.go` currently provides constant-work
password verification, optional TOTP, per-IP/per-username in-memory rate
limits, revocable sessions, and persistent `auth.login` audit records.

The current successful-login sequence is:

1. verify password and TOTP;
2. create the session row;
3. update the user's last-login time;
4. clear the in-memory limiter;
5. attempt a separate audit write whose error is ignored.

The current implementation has no durable security event, no notification
outbox, no persisted suspicious threshold, no source/device change comparison,
and no delivery history. A process restart also clears failed-login thresholds.

The reviewed commercial notification design already reserves
`security.login_succeeded` and `security.login_suspicious` categories and a
durable outbox. This document completes the producer semantics, persistence,
privacy, user experience, and verification boundary.

## Competitor evidence and intentional hardening

Komari commit `4077201f098774511eaf504f220c5f6be009346b` optionally sends a
login event when a session is created. It includes authentication method, full
source IP, GeoIP name, and raw user-agent text:

- session creation and login event:
  <https://github.com/komari-monitor/komari/blob/4077201f098774511eaf504f220c5f6be009346b/database/accounts/sessions.go>
- login-notification setting:
  <https://github.com/komari-monitor/komari/blob/4077201f098774511eaf504f220c5f6be009346b/internal/config/settings.go>
- login event category:
  <https://github.com/komari-monitor/komari/blob/4077201f098774511eaf504f220c5f6be009346b/database/models/messageEvent/MessageEvent.go>

The useful outcome is immediate awareness of a new session. Beat intentionally
does not copy the implementation because the event is launched in an
untracked goroutine before the session database insert, outbound delivery is
synchronous/retried in process, failures are not durable, and the raw IP and
user agent are sent to the configured third party.

Beat instead commits the session and security outbox atomically, sends later
through leased workers, and exposes coarse source/device information by
default.

## Security and reliability invariants

1. Authentication never performs outbound notification network I/O.
2. Session creation, user last-login update, successful-login security event,
   audit event, notification message, and selected delivery rows commit in one
   immediate SQLite transaction.
3. If that transaction fails, authentication fails closed and no usable
   session cookie is issued. A delivery-worker failure after commit never
   invalidates the valid session.
4. Every successful password, future OIDC, recovery, or bootstrap session has
   one deterministic security event. Retries cannot create duplicate events.
5. Ordinary failed attempts remain audit/threshold input and do not each create
   an outbound notification.
6. Suspicious events are generated only by persisted bounded rules and use a
   deterministic aggregation key to prevent notification flooding.
7. Security event and delivery payloads never contain passwords, TOTP values,
   session IDs/tokens/prefixes, cookies, OIDC tokens/claims, authorization
   headers, reset/recovery secrets, or raw user-agent strings.
8. A notification does not confirm whether an attempted username exists.
9. Canonical source addresses come only from the trusted-proxy-aware request
   helper. Untrusted forwarding headers cannot alter event identity.
10. Security events are platform application data in SQLite. They are not Agent
    metrics and never create MTS measurements.

## Event vocabulary and payload

The first revision defines the two notification-v6 categories:

- `security.login_succeeded`: one administrator session was committed,
  including bootstrap through the fixed `bootstrap` authentication method;
- `security.login_suspicious`: a durable threshold or material change rule
  crossed, including lockout through the fixed `rate_limit_lockout` reason.

Each event contains only a versioned structured envelope:

- event ID, kind, severity, occurrence time, and deterministic source key;
- administrator ID and display-name snapshot only after identity is proven;
- authentication method from a fixed enum (`password`, `oidc`, `recovery`, or
  `bootstrap`);
- canonical source family plus privacy-controlled source summary;
- normalized browser family, OS family, and device class when parsable;
- coarse country/region only when trusted GeoIP is installed and allowed;
- reason codes from a fixed bounded enum;
- related audit-event ID and notification-message ID;
- policy revision used for classification.

Free-form raw request values are not copied into the event. Unknown or
oversized authentication methods, reason codes, device strings, or location
values are rejected or mapped to fixed `unknown` values.

## Source, device, and location privacy

The canonical full source address remains available in the existing protected
audit/session data subject to retention. The default notification summary is:

- IPv4: network prefix truncated to `/24`;
- IPv6: network prefix truncated to `/48`;
- loopback/private/link-local: a fixed class label rather than the literal
  address;
- optional trusted GeoIP: country and coarse region, never precise coordinates.

An owner may explicitly enable full source IP in security notifications after
recent reauthentication, but the UI warns that the address will be sent to
third-party channels. The default and recommended setting remains coarse.

The raw user agent is parsed locally through a bounded maintained parser. The
security payload keeps only normalized browser family, OS family, and broad
device class. Parsing errors produce `unknown`; the raw value is never placed in
notification messages, delivery errors, or Prometheus labels.

Source and device comparison uses keyed HMAC fingerprints so equality can be
checked without making raw values part of the event routing key. Rotating the
HMAC key starts a new comparison epoch and cannot reveal earlier values.

## Suspicious-login detection

The first revision uses explainable bounded rules rather than a risk score or
machine-learning model:

- `rate_limit_lockout`: the persisted IP or username-key threshold locks;
- `repeated_invalid_totp`: a proven account receives a bounded number of
  invalid TOTP attempts within the configured window;
- `new_source_prefix`: a successful login uses a source prefix not seen for
  that administrator during the lookback period;
- `new_device_family`: a successful login uses a new normalized browser/OS
  family during the lookback period;
- `disabled_or_revoked_identity`: repeated attempts target a disabled account
  or revoked external identity, without exposing that fact in the response;
- `concurrent_distant_source`: optional only after trusted GeoIP exists and
  only when two recent successful sessions have incompatible coarse locations.

Defaults are conservative: lockout at the existing five failures in 15
minutes, invalid-TOTP suspicious threshold at five in 15 minutes, and a 30-day
successful-login lookback. New source or device produces a medium event; a
lockout or combined new source plus new device produces high severity.

Failed-attempt aggregation keys use HMACs of normalized attempted username and
canonical source prefix. Delivery payloads say that repeated failed login
attempts occurred; they do not include the attempted username unless identity
was already proven by a successful password step and the policy explicitly
allows the administrator display name.

The persisted threshold record has a fixed expiry and bounded global row count.
Oldest expired rows are deleted in batches. An attacker cannot create an
unbounded number of username or source keys.

## Transaction and outbox semantics

The authentication service moves session creation into one store operation.
For a successful login it writes:

- session row;
- user last-login time;
- successful-login security event;
- successful `auth.login` audit event;
- updated known-source/device fingerprint state;
- notification message and one delivery row per matching channel revision.

The deterministic source key is
`login-success:<session-id>` internally, but the session ID is not copied into
the message payload. Unique constraints make a retried transaction idempotent.
If no channel subscribes, the immutable message is recorded as `not_routed` so
the security event is still visible and auditable.

Failed attempts update the persistent threshold and audit event in one
transaction. Crossing a suspicious threshold inserts the security event and
outbox rows in that transaction. Later attempts inside the deduplication window
increment a bounded count and last-seen time without creating new deliveries.

The notification v6 leased-worker, retry, circuit-breaker, dead-letter, and
secret-handling semantics apply unchanged. Authentication latency includes only
SQLite work, never provider latency or DNS.

## Persistence and dependency order

This producer uses canonical SQLite migration `v12`. It depends on commercial
notification delivery `v6`, follows GeoIP/lifecycle `v7` for optional trusted
location, and is ordered after automation access `v11` so the final security
event vocabulary is stable. It retains backup archive format `v4`. The
assignment is maintained in
[`commercial-parity-implementation-sequence.md`](./commercial-parity-implementation-sequence.md).
SQLite additions are application data:

- `security_events`: immutable event envelope, severity, actor snapshot,
  privacy-safe source/device/location summaries, reason set, policy revision,
  source key, and retention deadline;
- `security_login_fingerprints`: administrator, HMAC epoch, source/device
  fingerprints, first/last seen, and bounded success count;
- `security_login_thresholds`: key kind/HMAC, window start, count, lock expiry,
  last seen, and last emitted event;
- `security_notification_settings`: enabled event kinds, privacy level,
  lookback/threshold bounds, retention, and revision.

Notification content remains in the encrypted `notification_messages` and
delivery tables defined by notification v6. Security settings and routing
changes require owner role, recent reauthentication, and an audit record.

No password hash, raw credential, full user agent, or session token is added to
these tables. Indexes use bounded event kind, actor ID, occurrence time,
severity, and HMAC values.

## API and authorization

Administrator endpoints provide:

- paginated security-event history with kind, severity, actor, time, source
  class, reason, and delivery state filters;
- one event detail with linked sanitized audit and delivery attempts;
- owner-managed security-notification policy and redacted preview;
- acknowledgement metadata for operator workflow, without mutating the
  immutable source event.

Owners manage thresholds, privacy, retention, and routes. Admins may list,
filter, inspect, and acknowledge events. Full-source display, when enabled,
requires owner role and recent reauthentication. Public and Agent routes expose
none of this data.

All selectors return `{value, label}` options and render administrator names,
event labels, reason labels, channel names, and severity labels. IDs remain
machine values and never appear after selection.

## Administration UI

Security events live in the existing `/admin/security` workspace. The audience
is an operator investigating access, and the page's single job is to establish
what happened, from where, and whether notification delivery succeeded.

The visual system remains Beat's shadcn `base-nova`, semantic colors, Geist
type, and tabular timestamps. Its signature element is an unframed chronological
event rail that aligns authentication, audit, and delivery stages without
turning each stage into a nested Card.

The interface composes:

- `Tabs` for events, sessions, administrators, TOTP, and audit views;
- `Table` for dense event history and `Badge` for severity/delivery state;
- `Sheet` with an accessible title for event detail and linked attempts;
- `FieldGroup`, `Field`, `Switch`, `Select`, and bounded numeric inputs for
  owner policy;
- `Alert` for dead delivery or degraded persistence;
- `Skeleton`, `Empty`, and `Tooltip` for loading, empty, and icon states.

There is no live feed that steals focus. Background updates preserve current
filters, scroll position, and the open detail Sheet. Event rows show human
names, coarse source, device summary, reason, time, and delivery state; raw IDs,
raw user agents, and credentials are absent.

## Retention, backup, and rollback

Security events default to 180 days, configurable from 30 through 730 days.
Threshold windows expire quickly; known source/device fingerprints default to
90 days. Maintenance deletes expired terminal data in bounded batches but never
deletes pending notification deliveries.

SQLite backup includes settings, events, threshold state, fingerprints,
encrypted messages, and delivery history. Restore expires old leases, preserves
event/message source keys, and prevents duplicate enqueue during reconciliation.

Rollback requires the matching pre-migration SQLite backup because the old
binary rejects the newer schema. No MTS migration or metric cleanup is involved.

## Observability and failure behavior

Prometheus metrics include successful-login events, suspicious events by fixed
reason/severity, transaction failures, threshold row count, outbox latency,
delivery state, and oldest undelivered security message. Usernames, IPs, device
families, event IDs, channel IDs, and free-form errors are not labels.

Readiness degrades if security-event/outbox persistence is unavailable after
the feature is enabled. A provider outage affects delivery health but does not
make authentication unavailable after the session transaction commits. Logs
contain request/event IDs and stable error codes, never message payloads or raw
request identity data.

## Test and acceptance gates

Backend tests cover:

- atomic session/user/audit/security-event/outbox commit and rollback;
- fail-closed persistence errors and no cookie on transaction failure;
- no outbound call in the login request;
- password, TOTP, bootstrap, and future OIDC method mapping;
- threshold windows, lock expiry, deduplication, restart persistence, global
  bounds, HMAC epochs, and concurrent attempts;
- unknown-user constant work and no username-enumerating event payload;
- trusted/untrusted proxy IPv4/IPv6 source handling, prefix privacy, raw-IP
  opt-in, user-agent normalization, GeoIP absence/failure, and redaction;
- notification routing, no-subscriber state, retry/dead-letter linkage,
  retention, backup/restore, and reconciliation idempotency.

Frontend tests cover every selector rendering labels rather than IDs, owner and
admin permissions, privacy preview, event filtering/detail, background updates,
dead delivery, mobile layout, keyboard use, accessible names, and absence of
raw user agents, tokens, or hidden full IP values from the DOM.

Acceptance requires at least 90 percent Go statement and frontend line
coverage, race tests, lint, `goimports-reviser`, production build, browser
authentication tests, controlled notification delivery, backup/restore drill,
and IPv4/IPv6 evidence that public routes remain no-login while security history
and policy routes return `401` without an administrator session.

## Approval boundary

This design authorizes no implementation. It is canonical SQLite migration
`v12` and changes login transaction
semantics, rate-limit persistence, SQLite schema, notification production,
security retention, administrator APIs, and UI. Implementation requires prior
approval of notification v6 and explicit approval of this complete design.
