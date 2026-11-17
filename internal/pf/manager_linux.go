package pf

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"

	"open-mihomo-gateway/internal/config"
	"open-mihomo-gateway/internal/process"
	"open-mihomo-gateway/internal/runtime"
)

// Preserve the existing lifecycle interface. Linux owns one nftables table;
// it never flushes a ruleset, changes another table or disables a firewall.
type Manager struct {
	cfg   config.Config
	paths runtime.Paths
}

func New(cfg config.Config, paths runtime.Paths) Manager { return Manager{cfg: cfg, paths: paths} }
func (m Manager) table() string {
	sum := sha256.Sum256([]byte(m.cfg.PF.AnchorName))
	return "opensurge_" + hex.EncodeToString(sum[:8])
}
func (m Manager) owner() string { return "OpenSurge owned gateway v1 " + m.table() }
func (m Manager) Check() error {
	if m.cfg.Gateway.Mode != config.GatewayModeSameLAN || m.cfg.DHCP.Enabled || !m.cfg.Transparent.TUNEnabled() || !m.cfg.Transparent.TUNAutoRoute || m.cfg.Transparent.MacTUNIPv6 || m.cfg.Transparent.TUNIPv6 != config.TUNIPv6Off || m.cfg.LocalSystemProxy.Enabled {
		return fmt.Errorf("Ubuntu preview requires same_lan IPv4 TUN auto-route, DHCP off, host/downstream IPv6 takeover off and macOS system proxy off")
	}
	_, err := exec.LookPath("nft")
	if err != nil {
		return fmt.Errorf("nftables is required: %w", err)
	}
	if loaded, err := m.Loaded(); err != nil {
		return err
	} else if loaded {
		return fmt.Errorf("owned nftables table already exists; recover the previous gateway before starting")
	}
	return nil
}

// Install the fail-closed guard before enabling forwarding or starting TUN.
// Other platform firewall managers retain their existing lifecycle order.
func (m Manager) BeforeForwarding() bool { return true }
func (m Manager) Enabled() (bool, error) {
	if err := m.Check(); err != nil {
		return false, err
	}
	return true, nil
}
func (m Manager) WriteAnchor() error {
	raw, err := m.render()
	if err != nil {
		return err
	}
	return os.WriteFile(m.paths.PFAnchor, []byte(raw), 0640)
}
func (m Manager) render() (string, error) {
	scope, err := m.cfg.LANScope()
	if err != nil {
		return "", err
	}
	lan, upstream, tun := strconv.Quote(m.cfg.Gateway.Interface), strconv.Quote(m.cfg.Gateway.UpstreamInterface), strconv.Quote(m.cfg.Transparent.TUNDevice)
	guard := ""
	if m.cfg.Transparent.TUNEnabled() {
		guard = fmt.Sprintf("iifname %s ip saddr %s ip daddr != %s oifname != %s counter drop\n", lan, scope.String(), scope.String(), tun)
	}
	return fmt.Sprintf(`table inet %s {
 comment %s
 chain forward_guard {
  type filter hook forward priority -10; policy accept;
  %s
  iifname %s meta nfproto ipv6 counter drop
 }
 chain postrouting {
  type nat hook postrouting priority srcnat; policy accept;
  ip saddr %s ip daddr != %s oifname %s counter masquerade
 }
 chain no_redirects {
  type filter hook output priority filter; policy accept;
  oifname %s icmp type redirect counter drop
 }
}
`, m.table(), strconv.Quote(m.owner()), guard, lan, scope.String(), scope.String(), upstream, lan), nil
}
func (m Manager) Loaded() (bool, error) {
	raw, err := process.Output("nft", "-j", "list", "tables")
	if err != nil {
		return false, err
	}
	var inventory struct {
		NFTables []struct {
			Table *struct{ Family, Name string } `json:"table"`
		} `json:"nftables"`
	}
	if err = json.Unmarshal(raw, &inventory); err != nil {
		return false, err
	}
	found := false
	for _, item := range inventory.NFTables {
		if item.Table != nil && item.Table.Family == "inet" && item.Table.Name == m.table() {
			found = true
		}
	}
	if !found {
		return false, nil
	}
	raw, err = process.Output("nft", "-j", "list", "table", "inet", m.table())
	if err != nil {
		return false, err
	}
	var detail struct {
		NFTables []struct {
			Table *struct{ Family, Name, Comment string } `json:"table"`
		} `json:"nftables"`
	}
	if err = json.Unmarshal(raw, &detail); err != nil {
		return false, err
	}
	for _, item := range detail.NFTables {
		if item.Table != nil && item.Table.Family == "inet" && item.Table.Name == m.table() && item.Table.Comment == m.owner() {
			return true, nil
		}
	}
	return false, fmt.Errorf("refusing to change an nftables table without the OpenSurge ownership marker")
}
func (m Manager) Load(_ bool) error {
	exists, err := m.Loaded()
	if err != nil {
		return err
	}
	if exists {
		return fmt.Errorf("owned nftables table already exists; recover the previous gateway before starting")
	}
	if err = process.Run("nft", "--check", "--file", m.paths.PFAnchor); err != nil {
		return err
	}
	return process.Run("nft", "--file", m.paths.PFAnchor)
}
func (m Manager) Unload(_ bool) error {
	exists, err := m.Loaded()
	if err != nil || !exists {
		return err
	}
	return process.Run("nft", "delete", "table", "inet", m.table())
}
