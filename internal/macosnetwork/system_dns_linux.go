package macosnetwork

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"slices"
	"strings"

	"open-mihomo-gateway/internal/config"
	"open-mihomo-gateway/internal/runtime"
)

func linuxDNSValues(ctx context.Context, kind, iface string) ([]string, error) {
	args := []string{kind}
	if iface != "" {
		args = append(args, iface)
	}
	out, err := runCommand(ctx, "resolvectl", args...)
	if err != nil {
		return nil, fmt.Errorf("systemd-resolved query failed: %w", err)
	}
	if iface == "" {
		for _, line := range strings.Split(out, "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "Global:") {
				_, value, _ := strings.Cut(line, ":")
				return strings.Fields(value), nil
			}
		}
		return nil, nil
	}
	_, values, ok := strings.Cut(out, ":")
	if !ok {
		return nil, fmt.Errorf("unexpected resolvectl output for %s", iface)
	}
	return strings.Fields(values), nil
}
func linuxDNSIdentity(iface string) (string, error) {
	dev, err := net.InterfaceByName(iface)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("linux:%d:%s:%s", dev.Index, dev.Name, dev.HardwareAddr.String()), nil
}
func linuxPrepareDNS(ctx context.Context, iface string) (runtime.SystemDNSSnapshot, error) {
	id, err := linuxDNSIdentity(iface)
	if err != nil {
		return runtime.SystemDNSSnapshot{}, err
	}
	s := runtime.SystemDNSSnapshot{Interface: iface, NetworkService: iface, ServiceID: id}
	s.Servers, err = linuxDNSValues(ctx, "dns", iface)
	if err != nil {
		return s, err
	}
	s.Resolvers = slices.Clone(s.Servers)
	if len(s.Resolvers) == 0 {
		s.Resolvers, err = linuxDNSValues(ctx, "dns", "")
		if err != nil {
			return s, err
		}
	}
	if len(s.Resolvers) == 0 {
		return s, fmt.Errorf("no upstream DNS discovered; configure Ubuntu DNS before starting")
	}
	for _, server := range s.Resolvers {
		a, e := netip.ParseAddr(server)
		if e != nil || a.IsLoopback() || server == config.LocalSystemDNSServer {
			return s, fmt.Errorf("upstream DNS needs a non-loopback IP; specialized DNS configuration requires explicit manual integration")
		}
	}
	s.LinuxDomains, err = linuxDNSValues(ctx, "domain", iface)
	if err != nil {
		return s, err
	}
	for _, domain := range s.LinuxDomains {
		domain = strings.TrimPrefix(domain, "~")
		if domain != "." {
			s.Domains = append(s.Domains, domain)
		}
	}
	for _, kind := range []string{"dnsovertls", "dnssec"} {
		values, e := linuxDNSValues(ctx, kind, iface)
		if e != nil {
			return s, e
		}
		if slices.Contains(values, "yes") || (kind == "dnsovertls" && slices.Contains(values, "opportunistic")) {
			return s, fmt.Errorf("strict per-link %s is configured; disable OpenSurge host-DNS integration instead of weakening it", kind)
		}
	}
	return s, nil
}
func linuxDNSCheckIdentity(s runtime.SystemDNSSnapshot) error {
	id, err := linuxDNSIdentity(s.Interface)
	if err != nil {
		return err
	}
	if id != s.ServiceID {
		return fmt.Errorf("DNS link identity changed; refusing to modify another interface")
	}
	return nil
}
func linuxSetDNSValues(ctx context.Context, kind, iface string, values []string) error {
	if len(values) == 0 {
		values = []string{""}
	}
	_, err := runCommand(ctx, "resolvectl", append([]string{kind, iface}, values...)...)
	return err
}
func linuxEnableDNS(ctx context.Context, s runtime.SystemDNSSnapshot) error {
	if err := linuxDNSCheckIdentity(s); err != nil {
		return err
	}
	if err := linuxSetDNSValues(ctx, "dns", s.Interface, []string{config.LocalSystemDNSServer}); err != nil {
		return err
	}
	if err := linuxSetDNSValues(ctx, "domain", s.Interface, []string{"~."}); err != nil {
		return err
	}
	return linuxVerifyDNS(ctx, s)
}
func linuxVerifyDNS(ctx context.Context, s runtime.SystemDNSSnapshot) error {
	if err := linuxDNSCheckIdentity(s); err != nil {
		return err
	}
	servers, err := linuxDNSValues(ctx, "dns", s.Interface)
	if err != nil {
		return err
	}
	domains, err := linuxDNSValues(ctx, "domain", s.Interface)
	if err != nil {
		return err
	}
	if !slices.Equal(servers, []string{config.LocalSystemDNSServer}) || !slices.Equal(domains, []string{"~."}) {
		return fmt.Errorf("Ubuntu DNS settings were not confirmed; rollback is required")
	}
	return nil
}
func linuxRestoreDNS(ctx context.Context, s runtime.SystemDNSSnapshot) (bool, error) {
	if !s.Owned {
		return false, nil
	}
	if _, err := net.InterfaceByName(s.Interface); err != nil {
		return false, nil
	}
	if err := linuxDNSCheckIdentity(s); err != nil {
		return true, err
	}
	if err := linuxSetDNSValues(ctx, "dns", s.Interface, s.Servers); err != nil {
		return true, err
	}
	if err := linuxSetDNSValues(ctx, "domain", s.Interface, s.LinuxDomains); err != nil {
		return true, err
	}
	servers, err := linuxDNSValues(ctx, "dns", s.Interface)
	if err != nil {
		return true, err
	}
	domains, err := linuxDNSValues(ctx, "domain", s.Interface)
	if err != nil {
		return true, err
	}
	if !slices.Equal(servers, s.Servers) || !slices.Equal(domains, s.LinuxDomains) {
		return true, fmt.Errorf("Ubuntu DNS restore could not be confirmed")
	}
	return true, nil
}
