package routing

import (
	"context"
	"time"
)

func serviceFresh(o *ServiceTLSObservation, p Policy, now time.Time) bool {
	return o != nil && !o.CheckedAt.After(now) && now.Sub(o.CheckedAt) <= time.Duration(p.HealthSeconds*2)*time.Second
}
func serviceFailed(o *ServiceTLSObservation, p Policy, now time.Time) bool {
	return serviceFresh(o, p, now) && o.Status == "certificate_failed"
}
func servicePassed(o *ServiceTLSObservation, p Policy, now time.Time) bool {
	return serviceFresh(o, p, now) && o.Status == "passed" && (o.RetryAt.IsZero() || (!now.Before(o.RetryAt) && o.Successes >= 2))
}

// Service observations live in the existing version/fingerprint-bound node
// observation. Current faults are rechecked immediately; cooling backups wait.
// HTTP responses are not interpreted here: the adapter checks TLS only.
func detectServiceTLS(ctx context.Context, c Candidate, old *ServiceTLSObservation, current bool, p Policy, now func() time.Time, probe ProbeFunc) *ServiceTLSObservation {
	if !current && old != nil && now().Before(old.RetryAt) {
		return old
	}
	o := &ServiceTLSObservation{Status: "unknown", CheckedAt: now()}
	if old != nil {
		o.RetryAt = old.RetryAt
	}
	first := probe(ctx, c, 2)
	if ctx.Err() != nil || first.InfrastructureError {
		return o
	}
	o.CheckedAt = now()
	if first.Reachable {
		o.Status, o.Successes = "passed", 1
		if old != nil && old.Status == "passed" {
			o.Successes = old.Successes + 1
		}
		if !o.RetryAt.IsZero() && o.Successes < 2 {
			second := probe(ctx, c, 2)
			if ctx.Err() != nil || second.InfrastructureError || !second.Reachable {
				o.Status = "recovery_pending"
			} else {
				o.Successes = 2
			}
		}
		return o
	}
	o.Host, o.Error = first.ServiceHost, first.CertificateError
	if o.Host == "" || o.Error == "" {
		return o // Timeouts and transport failures do not prove a bad certificate.
	}
	second := probe(ctx, c, 2)
	o.CheckedAt = now()
	if ctx.Err() == nil && !second.InfrastructureError && !second.Reachable && second.ServiceHost == first.ServiceHost && second.CertificateError == first.CertificateError {
		o.Status = "certificate_failed"
		o.RetryAt = o.CheckedAt.Add(time.Duration(p.CooldownSeconds) * time.Second)
	}
	return o
}
