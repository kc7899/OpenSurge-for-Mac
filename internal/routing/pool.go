package routing

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"gopkg.in/yaml.v3"
)

const PoolName = "open-surge-stable-pool"

var poolFilePattern = regexp.MustCompile(`^stable-pool-[a-f0-9]{64}\.yaml$`)

type Pool struct {
	Proxies []map[string]any `yaml:"proxies"`
	Graph   Graph            `yaml:"x-opensurge-stable-routing"`
}

func PoolVersion(candidates []Candidate) string {
	raw, _ := yaml.Marshal(candidates)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
func PoolPath(profile []byte, baseDir string) (string, error) {
	var doc struct {
		Providers map[string]struct{ Type, Path string } `yaml:"proxy-providers"`
	}
	if yaml.Unmarshal(profile, &doc) != nil {
		return "", fmt.Errorf("invalid profile")
	}
	p, ok := doc.Providers[PoolName]
	if !ok || p.Type != "file" {
		return "", fmt.Errorf("running profile requires one-time dynamic pool migration")
	}
	name := filepath.Base(p.Path)
	if !poolFilePattern.MatchString(name) || filepath.Clean(p.Path) != name && filepath.Clean(p.Path) != "./"+name {
		return "", fmt.Errorf("invalid managed pool path")
	}
	return filepath.Join(baseDir, name), nil
}

// ReadPool uses the independently updated provider file when present, otherwise
// the immutable profile payload used for first start. Caller verifies applied
// profile identity separately; this function never writes a runtime file.
func ReadPool(profile []byte, baseDir string) (Pool, error) {
	var doc struct {
		Graph     Graph            `yaml:"x-opensurge-stable-routing"`
		Proxies   []map[string]any `yaml:"proxies"`
		Providers map[string]struct {
			Payload []map[string]any `yaml:"payload"`
		} `yaml:"proxy-providers"`
	}
	if yaml.Unmarshal(profile, &doc) != nil {
		return Pool{}, fmt.Errorf("invalid profile")
	}
	if baseDir == "" {
		pool := Pool{Graph: doc.Graph, Proxies: doc.Providers[PoolName].Payload}
		return pool, pool.Validate()
	}
	path, err := PoolPath(profile, baseDir)
	if err != nil {
		return Pool{Proxies: doc.Proxies, Graph: doc.Graph}, nil
	}
	f, e := os.Open(path)
	if e == nil {
		defer f.Close()
		info, e := f.Stat()
		if e != nil || !info.Mode().IsRegular() || info.Size() > 10<<20 {
			return Pool{}, fmt.Errorf("invalid pool file")
		}
		var pool Pool
		if yaml.NewDecoder(f).Decode(&pool) != nil {
			return Pool{}, fmt.Errorf("invalid pool data")
		}
		return pool, pool.Validate()
	}
	if !os.IsNotExist(e) {
		return Pool{}, e
	}
	pool := Pool{Graph: doc.Graph, Proxies: doc.Providers[PoolName].Payload}
	return pool, pool.Validate()
}
func (p Pool) Validate() error {
	if len(p.Graph.Candidates) != len(p.Proxies) || len(p.Proxies) == 0 || p.Graph.Version != PoolVersion(p.Graph.Candidates) {
		return fmt.Errorf("pool manifest mismatch")
	}
	byID := map[string]Candidate{}
	for _, c := range p.Graph.Candidates {
		if c.ID == "" || byID[c.ID].ID != "" {
			return fmt.Errorf("duplicate candidate")
		}
		identity := sha256.Sum256([]byte(c.SourceID + "\x00" + c.Name + "\x00" + c.Fingerprint))
		if c.SourceID == "" || c.ID != "stable-"+hex.EncodeToString(identity[:16]) {
			return fmt.Errorf("candidate identity mismatch")
		}
		byID[c.ID] = c
	}
	for _, node := range p.Proxies {
		name, _ := node["name"].(string)
		c, ok := byID[name]
		if !ok {
			return fmt.Errorf("unmapped node")
		}
		delete(byID, name)
		copy := map[string]any{}
		for k, v := range node {
			copy[k] = v
		}
		copy["name"] = c.Name
		raw, err := yaml.Marshal(copy)
		if err != nil {
			return fmt.Errorf("invalid node")
		}
		sum := sha256.Sum256(raw)
		if hex.EncodeToString(sum[:]) != c.Fingerprint {
			return fmt.Errorf("node fingerprint mismatch")
		}
		if _, ok := node["dialer-proxy"]; ok {
			return fmt.Errorf("unresolved chain")
		}
	}
	return nil
}

// RefreshPool retains a removed current outlet only as a retiring transition
// member. The decision layer must replace it; it is never offered as a backup.
func RefreshPool(current Pool, next Pool, selected string) Pool {
	present := map[string]bool{}
	for _, c := range next.Graph.Candidates {
		present[c.ID] = true
	}
	if !present[selected] {
		for _, c := range current.Graph.Candidates {
			if c.ID != selected {
				continue
			}
			c.Retiring = true
			next.Graph.Candidates = append(next.Graph.Candidates, c)
			for _, node := range current.Proxies {
				if node["name"] == selected {
					next.Proxies = append(next.Proxies, node)
					break
				}
			}
			break
		}
	}
	next.Graph.Version = PoolVersion(next.Graph.Candidates)
	return next
}
