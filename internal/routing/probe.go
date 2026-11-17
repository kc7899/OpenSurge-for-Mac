package routing

import (
	"context"
	"crypto/x509"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// ProbeNode runs a short-lived loopback-only core with one standalone node.
// It cannot change live selectors, host DNS, routing or the running core.
// Node data and process output never appear in errors or logs.
func ProbeNode(ctx context.Context, binary string, node map[string]any, target int) (ProbeResult, error) {
	return probeNodeWithServiceTrust(ctx, binary, node, target, chatGPTTLSHosts, nil)
}

func probeNodeWithServiceTrust(ctx context.Context, binary string, node map[string]any, target int, hosts []string, roots *x509.CertPool) (ProbeResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	dir, err := os.MkdirTemp("", "opensurge-isolated-probe-")
	if err != nil {
		return ProbeResult{}, err
	}
	defer os.RemoveAll(dir)
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return ProbeResult{}, err
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	name, _ := node["name"].(string)
	if name == "" {
		return ProbeResult{}, fmt.Errorf("node name missing")
	}
	if _, ok := node["dialer-proxy"]; ok {
		return ProbeResult{}, fmt.Errorf("unresolved probe dependency")
	}
	doc := map[string]any{"mixed-port": port, "bind-address": "127.0.0.1", "allow-lan": false, "mode": "rule", "log-level": "silent", "ipv6": false, "tun": map[string]any{"enable": false}, "proxies": []any{node}, "rules": []string{"MATCH," + name}, "dns": map[string]any{"enable": true, "nameserver": []string{"1.1.1.1", "8.8.8.8"}, "proxy-server-nameserver": []string{"1.1.1.1", "8.8.8.8"}}}
	data, err := yaml.Marshal(doc)
	if err != nil {
		return ProbeResult{}, fmt.Errorf("cannot compose isolated probe")
	}
	path := filepath.Join(dir, "probe.yaml")
	if err := os.WriteFile(path, data, 0600); err != nil {
		return ProbeResult{}, err
	}
	cmd := exec.CommandContext(ctx, binary, "-d", dir, "-f", path)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return ProbeResult{}, fmt.Errorf("cannot start isolated core")
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	address := fmt.Sprintf("127.0.0.1:%d", port)
	ready := false
	for i := 0; i < 100; i++ {
		conn, err := net.DialTimeout("tcp4", address, 20*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			ready = true
			break
		}
		select {
		case <-ctx.Done():
			return ProbeResult{}, ctx.Err()
		case <-time.After(20 * time.Millisecond):
		}
	}
	if !ready {
		return ProbeResult{}, fmt.Errorf("isolated core not ready")
	}
	if target == 2 {
		return probeServiceTLS(ctx, address, hosts, roots), nil
	}
	proxyURL := &url.URL{Scheme: "http", Host: address}
	transport := &http.Transport{Proxy: http.ProxyURL(proxyURL), DisableKeepAlives: true}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 8 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	testURL := "https://www.cloudflare.com/cdn-cgi/trace"
	if target == 1 {
		testURL = "https://www.gstatic.com/generate_204"
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, testURL, nil)
	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return ProbeResult{}, nil
	}
	defer resp.Body.Close()
	data, err = io.ReadAll(io.LimitReader(resp.Body, 8193))
	if err != nil || len(data) > 8192 {
		return ProbeResult{}, nil
	}
	result := ProbeResult{Reachable: resp.StatusCode == 200 || resp.StatusCode == 204, DelayMS: int(time.Since(start).Milliseconds())}
	if target == 0 && resp.StatusCode == 200 {
		result.Country, result.IP = ParseTrace(data)
	}
	return result, nil
}

func ParseTrace(data []byte) (country, ip string) {
	seen := map[string]bool{}
	for _, line := range strings.Split(string(data), "\n") {
		key, val, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok || (key != "loc" && key != "ip") {
			continue
		}
		if seen[key] {
			return "", ""
		}
		seen[key] = true
		if key == "loc" {
			country = val
		} else {
			ip = val
		}
	}
	address, err := netip.ParseAddr(ip)
	if err != nil || !address.IsGlobalUnicast() || address.IsPrivate() || len(country) != 2 || country[0] < 'A' || country[0] > 'Z' || country[1] < 'A' || country[1] > 'Z' {
		return "", ""
	}
	return country, address.String()
}
