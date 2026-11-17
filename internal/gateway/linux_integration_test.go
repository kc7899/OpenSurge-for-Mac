//go:build linux

package gateway

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"open-mihomo-gateway/internal/config"
	"open-mihomo-gateway/internal/mihomo"
	"open-mihomo-gateway/internal/pf"
	"open-mihomo-gateway/internal/process"
	"open-mihomo-gateway/internal/runtime"
	"open-mihomo-gateway/internal/sysctl"
)

// This test is destructive ONLY inside a private mount + network namespace.
// The workflow isolates /run as well, so no host systemd D-Bus is reachable.
func TestUbuntuGatewayDataPlaneNamespace(t *testing.T) {
	if os.Getenv("OPENSURGE_LINUX_NETWORK_TEST") != "1" {
		t.Skip("explicit isolated Linux runner required")
	}
	self, _ := os.Readlink("/proc/self/ns/net")
	host, _ := os.Readlink("/proc/1/ns/net")
	if self == "" || self == host || os.Geteuid() != 0 {
		t.Fatal("refusing to mutate host network namespace")
	}
	if _, err := os.Stat("/run/dbus/system_bus_socket"); !os.IsNotExist(err) {
		t.Fatal("private /run mount required: host D-Bus must not be accessible")
	}
	binary := os.Getenv("OPENSURGE_TEST_CORE")
	fixture := os.Getenv("OPENSURGE_LINUX_FIXTURE")
	if binary == "" || fixture == "" {
		t.Fatal("pinned Linux core and offline fixture required")
	}
	run := func(name string, args ...string) string {
		t.Helper()
		out, err := exec.Command(name, args...).CombinedOutput()
		if err != nil {
			t.Fatalf("%s %v: %v\n%s", name, args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	run("ip", "link", "set", "lo", "up")
	run("ip", "link", "add", "lan0", "type", "bridge")
	run("ip", "addr", "add", "192.168.50.212/24", "dev", "lan0")
	run("ip", "link", "set", "lan0", "up")
	for _, ns := range []string{"router", "client"} {
		run("ip", "netns", "add", ns)
		run("ip", "link", "add", ns+"0", "type", "veth", "peer", "name", "eth0", "netns", ns)
		run("ip", "link", "set", ns+"0", "master", "lan0")
		run("ip", "link", "set", ns+"0", "up")
		run("ip", "-n", ns, "link", "set", "lo", "up")
		run("ip", "-n", ns, "link", "set", "eth0", "up")
	}
	run("ip", "-n", "router", "addr", "add", "192.168.50.1/24", "dev", "eth0")
	run("ip", "-n", "router", "addr", "add", "203.0.113.10/32", "dev", "lo")
	run("ip", "-n", "client", "addr", "add", "192.168.50.100/24", "dev", "eth0")
	run("ip", "-n", "client", "route", "add", "default", "via", "192.168.50.212")
	run("ip", "route", "add", "default", "via", "192.168.50.1")
	fixtureCmd := exec.Command("ip", "netns", "exec", "router", "python3", fixture)
	fixtureCmd.Stdout, fixtureCmd.Stderr = os.Stdout, os.Stderr
	if err := fixtureCmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = fixtureCmd.Process.Kill(); _ = fixtureCmd.Wait() }()
	time.Sleep(150 * time.Millisecond)
	// A foreign rule is kept byte-for-byte throughout startup and cleanup.
	run("nft", "add", "table", "inet", "foreign_fixture")
	foreign := run("nft", "-j", "list", "table", "inet", "foreign_fixture")
	before, err := sysctl.New().Current()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	cfg := config.Default()
	cfg.Gateway = config.GatewayConfig{Mode: config.GatewayModeSameLAN, Interface: "lan0", UpstreamInterface: "lan0", LANIP: "192.168.50.212", LANPrefixLen: 24}
	cfg.DHCP.Enabled, cfg.DHCP.Binary = false, "/usr/sbin/dnsmasq"
	cfg.DNS.Listen = cfg.Gateway.LANIP
	cfg.LocalSystemDNS.Enabled = false // No access to runner's systemd-resolved.
	cfg.Transparent.Mode, cfg.Transparent.TUNDevice = config.TransparentModeTUN, "opensurge0"
	cfg.Transparent.MacTUNIPv6, cfg.Transparent.TUNStrictRoute, cfg.Transparent.TUNAutoDetectInterface = false, true, true
	cfg.PF.AnchorName = "ubuntu-test"
	cfg.Runtime.Dir = filepath.Join(dir, "runtime")
	cfg.Mihomo.Binary, cfg.Mihomo.Config = binary, filepath.Join(cfg.Runtime.Dir, "mihomo.yaml")
	cfg.Mihomo.Secret = "offline-namespace-fixture"
	cfg.Mihomo.ProfileMode, cfg.Mihomo.Profile = config.MihomoProfileModeImported, filepath.Join(dir, "profile.yaml")
	profile := `dns:
  nameserver: ['udp://192.168.50.1:5353']
  default-nameserver: ['192.168.50.1']
  proxy-server-nameserver: ['udp://192.168.50.1:5353']
proxies:
  - {name: A, type: http, server: 192.168.50.1, port: 3128}
  - {name: B, type: http, server: 192.168.50.1, port: 3129}
proxy-groups:
  - {name: Proxy, type: select, proxies: [A, B, REJECT]}
rules:
  - DOMAIN,direct.test,DIRECT
  - DOMAIN,blocked.test,REJECT
  - DOMAIN,proxy.test,Proxy
  - MATCH,REJECT
`
	if err := os.WriteFile(cfg.Mihomo.Profile, []byte(profile), 0600); err != nil {
		t.Fatal(err)
	}
	manager := New(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 70*time.Second)
	defer cancel()
	if err := manager.Start(ctx); err != nil {
		b, _ := os.ReadFile(runtime.NewPaths(cfg).MihomoLog)
		t.Fatalf("start: %v\n%s", err, b)
	}
	defer func() {
		if err := manager.Stop(context.Background()); err != nil {
			t.Error(err)
		}
	}()
	state, exists, err := runtime.LoadState(runtime.NewPaths(cfg).StateFile)
	if err != nil || !exists || state.PIDMihomo <= 0 {
		t.Fatal(state, exists, err)
	}
	configBefore, _ := os.ReadFile(cfg.Mihomo.Config)
	dns := run("ip", "netns", "exec", "client", "dig", "+short", "@192.168.50.212", "proxy.test", "A")
	if !strings.HasPrefix(dns, "198.18.") {
		t.Fatalf("DNS did not use fake-IP: %s", dns)
	}
	get := func(host, expected string) {
		t.Helper()
		ip := run("ip", "netns", "exec", "client", "dig", "+short", "@192.168.50.212", host, "A")
		body := run("ip", "netns", "exec", "client", "curl", "--noproxy", "*", "--silent", "--show-error", "--max-time", "4", "--resolve", fmt.Sprintf("%s:80:%s", host, ip), "http://"+host+"/")
		if body != expected {
			t.Fatalf("%s = %q, want %q", host, body, expected)
		}
	}
	get("proxy.test", "via-A")
	get("direct.test", "via-direct")
	blocked := run("ip", "netns", "exec", "client", "dig", "+short", "@192.168.50.212", "blocked.test", "A")
	if out, e := exec.Command("ip", "netns", "exec", "client", "curl", "--noproxy", "*", "-fsS", "--max-time", "2", "--resolve", "blocked.test:80:"+blocked, "http://blocked.test/").CombinedOutput(); e == nil {
		t.Fatalf("REJECT leaked traffic: %s", out)
	}
	if err := mihomo.SelectProxyGroup(ctx, cfg, "Proxy", "B"); err != nil {
		t.Fatal(err)
	}
	get("proxy.test", "via-B")
	get("direct.test", "via-direct")
	for i := 0; i < 3; i++ {
		if _, err := mihomo.FetchVersion(ctx, cfg); err != nil {
			t.Fatal(err)
		}
	}
	after, _, _ := runtime.LoadState(runtime.NewPaths(cfg).StateFile)
	configAfter, _ := os.ReadFile(cfg.Mihomo.Config)
	if after.PIDMihomo != state.PIDMihomo || string(configAfter) != string(configBefore) || !process.IsAlive(state.PIDMihomo) {
		t.Fatal("API selection/check restarted or rewrote gateway")
	}
	if err := mihomo.SelectProxyGroup(ctx, cfg, "Proxy", "REJECT"); err != nil {
		t.Fatal(err)
	}
	get("direct.test", "via-direct")
	if out, e := exec.Command("ip", "netns", "exec", "client", "curl", "--noproxy", "*", "-fsS", "--max-time", "2", "--resolve", "proxy.test:80:"+dns, "http://proxy.test/").CombinedOutput(); e == nil {
		t.Fatalf("empty proxy selection leaked: %s", out)
	}
	// A disappeared core must not turn the Linux host into an unfiltered NAT.
	if err := process.StopPID(state.PIDMihomo, 3*time.Second); err != nil {
		t.Fatal(err)
	}
	if out, e := exec.Command("ip", "netns", "exec", "client", "curl", "--noproxy", "*", "-fsS", "--max-time", "2", "http://203.0.113.10/").CombinedOutput(); e == nil {
		t.Fatalf("core loss leaked direct forwarding: %s", out)
	}
	if err := manager.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	current, err := sysctl.New().Current()
	if err != nil || current != before {
		t.Fatal("forwarding not restored", before, current, err)
	}
	if loaded, e := pf.New(cfg, runtime.NewPaths(cfg)).Loaded(); e != nil || loaded {
		t.Fatal("owned firewall not cleaned", loaded, e)
	}
	if value := run("nft", "-j", "list", "table", "inet", "foreign_fixture"); value != foreign {
		t.Fatal("foreign firewall changed")
	}
	// Forced startup failure leaves no owned table or forwarding change.
	bad := cfg
	bad.Mihomo.Binary = "/bin/false"
	bad.Runtime.Dir = filepath.Join(dir, "failure")
	bad.Mihomo.Config = filepath.Join(bad.Runtime.Dir, "mihomo.yaml")
	if err := New(bad).Start(ctx); err == nil {
		t.Fatal("broken core start succeeded")
	}
	current, err = sysctl.New().Current()
	if err != nil || current != before {
		t.Fatal("failed start changed forwarding")
	}
	// Occupy downstream DNS only after the healthy core can start. This forces
	// a post-forwarding failure and exercises the actual rollback path.
	conflict, err := net.ListenPacket("udp4", "192.168.50.212:53")
	if err != nil {
		t.Fatal(err)
	}
	defer conflict.Close()
	if err := New(cfg).Start(ctx); err == nil {
		t.Fatal("DNS conflict startup unexpectedly succeeded")
	}
	current, err = sysctl.New().Current()
	if err != nil || current != before {
		t.Fatal("rollback did not restore forwarding", err)
	}
	if loaded, err := pf.New(cfg, runtime.NewPaths(cfg)).Loaded(); err != nil || loaded {
		t.Fatal("rollback retained firewall", loaded, err)
	}
	t.Log("IPv4 downstream DNS, proxy, direct, reject, API selection without restart, core-loss guard, owned cleanup and startup failure passed; IPv6/Tailscale/host-resolved not asserted")
}
