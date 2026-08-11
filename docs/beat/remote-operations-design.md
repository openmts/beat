# Audited remote operations

Updated: 2026-07-30

Status: reviewed design; implementation requires explicit approval

## Goal

Add commercially reliable one-off Agent tasks, a reusable command library, and
policy-controlled single-file transfer without turning monitoring credentials
into an unbounded remote-control channel.

The feature has five distinct surfaces:

- queued Agent command tasks with per-node state, cancellation, bounded output,
  and retained results;
- an encrypted command-snippet library that inserts content into an editor but
  never executes on selection;
- deploy-file transfers from an authenticated browser to one or more Agents;
- collect-file transfers from one Agent to an authenticated browser;
- bounded persisted presentation preferences for the existing SSH terminal.

The existing SSH terminal remains available as a separate interactive channel.
The current synchronous SSH batch endpoint is not silently repurposed as the
new Agent protocol.

## Current Beat boundary

Beat currently offers:

- an authenticated interactive SSH WebSocket;
- a synchronous `POST /api/v1/terminal/execute` request that sends the same
  command to every online node selected by the frontend;
- a 30-second request timeout, five Server-side SSH workers, at most 50 nodes,
  a 4 KiB command, and at most 1 MiB combined SSH output;
- TOFU SSH host-key checking and a managed SSH key per node.

That path is useful for immediate operator access, but it has no durable task
queue, explicit target attempt state, cancellation protocol, restart recovery,
result retention, Agent-side local policy, file roots, or transfer integrity
model. Agent-based operations must therefore be a separate subsystem.

## Competitor evidence

Komari creates a task and per-client result rows, dispatches arbitrary shell to
connected or short-lived queued Agents, and accepts an Agent result. Its admin
execution method is marked sensitive. The current Agent executes Unix commands
through `sh -s` and Windows commands through a temporary PowerShell file.

Observed limitations include no command timeout, output bound, cancellation,
durable delivery lease, at-most-once start protocol, per-node capability
policy, or encrypted task content. The Agent logs the complete command, and a
legacy result URL places the Agent token in its query string.

Komari's clipboard is a plaintext SQLite-backed library of named command
snippets shown beside the terminal. It is not access to an operating-system
desktop clipboard. The inspected current Server, Agent, and web protocol do not
expose node file transfer; upload/download handlers are for Server backup,
theme, favicon, and similar application assets.

DStatus provides a plaintext SSH script library and Server-initiated SSH
execution. Its reviewed sources do not expose an equivalent Agent file-transfer
protocol.

Reviewed community sources:

- Komari task creation and dispatch:
  <https://github.com/komari-monitor/komari/blob/main/web/rpc/jsonrpc/admin.system.go>
- Komari task persistence and result API:
  <https://github.com/komari-monitor/komari/blob/main/database/tasks/tasks.go>
  and
  <https://github.com/komari-monitor/komari/blob/main/web/rpc/jsonrpc/client.go>
- Komari Agent execution:
  <https://github.com/komari-monitor/komari-agent/blob/main/server/task.go>
- Komari command library:
  <https://github.com/komari-monitor/komari/blob/main/web/rpc/jsonrpc/admin.clipboard.go>
  and
  <https://github.com/komari-monitor/komari-web/blob/radix/src/pages/terminal/CommandClipboard.tsx>
- DStatus SSH scripts:
  <https://github.com/fev125/dstatus/blob/main/modules/ssh_scripts/index.js>
  and
  <https://github.com/fev125/dstatus/blob/main/database/ssh_scripts.js>

Beat matches the useful task and command-library workflows with stronger
execution controls. File transfer is a deliberate commercial extension rather
than a claim about current competitor protocol parity.

## Security invariants

1. Remote operations are disabled by default on both Server policy and every
   Agent. Enabling only one side does not enable execution.
2. The effective capability is the intersection of owner policy and the
   Agent's local `0600` configuration. The Server cannot expand local roots,
   run-as identity, task kinds, timeout, size, or concurrency.
3. Only an active per-node Agent token can lease or complete that node's work.
   Enrollment grants, legacy names, request fields, and another node's token do
   not authorize an operation.
4. An Agent never launches a command or mutates a file before the Server has
   durably acknowledged the exact target attempt and lease nonce.
5. Once start is acknowledged, an attempt is never automatically executed
   again. Lost and interrupted attempts require an explicit administrator
   retry, which creates a new attempt ID.
6. Commands, snippet content, stdout, stderr, transferred bytes, credentials,
   and absolute Agent paths never appear in logs, audits, metrics, URLs, or list
   APIs.
7. Command content and retained output are encrypted in SQLite. Transfer bytes
   are encrypted in private Server spool files. Plaintext exists only in
   bounded memory or private Agent temporary files while in use.
8. File requests identify an Agent-configured root ID and a relative path. The
   Server cannot submit an arbitrary absolute path.
9. Directories, symlinks/reparse points, devices, sockets, FIFOs, and remote
   directory listing are not supported. A transfer handles one regular file.
10. Public pages, public APIs, MTS, alert rules, and metric WebSockets never
    expose operation state or content.

## Authorization and policy

### Roles

- Owners create, update, assign, and disable operation policies after recent
  reauthentication.
- Owners and admins may create command drafts, compose tasks, cancel work, and
  inspect metadata.
- Creating a command task, starting a file transfer, viewing full command or
  output content, and downloading a collected artifact require recent
  reauthentication.
- Only owners approve a shared command snippet or enable ad-hoc shell in a
  policy. Admins may keep private drafts and execute an already permitted
  command after recent reauthentication.

Cancel remains available to an authenticated owner or admin without an extra
reauth prompt so an unsafe operation can be stopped immediately. Authorization
is enforced on every route; hiding a control is not a security boundary.

### Server policy

An owner policy targets explicit nodes or groups and contains:

- enabled task kinds: approved snippet, ad-hoc shell, deploy file, collect file;
- maximum target count, active tasks, per-node concurrency, timeout, and output;
- permitted Agent root IDs and read/write direction;
- per-file and per-transfer size, global spool quota, overwrite, and executable
  mode controls;
- task, output, transfer, and artifact retention;
- whether a successful malware scan is required before dispatch or download;
- notification routing for completion or failure.

Policies use deny-by-default values. Group membership changes do not alter an
already-created task's immutable node target snapshot.

### Agent local policy

The Agent configuration gains an optional `operations` object. Absence means
disabled, preserving old configurations:

```json
{
  "operations": {
    "enabled": false,
    "max_concurrency": 1,
    "commands": {
      "enabled": false,
      "allow_ad_hoc": false,
      "run_as_user": "beat-ops",
      "maximum_timeout": "5m",
      "maximum_output_bytes": 262144,
      "working_root": "work"
    },
    "file_roots": [
      {
        "id": "inbox",
        "name": "Application inbox",
        "path": "/srv/beat-inbox",
        "read": false,
        "write": true,
        "maximum_file_bytes": 268435456,
        "overwrite": false,
        "allow_executable": false
      }
    ]
  }
}
```

The real configuration never contains explanatory comments. Root IDs and names
are sent to the Server as capabilities; absolute paths are not. An Agent startup
fails closed for duplicate IDs, relative roots, symlink roots, unsafe modes,
missing run-as identities, invalid limits, or a root overlapping Agent config,
state, `/proc`, `/sys`, `/dev`, or another explicitly denied location.

On Unix, writable roots must not be group/world-writable and command execution
defaults to a dedicated non-root account. Root execution requires an explicit
local flag and an explicit owner policy flag. On Windows, commands run as the
dedicated Agent service account and the operations state, temporary scripts,
and transfer roots require a restrictive service-account DACL. The Server
cannot opt into root or Administrator execution remotely.

## Command library

The interface calls this feature `Command library`, not clipboard, because it
does not read a user's local clipboard or a node desktop clipboard.

A snippet has a name, description, shell family, content, owner, visibility
(`private` or `shared`), approval state, immutable version, keyed content
fingerprint, and timestamps. Every edit creates a new version and removes
approval from the new content. Historical tasks retain the exact encrypted
version they executed.

Snippet content is encrypted through the shared application-secret lifecycle
defined in
[`application-secret-lifecycle-design.md`](./application-secret-lifecycle-design.md).
AES-GCM associated data contains resource ID, version, shell family, and schema
version. The versioned envelope names its wrapped data key, legacy nil-AAD TOTP
has one typed conversion path, and swapping ciphertext between rows must fail
authentication.

List APIs return metadata and digest only. Full content requires a recently
reauthenticated detail request and is never cached. Copying to the browser
clipboard requires a direct user gesture and secure context. Selecting a
snippet inserts it into the task editor; it never submits or sends a newline to
an interactive terminal automatically.

The first version deliberately has no server-side template language, secret
substitution, arbitrary JavaScript, scheduled execution, or hidden parameters.
The operator reviews the exact immutable command before execution.

## Command execution protocol

### Task creation

An owner or admin selects explicit node names or groups, but the Server resolves
and stores an immutable node-ID snapshot in one transaction. Creation validates
policy, recent reauthentication, node credential state, Agent capability age,
target count, command size, timeout, output limit, and concurrency quota.

The Server encrypts the command body with resource-bound AAD and stores only a
keyed fingerprint in readable metadata. A task receives one target row per
node. Initial target state is `queued`; offline nodes remain queued until task
expiry rather than being reported as completed failures.

Default limits are conservative and owner policies may only reduce Agent
limits: 64 KiB command input, five-minute timeout, 256 KiB combined stdout and
stderr, one concurrent command per Agent, 50 targets, and a 24-hour queue
expiry. Hard implementation maxima are 64 KiB input, one-hour timeout, 1 MiB
output, four concurrent commands per Agent, and 200 targets even if database
values are corrupt.

### Lease and start

An enabled Agent long-polls
`POST /api/v1/agent-operations/lease` with its per-node token and current
capability digest. The Server may return one command or transfer lease containing
an attempt ID, random lease nonce, expiry, and bounded payload. Only the nonce
hash is stored.

For a command, the Agent:

1. validates the payload against its local policy;
2. writes and syncs a `0600` `leased` journal under a `0700` operations state
   directory;
3. calls `POST /api/v1/agent-operations/attempts/{id}/start` with the lease;
4. retries that idempotent start until it receives the durable authorization;
5. writes and syncs `authorized`, then launches the process exactly once.

The Server transitions a target from `queued` to `running` in an immediate
transaction and returns the same authorization for retries of the same nonce.
An expired lease that never reached `running` may be offered again. A target
that reached `running` is never automatically requeued.

The Agent heartbeats a running attempt every five seconds. A heartbeat response
can request cancellation and the Agent acknowledges process-tree termination.
If heartbeats stop beyond the bounded grace period, the Server marks execution
`unknown`, not failed. The same attempt may later heartbeat or submit its
idempotent result. Retrying an unknown attempt requires an explicit warning that
the prior side effect cannot be proven stopped; it is never an automatic action.

### Process boundary

Unix commands use a configured shell with the script on standard input, so
content is absent from process arguments. Windows uses a private PowerShell
file with a restrictive DACL and passes only its generated path. Neither path
logs the command.

The Agent uses a clean, allowlisted environment, fixed `PATH`, configured
working root and run-as identity, no inherited Agent token, and bounded
stdin/stdout/stderr. Timeout, cancellation, output exhaustion, Agent shutdown,
or local storage failure terminates the entire child process tree and waits for
it.

On Linux, the signed Agent binary re-executes an internal operation helper that
receives only structured descriptors and IDs, sets the run-as identity,
file-size/open-file/process-count/CPU/address-space limits, `no_new_privs`, and
parent-death handling, then executes the shell with content on standard input.
The attempt runs in a dedicated cgroup v2 or hardened systemd scope so cancel
can kill escaped descendants; if policy requires these controls and they are
unavailable, execution fails closed. Windows uses a Job Object with
kill-on-close, resource limits, and the dedicated service account. Commands
cannot change the Agent's local operation policy.

### Result and restart behavior

Stdout and stderr are captured separately in private `0600` files and never
rendered as HTML or interpreted ANSI. Crossing the combined byte limit stops the
process and marks the result `output_limit`. The Agent submits a gzip-capable,
strictly bounded result containing exit status, signal/reason, byte counts,
truncation state, start/end times, and SHA-256 digests.

The Server accepts completion only from the authenticated target node and exact
attempt nonce. Duplicate completion with the same digest is idempotent;
different content conflicts and is audited. It encrypts stdout/stderr with
resource-bound AAD before committing terminal state. The Agent retains its
local result until acknowledgement, then unlinks its generated journal and
temporary output. Beat does not claim physical secure erasure from SSDs,
journaling filesystems, snapshots, or host backups.

After Agent restart, a completed journal resumes result delivery. A `leased`
journal may reacquire start authorization. An `authorized` or `running` journal
is reported `interrupted` and never relaunched. Child process groups must die
with the Agent so an untracked command cannot continue.

Target states are:

```text
queued -> leased -> running -> succeeded | failed | timed_out
   |         |          |       canceled | output_limit | interrupted
   |         |          +-> unknown -> result | manual risk-ack retry
   |         |          +-> cancel_requested -> canceled
   |         +-> queued (only before durable start)
   +-> expired | canceled
```

Retry is an explicit administrator action that creates a new attempt, retains
the previous result, rechecks current policy/capability, and records actor and
reason.

## File transfer

### Shared rules

File operations use an Agent root ID and normalized relative path. Both Server
and Agent reject empty/absolute paths, `..`, NUL, mixed separator tricks,
Windows drive/UNC/device/alternate-stream syntax, excessive components, and
overlong UTF-8 names.

Go 1.26 `os.Root` provides the common containment API, but is not sufficient by
itself because it follows same-root symlinks and permits bind mounts. Linux uses
`openat2`/`renameat2` through `x/sys/unix` with `RESOLVE_BENEATH`,
`RESOLVE_NO_SYMLINKS`, `RESOLVE_NO_MAGICLINKS`, and `RESOLVE_NO_XDEV`. Windows
uses handle-relative opens that reject reparse points and volume changes.
No-overwrite publication uses an atomic no-replace primitive; explicit
overwrite uses an atomic platform replacement. File transfer stays disabled on
a platform without equivalent guarantees.

Roots must also be dedicated and not writable by untrusted users. An opened
source is checked again through its handle and must be one regular file within
size policy. The implementation never authorizes a path using a check followed
by an unrelated pathname open.

No transfer preserves owner, group, ACL, xattr, sparse layout, or timestamps.
Created directories use `0700`; files use `0600`, or `0700` only when both
local and owner policy explicitly permit an executable destination. Existing
files are not overwritten by default.

Default transfer limits are 4 MiB chunks, 256 MiB per file, two concurrent
transfers per Agent, and a 4 GiB Server spool. Hard code limits are 2 GiB per
file and 4096 chunks. A startup spool limit and local Agent root limit may be
lower; owner policy may only reduce the effective value.

### Deploy file

The browser initializes a transfer with filename, exact byte size, complete
SHA-256 digest, targets, root ID, relative destination, mode, and overwrite
choice. It uploads fixed-size chunks with `Content-Range`, sequence, length, and
chunk digest. Repeating an identical chunk is idempotent; conflicting bytes
fail the session.

The Server encrypts chunks immediately into a random, non-deduplicated spool
artifact, enforces user/global quotas, verifies the final size and digest, and
runs any required malware scanner before creating node targets. A scanner is a
fixed startup integration called without a shell; policy that requires scanning
fails closed when it is unavailable.

Each Agent leases its target and downloads authenticated chunks. It writes a
random `0600` temporary file inside the destination root, syncs, verifies size
and SHA-256, applies the allowed mode, and atomically renames. An overwrite uses
an atomic replacement only after complete verification. Cancellation or failure
removes only the generated temporary file.

### Collect file

A recently reauthenticated owner or admin submits one node, root ID, and
relative source. The Agent validates and opens one regular file, reports size
and digest, then uploads authenticated chunks from the already-open descriptor.
The Server encrypts each chunk, verifies the final digest, optionally scans it,
and marks the artifact ready.

Browser download requires another recently authenticated request, uses a
sanitized attachment filename, `application/octet-stream`, `nosniff`, and
`Cache-Control: no-store`. The API never accepts a Server filesystem path.

Deploy may target multiple nodes using one encrypted source artifact. Collect
is one node per transfer to keep provenance unambiguous. Transfer sessions are
resumable by missing chunk number, bounded in duration, cancelable, and expose
per-target progress without revealing absolute paths.

## Encrypted Server spool

Transient bytes live under `<data-dir>/operations-spool`, directory mode
`0700`, with random file names and mode `0600`. They are never stored in MTS or
served directly by the static file handler.

The active wrapped application data key derives a versioned operations spool
key with standard-library HKDF-SHA-256 and a fixed domain label. Artifact
metadata records the data-key ID so decrypt-only predecessors can finish
in-flight work during rotation. Each fixed-size frame uses a fresh AES-GCM nonce
and associated data binding artifact ID, direction, chunk number, declared total
size, and complete digest. Exact frame count and final digest reject truncation,
reordering, and cross-artifact substitution. Encryption/decryption streams with
bounded memory.

Separate HKDF domain labels derive HMAC-SHA-256 fingerprint keys. Commands,
snippet content, result text, and relative paths use keyed fingerprints for
correlation and idempotency so a database or audit disclosure cannot cheaply
dictionary-match common plaintext. File payloads retain ordinary SHA-256
because browsers and Agents need an interoperable end-to-end digest.

The spool has hard total, per-user, per-node, and per-transfer quotas. Incomplete
uploads expire after six hours; completed deploy sources and collected files
default to 24 hours after completion; cleanup is transactional and removes a
database row only after its generated file is handled. Orphan reconciliation
runs at startup.

## Shared application-secret foundation

Migration `v5` is the single owner of Beat's versioned secret envelope, AAD
registry, wrapped data-key registry, data/root-key rotation, readiness, and
backup pairing validation. It converts existing TOTP ciphertext to user-bound
AAD, converts retained `v2` OIDC envelopes to wrapped data keys, and migrates
every legacy SSH private key from plaintext to the shared envelope before
serving. SSH list queries become metadata-only; one authorized terminal
admission decrypts only its selected key for the shortest practical lifetime.

The 32-byte `admin-data.key` remains the root key-encryption key. Random data
keys are wrapped inside SQLite, so normal data-key rotation is transactional
with application state and does not alter the backup payload. Root-key rotation
is a stopped-service journaled operation. The complete contract is
[`application-secret-lifecycle-design.md`](./application-secret-lifecycle-design.md);
remote operations may not introduce another cipher, key file, or rotation path.

## SQLite application data

Remote operations use canonical SQLite migration `v5` after secure Agent
enrollment `v4`, as assigned by
[`commercial-parity-implementation-sequence.md`](./commercial-parity-implementation-sequence.md).

Tables are:

- `application_data_keys` and bounded secret-lifecycle state: wrapped active and
  decrypt-only data keys, backfill/rotation generation and cursor, with no
  plaintext key material;
- rebuilt `ssh_keys`: metadata plus AAD-bound private-key ciphertext, with legacy
  plaintext removed before readiness;
- `operation_policies`: owner-defined scope, limits, enabled kinds, retention,
  scan requirement, creator, version, and timestamps;
- `operation_policy_nodes` and `operation_policy_groups`: explicit policy
  assignments with foreign keys;
- `operation_snippets`: stable identity, name, description, visibility, owner,
  current version, and timestamps;
- `operation_snippet_versions`: snippet ID, immutable version, shell family,
  encrypted content, keyed fingerprint, approval actor/time, and timestamps;
- `operation_tasks`: kind, encrypted immutable payload, keyed fingerprint,
  policy snapshot, actor, timeout/output limits, queue expiry, cancel state,
  and timestamps;
- `operation_task_targets`: node ID/name snapshot, aggregate state, current
  attempt, cancellation state, and timestamps;
- `operation_attempts`: target, attempt number, nonce hash, lease/start/result
  state, capability digest, encrypted stdout/stderr, result metadata, retry
  actor/reason, and timestamps;
- `operation_transfers`: direction, encrypted relative path, root ID, size,
  digest, mode/overwrite policy, artifact ID, state, actor, expiry, and times;
- `operation_transfer_targets`: node snapshot, attempt, progress, result, and
  timestamps;
- `operation_artifacts`: random storage name, encrypted frame metadata, size,
  digest, scan status, expiry, download state, and timestamps.

Commands, paths, output, and file bytes are application/security data, not time
series. SQLite stores task metadata and encrypted bounded text. Encrypted binary
transfer content stays in the spool. No operation content or state is written
to MTS.

All state transitions use immediate transactions, affected-row checks, foreign
keys, unique attempt constraints, and idempotency keys. Deleting a node does not
silently erase audit history; immutable node-name snapshots remain.

## Node and group lifecycle integration

Deleting a node with queued, leased, running, unknown, or transferring work
returns a conflict. The operator must cancel and reach a terminal state first.
An owner with recent reauthentication may force deletion after acknowledging
that an unreachable process cannot be proven stopped; Beat revokes the token,
cancels queued work, marks unresolved attempts interrupted/unknown, clears
foreign keys, and retains snapshots and audit evidence.

Token rotation does not change node ownership of work. The old token fails
immediately; the same Agent can resume its journals only after receiving the new
token through the existing secure configuration workflow. Token revocation
prevents new leases and result submission and marks nonterminal work for
operator reconciliation; it cannot retroactively undo a process already
running on an unreachable host.

Deleting a group referenced by an enabled policy returns a conflict naming the
policies. The owner must reassign or disable them. Group deletion never silently
widens a policy to the default group. A policy edit does not mutate an existing
task's target or execution snapshot.

## API surface

Owner policy routes, all requiring recent reauthentication for mutation:

- `GET/POST /api/v1/admin/operations/policies`
- `GET/PUT /api/v1/admin/operations/policies/{id}`
- `POST /api/v1/admin/operations/policies/{id}/disable`

Authenticated command-library routes:

- `GET/POST /api/v1/admin/operations/snippets`
- `GET/PUT/DELETE /api/v1/admin/operations/snippets/{id}`
- `POST /api/v1/admin/operations/snippets/{id}/approve`

Authenticated task routes:

- `GET/POST /api/v1/admin/operations/tasks`
- `GET /api/v1/admin/operations/tasks/{id}`
- `POST /api/v1/admin/operations/tasks/{id}/cancel`
- `POST /api/v1/admin/operations/tasks/{id}/targets/{target_id}/retry`
- `GET /api/v1/admin/operations/tasks/{id}/targets/{target_id}/output`

Transfer routes:

- `GET/POST /api/v1/admin/operations/transfers`
- `POST /api/v1/admin/operations/transfers/{id}/chunks`
- `POST /api/v1/admin/operations/transfers/{id}/finalize`
- `POST /api/v1/admin/operations/transfers/{id}/cancel`
- `GET /api/v1/admin/operations/transfers/{id}/download`

Agent routes, all bound to middleware-resolved node identity:

- `POST /api/v1/agent-operations/lease`
- `POST /api/v1/agent-operations/attempts/{id}/start`
- `POST /api/v1/agent-operations/attempts/{id}/heartbeat`
- `POST /api/v1/agent-operations/attempts/{id}/complete`
- `GET/POST /api/v1/agent-operations/attempts/{id}/chunks`

Every body and decompressed body has an explicit limit, unknown JSON fields are
rejected, cursors and filters are allowlisted, and upload endpoints validate
content length and stream rather than buffering whole files.

Command, snippet-content, output, and artifact-download responses use
`Cache-Control: no-store`, restrictive content types, and no reflection into
HTML. Output is delivered as plain text with control characters escaped except
for tab and line breaks.

## Administration experience

Add `/admin/operations` as a compact shadcn work surface with four tabs:
`Tasks`, `Command library`, `Transfers`, and owner-only `Policies`. The existing
terminal stays focused on live SSH and links to the command library without
embedding another card hierarchy.

The page's signature element is a target execution matrix: one task header and
stable node rows showing node name, group name, capability, queue/run state,
elapsed time, exit reason, output bytes, and retry count. Selecting a row opens
a side sheet with plain-text stdout/stderr and attempt timeline. Status uses
icon plus text, never color alone.

The composer uses an explicit multi-node/group selector whose trigger renders
human names, a line-numbered monospace editor, immutable fingerprint preview,
timeout
and output controls, and a final confirmation naming the exact target count.
Choosing a snippet inserts content for review. Ad-hoc mode is absent when policy
disallows it.

Transfers show direction, human root name, relative path, node names, digest,
scan state, byte progress, expiry, and per-target outcome. Upload uses a real
file picker and progress control; collect requires a known relative path rather
than a remote file browser. Destructive overwrite is an explicit checkbox and
confirmation, not a default.

Desktop uses dense unframed toolbars, tables, and sheets. Mobile renders each
task or transfer as one flat card with stable status/action areas and an
overflow menu. No card is nested inside another card. Loading, empty, partial
failure, stale capability, canceled, expired, lost-Agent, quota, scan failure,
and permission-denied states have actionable messages.

Task and transfer progress updates through an authenticated event stream or
bounded background polling without reloading the page. Actions reconcile after
failure instead of leaving optimistic false state. All node, group, policy,
root, snippet, actor, and task selectors display names; raw IDs remain API
values only and are guarded by regression tests.

### Terminal presentation preferences

Komari stable Web exposes persisted xterm.js display settings. Beat includes a
bounded equivalent in migration `v5` without accepting arbitrary CSS or an
unvalidated xterm options object. The owner-managed operations policy stores:

- font size from 11 to 24 px and scrollback from 500 to 20,000 lines;
- cursor blink, convert-EOL, and Mac Option-as-Meta switches;
- terminal padding from 0 to 32 px;
- one of the built-in `system`, `light`, `dark`, or `high-contrast` palettes;
- an optional validated comma-separated font-family list capped at 256 bytes.

The Server normalizes every value and returns only this typed structure. Font
families permit quoted/unquoted names and generic families, reject control
characters, CSS punctuation outside the grammar, URLs, functions, and unknown
tokens, and never load a remote font. Raw CSS, arbitrary color JSON, opacity,
background images, selectors, and per-user script are not supported.

Preferences apply when a new terminal opens and do not interrupt an active
session. The settings surface uses `FieldGroup`, bounded numeric steppers,
switches, and a palette `Select`; it shows names, not internal enum keys. A
`Reset terminal appearance` command restores defaults. Mobile keeps stable
terminal controls and never lets long font names widen the page.

Tests cover normalization, ranges, malformed font lists, XSS/CSS injection,
fallback defaults, reset, restart persistence, light/dark/high-contrast
legibility, active-session stability, keyboard input, resize, and mobile
overflow. Preference values are application settings in SQLite, never MTS,
audit secret material, or backup-format changes.

## Audit, logs, metrics, and alerts

Audit events cover policy lifecycle, snippet draft/approve/update/delete, task
create/start/cancel/retry/expire/result conflict, transfer initialize/finalize,
artifact scan/download/expire, and Agent policy rejection. Details include
bounded reason codes, actor, node count, operation kind, command fingerprint,
byte counts, and keyed path fingerprint. They never contain command/path
plaintext, output, file names, file content, lease nonce, token, or scanner
output.

Server and Agent structured logs use operation/attempt IDs, state, duration,
exit reason, and byte counts only. Even debug level does not log payloads or
authorization headers.

Prometheus metrics cover queue depth/age, lease/start/completion outcomes,
running duration, cancellation latency, output-limit termination, transfer
bytes/duration, spool usage, scan outcomes, cleanup, and policy rejection.
Labels are bounded enums; task, node, actor, filename, path, command, and digest
are never labels.

Readiness reports migration, root-key and wrapped-key registry health,
TOTP/SSH backfill, rotation, operations scheduler, and spool health. Missing
referenced key material, legacy plaintext after backfill, or an unwritable spool
marks the subsystem unready; public monitoring liveness remains independent.
Alerts cover old queues, lost running attempts, repeated policy rejection, key
rotation/backfill failure, spool quota, cleanup/scanner failure, and abnormal
task failure rates.

## Retention, backup, restore, and rollback

Task metadata defaults to 90 days and encrypted output to 30 days. Snippets and
policies persist until deleted. Transfer artifacts have the short TTL described
above. Retention runs under the existing maintenance scheduler and shared
operation lock.

SQLite backups include the wrapped data-key registry, encrypted SSH/TOTP data,
policies, snippets, task metadata, and retained encrypted command output. The
matching root key already included in the backup unwraps those data keys.
Validation must unwrap every referenced key and authenticate every registered
ciphertext with reconstructed AAD; size/checksum-only key validation is not
sufficient. Transient transfer spool files are intentionally not
backup-authoritative and do not increment the archive format. A staged restore
marks referenced in-flight transfers expired, marks running tasks interrupted,
and removes orphan spool files before operation readiness. It never resumes a
file mutation from mismatched restored state.

Deployment order:

1. Back up Server/Agent binaries, static assets, SQLite, MTS, data key, Agent
   configs, and operations roots according to host policy.
2. Deploy the consecutive migration, encrypted storage, API, and frontend with
   every policy disabled and old SSH behavior unchanged.
3. Deploy Agents that understand the optional disabled operations config;
   verify metrics and network probes are unchanged.
4. Enable one non-root test Agent, one approved harmless snippet, no file roots,
   and a one-node policy. Exercise queue, cancel, timeout, output limit, restart,
   result retry, and duplicate start.
5. Add one dedicated write-only inbox and one dedicated read-only collection
   root; exercise digest mismatch, symlink, overwrite, quota, scanner, resume,
   cancellation, restart, and IPv4/IPv6 paths.
6. Broaden policy only after audits, metrics, alerts, backup/restore, and spool
   cleanup have production evidence.

Before any operation is enabled, rollback restores the old binary and matching
database backup. After migration, the old Server rejects the newer schema. If a
command has started, rollback never attempts to undo its external side effects.
Operators cancel active work, verify affected hosts, restore matching Server
and Agent versions/configs, and rotate any credential exposed by task content.

## Test and acceptance gates

Backend and Agent tests cover:

- role/recent-reauth enforcement and disabled-by-default policy intersection;
- node identity isolation, legacy/pending/revoked rejection, lease nonce theft,
  expiry, replay, cross-node completion, and idempotent results;
- concurrent lease/start/cancel/retry transactions and every injected failure;
- proof that execution never starts before durable authorization and never
  automatically restarts after durable start;
- process-tree cancellation on Unix and Windows, non-root execution, clean
  environment, timeout, output/disk limits, and restart journals;
- legacy TOTP/SSH backfill, AAD row/field/version swapping, mixed-key and
  root/data-key rotation, key loss/corruption, gzip bombs, payload limits, and
  complete redaction from logs/audit/metrics/errors;
- every path encoding, traversal, symlink/reparse, rename race, non-regular
  file, mode, overwrite, digest, chunk replay/conflict, quota, scanner, cleanup,
  and spool corruption path;
- SQLite migration/restart/future-version behavior, retention, backup/restore
  reconciliation, and orphan handling.

Frontend tests cover human-readable selector labels, explicit target snapshots,
snippet insert-not-run behavior, reauth boundaries, command/output redaction,
task matrix updates, cancel/retry conflicts, upload resume/progress, overwrite,
download expiry, terminal display preference validation/reset, partial target
failure, responsive layouts, keyboard/focus, and loading/empty/error states.

Acceptance requires:

- task parity on supported Linux and Windows Agents with bounded arbitrary shell
  only when explicitly enabled;
- command library parity without plaintext content at rest or automatic
  execution;
- safe deploy/collect of a regular file over IPv4 and IPv6 with exact digest,
  resume, cancellation, and no partial destination publication;
- no duplicate command execution across lost responses, Server restart, Agent
  restart, or lease expiry;
- immediate cancellation propagation and termination of descendants;
- no operation content in public APIs, MTS, logs, audits, metrics, browser
  storage, URLs, process arguments, or unencrypted Server spool;
- at least 90 percent backend statement and frontend line coverage;
- race/shuffle tests, `goimports-reviser`, `golangci-lint`, `go vet`,
  `govulncheck`, module verification, production builds, browser smoke tests,
  backup/restore drills, and deployed IPv4/IPv6 acceptance aligned with CI.

## Approval boundary

Implementation requires explicit approval for the complete batch because it
adds arbitrary Agent execution and file mutation capability, changes the Agent
configuration and shared protocol, adds SQLite tables and encrypted fields,
extends the administrator data-key API, creates an encrypted spool directory,
adds role/reauth behavior, introduces optional malware-scanner integration, and
adds bounded Server/Agent operation settings to environment templates and
generated configurations.

Approval must confirm:

1. Existing interactive SSH remains separate; the new Agent task path does not
   silently replace it. Its recent-auth, non-cacheable response, session
   invalidation and audit admission are first supplied by the schema-free
   [`administrative-mutation-consistency-design.md`](./administrative-mutation-consistency-design.md)
   contract and cannot be weakened by `v5`.
2. Remote operations are disabled by default and use the intersection of owner
   policy and local Agent policy.
3. Explicit at-most-once start semantics: no automatic retry after durable
   start, even if that leaves an interrupted task requiring manual retry.
4. Owner/admin execution authority, owner-only policy/snippet approval, and the
   listed recent-reauth boundaries.
5. Single regular-file transfer through root IDs, with no remote directory
   listing, symlinks, metadata preservation, or default overwrite.
6. The shared wrapped-key registry, TOTP/SSH plaintext removal, versioned
   AAD-bound SQLite command/output data, root/data-key rotation, encrypted
   transient spool, and deliberate exclusion of transient transfer artifacts
   from backups.
7. Terminal preferences are typed and bounded; arbitrary CSS, remote fonts,
   background images, and unvalidated xterm option objects remain forbidden.
8. Migration numbering relative to OIDC, theme packages, and Agent enrollment.
