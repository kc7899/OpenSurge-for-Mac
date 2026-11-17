package routing

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func testTLSIssuer(t *testing.T) (*x509.Certificate, *ecdsa.PrivateKey, *x509.CertPool) {
	t.Helper()
	key, e := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	ca := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "isolated fixture CA"}, IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign, NotBefore: time.Now().Add(-24 * time.Hour), NotAfter: time.Now().Add(24 * time.Hour)}
	der, e := x509.CreateCertificate(rand.Reader, ca, ca, &key.PublicKey, key)
	if e != nil {
		t.Fatal(e)
	}
	ca, e = x509.ParseCertificate(der)
	if e != nil {
		t.Fatal(e)
	}
	pool := x509.NewCertPool()
	pool.AddCert(ca)
	return ca, key, pool
}
func testTLSService(t *testing.T, ca *x509.Certificate, issuer *ecdsa.PrivateKey, kind string) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	key, e := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	leaf := &x509.Certificate{SerialNumber: big.NewInt(2), DNSNames: []string{"chatgpt.com", "api.openai.com"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	if kind == "wrong_host" {
		leaf.DNSNames = []string{"other.invalid"}
	}
	if kind == "expired" {
		leaf.NotBefore = time.Now().Add(-2 * time.Hour)
		leaf.NotAfter = time.Now().Add(-time.Hour)
	}
	der, e := x509.CreateCertificate(rand.Reader, leaf, ca, &key.PublicKey, issuer)
	if e != nil {
		t.Fatal(e)
	}
	count := &atomic.Int32{}
	s := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { count.Add(1); w.WriteHeader(http.StatusForbidden) }))
	s.Config.ErrorLog = log.New(io.Discard, "", 0)
	s.TLS = &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der, ca.Raw}, PrivateKey: key}}}
	s.StartTLS()
	t.Cleanup(s.Close)
	return s, count
}
func testConnectProxy(t *testing.T, origin *httptest.Server) *httptest.Server {
	t.Helper()
	p := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodConnect {
			http.Error(w, "CONNECT required", 405)
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
	t.Cleanup(p.Close)
	return p
}

func TestTLSProbeStrictValidationWithoutHTTP(t *testing.T) {
	ca, key, roots := testTLSIssuer(t)
	for _, kind := range []string{"valid", "wrong_host", "expired", "untrusted"} {
		t.Run(kind, func(t *testing.T) {
			service, count := testTLSService(t, ca, key, kind)
			proxy := testConnectProxy(t, service)
			trust := roots
			if kind == "untrusted" {
				trust = x509.NewCertPool()
			}
			r := probeServiceTLS(context.Background(), proxy.Listener.Addr().String(), chatGPTTLSHosts, trust)
			want := map[string]string{"wrong_host": "hostname_mismatch", "expired": "invalid_validity", "untrusted": "untrusted_chain"}[kind]
			if r.Reachable != (kind == "valid") || r.CertificateError != want {
				t.Fatal(kind, r)
			}
			if count.Load() != 0 {
				t.Fatal("TLS-only check sent an HTTP request")
			}
		})
	}
}

type tlsCoreSelector struct {
	revision, base string
	client         *http.Client
	writes         int
}

func (s *tlsCoreSelector) Revision(context.Context) (string, error) { return s.revision, nil }
func (s *tlsCoreSelector) Current(ctx context.Context) (string, error) {
	r, e := http.NewRequestWithContext(ctx, "GET", s.base+"/proxies/"+url.PathEscape(GroupName), nil)
	if e != nil {
		return "", e
	}
	resp, e := s.client.Do(r)
	if e != nil {
		return "", e
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("selector not ready: %d", resp.StatusCode)
	}
	var g struct {
		Now string `json:"now"`
	}
	e = json.NewDecoder(resp.Body).Decode(&g)
	if e == nil && g.Now == "" {
		e = fmt.Errorf("selector has no actual choice")
	}
	return g.Now, e
}
func (s *tlsCoreSelector) Select(ctx context.Context, id string) error {
	b, _ := json.Marshal(map[string]string{"name": id})
	r, e := http.NewRequestWithContext(ctx, "PUT", s.base+"/proxies/"+url.PathEscape(GroupName), strings.NewReader(string(b)))
	if e != nil {
		return e
	}
	resp, e := s.client.Do(r)
	if e != nil {
		return e
	}
	defer resp.Body.Close()
	if resp.StatusCode != 204 {
		return fmt.Errorf("selection status %d", resp.StatusCode)
	}
	s.writes++
	return nil
}

func TestCertificateFailoverThroughRealIsolatedCore(t *testing.T) {
	binary := os.Getenv("OPENSURGE_TEST_CORE")
	if binary == "" {
		t.Skip("set OPENSURGE_TEST_CORE for TLS failover gate")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	ca, key, roots := testTLSIssuer(t)
	bad, _ := testTLSService(t, ca, key, "wrong_host")
	good, _ := testTLSService(t, ca, key, "valid")
	badProxy, goodProxy := testConnectProxy(t, bad), testConnectProxy(t, good)
	_, bp, _ := net.SplitHostPort(badProxy.Listener.Addr().String())
	_, gp, _ := net.SplitHostPort(goodProxy.Listener.Addr().String())
	base := []byte(fmt.Sprintf("proxies: [{name: bad, type: http, server: 127.0.0.1, port: %s}, {name: good, type: http, server: 127.0.0.1, port: %s}]\nproxy-groups: [{name: Proxy, type: select, proxies: [bad]}]\nrules: ['MATCH,Proxy']\n", bp, gp))
	raw, graph, e := Compose(base, []Input{{Source: Source{ID: "fixture-subscription"}, YAML: base}})
	if e != nil {
		t.Fatal(e)
	}
	pool, e := ReadPool(raw, "")
	if e != nil {
		t.Fatal(e)
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
	var doc map[string]any
	if yaml.Unmarshal(raw, &doc) != nil {
		t.Fatal("profile fixture")
	}
	doc["mixed-port"], doc["external-controller"] = mixed, fmt.Sprintf("127.0.0.1:%d", api)
	doc["tun"], doc["dns"], doc["allow-lan"], doc["bind-address"], doc["log-level"] = map[string]any{"enable": false}, map[string]any{"enable": false}, false, "127.0.0.1", "silent"
	raw, _ = yaml.Marshal(doc)
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if os.WriteFile(path, raw, 0600) != nil {
		t.Fatal("write fixture")
	}
	cmd := exec.CommandContext(ctx, binary, "-d", dir, "-f", path)
	if e = cmd.Start(); e != nil {
		t.Fatal(e)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	selector := &tlsCoreSelector{revision: graph.Version, base: fmt.Sprintf("http://127.0.0.1:%d", api), client: &http.Client{Timeout: time.Second}}
	for i := 0; i < 100; i++ {
		if _, e = selector.Current(ctx); e == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if e != nil {
		t.Fatal(e)
	}
	current := graph.Candidates[0].ID
	replacement := graph.Candidates[1].ID
	if e = selector.Select(ctx, current); e != nil {
		t.Fatal(e)
	}
	selector.writes = 0
	proxyURL, _ := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", mixed))
	tr := &http.Transport{Proxy: http.ProxyURL(proxyURL), TLSClientConfig: &tls.Config{RootCAs: roots}}
	defer tr.CloseIdleConnections()
	traffic := &http.Client{Transport: tr, Timeout: time.Second}
	if resp, e := traffic.Get("https://chatgpt.com/"); e == nil {
		resp.Body.Close()
		t.Fatal("bad certificate was accepted")
	}
	policy := DefaultPolicy()
	policy.Enabled = true
	snapshot := Snapshot{Version: graph.Version, Current: current, Sources: []Source{{ID: "fixture-subscription"}}, Candidates: graph.Candidates, NetworkReady: true}
	nodes := map[string]map[string]any{}
	for _, n := range pool.Proxies {
		nodes[n["name"].(string)] = n
	}
	probe := func(ctx context.Context, c Candidate, target int) ProbeResult {
		if target != 2 {
			return ProbeResult{Reachable: true, Country: "US", IP: "203.0.113.1", DelayMS: 10}
		}
		r, e := probeNodeWithServiceTrust(ctx, binary, nodes[c.ID], target, chatGPTTLSHosts, roots)
		if e != nil {
			return ProbeResult{InfrastructureError: true}
		}
		return r
	}
	m := &Monitor{}
	result, e := m.Check(ctx, policy, snapshot, selector, probe)
	if e != nil || !result.Changed || result.Decision.Target != replacement || selector.writes != 1 {
		t.Fatal(result, e, selector.writes)
	}
	resp, e := traffic.Get("https://chatgpt.com/")
	if e != nil {
		t.Fatal("replacement TLS failed", e)
	}
	resp.Body.Close()
	// HTTP 403 at a healthy TLS server is not a certificate failure.
	if resp.StatusCode != 403 {
		t.Fatal(resp.StatusCode)
	}
	snapshot.Current = replacement
	if r, e := m.Check(ctx, policy, snapshot, selector, probe); e != nil || r.Changed || selector.writes != 1 {
		t.Fatal("healthy replacement was changed", r, e)
	}
	after, _ := os.ReadFile(path)
	if string(after) != string(raw) {
		t.Fatal("main config was rewritten")
	}
	if current, e := selector.Current(ctx); e != nil || current != replacement {
		t.Fatal("current selection not preserved", current, e)
	}
	t.Log("strict TLS failure confirmed and isolated selector changed once; replacement HTTPS works even with HTTP 403; no config reload or restart")
}
