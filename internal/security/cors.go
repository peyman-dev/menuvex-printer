package security

import (
	"net/http"
	"strings"
)

// OriginAllowed reports whether an Origin header value is trusted.
// An empty origin (curl, native apps, same-origin navigations without
// Origin) is always allowed; only browsers send Origin, and only
// explicitly configured origins pass.
func OriginAllowed(trusted []string, origin string) bool {
	if origin == "" {
		return true
	}
	for _, t := range trusted {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		if t == "*" || t == origin {
			return true
		}
	}
	return false
}

// CORS wraps next with explicit-origin CORS handling:
//
//   - Requests without Origin pass through untouched.
//   - Requests with a trusted Origin get ACAO + Vary headers.
//   - OPTIONS preflights from trusted origins are answered directly.
//   - Requests with an untrusted Origin are rejected via forbidden.
func CORS(trusted []string, forbidden http.Handler, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin == "" {
			next.ServeHTTP(w, r)
			return
		}
		if !OriginAllowed(trusted, origin) {
			forbidden.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Vary", "Origin")
		if r.Method == http.MethodOptions {
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
			w.Header().Set("Access-Control-Max-Age", "86400")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
