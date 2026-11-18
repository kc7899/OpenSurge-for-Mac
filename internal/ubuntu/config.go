// Package ubuntu prepares a fresh Linux installation, never imports a Mac
// runtime journal and never changes the host's network configuration.
package ubuntu

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"path/filepath"

	"open-mihomo-gateway/internal/config"
	"open-mihomo-gateway/internal/macosnetwork"
)

const Root = "/var/lib/opensurge"

func InitialConfig(root string, s macosnetwork.Snapshot) (config.Config, error) {
	c := config.Default()
	ip := net.ParseIP(s.IPv4).To4()
	mask := net.IPMask(net.ParseIP(s.SubnetMask).To4())
	prefix, bits := mask.Size()
	if ip == nil || bits != 32 || prefix < 8 || prefix > 30 || s.Interface == "" || net.ParseIP(s.Router).To4() == nil {
		return c, fmt.Errorf("an existing LAN IPv4 /8–/30 and physical default router are required; configure Netplan first")
	}
	c.Gateway = config.GatewayConfig{Mode: config.GatewayModeSameLAN, Interface: s.Interface, UpstreamInterface: s.Interface, LANIP: s.IPv4, LANPrefixLen: prefix}
	c.DHCP.Enabled = false
	c.DHCP.Binary = "/usr/sbin/dnsmasq"
	c.DHCP.BypassGateway = s.Router
	c.DHCP.BypassDNS = append([]string(nil), s.DNS...)
	c.DNS.Listen = s.IPv4
	c.Mihomo.Binary = filepath.Join(root, "bin", "mihomo")
	c.Runtime.Dir = filepath.Join(root, "runtime")
	c.Mihomo.Config = filepath.Join(c.Runtime.Dir, "mihomo.yaml")
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return c, err
	}
	c.Mihomo.Secret = hex.EncodeToString(secret)
	c.Tailscale.Hostname = "opensurge-ubuntu"
	c.Tailscale.AuthKeyFile = filepath.Join(root, "data", "tailscale-auth-key")
	c.Tailscale.StateDir = filepath.Join(root, "data", "tailscale")
	c.PF.AnchorName = "opensurge-ubuntu"
	c.Transparent.Mode = config.TransparentModeTUN
	c.Transparent.TUNDevice = "opensurge0"
	c.Transparent.TUNStrictRoute = true
	c.Transparent.TUNAutoDetectInterface = true
	c.Transparent.MacTUNIPv6 = false
	c.Transparent.TUNIPv6 = config.TUNIPv6Off
	c.LocalSystemProxy.Enabled = false
	return c, config.Validate(c)
}
