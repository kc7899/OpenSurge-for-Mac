package controlapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestExplicitSSHForwardOriginKeepsCSRFBoundary(t *testing.T) {
	fixture := newTestServer(t)
	options := Options{ConfigPath: fixture.configPath, StoreDir: fixture.store.Dir(), Addr: "127.0.0.1:61767", BrowserOrigin: "http://127.0.0.1:16167"}
	s, err := New(options)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(s.BootstrapURL(), options.BrowserOrigin+"/bootstrap?") || s.addr != options.Addr {
		t.Fatal("SSH origin changed listener or bootstrap address")
	}
	s.sessions["browser-session"] = time.Now().Add(time.Hour)
	h := s.auth(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	for _, test := range []struct {
		origin string
		status int
	}{{options.BrowserOrigin, 204}, {"http://127.0.0.1:61767", 403}, {"https://untrusted.example", 403}, {"", 403}} {
		req := httptest.NewRequest(http.MethodPut, "http://127.0.0.1:16167/api/v1/config", nil)
		req.AddCookie(&http.Cookie{Name: "opensurge_session", Value: "browser-session"})
		req.Header.Set("Origin", test.origin)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code != test.status {
			t.Fatal(test, w.Code)
		}
	}
	for _, origin := range []string{"http://0.0.0.0:16167", "http://127.0.0.1:16167/path", "https://remote.example", "http://user@127.0.0.1:16167"} {
		options.BrowserOrigin = origin
		if _, err := New(options); err == nil {
			t.Fatal("accepted unsafe browser origin", origin)
		}
	}
}
