package controlapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"open-mihomo-gateway/internal/mihomo"
	"open-mihomo-gateway/internal/routing"
)

const stableSettingsFile = "stable-routing-settings-v1.json"

type stableSettingsResponse struct {
	Policy   routing.Policy   `json:"policy"`
	Revision string           `json:"revision"`
	Sources  []routing.Source `json:"sources"`
	Message  string           `json:"message"`
}

func (s *Server) loadStablePolicy() (routing.Policy, string, error) {
	p := routing.DefaultPolicy()
	raw, err := os.ReadFile(filepath.Join(s.store.Dir(), stableSettingsFile))
	if errors.Is(err, os.ErrNotExist) {
		b, _ := json.Marshal(p)
		return p, mihomo.ProfileOverlayDigest(b), nil
	}
	if err != nil {
		return p, "", err
	}
	if err = json.Unmarshal(raw, &p); err != nil {
		return p, "", fmt.Errorf("invalid stable routing settings")
	}
	if err = p.Validate(); err != nil {
		return p, "", err
	}
	return p, mihomo.ProfileOverlayDigest(raw), nil
}
func (s *Server) stableSourceList() ([]routing.Source, error) {
	sources, err := s.store.Sources()
	if err != nil {
		return nil, err
	}
	out := []routing.Source{}
	for _, source := range sources {
		if source.Valid && source.Kind == "mihomo_profile" {
			out = append(out, routing.Source{ID: source.ID, Name: source.Name})
		}
	}
	return out, nil
}

// Settings writes are drafts. They do not call a lifecycle action, select a
// node or silently replace the running composition.
func (s *Server) handleStableSettings(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, revision, err := s.loadStablePolicy()
	if err != nil {
		writeError(w, 500, "stable_settings_invalid", err.Error())
		return
	}
	sources, err := s.stableSourceList()
	if err != nil {
		writeError(w, 500, "sources_unavailable", "cannot read subscription inventory")
		return
	}
	if r.Method == http.MethodPut {
		if strings.Trim(r.Header.Get("If-Match"), `"`) != revision {
			writeError(w, 409, "revision_conflict", "reload settings before saving")
			return
		}
		var draft routing.Policy
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
		decoder.DisallowUnknownFields()
		if err = decoder.Decode(&draft); err != nil {
			writeError(w, 422, "invalid_settings", "invalid stable routing settings")
			return
		}
		if err = draft.Validate(); err != nil {
			writeError(w, 422, "invalid_settings", err.Error())
			return
		}
		known := map[string]bool{}
		for _, source := range sources {
			known[source.ID] = true
		}
		for _, id := range draft.SourceOrder {
			if !known[id] {
				writeError(w, 422, "unknown_source", "source was removed; reload settings")
				return
			}
		}
		// An explicitly saved order includes newly seen sources, so later renames
		// cannot reorder them behind the user's back.
		draft.SourceOrder = routing.ReconcileOrder(sources, draft.SourceOrder)
		raw, e := json.MarshalIndent(draft, "", "  ")
		if e != nil {
			writeError(w, 500, "save_failed", "cannot serialize settings")
			return
		}
		raw = append(raw, '\n')
		if e = writeAtomic(filepath.Join(s.store.Dir(), stableSettingsFile), raw, 0600); e != nil {
			writeError(w, 500, "save_failed", "cannot save settings")
			return
		}
		p = draft
		revision = mihomo.ProfileOverlayDigest(raw)
	}
	w.Header().Set("ETag", `"`+revision+`"`)
	writeJSON(w, 200, stableSettingsResponse{Policy: p, Revision: revision, Sources: routing.OrderedSources(sources, p.SourceOrder), Message: "Settings saved as a draft. Multi-source graph changes require explicit profile application; saving never reloads the gateway."})
}

func (s *Server) composeStableSources(composition mihomo.ProfileOverlayComposition) (mihomo.ProfileOverlayComposition, error) {
	p, _, err := s.loadStablePolicy()
	if err != nil {
		return composition, err
	}
	if !p.Enabled {
		return composition, nil
	}
	sources, err := s.store.Sources()
	if err != nil {
		return composition, err
	}
	inputs := []routing.Input{}
	for _, source := range sources {
		if !source.Valid || source.Kind != "mihomo_profile" {
			continue
		}
		data, _, err := readValidatedSourceSnapshot(s.store.Dir(), source)
		if err != nil {
			return composition, fmt.Errorf("stable source snapshot unavailable")
		}
		inputs = append(inputs, routing.Input{Source: routing.Source{ID: source.ID, Name: source.Name}, YAML: data})
	}
	data, graph, err := routing.Compose([]byte(composition.ProfileYAML), inputs)
	if err != nil {
		return composition, err
	}
	inspection, err := mihomo.InspectImportedProfile(data)
	if err != nil {
		return composition, err
	}
	composition.ProfileYAML = string(data)
	composition.Digest = mihomo.ProfileOverlayDigest(data)
	composition.Inspection = inspection
	raw, err := json.Marshal(graph)
	if err != nil {
		return composition, err
	}
	dir := filepath.Join(s.store.Dir(), "stable-routing-graphs")
	if err = os.MkdirAll(dir, 0700); err != nil {
		return composition, err
	}
	if err = writeAtomic(filepath.Join(dir, composition.Digest+".json"), raw, 0600); err != nil {
		return composition, err
	}
	return composition, nil
}
