// Package routing owns stable failover decisions. It cannot start, reload or
// restart a gateway. Callers supply immutable, versioned observations.
package routing

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

type Policy struct {
	Enabled         bool     `json:"enabled"`
	ChatGPTTLS      bool     `json:"chatgpt_tls"`
	SourceOrder     []string `json:"source_order"`
	Regions         []string `json:"regions"`
	Forbidden       []string `json:"forbidden"`
	HealthSeconds   int      `json:"health_seconds"`
	QuotaSeconds    int      `json:"quota_seconds"`
	CooldownSeconds int      `json:"cooldown_seconds"`
}

func DefaultPolicy() Policy {
	return Policy{ChatGPTTLS: true, Regions: []string{"US", "SG", "JP"}, Forbidden: []string{"HK"}, HealthSeconds: 60, QuotaSeconds: 600, CooldownSeconds: 300}
}
func (p Policy) Validate() error {
	if p.HealthSeconds < 15 || p.HealthSeconds > 3600 || p.QuotaSeconds < 60 || p.QuotaSeconds > 86400 || p.CooldownSeconds < 30 || p.CooldownSeconds > 86400 {
		return fmt.Errorf("invalid detection or cooldown interval")
	}
	seen := map[string]bool{}
	for _, id := range p.SourceOrder {
		if id == "" || seen[id] {
			return fmt.Errorf("duplicate or empty source ID")
		}
		seen[id] = true
	}
	for _, list := range [][]string{p.Regions, p.Forbidden} {
		seen = map[string]bool{}
		for _, c := range list {
			if len(c) != 2 || c[0] < 'A' || c[0] > 'Z' || c[1] < 'A' || c[1] > 'Z' || seen[c] {
				return fmt.Errorf("invalid or duplicate country code")
			}
			seen[c] = true
		}
	}
	if len(p.Regions) == 0 {
		return fmt.Errorf("region order is required")
	}
	for _, c := range p.Regions {
		if contains(p.Forbidden, c) {
			return fmt.Errorf("preferred region is forbidden")
		}
	}
	return nil
}
func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// CompareName performs natural comparison without converting digit runs to
// machine integers. Leading zeroes and case are equal; callers use stable IDs
// to break ties. No source-specific naming convention is inferred.
func CompareName(a, b string) int {
	a, b = strings.ToLower(a), strings.ToLower(b)
	for len(a) > 0 && len(b) > 0 {
		if digit(a[0]) && digit(b[0]) {
			i, j := 0, 0
			for i < len(a) && digit(a[i]) {
				i++
			}
			for j < len(b) && digit(b[j]) {
				j++
			}
			x, y := strings.TrimLeft(a[:i], "0"), strings.TrimLeft(b[:j], "0")
			if len(x) < len(y) {
				return -1
			}
			if len(x) > len(y) {
				return 1
			}
			if x != y {
				return strings.Compare(x, y)
			}
			a, b = a[i:], b[j:]
			continue
		}
		if a[0] != b[0] {
			return strings.Compare(a[:1], b[:1])
		}
		a, b = a[1:], b[1:]
	}
	return strings.Compare(a, b)
}
func digit(b byte) bool { return b >= '0' && b <= '9' }

type Quota struct {
	CheckedAt time.Time `json:"checked_at"`
	Known     bool      `json:"known"`
	Used      uint64    `json:"used"`
	Total     uint64    `json:"total"`
	ExpiresAt time.Time `json:"expires_at,omitempty"`
	Error     string    `json:"error,omitempty"`
}

func (q Quota) Exhausted(now time.Time) bool {
	return (q.Known && q.Total > 0 && q.Used >= q.Total) || (!q.ExpiresAt.IsZero() && !now.Before(q.ExpiresAt))
}

type Source struct {
	ID      string    `json:"id"`
	Name    string    `json:"name"`
	Quota   Quota     `json:"quota"`
	RetryAt time.Time `json:"retry_at,omitempty"`
}

func OrderedSources(sources []Source, order []string) []Source {
	out := append([]Source{}, sources...)
	rank := map[string]int{}
	for i, id := range order {
		rank[id] = i + 1
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := rank[out[i].ID], rank[out[j].ID]
		if a != b {
			if a == 0 {
				return false
			}
			if b == 0 {
				return true
			}
			return a < b
		}
		c := CompareName(out[i].Name, out[j].Name)
		if c != 0 {
			return c < 0
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// ReconcileOrder freezes an explicitly saved order across renames, drops deleted
// IDs and appends new sources deterministically. Empty order means automatic.
func ReconcileOrder(sources []Source, order []string) []string {
	if len(order) == 0 {
		return nil
	}
	out := []string{}
	valid := map[string]bool{}
	for _, s := range sources {
		valid[s.ID] = true
	}
	seen := map[string]bool{}
	for _, id := range order {
		if valid[id] && !seen[id] {
			out = append(out, id)
			seen[id] = true
		}
	}
	for _, s := range OrderedSources(sources, nil) {
		if !seen[s.ID] {
			out = append(out, s.ID)
			seen[s.ID] = true
		}
	}
	return out
}

type Candidate struct {
	SourceName  string `json:"source_name,omitempty" yaml:"source_name,omitempty"`
	Retiring    bool   `json:"retiring,omitempty" yaml:"retiring,omitempty"`
	ID          string `json:"id"`
	SourceID    string `json:"source_id"`
	Fingerprint string `json:"fingerprint"`
	Name        string `json:"name"`
}
type ServiceTLSObservation struct {
	Status    string    `json:"status"`
	Host      string    `json:"host,omitempty"`
	Error     string    `json:"error,omitempty"`
	CheckedAt time.Time `json:"checked_at"`
	RetryAt   time.Time `json:"retry_at,omitempty"`
	Successes int       `json:"successes"`
}

type Observation struct {
	ServiceTLS  *ServiceTLSObservation `json:"service_tls,omitempty"`
	Version     string                 `json:"version"`
	Fingerprint string                 `json:"fingerprint"`
	CheckedAt   time.Time              `json:"checked_at"`
	// Failure is confirmed only by the detector after independent-target retest.
	Healthy          bool      `json:"healthy"`
	ConfirmedFailure bool      `json:"confirmed_failure"`
	Country          string    `json:"country,omitempty"`
	IP               string    `json:"ip,omitempty"`
	DelayMS          int       `json:"delay_ms,omitempty"`
	Successes        int       `json:"successes"`
	RetryAt          time.Time `json:"retry_at,omitempty"`
}
type Snapshot struct {
	Version      string
	Current      string
	Sources      []Source
	Candidates   []Candidate
	Observations map[string]Observation
	// NetworkReady means positive upstream evidence, not absence of an error.
	NetworkReady bool
	ScanComplete bool
}
type Decision struct {
	Target string `json:"target"`
	Reason string `json:"reason"`
	Change bool   `json:"change"`
}

const Reject = "REJECT"

func fresh(o Observation, c Candidate, s Snapshot, p Policy, now time.Time) bool {
	age := now.Sub(o.CheckedAt)
	return o.Version == s.Version && o.Fingerprint == c.Fingerprint && age >= 0 && age <= time.Duration(p.HealthSeconds*2)*time.Second
}
func allowed(o Observation, p Policy) bool {
	return o.Country != "" && contains(p.Regions, o.Country) && !contains(p.Forbidden, o.Country)
}
func Decide(p Policy, s Snapshot, now time.Time) Decision {
	keep := func(reason string) Decision { return Decision{Target: s.Current, Reason: reason} }
	if !p.Enabled {
		return keep("automatic_disabled")
	}
	sources := map[string]Source{}
	for _, src := range s.Sources {
		sources[src.ID] = src
	}
	var current Candidate
	found := false
	for _, c := range s.Candidates {
		if c.ID == s.Current {
			current = c
			found = true
			break
		}
	}
	reason := "initial_selection"
	if found {
		src, exists := sources[current.SourceID]
		o := s.Observations[current.ID]
		switch {
		case current.Retiring:
			reason = "node_removed"
		case exists && src.Quota.Exhausted(now):
			reason = "quota_exhausted_or_expired"
		case fresh(o, current, s, p, now) && o.Country != "" && !allowed(o, p):
			reason = "region_disallowed"
		case p.ChatGPTTLS && fresh(o, current, s, p, now) && o.Healthy && serviceFailed(o.ServiceTLS, p, now):
			reason = "service_certificate_failure"
		case fresh(o, current, s, p, now) && o.Healthy && allowed(o, p):
			return keep("current_healthy")
		case fresh(o, current, s, p, now) && o.ConfirmedFailure:
			reason = "confirmed_failure"
		default:
			return keep("current_status_unknown")
		}
	}
	// Region prohibition and positive exhaustion evidence must fail closed even
	// when upstream evidence is unavailable; never retain a prohibited outlet.
	hard := reason == "region_disallowed" || reason == "quota_exhausted_or_expired" || reason == "node_removed"
	if !s.NetworkReady {
		if hard {
			return Decision{Target: Reject, Reason: reason, Change: s.Current != Reject}
		}
		return keep("upstream_unknown")
	}
	if !s.ScanComplete {
		if hard {
			return Decision{Target: Reject, Reason: reason, Change: s.Current != Reject}
		}
		return keep("scan_incomplete")
	}
	for _, c := range backupCandidates(p, s, now, false) {
		if reason == "service_certificate_failure" && !servicePassed(s.Observations[c.ID].ServiceTLS, p, now) {
			continue
		}
		return Decision{Target: c.ID, Reason: reason, Change: c.ID != s.Current}
	}
	if reason == "service_certificate_failure" {
		return keep("service_certificate_no_verified_backup")
	}
	if s.ScanComplete || hard {
		return Decision{Target: Reject, Reason: "no_eligible_candidate", Change: s.Current != Reject}
	}
	return keep("scan_incomplete")
}

func backupCandidates(p Policy, s Snapshot, now time.Time, includeRecovering bool) []Candidate {
	sources := map[string]Source{}
	for _, src := range s.Sources {
		sources[src.ID] = src
	}
	var current Candidate
	found := false
	for _, c := range s.Candidates {
		if c.ID == s.Current {
			current, found = c, true
			break
		}
	}
	out := []Candidate{}
	order := OrderedSources(s.Sources, p.SourceOrder)
	if found && !sources[current.SourceID].Quota.Exhausted(now) {
		for i, src := range order {
			if src.ID == current.SourceID {
				order = append([]Source{src}, append(order[:i], order[i+1:]...)...)
				break
			}
		}
	}
	for _, src := range order {
		if src.Quota.Exhausted(now) || now.Before(src.RetryAt) {
			continue
		}
		for _, country := range p.Regions {
			if contains(p.Forbidden, country) {
				continue
			}
			eligible := []Candidate{}
			for i := range s.Candidates {
				c := &s.Candidates[i]
				o := s.Observations[c.ID]
				if c.Retiring || c.SourceID != src.ID || c.ID == s.Current || !fresh(o, *c, s, p, now) || !o.Healthy || !allowed(o, p) || o.Country != country || now.Before(o.RetryAt) {
					continue
				}
				if !o.RetryAt.IsZero() && o.Successes < 2 {
					continue
				}
				if p.ChatGPTTLS && o.ServiceTLS != nil && (o.ServiceTLS.Status == "certificate_failed" || (!o.ServiceTLS.RetryAt.IsZero() && (now.Before(o.ServiceTLS.RetryAt) || o.ServiceTLS.Successes < 2))) && (!includeRecovering || now.Before(o.ServiceTLS.RetryAt)) {
					continue
				}
				eligible = append(eligible, *c)
			}
			sort.Slice(eligible, func(i, j int) bool {
				a, b := s.Observations[eligible[i].ID].DelayMS, s.Observations[eligible[j].ID].DelayMS
				if a != b {
					return a < b
				}
				return eligible[i].ID < eligible[j].ID
			})
			out = append(out, eligible...)
		}
	}
	return out
}
