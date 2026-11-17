package routing

import (
	"context"
	"testing"
	"time"
)

func TestMonitorHealthyDoesNotSweepOrMutate(t *testing.T) {
	p, s, n := fixture()
	p.ChatGPTTLS = false
	s.ScanComplete = false
	m := &Monitor{Now: func() time.Time { return n }}
	calls := 0
	probe := func(context.Context, Candidate, int) ProbeResult {
		calls++
		return ProbeResult{Reachable: true, Country: "US", IP: "203.0.113.1", DelayMS: 50}
	}
	api := &fakeSelector{version: s.Version, current: s.Current}
	r, err := m.Check(context.Background(), p, s, api, probe)
	if err != nil || r.Changed || calls != 1 || api.calls != 0 || r.Decision.Reason != "current_healthy" {
		t.Fatal(r, err, calls, api.calls)
	}
}
func TestMonitorConfirmedFailureChangesOnce(t *testing.T) {
	p, s, n := fixture()
	p.ChatGPTTLS = false
	s.ScanComplete = false
	m := &Monitor{Now: func() time.Time { return n }}
	probe := func(_ context.Context, c Candidate, target int) ProbeResult {
		if c.ID == "a" {
			return ProbeResult{}
		}
		return ProbeResult{Reachable: true, Country: "US", IP: "203.0.113.2", DelayMS: 50}
	}
	api := &fakeSelector{version: s.Version, current: s.Current}
	r, err := m.Check(context.Background(), p, s, api, probe)
	if err != nil || !r.Changed || api.calls != 1 || api.current != "b" {
		t.Fatal(r, err, api)
	}
	// The next cycle reads the real selection; the recovered former source never
	// displaces it and no second select is issued.
	s.Current = api.current
	r, err = m.Check(context.Background(), p, s, api, probe)
	if err != nil || r.Changed || api.calls != 1 {
		t.Fatal(r, err, api)
	}
}

func TestMonitorHardProhibitionRejectsEvenWhenUpstreamUnknown(t *testing.T) {
	p, s, n := fixture()
	p.ChatGPTTLS = false
	s.NetworkReady = false
	m := &Monitor{Now: func() time.Time { return n }}
	probe := func(context.Context, Candidate, int) ProbeResult {
		return ProbeResult{Reachable: true, Country: "HK", IP: "203.0.113.1"}
	}
	api := &fakeSelector{version: s.Version, current: s.Current}
	r, err := m.Check(context.Background(), p, s, api, probe)
	if err != nil || !r.Changed || api.current != Reject || api.calls != 1 {
		t.Fatal(r, err, api)
	}
}
