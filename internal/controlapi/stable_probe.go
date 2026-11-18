package controlapi

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"open-mihomo-gateway/internal/config"
	"open-mihomo-gateway/internal/macosnetwork"
	"open-mihomo-gateway/internal/process"
	"open-mihomo-gateway/internal/routing"
)

type StableProbeInput struct {
	CandidateID string `json:"candidate_id"`
	Target      int    `json:"target"`
}
type stableProbeRunner interface {
	ProbeStable(context.Context, string, StableProbeInput) (routing.ProbeResult, error)
}

func (c HelperClient) ProbeStable(ctx context.Context, path string, input StableProbeInput) (routing.ProbeResult, error) {
	r, err := c.call(ctx, HelperRequest{Action: "stable-probe", ConfigPath: path, StableProbe: &input})
	if err != nil {
		return routing.ProbeResult{}, err
	}
	if r.ProbeResult == nil {
		return routing.ProbeResult{}, fmt.Errorf("helper did not confirm a probe result")
	}
	return *r.ProbeResult, nil
}

// Interface-bound Linux sockets require privilege on supported kernels. Keep
// this fixed operation in the existing root helper, not in the Web service.
// The caller supplies only a saved candidate ID and a fixed target number;
// never an executable, arbitrary node, URL, interface, trust root or config.
func (DirectRunner) ProbeStable(ctx context.Context, path string, input StableProbeInput) (routing.ProbeResult, error) {
	if input.Target < 0 || input.Target > 4 || (input.Target >= 3 && input.CandidateID != "") {
		return routing.ProbeResult{}, fmt.Errorf("invalid stable probe target")
	}
	if os.Geteuid() != 0 {
		return routing.ProbeResult{}, fmt.Errorf("privileged helper is required for physical uplink probes")
	}
	cfg, err := config.LoadRuntime(path)
	if err != nil {
		return routing.ProbeResult{}, err
	}
	state, exists := currentBootRuntimeState(cfg)
	if !exists || state.PIDMihomo <= 0 {
		return routing.ProbeResult{}, fmt.Errorf("active gateway is required")
	}
	if matches, err := process.MatchesFingerprint(state.PIDMihomo, state.MihomoProcessFingerprint); err != nil || !matches {
		return routing.ProbeResult{}, fmt.Errorf("gateway identity is not confirmed")
	}
	digest, err := config.MihomoProfileDigest(cfg)
	if err != nil || digest != state.ProfileDigest {
		return routing.ProbeResult{}, fmt.Errorf("applied profile changed")
	}
	network := routing.ProbeNetwork{Interface: cfg.Gateway.UpstreamInterface}
	if state.LocalSystemDNS != nil {
		network.Resolvers = append([]string(nil), state.LocalSystemDNS.Resolvers...)
	}
	if len(network.Resolvers) == 0 {
		if info, err := macosnetwork.Discover(ctx, cfg.Gateway.UpstreamInterface, cfg.Gateway.UpstreamInterface); err == nil {
			network.Resolvers = info.DNS
		}
	}
	if input.Target >= 3 {
		return routing.ProbeNodeWithNetwork(ctx, cfg.Mihomo.Binary, map[string]any{"name": "opensurge-physical-upstream", "type": "direct"}, input.Target, network)
	}
	raw, err := os.ReadFile(cfg.Mihomo.Profile)
	if err != nil {
		return routing.ProbeResult{}, err
	}
	pool, err := routing.ReadPool(raw, filepath.Dir(cfg.Mihomo.Profile))
	if err != nil {
		return routing.ProbeResult{}, err
	}
	for _, node := range pool.Proxies {
		if node["name"] == input.CandidateID {
			return routing.ProbeNodeWithNetwork(ctx, cfg.Mihomo.Binary, node, input.Target, network)
		}
	}
	return routing.ProbeResult{}, fmt.Errorf("candidate is no longer in the applied pool")
}
