package macosnetwork

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestLinuxDNSRestoresOnlyOwnedSettings(t *testing.T) {
	old := runCommand
	defer func() { runCommand = old }()
	dns := []string{"192.0.2.53"}
	domains := []string{"example.internal", "~private.internal"}
	writes := []string{}
	runCommand = func(_ context.Context, binary string, args ...string) (string, error) {
		if binary != "resolvectl" {
			return "", fmt.Errorf("unexpected command")
		}
		kind := args[0]
		if len(args) > 2 {
			writes = append(writes, kind)
			values := args[2:]
			if len(values) == 1 && values[0] == "" {
				values = nil
			}
			if kind == "dns" {
				dns = append([]string(nil), values...)
			} else if kind == "domain" {
				domains = append([]string(nil), values...)
			} else {
				return "", fmt.Errorf("unowned DNS setting modified")
			}
		}
		values := []string{"no"}
		if kind == "dns" {
			values = dns
		} else if kind == "domain" {
			values = domains
		}
		return "Link 1 (lo): " + strings.Join(values, " "), nil
	}
	s, e := linuxPrepareDNS(context.Background(), "lo")
	if e != nil {
		t.Fatal(e)
	}
	s.Owned = true
	if e = linuxEnableDNS(context.Background(), s); e != nil {
		t.Fatal(e)
	}
	if _, e = linuxRestoreDNS(context.Background(), s); e != nil {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(dns, []string{"192.0.2.53"}) || !reflect.DeepEqual(domains, []string{"example.internal", "~private.internal"}) {
		t.Fatal("DNS snapshot was not restored", dns, domains)
	}
	for _, w := range writes {
		if w != "dns" && w != "domain" {
			t.Fatal("unowned resolved setting changed", w)
		}
	}
	s.ServiceID = "another-interface"
	if e = linuxEnableDNS(context.Background(), s); e == nil {
		t.Fatal("changed interface identity was accepted")
	}
}
