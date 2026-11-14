package routing

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

func fixture() (Policy, Snapshot, time.Time) {
	now := time.Unix(1800000000, 0)
	p := DefaultPolicy()
	p.Enabled = true
	s := Snapshot{Version: "v1", Current: "a", NetworkReady: true, ScanComplete: true, Sources: []Source{{ID: "one", Name: "订阅2"}, {ID: "two", Name: "订阅10"}}, Candidates: []Candidate{{ID: "a", SourceID: "one", Fingerprint: "a"}, {ID: "b", SourceID: "one", Fingerprint: "b"}, {ID: "c", SourceID: "two", Fingerprint: "c"}}, Observations: map[string]Observation{}}
	for _, c := range s.Candidates {
		s.Observations[c.ID] = Observation{Version: s.Version, Fingerprint: c.Fingerprint, CheckedAt: now, Healthy: true, Country: "US", DelayMS: 100, Successes: 2}
	}
	return p, s, now
}
func fail(s *Snapshot, id string) {
	o := s.Observations[id]
	o.Healthy = false
	o.ConfirmedFailure = true
	s.Observations[id] = o
}
func TestStableFailover(t *testing.T) {
	cases := []struct {
		name, target, reason string
		change               bool
		edit                 func(*Policy, *Snapshot, time.Time)
	}{
		{"healthy stays", "a", "current_healthy", false, func(p *Policy, s *Snapshot, n time.Time) {
			p.SourceOrder = []string{"two", "one"}
			o := s.Observations["c"]
			o.DelayMS = 1
			s.Observations["c"] = o
		}},
		{"same subscription first", "b", "confirmed_failure", true, func(p *Policy, s *Snapshot, n time.Time) {
			fail(s, "a")
			o := s.Observations["b"]
			o.Country = "JP"
			s.Observations["b"] = o
		}},
		{"all current source down", "c", "confirmed_failure", true, func(p *Policy, s *Snapshot, n time.Time) { fail(s, "a"); fail(s, "b") }},
		{"quota exhausted", "c", "quota_exhausted_or_expired", true, func(p *Policy, s *Snapshot, n time.Time) {
			s.Sources[0].Quota = Quota{Known: true, Used: 100, Total: 100}
		}},
		{"unknown quota stays", "a", "current_healthy", false, func(p *Policy, s *Snapshot, n time.Time) { s.Sources[0].Quota = Quota{Error: "403"} }},
		{"all unavailable rejects", Reject, "no_eligible_candidate", true, func(p *Policy, s *Snapshot, n time.Time) {
			for _, c := range s.Candidates {
				fail(s, c.ID)
			}
		}},
		{"network unknown stays", "a", "upstream_unknown", false, func(p *Policy, s *Snapshot, n time.Time) { fail(s, "a"); s.NetworkReady = false }},
		{"unknown current stays", "a", "current_status_unknown", false, func(p *Policy, s *Snapshot, n time.Time) { delete(s.Observations, "a") }},
		{"incomplete scan stays", "a", "scan_incomplete", false, func(p *Policy, s *Snapshot, n time.Time) {
			for _, c := range s.Candidates {
				fail(s, c.ID)
			}
			s.ScanComplete = false
		}},
		{"stale result ignored", "a", "current_status_unknown", false, func(p *Policy, s *Snapshot, n time.Time) {
			o := s.Observations["a"]
			o.Version = "old"
			o.Healthy = false
			o.ConfirmedFailure = true
			s.Observations["a"] = o
		}},
		{"cooling source skipped", Reject, "no_eligible_candidate", true, func(p *Policy, s *Snapshot, n time.Time) {
			fail(s, "a")
			fail(s, "b")
			s.Sources[1].RetryAt = n.Add(time.Minute)
		}},
		{"one recovery success insufficient", "c", "confirmed_failure", true, func(p *Policy, s *Snapshot, n time.Time) {
			fail(s, "a")
			o := s.Observations["b"]
			o.RetryAt = n.Add(-time.Second)
			o.Successes = 1
			s.Observations["b"] = o
		}},
		{"forbidden current leaves", "b", "region_disallowed", true, func(p *Policy, s *Snapshot, n time.Time) {
			o := s.Observations["a"]
			o.Country = "HK"
			s.Observations["a"] = o
		}},
		{"forbidden and upstream unknown rejects", Reject, "region_disallowed", true, func(p *Policy, s *Snapshot, n time.Time) {
			o := s.Observations["a"]
			o.Country = "HK"
			s.Observations["a"] = o
			s.NetworkReady = false
		}},
		{"disabled preserves", "a", "automatic_disabled", false, func(p *Policy, s *Snapshot, n time.Time) { p.Enabled = false; fail(s, "a") }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, s, n := fixture()
			tc.edit(&p, &s, n)
			d := Decide(p, s, n)
			if d.Target != tc.target || d.Change != tc.change || d.Reason != tc.reason {
				t.Fatalf("%+v", d)
			}
		})
	}
}
func TestNaturalOrderAndIdentity(t *testing.T) {
	if CompareName("订阅2", "订阅10") >= 0 || CompareName("02", "2") != 0 || CompareName("A2", "a2") != 0 || CompareName("x"+strings.Repeat("9", 100), "x1"+strings.Repeat("0", 100)) >= 0 {
		t.Fatal("natural comparison")
	}
	sources := []Source{{ID: "c", Name: "a2"}, {ID: "b", Name: "a02"}, {ID: "a", Name: "a10"}}
	if got := OrderedSources(sources, nil); got[0].ID != "b" || got[1].ID != "c" {
		t.Fatal(got)
	}
	if got := ReconcileOrder(sources, []string{"gone", "c", "b"}); !reflect.DeepEqual(got, []string{"c", "b", "a"}) {
		t.Fatal(got)
	}
	sources[0].Name = "renamed"
	if OrderedSources(sources, []string{"c", "b", "a"})[0].ID != "c" {
		t.Fatal("rename changed saved order")
	}
	if CompareName("Xx2", "X3") <= 0 {
		t.Fatal("must not normalize user prefixes")
	}
}
func TestQuotaUnknownAndExpiry(t *testing.T) {
	now := time.Now()
	for _, h := range []string{"", "upload=1;download=2", "upload=-1;download=2;total=3", "upload=18446744073709551615;download=2;total=3", "total=1;total=2"} {
		q := ParseQuota(h, now)
		if q.Exhausted(now) || q.Known {
			t.Fatalf("%q: %+v", h, q)
		}
	}
	q := ParseQuota("upload=1;download=2;total=3", now)
	if !q.Exhausted(now) {
		t.Fatal(q)
	}
	q = ParseQuota("expire=1", now)
	if !q.Exhausted(now) {
		t.Fatal(q)
	}
}

type fakeSelector struct {
	version, current string
	calls            int
	err              error
}

func (f *fakeSelector) Revision(context.Context) (string, error) { return f.version, nil }
func (f *fakeSelector) Current(context.Context) (string, error)  { return f.current, nil }
func (f *fakeSelector) Select(_ context.Context, s string) error {
	f.calls++
	f.current = s
	return f.err
}
func TestExecutorNoRetryAndStaleGuard(t *testing.T) {
	e := &Executor{}
	f := &fakeSelector{version: "v", current: "a", err: context.DeadlineExceeded}
	d := Decision{Target: "b", Change: true}
	changed, err := e.Apply(context.Background(), f, "v", "a", d)
	if !changed || err != nil || f.calls != 1 {
		t.Fatalf("%v %v %+v", changed, err, f)
	}
	_, err = e.Apply(context.Background(), f, "old", "b", d)
	if !errors.Is(err, ErrStale) || f.calls != 1 {
		t.Fatal(err)
	}
	_, err = e.Apply(context.Background(), f, "v", "a", d)
	if !errors.Is(err, ErrStale) || f.calls != 1 {
		t.Fatal(err)
	}
}
