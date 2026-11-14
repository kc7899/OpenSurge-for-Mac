package routing

import (
	"context"
	"sync"
	"time"
)

// Monitor owns one detection cycle at a time. Runtime and persistence adapters
// supply the actual graph and settings; its only mutation capability is Select.
// There is no gateway lifecycle or configuration writer in this component.
type Monitor struct {
	mu           sync.Mutex
	executor     Executor
	observations map[string]Observation
	Now          func() time.Time
}
type CycleResult struct {
	Decision     Decision               `json:"decision"`
	Observations map[string]Observation `json:"observations"`
	Changed      bool                   `json:"changed"`
}

func (m *Monitor) Check(ctx context.Context, p Policy, s Snapshot, selector Selector, probe ProbeFunc) (CycleResult, error) {
	if err := p.Validate(); err != nil {
		return CycleResult{}, err
	}
	if !m.mu.TryLock() {
		return CycleResult{}, ErrBusy
	}
	defer m.mu.Unlock()
	now := m.Now
	if now == nil {
		now = time.Now
	}
	if m.observations == nil {
		m.observations = map[string]Observation{}
	}
	// Callers may restore persisted observations. Version/fingerprint checks in
	// Detect and Decide prevent old graph results from becoming authorization.
	for id, o := range s.Observations {
		if _, exists := m.observations[id]; !exists {
			m.observations[id] = o
		}
	}
	s.Observations = map[string]Observation{}
	for _, c := range s.Candidates {
		s.Observations[c.ID] = m.observations[c.ID]
	}
	result := func(d Decision, changed bool) CycleResult {
		out := map[string]Observation{}
		for id, o := range s.Observations {
			out[id] = o
		}
		return CycleResult{Decision: d, Observations: out, Changed: changed}
	}
	if !p.Enabled {
		return result(Decide(p, s, now()), false), nil
	}
	// Always inspect the actual current node first. A healthy current node avoids
	// the full candidate sweep and keeps its public endpoint stable.
	for _, c := range s.Candidates {
		if c.ID == s.Current {
			o := Detect(ctx, c, s.Version, m.observations[c.ID], now, time.Duration(p.CooldownSeconds)*time.Second, probe)
			m.observations[c.ID] = o
			s.Observations[c.ID] = o
			break
		}
	}
	d := Decide(p, s, now())
	if !d.Change && (d.Reason == "current_healthy" || d.Reason == "current_status_unknown" || d.Reason == "upstream_unknown") {
		return result(d, false), nil
	}
	if !s.NetworkReady {
		changed, err := m.executor.Apply(ctx, selector, s.Version, s.Current, d)
		return result(d, changed), err
	}
	// Bounded parallelism; each adapter also bounds a single probe. A cancelled
	// sweep is explicitly incomplete, and cannot promote a lower-priority outlet.
	type measured struct {
		id string
		o  Observation
	}
	jobs := make(chan Candidate)
	answers := make(chan measured, len(s.Candidates))
	var workers sync.WaitGroup
	for i := 0; i < 4; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for c := range jobs {
				old := s.Observations[c.ID]
				o := Detect(ctx, c, s.Version, old, now, time.Duration(p.CooldownSeconds)*time.Second, probe)
				if !o.RetryAt.IsZero() && o.Healthy && o.Successes == 1 && !now().Before(o.RetryAt) && ctx.Err() == nil {
					o = Detect(ctx, c, s.Version, o, now, time.Duration(p.CooldownSeconds)*time.Second, probe)
				}
				answers <- measured{id: c.ID, o: o}
			}
		}()
	}
	sent := 0
sendLoop:
	for _, c := range s.Candidates {
		if c.ID == s.Current {
			continue
		}
		select {
		case <-ctx.Done():
			break sendLoop
		case jobs <- c:
			sent++
		}
	}
	close(jobs)
	workers.Wait()
	close(answers)
	for answer := range answers {
		m.observations[answer.id] = answer.o
		s.Observations[answer.id] = answer.o
	}
	expected := len(s.Candidates)
	for _, c := range s.Candidates {
		if c.ID == s.Current {
			expected--
			break
		}
	}
	s.ScanComplete = sent == expected && ctx.Err() == nil
	for _, c := range s.Candidates {
		o := s.Observations[c.ID]
		if !o.Healthy && !o.ConfirmedFailure && !now().Before(o.RetryAt) {
			s.ScanComplete = false
		}
	}
	d = Decide(p, s, now())
	if ctx.Err() != nil {
		return result(d, false), ctx.Err()
	}
	changed, err := m.executor.Apply(ctx, selector, s.Version, s.Current, d)
	return result(d, changed), err
}
