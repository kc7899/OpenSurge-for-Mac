#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
[[ "$(uname -s)" == Linux && "$(uname -m)" == x86_64 ]] || { echo 'Build this package on Linux x86_64.' >&2; exit 1; }
VERSION="${OPENSURGE_DEB_VERSION:-0.3.4~ubuntu.1}"
[[ "$VERSION" =~ ^[0-9][0-9A-Za-z.+~_-]*$ ]] || { echo 'Invalid Debian version' >&2; exit 1; }
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
dpkg-deb --root-owner-group --build "$STAGE" "$OUT/opensurge-gateway_${VERSION}_amd64.deb"
cd "$OUT"
sha256sum "opensurge-gateway_${VERSION}_amd64.deb" > SHA256SUMS
echo "Package: $OUT/opensurge-gateway_${VERSION}_amd64.deb"
