package sysctl

import "testing"

func TestLinuxForwardingIsScopedToInboundInterface(t *testing.T) {
	key, err := NewForInterface("vlan.10").forwardingKey()
	if err != nil || key != "net/ipv4/conf/vlan.10/forwarding" {
		t.Fatal(key, err)
	}
	for _, name := range []string{"../all", "..", "lo\nall", "../ip_forward"} {
		if _, err := NewForInterface(name).forwardingKey(); err == nil {
			t.Fatal("accepted unsafe interface", name)
		}
	}
}
