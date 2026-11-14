package routing

import (
	"context"
	"time"
)

// ProbeResult separates transport reachability from geographical verification.
// A country-service error must not turn a working outlet into a failed outlet.
type ProbeResult struct {
	InfrastructureError bool
	Reachable           bool
	Country, IP         string
	DelayMS             int
}
type ProbeFunc func(context.Context, Candidate, int) ProbeResult

// Detect performs a second independent-target probe only after the first fails.
// The adapter owns bounded per-request timeouts and the mapping of target IDs.
// The supplied clock lets the decision tests avoid sleeps.
func Detect(ctx context.Context, c Candidate, version string, old Observation, now func() time.Time, cooldown time.Duration, probe ProbeFunc) Observation {
	o := Observation{Version: version, Fingerprint: c.Fingerprint, CheckedAt: now()}
	previous := old.Version == version && old.Fingerprint == c.Fingerprint
	if previous {
		o.RetryAt = old.RetryAt
	}
	if previous && now().Before(old.RetryAt) {
		return old
	}
	result := probe(ctx, c, 0)
	if ctx.Err() != nil || result.InfrastructureError {
		return o
	}
	if !result.Reachable {
		result = probe(ctx, c, 1)
		if ctx.Err() != nil || result.InfrastructureError {
			return o
		}
	}
	o.CheckedAt = now()
	if !result.Reachable {
		o.ConfirmedFailure = true
		o.RetryAt = o.CheckedAt.Add(cooldown)
		return o
	}
	o.Healthy = true
	o.Country = result.Country
	o.IP = result.IP
	o.DelayMS = result.DelayMS
	o.Successes = 1
	if previous && old.Healthy && old.Country == result.Country && old.IP == result.IP {
		o.Successes = old.Successes + 1
	}
	return o
}
