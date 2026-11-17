package mihomo

import (
	"context"
	"net/http"
	"net/http/httptest"
	"open-mihomo-gateway/internal/config"
	"open-mihomo-gateway/internal/routing"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStableNodesExposeOriginalNamesWithoutChangingSelectionKeys(t *testing.T) {
	base := []byte("proxies: [{name: '🇺🇸 美国-US-1', type: http, server: example.com, port: 443}]\nproxy-groups: [{name: Proxy, type: select, proxies: ['🇺🇸 美国-US-1']}]\nrules: ['MATCH,Proxy']\n")
	raw, g, err := routing.Compose(base, []routing.Input{{Source: routing.Source{ID: "any-source"}, YAML: base}})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "profile.yaml")
	if os.WriteFile(path, raw, 0600) != nil {
		t.Fatal("write fixture")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Error("presentation must not mutate core")
		}
		switch r.URL.Path {
		case "/proxies":
			_, _ = w.Write([]byte(`{"proxies":{"Proxy":{"type":"Selector","all":["` + g.Candidates[0].ID + `"]}}}`))
		case "/providers/proxies/" + routing.PoolName:
			_, _ = w.Write([]byte(`{"proxies":[{"name":"` + g.Candidates[0].ID + `","type":"Trojan","udp":true,"alive":true}]}`))
		case "/providers/proxies/" + routing.PoolName + "/" + g.Candidates[0].ID + "/healthcheck":
			_, _ = w.Write([]byte(`{"delay":725}`))
		default:
			t.Error("unexpected endpoint", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	cfg := config.Default()
	cfg.Mihomo.ProfileMode = config.MihomoProfileModeImported
	cfg.Mihomo.Profile = path
	cfg.Mihomo.APIAddr = strings.TrimPrefix(server.URL, "http://")
	health, err := fetchProxyHealthWithClient(context.Background(), cfg, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	var node ProxyHealth
	for _, h := range health.Proxies {
		if h.Name == g.Candidates[0].ID {
			node = h
		}
	}
	if node.DisplayName != "🇺🇸 美国-US-1" || !node.Probeable || node.Status != "untested" {
		t.Fatal(health)
	}
	result := measureProxyDelayWithClient(context.Background(), cfg, server.Client(), node.Name, DefaultProxyDelayTestURL, 5*time.Second)
	if result.Status != "reachable" || result.DelayMS != 725 {
		t.Fatal("provider member was not measured through its own endpoint", result)
	}
	after, _ := os.ReadFile(path)
	if string(after) != string(raw) {
		t.Fatal("display read rewrote applied profile")
	}
}

func TestMissingDelayEndpointIsNotNodeUnreachable(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()
	cfg := config.Default()
	cfg.Mihomo.APIAddr = strings.TrimPrefix(server.URL, "http://")
	result := measureProxyDelayWithClient(context.Background(), cfg, server.Client(), "removed-node", DefaultProxyDelayTestURL, time.Second)
	if result.Status != "error" {
		t.Fatal("HTTP 404 must remain an API error, not an outage", result)
	}
}
