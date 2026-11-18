//go:build !linux

package pf

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"open-mihomo-gateway/internal/config"
	"open-mihomo-gateway/internal/process"
	"open-mihomo-gateway/internal/runtime"
)

type Manager struct {
	cfg   config.Config
	paths runtime.Paths
}

func New(cfg config.Config, paths runtime.Paths) Manager {
	return Manager{cfg: cfg, paths: paths}
}

func (m Manager) Check() error {
	if _, err := exec.LookPath("pfctl"); err != nil {
		return fmt.Errorf("pfctl not found in PATH")
	}
	return nil
}

func (m Manager) WriteAnchor() error {
	rendered, err := RenderAnchor(m.cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(m.paths.PFAnchor, []byte(rendered), 0o640)
}

func (m Manager) Enabled() (bool, error) {
	out, err := process.Output("pfctl", "-s", "info")
	if err != nil {
		return false, err
	}
	return parseEnabled(string(out)), nil
}

func (m Manager) Load(enablePF bool) error {
	if err := runPF("pfctl", "-a", m.cfg.PF.AnchorName, "-f", m.paths.PFAnchor); err != nil {
		return err
	}
	if enablePF {
		if err := runPF("pfctl", "-e"); err != nil {
			_ = m.Unload(false)
			return err
		}
	}
	return nil
}

func (m Manager) Unload(disablePF bool) error {
	err := runPF("pfctl", "-a", m.cfg.PF.AnchorName, "-F", "all")
	if disablePF {
		if disableErr := runPF("pfctl", "-d"); err == nil {
			err = disableErr
		}
	}
	return err
}

func (m Manager) Loaded() (bool, error) {
	anchorName := strings.TrimSpace(m.cfg.PF.AnchorName)
	if anchorName == "" {
		return false, nil
	}
	for _, kind := range []string{"rules", "nat"} {
		out, err := process.Output("pfctl", "-a", anchorName, "-s", kind)
		if err != nil {
			return false, err
		}
		if strings.TrimSpace(string(out)) != "" {
			return true, nil
		}
	}
	parent, child, nested := splitAnchor(anchorName)
	args := []string{"-s", "Anchors"}
	if nested {
		args = []string{"-a", parent, "-s", "Anchors"}
	}
	out, err := process.Output("pfctl", args...)
	if err != nil {
		return false, err
	}
	return anchorOutputContains(string(out), child), nil
}

func runPF(name string, args ...string) error {
	return process.Run(name, args...)
}
