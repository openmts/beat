#!/usr/bin/env bash
set -euo pipefail

assets_dir="${1:?assets directory is required}"
repository="${2:?repository is required}"
release_tag="${3:?release tag is required}"
version="${4:?version is required}"
commit="${5:?commit is required}"
published_at="${6:?published time is required}"

assets_dir="$(cd "$assets_dir" && pwd)"
base_url="https://github.com/${repository}/releases/download/${release_tag}"

asset_json() {
  local name="$1"
  local file="${assets_dir}/${name}"

  if [[ ! -f "$file" ]]; then
    echo "missing release asset: ${file}" >&2
    exit 1
  fi

  local size sha256
  size="$(stat -c '%s' "$file")"
  sha256="$(sha256sum "$file" | awk '{print $1}')"

  jq -cn \
    --arg name "$name" \
    --arg url "${base_url}/${name}" \
    --argjson size "$size" \
    --arg sha256 "$sha256" \
    '{name: $name, url: $url, size: $size, sha256: $sha256}'
}

agent_amd64="$(asset_json beat-agent-linux-amd64)"
agent_arm64="$(asset_json beat-agent-linux-arm64)"
server_amd64="$(asset_json beat-server-linux-amd64)"
server_arm64="$(asset_json beat-server-linux-arm64)"

jq -n \
  --arg version "$version" \
  --arg release_tag "$release_tag" \
  --arg repository "$repository" \
  --arg commit "$commit" \
  --arg published_at "$published_at" \
  --argjson agent_amd64 "$agent_amd64" \
  --argjson agent_arm64 "$agent_arm64" \
  --argjson server_amd64 "$server_amd64" \
  --argjson server_arm64 "$server_arm64" \
  '{
    schemaVersion: 1,
    version: $version,
    releaseTag: $release_tag,
    repository: $repository,
    commit: $commit,
    publishedAt: $published_at,
    assets: {
      agent: {amd64: $agent_amd64, arm64: $agent_arm64},
      server: {amd64: $server_amd64, arm64: $server_arm64}
    }
  }' > "${assets_dir}/beat-manifest.json"

jq -e '.schemaVersion == 1 and (.assets.agent | length == 2) and (.assets.server | length == 2)' \
  "${assets_dir}/beat-manifest.json" >/dev/null
