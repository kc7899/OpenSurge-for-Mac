//go:build linux

package ubuntu

import (
	"strings"
	"testing"

	"open-mihomo-gateway/internal/config"
	"open-mihomo-gateway/internal/macosnetwork"
)

func TestFreshUbuntuConfigUsesObservedLANAndPrivatePaths(t *testing.T) {
	s := macosnetwork.Snapshot{Interface: "ens18", IPv4: "192.168.8.20", SubnetMask: "255.255.254.0", Router: "192.168.8.1", DNS: []string{"192.168.8.1"}}
	c, err := InitialConfig("/var/lib/opensurge", s)
	if err != nil {
		t.Fatal(err)
	}
	if c.Gateway.LANIP != s.IPv4 || c.Gateway.Interface != s.Interface || c.Gateway.LANPrefixLen != 23 || c.DHCP.Enabled || c.Transparent.MacTUNIPv6 || c.Transparent.TUNIPv6 != "off" || c.Mihomo.Profile != "" {
		t.Fatal("wrong onboarding behavior", c.Gateway)
	}
	if len(c.Mihomo.Secret) != 64 || c.LocalSystemProxy.Enabled || !c.LocalSystemDNS.Enabled || c.Mihomo.ProfileMode != config.MihomoProfileModeManaged {
		t.Fatal("wrong initial security/network scope")
	}
	if strings.Contains(config.Render(c), "/Library/") {
		t.Fatal("Mac paths migrated")
	}
	for _, field := range []string{"IPv4", "SubnetMask", "Router", "Interface"} {
		bad := s
		switch field {
		case "IPv4":
			bad.IPv4 = ""
		case "SubnetMask":
			bad.SubnetMask = "255.255.255.255"
		case "Router":
			bad.Router = ""
		case "Interface":
			bad.Interface = ""
		}
		if _, err := InitialConfig("/var/lib/opensurge", bad); err == nil {
			t.Fatal("accepted incomplete network", field)
		}
	}
}
