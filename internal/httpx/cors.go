package httpx

import (
	"net/http"
	"strconv"
	"strings"
)

// CORS allows the web front end, which is deployed as a separate origin from
// the API, to call it from a browser.
//
// The allowlist is exact-match and comes from config. There is deliberately no
// wildcard and no origin reflection: this API is authenticated with a bearer
// token that the browser holds, so echoing back whatever Origin arrives would
// let any site on the internet drive a logged-in user's wallet.
func CORS(allowed []string) func(http.Handler) http.Handler {
	allowSet := make(map[string]bool, len(allowed))
	for _, o := range allowed {
		if o = strings.TrimSpace(strings.TrimRight(o, "/")); o != "" {
			allowSet[o] = true
		}
	}

	const (
		allowMethods = "GET, POST, PATCH, DELETE, OPTIONS"
		allowHeaders = "Authorization, Content-Type, X-Request-Id"
		maxAge       = 86400
	)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := strings.TrimRight(r.Header.Get("Origin"), "/")

			if origin != "" && allowSet[origin] {
				h := w.Header()
				h.Set("Access-Control-Allow-Origin", origin)
				// Caches must not serve one origin's response to another.
				h.Add("Vary", "Origin")
				h.Set("Access-Control-Expose-Headers", "X-Request-Id, Retry-After")
				// No Allow-Credentials: the token travels in the Authorization
				// header, not a cookie, so there is nothing for the browser to
				// attach automatically and therefore no CSRF surface here.

				if r.Method == http.MethodOptions {
					h.Set("Access-Control-Allow-Methods", allowMethods)
					h.Set("Access-Control-Allow-Headers", allowHeaders)
					h.Set("Access-Control-Max-Age", strconv.Itoa(maxAge))
					w.WriteHeader(http.StatusNoContent)
					return
				}
			} else if r.Method == http.MethodOptions && origin != "" {
				// Disallowed origin preflighting: answer without the CORS
				// headers so the browser blocks it, rather than 404ing.
				w.WriteHeader(http.StatusForbidden)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
