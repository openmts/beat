# Per-node Agent identity and lifecycle

Status: deployed identity boundary; credential-response and audit consistency
remain partial

Secure discovery is a separate, not-yet-approved extension. Its enrollment
grants never authenticate Agent reports, and accepted machines still activate
through the per-node token model defined here. See
[`agent-auto-discovery-design.md`](./agent-auto-discovery-design.md).

Audited Agent tasks and file transfers are another not-yet-approved extension.
They lease work only through middleware-resolved active node identity; one
node's token can never read or complete another node's attempt. See
[`remote-operations-design.md`](./remote-operations-design.md).

## Goal

Replace the shared Agent bearer token with a credential bound to exactly one
pre-created node. The authenticated node identity, rather than a name supplied
by the request body or query string, must authorize metric reports, network
assignments, and network results.

Komari creates a client UUID and token before the Agent connects, then resolves
the caller UUID from that token for client RPCs. Beat follows the capability
shape but strengthens secret storage: Beat stores only a token hash and shows
the plaintext token once.

## Current risk

`BEAT_AGENT_TOKEN` currently authenticates an Agent role, not a node. The
report endpoint accepts `name` in JSON, while network endpoints accept
`node_name` in the query or JSON body. Any holder of the shared token can
therefore claim another node's name and write metrics or network results under
that identity.

The final state must make that spoofing path impossible.

## Identity state

Each node has one of three credential states:

- `legacy`: no per-node token has ever been issued. The temporary migration
  path may accept the shared token only for an already existing node with the
  matching name.
- `active`: a per-node token hash exists and is not revoked. Only that token
  authenticates the node.
- `revoked`: a per-node token was issued and later revoked. No Agent request
  authenticates until an administrator rotates the token.

A node never returns from `active` or `revoked` to `legacy`. Token rotation
atomically replaces the hash and clears revocation. Deleting a node removes its
credential with the node.

The discovery design proposes a fourth `pending_claim` state for a hidden node
reservation. It is not part of the deployed schema yet and requires explicit
approval. `pending_claim` would reject both legacy and per-node Agent
authentication until a signed claim transaction generates the first token.

## SQLite application data

The existing `nodes` table gains nullable application-data columns:

- `agent_token_hash`: SHA-256 digest of the complete generated token.
- `agent_token_prefix`: non-secret display prefix used to identify a token.
- `agent_token_created_at`: issue or last rotation time.
- `agent_token_last_used_at`: last persisted authenticated use time.
- `agent_token_revoked_at`: revocation time, null for an active token.

No plaintext token is stored. These fields contain platform identity state,
not metrics, so SQLite remains the correct store. CPU, memory, traffic, probe,
and all other time-series samples remain in MTS.

The token hash has a unique partial index when non-null, and node names are
unique. Token issue is transactional; rotation atomically replaces all token
state in one update, and revocation atomically records its timestamp. Every
query is parameterized and context-aware.

`agent_token_last_used_at` is persisted at most once per five minutes per
node. This avoids a SQLite write for every five-second metrics report while
still giving useful lifecycle visibility.

The deployed identity boundary does not yet provide report body, rate, global
concurrency, or MTS admission budgets. Those availability controls are a
separate schema-free approval unit reviewed in
[`ingress-resource-governance-design.md`](./ingress-resource-governance-design.md);
they key primarily by this authenticated node ID and never move metric values
into SQLite.

## Token format

The server generates 32 random bytes with `crypto/rand` and encodes them with
unpadded base64url. The external token contains a fixed versioned prefix so it
is recognizable in configuration:

```text
beat_agent_v1_<base64url-random>
```

The server rejects generation if randomness cannot be read. Token comparisons
use a SHA-256 digest and constant-time comparison. Tokens and authorization
headers must never be logged.

The plaintext token appears only in the successful node-create or token-rotate
response. List and get responses expose only credential state, prefix, issue
time, last-used time, and revocation time.

The current handler can commit node creation or rotation and then fail while
building an MTS-backed response. That can return `500` after the one-time token
has become authoritative but before the administrator receives it. Response
preparation, atomic audit and explicit lost-response rotation recovery are
reviewed in
[`administrative-mutation-consistency-design.md`](./administrative-mutation-consistency-design.md).

## Authentication context

Agent authentication resolves a concrete identity and adds it to the request
context:

```text
AgentIdentity {
  NodeID
  NodeName
  Mode: per-node | legacy
}
```

Handlers fail closed if the identity is absent. The middleware and handler
contracts are:

- `POST /api/v1/nodes/report`: updates the authenticated node only. The body
  no longer authorizes a node name.
- `GET /api/v1/network/assignments`: returns tasks for the authenticated node;
  `node_name` is not accepted as an authorization selector.
- `POST /api/v1/network/results`: writes results for the authenticated node;
  a body `node_name`, if retained during migration, is ignored for identity.

Agent-facing error responses remain generic. A missing, unknown, malformed, or
revoked token returns HTTP 401 without revealing whether a token prefix or node
exists.

Administrator authentication and Agent authentication use separate router
switches. Disabling the legacy Agent token must not disable administrator
authorization.

## Administrator API

All routes require the deployed administrator session. The reviewed
[`administrative-mutation-consistency-design.md`](./administrative-mutation-consistency-design.md)
adds owner/admin recent-auth admission and non-cacheable responses for node
creation and Agent-token rotation/revocation without changing the Agent identity
or token-at-rest model:

- `POST /api/v1/nodes`
  - Pre-creates a node and issues its first token.
  - Requires a unique name, host, and valid SSH port.
  - Returns the node, one-time plaintext token, and complete Agent JSON
    configuration in the same response.
- `POST /api/v1/nodes/{id}/token/rotate`
  - Atomically replaces any previous credential.
  - Returns the one-time plaintext token and complete Agent JSON
    configuration in the same response.
- `POST /api/v1/nodes/{id}/token/revoke`
  - Revokes the current credential and returns no token.
- `GET /api/v1/nodes/{id}/install`
  - Returns a token-free Agent JSON template containing the requested validated
    server URL, node name, advertised host, SSH port, and report interval.

An install GET must not recover or redisplay an old plaintext token. If the
administrator has closed the one-time create or rotate dialog, rotation is
required to obtain a new complete configuration containing a token.

## Admin experience

The node page adds a create-node command and lifecycle actions on each card:

- Credential badge: legacy, active, or revoked.
- Agent version, issue time, and last-used time.
- Installation configuration.
- Rotate token.
- Revoke token.

Create and rotate open a one-time credential dialog. The dialog uses explicit
copy buttons for the JSON configuration and token, explains that the secret
cannot be shown again, and never places the token in a URL. Closing the dialog
clears the plaintext from React state.

Every node and group selector renders a human-readable label. IDs remain API
values only.

## Migration

The migration is deliberately one-way and bounded:

1. Deploy schema and dual Agent authentication.
2. A shared legacy token may authenticate only an already existing `legacy`
   node whose exact name is present in the request. It cannot create a node.
3. Issue a per-node token for the deployed `beat-host`.
4. Write the token to `agent.json` with mode `0600` and restart only the Beat
   Agent.
5. Verify reports, assignments, and probe results use the authenticated node
   ID.
6. Verify the shared token can no longer authenticate as `beat-host`.
7. After every node is migrated, unset the legacy Agent token while keeping
   administrator authentication enabled.

No automatic token issuance occurs on legacy reports. This prevents an Agent
holding the shared token from claiming a new name and converting it into a
per-node credential.

## Rollback

Before migration, back up the Beat binaries, static assets, SQLite database,
and Agent configuration with mode `0600` for files and `0700` for directories.

If the new Agent cannot report, restore its prior configuration and keep the
dual-auth Server running while diagnosing. Restoring an old Server binary is
allowed only together with the matching SQLite backup because the old binary
does not understand the credential columns or identity contract.

Revoking or rotating a token is not rolled back by recovering plaintext: the
old secret is intentionally unavailable. Recovery issues a new token.

## Threat and acceptance matrix

| Threat | Required evidence |
| --- | --- |
| Shared-token node spoofing | Shared token cannot authenticate an active or revoked node |
| Per-node token cross-node write | Token for node A cannot report or submit network results as node B |
| Unauthorized assignment read | Node A receives only assignments resolved from its authenticated ID |
| Database disclosure | SQLite contains token hashes and prefixes but no plaintext token |
| Token replay after rotation | Old token returns 401 immediately after successful rotation |
| Token replay after revocation | Revoked token returns 401 |
| Token enumeration | Unknown and revoked tokens return the same generic response |
| Write amplification | Repeated reports inside five minutes do not repeatedly update last-used time |
| Admin regression | Public pages stay open and administrator routes remain protected |
| Storage regression | Metric and probe samples still exist only in MTS |

Backend tests cover token generation, hashing, collision/failure handling,
state transitions, migration restrictions, context identity, cross-node
rejection, last-used throttling, transactions, every error path, unavailable MTS
after token commit, and lost-response recovery by explicit rotation. Frontend
tests must cover labels, one-time secret clearing, create/rotate/revoke
confirmation, ambiguous-response guidance, copy controls, and responsive card
behavior.

Repository quality gates and production build are required before deployment.
Final verification includes IPv6 smoke tests, live Agent reports with a
per-node token, and proof that the legacy token cannot impersonate the migrated
node.
