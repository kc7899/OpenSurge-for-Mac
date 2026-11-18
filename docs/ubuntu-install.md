# OpenSurge Ubuntu gateway preview

Target: Ubuntu Server 24.04, amd64 (x86_64), existing same-LAN IPv4 network.
This is a Linux background service plus the shared Web console, without a
native desktop app. Package installation starts only the control services;
it does not start the network gateway or import any subscription.

## Install and open

1. Reserve a stable LAN IPv4 for the Ubuntu server in the main router (or
   configure Netplan yourself). Keep the main router DHCP enabled. OpenSurge
   reads the address and physical default router; it never rewrites Netplan.
2. Check the published SHA256, then install the downloaded package:
   `sudo apt install ./opensurge-gateway_0.3.4-ubuntu.3_amd64.deb`
3. On your computer, keep an SSH tunnel open:
   `ssh -N -L 16167:127.0.0.1:61767 USER@UBUNTU_IP`
4. On Ubuntu run `sudo opensurge-admin console`, then open its one-time URL
   within 30 seconds on your computer. Repeat this command if the URL expires.
   This URL grants administrator access; do not share it. The long-term token
   is not printed into the systemd journal.
5. Import your own Clash/Mihomo subscription in the Web console, validate and
   apply it. Configure stable routing and start the gateway in Network settings.
6. For each downstream device, set **both IPv4 gateway and DNS** to Ubuntu's
   LAN IPv4. You can instead configure those DHCP options on your main router
   for all intended devices. Registration in OpenSurge does not configure
   client addressing automatically.

The Web control API and explicit HTTP proxy bind to loopback by default.
The existing password-protected LAN console can be enabled separately. Never
publish the raw control API or Mihomo controller to the Internet.

Since ubuntu.3, installation adds one owned optional include to
`/etc/apparmor.d/local/usr.sbin.dnsmasq`. Its packaged rules permit only reads
of `/var/lib/opensurge/runtime/dnsmasq.conf` and writes of `dnsmasq.pid` and
`logs/dnsmasq.log`. Only the stock dnsmasq AppArmor profile is reloaded when
AppArmor is active and that profile is enabled. Removal deletes our include
and reloads the same profile, retaining administrator rules. AppArmor is never
disabled. A customized profile that omits the stock local include needs
administrator review; the package does not rewrite distribution policy.

The package also seeds missing `GeoIP.dat`, `GeoSite.dat`, `geoip.metadb` and
`ASN.mmdb` files in both `data/` (imported profiles' Mihomo home) and `runtime/`
(generated profiles' home), with root:opensurge 0640 ownership. Existing caches
are preserved on upgrade. The fixed public snapshot and checksums are recorded
in BUILD-INFO.txt and `/usr/share/opensurge/geodata/SHA256SUMS`; attribution and
licenses are in the package notices. This removes the initial geodata download
dependency for ordinary GEOIP/GEOSITE/IP-ASN validation. It does not supply
arbitrary remote rule-providers, missing custom categories or subscription data.

## Scope and limits

- Reuses subscription refresh, original node labels, shared policy/devices
  console, stable routing and the confirmed ChatGPT TLS-fault guard from rc.4.
- Linux networking uses Mihomo TUN/auto-redirect and one owned nftables table.
  Ordinary API selection and health checks do not restart/reload the gateway.
- IPv4 only. Forwarded IPv6 entering the Ubuntu LAN interface is blocked while
  the gateway runs. Devices can still bypass Ubuntu through the main router's
  native IPv6 default route. Remove that competing route on affected clients
  or disable IPv6 for them; this preview does not take over IPv6 RA/DHCPv6.
- Same-LAN bypass topology only. No Linux DHCP takeover, native Mac system
  proxy, sleep/closed-lid controls or downstream Mac IPv6 packet broker.
- Existing per-device policy is reused. The larger proposed automatic new-device
  registration/default-approval feature is not complete in the rc.4 baseline.
- Tailscale code is included but a real Tailnet and ACLs are not certified by
  the offline IPv4 tests. No automatic Tailnet membership or ACL changes.
- Do not disable UFW/firewalld or flush their rules to make this work. Other
  firewall policies can block forwarding; reconcile them explicitly. This
  package does not claim exclusive control of the host's networking.
- Per-link systemd-resolved DNS/domain settings are snapshotted and restored.
  Strict DNS-over-TLS/DNSSEC configurations are rejected rather than weakened;
  advanced administrators may disable host-DNS integration and configure a
  compatible resolver path explicitly. Private-bus tests exercise the real
  Ubuntu service; they do not certify every physical server/network setup.

The `.deb` is an unsigned preview package. Verify its SHA256 against the
published `SHA256SUMS`. IPv4 namespace tests and package lifecycle tests have
passed; physical NICs, real household clients, IPv6 and a real Tailnet have not
been certified. Do not transfer all clients before verifying one test client.

## Service operations, upgrade and recovery

`sudo opensurge-admin status` and `sudo opensurge-admin doctor` inspect the
installation. `journalctl -u opensurge-control -u opensurge-helper` shows service
errors; review and redact logs before sharing. Sources and recovery state stay
in `/var/lib/opensurge`; back up that directory privately before upgrading.

`sudo opensurge-admin stop` restores owned forwarding, DNS and firewall state.
Stopping `opensurge-helper` also runs that lifecycle cleanup. Restarting only
`opensurge-control` retains the data plane and reconciles its actual state.
The helper has no systemd automatic restart loop; core recovery remains the
existing control-plane lifecycle. The package does not automatically resume
gateway takeover after reboot. Start it from the Web console after reviewing
the current address and subscription.

Upgrade/remove requires a maintenance window: downstream devices using Ubuntu
as gateway/DNS may disconnect. First move them back to the main router or
accept that interruption, stop the gateway, then run `sudo apt install ./NEW.deb`
or `sudo apt remove opensurge-gateway`. The package aborts if owned cleanup
fails. Existing lawful config and sources are retained; upgrades never import
Mac runtime/PID records or old experimental state. Retained records are not a
promise that arbitrary future versions are backward compatible.

Rollback: stop gateway, restore the previous `.deb` (`sudo apt install
--allow-downgrades ./OLD.deb`) and the private pre-upgrade backup while both
services are stopped. Start helper/control, inspect status, then manually
start gateway. Data is intentionally kept even on package purge; only delete
it yourself after confirming restoration. Do not edit/delete a live recovery
journal or flush all nftables tables.
