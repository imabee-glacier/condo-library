#!/usr/bin/env bash
# Installs the latest condo release: curl -fsSL <release-url>/install.sh | bash
# CONDO_INSTALL_DIR sets the target (default ~/.local/bin); CONDO_VERSION pins a version (e.g. 0.2.0).
set -euo pipefail

repo_url=${CONDO_REPO_URL:-https://github.com/imabee-glacier/condo-library}
dir=${CONDO_INSTALL_DIR:-$HOME/.local/bin}

case "$(uname -s)-$(uname -m)" in
  Linux-x86_64) triple=x86_64-unknown-linux-gnu ;;
  *) echo "condo: no prebuilt binary for $(uname -s) $(uname -m)" >&2; exit 1 ;;
esac

if [ -n "${CONDO_VERSION:-}" ]; then
  tag=condo-library-v${CONDO_VERSION#v}
else
  tag=$(curl -fsSLI -o /dev/null -w '%{url_effective}' "$repo_url/releases/latest")
  tag=${tag##*/}
fi
version=${tag#condo-library-v}
asset=condo-library-$version-$triple.tar.gz

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

echo "Installing condo $version to $dir"
curl -fsSL -o "$tmp/$asset" "$repo_url/releases/download/$tag/$asset"
curl -fsSL -o "$tmp/$asset.sha256" "$repo_url/releases/download/$tag/$asset.sha256"
(cd "$tmp" && sha256sum -c "$asset.sha256" > /dev/null)

mkdir -p "$dir"
tar -xzf "$tmp/$asset" -C "$dir" ./condo
echo "Installed $("$dir/condo" --version)"
case ":$PATH:" in *":$dir:"*) ;; *) echo "Add $dir to your PATH to run condo." ;; esac
