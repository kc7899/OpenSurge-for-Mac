package routing

import (
	"gopkg.in/yaml.v3"
	"strings"
	"testing"
)

func TestComposeNamespacesAndPreservesRouting(t *testing.T) {
	base := []byte("proxies: [{name: Node, type: http, server: example.com, port: 443}]\nproxy-groups: [{name: Proxy, type: select, proxies: [Node]}]\nrules: ['DOMAIN-SUFFIX,cn,DIRECT', 'MATCH,Proxy']\ndns: {nameserver: [1.1.1.1]}\n")
	inputs := []Input{{Source: Source{ID: "one"}, YAML: base}, {Source: Source{ID: "two"}, YAML: base}}
	out, g, err := Compose(base, inputs)
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Candidates) != 2 || g.Candidates[0].ID == g.Candidates[1].ID {
		t.Fatal(g)
	}
	var doc map[string]any
	if err = yaml.Unmarshal(out, &doc); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "DOMAIN-SUFFIX,cn,DIRECT") || !strings.Contains(string(out), "1.1.1.1") || !strings.Contains(string(out), GroupName) {
		t.Fatal(string(out))
	}
	if string(inputs[0].YAML) != string(base) {
		t.Fatal("source mutated")
	}
	_, g2, err := Compose(base, inputs)
	if err != nil || g.Version != g2.Version {
		t.Fatal("unstable composition")
	}
	inputs[0].Source.Name = "renamed"
	_, g2, err = Compose(base, inputs)
	if err != nil || g.Candidates[0].ID != g2.Candidates[0].ID {
		t.Fatal("rename changed node identity")
	}
	inputs[0].YAML = []byte("proxy-providers: {remote: {type: http}}")
	if _, _, err = Compose(base, inputs); err == nil {
		t.Fatal("unresolved provider accepted")
	}
}
