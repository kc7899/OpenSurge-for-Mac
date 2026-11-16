package mihomo

import (
	"fmt"
	"os"
	"path/filepath"

	"open-mihomo-gateway/internal/config"
	"open-mihomo-gateway/internal/routing"
)

// IDs are selection keys; original names and country flags are presentation.
// Read only the applied pool, including provider updates, never a source draft.
func stablePolicyNames(cfg config.Config) map[string]string {
	names := map[string]string{}
	pool, _, err := stablePolicyPool(cfg)
	if err != nil {
		return names
	}
	for _, c := range pool.Graph.Candidates {
		label := c.Name
		if c.Retiring {
			label += "（订阅已移除，等待切换）"
		}
		if c.SourceName != "" {
			label += " · " + c.SourceName
		}
		if c.ID != "" && c.Name != "" {
			names[c.ID] = label
		}
	}
	return names
}

func stablePolicyPool(cfg config.Config) (routing.Pool, bool, error) {
	if cfg.Mihomo.ProfileMode != config.MihomoProfileModeImported || cfg.Mihomo.Profile == "" {
		return routing.Pool{}, false, nil
	}
	info, err := os.Stat(cfg.Mihomo.Profile)
	if err != nil {
		return routing.Pool{}, false, err
	}
	if !info.Mode().IsRegular() || info.Size() > 10<<20 {
		return routing.Pool{}, false, fmt.Errorf("invalid applied profile file")
	}
	raw, err := os.ReadFile(cfg.Mihomo.Profile)
	if err != nil {
		return routing.Pool{}, false, err
	}
	pool, err := routing.ReadPool(raw, filepath.Dir(cfg.Mihomo.Profile))
	_, pathErr := routing.PoolPath(raw, filepath.Dir(cfg.Mihomo.Profile))
	return pool, pathErr == nil, err
}
