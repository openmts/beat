# Agent auto-discovery and secure enrollment

Updated: 2026-07-30

Status: reviewed design; implementation requires explicit approval

## Goal

Add convenient Agent discovery without weakening Beat's deployed per-node
identity model. An enrollment credential may create or resume a bounded
application, but it is never an Agent reporting credential. Every accepted
machine ultimately receives its own one-time `beat_agent_v1_*` token.

The design supports two workflows:

- a manual inbox where an administrator reviews every discovered machine;
- an owner-created automatic grant whose scope, lifetime, quota, group, source
  network, hostname, labels, visibility, and SSH defaults are fixed in advance.

Public monitoring remains login-free. Enrollment management remains inside the
authenticated administration application.

## Competitor evidence and rejected risks

Komari accepts one global auto-discovery key and immediately creates a formal
client with a long-lived UUID and token. The key has only a minimum length
check; the flow has no approval, grant expiry, quota, source policy, or
per-registration replay boundary. Client tokens are recoverable from its
database.

DStatus adds a pending/approved workflow, default group, notifications, and
approve/reject controls. However, registration already returns a long-lived
API key before approval. Its observed flow also relies on IP-based identity,
trusts forwarding headers directly, and can actively probe the Agent host.

Reviewed community sources:

- Komari registration handler:
  <https://github.com/komari-monitor/komari/blob/main/web/api/client/autoDiscovery.go>
- Komari client credential creation:
  <https://github.com/komari-monitor/komari/blob/main/database/clients/client.go>
- DStatus discovery protocol:
  <https://github.com/fev125/dstatus/blob/main/modules/autodiscovery/index.js>
- DStatus discovery persistence and administration:
  <https://github.com/fev125/dstatus/blob/main/database/autodiscovery.js> and
  <https://github.com/fev125/dstatus/blob/main/views/admin/autodiscovery.html>

Beat adopts the useful operator workflows, not those trust models. In
particular, Beat does not:

- let a shared key write metrics or impersonate an existing node;
- issue a node token before approval;
- merge identities by hostname, IP address, or machine ID;
- trust forwarding headers outside the existing trusted-proxy policy;
- connect back to a discovered Agent;
- persist recoverable enrollment or node credentials;
- allow one unbounded, permanent registration key.

## Security invariants

These conditions are part of the protocol contract, not optional UI behavior:

1. A grant secret authorizes only enrollment operations.
2. A pending or approved-but-unclaimed request cannot report metrics, read
   network assignments, submit network results, or use remote operations.
3. Pending and approved-but-unclaimed nodes never appear in public node APIs,
   public WebSockets, availability alerts, traffic reports, or MTS series.
4. Approval reserves a node in `pending_claim`; claim generates the node token
   inside the activation transaction. No plaintext node token is staged.
5. Every active machine has an independent per-node token. Compromise or
   revocation of one machine does not authorize another.
6. Grant, request, and claim secrets are random, shown once, stored as hashes,
   redacted from logs, and absent from URLs.
7. Claim tickets are short-lived, single-use, and bound to the enrollment
   request and Agent public key.
8. All enrollment traffic uses HTTPS. Plain HTTP is allowed only for an
   explicit loopback development configuration.
9. The Server never initiates a discovery connection to an Agent.
10. SQLite stores enrollment policy and identity lifecycle data; all reported
    metric and probe samples continue to be written only to MTS.

## Enrollment identities

### Grant secret

An owner creates a 32-byte random secret with a recognizable versioned prefix:

```text
beat_enroll_v1_<base64url-random>
```

SQLite stores only its SHA-256 hash and a non-secret display prefix. The
plaintext appears in the create or rotate response once. Rotation invalidates
the old secret and every outstanding challenge created from it.

Grant management requires an owner session with recent reauthentication. A
grant defines:

- name, enabled time, absolute expiry, and immediate revocation state;
- `manual` or `automatic` approval mode;
- maximum accepted enrollments and the permanently consumed slot count;
- default group, fixed labels/remarks, visibility, node-name prefix, and SSH
  policy;
- optional trusted source CIDRs and hostname regular expression;
- a fixed SSH port and whether the canonical source IP may become the node
  host; reported hosts are suggestions only and are never auto-trusted;
- whether a signed claim may be recovered during the bounded delivery window.

The consumed count increments when approval reserves a node and is not reduced
by later revocation or expiry. This prevents a leaked grant from being reused
through repeated enroll/revoke churn. An owner must rotate or replace a grant
to deliberately provide more capacity.

### Agent enrollment key

Before its first enrollment, the Agent generates an Ed25519 key pair using the
standard library. The private key is written to a `0700` state directory in a
`0600` file before any request is sent. The public-key SHA-256 fingerprint is
the request identity.

This key proves continuity while the request waits for approval and protects
claim from theft of a polling token. It is not an Agent reporting credential
and cannot authenticate any existing node.

Beat never merges an active node based on a matching enrollment key. A key can
only resume its own pending or approved request. After a successful claim and
the bounded recovery window, the Agent removes the enrollment private key and
request token.

## Protocol

### Challenge

`POST /api/v1/agent-enrollment/challenge` receives the grant secret in the
`Authorization` header and a bounded body containing the Agent public key and
metadata digest. The Server validates grant state, time window, quota, source
CIDR, body size, and rate limits before returning:

- challenge ID and 32 random challenge bytes;
- canonical Server origin, issue time, and two-minute expiry;
- protocol version.

SQLite stores only the challenge hash, grant ID, key fingerprint, metadata
digest, canonical trusted source IP, expiry, and consumption state. Unknown,
revoked, expired, and source-mismatched grants produce the same response shape.

### Request creation

The Agent signs a canonical, length-delimited payload containing the protocol
version, challenge ID and bytes, Server origin, public-key fingerprint,
metadata digest, and expiry. It then calls
`POST /api/v1/agent-enrollment/requests` with the signature and metadata.

The Server consumes the challenge exactly once, verifies every bound value,
revalidates the grant and source policy, validates the metadata, and creates a
request. Metadata is limited to:

- suggested hostname and advertised host;
- OS, architecture, Agent version, and virtualization information;
- SSH port suggestion;
- an Agent-generated installation nonce.

These values are untrusted display suggestions. They do not authorize a node,
select an existing node, or directly control a Server connection target.

The response contains a request ID and a random, one-time-visible, 32-byte
request token. Only its hash is stored. The Agent atomically persists the ID
and token in its private enrollment state, then erases the grant secret. If the
response is lost, the Agent retains the grant, obtains a new challenge, and
repeats the signed request. The same grant and public key resume the same
unclaimed request and rotate its request token; they never create duplicate
nodes.

Manual mode leaves the request `pending`. Automatic mode applies the complete
grant policy and performs approval in the same immediate transaction.

Automatic mode derives the proposed final name from the grant's fixed prefix
and a strictly sanitized hostname. A missing hostname, policy mismatch, or name
collision leaves the request pending with a policy-conflict reason for manual
review; it never adds a random suffix or merges with the existing node.

### Approval and reservation

Approval validates the grant capacity and chosen node fields again, increments
the consumed slot count, creates a hidden node in `pending_claim`, and records
the node ID on the request in one transaction. It does not generate a node
token.

Manual approval allows an owner or admin to choose the final node name, group,
labels, visibility, remarks, canonical host, and SSH port. Automatic approval
uses only owner-defined policy: the canonical trusted source IP may be used as
the host, the port is fixed by the grant, and no SSH public key is assigned.
Consequently remote terminal and batch operations remain unavailable until an
administrator explicitly assigns a managed SSH key.

Node names are unique across active, legacy, revoked, and pending-claim nodes.
The Agent hostname remains a suggestion and cannot claim an existing name.
An enabled or unexpired grant holds a restrictive foreign key to its default
group. Group deletion returns a conflict naming the affected grants until an
owner reassigns or revokes them; it never silently redirects enrollment.

### Status and claim ticket

The Agent polls `POST /api/v1/agent-enrollment/requests/{id}/status` with the
request token in `Authorization` and an Ed25519 signature over the request ID,
canonical Server origin, timestamp, and a fresh nonce. The token and signature
are both required, so theft of the polling token cannot rotate tickets or block
the legitimate Agent. Responses are non-cacheable and reveal only that
request's state and bounded rejection reason.

When the request is approved, the response includes a random claim ticket with
a two-minute TTL and a monotonically increasing ticket generation. Each valid
status call atomically rotates the ticket. The Agent persists and claims only
the greatest generation, so duplicated or reordered responses cannot restore
an older ticket.

The Agent signs a canonical claim payload containing the request ID, node ID,
claim ticket, canonical Server origin, timestamp, and a fresh nonce. It calls
`POST /api/v1/agent-enrollment/requests/{id}/claim` with both ticket and
signature.

The claim transaction verifies request state, ticket hash and expiry, Agent
key, nonce freshness, reserved node state, and that the grant remains active.
It generates a
new `beat_agent_v1_*` token, stores only the token hash and prefix, changes the
node to `active`, marks the request `claimed`, and consumes the ticket. The
response returns the plaintext token and complete Agent configuration once.

If the response is lost after commit, the same key and request token may obtain
a recovery ticket only when the grant enabled claim recovery, for at most 15
minutes and three attempts. Recovery atomically rotates the node token, so a
possibly delivered previous token stops working immediately. After that
window, an administrator must use the existing node-token rotation workflow.

### Agent activation

The Agent writes the returned configuration to a same-directory temporary file
with mode `0600`, syncs it, atomically renames it to `agent.json`, and syncs the
`0700` parent directory. Only after that succeeds does it start reporting and
the network runner. It then removes enrollment state after the recovery window.

Enrollment secrets must be supplied through a `0600` bootstrap file, standard
input, or a protected environment mechanism. Installation instructions must
not place them in shell arguments, command history, process listings, URLs, or
download scripts.

## State machines

Request transitions are one-way except for bounded claim recovery:

```text
pending -> approved -> claimed
   |          +-> expired/rejected before claim
   +-> rejected
   +-> expired
claimed -> claimed (bounded token rotation recovery only)
```

Node credential states extend the existing lifecycle:

```text
legacy                         (migration only)
pending_claim -> active -> revoked
```

`pending_claim` rejects both the legacy shared token and per-node Agent
authentication. Revoking a grant atomically rejects its unclaimed requests,
invalidates challenges/tickets, and deletes hidden node reservations that have
never held a token. Request expiry performs the same reservation cleanup. The
request retains a node-name snapshot for audit, while its node foreign key is
cleared. Neither path restores consumed grant capacity. Grant revocation never
revokes already claimed per-node credentials, but it does disable their
remaining claim-recovery window; active nodes use the independent node
lifecycle controls.

## SQLite application data

Secure enrollment uses canonical SQLite migration `v4` after OIDC `v2` and
theme packages `v3`, as assigned by
[`commercial-parity-implementation-sequence.md`](./commercial-parity-implementation-sequence.md).

`agent_enrollment_grants` contains:

- ID, unique name, secret hash and display prefix;
- approval mode, enable/expiry/revocation timestamps;
- default group ID, fixed labels/remarks, visibility, node-name prefix, and SSH
  policy;
- maximum and consumed enrollments;
- normalized CIDR list and anchored hostname expression;
- claim-recovery policy, creator, and timestamps.

`agent_enrollment_challenges` contains:

- ID, grant ID, challenge hash, key fingerprint, and metadata digest;
- trusted source IP, expiry, creation time, and consumed time.

`agent_enrollment_requests` contains:

- ID, grant ID, enrollment-key fingerprint and public key;
- request-token hash, prefix, and last rotation time;
- status and sanitized Agent suggestions;
- trusted source IP, installation nonce, first/last request time;
- reserved node ID, approver, approval/rejection/claim timestamps and reason;
- claim-ticket hash/expiry, recovery deadline/count, and timestamps.

The `nodes` table gains an explicit credential state so `pending_claim` cannot
be mistaken for legacy. Migration derives current rows deterministically from
their existing token hash, creation, and revocation columns. Foreign keys,
unique indexes, check constraints, immediate transactions, and affected-row
checks enforce state and capacity invariants.

Every node read path explicitly decides whether hidden reservations are valid.
Public/admin node lists, WebSocket snapshots, alerts, traffic aggregation, MTS
queries, probes, terminal, batch commands, and legacy-name authentication all
exclude `pending_claim`; only enrollment management and its activation
transaction may read it. This is enforced in store methods rather than by UI
filtering.

Challenges expire after two minutes. Unapproved requests default to seven days;
approved requests default to 24 hours. Cleanup deletes expired challenges and
redacts expired request secrets while retaining minimal audit-correlated
metadata according to the existing 180-day policy.

## API and authorization

Enrollment protocol routes are public but grant/request authenticated, HTTPS
only, same-origin independent, rate limited, and JSON-body bounded:

- `POST /api/v1/agent-enrollment/challenge`
- `POST /api/v1/agent-enrollment/requests`
- `POST /api/v1/agent-enrollment/requests/{id}/status`
- `POST /api/v1/agent-enrollment/requests/{id}/claim`

Owner-only, recently reauthenticated grant routes:

- `GET/POST /api/v1/admin/agent-enrollment/grants`
- `GET/PUT /api/v1/admin/agent-enrollment/grants/{id}`
- `POST /api/v1/admin/agent-enrollment/grants/{id}/rotate`
- `POST /api/v1/admin/agent-enrollment/grants/{id}/revoke`

Owner and admin request routes match existing operational node-management
authority:

- `GET /api/v1/admin/agent-enrollment/requests`
- `GET /api/v1/admin/agent-enrollment/requests/{id}`
- `POST /api/v1/admin/agent-enrollment/requests/{id}/approve`
- `POST /api/v1/admin/agent-enrollment/requests/{id}/reject`

All list routes use bounded cursor pagination and allowlisted filters. API
responses show group, grant, node, and actor names; IDs remain opaque values and
never become visible selector labels.

## Abuse and network controls

- Enrollment uses the canonical client IP from the deployed trusted-proxy
  helper. Untrusted `Forwarded` and `X-Forwarded-For` values are ignored.
- CIDR matching uses normalized addresses. Rate limits combine exact IP,
  normalized IPv6 `/64`, grant, key fingerprint, and a global bound.
- Default limits bound challenge creation, failed signatures, request creation,
  polling, claim attempts, active challenges, and total pending requests.
- Enrollment bodies default to 32 KiB, strings have explicit lengths, labels
  have count/size limits, and unknown JSON fields are rejected.
- Signatures cover length-delimited canonical fields; protocol version and
  Server origin prevent cross-protocol and cross-instance replay.
- Nonces, challenge consumption, request-token rotation, and claim-ticket
  consumption are transactional. Clock-skew windows are small and explicit.
- Public failures are generic. Structured logs and audits contain stable reason
  codes but no authorization headers, secrets, signatures, or full public keys.
- The Server never resolves or dials the Agent-reported host during enrollment.

## Administration experience

The node-management area gains a compact shadcn `Enrollment` view with three
tabs: `Pending approval`, `Enrollment grants`, and `Recent enrollments`. It is
an operational work surface, not a marketing dashboard.

Pending rows/cards show suggested hostname, trusted source IP, OS/architecture,
Agent version, grant name, first/last request time, and risk/status indicators.
The approval sheet exposes final node name, group by group name, fixed labels,
visibility, host, SSH port, and remarks. Conflicts and policy failures identify
the field that needs correction without exposing secrets.

Grant management shows name, mode, default group name, source scope, hostname
constraint, occupied/maximum slots, expiry, and status. Creation and rotation
use the existing one-time-secret dialog pattern; closing it clears plaintext
from React state. Revoke requires recent reauthentication and a clear impact
summary for pending and approved requests.

Desktop uses a dense queue with stable columns and a side sheet for review.
Mobile converts each request to one flat card with actions in an overflow menu;
cards are not nested. Status, expiry, and risk use icon-plus-text, not color
alone. Keyboard focus, screen-reader labels, loading skeletons, empty states,
pagination, stale-data refresh, and optimistic-action rollback are required.

New requests update through background polling or an authenticated event
stream without reloading the page. Every select trigger renders the chosen
human-readable name, with regression tests preventing raw IDs from appearing.

## Audit, metrics, readiness, and notifications

Redacted audit actions include grant create/update/rotate/revoke, challenge and
request rate-limit rejection, request create/resume, approve/reject/expire,
claim, recovery rotation, and policy conflict. Public protocol failures use an
anonymous actor and coarse source information; administrator actions retain the
authenticated actor and request ID.

Prometheus metrics include status counts, request/claim outcomes, challenge
expiry, rate-limit reasons, queue age, approval latency, claim latency, and
cleanup results. Labels are bounded enums; grant, request, node, hostname, and
IP values are never metric labels.

Readiness covers enrollment-store migrations and cleanup scheduler health, but
a non-empty or full pending queue is an operational alert rather than a
liveness failure. Alert recommendations cover sustained registration failures,
queue age, capacity exhaustion, repeated bad signatures, cleanup failure, and
claim recovery spikes. Optional discovery notifications use the existing
delivery service and contain no credentials.

## Backup, restore, deployment, and rollback

Enrollment data is SQLite-owned, so this feature does not itself increment the
active backup archive format. The current SQLite snapshot and restore path,
whether format 1 or a later approved format, covers the new tables. Restore
startup expires stale challenges/tickets before enrollment routes become
ready. Agent-side identity and configuration files remain host credentials and
are backed up only through the operator's host process.

Deployment order:

1. Back up binaries, static assets, SQLite, MTS, and the deployed Agent config.
2. Deploy the consecutive migration, Server, frontend, and Agent enrollment
   support with grant creation disabled.
3. Verify current per-node Agents, public APIs, MTS writes, network tasks,
   remote operations, backup, and restore are unchanged.
4. Enable one manual, one-slot, short-lived test grant restricted to the
   acceptance host CIDR.
5. Exercise request, approval, claim, report, revoke, retry, restart, and IPv6
   paths; verify no pending request appears publicly or in MTS.
6. Enable automatic grants only after quota, source, expiry, and abuse alarms
   are observed in production.

Before any claim, rollback may restore the old binary and matching database
backup. After migration, the old Server rejects the newer schema. After a node
claims, rollback also requires restoring its prior Agent configuration or
reissuing a token against the restored Server. No rollback attempts to recover
plaintext secrets.

## Test and acceptance gates

Backend tests cover randomness failures, hashing, constant-time comparison,
Ed25519 canonical signatures, origin binding, challenge/status/claim replay,
polling-token theft, expiry, every grant constraint, trusted/untrusted proxy
inputs, IPv4/IPv6 CIDRs, rate limits, quota concurrency, idempotent resume,
state transitions, transactional faults, name conflicts, grant revocation,
claim recovery, cleanup, audit redaction, and proof that pending credentials
cannot access any Agent capability.

Frontend tests cover name labels instead of IDs, filtering/pagination, approval
validation, one-time secret clearing, owner/admin authorization, recent reauth,
revoke impact, real-time refresh, error rollback, responsive cards, keyboard
operation, and empty/loading/error states.

Acceptance requires:

- manual and bounded automatic enrollment over IPv4 and IPv6;
- no public/MTS/task visibility before claim;
- independent per-node tokens after claim and immediate old-token rejection
  after recovery rotation or node revocation;
- no plaintext enrollment or node secret in SQLite, logs, audit, metrics,
  browser storage, URLs, process arguments, backups, or API reads;
- restart-safe challenge/request/claim behavior and concurrent quota proof;
- restore and rollback drills;
- at least 90 percent backend statement and frontend line coverage;
- race/shuffle tests, `goimports-reviser`, `golangci-lint`, `go vet`,
  `govulncheck`, module verification, production builds, and browser smoke
  tests aligned with CI.

## Approval boundary

Implementation requires explicit approval for the complete batch because it
changes shared API and Agent protocols, adds three SQLite tables and an
explicit node credential state, changes backup/restore validation, adds Agent
identity files and CLI/bootstrap behavior, and adds administrative workflows.
Approval must also confirm:

1. Ed25519 challenge/request/claim protocol and bounded recovery rotation.
2. Owner-only recent-reauth grant management; owner/admin approval authority.
3. Automatic grants using trusted source IP plus fixed SSH policy, never an
   Agent-reported connection target.
4. Migration numbering relative to OIDC and theme packages.
5. The one-way quota consumption rule and grant-revocation behavior.
