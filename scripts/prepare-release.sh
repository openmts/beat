#!/bin/bash
set -Eeuo pipefail
umask 077
script_dir=$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)

[[ $# == 1 && -d "$1" && ! -L "$1" ]] || { printf 'usage: prepare-release.sh <release-directory>\n' >&2; exit 2; }
: "${GITHUB_REPOSITORY:?release repository is required}" "${GITHUB_REF:?release tag is required}"
[[ "$GITHUB_REF" == refs/tags/v* ]] || { printf 'release requires a version tag\n' >&2; exit 1; }
cd "$1"
[[ -s checksums.txt && ! -L checksums.txt ]] || { printf 'release checksums are missing\n' >&2; exit 1; }
sha256sum --check --status checksums.txt
shopt -s nullglob
archives=(beat_*.tar.gz)
[[ ${#archives[@]} -gt 0 ]] || { printf 'release archives are missing\n' >&2; exit 1; }
prepare_dir=$(mktemp -d .release-prepare.XXXXXX)
trap 'rm -rf -- "$prepare_dir"' EXIT
assets=()
for archive in "${archives[@]}"; do
    sbom="$archive.sbom.json"
    [[ -f "$archive" && ! -L "$archive" && -s "$sbom" && ! -L "$sbom" ]] || { printf 'archive or matching SBOM is missing\n' >&2; exit 1; }
    python3 "$script_dir/state_archive.py" --release "$archive"
    python3 -c 'import json,sys; value=json.load(open(sys.argv[1])); assert isinstance(value,dict) and ("spdxVersion" in value or value.get("bomFormat")=="CycloneDX"), "invalid SBOM"' "$sbom"
    assets+=("$archive" "$sbom")
done
# 签名覆盖最终发布的归档和 SBOM；签名或验证失败时调用方不能发布。
sha256sum -- "${assets[@]}" >"$prepare_dir/checksums.txt"
cosign sign-blob --yes --bundle "$prepare_dir/checksums.txt.bundle" "$prepare_dir/checksums.txt"
[[ -s "$prepare_dir/checksums.txt.bundle" && ! -L "$prepare_dir/checksums.txt.bundle" ]]
cosign verify-blob --bundle "$prepare_dir/checksums.txt.bundle" \
    --certificate-identity "https://github.com/$GITHUB_REPOSITORY/.github/workflows/release.yml@$GITHUB_REF" \
    --certificate-oidc-issuer https://token.actions.githubusercontent.com "$prepare_dir/checksums.txt"
mv -- "$prepare_dir/checksums.txt" checksums.txt
mv -- "$prepare_dir/checksums.txt.bundle" checksums.txt.bundle
