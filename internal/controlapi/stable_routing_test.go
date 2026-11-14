package controlapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"open-mihomo-gateway/internal/routing"
)

func TestStableSettingsAreRevisionedDrafts(t *testing.T) {
	store := NewStore(t.TempDir())
	if err := store.Ensure(); err != nil {
		t.Fatal(err)
	}
	// No lifecycle runner or config is supplied: saving drafts must not require
	// or attempt either. The endpoint can be used before a gateway is configured.
	s := &Server{store: store}
	if err := store.SaveSources([]Source{{ID: "alpha", Name: "订阅2", Kind: "mihomo_profile", Valid: true}, {ID: "beta", Name: "订阅10", Kind: "mihomo_profile", Valid: true}}); err != nil {
		t.Fatal(err)
	}
	get := httptest.NewRecorder()
	s.handleStableSettings(get, httptest.NewRequest(http.MethodGet, "/", nil))
	if get.Code != 200 {
		t.Fatal(get.Body.String())
	}
	var view stableSettingsResponse
	if err := json.Unmarshal(get.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	if view.Policy.Enabled || view.Policy.HealthSeconds != 60 || len(view.Sources) != 2 || view.Sources[0].ID != "alpha" {
		t.Fatal(view)
	}
	p := view.Policy
	p.Enabled = true
	p.SourceOrder = []string{"beta"}
	raw, _ := json.Marshal(p)
	put := func(revision string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPut, "/", bytes.NewReader(raw))
		req.Header.Set("If-Match", revision)
		w := httptest.NewRecorder()
		s.handleStableSettings(w, req)
		return w
	}
	if got := put("stale"); got.Code != 409 {
		t.Fatal(got.Code)
	}
	if got := put(view.Revision); got.Code != 200 {
		t.Fatal(got.Body.String())
	}
	saved, _, err := s.loadStablePolicy()
	if err != nil || !saved.Enabled || len(saved.SourceOrder) != 2 || saved.SourceOrder[0] != "beta" {
		t.Fatal(saved, err)
	}
	entries, err := os.ReadDir(store.Dir())
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.Name() == "config.yaml" || entry.Name() == "state.json" {
			t.Fatal("draft mutated runtime")
		}
	}
	info, err := os.Stat(filepath.Join(store.Dir(), stableSettingsFile))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal(info, err)
	}
	p = routing.DefaultPolicy()
	p.SourceOrder = []string{"missing"}
	raw, _ = json.Marshal(p)
	_, revision, _ := s.loadStablePolicy()
	if got := put(revision); got.Code != 422 {
		t.Fatal(got.Code)
	}
}

func TestPrivateDevelopmentLibraryLoads(t *testing.T) {
	path := os.Getenv("OPENSURGE_TEST_SOURCE_STORE")
	if path == "" {
		t.Skip("private development library not requested")
	}
	s := &Server{store: NewStore(path), configPath: filepath.Join(t.TempDir(), "unconfigured.yaml")}
	sources, err := s.store.Sources()
	if err != nil {
		t.Fatal("cannot load development library")
	}
	for _, source := range sources {
		if _, _, err := readValidatedSourceSnapshot(path, source); err != nil {
			t.Fatal("development snapshot failed managed-directory validation")
		}
	}
	if len(sources) == 0 {
		t.Fatal("development library is empty")
	}
	t.Logf("loaded %d copied subscriptions through the application's managed snapshot validator", len(sources))
}
