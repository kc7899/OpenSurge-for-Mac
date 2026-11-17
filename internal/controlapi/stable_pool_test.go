package controlapi

import (
	"context"
	"fmt"
	"gopkg.in/yaml.v3"
	"open-mihomo-gateway/internal/config"
	"open-mihomo-gateway/internal/routing"
	"os"
	"path/filepath"
	"testing"
)

func TestStablePoolFailedUpdateRestoresFallbackAndReconciles(t *testing.T) {
	binary := os.Getenv("OPENSURGE_TEST_CORE")
	if binary == "" {
		t.Skip("set pinned core for real static validation")
	}
	base := []byte("proxies: [{name: 'US node', type: http, server: 127.0.0.1, port: 18080}]\nproxy-groups: [{name: Proxy, type: select, proxies: ['US node']}]\nrules: ['MATCH,Proxy']\n")
	profile, _, err := routing.Compose(base, []routing.Input{{Source: routing.Source{ID: "source"}, YAML: base}})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	cfg := config.Default()
	cfg.Mihomo.Binary = binary
	cfg.Mihomo.Profile = filepath.Join(dir, "profile.yaml")
	if os.WriteFile(cfg.Mihomo.Profile, profile, 0600) != nil {
		t.Fatal("profile fixture")
	}
	path, err := routing.PoolPath(profile, dir)
	if err != nil {
		t.Fatal(err)
	}
	old, err := routing.ReadPool(profile, dir)
	if err != nil {
		t.Fatal(err)
	}
	next := old
	next.Graph.Candidates = append([]routing.Candidate(nil), old.Graph.Candidates...)
	next.Graph.Candidates[0].SourceName = "renamed"
	next.Graph.Version = routing.PoolVersion(next.Graph.Candidates)
	payload, _ := yaml.Marshal(next)
	calls := 0
	err = updateStablePoolFile(context.Background(), cfg, path, payload, func(context.Context) error {
		calls++
		actual, e := routing.ReadPool(profile, dir)
		if e != nil {
			t.Fatal(e)
		}
		if calls == 1 {
			if actual.Graph.Version != next.Graph.Version {
				t.Fatal("new pool not staged")
			}
			return fmt.Errorf("simulated API failure")
		}
		if actual.Graph.Version != old.Graph.Version {
			t.Fatal("old fallback not restored before reconcile")
		}
		return nil
	})
	if err == nil || calls != 2 {
		t.Fatal(err, calls)
	}
	restored, err := routing.ReadPool(profile, dir)
	if err != nil || restored.Graph.Version != old.Graph.Version {
		t.Fatal("rollback failed", err)
	}
	calls = 0
	payload[0] = '!'
	if updateStablePoolFile(context.Background(), cfg, path, payload, func(context.Context) error { calls++; return nil }) == nil || calls != 0 {
		t.Fatal("invalid pool reached API")
	}
}
