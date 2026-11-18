package macosnetwork

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/netip"
	"sort"
	"strings"
)

func linuxInterface(name string) (string, error) {
	if _, err := net.InterfaceByName(name); err != nil {
		return "", err
	}
	return name, nil
}
func linuxInterfaces() ([]InterfaceOption, error) {
	all, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	out := []InterfaceOption{}
	for _, iface := range all {
		if iface.Flags&net.FlagLoopback != 0 || iface.Flags&net.FlagUp == 0 {
			continue
		}
		addrs, _ := iface.Addrs()
		out = append(out, InterfaceOption{Interface: iface.Name, NetworkService: iface.Name, IPv6LinkLocal: firstLinkLocalIPv6(addrs)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Interface < out[j].Interface })
	return out, nil
}
func linuxRoute(ctx context.Context, destination string) (RouteSelection, error) {
	args := []string{"-j", "-4", "route", "get", destination}
	if destination == "default" {
		args = []string{"-j", "-4", "route", "show", "table", "main", "default"}
	} else if ip, e := netip.ParseAddr(destination); e == nil && ip.Is6() {
		args[1] = "-6"
	}
	raw, err := runCommand(ctx, "ip", args...)
	if err != nil {
		return RouteSelection{}, err
	}
	var routes []struct {
		Dev, Gateway, Dst string
		Metric            int
	}
	if err = json.Unmarshal([]byte(raw), &routes); err != nil {
		return RouteSelection{}, err
	}
	if len(routes) == 0 {
		return RouteSelection{}, fmt.Errorf("no IPv4 route for %s", destination)
	}
	sort.SliceStable(routes, func(i, j int) bool { return routes[i].Metric < routes[j].Metric })
	r := routes[0]
	if r.Dev == "" {
		return RouteSelection{}, fmt.Errorf("route has no interface")
	}
	return RouteSelection{Interface: r.Dev, Gateway: r.Gateway, Prefix: r.Dst}, nil
}
func linuxDiscover(ctx context.Context, service, iface string) (Snapshot, error) {
	if iface == "" {
		iface = service
	}
	if service != "" && service != iface {
		return Snapshot{}, fmt.Errorf("Linux network service must identify the same interface")
	}
	device, err := net.InterfaceByName(iface)
	if err != nil {
		return Snapshot{}, err
	}
	raw, err := runCommand(ctx, "ip", "-j", "-4", "address", "show", "dev", iface)
	if err != nil {
		return Snapshot{}, err
	}
	var links []struct {
		AddrInfo []struct {
			Local, Family, Scope string
			PrefixLen            int `json:"prefixlen"`
			Dynamic              bool
			Flags                []string
		} `json:"addr_info"`
	}
	if err = json.Unmarshal([]byte(raw), &links); err != nil {
		return Snapshot{}, err
	}
	s := Snapshot{Interface: iface, NetworkService: iface, HardwareAddr: device.HardwareAddr.String(), IPv4Mode: IPv4ModeManual}
	for _, l := range links {
		for _, a := range l.AddrInfo {
			if a.Family == "inet" && a.Scope == "global" && net.ParseIP(a.Local).To4() != nil {
				s.IPv4 = a.Local
				s.SubnetMask = net.IP(net.CIDRMask(a.PrefixLen, 32)).String()
				if a.Dynamic || strings.Contains(strings.Join(a.Flags, ","), "dynamic") {
					s.IPv4Mode = IPv4ModeDHCP
				}
				break
			}
		}
		if s.IPv4 != "" {
			break
		}
	}
	route, err := linuxRoute(ctx, "default")
	if err != nil {
		return Snapshot{}, err
	}
	s.Router = route.Gateway
	if route.Interface != iface {
		return Snapshot{}, fmt.Errorf("interface %s does not carry the physical IPv4 default route", iface)
	}
	if dns, e := linuxDNSValues(ctx, "dns", iface); e == nil {
		s.DNS = dns
	}
	if v6, e := runCommand(ctx, "ip", "-j", "-6", "route", "show", "default"); e == nil {
		var routes []struct{ Dev string }
		if json.Unmarshal([]byte(v6), &routes) == nil {
			for _, r := range routes {
				if r.Dev == iface {
					s.IPv6Default = true
				}
			}
		}
	}
	if s.IPv4 == "" || s.Router == "" {
		return Snapshot{}, fmt.Errorf("Ubuntu needs an existing IPv4 address and default router; configure Netplan first")
	}
	return s, nil
}
func linuxNeighbors(ctx context.Context, iface string) ([]Neighbor, error) {
	if _, err := linuxInterface(iface); err != nil {
		return nil, err
	}
	raw, err := runCommand(ctx, "ip", "-j", "-4", "neigh", "show", "dev", iface)
	if err != nil {
		return nil, err
	}
	var rows []struct{ Dst, LLAddr string }
	if err = json.Unmarshal([]byte(raw), &rows); err != nil {
		return nil, err
	}
	out := []Neighbor{}
	for _, r := range rows {
		ip := net.ParseIP(r.Dst).To4()
		mac, e := net.ParseMAC(r.LLAddr)
		if ip != nil && e == nil {
			out = append(out, Neighbor{IP: ip.String(), MAC: mac.String(), Interface: iface})
		}
	}
	return out, nil
}
func linuxVerifyTUN(ctx context.Context, device string, ipv6 bool) error {
	if ipv6 {
		return fmt.Errorf("Ubuntu preview does not yet support IPv6 takeover")
	}
	iface, err := net.InterfaceByName(device)
	if err != nil {
		return err
	}
	if iface.Flags&net.FlagUp == 0 {
		return fmt.Errorf("TUN interface is down")
	}
	route, err := linuxRoute(ctx, "198.18.0.2")
	if err != nil {
		return err
	}
	if route.Interface != device {
		return fmt.Errorf("fake-IP route selects %s instead of %s", route.Interface, device)
	}
	return nil
}
