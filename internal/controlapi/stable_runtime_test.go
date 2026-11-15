package controlapi

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"open-mihomo-gateway/internal/config"
	"open-mihomo-gateway/internal/mihomo"
	"open-mihomo-gateway/internal/routing"
	runtimepkg "open-mihomo-gateway/internal/runtime"
)

func TestStableRuntimeKeepsHealthySelectionWithoutLifecycleRunner(t *testing.T) {
	store := NewStore(t.TempDir())
	if err := store.Ensure(); err != nil {
		t.Fatal(err)
	}
	p := routing.DefaultPolicy()
	p.Enabled = true
	settings, _ := json.Marshal(p)
	if err := writeAtomic(filepath.Join(store.Dir(), stableSettingsFile), settings, 0600); err != nil {
		t.Fatal(err)
	}
	base := []byte("proxies: [{name: node, type: http, server: 127.0.0.1, port: 18080}]\nproxy-groups: [{name: Proxy, type: select, proxies: [node]}]\nrules: ['MATCH,Proxy']\n")
	raw, graph, err := routing.Compose(base, []routing.Input{{Source: routing.Source{ID: "one"}, YAML: base}})
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.Gateway.Mode = config.GatewayModeSameLAN
	cfg.Transparent.Mode = config.TransparentModeTUN
	cfg.Transparent.TUNAutoDetectInterface = true
	cfg.DHCP.Enabled = false
	cfg.Runtime.Dir = filepath.Join(t.TempDir(), "runtime")
	cfg.Mihomo.ProfileMode = config.MihomoProfileModeImported
	cfg.Mihomo.Profile = filepath.Join(store.Dir(), "applied.yaml")
	cfg.Mihomo.Config = filepath.Join(cfg.Runtime.Dir, "mihomo.yaml")
	if err := os.MkdirAll(cfg.Runtime.Dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg.Mihomo.Profile, raw, 0600); err != nil {
		t.Fatal(err)
	}
	digest := mihomo.ProfileOverlayDigest(raw)
	graphRaw, _ := json.Marshal(graph)
	graphDir := filepath.Join(store.Dir(), "stable-routing-graphs")
	_ = os.MkdirAll(graphDir, 0700)
	if err := os.WriteFile(filepath.Join(graphDir, digest+".json"), graphRaw, 0600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(store.Dir(), "config.yaml")
	if err := os.WriteFile(path, []byte(config.Render(cfg)), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := config.LoadRuntime(path); err != nil {
		t.Fatalf("isolated config: %v", err)
	}
	boot, err := runtimepkg.CurrentBootSession()
	if err != nil {
		t.Skip("current boot identity unavailable")
	}
	state := runtimepkg.State{PIDMihomo: os.Getpid(), BootSessionID: boot.ID, ProfileDigest: digest, StartedAt: time.Now()}
	stateRaw, _ := json.Marshal(state)
	if err := os.WriteFile(runtimepkg.NewPaths(cfg).StateFile, stateRaw, 0600); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveSources([]Source{{ID: "one", Name: "Any Name", Kind: "mihomo_profile", Valid: true}}); err != nil {
		t.Fatal(err)
	}
	calls := 0
	current := graph.Candidates[0].ID
	s := &Server{store: store, configPath: path, fetchProxyGroups: func(context.Context, config.Config) ([]mihomo.ProxyGroup, error) {
		return []mihomo.ProxyGroup{{Name: routing.GroupName, Selected: current}}, nil
	}}
	s.stable.upstream = func(context.Context) bool { return true }
	s.stable.probe = func(context.Context, routing.Candidate, int) routing.ProbeResult {
		calls++
		return routing.ProbeResult{Reachable: true, Country: "US", IP: "1.1.1.1", DelayMS: 25}
	}
	_, revision, err := s.loadStablePolicy()
	if err != nil {
		t.Fatal(err)
	}
	previous := stableStatus{Quotas: map[string]routing.Quota{"one": {CheckedAt: time.Now(), Known: true, Total: 100}}}
	got := s.stableCycle(context.Background(), p, revision, previous)
	if got.Mode != "active" || got.Current != current || got.Reason != "current_healthy" || calls != 1 {
		t.Fatalf("status=%+v probes=%d", got, calls)
	}
	// No ActionRunner or ConfigurationRunner was supplied: a lifecycle call would
	// panic. The cycle only read configuration and observed the existing outlet.
	s.saveStableStatus(got)
	copy := s.stable.snapshot()
	copy.Quotas["one"] = routing.Quota{}
	if !s.stable.snapshot().Quotas["one"].Known {
		t.Fatal("status snapshot aliases mutable maps")
	}
}
