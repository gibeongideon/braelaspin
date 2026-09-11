package httpx

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/dibon/braelaspin/internal/auth"
	"github.com/dibon/braelaspin/internal/rds"
)

type ctxKey int

const (
	ctxRequestID ctxKey = iota
	ctxLogger
	ctxClaims
	ctxRealIP
)

func RequestID(ctx context.Context) string {
	if v, ok := ctx.Value(ctxRequestID).(string); ok {
		return v
	}
	return ""
}

// Logger returns the request-scoped logger, which already carries request_id
// and user_id. Every log line in a handler should come from here so a single
// request_id retrieves the complete server-side story of a failure.
func Logger(ctx context.Context) *slog.Logger {
	if v, ok := ctx.Value(ctxLogger).(*slog.Logger); ok {
		return v
	}
	return slog.Default()
}

func Claims(ctx context.Context) *auth.Claims {
	if v, ok := ctx.Value(ctxClaims).(*auth.Claims); ok {
		return v
	}
	return nil
}

// UserID returns the authenticated user, or 0. Handlers behind RequireAuth can
// rely on it being non-zero.
func UserID(ctx context.Context) int64 {
	c := Claims(ctx)
	if c == nil {
		return 0
	}
	id, err := c.UserID()
	if err != nil {
		return 0
	}
	return id
}

func RealIP(ctx context.Context) string {
	if v, ok := ctx.Value(ctxRealIP).(string); ok {
		return v
	}
	return ""
}

// WithRequestID assigns an id and a scoped logger to every request.
func WithRequestID(base *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := r.Header.Get("X-Request-Id")
			if id == "" || len(id) > 64 {
				b := make([]byte, 8)
				_, _ = rand.Read(b)
				id = hex.EncodeToString(b)
			}
			lg := base.With("request_id", id)
			ctx := context.WithValue(r.Context(), ctxRequestID, id)
			ctx = context.WithValue(ctx, ctxLogger, lg)
			w.Header().Set("X-Request-Id", id)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// WithRealIP resolves the client address, trusting X-Forwarded-For only when
// the immediate peer is a configured proxy.
//
// This matters because the M-Pesa webhook allowlist is checked against this
// value. Trusting the header unconditionally would let anyone spoof a
// Safaricom source address by setting one.
func WithRealIP(trusted []*net.IPNet) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := peerIP(r.RemoteAddr)
			if isTrusted(ip, trusted) {
				if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
					// Left-most entry is the original client.
					if first := strings.TrimSpace(strings.Split(fwd, ",")[0]); first != "" {
						ip = first
					}
				} else if rip := strings.TrimSpace(r.Header.Get("X-Real-Ip")); rip != "" {
					ip = rip
				}
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxRealIP, ip)))
		})
	}
}

func peerIP(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return remoteAddr
	}
	return host
}

func isTrusted(ip string, trusted []*net.IPNet) bool {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return false
	}
	for _, n := range trusted {
		if n.Contains(parsed) {
			return true
		}
	}
	return false
}

// Recover turns a panic into a 500 rather than a dropped connection, and logs
// the stack. A panic on the money path must never take the process down with
// other requests in flight.
func Recover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				Logger(r.Context()).Error("panic in handler",
					"panic", rec, "path", routePattern(r), "stack", string(debug.Stack()))
				Fail(w, r, ErrInternal())
			}
		}()
		next.ServeHTTP(w, r)
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (w *statusWriter) WriteHeader(code int) {
	if w.status == 0 {
		w.status = code
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	n, err := w.ResponseWriter.Write(b)
	w.bytes += n
	return n, err
}

// Unwrap lets http.ResponseController reach the underlying writer, which the
// WebSocket upgrade needs.
func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// LogRequests writes exactly one line per request.
//
// It logs the route PATTERN rather than the raw URL, so cardinality stays
// bounded and /v1/game/spins/12345 does not become its own metric series.
func LogRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w}
		next.ServeHTTP(sw, r)

		if sw.status == 0 {
			sw.status = http.StatusOK
		}
		lg := Logger(r.Context())
		attrs := []any{
			"method", r.Method,
			"route", routePattern(r),
			"status", sw.status,
			"duration_ms", time.Since(start).Milliseconds(),
			"bytes", sw.bytes,
			"ip", RealIP(r.Context()),
		}
		if uid := UserID(r.Context()); uid != 0 {
			attrs = append(attrs, "user_id", uid)
		}
		lg.Info("request", attrs...)
	})
}

func routePattern(r *http.Request) string {
	if rc := chi.RouteContext(r.Context()); rc != nil {
		if p := rc.RoutePattern(); p != "" {
			return p
		}
	}
	return r.URL.Path
}

// RequireAuth validates the bearer token and puts the claims in the context.
func RequireAuth(ts *auth.TokenService) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			raw := bearer(r)
			if raw == "" {
				Fail(w, r, ErrUnauthorized())
				return
			}
			claims, err := ts.VerifyAccess(r.Context(), raw)
			if err != nil {
				switch {
				case errors.Is(err, auth.ErrUserSuspended):
					Fail(w, r, Errorf(http.StatusForbidden, "account_suspended",
						"This account has been suspended. Contact support.").WithCause(err))
				case errors.Is(err, auth.ErrTokenInvalid):
					Fail(w, r, ErrUnauthorized().WithCause(err))
				default:
					// A Redis failure during the suspension check lands here.
					// Failing closed is correct: we cannot prove this user is
					// allowed in.
					Fail(w, r, ErrInternal().WithCause(err))
				}
				return
			}

			ctx := context.WithValue(r.Context(), ctxClaims, claims)
			if uid, err := claims.UserID(); err == nil {
				ctx = context.WithValue(ctx, ctxLogger, Logger(ctx).With("user_id", uid))
			}
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequireAdmin gates the admin surface. It must be chained after RequireAuth.
func RequireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c := Claims(r.Context())
		if c == nil || !c.IsAdmin {
			Fail(w, r, ErrForbidden())
			return
		}
		next.ServeHTTP(w, r)
	})
}

func bearer(r *http.Request) string {
	h := r.Header.Get("Authorization")
	const p = "Bearer "
	if len(h) > len(p) && strings.EqualFold(h[:len(p)], p) {
		return strings.TrimSpace(h[len(p):])
	}
	return ""
}

// RateLimitSpec describes one limit.
type RateLimitSpec struct {
	Action string
	Limit  int
	Window time.Duration
	// ByUser limits per authenticated user; otherwise per client IP.
	ByUser bool
}

// RateLimit applies a fixed-window limit.
//
// A Redis outage fails OPEN here, deliberately: rate limiting is abuse
// control, not authorisation, and taking the whole API down because the
// limiter is unavailable trades a small risk for a large one. The suspension
// check in RequireAuth fails CLOSED, because that one is authorisation.
func RateLimit(r *rds.Client, spec RateLimitSpec) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			subject := RealIP(req.Context())
			if spec.ByUser {
				if uid := UserID(req.Context()); uid != 0 {
					subject = "u" + strconv.FormatInt(uid, 10)
				}
			}

			ok, retryAfter, err := r.Allow(req.Context(), spec.Action, subject, spec.Limit, spec.Window)
			if err != nil {
				Logger(req.Context()).Warn("rate limiter unavailable, allowing request",
					"action", spec.Action, "err", err)
				next.ServeHTTP(w, req)
				return
			}
			if !ok {
				w.Header().Set("Retry-After", strconv.Itoa(int(retryAfter.Seconds())+1))
				Fail(w, req, ErrRateLimited().WithMeta("retry_after_seconds", int(retryAfter.Seconds())+1))
				return
			}
			next.ServeHTTP(w, req)
		})
	}
}

// Timeout bounds how long a handler may run.
func Timeout(d time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, cancel := context.WithTimeout(r.Context(), d)
			defer cancel()
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
