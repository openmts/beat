# Agent rollout foundation

Status: implemented library foundation and backup integration; migration v15
remains unregistered. Central runtime, authenticated route registration and
end-to-end updater acceptance remain pending. `agentrollout.Config.Enabled`
defaults to false.

## Trust and portable protocol

`internal/agentrollout/protocol` contains the shared canonical JSON format,
manifest validation, SemVer/tag rules, Ed25519 verification and signed next-key
transition validation. `VerifyManifestSignature` accepts a `SigningTrust` taken
from an out-of-band public-key configuration. It does not read files, use SQLite,
start a process or verify Sigstore. An optional `v` prefix does not change a
release version; prerelease and build identifiers remain significant.

The Server adds `releasesignature.Verifier`: offline Sigstore evidence must pass
under the startup-configured root, issuer and exact identity/workflow policy.
`ReleaseTag` is derived from the already Ed25519-verified manifest version, never
from an independently supplied upload field. Signature verification runs outside
SQLite transactions. Owner/recent proof and trust revisions are checked again
before the admission transaction commits.

Enabled construction requires a verifier exposing both `Verify` and the
read-only `Policy() releasesignature.PolicyMetadata` contract. Pinned Ed25519
public keys and revisions, complete public Sigstore roots, root and verifier
SHA-256 digests, identity rules and policy revisions are persisted in SQLite.
Executable paths and temporary directories are excluded. These rows are backup
metadata, not an authority to configure a verifier. Startup compares them with
the out-of-band adapter. A new runtime policy invalidates cached verification
and pauses campaigns; a forged row under an existing revision is rejected.
Every cached release binds both its signing-key revision and Sigstore policy
revision. Proposed next keys are validated but are not automatically installed.

## Server workflow

- `Import` admits the immutable signed manifest. A version/channel cannot be
  silently replaced with different canonical content. Revoked releases stay
  revoked. `RevokeKey` and `RevokeRelease` stop new assignments.
- `PutArtifact` checks exact length and SHA-256 before publication. Artifacts
  use digest-derived paths, private directories and `0600` files. Verified
  re-import can repair corrupt regular cache files. Unsafe links and foreign
  ownership are rejected. No artifact is executed. A later SQLite failure may
  leave verified bytes in the digest cache; this does not claim filesystem
  rollback, and missing/unverified database entries cannot dispatch.
- `Create` resolves node/group/tag selectors to immutable target snapshots,
  including source version, name and platform. The initial campaign is paused.
- `Promote`, `Pause`, `Resume` and `Abort` require a current owner session and
  recent proof, and commit their audit evidence with their SQLite state.
  Promotion is manual. Complete, fresh health observations are required before
  widening a wave. Containers and unsupported, stale, busy, incompatible or
  undersized targets are held with explicit reasons.
- `Lease` requires a current per-node credential, checks release compatibility
  with both Server and Agent protocols, and limits concurrent assignments.
  Only nonce hashes persist. Lost offers are not silently replayed.
- `Observe` binds the node, attempt and nonce, checks current Server-side version
  and successful-report metadata, and applies bounded failure thresholds. Gaps
  or failures reset the observation window. A breach pauses the campaign.
- `Recover` records expired attempts as unknown and pauses their campaign,
  including campaigns that were already manually paused. Resuming uncertainty
  requires an explicit acknowledgment. No recovery path auto-promotes a wave.

## Integration still required

Register `store.MigrateAgentRollout` only through the central v15 migration after
v14 acceptance. Then wire authenticated routes, body/concurrency limits, current
actor/Agent identity propagation, startup/periodic recovery, readiness and
bounded operational metrics. Ordinary library calls are not HTTP authorization.

The production backup/restore path now calls `agentrollout.ReconcileRestore` on
the private staged SQLite database before replacement. It pauses unfinished
campaigns, clears all target/attempt nonces and cached verification, and marks
artifacts missing. Completed/aborted campaigns keep their terminal state.
Ignored writes, overwritten results and failed audit writes roll back the
reconciliation; failure discards staging before replacing live files.

Backup format v4 records an optional `agent_release_cache` section with
`excluded`, `requires_verified_reimport` and sorted release/artifact IDs,
SHA-256 digests and sizes. The list must match canonical manifests and artifact
rows in the private SQLite snapshot. No cached executable enters the archive.
Old snapshots without the module are accepted; partial tables, forged views or
an inconsistent v15 ledger are rejected. Archived expiry/revocation remains
historical metadata. Startup must construct trust from out-of-band configuration,
never from archived roots; archive validation does not run a signature verifier.

Both writer and reader enforce the 1 MiB backup-manifest limit. Release and
artifact inventories and historical trust-key rows are bounded at 2,048 each,
individual signed payloads at 1 MiB, signature bundles at 4 MiB, and cumulative
payload scanning at 16 MiB. Oversized archives fail before
publication instead of producing an unreadable ready backup.

Integration tests perform a real archive/ApplyPending roundtrip using the
pending DDL on private fixtures. They verify pause/nonce/cache state, one restore
audit on repeated ApplyPending, unchanged source state, and preservation of live
database/key/MTS files on reconciliation failure. This does not substitute for
the still-pending registered-v15 deployment and end-to-end rollout gate.

The Agent helper/poller and owner release UI are implemented in separate
batches. Runtime integration, signed initial installation, full rollout/rollback,
notifications and native platform acceptance remain open. Agent integration must
use the pure protocol verifier and respect the campaign observation/freshness
policy. This foundation is not a completed self-update or platform-support claim.
