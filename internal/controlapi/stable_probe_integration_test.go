package controlapi

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"open-mihomo-gateway/internal/config"
	"open-mihomo-gateway/internal/process"
	"open-mihomo-gateway/internal/routing"
	runtimepkg "open-mihomo-gateway/internal/runtime"
)

func TestRootStableProbeUsesSavedNodeAndRejectsDrift(t *testing.T) {
	binary := os.Getenv("OPENSURGE_TEST_CORE")
	if binary == "" || os.Geteuid() != 0 {
		t.Skip("explicit isolated root probe test with pinned core required")
	}
	var hits atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.Method != http.MethodConnect {
			http.Error(w, "CONNECT required", 405)
			return
		}
		c, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			return
		}
		defer c.Close()
		// A deliberate non-TLS fixture proves the saved proxy was reached;
		// it cannot accidentally contact Cloudflare or accept an invalid CA.
		_, _ = io.WriteString(c, "HTTP/1.1 200 Connection Established\r\n\r\ninvalid-tls-fixture")
	}))
	defer upstream.Close()
	u, _ := url.Parse(upstream.URL)
	host, port, _ := net.SplitHostPort(u.Host)
	base := []byte("proxies: [{name: saved, type: http, server: '" + host + "', port: " + port + "}]\nproxy-groups: [{name: Proxy, type: select, proxies: [saved]}]\nrules: ['MATCH,REJECT']\n")
	profile, graph, err := routing.Compose(base, []routing.Input{{Source: routing.Source{ID: "saved-subscription"}, YAML: base}})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	cfg := config.Default()
	cfg.Mihomo.Binary = binary
	cfg.DHCP.Enabled = false
	cfg.Runtime.Dir = dir
	cfg.Mihomo.Config = filepath.Join(dir, "core.yaml")
	cfg.Mihomo.ProfileMode = config.MihomoProfileModeImported
	cfg.Mihomo.Profile = filepath.Join(dir, "profile.yaml")
	iface := "lo"
	if _, err := net.InterfaceByName(iface); err != nil {
		iface = "lo0"
	}
	cfg.Gateway.UpstreamInterface = iface
	if err := os.WriteFile(cfg.Mihomo.Profile, profile, 0600); err != nil {
		t.Fatal(err)
	}
	digest, err := config.MihomoProfileDigest(cfg)
	if err != nil {
		t.Fatal(err)
	}
	boot, err := runtimepkg.CurrentBootSession()
	if err != nil {
		t.Fatal(err)
	}
	fingerprint, err := process.Fingerprint(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	state := runtimepkg.State{BootSessionID: boot.ID, PIDMihomo: os.Getpid(), MihomoProcessFingerprint: fingerprint, ProfileDigest: digest}
	if err := runtimepkg.SaveState(runtimepkg.NewPaths(cfg).StateFile, state); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(config.Render(cfg)), 0600); err != nil {
		t.Fatal(err)
	}
	runner := DirectRunner{}
	result, err := runner.ProbeStable(context.Background(), path, StableProbeInput{CandidateID: graph.Candidates[0].ID, Target: 0})
	if err != nil || result.Reachable || hits.Load() == 0 {
		t.Fatal("saved physical proxy was not independently reached", result, err, hits.Load())
	}
	before := hits.Load()
	if _, err := runner.ProbeStable(context.Background(), path, StableProbeInput{CandidateID: "unsaved-node", Target: 0}); err == nil {
		t.Fatal("unsaved node accepted")
	}
	if err := os.WriteFile(cfg.Mihomo.Profile, append(profile, []byte("\n# changed\n")...), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := runner.ProbeStable(context.Background(), path, StableProbeInput{CandidateID: graph.Candidates[0].ID, Target: 0}); err == nil {
		t.Fatal("changed profile accepted")
	}
	if hits.Load() != before {
		t.Fatal("rejected probe touched a proxy")
	}
}
