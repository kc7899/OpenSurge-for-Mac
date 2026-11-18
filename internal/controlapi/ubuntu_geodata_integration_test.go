//go:build linux

package controlapi

import (
	"crypto/sha256"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"

	"open-mihomo-gateway/internal/config"
	"open-mihomo-gateway/internal/runtime"
)

// Run as root in a NEW network namespace with loopback only. This exercises
// the real import/apply validation, not a hand-written Mihomo -t equivalent.
func TestUbuntuOfflineImportedGeodata(t *testing.T) {
	if os.Getenv("OPENSURGE_OFFLINE_IMPORT_TEST") != "1" {
		t.Skip("explicit isolated offline import test required")
	}
	if os.Geteuid() != 0 {
		t.Fatal("isolated root test required")
	}
	interfaces, err := net.Interfaces()
	if err != nil {
		t.Fatal(err)
	}
	for _, iface := range interfaces {
		if iface.Flags&net.FlagLoopback == 0 {
			t.Fatalf("offline test refuses a namespace with non-loopback interface %s", iface.Name)
		}
	}
	core, cache := os.Getenv("OPENSURGE_TEST_CORE"), os.Getenv("OPENSURGE_TEST_GEODATA")
	if core == "" || cache == "" {
		t.Fatal("real core and packaged geodata required")
	}
	for _, datMode := range []bool{false, true} {
		t.Run(fmt.Sprintf("geodata-mode=%t", datMode), func(t *testing.T) {
			dir := t.TempDir()
			dataDir := filepath.Join(dir, "data")
			if err := os.Mkdir(dataDir, 0750); err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"GeoIP.dat", "GeoSite.dat", "geoip.metadb", "ASN.mmdb"} {
				data, err := os.ReadFile(filepath.Join(cache, name))
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dataDir, name), data, 0640); err != nil {
					t.Fatal(err)
				}
			}
			cfg := config.Default()
			cfg.Runtime.Dir = filepath.Join(dir, "runtime")
			cfg.Mihomo.Config = filepath.Join(cfg.Runtime.Dir, "mihomo.yaml")
			cfg.Mihomo.Binary = core
			if err := runtime.Ensure(runtime.NewPaths(cfg)); err != nil {
				t.Fatal(err)
			}
			configPath := filepath.Join(dir, "config.yaml")
			if err := writeAtomic(configPath, []byte(config.Render(cfg)), 0640); err != nil {
				t.Fatal(err)
			}
			payload := []byte(fmt.Sprintf(`geodata-mode: %t
proxies:
  - {name: Offline fixture, type: http, server: 192.0.2.1, port: 9}
proxy-groups:
  - {name: Proxy, type: select, proxies: [Offline fixture]}
rules:
  - GEOSITE,cn,DIRECT
  - GEOIP,CN,DIRECT,no-resolve
  - IP-ASN,13335,Proxy,no-resolve
  - MATCH,REJECT
`, datMode))
			digest := fmt.Sprintf("%x", sha256.Sum256(payload))
			result, err := (DirectRunner{}).ApplyProfile(t.Context(), configPath, fileDigest(configPath), payload, digest, "")
			if err != nil {
				t.Fatalf("offline import/apply failed: %v", err)
			}
			applied, err := config.Load(configPath)
			if err != nil || filepath.Dir(applied.Mihomo.Profile) != dataDir || result.Reloaded {
				t.Fatalf("unexpected import state: %+v %v", result, err)
			}
			if _, err := os.Stat(runtime.NewPaths(applied).StateFile); !os.IsNotExist(err) {
				t.Fatalf("offline validation started a gateway: %v", err)
			}
		})
	}
}
