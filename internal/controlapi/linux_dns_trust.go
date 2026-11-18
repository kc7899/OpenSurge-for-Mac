package controlapi

import (
	"fmt"
	"os"
	"path/filepath"
)

// Linux uses the distribution-managed dnsmasq-base executable. This is a
// single exact exception, not permission to execute arbitrary /usr paths.
func requireLinuxDNSBinary(path string) error {
	if path != "/usr/sbin/dnsmasq" {
		return fmt.Errorf("Ubuntu requires the distribution /usr/sbin/dnsmasq binary")
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return err
	}
	for current := resolved; ; current = filepath.Dir(current) {
		info, err := os.Stat(current)
		if err != nil {
			return err
		}
		if err := requireRootOwnedMode(info); err != nil {
			return fmt.Errorf("untrusted distribution dnsmasq path: %w", err)
		}
		if current == resolved && (!info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0) {
			return fmt.Errorf("dnsmasq is not executable")
		}
		if current == "/" {
			break
		}
	}
	return nil
}
