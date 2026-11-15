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
		_, _ = w.Write([]byte(`{"proxies":{"` + g.Candidates[0].ID + `":{"type":"Trojan","udp":true}}}`))
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
	if len(health.Proxies) != 1 || health.Proxies[0].Name != g.Candidates[0].ID || health.Proxies[0].DisplayName != "🇺🇸 美国-US-1" {
		t.Fatal(health)
	}
	after, _ := os.ReadFile(path)
	if string(after) != string(raw) {
		t.Fatal("display read rewrote applied profile")
	}
}
