# Agent remote release management

Updated: 2026-10-20

The Agent version page discovers releases from the configured GitHub repository,
defaulting to `openmts/beat`. A GitHub release is only a distribution and
discovery channel. It is not a trust root, and the appearance of a release does
not change the fleet or authorize an automatic rollout.

## Operator flow

1. Open **Agent versions & staged upgrades → Version packages**.
2. Review the GitHub release catalog. The repository and release publication
   time are visible beside each version.
3. Select **Sync version** and complete the recent-authentication prompt.
4. Beat downloads the canonical manifest, Ed25519 signature, optional Sigstore
   bundle, and every artifact named by the verified manifest.
5. The existing release importer verifies the signature policy, manifest
   compatibility, exact byte size, SHA-256 digest, and artifact platform before
   marking a version available.
6. Create a rollout explicitly, choose targets and policy, and promote each wave.
   Synchronization never starts, promotes, or expands a rollout.

The manual signed import remains under **Manual signed import (fallback)** for
offline or emergency use.

## Release assets

A publishable release contains all of the following relevant assets:

```text
beat-agent-manifest.json
beat-agent-manifest.sig
beat-agent-manifest.sigstore.json
beat-agent_linux_amd64
beat-agent_linux_arm64
beat-agent-release-notes.txt
beat-agent-release-spec.json
checksums.txt
checksums.txt.bundle
beat_<version>_linux_amd64.tar.gz
beat_<version>_linux_arm64.tar.gz
beat_<version>_linux_<arch>.tar.gz.sbom.json
```

The direct Agent binaries are extracted from the reproducible GoReleaser
archives because the updater installs a verified binary, not a package archive.
The manifest names those exact files and pins their sizes and SHA-256 digests.
The Sigstore certificate is bound to the release workflow at the tag ref, while
the Ed25519 signature is checked against the public key configured on the server
and installed on Agents.

Tagged releases require these GitHub settings:

- secret `BEAT_RELEASE_SIGNING_KEY`: one base64 or hexadecimal Ed25519 private
  seed/private key;
- repository variable `BEAT_RELEASE_KEY_ID` (optional, default
  `beat-release`): must match the signing key ID in server and Agent trust.

`scripts/prepare-agent-release.py` and `scripts/prepare-release.sh` validate the generated assets without
executing them, creates the manifest, and verifies both signatures before the
draft release can be published.

## Server configuration

Remote discovery is enabled with the signed-release module. These optional
environment variables configure it:

| Variable | Default | Purpose |
| --- | --- | --- |
| `BEAT_AGENT_RELEASE_REPOSITORY` | `openmts/beat` | GitHub `owner/repository` used for discovery. |
| `BEAT_AGENT_RELEASE_API_URL` | `https://api.github.com` | GitHub API base URL; Git is not used. |
| `BEAT_AGENT_RELEASE_TOKEN` | empty | Optional read-only token for a private repository or higher rate limit. It is never returned by the API. |

The token should be a fine-grained GitHub token restricted to the release
repository with read-only **Contents** access. The repository API host and the
known GitHub asset redirect hosts are fixed at startup; arbitrary redirect
destinations are rejected. Responses and artifact sizes are bounded, and only
two remote synchronizations can run concurrently.

Without a token, public API rate limits apply. Exhaustion returns `429` with
`Retry-After`; the UI retains the last catalog until an explicit refresh.
Invalid repository/API settings fail server startup rather than silently
falling back to another source.

## Security invariants

- A remote release is not considered trusted until its manifest and all named
  artifacts pass existing server verification.
- Draft releases cannot be synchronized.
- The server fetches assets by numeric GitHub asset ID rather than trusting
  arbitrary URLs from the response.
- Release tags must be canonical `v`-prefixed semantic versions accepted by the
  Agent rollout protocol.
- Synchronization is an owner write with recent-authentication and audit
  enforcement from the existing rollout routes.
- Importing a release does not create or promote a rollout.
