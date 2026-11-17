package controlapi

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
	"open-mihomo-gateway/internal/config"
	"open-mihomo-gateway/internal/mihomo"
	"open-mihomo-gateway/internal/routing"
	"open-mihomo-gateway/internal/runtime"
)

// Exercise the actual running policy workspace, not a fabricated /proxies
// response. Provider nodes are absent from that endpoint in the real engine.
// Even failed view probes must preserve the selected outlet and live traffic.
func TestStableProviderWorkspaceViewTestsPreserveTrafficRealCore(t *testing.T) {
	binary := os.Getenv("OPENSURGE_TEST_CORE")
	if binary == "" {
		t.Skip("set OPENSURGE_TEST_CORE for isolated workspace validation")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(3 * time.Millisecond)
		_, _ = io.WriteString(w, "healthy-outlet")
	}))
	defer origin.Close()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodConnect || strings.HasSuffix(r.Host, ":443") {
			http.Error(w, "fixture refuses external test target", http.StatusServiceUnavailable)
			return
		}
		remote, e := net.DialTimeout("tcp", origin.Listener.Addr().String(), time.Second)
		if e != nil {
			http.Error(w, "fixture unavailable", 502)
			return
		}
		defer remote.Close()
		client, _, e := w.(http.Hijacker).Hijack()
		if e != nil {
			return
		}
		defer client.Close()
		_, _ = io.WriteString(client, "HTTP/1.1 200 Connection Established\r\n\r\n")
		go func() { _, _ = io.Copy(remote, client); _ = remote.Close() }()
		_, _ = io.Copy(client, remote)
	}))
	defer upstream.Close()
	u, _ := url.Parse(upstream.URL)
	host, proxyPort, _ := net.SplitHostPort(u.Host)
	base := []byte(fmt.Sprintf("proxies: [{name: '🇺🇸 美国可用', type: http, server: '%s', port: %s}, {name: '🇯🇵 日本失败', type: http, server: 127.0.0.1, port: 1}]\nproxy-groups: [{name: Proxy, type: select, proxies: ['🇺🇸 美国可用']}]\nrules: ['MATCH,Proxy']\n", host, proxyPort))
	profile, graph, err := routing.Compose(base, []routing.Input{{Source: routing.Source{ID: "subscription-identity", Name: "任意订阅"}, YAML: base}})
	if err != nil {
		t.Fatal(err)
	}
	port := func() int {
		l, e := net.Listen("tcp4", "127.0.0.1:0")
		if e != nil {
			t.Fatal(e)
		}
		defer l.Close()
		return l.Addr().(*net.TCPAddr).Port
	}
	api, mixed := port(), port()
	for mixed == api {
		mixed = port()
	}
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DHCP.Enabled = false
	cfg.Runtime.Dir = filepath.Join(dir, "runtime")
	cfg.Mihomo.Binary = binary
	cfg.Mihomo.APIAddr = fmt.Sprintf("127.0.0.1:%d", api)
	cfg.Mihomo.Secret = "isolated-workspace-fixture"
	cfg.Mihomo.ProfileMode = config.MihomoProfileModeImported
	cfg.Mihomo.Profile = filepath.Join(dir, "profile.yaml")
	cfg.Mihomo.Config = filepath.Join(dir, "running.yaml")
	configPath := filepath.Join(dir, "config.yaml")
	if err = os.MkdirAll(cfg.Runtime.Dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(cfg.Mihomo.Profile, profile, 0600); err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err = yaml.Unmarshal(profile, &doc); err != nil {
		t.Fatal(err)
	}
	doc["mixed-port"], doc["external-controller"], doc["secret"] = mixed, cfg.Mihomo.APIAddr, cfg.Mihomo.Secret
	doc["bind-address"], doc["allow-lan"], doc["log-level"] = "127.0.0.1", false, "silent"
	doc["tun"], doc["dns"] = map[string]any{"enable": false}, map[string]any{"enable": false}
	running, _ := yaml.Marshal(doc)
	if err = os.WriteFile(cfg.Mihomo.Config, running, 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(configPath, []byte(config.Render(cfg)), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(ctx, binary, "-d", dir, "-f", cfg.Mihomo.Config)
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	for i := 0; i < 100; i++ {
		if _, err = mihomo.FetchVersion(ctx, cfg); err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	boot, err := runtime.CurrentBootSession()
	if err != nil {
		t.Fatal(err)
	}
	if err = runtime.SaveState(runtime.NewPaths(cfg).StateFile, runtime.State{BootSessionID: boot.ID, PIDMihomo: cmd.Process.Pid}); err != nil {
		t.Fatal(err)
	}
	selected := graph.Candidates[0].ID
	if err = mihomo.SelectProxyGroup(ctx, cfg, routing.GroupName, selected); err != nil {
		t.Fatal(err)
	}
	input := PolicyWorkspaceInput{Revision: fileDigest(configPath), ExpectedGatewayState: policyWorkspaceGatewayRunning, Request: PolicyWorkspaceRequest{Action: "read"}}
	view, err := runPolicyWorkspace(ctx, configPath, input)
	if err != nil {
		t.Fatal(err)
	}
	if view.Mode != "running" {
		t.Fatal(view.Mode)
	}
	names := []string{}
	for _, c := range graph.Candidates {
		found := false
		for _, h := range view.Health.Proxies {
			if h.Name == c.ID {
				found = true
				if h.DisplayName != c.Name+" · 任意订阅" || !h.Probeable || h.Status != "untested" {
					t.Fatal(h)
				}
				names = append(names, h.Name)
			}
		}
		if !found {
			t.Fatal("provider node missing from actual UI inventory", c.ID)
		}
	}
	// A reachable local target proves the provider-specific API is wired.
	probe := mihomo.MeasureProxyDelay(ctx, cfg, selected, origin.URL+"/ok", time.Second)
	if probe.Status != "reachable" {
		t.Fatal("provider node falsely unreachable", probe)
	}
	proxyURL, _ := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", mixed))
	tr := &http.Transport{Proxy: http.ProxyURL(proxyURL)}
	defer tr.CloseIdleConnections()
	traffic := &http.Client{Transport: tr, Timeout: time.Second}
	assertTraffic := func() {
		r, e := traffic.Get(origin.URL + "/traffic")
		if e != nil {
			t.Fatal(e)
		}
		defer r.Body.Close()
		b, _ := io.ReadAll(r.Body)
		if string(b) != "healthy-outlet" {
			t.Fatal("live traffic was displaced")
		}
	}
	assertTraffic()
	before, _ := os.Stat(cfg.Mihomo.Config)
	for i := 0; i < 2; i++ {
		input.Request = PolicyWorkspaceRequest{Action: "test", Names: names}
		view, err = runPolicyWorkspace(ctx, configPath, input)
		if err != nil {
			t.Fatal(err)
		}
		if len(view.Results) != 2 {
			t.Fatal("view did not test all members")
		}
		for _, r := range view.Results {
			if strings.Contains(r.Error, "Resource not found") {
				t.Fatal("used top-level endpoint for provider node", r)
			}
		}
		g := findWorkspaceGroup(view.Groups, routing.GroupName)
		if g == nil || g.Selected != selected {
			t.Fatal("view test changed current outlet", g)
		}
		assertTraffic()
	}
	after, _ := os.Stat(cfg.Mihomo.Config)
	data, _ := os.ReadFile(cfg.Mihomo.Config)
	if !before.ModTime().Equal(after.ModTime()) || string(data) != string(running) {
		t.Fatal("view test rewrote running config")
	}
	state, _, _ := runtime.LoadState(runtime.NewPaths(cfg).StateFile)
	if state.PIDMihomo != cmd.Process.Pid {
		t.Fatal("view test changed core identity")
	}
	t.Log("actual provider labels and probe, repeated view tests, unchanged selection/process/config and uninterrupted fixture traffic verified")
}
