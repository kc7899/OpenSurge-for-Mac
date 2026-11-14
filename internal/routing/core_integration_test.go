package routing

import (
	"context"
	"encoding/json"
	"fmt"
	"gopkg.in/yaml.v3"
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
)

// Run with OPENSURGE_TEST_CORE pointing to a pinned Mihomo executable. This
// starts a separate loopback-only core; no TUN, DHCP, system DNS or pf actions.
func TestIsolatedCoreCrossSourceSelection(t *testing.T) {
	binary := os.Getenv("OPENSURGE_TEST_CORE")
	if binary == "" {
		t.Skip("set OPENSURGE_TEST_CORE for isolated real-core validation")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	proxy := func(name string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Fixture-Source", name)
			w.WriteHeader(200)
			_, _ = io.WriteString(w, name)
		}))
	}
	a, b := proxy("subscription-a"), proxy("subscription-b")
	defer a.Close()
	defer b.Close()
	addr := func(s *httptest.Server) (string, string) {
		u, _ := url.Parse(s.URL)
		h, p, _ := net.SplitHostPort(u.Host)
		return h, p
	}
	ah, ap := addr(a)
	bh, bp := addr(b)
	port := func() int {
		l, e := net.Listen("tcp", "127.0.0.1:0")
		if e != nil {
			t.Fatal(e)
		}
		p := l.Addr().(*net.TCPAddr).Port
		_ = l.Close()
		return p
	}
	apiPort, mixed := port(), port()
	for mixed == apiPort {
		mixed = port()
	}
	dir := t.TempDir()
	for name, raw := range map[string]string{
		"a.yaml": fmt.Sprintf("proxies: [{name: 'source-a/node', type: http, server: '%s', port: %s}]\n", ah, ap),
		"b.yaml": fmt.Sprintf("proxies: [{name: 'source-b/node', type: http, server: '%s', port: %s}]\n", bh, bp),
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(raw), 0600); err != nil {
			t.Fatal(err)
		}
	}
	raw := fmt.Sprintf(`mixed-port: %d
bind-address: 127.0.0.1
allow-lan: false
external-controller: 127.0.0.1:%d
secret: isolated-fixture
mode: rule
log-level: error
dns: {enable: false}
tun: {enable: false}
proxy-providers:
  source-a: {type: file, path: ./a.yaml, health-check: {enable: false}}
  source-b: {type: file, path: ./b.yaml, health-check: {enable: false}}
proxy-groups:
  - {name: Stable, type: select, proxies: [REJECT], use: [source-a, source-b]}
rules:
  - DOMAIN,domestic.test,DIRECT
  - MATCH,Stable
`, mixed, apiPort)
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(ctx, binary, "-d", dir, "-f", path)
	log, err := os.Create(filepath.Join(dir, "core.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	cmd.Stdout = log
	cmd.Stderr = log
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	pid := cmd.Process.Pid
	// Validate the actual composition output with the same pinned engine.
	base := []byte("proxies: [{name: node, type: http, server: 127.0.0.1, port: 18080}]\nproxy-groups: [{name: Proxy, type: select, proxies: [node]}]\nrules: ['DOMAIN-SUFFIX,cn,DIRECT', 'MATCH,Proxy']\n")
	composed, _, composeErr := Compose(base, []Input{{Source: Source{ID: "source-a"}, YAML: base}, {Source: Source{ID: "source-b"}, YAML: base}})
	if composeErr != nil {
		t.Fatal(composeErr)
	}
	composedPath := filepath.Join(dir, "composed.yaml")
	if e := os.WriteFile(composedPath, composed, 0600); e != nil {
		t.Fatal(e)
	}
	if output, e := exec.CommandContext(ctx, binary, "-d", dir, "-t", "-f", composedPath).CombinedOutput(); e != nil {
		t.Fatalf("composed core config rejected: %s %v", output, e)
	}
	client := &http.Client{Timeout: time.Second}
	api := fmt.Sprintf("http://127.0.0.1:%d", apiPort)
	request := func(method, path, body string) ([]byte, int, error) {
		req, _ := http.NewRequestWithContext(ctx, method, api+path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer isolated-fixture")
		req.Header.Set("Content-Type", "application/json")
		resp, e := client.Do(req)
		if e != nil {
			return nil, 0, e
		}
		defer resp.Body.Close()
		data, e := io.ReadAll(resp.Body)
		return data, resp.StatusCode, e
	}
	ready := false
	for i := 0; i < 100; i++ {
		if _, code, e := request("GET", "/version", ""); e == nil && code == 200 {
			ready = true
			break
		}
		time.Sleep(30 * time.Millisecond)
	}
	if !ready {
		data, _ := os.ReadFile(filepath.Join(dir, "core.log"))
		t.Fatalf("isolated core not ready: %s", data)
	}
	u, _ := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", mixed))
	transport := &http.Transport{Proxy: http.ProxyURL(u)}
	defer transport.CloseIdleConnections()
	traffic := &http.Client{Transport: transport, Timeout: time.Second}
	for _, tc := range []struct{ node, source string }{{"source-a/node", "subscription-a"}, {"source-b/node", "subscription-b"}, {"source-a/node", "subscription-a"}} {
		if data, code, e := request("PUT", "/proxies/Stable", fmt.Sprintf(`{"name":%q}`, tc.node)); e != nil || code != 204 {
			t.Fatalf("select: %d %s %v", code, data, e)
		}
		resp, e := traffic.Get("http://foreign.test/check")
		if e != nil {
			t.Fatal(e)
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		if resp.Header.Get("X-Fixture-Source") != tc.source {
			t.Fatal("wrong egress", resp.Header)
		}
		// Per-node measurement must leave the live selection unchanged.
		_, _, _ = request("GET", "/proxies/"+url.PathEscape(tc.node)+"/delay?url="+url.QueryEscape("http://foreign.test/check")+"&timeout=1000", "")
		data, _, e := request("GET", "/proxies/Stable", "")
		if e != nil {
			t.Fatal(e)
		}
		var group struct {
			Now string `json:"now"`
		}
		if e = json.Unmarshal(data, &group); e != nil || group.Now != tc.node {
			t.Fatalf("probe changed selection: %s %v", data, e)
		}
	}
	if _, code, e := request("PUT", "/proxies/Stable", `{"name":"REJECT"}`); e != nil || code != 204 {
		t.Fatal("reject selection failed")
	}
	if resp, e := traffic.Get("http://foreign.test/check"); e == nil {
		_ = resp.Body.Close()
		if resp.StatusCode < 400 {
			t.Fatal("proxy traffic did not fail closed")
		}
	}
	after, _ := os.ReadFile(path)
	if string(after) != raw || cmd.Process.Pid != pid {
		t.Fatal("core configuration or process changed")
	}
	if _, code, e := request("GET", "/version", ""); e != nil || code != 200 {
		t.Fatal("core was restarted/stopped")
	}
	t.Logf("isolated PID %d preserved across cross-source selection, node probe and REJECT; configuration unchanged", pid)
}

// Optional private fixture validation never embeds credentials into test output
// or repository fixtures. The store must be a development copy, not live state.
func TestPrivateDevelopmentSubscriptions(t *testing.T) {
	store := os.Getenv("OPENSURGE_TEST_SOURCE_STORE")
	binary := os.Getenv("OPENSURGE_TEST_CORE")
	if store == "" || binary == "" {
		t.Skip("private development fixtures not requested")
	}
	data, err := os.ReadFile(filepath.Join(store, "sources.json"))
	if err != nil {
		t.Fatal("cannot read private fixture inventory")
	}
	var sources []struct {
		ID, Name     string
		SnapshotPath string `json:"snapshot_path"`
	}
	if json.Unmarshal(data, &sources) != nil {
		t.Fatal("invalid fixture inventory")
	}
	inputs := []Input{}
	var base []byte
	for _, source := range sources {
		raw, err := os.ReadFile(source.SnapshotPath)
		if err != nil {
			t.Fatal("cannot read private snapshot")
		}
		if base == nil {
			base = raw
		}
		inputs = append(inputs, Input{Source: Source{ID: source.ID, Name: source.Name}, YAML: raw})
	}
	result, graph, err := Compose(base, inputs)
	if err != nil {
		t.Fatal("private source composition failed; inspect input compatibility without exposing credentials")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "composed.yaml")
	if os.WriteFile(path, result, 0600) != nil {
		t.Fatal("cannot write isolated profile")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	// This is static validation only: no gateway, tunnel or public proxy listener.
	if _, err := exec.CommandContext(ctx, binary, "-d", dir, "-t", "-f", path).CombinedOutput(); err != nil {
		t.Fatal("private composed configuration failed core validation; diagnostic output withheld because it may contain credentials")
	}
	t.Logf("validated %d private sources and %d namespaced candidates; no gateway started", len(sources), len(graph.Candidates))
}

func TestPrivateDevelopmentExitProbes(t *testing.T) {
	if os.Getenv("OPENSURGE_TEST_LIVE_EXITS") != "1" {
		t.Skip("live exit probes not requested")
	}
	store := os.Getenv("OPENSURGE_TEST_SOURCE_STORE")
	binary := os.Getenv("OPENSURGE_TEST_CORE")
	if store == "" || binary == "" {
		t.Fatal("private fixture paths required")
	}
	data, err := os.ReadFile(filepath.Join(store, "sources.json"))
	if err != nil {
		t.Fatal("cannot read fixture inventory")
	}
	var sources []struct {
		ID           string
		SnapshotPath string `json:"snapshot_path"`
	}
	if json.Unmarshal(data, &sources) != nil {
		t.Fatal("invalid fixture inventory")
	}
	verified := 0
	for index, source := range sources {
		raw, err := os.ReadFile(source.SnapshotPath)
		if err != nil {
			t.Fatal("cannot read fixture")
		}
		var doc struct {
			Proxies []map[string]any `yaml:"proxies"`
		}
		if yaml.Unmarshal(raw, &doc) != nil {
			t.Fatal("invalid fixture")
		}
		var chosen map[string]any
		for _, node := range doc.Proxies {
			name, _ := node["name"].(string)
			if strings.Contains(name, "美国") || strings.Contains(strings.ToUpper(name), "US-") {
				chosen = node
				break
			}
		}
		if chosen == nil {
			t.Fatalf("source %d has no US-name probe seed", index+1)
		}
		result, err := ProbeNode(context.Background(), binary, chosen, 0)
		if err != nil {
			t.Fatalf("source %d: isolated probe infrastructure failed", index+1)
		}
		if !result.Reachable || result.Country != "US" || result.IP == "" {
			t.Logf("source %d: selected seed did not verify a US exit; candidate remains ineligible", index+1)
			continue
		}
		verified++
		t.Logf("source %d: HTTPS verified, country US, delay %d ms; private IP withheld", index+1, result.DelayMS)
	}
	if verified == 0 {
		t.Fatal("no seed verified; investigate source/network without switching live egress")
	}
}
