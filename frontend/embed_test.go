package frontend

import (
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

func TestIndexCSPNonce(t *testing.T) {
	h := Handler()
	seen := map[string]bool{}
	for _, path := range []string{"/", "/index.html", "/"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		match := regexp.MustCompile(`name="csp-nonce" content="([^"]+)"`).FindStringSubmatch(w.Body.String())
		if len(match) != 2 || match[1] == "__CSP_NONCE__" || seen[match[1]] {
			t.Fatalf("invalid/reused nonce: %v", match)
		}
		seen[match[1]] = true
		csp := w.Header().Get("Content-Security-Policy")
		if !strings.Contains(csp, "'nonce-"+match[1]+"'") || !strings.Contains(csp, "script-src 'self';") || strings.Contains(csp, "unsafe-eval") || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("invalid security headers: %v", w.Header())
		}
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("HEAD", "/", nil))
	if w.Body.Len() != 0 {
		t.Fatal("HEAD must have no body")
	}
}
