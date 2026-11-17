#!/usr/bin/env bash
set -euo pipefail
# This test installs into a disposable GitHub Actions Ubuntu VM. It must never
# be run on the Mac or on an existing user's Linux gateway.
[[ "${GITHUB_ACTIONS:-}" == true && "$(uname -s)" == Linux ]] || { echo 'Disposable Ubuntu Actions runner required.' >&2; exit 1; }
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
[[ ! -e /var/lib/opensurge ]] || { echo 'Refusing to overwrite an existing installation' >&2; exit 1; }
VERSION="$(cat "$ROOT/packaging/linux/VERSION")"
PKG="$ROOT/artifacts/ubuntu/opensurge-gateway_${VERSION//\~/-}_amd64.deb"
dpkg-deb --info "$PKG"
sudo apt-get install -y "$PKG"
sudo systemctl is-active --quiet opensurge-helper opensurge-control
sudo systemd-analyze verify /usr/lib/systemd/system/opensurge-{helper,control}.service
test -s /usr/share/doc/opensurge-gateway/UBUNTU-NOTICES.md
test -s /usr/share/doc/opensurge-gateway/licenses/react-MIT.txt
test -s /usr/share/doc/opensurge-gateway/source-commit
# Service installation must not start a gateway or advertise/alter addressing.
sudo test ! -e /var/lib/opensurge/runtime/state.json
# The control identity can use the fixed helper channel, but cannot provide an
# arbitrary probe URL, executable or unbounded target number.
sudo -u opensurge python3 - <<'PY'
import json, socket
s = socket.socket(socket.AF_UNIX)
s.connect('/run/opensurge/helper.sock')
s.sendall(json.dumps({'action':'stable-probe','config_path':'/var/lib/opensurge/config.yaml','stable_probe':{'target':99,'candidate_id':'not-a-node'}}).encode() + b'\n')
r = json.loads(s.makefile().readline())
assert not r['ok'] and 'invalid stable probe target' in r['error'], r
s.close()
PY
if ip link show opensurge0 >/dev/null 2>&1; then echo 'Installer started TUN unexpectedly' >&2; exit 1; fi
test "$(curl -s -o /dev/null -w '%{http_code}' http://127.0.0.1:61767/api/v1/overview)" == 401
[[ "$(sudo stat -c %U:%G:%a /var/lib/opensurge/config.yaml)" == root:opensurge:640 ]]
[[ "$(sudo stat -c %U:%G:%a /var/lib/opensurge/control/control-token)" == opensurge:opensurge:600 ]]
WORK="$(mktemp -d)"; trap 'rm -rf "$WORK"' EXIT
# Keep administrator login links/cookies private and out of CI logs.
sudo opensurge-admin console > "$WORK/console"
URL="$(sed -n 's@^http://127.0.0.1:16167/@http://127.0.0.1:61767/@p' "$WORK/console")"
[[ -n "$URL" ]]
curl -fsS -c "$WORK/cookies" "$URL" -o /dev/null
curl -fsS -b "$WORK/cookies" http://127.0.0.1:61767/api/v1/overview > "$WORK/overview.json"
curl -fsS -b "$WORK/cookies" -H 'Origin: http://127.0.0.1:16167' -H 'Content-Type: application/json' -X PUT -d '{"language":"zh-Hans"}' http://127.0.0.1:61767/api/v1/ui-preferences -o /dev/null
test "$(curl -s -o /dev/null -w '%{http_code}' -b "$WORK/cookies" -H 'Origin: https://untrusted.example' -H 'Content-Type: application/json' -X PUT -d '{"language":"en"}' http://127.0.0.1:61767/api/v1/ui-preferences)" == 403
python3 - "$WORK/overview.json" <<'PY'
import json, sys
o = json.load(open(sys.argv[1]))
assert o['platform'] == 'linux'
assert o['status']['gateway'] == 'stopped', o['status']
PY
curl -fsS -b "$WORK/cookies" http://127.0.0.1:61767/network > "$WORK/ui.html"
grep -q '/assets/' "$WORK/ui.html"
# Prove the non-root Web service can import a synthetic source and apply it
# through the fixed root helper, without taking over any runner network.
cat > "$WORK/fixture.yaml" <<'EOF'
proxies:
  - {name: 'US offline fixture', type: http, server: 192.0.2.1, port: 9}
proxy-groups:
  - {name: Proxy, type: select, proxies: ['US offline fixture']}
rules: ['MATCH,REJECT']
EOF
curl -fsS -b "$WORK/cookies" -H 'Origin: http://127.0.0.1:16167' -F "file=@$WORK/fixture.yaml" -F 'name=Offline package fixture' -F 'kind=mihomo_profile' http://127.0.0.1:61767/api/v1/sources > "$WORK/source.json"
SOURCE="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["id"])' "$WORK/source.json")"
REV="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["revision"])' "$WORK/overview.json")"
if ! curl --fail-with-body -sS --max-time 90 -b "$WORK/cookies" -H 'Origin: http://127.0.0.1:16167' -H "If-Match: \"$REV\"" -X POST "http://127.0.0.1:61767/api/v1/sources/$SOURCE/apply" > "$WORK/applied.json"; then
 python3 -c 'import json,sys; print("Synthetic source apply failed:", json.load(open(sys.argv[1])).get("error",{}).get("code","unknown"))' "$WORK/applied.json"
 exit 1
fi
curl -fsS -b "$WORK/cookies" http://127.0.0.1:61767/api/v1/overview > "$WORK/after-apply.json"
python3 - "$WORK/after-apply.json" <<'PY'
import json, sys
o = json.load(open(sys.argv[1]))
assert o['status']['gateway'] == 'stopped', o['status']
assert not o.get('status_error'), o.get('status_error')
PY
sudo test ! -e /var/lib/opensurge/runtime/state.json
# Upgrade preserves the generated core secret and private control store.
sudo cp /var/lib/opensurge/config.yaml "$WORK/config"
sudo sha256sum /var/lib/opensurge/control/control-token | cut -d' ' -f1 > "$WORK/token-digest"
sudo apt-get install --reinstall -y "$PKG"
sudo cmp "$WORK/config" /var/lib/opensurge/config.yaml
sudo sha256sum /var/lib/opensurge/control/control-token | cut -d' ' -f1 | cmp - "$WORK/token-digest"
sudo test ! -e /var/lib/opensurge/runtime/state.json
sudo apt-get remove -y opensurge-gateway
sudo cmp "$WORK/config" /var/lib/opensurge/config.yaml
if systemctl is-active --quiet opensurge-control; then echo 'Control service still active after removal' >&2; exit 1; fi
echo 'Debian install, loopback authentication, clean stopped state, config/token preservation and removal passed.'
