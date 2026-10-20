# Theme packages and market

Updated: 2026-09-14

## Goal and scope

Beat will support installable, exportable, previewable, versioned public-theme
packages and checksum-verified remote markets while preserving the current
trusted administrator interface. The result matches the useful theme outcomes
available in Komari without allowing a downloaded theme to inherit Beat's
administrator origin, cookies, APIs, or filesystem authority.

Themes apply only to the unauthenticated public dashboard and public node
detail experience. `/admin`, authentication pages, operational endpoints, and
administrator dialogs always use Beat's built-in shadcn interface. Theme
failure must never make administration, health checks, or rollback unavailable.

This batch includes:

- local ZIP upload, validation, install, export, delete, and side-by-side
  versions;
- live preview, explicit activation, previous-version rollback, and automatic
  built-in fallback;
- declarative themes rendered by trusted Beat components;
- isolated bundle themes rendered in a sandboxed public iframe;
- configurable HTTPS market sources, signed catalogs, checksummed packages,
  update discovery, and explicit installation;
- backup/restore, audit, readiness, metrics, and deployment coverage.

It does not allow server-side templates, Go plugins, shell commands, arbitrary
filesystem paths, theme code in administrator pages, automatic installation or
activation, unsigned remote catalogs, or runtime access to third-party
networks.

## Competitor audit

The Komari source inspected on 2026-07-30 exposes theme ZIP upload/import,
managed/raw/redirect theme modes, remote update, configurable market sources,
preview metadata, catalog caching, SHA-256 package verification, and install
from market.

Beat will not copy the following unsafe implementation properties:

- raw theme HTML and scripts sharing the application's authenticated origin;
- whole-body uploads or downloads without a strict request limit;
- fixed temporary filenames shared by concurrent requests;
- package replacement that deletes the active version before the new version
  is fully validated and committed;
- archive-provided file modes, unrestricted file types, or large extraction
  limits;
- URL validation followed by a separate default-client DNS lookup, which
  leaves DNS rebinding and redirect gaps;
- ignored download errors or upstream errors returned directly to clients;
- remote catalogs trusted only by transport and an author-supplied checksum;
- no durable activation history or one-command rollback.

## Theme package format

A package is a ZIP archive with `beat-theme.json` at its root. Every archive is
identified by the SHA-256 digest of the exact uploaded bytes. Manifest format
version 1 is strict JSON with unknown fields rejected.

Required manifest fields:

- `schema_version`: `1`;
- `id`: lowercase URL-safe identifier, 3 to 64 characters;
- `name`, `description`, `version`, `author`, and `license`;
- `runtime`: `declarative` or `sandboxed`;
- `minimum_beat_version` and optional `maximum_beat_version`;
- `preview`: package-relative PNG, JPEG, or WebP path;
- `files`: sorted inventory containing path, byte size, and SHA-256 for every
  non-manifest file.

Optional fields include homepage URL, release notes, supported locales, and a
`configuration` block declaring which display fields the site owner may adjust
(see below). Versions use canonical semantic versioning. An
installed `(theme_id, version)` is immutable; an archive with the same identity
and different digest is rejected instead of silently replacing it.

### Declarative runtime

Declarative packages contain strict `theme.json` schema 1 and passive assets. The
implemented configuration supports cards/table/tiles/list overviews, balanced/compact/
spotlight details, comfortable/compact density, a 0–8 px radius, validated light/
dark color tokens, an ordered `resources` subset (cpu/memory/disk), the content
switches `show_network`/`show_host`/`show_tags`/`show_remark`, and an optional
`font` token (`system`/`serif`/`mono`). Unknown fields,
CSS selectors, arbitrary component names and scripts are rejected. Owner overrides
are stored in the existing `configuration_json` field and accept only the keys the
package declares, so content changes require a new package version.
The [author guide](theme-author-guide.md) is the complete supported schema.

#### Declared configuration (`configuration`)

A declarative package may declare `"configuration": {"type": "managed", "items": [...]}`
so the owner sees the relevant controls instead of a fixed four-field form. Items
are `{key, name?, options?}`, at most 12, without duplicates. Keys are limited to
the enum/number/switch fields the renderer already implements: `overview`,
`detail`, `density`, `font` (enums), `radius` (0–8) and
`show_network`/`show_host`/`show_tags`/`show_remark` (switches). `options` may only
narrow the legal values of an enum key. Field type and legal values are derived from
the key, so authors cannot declare a type, and array fields such as `resources` are
not configurable.

The stored configuration stays an overlay: keys the site did not set fall back to
the package's own `theme.json` values, and `{}` restores them. Manifests without
`configuration` keep the historical four-field behavior, and client-side limits are
optional hints only — the server re-validates every key and value on save.

`raw` (author-supplied configuration HTML) and `redirect` (third-party configuration
page) are deliberately not implemented: both would hand the owner session to the
theme or to an arbitrary site. Configurable fields therefore remain a closed set that
cannot change colors, structure, or code.

### Sandboxed runtime

Sandboxed packages may contain a compiled public UI bundle consisting of HTML,
CSS, JavaScript modules, raster images, JSON, and WOFF2 fonts. The bundle is
rendered only inside a full-size iframe with `sandbox="allow-scripts"` and no
`allow-same-origin`, forms, popups, downloads, top navigation, or storage
authority.

The sandbox has no direct API or network access. Response-level sandbox and CSP
restrict assets to the validated package's digest path, including when the entry
HTML is opened directly. The implemented policy and protocol 2 messages are defined
in [theme-sandbox-contract.md](theme-sandbox-contract.md).

A trusted parent runtime sends only sanitized public site, group, node, metric,
traffic, and quality snapshots through a versioned `postMessage` protocol. The
theme can send allowlisted navigation and readiness messages back. The parent
checks message source, protocol version, route, node ID, payload type, and size.
No administrator principal, cookie, CSRF material, hidden node, private remark,
or suppressed IP address enters the sandbox.

The sandbox cannot register service workers, read Beat storage, access the
parent DOM, or exfiltrate data through network requests. If it does not report
ready within a bounded interval, crashes repeatedly, or violates the runtime
protocol, the trusted shell displays the built-in public theme.

## Archive validation and installation

Validation happens before extraction and before any existing state changes.
Limits for format version 1 are:

- archive body: 25 MiB;
- extracted content: 64 MiB;
- file count: 512;
- manifest: 256 KiB;
- individual code/data file: 8 MiB;
- individual image or font: 12 MiB;
- normalized path length: 240 bytes and depth: 8 segments.

Every entry must be a regular file or directory with a UTF-8, slash-separated,
relative, locally valid path. Beat rejects absolute paths, drive prefixes,
backslashes, empty/dot/parent segments, duplicate or case-fold-colliding paths,
NUL/control characters, symlinks, hard links, devices, sockets, FIFOs, nested
archives, encrypted ZIP entries, data descriptors with inconsistent sizes, and
compression ratios above the configured bomb threshold.

Allowed file extensions are exact and runtime-specific. Content signatures and
served MIME types must match the extension. SVG, WASM, source maps, executable
files, server templates, hidden files, and unknown types are rejected in format
version 1. HTML is allowed only for a sandboxed entry document. All referenced
bundle paths must exist in the manifest inventory, and external resource URLs,
`<base>`, inline event handlers, form targets, and navigation directives are
rejected during HTML validation.

Installation uses a random `0700` staging directory under the Beat data
directory. Staged files are created as `0600`, synced, rehashed, and then
atomically renamed to a content-addressed package directory. Archive modes and
timestamps are ignored. SQLite metadata is committed only after the final
directory is durable. A failure leaves the current active theme untouched and
cleans only the request's private staging directory.

The immutable on-disk shape is:

```text
<data-dir>/themes/packages/<archive-sha256>/archive.zip
<data-dir>/themes/packages/<archive-sha256>/content/...
```

The built-in theme is compiled with Beat and is not deletable.

## Serving theme assets

Theme files are served by a dedicated handler, not the SPA `http.FileServer`.
The handler resolves an installed digest and an allowlisted manifest path,
opens it through Go's scoped filesystem API, verifies regular-file metadata,
sets an exact MIME type and `X-Content-Type-Options: nosniff`, and never follows
links.

Content-addressed assets use immutable cache headers and digest ETags. HTML uses
no-cache plus the sandbox CSP. Theme files cannot shadow `/api`, `/admin`,
`/healthz`, `/readyz`, `/metrics`, WebSockets, or built-in static assets.

## Persistence model

Theme packages use canonical SQLite migration `v6` after OIDC `v5`, as assigned
by
[`commercial-parity-implementation-sequence.md`](./commercial-parity-implementation-sequence.md).
Metrics and theme runtime samples remain outside SQLite; operational metrics
continue to use the existing Prometheus registry and time-series samples remain
in MTS.

### `theme_packages`

- package UUID, theme ID, name, semantic version, author, description, license;
- runtime type, manifest schema, minimum/maximum Beat version;
- archive digest and byte sizes;
- normalized manifest JSON and managed configuration JSON;
- source type (`upload` or `market`) and nullable market/source identity;
- state (`installed`, `quarantined`, or `deleted`), validation timestamp,
  creator, creation time, and last error category.

Unique constraints cover archive digest and `(theme_id, version)`.

### `theme_activations`

- activation UUID, package ID or built-in marker;
- previous package ID, actor ID, reason (`activate`, `rollback`, `fallback`);
- activation time and optional deactivation time;
- health state and last runtime failure category.

Only one activation is current. Activation and history are updated in one
immediate SQLite transaction. The latest known-good previous package remains
available for rollback.

### `theme_market_sources`

- source UUID, name, HTTPS catalog URL, enabled state, sort order;
- Ed25519 public key and key fingerprint;
- cache ETag, Last-Modified, fetched/expiry timestamps, catalog digest;
- last status, last error category, created/updated times.

Catalog documents are cached as bounded data, not executed. Source mutation is
owner-only and recently authenticated.

The singleton `site_settings` gains the active theme package ID. Public site
settings return a display-safe active-theme descriptor, never filesystem paths,
market keys, internal errors, or package configuration not declared public.

## Activation, preview, rollback, and deletion

Installing never activates. Preview uses the same trusted declarative renderer
or sandbox restrictions as production but loads current sanitized public data
inside an administrator preview surface. Preview produces no global state
change and expires when the dialog/session closes.

Activation requires an owner, recent reauthentication, a compatible installed
package, successful validation, and explicit confirmation. The frontend updates
only after the activation transaction succeeds. Existing visitors may finish
with their current immutable package; new loads use the new activation.

The server continuously protects availability:

- missing/corrupt content, manifest mismatch, iframe timeout, or repeated
  runtime protocol errors trigger the built-in theme for that request;
- request-local sandbox failures fall back without changing global activation;
- stored unhealthy activations and packages with missing/corrupt files are excluded
  from rollback candidates;
- one action rolls back to the most recent healthy package or built-in theme.

An active package cannot be deleted. Deletion checks current activation and the
site pointer inside the same owner-authorized transaction, writes `deleted` and a
resource audit, then moves files to private quarantine. Assets and export immediately
reject `deleted` packages. Completed cleanup returns 204; committed deletion with
pending cleanup returns 202 and `{package_id,state:"deleted",cleanup_pending:true}`.
A repeat DELETE and startup reconciliation retry cleanup. Only owned staging and
content directories are reconciled; unknown paths are preserved.

Reinstalling the exact archive requires complete archive/manifest validation and
restores the same package ID, configuration and provenance. It never activates the
package. A different digest under the same theme ID and version remains a conflict.
Install/restore failures remove the new owned content; interrupted writes are
reconciled at startup. Export verifies the installed archive digest before returning
it with private/no-store headers.

## Remote market security

A market source is an HTTPS catalog plus a Base64 Ed25519 public key. Its public
`source_id` is `sha256:` followed by the lowercase SHA-256 of the raw 32-byte key;
the local database UUID is only a locator. A key change is a new public identity.
Catalog schema 1 requires a future `expires_at` no more than seven days away, at
most 200 unique theme IDs, exact archive/manifest digests and valid compatibility
ranges. Signatures cover the exact downloaded bytes, including whitespace.

The dedicated client ignores proxies, resolves every hop once, rejects any private
or special-use address in the entire DNS answer, and dials only validated numeric
IPs. `BEAT_THEME_MARKET_ALLOW_PRIVATE=true` at server startup turns that address
policy off for self-hosted markets inside offline networks: literal private or
loopback URLs then pass validation and DNS answers are no longer required to be
public. It is an explicit operator opt-in, defaults to off, fails startup on a
non-boolean value, and only relaxes address policy - HTTPS-only, single
resolution, preserved Host/SNI, the three-redirect bound, and the 2 MiB/25 MiB
size limits still apply. Every other security property in this section is
unchanged. It preserves the original Host and TLS/SNI identity. At most three HTTPS
redirects are allowed. Connections, TLS and headers have 10-second bounds; each
request has a 30-second total bound. Catalogs are limited to 2 MiB and archives to
25 MiB. The browser does not load remote catalog preview URLs.

Up to 16 verified catalog bodies are retained in memory, keyed by local source ID,
URL and public key identity. Conditional requests use validators only from a verified
body. A 304 revalidates and returns that full body; database-only validators after
restart cannot produce success. Cache lifetime is at most 15 minutes and never
exceeds signed expiry. A network failure can return a still-valid last-known-good
catalog with explicit `stale` and `error_category`; signature/schema failures clear
the cache. Refresh always contacts the source, and URL/key/enabled changes invalidate
cached data and observations.

Installation binds the client's selected version and archive digest before download,
then verifies the raw archive and raw `beat-theme.json` digests plus theme ID,
version, runtime and compatibility range. The final install transaction rechecks
source enabled state, URL, key, catalog digest and expiry, along with the live owner
account, session and recent authentication. Source changes during download cannot
commit a package. There is no unattended update or activation and no new C09 schema.

The [publisher guide](theme-market-publishing.md) documents `beat-theme market
keygen`, `identity` and `publish`, which generate this format using the production
validator. Signing keys are kept outside the public artifact directory.

## API and authorization

Public routes:

- `GET /api/v1/themes/active`: active display descriptor and runtime protocol
  version;
- immutable theme asset routes under a dedicated `/themes/assets/` prefix.

Owner-only theme routes; every mutation also requires recent reauthentication:

- `GET /api/v1/admin/themes`;
- `POST /api/v1/admin/themes/upload`;
- `GET /api/v1/admin/themes/{id}/export`;
- `PUT /api/v1/admin/themes/{id}/configuration`;
- `POST /api/v1/admin/themes/{id}/activate`;
- `POST /api/v1/admin/themes/rollback`;
- `POST /api/v1/admin/themes/built-in`;
- `DELETE /api/v1/admin/themes/{id}`.

Owner-only market routes with recent reauthentication on mutations/install:

- `GET/POST /api/v1/admin/theme-markets`;
- `PUT/DELETE /api/v1/admin/theme-markets/{id}`;
- `GET /api/v1/admin/theme-markets/{id}/catalog` with forced, bounded refresh;
- `POST /api/v1/admin/theme-markets/{source_id}/themes/{theme_id}/install`,
  with required `{version,package_sha256}`. A changed selection/source returns 409
  and requires refresh and explicit reselection; missing selection fields return 400.

All HTTP mutations bind a real owner/session to a transaction-level authority check.
Resource-specific audit insertion is atomic with package/configuration/activation or
source changes; the existing HTTP access audit remains separate. Missing recent proof
returns 428 for the same confirmed operation to retry after local reauthentication.

Upload uses an exact binary body/content type and `http.MaxBytesReader` before
temporary storage. APIs return provider/theme names and versions instead of
internal IDs in display fields, while IDs remain stable command parameters.

## Frontend behavior

The current Settings page keeps site identity, branding, color-mode default,
and public-display controls. It adds a full-width `Themes` section designed as
an operational library rather than a marketing gallery:

- segmented `Installed` and `Market` views;
- compact theme rows/cards with preview image, name, author, version, runtime
  type, compatibility, source, active/healthy state, and update availability;
- icon actions for preview, export, activate, rollback, update, and delete with
  tooltips;
- upload and market-source dialogs using existing shadcn fields and recent-auth
  dialog patterns;
- a side-by-side preview surface with desktop/mobile viewport controls, whose
  display-settings form renders exactly the fields the package declared
  (enum selects, integer input, switches) and never undeclared keys;
- distinct runtime labels, verified/stale catalog status and explicit cleanup retry;
- names in selects and confirmation text, never raw package/source IDs.

The built-in theme stays visible as a recovery choice. Long URLs and public-key identities wrap within their source card; text never overflows. Status uses icon plus text,
keyboard focus is visible, reduced motion is respected, and mobile actions wrap within the card. No theme package can restyle this administrator UI.

## Backup, restore, and operations

Backup format version 2 adds immutable theme archives/content and theme metadata
to the existing SQLite/MTS/key backup. The manifest records each theme file's
path, size, and SHA-256. Restore validates the same package rules, extracts into
a private staging root, and swaps SQLite, MTS, key, and theme data through the
existing restart-safe restore journal. A missing theme payload for an active
package makes validation fail instead of restoring a broken activation.

Readiness gains a `themes` check covering root permissions, active package
metadata/content consistency, and built-in fallback availability. Prometheus
adds installed bytes/count, activation health, sandbox fallback totals, market
refresh totals, and validation failures without theme/author labels that could
create unbounded cardinality.

All theme directories are `0700` and files `0600`. The existing rootless,
read-only container remains valid because theme data lives under the writable
Beat data directory, not `/opt/beat/static`.

Audit records cover upload, install, export, configuration, preview failure,
activate, fallback, rollback, delete, market source changes, catalog refresh,
and market install. Details are allowlisted and exclude archive bytes, file
content, URLs with credentials, and raw upstream/parser errors.

## Test strategy and quality gates

Backend tests cover:

- manifest strictness, compatibility, semantic versions, file inventories,
  MIME/magic matching, and declarative schemas;
- traversal variants, Unicode/case collisions, links/devices, ZIP bombs,
  duplicates, malformed sizes, file/depth/count/body limits, and interrupted
  extraction;
- `0700`/`0600` permissions, atomic installation, concurrent same-package
  installs, immutable version conflicts, quarantine, cleanup, and export;
- sandbox headers/attributes/CSP, asset routing, no API/admin shadowing,
  postMessage origin/source/schema/size checks, timeout, and built-in fallback;
- owner/recent-auth boundaries, audit redaction, activation transactions,
  health thresholds, rollback, and active-delete conflict;
- market signature, key rotation, digest/manifest mismatch, SSRF/DNS rebinding,
  redirect/TLS/timeout/size controls, cache validators, and cancellation;
- migration idempotence, backup format 1 compatibility, format 2 theme
  round-trip, corrupt/missing active assets, and restart-safe restore.

Frontend tests cover installed/market names instead of IDs, upload, preview,
viewport controls, activation, fallback status, rollback, update discovery,
configuration forms, source management, confirmations, recent-auth behavior,
overflow, keyboard use, and responsive layouts. Sandboxed runtime protocol tests
use a real iframe-capable browser suite in addition to component tests.

Completion requires at least 90% Go statement coverage and 90% frontend line
coverage plus race/shuffle, `go vet`, `goimports-reviser`, `golangci-lint`,
`govulncheck`, module verification, frontend audit/test/lint/build, browser
screenshots, container build, backup/restore rehearsal, and deployed IPv6 smoke
tests. Agent reporting must continue throughout the Server-only cutover.

## Deployment and rollback

1. Stop only Beat Server and create a verified stopped-service backup of the
   binary, built-in static assets, SQLite/WAL/SHM, MTS, keys, and data directory.
2. Deploy the consecutive schema migration, backup format 2 support, theme
   runtime, and matching frontend.
3. Verify built-in public/admin behavior before installing any package.
4. Upload and preview a declarative package and a sandboxed test package;
   activate each, verify public/node routes and fallback, then roll back.
5. Configure a signed market source, refresh, install by digest, preview, and
   explicitly activate.
6. Verify readiness, metrics, audit, backup/restore, public access, protected
   administration, Agent reports, and both published IPv6 addresses.

Immediate feature rollback activates the built-in theme. Binary rollback after
the schema/backup-format migration requires the pre-migration SQLite and data
backup because the old Server rejects a newer schema and cannot restore theme
payloads.

## Approval boundary

Implementation requires explicit approval for this complete batch:

1. Canonical SQLite migration `v6` with theme package, activation,
   market-source, and active-site-theme persistence.
2. A new writable `<data-dir>/themes` hierarchy and dedicated theme asset and
   sandbox runtime handlers.
3. New public active-theme/asset routes and owner-only theme/market APIs.
4. Settings-page theme library/market behavior and sandboxed public rendering.
5. Backup archive format version 2 and restore changes that include theme data.
6. HTTPS-only, Ed25519-signed remote markets, no automatic update/activation,
   declarative themes by default, and executable bundles only in the isolated
   no-origin/no-network iframe.

No schema, API, filesystem, backup-format, frontend, dependency, production, or
deployment change is authorized by this design document alone.
