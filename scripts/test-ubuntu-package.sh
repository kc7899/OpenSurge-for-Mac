#!/usr/bin/env bash
set -euo pipefail
# This test installs into a disposable GitHub Actions Ubuntu VM. It must never
# be run on the Mac or on an existing user's Linux gateway.
[[ "${GITHUB_ACTIONS:-}" == true && "$(uname -s)" == Linux ]] || { echo 'Disposable Ubuntu Actions runner required.' >&2; exit 1; }
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
[[ ! -e /var/lib/opensurge ]] || { echo 'Refusing to overwrite an existing installation' >&2; exit 1; }
WORK="$(mktemp -d)"; trap 'rm -rf "$WORK"' EXIT
# Enable Ubuntu's stock dnsmasq profile for this disposable VM only, after
# other namespace tests have finished using temporary, non-production paths.
sudo apt-get install -y apparmor-profiles
sudo test -f /etc/apparmor.d/usr.sbin.dnsmasq
# aa-enforce's Python loader scans unrelated runner profiles and cannot parse
# some kernel mount rules. Edit only this disposable stock profile's mode and
# use the real parser directly (as the package does), keeping enforcement.
sudo python3 - <<'PY'
from pathlib import Path
import re
p = Path('/etc/apparmor.d/usr.sbin.dnsmasq')
def enforce(m):
    flags = [f.strip() for f in m[1].split(',') if f.strip() != 'complain']
    return 'flags=(' + ','.join(flags) + ')' if flags else ''
p.write_text(re.sub(r'flags=\(([^)]*)\)', enforce, p.read_text()))
PY
sudo apparmor_parser -r -W -T /etc/apparmor.d/usr.sbin.dnsmasq
sudo grep -Eq '(^|/)dnsmasq \(enforce\)$' /sys/kernel/security/apparmor/profiles
sudo cat /etc/apparmor.d/usr.sbin.dnsmasq > "$WORK/distribution-profile"
printf '# Administrator local rules sentinel\n' | sudo tee -a /etc/apparmor.d/local/usr.sbin.dnsmasq >/dev/null
VERSION="$(cat "$ROOT/packaging/linux/VERSION")"
PKG="$ROOT/artifacts/ubuntu/opensurge-gateway_${VERSION//\~/-}_amd64.deb"
dpkg-deb --info "$PKG"
sudo apt-get install -y "$PKG"
sudo systemctl is-active --quiet opensurge-helper opensurge-control
sudo systemd-analyze verify /usr/lib/systemd/system/opensurge-{helper,control}.service
sudo cmp "$WORK/distribution-profile" /etc/apparmor.d/usr.sbin.dnsmasq
for geo_dir in /var/lib/opensurge/data /var/lib/opensurge/runtime; do
 sudo sh -ec 'cd "$1"; sha256sum -c /usr/share/opensurge/geodata/SHA256SUMS' _ "$geo_dir"
done
# The actual root import runner must validate both DAT and MetaDB profiles in
# a namespace with no external interfaces, DNS or Internet route.
go test -c -o "$WORK/offline-import.test" "$ROOT/internal/controlapi"
sudo env OPENSURGE_OFFLINE_IMPORT_TEST=1 OPENSURGE_TEST_CORE=/var/lib/opensurge/bin/mihomo OPENSURGE_TEST_GEODATA=/var/lib/opensurge/data unshare --net "$WORK/offline-import.test" -test.run '^TestUbuntuOfflineImportedGeodata$' -test.v
# Reproduce the reported denial with the exact installed runtime path. Remove
# only our include, verify denial, then let real postinst restore it on reinstall.
sudo tee /var/lib/opensurge/runtime/dnsmasq.conf >/dev/null <<'EOF'
port=15353
interface=lo
bind-interfaces
no-resolv
no-hosts
address=/opensurge-fixture.test/192.0.2.15
pid-file=/var/lib/opensurge/runtime/dnsmasq.pid
EOF
sudo sh -eu -c '. "$1"; opensurge_apparmor remove' _ "$ROOT/packaging/linux/apparmor-functions"
if sudo /usr/sbin/dnsmasq --test --conf-file=/var/lib/opensurge/runtime/dnsmasq.conf > "$WORK/denied" 2>&1; then
 echo 'AppArmor baseline unexpectedly allowed the runtime configuration' >&2; exit 1
fi
grep -q 'Permission denied' "$WORK/denied"
sudo apt-get install --reinstall -y "$PKG"
sudo env GITHUB_ACTIONS="$GITHUB_ACTIONS" python3 "$ROOT/scripts/test-ubuntu-apparmor.py"
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
rules: ['GEOSITE,cn,DIRECT', 'GEOIP,CN,DIRECT,no-resolve', 'IP-ASN,13335,Proxy,no-resolve', 'MATCH,REJECT']
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
sudo stat -c '%i:%Y:%s' /var/lib/opensurge/data/GeoIP.dat > "$WORK/cache-stat"
sudo apt-get install --reinstall -y "$PKG"
sudo cmp "$WORK/config" /var/lib/opensurge/config.yaml
sudo sha256sum /var/lib/opensurge/control/control-token | cut -d' ' -f1 | cmp - "$WORK/token-digest"
sudo stat -c '%i:%Y:%s' /var/lib/opensurge/data/GeoIP.dat | cmp - "$WORK/cache-stat"
test "$(grep -Fc '#include if exists "/usr/share/opensurge/apparmor-dnsmasq"' /etc/apparmor.d/local/usr.sbin.dnsmasq)" == 1
sudo test ! -e /var/lib/opensurge/runtime/state.json
sudo apt-get remove -y opensurge-gateway
! grep -Fq '/usr/share/opensurge/apparmor-dnsmasq' /etc/apparmor.d/local/usr.sbin.dnsmasq
grep -Fq '# Administrator local rules sentinel' /etc/apparmor.d/local/usr.sbin.dnsmasq
sudo cmp "$WORK/distribution-profile" /etc/apparmor.d/usr.sbin.dnsmasq
if sudo /usr/sbin/dnsmasq --test --conf-file=/var/lib/opensurge/runtime/dnsmasq.conf > "$WORK/removed-denied" 2>&1; then
 echo 'Removed package left AppArmor runtime access active' >&2; exit 1
fi
grep -q 'Permission denied' "$WORK/removed-denied"
sudo cmp "$WORK/config" /var/lib/opensurge/config.yaml
if systemctl is-active --quiet opensurge-control; then echo 'Control service still active after removal' >&2; exit 1; fi
echo 'Debian install, loopback authentication, clean stopped state, config/token preservation and removal passed.'
