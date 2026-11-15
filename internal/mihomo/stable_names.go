package mihomo

import (
	"open-mihomo-gateway/internal/config"
	"open-mihomo-gateway/internal/routing"
	"os"
	"path/filepath"
)

// IDs are selection keys; original names and country flags are presentation.
// Read only the applied pool, including provider updates, never a source draft.
func stablePolicyNames(cfg config.Config) map[string]string {
	names := map[string]string{}
	if cfg.Mihomo.ProfileMode != config.MihomoProfileModeImported || cfg.Mihomo.Profile == "" {
		return names
	}
	info, err := os.Stat(cfg.Mihomo.Profile)
	if err != nil || info.Size() > 10<<20 {
		return names
	}
	raw, err := os.ReadFile(cfg.Mihomo.Profile)
	if err != nil {
		return names
	}
	pool, err := routing.ReadPool(raw, filepath.Dir(cfg.Mihomo.Profile))
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
