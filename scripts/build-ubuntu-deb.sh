#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
[[ "$(uname -s)" == Linux && "$(uname -m)" == x86_64 ]] || { echo 'Build this package on Linux x86_64.' >&2; exit 1; }
VERSION="${OPENSURGE_DEB_VERSION:-$(cat "$ROOT/packaging/linux/VERSION")}"
[[ "$VERSION" =~ ^[0-9][0-9A-Za-z.+~_-]*$ ]] || { echo 'Invalid Debian version' >&2; exit 1; }
# GitHub sanitizes '~' in release asset names. Keep Debian's version syntax
# inside the package, with a portable public filename and matching checksum.
PACKAGE_FILE="opensurge-gateway_${VERSION//\~/-}_amd64.deb"
CORE="${OPENSURGE_LINUX_CORE:-$ROOT/bin/mihomo-linux-amd64}"
[[ -x "$CORE" ]] || { echo 'Build the pinned Linux Mihomo first.' >&2; exit 1; }
file "$CORE" | grep -q 'ELF 64-bit.*x86-64' || { echo 'Wrong core format/architecture' >&2; exit 1; }
OUT="${OPENSURGE_DEB_OUTPUT:-$ROOT/artifacts/ubuntu}"
mkdir -p "$OUT"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
STAGE="$WORK/package"
mkdir -p "$STAGE/DEBIAN" "$STAGE/usr/lib/opensurge" "$STAGE/usr/sbin" "$STAGE/usr/lib/systemd/system" "$STAGE/usr/share/doc/opensurge-gateway"
cd "$ROOT"
for cmd in omg opensurge-helper opensurge-control; do
 CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags='-s -w' -o "$STAGE/usr/lib/opensurge/$cmd" "./cmd/$cmd"
done
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags='-s -w' -o "$STAGE/usr/sbin/opensurge-admin" ./cmd/opensurge-admin
install -m 0755 "$CORE" "$STAGE/usr/lib/opensurge/mihomo"
install -m 0644 packaging/linux/*.service "$STAGE/usr/lib/systemd/system/"
for script in postinst prerm postrm; do install -m 0755 "packaging/linux/$script" "$STAGE/DEBIAN/$script"; done
install -m 0644 LICENSE "$STAGE/usr/share/doc/opensurge-gateway/copyright"
mkdir -p "$STAGE/usr/share/doc/opensurge-gateway/licenses"
install -m 0644 third_party/licenses/* "$STAGE/usr/share/doc/opensurge-gateway/licenses/"
install -m 0644 THIRD_PARTY_NOTICES.md "$STAGE/usr/share/doc/opensurge-gateway/THIRD_PARTY_NOTICES.md"
install -m 0644 packaging/linux/THIRD-PARTY-NOTICES.md "$STAGE/usr/share/doc/opensurge-gateway/UBUNTU-NOTICES.md"
install -m 0644 docs/ubuntu-install.md "$STAGE/usr/share/doc/opensurge-gateway/README.md"
git rev-parse HEAD > "$STAGE/usr/share/doc/opensurge-gateway/source-commit"
cat > "$STAGE/DEBIAN/control" <<EOF
Package: opensurge-gateway
Version: $VERSION
Architecture: amd64
Maintainer: OpenSurge fork contributors
Depends: adduser, ca-certificates, dnsmasq-base, iproute2, nftables, procps, systemd (>= 255), systemd-resolved
Section: net
Priority: optional
Homepage: https://github.com/kc7899/OpenSurge-for-Mac
Description: OpenSurge Ubuntu IPv4 gateway and Web console (preview)
 Reuses the OpenSurge stable routing and subscription control plane.
 No gateway takeover on installation. Same-LAN IPv4 only.
EOF
dpkg-deb --root-owner-group --build "$STAGE" "$OUT/$PACKAGE_FILE"
install -m 0644 docs/ubuntu-install.md "$OUT/INSTALL.md"
{
 printf 'OpenSurge Ubuntu preview %s\nSource: ' "$VERSION"
 git rev-parse HEAD
 printf 'Repository: https://github.com/kc7899/OpenSurge-for-Mac\nMihomo: 1.19.33-opensurge.1 (pinned patched source)\n'
 sha256sum "$CORE"
} > "$OUT/BUILD-INFO.txt"
cd "$OUT"
sha256sum "$PACKAGE_FILE" > SHA256SUMS
echo "Package: $OUT/$PACKAGE_FILE"
