package controlapi

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
	"open-mihomo-gateway/internal/config"
	"open-mihomo-gateway/internal/mihomo"
	"open-mihomo-gateway/internal/routing"
)

type StablePoolRunner interface {
	UpdateStablePool(context.Context, string, string, []byte) error
}

func (c HelperClient) UpdateStablePool(ctx context.Context, path, revision string, payload []byte) error {
	_, err := c.call(ctx, HelperRequest{Action: "config-update-stable-pool", ConfigPath: path, Revision: revision, Payload: payload})
	return err
}
func (DirectRunner) UpdateStablePool(ctx context.Context, path, revision string, payload []byte) error {
	if os.Geteuid() != 0 {
		return fmt.Errorf("privileged helper is required")
	}
	return withConfigurationLifecycleLock(path, func() error {
		if revision != fileDigest(path) {
			return fmt.Errorf("configuration changed")
		}
		cfg, err := config.LoadRuntime(path)
		if err != nil {
			return err
		}
		applied, ok := currentBootRuntimeState(cfg)
		if !ok {
			return fmt.Errorf("gateway is not active")
		}
		digest, digestErr := config.MihomoProfileDigest(cfg)
		if digestErr != nil || digest != applied.ProfileDigest {
			return fmt.Errorf("applied profile changed")
		}
		profile, err := os.ReadFile(cfg.Mihomo.Profile)
		if err != nil {
			return err
		}
		poolPath, err := routing.PoolPath(profile, filepath.Dir(cfg.Mihomo.Profile))
		if err != nil {
			return err
		}
		return updateStablePoolFile(ctx, cfg, poolPath, payload, func(ctx context.Context) error {
			_, err := mihomo.UpdateProxyProvider(ctx, cfg, routing.PoolName)
			return err
		})
	})
}

// Validation starts no listeners or gateway. File replacement and provider API
// update share the existing lifecycle lock. No start/stop/reload is available.
func updateStablePoolFile(ctx context.Context, cfg config.Config, path string, payload []byte, update func(context.Context) error) error {
	if len(payload) == 0 || len(payload) > maxSourceSize {
		return fmt.Errorf("pool payload size invalid")
	}
	var pool routing.Pool
	if yaml.Unmarshal(payload, &pool) != nil {
		return fmt.Errorf("invalid pool document")
	}
	if err := pool.Validate(); err != nil {
		return err
	}
	dir, err := os.MkdirTemp("", "opensurge-pool-validation-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	doc := map[string]any{"proxies": pool.Proxies, "rules": []string{"MATCH,REJECT"}, "dns": map[string]any{"enable": false}, "tun": map[string]any{"enable": false}, "log-level": "silent"}
	raw, err := yaml.Marshal(doc)
	if err != nil {
		return err
	}
	probePath := filepath.Join(dir, "validate.yaml")
	if err = os.WriteFile(probePath, raw, 0600); err != nil {
		return err
	}
	validateCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(validateCtx, cfg.Mihomo.Binary, "-d", dir, "-t", "-f", probePath)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if cmd.Run() != nil {
		return fmt.Errorf("new node pool failed engine validation; existing pool preserved")
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	before, err := snapshotFile(path)
	if err != nil {
		return err
	}
	if !before.exists {
		profile, readErr := os.ReadFile(cfg.Mihomo.Profile)
		if readErr != nil {
			return readErr
		}
		old, readErr := routing.ReadPool(profile, filepath.Dir(cfg.Mihomo.Profile))
		if readErr != nil {
			return readErr
		}
		before.data, readErr = yaml.Marshal(old)
		if readErr != nil {
			return readErr
		}
		before.exists, before.mode = true, 0640
	}
	if err = writeAtomic(path, payload, 0640); err != nil {
		return err
	}
	if err = update(ctx); err != nil {
		rollbackErr := restoreFile(path, before)
		reconcileCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 8*time.Second)
		defer cancel()
		reconcileErr := update(reconcileCtx)
		if rollbackErr != nil || reconcileErr != nil {
			return fmt.Errorf("provider update outcome uncertain; reconciliation required")
		}
		return fmt.Errorf("provider update failed; previous pool restored")
	}
	return nil
}

// Called under sourcesMu after a subscription refresh. Healthy selected nodes
// retain their runtime ID. A removed/changed current node is retired until the
// monitor has verified a replacement, avoiding a provider fallback race.
func (s *Server) refreshStablePool(ctx context.Context) (bool, error) {
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()
	cfg, _, identity, err := s.stableIdentity()
	if err != nil {
		return false, err
	}
	revision := fileDigest(s.configPath)
	raw, err := os.ReadFile(cfg.Mihomo.Profile)
	if err != nil {
		return false, err
	}
	if _, err = routing.PoolPath(raw, filepath.Dir(cfg.Mihomo.Profile)); err != nil {
		return false, err
	}
	old, err := routing.ReadPool(raw, filepath.Dir(cfg.Mihomo.Profile))
	if err != nil {
		return false, err
	}
	library, err := s.store.Sources()
	if err != nil {
		return false, err
	}
	inputs := []routing.Input{}
	for _, source := range library {
		if !source.Valid || source.Kind != "mihomo_profile" {
			continue
		}
		data, _, err := readValidatedSourceSnapshot(s.store.Dir(), source)
		if err != nil {
			return false, err
		}
		inputs = append(inputs, routing.Input{Source: routing.Source{ID: source.ID, Name: source.Name}, YAML: data})
	}
	base := []byte("proxy-groups: [{name: Pool, type: select, proxies: [REJECT]}]\nrules: ['MATCH,Pool']\n")
	nextRaw, _, err := routing.Compose(base, inputs)
	if err != nil {
		return false, err
	}
	next, err := routing.ReadPool(nextRaw, "")
	if err != nil {
		return false, err
	}
	groups, err := s.fetchProxyGroups(ctx, cfg)
	if err != nil {
		return false, err
	}
	selected := ""
	for _, g := range groups {
		if g.Name == routing.GroupName {
			selected = g.Selected
		}
	}
	next = routing.RefreshPool(old, next, selected)
	if next.Graph.Version == old.Graph.Version {
		return true, nil
	}
	runner, ok := s.configRunner.(StablePoolRunner)
	if !ok {
		return false, fmt.Errorf("helper update required for dynamic node pools")
	}
	payload, err := yaml.Marshal(next)
	if err != nil {
		return false, err
	}
	if _, _, check, e := s.stableIdentity(); e != nil || check != identity {
		return false, routing.ErrStale
	}
	if err = runner.UpdateStablePool(ctx, s.configPath, revision, payload); err != nil {
		return false, err
	}
	select {
	case s.stable.wake <- struct{}{}:
	default:
	}
	return true, nil
}
