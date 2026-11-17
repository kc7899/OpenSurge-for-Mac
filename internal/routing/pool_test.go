package routing

import (
	"gopkg.in/yaml.v3"
	"testing"
)

func testPool(t *testing.T, name, server string) Pool {
	t.Helper()
	raw := []byte("proxies: [{name: '" + name + "', type: http, server: '" + server + "', port: 443}]\nproxy-groups: [{name: Proxy, type: select, proxies: ['" + name + "']}]\nrules: ['MATCH,Proxy']\n")
	out, _, err := Compose(raw, []Input{{Source: Source{ID: "source", Name: "Subscription"}, YAML: raw}})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := ReadPool(out, "")
	if err != nil {
		t.Fatal(err)
	}
	return pool
}
func TestChangedNodeRetiresOldRuntimeIdentity(t *testing.T) {
	old := testPool(t, "US node", "one.test")
	next := testPool(t, "US node", "two.test")
	if old.Graph.Candidates[0].ID == next.Graph.Candidates[0].ID {
		t.Fatal("endpoint change reused live identity")
	}
	merged := RefreshPool(old, next, old.Graph.Candidates[0].ID)
	if err := merged.Validate(); err != nil {
		t.Fatal(err)
	}
	if len(merged.Proxies) != 2 || !merged.Graph.Candidates[1].Retiring {
		t.Fatal(merged.Graph)
	}
	p, s, now := fixture()
	s.Current = old.Graph.Candidates[0].ID
	s.Candidates = merged.Graph.Candidates
	s.Sources = []Source{{ID: "source"}}
	s.Observations = map[string]Observation{}
	for _, c := range s.Candidates {
		s.Observations[c.ID] = Observation{Version: s.Version, Fingerprint: c.Fingerprint, Healthy: true, Country: "US", CheckedAt: now}
	}
	d := Decide(p, s, now)
	if !d.Change || d.Target != next.Graph.Candidates[0].ID || d.Reason != "node_removed" {
		t.Fatal(d)
	}
	clean := RefreshPool(merged, next, next.Graph.Candidates[0].ID)
	if len(clean.Proxies) != 1 || clean.Graph.Candidates[0].Retiring {
		t.Fatal("retired node not removed after transition")
	}
	raw, err := yaml.Marshal(clean)
	if err != nil || len(raw) == 0 {
		t.Fatal(err)
	}
}
func TestPoolRejectsTampering(t *testing.T) {
	p := testPool(t, "US node", "one.test")
	p.Proxies[0]["server"] = "changed.test"
	if p.Validate() == nil {
		t.Fatal("accepted changed node without manifest fingerprint")
	}
}
