# GeoIP and node lifecycle

Status: Reviewed design; implementation requires explicit approval

## Goal

Beat must match current Komari and DStatus location, price, billing-cycle, and
expiry presentation while meeting commercial privacy, correctness, recovery,
and operational requirements.

The proposed implementation, once explicitly approved, adds:

- privacy-first country resolution with local MMDB, reviewed HTTPS providers,
  manual overrides, bounded persistent cache, and background refresh;
- trusted source-IP selection that never treats an Agent-reported host as proof
  of network origin;
- optional public country display and country/expiry filters;
- provider, plan, exact price, billing interval, expiry date, and renewal mode;
- durable expiry reminders, expired and renewed events, and calendar-based
  automatic date advancement;
- provider/MMDB health, audit, backup/restore, and shadcn/ui administration.

Node and GeoIP configuration is platform application data and belongs in
SQLite. Agent metrics remain exclusively in MTS. Country databases are bounded
application assets, not time-series data.

## Current Beat boundary

Beat currently stores the Agent-advertised or administrator-configured
`nodes.host`, SSH port, presentation fields, traffic policy, inventory, and
heartbeat timestamps. It has no country, GeoIP provider, expiry, price,
billing interval, renewal history, or lifecycle notification state.

The current Agent report body supplies `host`; per-node authentication proves
which node is reporting but does not prove that the body value is the socket's
network origin. Public IP display can be disabled globally, but there is no
independent location-visibility control.

Relevant current boundaries:

- `backend/internal/model/node.go`;
- `backend/internal/store/node.go` and `node_heartbeat.go`;
- `backend/internal/api/handler/node.go` and `node_response.go`;
- `backend/internal/api/handler/node_update.go`;
- `frontend/src/components/admin-node-card.tsx`;
- `frontend/src/types/index.ts`.

## Competitor audit

The public upstream snapshots were inspected on 2026-07-30.

Komari stores region, price, free-form currency, billing-cycle days,
auto-renewal, and an expiration timestamp. It displays price and remaining time,
sends a configurable lead-time expiration message, and advances expiration for
online auto-renewing nodes. Its GeoIP providers include local MaxMind MMDB,
IPinfo, GeoJS, and ip-api. Country lookup is cached for 48 hours and is used to
derive a flag emoji.

The useful product surface is broad, but the inspected implementation also has
commercial risks:

- the free ip-api provider sends IP addresses over plaintext HTTP;
- every remote provider discloses node IPs without a per-provider privacy
  consent boundary;
- the MMDB updater uses an unsigned third-party download, unbounded `http.Get`,
  permissive directory/file creation, and non-atomic overwrite;
- some MMDB update error paths return while holding the update mutex;
- expiration reminders can repeat every day inside the lead window and ignore
  delivery errors;
- automatic renewal merely advances a date, depends on the node being online,
  and does not perform or verify payment.

DStatus stores `expire_time`, displays remaining time publicly, counts and
filters nodes expiring in 3/7/30 days, automatically resolves a country through
a fixed HTTPS service, retries failed resolution in memory, and supports a
manual country override. It stores location inside an untyped server JSON blob,
logs full IPs in location diagnostics, uses process-local unbounded maps for
cache/failures, and has no billing price or renewal ledger.

Beat matches the legitimate capability, not those failure modes.

Evidence:

- Komari client lifecycle fields:
  <https://github.com/komari-monitor/komari/blob/4077201f098774511eaf504f220c5f6be009346b/database/models/models.go>
- Komari GeoIP interface and cache:
  <https://github.com/komari-monitor/komari/blob/4077201f098774511eaf504f220c5f6be009346b/utils/geoip/geoip.go>
- Komari MMDB download and reload:
  <https://github.com/komari-monitor/komari/blob/4077201f098774511eaf504f220c5f6be009346b/utils/geoip/mmdb.go>
- Komari ip-api, GeoJS, and IPinfo providers:
  <https://github.com/komari-monitor/komari/blob/4077201f098774511eaf504f220c5f6be009346b/utils/geoip/ipapi.go>,
  <https://github.com/komari-monitor/komari/blob/4077201f098774511eaf504f220c5f6be009346b/utils/geoip/geojs.go>,
  and
  <https://github.com/komari-monitor/komari/blob/4077201f098774511eaf504f220c5f6be009346b/utils/geoip/ipinfo.go>
- Komari source-IP fallback and region enrichment:
  <https://github.com/komari-monitor/komari/blob/4077201f098774511eaf504f220c5f6be009346b/web/api/client/uploadBasicInfo.go>
- Komari expiry notifier and date advancement:
  <https://github.com/komari-monitor/komari/blob/4077201f098774511eaf504f220c5f6be009346b/utils/notifier/expire.go>
  and
  <https://github.com/komari-monitor/komari/blob/4077201f098774511eaf504f220c5f6be009346b/utils/renewal/renewal.go>
- DStatus location service and retry/cache logic:
  <https://github.com/fev125/dstatus/blob/4afc9e43c9df28096352c05ae924fcadbc830a2f/modules/stats/iplocation.js>
- DStatus expiry persistence:
  <https://github.com/fev125/dstatus/blob/4afc9e43c9df28096352c05ae924fcadbc830a2f/database/servers.js>
- DStatus public expiry and country presentation:
  <https://github.com/fev125/dstatus/blob/4afc9e43c9df28096352c05ae924fcadbc830a2f/static/js/stats.js>
  and
  <https://github.com/fev125/dstatus/blob/4afc9e43c9df28096352c05ae924fcadbc830a2f/views/stats/card.html>
- MaxMind database update guidance:
  <https://dev.maxmind.com/geoip/updating-databases/>
- MaxMind country database semantics:
  <https://dev.maxmind.com/geoip/docs/databases/city-and-country/>

## Product semantics

### Location scope

The parity target is country-level location. Beat stores a normalized source
scope and optional ISO 3166-1 alpha-2 country code. It does not collect city,
postal code, latitude/longitude, ISP, organization, ASN, or timezone in this
batch. Country names are rendered from the code with the browser's
`Intl.DisplayNames`; flag emoji is derived locally. This avoids stale localized
names and unnecessary personal data.

Source scope is one of:

- `public`: eligible for country resolution;
- `private`: RFC1918 or IPv6 ULA;
- `loopback`;
- `link_local`;
- `carrier_grade_nat`;
- `documentation` or `benchmark`;
- `multicast`, `unspecified`, or `invalid`.

Non-public sources never leave the Server. The UI renders a localized scope
label such as `Local network` instead of inventing a country code.

### Location modes

Each node has one mode:

- `observed`: use the canonical Agent request address after trusted-proxy
  processing; this is the default for authenticated Agent nodes;
- `managed_target`: resolve an owner-approved managed hostname or address;
  never enabled automatically;
- `manual`: owner/admin selects a country or local/other scope and automatic
  refresh cannot overwrite it;
- `disabled`: no resolution and no public location.

Agent payload `host`, inventory, interface addresses, and reverse DNS are not
trusted location inputs. For `observed`, the HTTP middleware's effective client
IP is authoritative: forwarding headers from untrusted peers remain ignored by
the existing trusted-proxy policy.

Managed-target mode resolves through a dedicated resolver with the same address
classification. Private results stay local. A hostname returning multiple
public countries is rejected as ambiguous instead of selecting one silently.

### Provider modes

Global provider mode is one of:

1. `disabled`;
2. `mmdb`, recommended and default after a database is installed;
3. `geojs_https`;
4. `ipinfo_https`, with optional encrypted token;
5. `ip_api_pro_https`, requiring an encrypted paid HTTPS API key.

Remote providers are disabled by default. Enabling one requires owner role,
recent reauthentication, and an explicit acknowledgement that normalized public
IP addresses will be sent to that named third party. Beat sends only the IP;
node ID, name, group, host label, metrics, and account metadata are never part
of the provider request.

The insecure free HTTP ip-api endpoint is not supported. Arbitrary provider
URLs or executable provider plugins are not accepted. Additional providers are
native reviewed Go implementations behind the same interface and tests.

### Accuracy and last-known-good behavior

GeoIP is an approximation. APIs and UI display the provider, database build or
provider revision, resolution time, and stale state to administrators. Public
responses expose only scope, country code, and resolution time.

A refresh failure preserves the last known good country, records a sanitized
error code, and marks it stale after 90 days. It never replaces a valid country
with `Unknown` because a provider is temporarily unavailable.

## Privacy and source storage

The normalized location input is encrypted with AAD binding node ID, source
mode, and schema version. A separate HMAC-SHA-256 fingerprint, derived with the
domain-separated wrapped-key mechanism in
[`application-secret-lifecycle-design.md`](./application-secret-lifecycle-design.md),
detects source changes and keys the cache without exposing the IP in SQLite
indexes.

Plaintext source IPs or hostnames never appear in GeoIP tables, metrics, audit
values, provider error messages, or structured GeoIP logs. Existing node host
visibility remains governed by current public IP settings; the location feature
does not broaden it.

Disabling location hides it immediately. The owner chooses whether to retain
last-known-good private results for later re-enablement or purge source
ciphertext, cache entries, and derived location. Remote-provider consent changes
are audited.

## Local MMDB lifecycle

Beat does not redistribute a GeoLite database. The owner can:

- upload a licensed country MMDB;
- configure official MaxMind account credentials and request an update from the
  fixed vendor endpoint;
- mount a read-only external MMDB path through reviewed deployment settings.

The external-path option changes the deployment/environment contract. Exposing
it in administration does not authorize the mount itself: its root deployment
setting and runtime filesystem access require explicit implementation and
deployment approval.

`github.com/oschwald/maxminddb-golang/v2` is the planned parser. Upload/update
uses this pipeline:

1. owner role and recent reauthentication;
2. request body and archive maximum 128 MiB;
3. staging directory `0700` and files `0600`;
4. fixed HTTPS vendor host, no redirects outside that origin, bounded
   deadlines, and no proxy-environment inheritance;
5. vendor SHA-256 sidecar verification for official downloads;
6. archive traversal, duplicate, link, device, compression-ratio, and entry
   count checks;
7. exactly one regular MMDB whose metadata reports a supported Country edition;
8. open and sample lookup before publication;
9. fsync file, atomic rename, fsync directory, then swap the live reader;
10. retain one previous database for immediate rollback.

Failures leave the current reader and file untouched. Reader replacement uses a
short lock only for pointer swap; download, extraction, hashing, and validation
occur outside the lock. Update state, build epoch, checksum, size, source, and
sanitized failure code are persisted.

The read-only external-path mode is never copied or chmodded by Beat. Readiness
is degraded when that configured file is missing or invalid.

## Resolution scheduling and cache

Agent ingestion only records a changed encrypted source and schedules work; it
never waits for a remote GeoIP call. Local MMDB lookup may run inline only when
it remains below a measured latency budget, otherwise it uses the same worker.

`node_locations` contains a durable lease and next-refresh time. Workers claim
rows transactionally, resolve outside the transaction, and complete only with a
matching random lease token. Expired leases recover at startup. Defaults:

- local MMDB: refresh after database revision change or source change;
- remote provider: source change or 30 days with deterministic jitter;
- retry: 5 minutes, 30 minutes, 2 hours, 12 hours, then daily;
- maximum five remote requests per second globally and one per provider host;
- maximum eight workers, with remote calls further rate-limited;
- 10-second request timeout and 32 KiB response limit.

`geoip_cache` is keyed by provider revision and source fingerprint. It stores
only scope/country/result metadata, never plaintext source. Successful entries
expire after 30 days, negative entries after one hour, and an LRU maintenance
job enforces a hard 100,000-entry limit. Manual locations do not enter cache.

Changing provider or installing a new MMDB marks eligible automatic locations
due in bounded batches. The public dashboard and Agent ingestion continue when
providers are unavailable.

## Node lifecycle model

Lifecycle metadata is optional and separate from operational node health.
Expiry never disables monitoring, hides a node, revokes an Agent credential, or
stops collection unless a future owner-approved policy explicitly adds that
behavior.

Fields:

- provider and plan name, each 100 UTF-8 bytes maximum;
- exact non-negative `price_micros`, representing one millionth of a currency
  unit, and uppercase three-letter currency code;
- billing interval unit `day`, `month`, or `year`, count 1-120;
- date-only `expires_on` in ISO `YYYY-MM-DD`;
- renewal mode `manual` or `calendar_auto_advance`;
- calendar anchor month/day and end-of-month behavior;
- private notes, 2 KiB maximum;
- independent public visibility for expiry and price;
- creator/updater and timestamps.

`price_micros` avoids binary floating-point corruption and supports currencies
or service credits with more precision than standard minor units. Formatting
uses the configured currency code and removes insignificant trailing digits.

An absent `expires_on` means no tracked expiry. It is displayed as `No expiry`,
not as a far-future sentinel date.

### Date semantics

Lifecycle dates are contractual calendar dates, not timestamps. The site owns
an IANA lifecycle timezone, defaulting to UTC. A node is:

- `scheduled` when its expiry is after the current local date;
- `due_today` when equal;
- `expired` when before;
- `untracked` when absent.

Date-only storage avoids DST and browser-timezone drift. APIs include
`expires_on`, derived signed `days_remaining`, and status. The Server remains
authoritative; the browser may update display at local midnight but refreshes
from the API to confirm state.

Month/year advancement preserves the original anchor. An end-of-month contract
stays at the end of each target month. A February 29 annual anchor uses February
28 in non-leap years and returns to February 29 in leap years. Day intervals use
calendar-day addition in the lifecycle timezone.

### Renewal modes

`manual` never changes a date automatically. An administrator records the new
expiry after confirming renewal with the provider.

`calendar_auto_advance` means only that Beat advances the tracked calendar
date. It does not charge a card, contact a vendor, prove payment, or renew an
external service. UI and notification text use `Date advanced` rather than
`Payment completed`.

The daily scheduler advances a due/expired date repeatedly until it is in the
future, capped at 120 intervals. It does not depend on node online status. The
old date, new date, skipped interval count, actor `scheduler`, and deterministic
period key are committed in one immediate transaction. A unique constraint
makes restart or concurrent scheduler execution idempotent.

## Lifecycle events and notifications

Default reminder stages are 30, 14, 7, 3, 1, and 0 days before expiry, plus 1
and 7 days after expiry. The owner may select a sorted unique set from 0-365 and
disable post-expiry reminders.

Each stage creates one `node_lifecycle_event` keyed by node, expiry date, event
kind, and stage. The same immediate SQLite transaction creates the deterministic
`notification_messages` row and every delivery row selected by the installed
notification-routing rules. If no channel subscribes, the transaction records
the lifecycle event and its explicit `not routed` result without creating a
delivery. Events are:

- `node.expiring`;
- `node.expired`;
- `node.renewed` for a manual change or calendar advancement.

Editing an expiry date creates a new period key and does not reuse reminders
from the old date. Moving a date farther away resolves the old period without
deleting its audit evidence. Reducing lead stages never deletes already-sent
events.

Reminder delivery uses selected notification channels, durable retry, event
routing, templates, and dead-letter history from
[`notification-provider-breadth-design.md`](./notification-provider-breadth-design.md).
If no channel subscribes, the lifecycle event remains visible in administration
with `not routed` status.

## Persistence and migration

This batch uses canonical SQLite migration `v7` and introduces backup archive
format `v3`, after notification delivery `v6` and theme backup format `v2`.
The assignments are maintained in
[`commercial-parity-implementation-sequence.md`](./commercial-parity-implementation-sequence.md).

### `geoip_settings`

- singleton provider mode, revision, enabled/consent state, and refresh policy;
- canonical non-secret provider config and encrypted credential document;
- MMDB source, build epoch, checksum, size, update state, and previous version;
- remote rate limits and update timestamps.

### `node_locations`

- node ID primary key and source mode;
- encrypted normalized source and HMAC fingerprint;
- source scope, country code, provider/revision, and manual flag;
- public visibility override: `inherit`, `public`, or `private`;
- last success/attempt, stale state, sanitized error code, and next refresh;
- lease token hash, lease expiry, and worker ID.

### `geoip_cache`

- provider/revision and source fingerprint primary key;
- scope, country code, success/negative result, timestamps, and LRU access time;
- no source plaintext or node relationship.

### `lifecycle_settings`

- singleton lifecycle timezone;
- reminder lead/post-expiry day sets;
- scheduler claim date/status and update time;
- global default public visibility for expiry, price, and country.

### `node_lifecycle`

- node ID primary key;
- provider, plan, price micros, currency, interval unit/count;
- expiry date, renewal mode, anchor fields, and private notes;
- expiry/price public visibility overrides;
- created/updated actor and timestamps.

### `node_lifecycle_events`

- immutable event ID, node ID, kind, expiry period key, and stage;
- old/new expiry dates, skipped interval count, actor, and timestamp;
- unique deterministic key and notification message relationship.

Foreign keys cascade location and current lifecycle metadata when a node is
deleted. Lifecycle events follow the same retained operational-history policy as
notification evidence and use a tombstoned node display name when necessary.

Migration backfills no inferred country, price, or expiry. Existing nodes start
with `observed` location mode but are scheduled only when GeoIP is enabled; all
lifecycle fields remain untracked. No Agent metric is copied into SQLite.

## Backup format v3 and recovery

Backup format v3 retains format v2 payloads and may add:

- `geoip/country.mmdb` for the managed live local database;
- `geoip/country.mmdb.previous` when rollback retention is enabled;
- manifest metadata for edition, build epoch, size, and checksum.

External read-only MMDB paths are not copied; the manifest records the required
mount path and readiness remains degraded until it is restored. Provider tokens
and encrypted location sources remain inside SQLite, use the `v5` wrapped-key
registry, and are authenticated against the backed-up root key during archive
validation.

Restore validation applies the same archive bounds, verifies payload checksums,
opens each MMDB, checks supported edition metadata, stages files as `0600` under
a `0700` directory, and atomically publishes them before Server startup.
Expired GeoIP leases reset to due; manual locations are untouched. Lifecycle
scheduler state is reconciled by deterministic date/event keys, so restore may
re-evaluate a stage but cannot create a duplicate SQLite event or outbox row.

Migration v7 and backup v3 are forward-only. Rollback requires the pre-migration
backup and previous binary.

## API and authorization

New authenticated APIs provide:

- GeoIP settings, provider health, remote consent, MMDB upload/update/rollback,
  and cache statistics;
- node location mode/manual override/refresh;
- lifecycle create/update/clear and lifecycle event history;
- derived expiry summaries and filters.

Public node responses include location and lifecycle only when allowed by both
global and per-node visibility. Country visibility is evaluated independently
from the existing `show_ip_addresses` policy: hiding an IP does not reveal or
hide country by implication, and exposing country never exposes the source IP.
Public responses never include source input, fingerprint, provider credential,
provider error, private notes, scheduler state, or price when price visibility
is private.

Authorization:

- owner plus recent reauthentication: enable/change remote provider, provider
  secret, MMDB upload/update/rollback, external path, privacy purge, global
  visibility, or lifecycle timezone;
- owner/admin: change a node's manual location, managed-target mode, lifecycle
  metadata, reminder routing, or issue a bounded refresh;
- public: read only the sanitized fields explicitly enabled.

Every sensitive mutation records actor, node/setting ID, changed field names,
old/new non-secret state, consent change, and outcome. Audit never contains
source IP/hostname, credential, price notes, or provider response body.

## Administration and public UI

The design follows the existing compact shadcn/ui operations workspace.

`/admin/settings` gains an unframed **Location** section with provider mode,
privacy state, MMDB health/build, last update, remote request budget, and clear
upload/update/rollback commands. Remote modes show a persistent third-party
disclosure banner, not a transient toast.

The node editor gains **Location** and **Lifecycle** field groups:

- location mode segmented control, country select with localized labels, source
  status, last update, stale/error state, refresh, and visibility select;
- provider/plan, exact price and currency, interval controls, expiry date,
  renewal-mode switch, reminder preview, public expiry/price visibility, and
  private notes.

Country, currency, visibility, and interval selects always show labels rather
than IDs or enum values. Destructive clear/purge and calendar auto-advance
enablement use explicit confirmation. No nested cards are introduced.

Admin node cards show a compact country/scope label and lifecycle status only
when configured. A toolbar filters `Expired`, `3 days`, `7 days`, `30 days`,
country, provider, and renewal mode, and can sort by expiry or price.

Public grouped node cards may show country flag/code, expiry status, remaining
days, price/interval, and the same expiry/country filters when site and node
visibility allow them. They update through existing background data refresh and
never force a full-page reload. Hiding public IP does not automatically hide an
explicitly public country, but the settings UI explains that independence.

## Observability and maintenance

Low-cardinality metrics include:

- GeoIP resolution attempts, cache hit/miss, outcome, and provider mode;
- due/leased/stale location counts and oldest due age;
- MMDB build age, update success/failure, and rollback count;
- nodes by lifecycle status;
- lifecycle events by kind and calendar advancements;
- scheduler last success and duration.

No IP, hostname, fingerprint, node ID, country code, provider response, plan,
or price is a metric label. Logs use provider mode, result code, duration,
cache state, and opaque job/event ID.

Maintenance removes expired cache entries in bounded batches, enforces the hard
cache limit, retains only current/previous managed MMDB files, and deletes
lifecycle events according to operational-history retention without deleting
current metadata or unsent notification work.

GeoIP provider failure does not make the public dashboard or Agent ingestion
unready. Readiness is degraded for an enabled local provider with no valid live
database, a stuck source-secret migration, or an unreadable location queue.

## Test and verification gate

Implementation is not accepted until all of the following pass:

1. exhaustive IPv4/IPv6 classification tests covering loopback, private, ULA,
   link-local, CGNAT, documentation, benchmark, multicast, unspecified,
   IPv4-mapped IPv6, zones, and malformed input;
2. trusted/untrusted proxy tests proving only the canonical request source can
   drive observed location and Agent `host` cannot override it;
3. provider contract tests for local MMDB, GeoJS HTTPS, IPinfo HTTPS, and paid
   ip-api HTTPS, including timeouts, status, response bounds, malformed JSON,
   quota failures, redaction, and no remote private-IP requests;
4. source encryption/AAD, HMAC cache key, wrong-key, no-plaintext SQLite/log/API,
   consent, purge, manual override, last-known-good, stale, cache/LRU, lease,
   restart, and rate-limit tests;
5. MMDB upload/download tests for permissions, official checksum, wrong edition,
   corrupt/truncated data, traversal, duplicate/link/device entries, zip/tar
   bombs, fsync/rename failure, concurrent lookup/update, rollback, and retained
   live reader;
6. lifecycle tests for exact price parsing/formatting, invalid currency and
   intervals, UTC and DST boundaries, month end, leap day, long downtime,
   120-interval cap, concurrent scheduler, manual renewal, and idempotent event
   keys;
7. notification tests for every lead/expired stage, route absence, edit/reset,
   retry/dead-letter integration, and no duplicate event after restart/restore;
8. public/private API and frontend tests proving location/price/expiry redaction,
   readable select labels, 3/7/30/expired filters, sorting, background refresh,
   mobile layout, keyboard use, and accessibility;
9. migration v7 and backup v3 create/upload/restore/rollback tests, SQLite
   integrity, MMDB payload validation, external mount readiness, and MTS
   isolation;
10. Go statement and frontend line coverage at or above 90%, `go test -race`,
    repository tests, frontend tests/build, `goimports-reviser`, and
    `golangci-lint`;
11. a 10,000-node refresh/load test with bounded memory, cache size, workers,
    provider rate, restart recovery, and no Agent-ingestion latency regression;
12. pre-deployment backup, IPv4/IPv6 public/admin authorization smoke tests,
    controlled local-MMDB and opt-in remote-provider acceptance, file permission
    inspection, and rollback rehearsal.

## Approval boundary

Implementation changes shared node API/types, SQLite schema, trusted request
data use, external provider behavior, dependencies, background schedulers,
public presentation, notification routing, backup format, environment settings,
and deployment assets. It therefore requires explicit approval and must follow
the notification delivery batch.

Approval text:

```text
批准按 geoip-node-lifecycle-design.md 完整实施 GeoIP 与节点生命周期批次
```
