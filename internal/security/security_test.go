package security

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCheckToken(t *testing.T) {
	if !CheckToken("secret", "Bearer secret") {
		t.Fatal("valid token rejected")
	}
	for _, h := range []string{"", "secret", "Bearer ", "Bearer wrong", "bearer secret", "Basic c2VjcmV0"} {
		if CheckToken("secret", h) {
			t.Fatalf("invalid header accepted: %q", h)
		}
	}
	if CheckToken("", "Bearer secret") {
		t.Fatal("empty expected token must never match")
	}
}

func TestCheckQueryToken(t *testing.T) {
	if !CheckQueryToken("secret", "secret") {
		t.Fatal("valid query token rejected")
	}
	if CheckQueryToken("secret", "") || CheckQueryToken("", "secret") || CheckQueryToken("a", "b") {
		t.Fatal("invalid query token accepted")
	}
}

func TestOriginAllowed(t *testing.T) {
	trusted := []string{"https://menuvex.ir", "http://localhost:3000"}
	if !OriginAllowed(trusted, "") {
		t.Fatal("empty origin must be allowed")
	}
	if !OriginAllowed(trusted, "https://menuvex.ir") {
		t.Fatal("trusted origin rejected")
	}
	if OriginAllowed(trusted, "https://evil.com") {
		t.Fatal("untrusted origin allowed")
	}
	if OriginAllowed(trusted, "https://menuvex.ir.evil.com") {
		t.Fatal("suffix trick accepted")
	}
	if !OriginAllowed([]string{"*"}, "https://anything.example") {
		t.Fatal("explicit wildcard must allow all")
	}
}

func TestRequireAuth(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusTeapot) })
	denied := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusUnauthorized) })
	h := RequireAuth("secret", denied, next)

	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}

	req = httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer secret")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusTeapot {
		t.Fatalf("expected passthrough, got %d", rec.Code)
	}
}

func TestCORS(t *testing.T) {
	trusted := []string{"http://localhost:3000"}
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	forbidden := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusForbidden) })
	h := CORS(trusted, forbidden, next)

	// No origin: passthrough without CORS headers.
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != http.StatusOK || rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("no-origin request mishandled: %d %v", rec.Code, rec.Header())
	}

	// Trusted origin: echo + vary.
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("trusted origin rejected: %d", rec.Code)
	}
	if rec.Header().Get("Access-Control-Allow-Origin") != "http://localhost:3000" {
		t.Fatalf("missing ACAO echo: %v", rec.Header())
	}

	// Untrusted origin: rejected.
	req = httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Origin", "https://evil.com")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rec.Code)
	}

	// Preflight from trusted origin: answered with 204 + allow headers.
	req = httptest.NewRequest("OPTIONS", "/", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	req.Header.Set("Access-Control-Request-Method", "POST")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 preflight, got %d", rec.Code)
	}
	if rec.Header().Get("Access-Control-Allow-Headers") == "" {
		t.Fatalf("missing ACAH: %v", rec.Header())
	}
}
