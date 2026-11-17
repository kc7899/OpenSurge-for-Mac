package sysctl

import (
	"fmt"
	"os/exec"
	"strings"

	"open-mihomo-gateway/internal/process"
)

type Manager struct{ interfaceName string }

func New() Manager {
	return Manager{}
}

func (m Manager) Check() error {
	if _, err := exec.LookPath("sysctl"); err != nil {
		return fmt.Errorf("sysctl not found in PATH")
	}
	return nil
}

func (m Manager) Current() (string, error) {
	key, err := m.forwardingKey()
	if err != nil {
		return "", err
	}
	out, err := process.Output("sysctl", "-n", key)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func (m Manager) Enable() error {
	return m.setForwarding("1")
}

func (m Manager) Restore(previous string) error {
	previous = strings.TrimSpace(previous)
	if previous == "" {
		return nil
	}
	return m.setForwarding(previous)
}

func (m Manager) forwardingKey() (string, error) {
	if m.interfaceName == "" {
		return keyIPForwarding, nil
	}
	if len(m.interfaceName) > 15 || m.interfaceName == "." || m.interfaceName == ".." || strings.ContainsAny(m.interfaceName, "/\x00\n\t ") {
		return "", fmt.Errorf("invalid forwarding interface")
	}
	// Slash notation preserves literal dots in interface names. Setting only
	// this inbound interface avoids ip_forward's global configuration resets.
	return "net/ipv4/conf/" + m.interfaceName + "/forwarding", nil
}

func (m Manager) setForwarding(value string) error {
	if value != "0" && value != "1" {
		return fmt.Errorf("invalid forwarding backup value")
	}
	key, err := m.forwardingKey()
	if err != nil {
		return err
	}
	return process.Run("sysctl", "-w", key+"="+value)
}

func FormatForwarding(value string) string {
	switch strings.TrimSpace(value) {
	case "1":
		return "enabled"
	case "0":
		return "disabled"
	default:
		return "unknown"
	}
}
