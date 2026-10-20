# Agent rollout HTTP contract (v15, not registered)

`internal/agentrollouthttp.New(service).Register(mux)` registers only the isolated
handler. Runtime/router/schema registration and trusted signing configuration
remain separate integration work; rollout is disabled by default. Wrap owner
routes with the existing session and CSRF middleware and Agent routes with the
active per-node authentication middleware. The service independently rechecks
current credentials/owner authority inside its transactions.

All JSON responses use `{code,message,data}`. Success is HTTP 200, including
`data:null` for no assignment or completed commands. Errors use fixed redacted
messages: 400 invalid payload/signature, 403 authority denied, 404 unavailable
resource or disabled module, 409 conflicting state, 428 recent proof required,
503 dependency or concurrency capacity failure. Busy responses include
`Retry-After: 1`. Responses set no-store, nosniff and no-referrer.

## Owner routes

All paths below start with `/api/v1/admin/`. Reads require a current owner
session; writes additionally require recent proof, rechecked before commit.

| Method | Path | Input / data |
| --- | --- | --- |
| GET | `agent-rollout-status` | `Status`, also readable when disabled |
| GET | `agent-releases` | Pagination / `Page<Release>` |
| GET | `agent-releases/{release_id}` | `ReleaseDetail` |
| POST | `agent-releases/import` | `SignedManifest` / `Release` |
| PUT | `agent-releases/{release_id}/artifacts/{artifact_id}` | Raw `application/octet-stream` / null |
| POST | `agent-releases/{release_id}/revoke` | Empty / null |
| GET | `agent-rollouts` | Pagination / `Page<Rollout>` |
| POST | `agent-rollouts` | `CreateInput` / `Rollout` |
| GET | `agent-rollouts/{rollout_id}` | `Rollout` |
| GET | `agent-rollouts/{rollout_id}/targets` | Pagination / `Page<Target>` |
| POST | `agent-rollouts/{rollout_id}/promote` | Empty / null |
| POST | `agent-rollouts/{rollout_id}/pause` | Empty / null |
| POST | `agent-rollouts/{rollout_id}/resume` | `{acknowledge_uncertainty:boolean}` / null |
| POST | `agent-rollouts/{rollout_id}/abort` | Empty / null |

Pagination accepts `limit` (default 50, 1–100) and opaque `cursor`. Data is
`{items:[],next_cursor:""}`; pass `next_cursor` unchanged, and empty means end.
Ordering is stable by ID. Targets use their immutable snapshot metadata. Target
pages and rollout details also return `server_time` (RFC3339), sampled by the
service for that read. Other pages do not include this field.

`CreateInput` fields: `name`, `release_id`, `node_ids`, `group_ids`, `tags`,
`policy`, `action` (`upgrade`, default, or `rollback`), `acknowledge_rollback`.
Rollback requires the explicit acknowledgement, persists its action, emits
`rollout.rollback.create`, and permits only a strictly older target version.
It uses the same signature, trust, compatibility, observation and manual-wave
checks as upgrade. Existing assignments are not silently canceled by rollback.

`Rollout`: `id`, `release_id`, `name`, `action`, `state`, `reason`, `current_wave`,
`total_waves`, `policy`. Policy fields are the JSON fields in
`agentrollout.Policy`; observation is 30–3600 seconds. Created rollouts are paused
awaiting explicit promotion. Pause/abort describe server dispatch state; they do
not claim to undo changes already applied by an Agent.

`Release`: `id`, `manifest`, `verified`, `revoked`. `ReleaseDetail` adds
`artifacts:[{id,state,size,sha256}]`. `Target`: `id`, `node_id`, `name`,
`source_version`, `os`, `arch`, `state`, `reason`, `wave`, `healthy_since`,
`last_report_at`, `lease_expires_at`. The three target times are actual persisted
values, encoded as RFC3339 or null; the UI must not infer missing samples. Owner metadata omits
signature bundles, nonce values/hashes and internal filesystem paths.

`Status` contains `enabled`, `configured`, `ready`, and fixed `reason`:
`disabled`, `unconfigured`, `unavailable`, or `ready`. A disabled instance can
answer without the pending v15 tables. Configured means the service has loaded
its signing policy, verifier and keys; ready additionally requires the current
persisted policy and at least one valid, unrevoked matching key. Owner/session
query failures return the usual 403/503 instead of claiming disabled. Reads need
no recent proof. Initialization still rejects invalid enabled configuration;
this endpoint does not change the existing service startup contract.

SignedManifest binary fields use standard JSON base64: `payload`, `signature`,
`sigstore_bundle`. Payload itself is the canonical protocol manifest JSON.
Request JSON is capped at 2 MiB; unknown fields, extra JSON values and compressed
request bodies are rejected.

## Agent routes

| Method | Path | Input / data |
| --- | --- | --- |
| POST | `/api/v1/agents/rollout/lease` | Protocol `Capabilities` / `Assignment` or null |
| POST | `/api/v1/agents/rollout/observations` | Protocol `Observation` / null |
| GET | `/api/v1/agents/rollout/artifacts/{assignment_id}/{artifact_id}` | Header nonce / binary stream |

Assignment includes the persisted `action`, real server `server_schema`, signed
manifest, artifact metadata and a single `expires_at`. Its initial deadline is
15 minutes preparation plus the configured observation window (at most 75
minutes total), clamped to manifest and maintenance-window expiry. Observations
never renew this deadline. A lost lease response does not regenerate a nonce or
silently redispatch; expiry enters the existing uncertainty/recovery workflow.

Downloads require `X-Beat-Rollout-Nonce`; query strings are rejected. Only the
exact artifact persisted for that assignment is authorized. The endpoint
supports one RFC byte range, ETag (`"sha256-<digest>"`), Content-Length and
Content-Range, and serves identity encoding. Multi-range requests are rejected.
The Agent retains its own cumulative ten-minute download budget from VerifiedAt;
server streams have a ten-minute deadline bounded by assignment expiry.

At most 16 downloads and 4 uploads are active per service/handler. Each download
opens and hashes a bounded private regular file outside SQLite transactions,
then rechecks authority before use. Reads recheck current active identity,
nonce, original lease, rollout window, release/signature trust and ready artifact
metadata, and reject file size/mtime changes. No long file I/O runs within the
SQLite write transaction. A client must still verify its completed artifact.

Upload stages, hashes and publishes a content-addressed private cache file
before its final transaction. That transaction rechecks owner/recent authority,
release/trust and artifact metadata, then atomically marks ready and audits.
If final authorization/audit/commit fails, a verified private cache file may
remain, but the artifact is not marked ready and is not dispatchable. This is
not a claimed cross-filesystem/database rollback.

## Integration boundaries

The pending v15 DDL adds `agent_rollouts.action` and the assignment's selected
`agent_rollout_targets.artifact_id`. Do not centrally register or deploy it before
the coordinated v15 integration gate. Root signing/trust policy and backup
reconciliation stay under the runtime owner. Route logs must never capture
nonce headers, Authorization headers or observation/lease bodies.

## Isolated verification (2026-10-08)

- `GOMAXPROCS=4 go test -race -shuffle=20261008 -count=1 -timeout=10m`
  for `internal/agentrollout` and `protocol`: passed, **90.8% / 99.4%**;
  service elapsed 158.619 seconds.
- The same fixed-seed race run for `internal/agentrollouthttp`: passed,
  **99.5%**, 8.051 seconds.
- The subsequently added initial lease/observation/expiry-boundary regression
  passed its own fixed-seed race run (2.811 seconds).
- `go vet`, `golangci-lint` (zero issues), `goimports-reviser`, production
  function length/complexity and diff checks passed. Commands unset the stale
  environment `GOROOT` before invoking the toolchain.

These are package checks; central runtime/router/backup integration and the
complete CI gate remain required before registration or deployment.

The owner status and stored target-time additions passed fixed-seed targeted
race tests (service 5.888s, HTTP 2.717s), both full package regressions (6.617s /
0.353s), and zero-issue lint on the same date. Disabled status without v15 tables,
owner/no-recent authorization, unavailable trust, nullable times and server time
are explicitly covered.
