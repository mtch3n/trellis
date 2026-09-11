#!/bin/sh
# Download and install the latest trellis release.
#
#   curl -fsSL https://raw.githubusercontent.com/mtch3n/trellis/main/install.sh | sh
#
# Override the destination with TRELLIS_INSTALL_DIR, or the version with
# TRELLIS_VERSION (for example TRELLIS_VERSION=v0.0.1).
set -eu

REPO="mtch3n/trellis"
INSTALL_DIR="${TRELLIS_INSTALL_DIR:-$HOME/.local/bin}"

die() {
	echo "install: $*" >&2
	exit 1
}

os=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$os" in
linux | darwin) ;;
*) die "unsupported operating system '$os'; download a release archive from https://github.com/$REPO/releases/latest" ;;
esac

arch=$(uname -m)
case "$arch" in
x86_64 | amd64) arch=amd64 ;;
aarch64 | arm64) arch=arm64 ;;
*) die "unsupported architecture '$arch'" ;;
esac

for tool in curl tar; do
	command -v "$tool" >/dev/null 2>&1 || die "$tool is required"
done

version="${TRELLIS_VERSION:-}"
if [ -z "$version" ]; then
	version=$(curl -fsSL "https://api.github.com/repos/$REPO/releases/latest" |
		sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' |
		head -n 1)
	[ -n "$version" ] || die "could not determine the latest release"
fi

asset="trellis_${os}_${arch}.tar.gz"
url="https://github.com/$REPO/releases/download/$version/$asset"

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT INT TERM

echo "Downloading trellis $version ($os/$arch)..."
curl -fsSL "$url" -o "$tmp/$asset" || die "download failed: $url"
tar -xzf "$tmp/$asset" -C "$tmp" || die "could not extract $asset"
[ -f "$tmp/trellis" ] || die "the archive did not contain a trellis binary"

mkdir -p "$INSTALL_DIR"
# Install through a temporary name so an in-use binary is replaced atomically
# rather than truncated underneath a running process.
chmod 0755 "$tmp/trellis"
mv "$tmp/trellis" "$INSTALL_DIR/trellis.new"
mv "$INSTALL_DIR/trellis.new" "$INSTALL_DIR/trellis"

echo "Installed trellis $version to $INSTALL_DIR/trellis"
case ":$PATH:" in
*":$INSTALL_DIR:"*) ;;
*) echo "Add it to your PATH:  export PATH=\"$INSTALL_DIR:\$PATH\"" ;;
esac
