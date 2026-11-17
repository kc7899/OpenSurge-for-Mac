package controlapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"open-mihomo-gateway/internal/config"
	"open-mihomo-gateway/internal/macosnetwork"
	"open-mihomo-gateway/internal/mihomo"
	"open-mihomo-gateway/internal/routing"
	runtimepkg "open-mihomo-gateway/internal/runtime"
)

type stableRuntime struct {
	upstream func(context.Context) bool
	probe    routing.ProbeFunc
	mu       sync.Mutex
	monitor  routing.Monitor
	wake     chan struct{}
	state    stableStatus
}
type stableStatus struct {
	ServiceTLS   *routing.ServiceTLSObservation `json:"service_tls,omitempty"`
	UpdatedAt    time.Time                      `json:"updated_at"`
	Mode         string                         `json:"mode"`
	Message      string                         `json:"message"`
	Current      string                         `json:"current"`
	SourceID     string                         `json:"source_id,omitempty"`
	Node         string                         `json:"node,omitempty"`
	IP           string                         `json:"ip,omitempty"`
	Country      string                         `json:"country,omitempty"`
	LastSwitch   time.Time                      `json:"last_switch,omitempty"`
	Reason       string                         `json:"reason,omitempty"`
	Observations map[string]routing.Observation `json:"observations,omitempty"`
	Quotas       map[string]routing.Quota       `json:"quotas,omitempty"`
}

func (c *stableRuntime) snapshot() stableStatus {
	c.mu.Lock()
	defer c.mu.Unlock()
	raw, _ := json.Marshal(c.state)
	var copy stableStatus
	_ = json.Unmarshal(raw, &copy)
	return copy
}
func (s *Server) saveStableStatus(status stableStatus) {
	status.UpdatedAt = time.Now()
	raw, err := json.Marshal(status)
	if err != nil {
		return
	}
	var copy stableStatus
	if json.Unmarshal(raw, &copy) != nil {
		return
	}
	s.stable.mu.Lock()
	s.stable.state = copy
	s.stable.mu.Unlock()
	_ = writeAtomic(filepath.Join(s.store.Dir(), "stable-routing-state-v1.json"), raw, 0600)
}
func (s *Server) handleStableCheck(w http.ResponseWriter, r *http.Request) {
	select {
	case s.stable.wake <- struct{}{}:
	default:
	}
	writeJSON(w, 202, map[string]string{"message": "已安排检查；健康线路保持当前选择"})
}

func (s *Server) monitorStableRouting(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	var state stableStatus
	_ = readJSON(filepath.Join(s.store.Dir(), "stable-routing-state-v1.json"), &state)
	if state.Quotas == nil {
		state.Quotas = map[string]routing.Quota{}
	}
	next := time.Time{}
	lastRevision := ""
	for {
		force := false
		select {
		case <-ctx.Done():
			return
		case <-s.stable.wake:
			force = true
		case <-ticker.C:
		}
		p, revision, err := s.loadStablePolicy()
		if err != nil {
			continue
		}
		if !p.Enabled {
			if state.Mode != "disabled" {
				state.Mode = "disabled"
				state.Message = "自动切换已关闭，保持当前线路"
				s.saveStableStatus(state)
			}
			continue
		}
		if !force && revision == lastRevision && time.Now().Before(next) {
			continue
		}
		lastRevision = revision
		next = time.Now().Add(time.Duration(p.HealthSeconds) * time.Second)
		cycleCtx, cancel := context.WithTimeout(ctx, 150*time.Second)
		state = s.stableCycle(cycleCtx, p, revision, state)
		cancel()
		s.saveStableStatus(state)
	}
}

func (s *Server) stableIdentity() (config.Config, routing.Graph, string, error) {
	cfg, err := config.LoadRuntime(s.configPath)
	if err != nil {
		return cfg, routing.Graph{}, "", fmt.Errorf("网络配置不可读取")
	}
	state, ok := currentBootRuntimeState(cfg)
	if !ok || state.PIDMihomo == 0 {
		return cfg, routing.Graph{}, "", fmt.Errorf("网关尚未运行；不会自动启动")
	}
	digest, err := config.MihomoProfileDigest(cfg)
	if err != nil || digest != state.ProfileDigest {
		return cfg, routing.Graph{}, "", fmt.Errorf("运行配置与草稿不同，等待显式应用")
	}
	raw, err := os.ReadFile(cfg.Mihomo.Profile)
	if err != nil {
		return cfg, routing.Graph{}, "", err
	}
	pool, err := routing.ReadPool(raw, filepath.Dir(cfg.Mihomo.Profile))
	graph := pool.Graph
	if err != nil || graph.Version == "" {
		return cfg, graph, "", fmt.Errorf("请先显式应用一次多订阅配置")
	}
	_, rev, err := s.loadStablePolicy()
	if err != nil {
		return cfg, graph, "", err
	}
	identity := fmt.Sprintf("%s:%s:%s:%d", graph.Version, rev, fileDigest(s.configPath), state.PIDMihomo)
	return cfg, graph, identity, nil
}

type stableSelector struct {
	s                 *Server
	cfg               config.Config
	version, expected string
}

func (a stableSelector) Revision(context.Context) (string, error) {
	_, _, v, e := a.s.stableIdentity()
	return v, e
}
func (a stableSelector) Current(ctx context.Context) (string, error) {
	groups, err := a.s.fetchProxyGroups(ctx, a.cfg)
	if err != nil {
		return "", err
	}
	for _, g := range groups {
		if g.Name == routing.GroupName {
			return g.Selected, nil
		}
	}
	return "", fmt.Errorf("多订阅选择组未应用")
}
func (a stableSelector) Select(ctx context.Context, target string) error {
	if !a.s.lifecycleMu.TryLock() {
		return routing.ErrBusy
	}
	defer a.s.lifecycleMu.Unlock()
	v, err := a.Revision(ctx)
	if err != nil || v != a.version {
		return routing.ErrStale
	}
	current, err := a.Current(ctx)
	if err != nil {
		return err
	}
	if current != a.expected {
		return routing.ErrStale
	}
	return mihomo.SelectProxyGroup(ctx, a.cfg, routing.GroupName, target)
}

func (s *Server) stableCycle(ctx context.Context, p routing.Policy, revision string, previous stableStatus) stableStatus {
	cfg, graph, version, err := s.stableIdentity()
	if err != nil {
		previous.Mode = "pending"
		previous.Message = err.Error()
		return previous
	}
	selector := stableSelector{s: s, cfg: cfg, version: version}
	current, err := selector.Current(ctx)
	if err != nil {
		previous.Mode = "unknown"
		previous.Message = "无法读取核心选择，不切线、不重启"
		return previous
	}
	selector.expected = current
	raw, err := os.ReadFile(cfg.Mihomo.Profile)
	if err != nil {
		previous.Mode = "unknown"
		previous.Message = "无法读取已应用的节点配置"
		return previous
	}
	pool, err := routing.ReadPool(raw, filepath.Dir(cfg.Mihomo.Profile))
	if err != nil {
		previous.Mode = "unknown"
		previous.Message = "无法读取运行节点池"
		return previous
	}
	nodes := map[string]map[string]any{}
	for _, node := range pool.Proxies {
		name, _ := node["name"].(string)
		nodes[name] = node
	}
	sources, err := s.stableSourceList()
	if err != nil {
		previous.Mode = "unknown"
		return previous
	}
	if previous.Quotas == nil {
		previous.Quotas = map[string]routing.Quota{}
	}
	for i := range sources {
		q := previous.Quotas[sources[i].ID]
		if time.Since(q.CheckedAt) >= time.Duration(p.QuotaSeconds)*time.Second {
			fresh := s.stableQuota(ctx, sources[i].ID)
			if fresh.Known || !fresh.ExpiresAt.IsZero() {
				q = fresh
			} else if !q.Known && q.ExpiresAt.IsZero() {
				q = fresh
			} else {
				q.Error = fresh.Error
			}
			previous.Quotas[sources[i].ID] = q
		}
		sources[i].Quota = q
	}
	upstream := s.stable.upstream
	network := routing.ProbeNetwork{Interface: cfg.Gateway.UpstreamInterface}
	if state, exists, err := runtimepkg.LoadState(runtimepkg.NewPaths(cfg).StateFile); err == nil && exists && state.LocalSystemDNS != nil {
		network.Resolvers = append([]string(nil), state.LocalSystemDNS.Resolvers...)
	}
	if len(network.Resolvers) == 0 && (s.stable.probe == nil || s.stable.upstream == nil) {
		if info, err := macosnetwork.Discover(ctx, cfg.Gateway.UpstreamInterface, cfg.Gateway.UpstreamInterface); err == nil {
			network.Resolvers = info.DNS
		}
	}
	if upstream == nil {
		upstream = func(ctx context.Context) bool { return routing.ProbeUpstream(ctx, cfg.Mihomo.Binary, network) }
	}
	networkReady := upstream(ctx)
	snapshot := routing.Snapshot{Version: version, Current: current, Sources: sources, Candidates: graph.Candidates, Observations: previous.Observations, NetworkReady: networkReady, ScanComplete: false}
	result, err := s.stable.monitor.Check(ctx, p, snapshot, selector, func(probeCtx context.Context, c routing.Candidate, target int) routing.ProbeResult {
		if s.stable.probe != nil {
			return s.stable.probe(probeCtx, c, target)
		}
		node := nodes[c.ID]
		if node == nil {
			return routing.ProbeResult{InfrastructureError: true}
		}
		result, err := routing.ProbeNodeWithNetwork(probeCtx, cfg.Mihomo.Binary, node, target, network)
		if err != nil {
			return routing.ProbeResult{InfrastructureError: true}
		}
		return result
	})
	previous.Current = current
	previous.Mode = "active"
	if err != nil {
		previous.Mode = "unknown"
		previous.Message = "本次检查未完成，未确认切换；稍后重试"
		return previous
	}
	previous.Observations = result.Observations
	previous.Reason = result.Decision.Reason
	if result.Changed {
		previous.Current = result.Decision.Target
		previous.LastSwitch = time.Now()
	}
	previous.SourceID = ""
	previous.Node = ""
	previous.Country = ""
	previous.IP = ""
	previous.ServiceTLS = nil
	for _, c := range graph.Candidates {
		if c.ID == previous.Current {
			previous.SourceID = c.SourceID
			previous.Node = c.Name
			o := result.Observations[c.ID]
			previous.Country = o.Country
			previous.IP = o.IP
			previous.ServiceTLS = o.ServiceTLS
		}
	}
	retired := false
	for _, c := range pool.Graph.Candidates {
		if c.Retiring && c.ID != previous.Current {
			retired = true
		}
	}
	if retired && ctx.Err() == nil && s.sourcesMu.TryLock() {
		_, _ = s.refreshStablePool(ctx)
		s.sourcesMu.Unlock()
	}
	previous.Message = "线路检查完成；可用时保持当前选择"
	if p.ChatGPTTLS && previous.ServiceTLS != nil {
		switch previous.ServiceTLS.Status {
		case "certificate_failed":
			previous.Message = "ChatGPT 证书异常已复测确认；仅在合格备用验证通过后切换，暂无合格备用时保留当前线路"
		case "unknown", "recovery_pending":
			previous.Message = "ChatGPT 证书检测尚未确认；保持当前线路，不凭超时切换"
		}
	}
	if previous.Reason == "service_certificate_failure" && result.Changed {
		previous.Message = "已切换：原线路 ChatGPT 证书异常经复测确认，备用线路已通过证书与地区验证"
	}
	if previous.Current == routing.Reject {
		previous.Mode = "waiting"
		previous.Message = "无合格代理出口，等待恢复；直连规则继续生效"
	}
	return previous
}
func (s *Server) stableQuota(ctx context.Context, id string) routing.Quota {
	unknown := routing.Quota{CheckedAt: time.Now(), Error: "额度未知"}
	address, err := s.credentials.Get(ctx, id)
	if err != nil {
		return unknown
	}
	req, err := newSourceRequest(ctx, address)
	if err != nil {
		return unknown
	}
	transport := &http.Transport{DialContext: safeDialContext}
	defer transport.CloseIdleConnections()
	client := &http.Client{Timeout: 8 * time.Second, Transport: transport, CheckRedirect: func(r *http.Request, via []*http.Request) error {
		if len(via) >= 3 || r.URL.Scheme != "https" {
			return fmt.Errorf("unsafe redirect")
		}
		return nil
	}}
	resp, err := client.Do(req)
	if err != nil {
		return unknown
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		unknown.Error = fmt.Sprintf("订阅查询 HTTP %d，未判定为耗尽", resp.StatusCode)
		return unknown
	}
	return routing.ParseQuota(resp.Header.Get("subscription-userinfo"), time.Now())
}
