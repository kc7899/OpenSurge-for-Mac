package routing

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

const GroupName = "open-surge/stable-routing"
const graphMetadata = "x-opensurge-stable-routing"

type Graph struct {
	Version    string      `json:"version" yaml:"version"`
	Candidates []Candidate `json:"candidates" yaml:"candidates"`
}
type Input struct {
	Source Source
	YAML   []byte
}

// Compose keeps the selected base's DNS/rules and turns its user groups into
// aliases of one stable selector. Only explicit standalone nodes are accepted
// in v1: unresolved provider/dialer dependencies fail explicitly, never silently
// degrade into a partial failover pool. Inputs remain byte-for-byte unchanged.
func Compose(base []byte, inputs []Input) ([]byte, Graph, error) {
	var root map[string]any
	if err := yaml.Unmarshal(base, &root); err != nil {
		return nil, Graph{}, err
	}
	if root == nil {
		return nil, Graph{}, fmt.Errorf("base profile must be a mapping")
	}
	if _, exists := root[graphMetadata]; exists {
		return nil, Graph{}, fmt.Errorf("source uses reserved stable-routing metadata")
	}
	graph := Graph{Candidates: []Candidate{}}
	nodes := []any{}
	seen := map[string]bool{}
	sources := map[string]bool{}
	for _, input := range inputs {
		if input.Source.ID == "" || sources[input.Source.ID] {
			return nil, Graph{}, fmt.Errorf("missing or duplicate source ID")
		}
		sources[input.Source.ID] = true
		var doc struct {
			Proxies   []map[string]any `yaml:"proxies"`
			Providers map[string]any   `yaml:"proxy-providers"`
		}
		if err := yaml.Unmarshal(input.YAML, &doc); err != nil {
			return nil, Graph{}, fmt.Errorf("invalid source profile")
		}
		if len(doc.Providers) > 0 {
			return nil, Graph{}, fmt.Errorf("provider-backed source requires materialization before stable composition")
		}
		if len(doc.Proxies) == 0 {
			return nil, Graph{}, fmt.Errorf("source has no explicit proxy nodes")
		}
		for _, node := range doc.Proxies {
			name, _ := node["name"].(string)
			kind, _ := node["type"].(string)
			if name == "" || kind == "" {
				return nil, Graph{}, fmt.Errorf("node name and type are required")
			}
			if _, ok := node["dialer-proxy"]; ok {
				return nil, Graph{}, fmt.Errorf("chained node requires explicit dependency resolution")
			}
			switch strings.ToLower(kind) {
			case "direct", "reject", "reject-drop", "pass", "compatible", "dns", "tailscale":
				return nil, Graph{}, fmt.Errorf("unsupported automatic outlet type %q", kind)
			}
			raw, err := yaml.Marshal(node)
			if err != nil {
				return nil, Graph{}, err
			}
			digest := sha256.Sum256(raw)
			identity := sha256.Sum256([]byte(input.Source.ID + "\x00" + name + "\x00" + hex.EncodeToString(digest[:])))
			id := "stable-" + hex.EncodeToString(identity[:16])
			if seen[id] {
				return nil, Graph{}, fmt.Errorf("duplicate node identity")
			}
			seen[id] = true
			graph.Candidates = append(graph.Candidates, Candidate{ID: id, SourceName: input.Source.Name, SourceID: input.Source.ID, Name: name, Fingerprint: hex.EncodeToString(digest[:])})
			node["name"] = id
			nodes = append(nodes, node)
		}
	}
	if len(graph.Candidates) == 0 {
		return nil, Graph{}, fmt.Errorf("no automatic candidates")
	}
	groups, ok := root["proxy-groups"].([]any)
	if !ok {
		return nil, Graph{}, fmt.Errorf("base requires explicit policy groups")
	}
	aliases := []any{}
	for _, item := range groups {
		group, ok := item.(map[string]any)
		if !ok {
			return nil, Graph{}, fmt.Errorf("invalid group")
		}
		name, _ := group["name"].(string)
		if name == "" || name == GroupName || strings.HasPrefix(name, "open-surge/") {
			return nil, Graph{}, fmt.Errorf("reserved or empty group name")
		}
		aliases = append(aliases, map[string]any{"name": name, "type": "select", "proxies": []string{GroupName}})
	}
	// Explicit node targets in base rules would otherwise become dangling or
	// bypass the selector. Keep names as controlled aliases, with collisions fatal.
	groupNames := map[string]bool{}
	for _, item := range aliases {
		groupNames[item.(map[string]any)["name"].(string)] = true
	}
	if originals, ok := root["proxies"].([]any); ok {
		for _, item := range originals {
			n, ok := item.(map[string]any)
			if !ok {
				continue
			}
			name, _ := n["name"].(string)
			if name == "" || groupNames[name] || name == GroupName {
				return nil, Graph{}, fmt.Errorf("base target collision")
			}
			groupNames[name] = true
			aliases = append(aliases, map[string]any{"name": name, "type": "select", "proxies": []string{GroupName}})
		}
	}
	aliases = append(aliases, map[string]any{"name": GroupName, "type": "select", "proxies": []string{"REJECT"}, "use": []string{PoolName}})
	root["proxies"] = []any{}
	root["proxy-groups"] = aliases
	graph.Version = PoolVersion(graph.Candidates)
	root["proxy-providers"] = map[string]any{PoolName: map[string]any{"type": "file", "path": "./stable-pool-" + graph.Version + ".yaml", "payload": nodes, "health-check": map[string]any{"enable": false}}}
	root[graphMetadata] = graph
	out, err := yaml.Marshal(root)
	return out, graph, err
}
