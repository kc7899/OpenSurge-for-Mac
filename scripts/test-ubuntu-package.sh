#!/usr/bin/env bash
set -euo pipefail
# This test installs into a disposable GitHub Actions Ubuntu VM. It must never
# be run on the Mac or on an existing user's Linux gateway.
[[ "${GITHUB_ACTIONS:-}" == true && "$(uname -s)" == Linux ]] || { echo 'Disposable Ubuntu Actions runner required.' >&2; exit 1; }
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
[[ ! -e /var/lib/opensurge ]] || { echo 'Refusing to overwrite an existing installation' >&2; exit 1; }
PKG="$ROOT/artifacts/ubuntu/opensurge-gateway_0.3.4~ubuntu.1_amd64.deb"
dpkg-deb --info "$PKG"
sudo apt-get install -y "$PKG"
sudo systemctl is-active --quiet opensurge-helper opensurge-control
sudo systemd-analyze verify /usr/lib/systemd/system/opensurge-{helper,control}.service
# Service installation must not start a gateway or advertise/alter addressing.
test ! -e /var/lib/opensurge/runtime/state.json
if ip link show opensurge0 >/dev/null 2>&1; then echo 'Installer started TUN unexpectedly' >&2; exit 1; fi
test "$(curl -s -o /dev/null -w '%{http_code}' http://127.0.0.1:61767/api/v1/overview)" == 401
[[ "$(stat -c %U:%G:%a /var/lib/opensurge/config.yaml)" == root:opensurge:640 ]]
[[ "$(stat -c %U:%G:%a /var/lib/opensurge/control/control-token)" == opensurge:opensurge:600 ]]
WORK="$(mktemp -d)"; trap 'rm -rf "$WORK"' EXIT
# Keep administrator login links/cookies private and out of CI logs.
sudo opensurge-admin console > "$WORK/console"
URL="$(sed -n 's@^http://127.0.0.1:16167/@http://127.0.0.1:61767/@p' "$WORK/console")"
[[ -n "$URL" ]]
curl -fsS -c "$WORK/cookies" "$URL" -o /dev/null
curl -fsS -b "$WORK/cookies" http://127.0.0.1:61767/api/v1/overview > "$WORK/overview.json"
python3 - "$WORK/overview.json" <<'PY'
import json, sys
o = json.load(open(sys.argv[1]))
assert o['platform'] == 'linux'
assert o['status']['gateway'] == 'stopped', o['status']
PY
curl -fsS -b "$WORK/cookies" http://127.0.0.1:61767/network > "$WORK/ui.html"
grep -q '/assets/' "$WORK/ui.html"
# Upgrade preserves the generated core secret and private control store.
sudo cp /var/lib/opensurge/config.yaml "$WORK/config"
sudo sha256sum /var/lib/opensurge/control/control-token | cut -d' ' -f1 > "$WORK/token-digest"
sudo apt-get install --reinstall -y "$PKG"
sudo cmp "$WORK/config" /var/lib/opensurge/config.yaml
sudo sha256sum /var/lib/opensurge/control/control-token | cut -d' ' -f1 | cmp - "$WORK/token-digest"
test ! -e /var/lib/opensurge/runtime/state.json
sudo apt-get remove -y opensurge-gateway
sudo cmp "$WORK/config" /var/lib/opensurge/config.yaml
if systemctl is-active --quiet opensurge-control; then echo 'Control service still active after removal' >&2; exit 1; fi
echo 'Debian install, loopback authentication, clean stopped state, config/token preservation and removal passed.'
