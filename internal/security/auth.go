// Package security holds the agent's HTTP security middlewares:
// bearer-token authentication and explicit-origin CORS.
package security

import (
	"crypto/subtle"
	"net/http"
	"strings"
)

// CheckToken compares the Authorization header against the expected token
// in constant time. Expected format: "Bearer <token>".
func CheckToken(expected, header string) bool {
	if expected == "" {
		return false
	}
	const prefix = "Bearer "
	if !strings.HasPrefix(header, prefix) {
		return false
	}
	got := strings.TrimPrefix(header, prefix)
	if got == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(expected)) == 1
}

// CheckQueryToken compares a token passed as a URL query parameter
// (used only by the WebSocket endpoint, where browsers cannot set headers).
func CheckQueryToken(expected, got string) bool {
	if expected == "" || got == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(expected)) == 1
}

// RequireAuth wraps next and rejects requests without a valid bearer token.
func RequireAuth(expected string, unauthorized http.Handler, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !CheckToken(expected, r.Header.Get("Authorization")) {
			unauthorized.ServeHTTP(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}
