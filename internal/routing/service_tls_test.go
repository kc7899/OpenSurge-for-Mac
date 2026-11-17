package routing

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestServiceCertificateFailoverNeedsConfirmationAndVerifiedBackup(t *testing.T) {
	for _, scenario := range []string{"confirmed", "one_failure", "timeout", "all_fail", "backup_timeout", "hong_kong_only", "recovers_before_switch", "disabled", "upstream_unknown", "stale_settings"} {
		t.Run(scenario, func(t *testing.T) {
			p, s, now := fixture()
			s.ScanComplete = false
			p.ChatGPTTLS = scenario != "disabled"
			if scenario == "upstream_unknown" {
				s.NetworkReady = false
			}
			api := &fakeSelector{version: s.Version, current: s.Current}
			m := &Monitor{Now: func() time.Time { return now }}
			serviceCalls := map[string]int{}
			probe := func(_ context.Context, c Candidate, target int) ProbeResult {
				if target != 2 {
					country := "US"
					if scenario == "hong_kong_only" && c.ID != "a" {
						country = "HK"
					}
					return ProbeResult{Reachable: true, Country: country, IP: "203.0.113.1", DelayMS: 10}
				}
				serviceCalls[c.ID]++
				if c.ID == "a" {
					if scenario == "timeout" {
						return ProbeResult{}
					}
					if scenario == "one_failure" && serviceCalls[c.ID] > 1 || scenario == "recovers_before_switch" && serviceCalls[c.ID] > 2 {
						return ProbeResult{Reachable: true}
					}
					return ProbeResult{ServiceHost: "chatgpt.com", CertificateError: "hostname_mismatch"}
				}
				if scenario == "backup_timeout" {
					return ProbeResult{}
				}
				if scenario == "all_fail" {
					return ProbeResult{ServiceHost: "chatgpt.com", CertificateError: "hostname_mismatch"}
				}
				if scenario == "stale_settings" {
					api.version = "changed"
				}
				return ProbeResult{Reachable: true}
			}
			r, err := m.Check(context.Background(), p, s, api, probe)
			if scenario == "stale_settings" {
				if err != ErrStale || api.calls != 0 {
					t.Fatal(r, err, api)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if scenario == "confirmed" {
				if !r.Changed || api.current != "b" || api.calls != 1 || serviceCalls["a"] != 4 || r.Decision.Reason != "service_certificate_failure" {
					t.Fatal(r, api, serviceCalls)
				}
				s.Current = api.current
				if next, e := m.Check(context.Background(), p, s, api, func(_ context.Context, c Candidate, target int) ProbeResult {
					return ProbeResult{Reachable: true, Country: "US", IP: "203.0.113.1", DelayMS: 1}
				}); e != nil || next.Changed || api.calls != 1 {
					t.Fatal("healthy replacement was displaced", next, e)
				}
			} else if r.Changed || api.calls != 0 || api.current != "a" {
				t.Fatal("unproven service problem changed outlet", r, api, serviceCalls)
			}
			if scenario == "all_fail" {
				if r.Decision.Reason != "service_certificate_no_verified_backup" {
					t.Fatal(r)
				}
				before := serviceCalls["b"]
				if next, e := m.Check(context.Background(), p, s, api, probe); e != nil || next.Changed || serviceCalls["b"] != before {
					t.Fatal("cooling service candidate reprobed", next, e)
				}
			}
			if scenario == "disabled" && len(serviceCalls) != 0 {
				t.Fatal("disabled guard probed service")
			}
		})
	}
}

func TestServiceCertificateRecoveryAndUncertainRetest(t *testing.T) {
	p := DefaultPolicy()
	now := time.Unix(1800000000, 0)
	clock := func() time.Time { return now }
	c := Candidate{ID: "node"}
	bad := func(context.Context, Candidate, int) ProbeResult {
		return ProbeResult{ServiceHost: "chatgpt.com", CertificateError: "untrusted_chain"}
	}
	o := detectServiceTLS(context.Background(), c, nil, false, p, clock, bad)
	if !serviceFailed(o, p, now) {
		t.Fatal(o)
	}
	calls := 0
	good := func(context.Context, Candidate, int) ProbeResult { calls++; return ProbeResult{Reachable: true} }
	if next := detectServiceTLS(context.Background(), c, o, false, p, clock, good); next != o || calls != 0 {
		t.Fatal("cooldown bypassed", next, calls)
	}
	now = now.Add(time.Duration(p.CooldownSeconds) * time.Second)
	o = detectServiceTLS(context.Background(), c, o, false, p, clock, good)
	if !servicePassed(o, p, now) || calls != 2 {
		t.Fatal("recovery needs two fresh successes", o, calls)
	}
	calls = 0
	mixed := func(context.Context, Candidate, int) ProbeResult {
		calls++
		if calls == 1 {
			return bad(context.Background(), c, 2)
		}
		return ProbeResult{ServiceHost: "api.openai.com", CertificateError: "untrusted_chain"}
	}
	o = detectServiceTLS(context.Background(), c, nil, true, p, clock, mixed)
	if o.Status == "certificate_failed" {
		t.Fatal("different failed hosts must not confirm the same fault", o)
	}
}

func TestServiceFaultIsRecheckedAfterSlowCandidateSweep(t *testing.T) {
	p, s, base := fixture()
	s.ScanComplete = false
	var offset atomic.Int64
	var moved atomic.Bool
	now := func() time.Time { return base.Add(time.Duration(offset.Load()) * time.Second) }
	m := &Monitor{Now: now}
	api := &fakeSelector{version: s.Version, current: s.Current}
	probe := func(_ context.Context, c Candidate, target int) ProbeResult {
		if target == 2 {
			if c.ID == "a" {
				return ProbeResult{ServiceHost: "chatgpt.com", CertificateError: "hostname_mismatch"}
			}
			return ProbeResult{Reachable: true}
		}
		if c.ID != "a" && moved.CompareAndSwap(false, true) {
			offset.Store(131)
		}
		return ProbeResult{Reachable: true, Country: "US", IP: "203.0.113.1", DelayMS: 10}
	}
	r, e := m.Check(context.Background(), p, s, api, probe)
	if e != nil || !r.Changed || api.current != "b" || !r.Observations["a"].CheckedAt.Equal(now()) {
		t.Fatal("stale current evidence prevented or authorized a switch", r, e)
	}
}
