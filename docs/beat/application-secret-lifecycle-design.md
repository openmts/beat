# Application secret lifecycle

Updated: 2026-07-30

Status: reviewed shared contract; compatibility envelope begins in SQLite
migration `v2`, full lifecycle is owned by `v5`, and `v6+` reuse it

## Decision

Beat will have one application-secret system rather than separate encryption
code for TOTP, OIDC, SSH, notifications, commands, and future credentials.
Migration `v2` introduces the minimum versioned root-key envelope needed by its
OIDC client secret and PKCE verifier. Migration `v5` evolves that same decoder
and AAD registry into the wrapped data-key registry, rotation, readiness, and
complete backup/restore validation. Migration `v6` and later migrations consume
that primitive and may not create a parallel key file, cipher format, or
rotation path.

This contract does not consume another migration number or backup-format
version. The next unassigned SQLite migration remains `v20`, and the planned
backup sequence remains `v1-v4`. From `v5`, the SQLite snapshot contains wrapped
data keys, while the existing `secrets/admin-data.key` archive entry continues
to contain the matching 32-byte root key.

## Current evidence

The deployed baseline has three different storage properties:

- administrator passwords, administrator session tokens, and per-node Agent
  tokens are one-way hashes and have no plaintext recovery path;
- TOTP is AES-256-GCM encrypted by `internal/secretbox`, but its current
  `nonce || ciphertext` form has no envelope version, key ID, or AAD;
- SSH private keys and Telegram, SMTP, Webhook, and other channel credentials
  remain plaintext inside SQLite.

Reviewed but unimplemented migration `v2` also needs recoverable OIDC provider
client secrets and short-lived PKCE verifiers. Because it precedes `v5`, it must
use the compatibility envelope defined below rather than today's nil-AAD format
or an OIDC-specific cipher.

`ssh_keys.private_key` is selected by ordinary SSH-key list queries even though
the JSON model excludes it. `alert_channels.config` is sanitized only when an
API response is built; the database and delivery service still hold the full
credential-bearing value. API redaction therefore does not provide encryption
at rest.

The deployed backup includes SQLite and, when present, the 32-byte root key.
Validation checks the key's size and checksum but does not prove that it can
decrypt TOTP or future SSH/notification ciphertext. A missing or unrelated
32-byte key can therefore pass archive validation and fail only after restore.

## Security properties

The shared system must provide:

1. AES-256-GCM with a fresh `crypto/rand` nonce for every encryption;
2. a versioned, strictly decoded envelope with an explicit data-key ID;
3. canonical, resource-bound AAD so row, field, type, or revision substitution
   fails authentication;
4. one active data key and bounded decrypt-only predecessors;
5. fail-closed startup and runtime behavior for missing keys, unknown versions,
   invalid AAD, corrupt ciphertext, incomplete backfill, or interrupted
   rotation;
6. full backup validation of the SQLite/root-key/ciphertext relationship;
7. no plaintext fallback, silent field clearing, secret logging, or MTS use.

Encryption protects a copied SQLite database when the root key is not also
obtained. It does not protect a live privileged process, a host compromise, or
a Beat backup containing both SQLite and its root key. Backups are recoverable
credential bundles and require `0600` storage on an encrypted volume or an
independently encrypted/KMS-wrapped external backup system.

## Root key and data-key registry

`<data-dir>/admin-data.key` remains exactly 32 random bytes. It is a root
key-encryption key, not the direct long-term key for every application secret.
The data directory is `0700`; the key is a regular non-symlink file with `0600`
mode. Startup never follows a symlink, accepts another file type, or silently
generates a replacement when encrypted references or a restore/rotation marker
exist. Key bytes are never exposed through HTTP, logs, audit, metrics, process
arguments, or environment templates.

Before `v5`, envelope format 1 introduced by migration `v2` encrypts directly
with this root key and names the root-key fingerprint as its key ID. It is
accepted only for the registered OIDC resource profiles and is explicitly
migrated by `v5`.

Migration `v5` adds `application_data_keys` with random key ID, wrapped 32-byte
data key, state (`active` or `decrypt_only`), creation/activation timestamps,
and retirement metadata. The root key wraps each data key using AES-GCM and AAD
binding the key ID, wrapping format, and schema generation. Constraints permit
exactly one active key. A separate bounded backfill/rotation state records the
registered resource cursor and phase without secret material.

New writes resolve the active data key inside the same request lifetime. Reads
resolve only the envelope's referenced key. Unknown or retired-without-material
IDs fail closed; they never try every key and never interpret ciphertext as
plaintext.

## Ciphertext and AAD contract

Envelope format 2, introduced by migration `v5`, is a canonical binary
structure, not ad hoc string parsing. It contains magic, envelope version,
algorithm ID, data-key ID, nonce, and sealed ciphertext. Decoders reject unknown
fields, non-canonical lengths, trailing bytes, oversized values, and unsupported
versions before invoking AES-GCM.

AAD is a length-prefixed tuple with a fixed Beat domain, resource namespace,
row primary key, field name, immutable revision where applicable, and payload
schema version. Registered profiles include:

| Resource | Canonical binding |
| --- | --- |
| TOTP | `admin_users`, user ID, `totp_secret`, payload `v1` |
| OIDC provider | provider ID, `client_secret`, configuration version, payload `v1` |
| OIDC transaction | transaction ID, provider ID, `pkce_verifier`, payload `v1` |
| SSH | `ssh_keys`, key ID, `private_key`, payload `v1` |
| Snippet/task/output/path | owning operation ID, field, immutable version or attempt, payload version |
| Notification channel | channel ID, revision, channel type, secret document, schema version |
| Notification message | message ID, encrypted field, payload schema version |

Every future encrypted column must register its profile, maximum plaintext and
ciphertext size, migration/backfill owner, rotation enumerator, backup validator,
redaction tests, and deletion/retention rule before schema approval.

Legacy nil-AAD TOTP is accepted only by an explicitly typed legacy decoder. It
is never accepted by the generic decrypt path. Format 1 root-key envelopes from
migration `v2` and format 2 wrapped-data-key envelopes from `v5` have strict
registered decoders. A successful `v5` backfill or bounded read-repair rewrites
legacy TOTP using user-bound AAD; ciphertext copied between administrators then
fails authentication.

## Migration and backfill ownership

The existing startup order applies schema migrations before `secretbox` exists.
Therefore migration callbacks only create tables, columns, constraints, and a
pending marker. After root-key initialization and before the HTTP listener or
workers start, a dedicated coordinator performs secret backfill in immediate
transactions.

Migration `v2` must extend `secretbox` with the strict compatibility envelope
and resource-bound AAD, encrypt OIDC provider secrets and PKCE verifiers through
that API, and extend backup validation to authenticate every archived OIDC
ciphertext with the archived root key. It must never persist those fields in the
legacy nil-AAD form. Temporary transaction cleanup remains bounded and expired
transactions may be removed rather than migrated later.

Migration `v5` must:

1. create the wrapped data-key registry and one active data key;
2. convert every durable TOTP ciphertext and every retained `v2` OIDC envelope
   to the wrapped-data-key envelope with unchanged resource-bound AAD;
3. rebuild SSH storage so private keys are ciphertext, migrate every legacy
   private key, and prove no plaintext value remains;
4. make SSH list operations metadata-only and decrypt one selected private key
   just in time for an authorized terminal connection;
5. use the same primitive for operation snippets, task payloads, output, paths,
   fingerprints, and spool key derivation;
6. mark backfill complete only in the transaction that proves all registered
   rows are converted.

Any parse, encryption, affected-row, commit, or verification error leaves the
marker incomplete and prevents readiness and serving. Restart resumes from
durable state without treating a format 2 envelope as legacy plaintext.

Migration `v6` parses each legacy notification configuration, splits canonical
public configuration from the complete secret document, creates immutable
revision 1, encrypts it with the active data key and channel/revision AAD, clears
the old plaintext, and marks its own backfill complete in one immediate
transaction. Blank updates preserve the previous secret; only an explicit
owner/recent-auth clear action removes it.

## Rotation

Data-key rotation is online and restart-safe:

1. an owner with recent authentication requests rotation through the central
   privileged-operation envelope;
2. one immediate transaction inserts a new wrapped active data key, demotes the
   prior key to decrypt-only, and creates a rotation generation/cursor;
3. new writes immediately use the new key while bounded batches re-encrypt every
   registered ciphertext with unchanged canonical AAD;
4. each batch checks affected rows and advances its cursor atomically;
5. completion performs a full reference scan before the old key can enter its
   rollback-retention period and later be removed.

Backup, restore staging, another rotation, and schema migration cannot overlap
rotation. Runtime decrypt failure does not trigger a new key, clear a field, or
fall back to plaintext. Readiness reports a sanitized degraded rotation state;
missing referenced key material or an invalid registry is unready.

Root-key rotation is a stopped-service local operation because SQLite and a
filesystem key cannot commit atomically. It acquires the shared local lifecycle
lock, validates and decrypts every wrapped data key, creates a `0600` next-key
file and phase journal, rewraps all data keys in one SQLite transaction, fsyncs
both sides, atomically publishes the new root key, verifies all registered
ciphertexts, and removes the journal. Startup uses the journal to finish or
restore the old pairing after any injected crash. It never serves while the
pairing is ambiguous.

## Backup and restore

Backup holds the shared lifecycle lock long enough to snapshot a coherent
SQLite registry/ciphertext generation and copy its stable root key. Readers
derive the non-secret fingerprint, key-registry generation, envelope versions,
referenced data-key IDs, and backfill/rotation completeness from the existing
payloads, so no manifest or archive-format change is required. A missing root
key is allowed only when SQLite proves there are no wrapped keys or encrypted
resources.

Archive validation loads the archived root key, unwraps every referenced data
key, and scans every registered ciphertext in bounded batches using reconstructed
AAD. Missing/extra keys, unknown IDs, legacy plaintext after the owning backfill,
wrong AAD, corrupt envelopes, incomplete rotation, or an unprovable resource
reference rejects staging. Secret plaintext is discarded immediately and never
included in the validation report.

Startup restore treats SQLite, MTS, root key, manifest-owned files, and the
restore journal as one rollback unit. Validation occurs before activation and
again after swap. A restore can never create a fresh root key beside restored
ciphertext. Rollback restores the matching SQLite/root-key pair, not either
artifact independently.

## Operability and tests

Readiness exposes only bounded states: root key available, registry valid,
backfill complete, rotation idle/running/degraded, and ciphertext validation
healthy. Logs and audit contain resource type, opaque ID, key ID prefix,
generation, phase, count, duration, and stable reason code, never plaintext,
nonce, full key ID, wrapped key, or credential-bearing endpoint.

Acceptance requires tests for:

- legacy TOTP dual-read and conversion, plus row/field/revision swap rejection;
- malformed envelope, wrong/missing key ID, wrong root key, nonce failure, and
  no plaintext/error leakage;
- SSH and notification backfill success, injected rollback/restart at every
  phase, and database assertions proving zero legacy plaintext;
- concurrent writes during data-key rotation, mixed-key reads, cursor resume,
  old-key reference proof, retention, and removal;
- root-key rotation crash recovery at every filesystem/SQLite boundary;
- backup during reporting and rotation, missing/wrong key rejection, full AAD
  validation, activation failure rollback, and previous-format compatibility;
- at least 90 percent backend statement coverage, race/shuffle tests,
  `goimports-reviser`, `golangci-lint`, `go vet`, `govulncheck`, backup/restore
  drills, and deployed IPv4/IPv6 public/admin isolation.

## Approval boundary

This is not an independent implementation batch. Explicit approval of migration
`v2 identity-oidc` includes only the compatibility envelope, OIDC AAD profiles,
and matching backup validation. Explicit approval of `v5 remote-operations`
must also approve the shared key registry, TOTP/SSH/OIDC conversion,
data/root-key rotation, readiness, and complete backup/restore validation.
Approval of `v6` authorizes only the notification consumer and its schema after
the `v5` primitive is deployed. Earlier generic approvals do not authorize any
of these persistence changes.
