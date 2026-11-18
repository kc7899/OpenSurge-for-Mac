package gateway

import (
	"errors"
	"strings"
	"testing"

	"open-mihomo-gateway/internal/config"
	"open-mihomo-gateway/internal/runtime"
)

type earlyFirewallFixture struct{ *fakePF }

func (earlyFirewallFixture) BeforeForwarding() bool { return true }

func TestEarlyFirewallFailureCannotEnableForwardingOrStartCore(t *testing.T) {
	m, _, engine, _ := dnsLifecycleFixture(t)
	firewall := &fakePF{loadErr: errors.New("firewall could not be installed")}
	forwarding := &fakeSysctl{current: "0"}
	m.deps.newPF = func(config.Config, runtime.Paths) pfService { return earlyFirewallFixture{firewall} }
	m.deps.newSysctl = func() sysctlService { return forwarding }
	err := m.Start(t.Context())
	if err == nil || !strings.Contains(err.Error(), "firewall could not be installed") {
		t.Fatal(err)
	}
	if forwarding.enableCalled || engine.startCalled {
		t.Fatal("unprotected forwarding/core started")
	}
	if !firewall.unloadCalled {
		t.Fatal("failed firewall ownership intent was not cleaned")
	}
}
