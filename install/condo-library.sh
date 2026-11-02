#!/usr/bin/env bash
# Installs the newest condo-library release:
#   curl -fsSL https://raw.githubusercontent.com/imabee-glacier/condo-library/main/install/condo-library.sh | bash
# CONDO_INSTALL_DIR sets the target (default ~/.local/bin); CONDO_VERSION pins a version (e.g. 0.2.0).
set -euo pipefail

repo=${CONDO_REPO:-imabee-glacier/condo-library}
github=${CONDO_GITHUB_URL:-https://github.com}
api=${CONDO_API_URL:-https://api.github.com}
dir=${CONDO_INSTALL_DIR:-$HOME/.local/bin}

case "$(uname -s)-$(uname -m)" in
  Linux-x86_64) triple=x86_64-unknown-linux-gnu ;;
  *) echo "condo-library: no prebuilt binary for $(uname -s) $(uname -m)" >&2; exit 1 ;;
esac

if [ -n "${CONDO_VERSION:-}" ]; then
  version=${CONDO_VERSION#v}
else
  version=$(curl -fsSL "$api/repos/$repo/releases?per_page=100" \
    | grep -o '"tag_name": *"condo-library-v[0-9]*\.[0-9]*\.[0-9]*"' \
    | sed 's/.*condo-library-v//; s/"$//' | sort -V | tail -n 1)
  [ -n "$version" ] || { echo "condo-library: no release found for $repo" >&2; exit 1; }
fi
tag=condo-library-v$version
asset=condo-library-$version-$triple.tar.gz

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

echo "Installing condo-library $version to $dir"
curl -fsSL -o "$tmp/$asset" "$github/$repo/releases/download/$tag/$asset"
curl -fsSL -o "$tmp/$asset.sha256" "$github/$repo/releases/download/$tag/$asset.sha256"
(cd "$tmp" && sha256sum -c "$asset.sha256" > /dev/null)

mkdir -p "$dir"
tar -xzf "$tmp/$asset" -C "$dir" ./condo
echo "Installed $("$dir/condo" --version)"
case ":$PATH:" in *":$dir:"*) ;; *) echo "Add $dir to your PATH to run condo." ;; esac
