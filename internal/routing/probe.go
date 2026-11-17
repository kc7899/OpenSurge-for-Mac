package routing

import (
	"context"
	"crypto/rand"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
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

type ProbeNetwork struct {
	Interface string
	Resolvers []string
}

// Bind probes to the physical uplink so a failed live outlet cannot capture
// the traffic intended to verify its replacement. This changes no host routes.
func ProbeNodeWithNetwork(ctx context.Context, binary string, node map[string]any, target int, network ProbeNetwork) (ProbeResult, error) {
	return probeNodeWithServiceTrust(ctx, binary, node, target, chatGPTTLSHosts, nil, network)
}

func probeNodeWithServiceTrust(ctx context.Context, binary string, node map[string]any, target int, hosts []string, roots *x509.CertPool, networks ...ProbeNetwork) (ProbeResult, error) {
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
	apiPort := port
	for apiPort == port {
		l, e := net.Listen("tcp4", "127.0.0.1:0")
		if e != nil {
			return ProbeResult{}, e
		}
		apiPort = l.Addr().(*net.TCPAddr).Port
		_ = l.Close()
	}
	secretBytes := make([]byte, 24)
	if _, err := rand.Read(secretBytes); err != nil {
		return ProbeResult{}, err
	}
	secret := hex.EncodeToString(secretBytes)
	name, _ := node["name"].(string)
	if name == "" {
		return ProbeResult{}, fmt.Errorf("node name missing")
	}
	if _, ok := node["dialer-proxy"]; ok {
		return ProbeResult{}, fmt.Errorf("unresolved probe dependency")
	}
	doc := map[string]any{"mixed-port": port, "bind-address": "127.0.0.1", "allow-lan": false, "mode": "rule", "log-level": "silent", "ipv6": false, "tun": map[string]any{"enable": false}, "proxies": []any{node}, "rules": []string{"MATCH," + name}, "dns": map[string]any{"enable": true, "nameserver": []string{"1.1.1.1", "8.8.8.8"}, "proxy-server-nameserver": []string{"1.1.1.1", "8.8.8.8"}}}
	if len(networks) > 0 {
		n := networks[0]
		if n.Interface != "" {
			if _, err := net.InterfaceByName(n.Interface); err != nil {
				return ProbeResult{}, fmt.Errorf("probe physical interface unavailable")
			}
			if specified, _ := node["interface-name"].(string); specified != "" && specified != n.Interface {
				return ProbeResult{}, fmt.Errorf("unresolved probe interface dependency")
			}
			doc["interface-name"] = n.Interface
		}
		if len(n.Resolvers) > 0 {
			doc["dns"] = map[string]any{"enable": true, "nameserver": n.Resolvers, "proxy-server-nameserver": n.Resolvers}
		}
	}
	doc["external-controller"], doc["secret"] = fmt.Sprintf("127.0.0.1:%d", apiPort), secret
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
	readyTransport := &http.Transport{Proxy: nil}
	defer readyTransport.CloseIdleConnections()
	readyClient := &http.Client{Transport: readyTransport, Timeout: 200 * time.Millisecond}
	ready := false
	for i := 0; i < 100; i++ {
		// A listening mixed port can precede the loaded proxy graph. Confirm
		// the intended node before treating a transport/TLS failure as evidence.
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("http://127.0.0.1:%d/proxies/", apiPort)+url.PathEscape(name), nil)
		req.Header.Set("Authorization", "Bearer "+secret)
		if response, e := readyClient.Do(req); e == nil {
			_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
			_ = response.Body.Close()
			if response.StatusCode == http.StatusOK {
				// Proxy inventory is published before routing rules. Without this
				// second fence the first TLS request can hit an incomplete graph.
				rulesRequest, _ := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("http://127.0.0.1:%d/rules", apiPort), nil)
				rulesRequest.Header.Set("Authorization", "Bearer "+secret)
				rulesReady := false
				if rulesResponse, err := readyClient.Do(rulesRequest); err == nil {
					var data struct {
						Rules []struct{ Type, Proxy string } `json:"rules"`
					}
					if rulesResponse.StatusCode == 200 && json.NewDecoder(io.LimitReader(rulesResponse.Body, 65536)).Decode(&data) == nil {
						for _, r := range data.Rules {
							if strings.EqualFold(r.Type, "match") && r.Proxy == name {
								rulesReady = true
							}
						}
					}
					_ = rulesResponse.Body.Close()
				}
				if conn, e := net.DialTimeout("tcp4", address, 20*time.Millisecond); e == nil {
					_ = conn.Close()
					if rulesReady {
						ready = true
						break
					}
				}
			}
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
	if target == 3 {
		testURL = "https://www.baidu.com/"
	}
	if target == 4 {
		testURL = "https://www.qq.com/"
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, testURL, nil)
	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return ProbeResult{}, nil
	}
	defer resp.Body.Close()
	if target == 3 || target == 4 {
		return ProbeResult{Reachable: resp.StatusCode >= 200 && resp.StatusCode < 400, DelayMS: int(time.Since(start).Milliseconds())}, nil
	}
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
