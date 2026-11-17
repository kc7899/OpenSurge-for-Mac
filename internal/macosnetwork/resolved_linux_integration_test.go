package macosnetwork

import (
	"context"
	"os"
	"os/exec"
	"os/user"
	"reflect"
	"strconv"
	"testing"
	"time"
)

func TestLinuxResolvedPrivateBusNamespace(t *testing.T) {
	if os.Getenv("OPENSURGE_LINUX_NETWORK_TEST") != "1" {
		t.Skip("private Linux mount/network namespaces required")
	}
	self, _ := os.Readlink("/proc/self/ns/net")
	host, _ := os.Readlink("/proc/1/ns/net")
	if self == "" || self == host || os.Geteuid() != 0 {
		t.Fatal("refusing host network")
	}
	if _, err := os.Stat("/run/dbus/system_bus_socket"); !os.IsNotExist(err) {
		t.Fatal("private empty /run required")
	}
	run := func(name string, args ...string) {
		t.Helper()
		b, e := exec.Command(name, args...).CombinedOutput()
		if e != nil {
			t.Fatalf("%s: %v: %s", name, e, b)
		}
	}
	run("ip", "link", "set", "lo", "up")
	run("ip", "link", "add", "dnsfixture0", "type", "dummy")
	run("ip", "addr", "add", "192.0.2.10/24", "dev", "dnsfixture0")
	run("ip", "link", "set", "dnsfixture0", "up")
	if err := os.MkdirAll("/run/dbus", 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll("/run/systemd/resolve", 0755); err != nil {
		t.Fatal(err)
	}
	account, err := user.Lookup("systemd-resolve")
	if err != nil {
		t.Fatal(err)
	}
	uid, _ := strconv.Atoi(account.Uid)
	gid, _ := strconv.Atoi(account.Gid)
	if err := os.Chown("/run/systemd/resolve", uid, gid); err != nil {
		t.Fatal(err)
	}
	dbus := exec.Command("dbus-daemon", "--system", "--nofork", "--nopidfile")
	dbus.Stdout, dbus.Stderr = os.Stdout, os.Stderr
	if err := dbus.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = dbus.Process.Kill(); _ = dbus.Wait() }()
	time.Sleep(150 * time.Millisecond)
	resolved := exec.Command("/usr/lib/systemd/systemd-resolved")
	resolved.Env = append(os.Environ(), "SYSTEMD_LOG_LEVEL=warning")
	resolved.Stdout, resolved.Stderr = os.Stdout, os.Stderr
	if err := resolved.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resolved.Process.Kill(); _ = resolved.Wait() }()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	ready := false
	for i := 0; i < 50; i++ {
		if _, err := runCommand(ctx, "resolvectl", "dns", "dnsfixture0"); err == nil {
			ready = true
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !ready {
		t.Fatal("private systemd-resolved did not become ready")
	}
	run("resolvectl", "dns", "dnsfixture0", "192.0.2.53")
	run("resolvectl", "domain", "dnsfixture0", "example.internal", "~private.internal")
	run("resolvectl", "dnssec", "dnsfixture0", "no")
	run("resolvectl", "dnsovertls", "dnsfixture0", "no")
	for _, domains := range [][]string{{"example.internal", "~private.internal"}, nil} {
		if err := linuxSetDNSValues(ctx, "domain", "dnsfixture0", domains); err != nil {
			t.Fatal(err)
		}
		s, err := (SystemDNS{}).Prepare(ctx, "dnsfixture0")
		if err != nil {
			t.Fatal(err)
		}
		s.Owned = true
		if err := (SystemDNS{}).Enable(ctx, s); err != nil {
			t.Fatal(err)
		}
		if _, err := (SystemDNS{}).Restore(ctx, s); err != nil {
			t.Fatal(err)
		}
		after, err := (SystemDNS{}).Prepare(ctx, "dnsfixture0")
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(after.Servers, s.Servers) || !reflect.DeepEqual(after.LinuxDomains, s.LinuxDomains) {
			t.Fatal("DNS/domain restoration differs", s, after)
		}
	}
	t.Log("real Ubuntu systemd-resolved on a private system D-Bus: apply, read-back, restore and empty-domain clearing passed")
}
