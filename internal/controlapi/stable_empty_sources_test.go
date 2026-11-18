package controlapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestStableSettingsEmptySourcesAreJSONArray(t *testing.T) {
	store := NewStore(t.TempDir())
	if err := store.Ensure(); err != nil {
		t.Fatal(err)
	}
	s := &Server{store: store}
	response := httptest.NewRecorder()
	s.handleStableSettings(response, httptest.NewRequest(http.MethodGet, "/api/v1/stable-routing", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("HTTP %d", response.Code)
	}
	var body struct {
		Sources json.RawMessage `json:"sources"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if string(body.Sources) != "[]" {
		t.Fatalf("sources = %s; want []", body.Sources)
	}
}
