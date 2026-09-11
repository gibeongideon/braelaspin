package httpx

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dibon/braelaspin/internal/auth"
	"github.com/dibon/braelaspin/internal/config"
	"github.com/dibon/braelaspin/internal/rds"
)

// Deps is everything the router needs. Passed explicitly rather than through
// package-level state so tests can build a router against fakes.
type Deps struct {
	Cfg    *config.Config
	Log    *slog.Logger
	DB     *pgxpool.Pool
	Redis  *rds.Client
	Tokens *auth.TokenService
}

// Registrar is implemented by each feature package to mount its own routes.
// It keeps this file from turning into a list of every endpoint in the system.
type Registrar interface {
	Routes(r chi.Router)
}

// NewRouter assembles the middleware stack and mounts the health endpoints.
// Feature routes are added by the caller via Mount.
func NewRouter(d Deps) *chi.Mux {
	r := chi.NewRouter()

	r.Use(WithRequestID(d.Log))
	r.Use(WithRealIP(d.Cfg.TrustedProxyCIDRs))
	r.Use(Recover)
	r.Use(LogRequests)
	// The web front end is deployed as its own origin, so the API must opt it
	// in explicitly. Sits after Recover/Log so preflights are still logged.
	r.Use(CORS(d.Cfg.CORSOrigins))

	// Liveness. Zero dependencies, on purpose: this must not fail because
	// Postgres hiccuped, or a supervisor will restart a perfectly healthy
	// process in the middle of a database blip.
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		JSON(w, http.StatusOK, map[string]string{"status": "ok", "version": Version})
	})

	// Readiness. This one DOES check dependencies — it is what the load
	// balancer uses to decide whether to send traffic here.
	r.Get("/readyz", func(w http.ResponseWriter, req *http.Request) {
		out := map[string]string{"postgres": "ok", "redis": "ok"}
		code := http.StatusOK

		pgCtx, cancel := context.WithTimeout(req.Context(), 2*time.Second)
		defer cancel()
		if err := d.DB.Ping(pgCtx); err != nil {
			out["postgres"] = "unavailable"
			code = http.StatusServiceUnavailable
		}

		rCtx, cancel2 := context.WithTimeout(req.Context(), time.Second)
		defer cancel2()
		if err := d.Redis.Ping(rCtx); err != nil {
			out["redis"] = "unavailable"
			code = http.StatusServiceUnavailable
		}
		JSON(w, code, out)
	})

	return r
}

// Version is stamped at build time with -ldflags "-X ...httpx.Version=<sha>".
var Version = "dev"
