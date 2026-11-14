package routing

import (
	"context"
	"testing"
	"time"
)

func TestDetectorIndependentTargetAndCooldown(t *testing.T) {
	now := time.Unix(1800000000, 0)
	clock := func() time.Time { return now }
	c := Candidate{ID: "a", Fingerprint: "a"}
	calls := []int{}
	probe := func(_ context.Context, _ Candidate, target int) ProbeResult {
		calls = append(calls, target)
		return ProbeResult{Reachable: target == 1, Country: "US", IP: "203.0.113.1", DelayMS: 100}
	}
	o := Detect(context.Background(), c, "v", Observation{}, clock, time.Minute, probe)
	if !o.Healthy || o.ConfirmedFailure || len(calls) != 2 || calls[0] != 0 || calls[1] != 1 {
		t.Fatal(o, calls)
	}
	failed := func(context.Context, Candidate, int) ProbeResult { return ProbeResult{} }
	o = Detect(context.Background(), c, "v", o, clock, time.Minute, failed)
	if !o.ConfirmedFailure || !o.RetryAt.Equal(now.Add(time.Minute)) {
		t.Fatal(o)
	}
	calls = nil
	o = Detect(context.Background(), c, "v", o, clock, time.Minute, probe)
	if len(calls) != 0 {
		t.Fatal("cooldown probed")
	}
	now = now.Add(time.Minute)
	o = Detect(context.Background(), c, "v", o, clock, time.Minute, probe)
	if o.Successes != 1 {
		t.Fatal(o)
	}
	o = Detect(context.Background(), c, "v", o, clock, time.Minute, probe)
	if o.Successes != 2 {
		t.Fatal(o)
	}
	// A successful transport with unknown geography is healthy but ineligible,
	// never a confirmed node failure.
	unknown := func(context.Context, Candidate, int) ProbeResult { return ProbeResult{Reachable: true} }
	o = Detect(context.Background(), c, "v", o, clock, time.Minute, unknown)
	if !o.Healthy || o.ConfirmedFailure || o.Country != "" {
		t.Fatal(o)
	}
}
