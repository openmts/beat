#!/bin/bash
set -Eeuo pipefail
umask 077
script_dir=$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
repository_root=$(CDPATH= cd -- "$script_dir/.." && pwd)

[[ $# == 1 && -d "$1" && ! -L "$1" ]] || { printf 'usage: prepare-release.sh <release-directory>\n' >&2; exit 2; }
: "${GITHUB_REPOSITORY:?release repository is required}" "${GITHUB_REF:?release tag is required}" "${GITHUB_REF_NAME:?release tag name is required}" "${GITHUB_SHA:?release commit is required}" "${BEAT_RELEASE_SIGNING_KEY:?Agent release signing key secret is required}"
[[ "$GITHUB_REF" == refs/tags/v* ]] || { printf 'release requires a version tag\n' >&2; exit 1; }
cd "$1"
dist_dir=$PWD
[[ -s checksums.txt && ! -L checksums.txt ]] || { printf 'release checksums are missing\n' >&2; exit 1; }
sha256sum --check --status checksums.txt
shopt -s nullglob
archives=(beat_*.tar.gz)
[[ ${#archives[@]} -gt 0 ]] || { printf 'release archives are missing\n' >&2; exit 1; }
for archive in "${archives[@]}"; do
    sbom="$archive.sbom.json"
    [[ -f "$archive" && ! -L "$archive" && -s "$sbom" && ! -L "$sbom" ]] || { printf 'archive or matching SBOM is missing\n' >&2; exit 1; }
    python3 "$script_dir/state_archive.py" --release "$archive"
    python3 -c 'import json,sys; value=json.load(open(sys.argv[1])); assert isinstance(value,dict) and ("spdxVersion" in value or value.get("bomFormat")=="CycloneDX"), "invalid SBOM"' "$sbom"
done

python3 "$script_dir/prepare-agent-release.py" prepare .
prepare_dir=$(mktemp -d "$dist_dir/.release-prepare.XXXXXX")
trap 'rm -rf -- "$prepare_dir"' EXIT
printf '%s\n' "$BEAT_RELEASE_SIGNING_KEY" >"$prepare_dir/release.key"
chmod 0600 "$prepare_dir/release.key"
(
    cd "$repository_root"
    go build -trimpath -o "$prepare_dir/beat-cli" ./cmd/beat-cli
)
chmod 0700 "$prepare_dir/beat-cli"
(
    cd "$dist_dir"
    "$prepare_dir/beat-cli" release sign \
      --spec beat-agent-release-spec.json \
      --key "$prepare_dir/release.key" \
      --out beat-agent-signed.json
)
python3 "$script_dir/prepare-agent-release.py" extract .
cosign sign-blob --yes --bundle beat-agent-manifest.sigstore.json beat-agent-manifest.json
[[ -s beat-agent-manifest.sigstore.json && ! -L beat-agent-manifest.sigstore.json ]]
cosign verify-blob --bundle beat-agent-manifest.sigstore.json \
    --certificate-identity "https://github.com/$GITHUB_REPOSITORY/.github/workflows/release.yml@$GITHUB_REF" \
    --certificate-oidc-issuer https://token.actions.githubusercontent.com beat-agent-manifest.json

assets=(beat_*.tar.gz beat_*.tar.gz.sbom.json beat-agent_linux_amd64 beat-agent_linux_arm64 \
    beat-agent-release-notes.txt beat-agent-release-spec.json beat-agent-manifest.json \
    beat-agent-manifest.sig beat-agent-manifest.sigstore.json)
sha256sum -- "${assets[@]}" >checksums.txt
cosign sign-blob --yes --bundle checksums.txt.bundle checksums.txt
[[ -s checksums.txt.bundle && ! -L checksums.txt.bundle ]]
cosign verify-blob --bundle checksums.txt.bundle \
    --certificate-identity "https://github.com/$GITHUB_REPOSITORY/.github/workflows/release.yml@$GITHUB_REF" \
    --certificate-oidc-issuer https://token.actions.githubusercontent.com checksums.txt
