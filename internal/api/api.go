// Package api is the HTTP layer: request decoding, error mapping, and route
// mounting. It holds no business logic.
//
// Handlers live here rather than inside each service package so that the
// services stay pure and free of net/http, and so that aggregate endpoints
// like GET /me can read from users, wallet and game without those packages
// having to know about one another.
package api

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dibon/braelaspin/internal/auth"
	"github.com/dibon/braelaspin/internal/config"
	"github.com/dibon/braelaspin/internal/game"
	"github.com/dibon/braelaspin/internal/httpx"
	"github.com/dibon/braelaspin/internal/rds"
	"github.com/dibon/braelaspin/internal/users"
)

type Server struct {
	cfg    *config.Config
	log    *slog.Logger
	db     *pgxpool.Pool
	redis  *rds.Client
	tokens *auth.TokenService
	users  *users.Service
	game   *game.Service
	argon  auth.Argon2Params

	// dummyHash is verified against when a login names an unknown phone, so
	// that "no such user" and "wrong password" cost the same wall-clock time.
	// Without it, response latency is an oracle for which phone numbers are
	// registered.
	dummyHash string
}

type Deps struct {
	Cfg    *config.Config
	Log    *slog.Logger
	DB     *pgxpool.Pool
	Redis  *rds.Client
	Tokens *auth.TokenService
	Users  *users.Service
	Game   *game.Service
}

func New(d Deps) (*Server, error) {
	argon := auth.DefaultArgon2Params(d.Cfg.ArgonTime, d.Cfg.ArgonMemoryKiB, d.Cfg.ArgonThreads)

	// Hashed once at startup; the plaintext is irrelevant and never stored.
	dummy, err := auth.HashPassword("timing-equalisation-placeholder", argon)
	if err != nil {
		return nil, err
	}

	return &Server{
		cfg: d.Cfg, log: d.Log, db: d.DB, redis: d.Redis,
		tokens: d.Tokens, users: d.Users, game: d.Game,
		argon: argon, dummyHash: dummy,
	}, nil
}

// handler lets handlers return an error and keeps the Fail plumbing in one
// place instead of at every early return.
type handler func(http.ResponseWriter, *http.Request) error

func (s *Server) h(fn handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := fn(w, r); err != nil {
			httpx.Fail(w, r, err)
		}
	}
}

// Routes mounts the v1 API.
//
// Limits come from config so they can be moved per environment without a
// rebuild. The unauthenticated ones are keyed by IP; reads are keyed by user.
func (s *Server) Routes(r chi.Router) {
	var (
		limitRegister = httpx.RateLimitSpec{Action: "register", Limit: s.cfg.RLRegisterPerHour, Window: time.Hour}
		limitLogin    = httpx.RateLimitSpec{Action: "login", Limit: s.cfg.RLLoginPer5Min, Window: 5 * time.Minute}
		limitRefresh  = httpx.RateLimitSpec{Action: "refresh", Limit: s.cfg.RLRefreshPerMin, Window: time.Minute}
		limitRead     = httpx.RateLimitSpec{Action: "read", Limit: s.cfg.RLReadPerMin, Window: time.Minute, ByUser: true}
	)

	r.Route("/v1", func(r chi.Router) {
		r.Use(httpx.Timeout(10 * time.Second))

		// ── public ──────────────────────────────────────────────────────────
		r.Group(func(r chi.Router) {
			r.With(httpx.RateLimit(s.redis, limitRegister)).Post("/auth/register", s.h(s.register))
			r.With(httpx.RateLimit(s.redis, limitLogin)).Post("/auth/login", s.h(s.login))
			r.With(httpx.RateLimit(s.redis, limitRefresh)).Post("/auth/refresh", s.h(s.refresh))
			r.Post("/auth/logout", s.h(s.logout))
		})

		// ── authenticated ───────────────────────────────────────────────────
		r.Group(func(r chi.Router) {
			r.Use(httpx.RequireAuth(s.tokens))
			r.Use(httpx.RateLimit(s.redis, limitRead))

			r.Get("/me", s.h(s.me))
			r.Get("/wallet", s.h(s.walletBalances))
			r.Post("/auth/logout-all", s.h(s.logoutAll))
			r.Post("/wallet/demo/topup", s.h(s.demoTopUp))

			r.Get("/game/config", s.h(s.gameConfig))
			r.Get("/game/winners", s.h(s.winners))
			r.Get("/game/spins", s.h(s.spins))
			r.Get("/game/spins/by-ref/{ref}", s.h(s.spinByRef))
			r.Get("/history", s.h(s.history))

			// Spin carries its own tighter limit on top of the read limit.
			r.With(httpx.RateLimit(s.redis, httpx.RateLimitSpec{
				Action: "spin", Limit: s.cfg.RLSpinPerMin, Window: time.Minute, ByUser: true,
			})).Post("/game/spin", s.h(s.spin))
		})
	})
}
