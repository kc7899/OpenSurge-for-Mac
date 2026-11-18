package pf

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"open-mihomo-gateway/internal/config"
	"open-mihomo-gateway/internal/runtime"
)

func TestLinuxFirewallOwnedLifecycleNamespace(t *testing.T) {
	if os.Getenv("OPENSURGE_LINUX_NETWORK_TEST") != "1" {
		t.Skip("requires an isolated network namespace")
	}
	self, e := os.Readlink("/proc/self/ns/net")
	if e != nil {
		t.Fatal(e)
	}
	host, e := os.Readlink("/proc/1/ns/net")
	if e != nil || self == host {
		t.Fatal("refusing host network namespace")
	}
	if os.Geteuid() != 0 {
		t.Fatal("namespace test requires root")
	}
	run := func(args ...string) string {
		b, e := exec.Command("nft", args...).CombinedOutput()
		if e != nil {
			t.Fatalf("nft failed: %v %s", e, b)
		}
		return string(b)
	}
	run("add", "table", "inet", "opensurge_test_sentinel")
	sentinel := run("-j", "list", "table", "inet", "opensurge_test_sentinel")
	cfg := config.Default()
	cfg.Gateway.Interface = "lan0"
	cfg.Gateway.UpstreamInterface = "lan0"
	cfg.Transparent.Mode = config.TransparentModeTUN
	cfg.Transparent.TUNDevice = "opensurge0"
	paths := runtime.NewPaths(cfg)
	paths.PFAnchor = filepath.Join(t.TempDir(), "firewall.nft")
	m := New(cfg, paths)
	if e = m.WriteAnchor(); e != nil {
		t.Fatal(e)
	}
	if e = m.Load(false); e != nil {
		t.Fatal(e)
	}
	if loaded, e := m.Loaded(); e != nil || !loaded {
		t.Fatal(loaded, e)
	}
	if e = m.Load(false); e == nil {
		t.Fatal("duplicate load must not append duplicate rules")
	}
	if e = m.Unload(true); e != nil {
		t.Fatal(e)
	}
	if after := run("-j", "list", "table", "inet", "opensurge_test_sentinel"); after != sentinel {
		t.Fatal("unrelated firewall table was changed")
	}
	if e = m.Unload(false); e != nil {
		t.Fatal("cleanup must be idempotent", e)
	}
	// A coincidentally named foreign table must remain untouched.
	cmd := exec.Command("nft", "-f", "-")
	cmd.Stdin = strings.NewReader("table inet " + m.table() + " { comment \"foreign\"; }\n")
	if b, e := cmd.CombinedOutput(); e != nil {
		t.Fatal(e, string(b))
	}
	if e = m.Unload(false); e == nil {
		t.Fatal("foreign table was treated as owned")
	}
	run("list", "table", "inet", m.table())
}
