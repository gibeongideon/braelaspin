// Command braelaspin is the whole backend: API server, migrator, and health
// probe, in one static binary.
//
// Subcommands:
//
//	serve        run the API server (default)
//	migrate      up | down | status
//	healthcheck  probe /healthz over localhost (for container HEALTHCHECK,
//	             since a distroless image has no curl)
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	// Embeds the IANA timezone database in the binary.
	//
	// This is not optional. The M-Pesa STK password is
	// base64(shortcode+passkey+timestamp) with the timestamp in Africa/Nairobi,
	// so a distroless image without tzdata would silently produce an invalid
	// password for every deposit. The reference implementation used naive
	// server-local time and broke the same way off EAT.
	_ "time/tzdata"

	"github.com/dibon/braelaspin/internal/api"
	"github.com/dibon/braelaspin/internal/auth"
	"github.com/dibon/braelaspin/internal/config"
	"github.com/dibon/braelaspin/internal/db"
	"github.com/dibon/braelaspin/internal/game"
	"github.com/dibon/braelaspin/internal/httpx"
	"github.com/dibon/braelaspin/internal/rds"
	"github.com/dibon/braelaspin/internal/users"
)

// version is stamped at build time: -ldflags "-X main.version=$(git rev-parse --short HEAD)"
var version = "dev"

func main() {
	cmd := "serve"
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}

	if err := run(cmd, os.Args[2:]); err != nil {
		fmt.Fprintf(os.Stderr, "braelaspin: %v\n", err)
		os.Exit(1)
	}
}

func run(cmd string, args []string) error {
	switch cmd {
	case "healthcheck":
		return healthcheck()
	case "version":
		fmt.Println(version)
		return nil
	case "serve", "migrate":
		// these need config; fall through
	default:
		return fmt.Errorf("unknown command %q (want: serve | migrate | healthcheck | version)", cmd)
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	httpx.Version = version
	log := newLogger(cfg)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if cmd == "migrate" {
		return migrate(ctx, cfg, args)
	}
	return serve(ctx, cfg, log)
}

func migrate(ctx context.Context, cfg *config.Config, args []string) error {
	sub := "up"
	if len(args) > 0 {
		sub = args[0]
	}
	switch sub {
	case "up":
		return db.MigrateUp(ctx, cfg.DatabaseURL)
	case "down":
		return db.MigrateDown(ctx, cfg.DatabaseURL)
	case "status":
		return db.Status(ctx, cfg.DatabaseURL)
	default:
		return fmt.Errorf("unknown migrate subcommand %q (want: up | down | status)", sub)
	}
}

func serve(ctx context.Context, cfg *config.Config, log *slog.Logger) error {
	// Validate the wheel before anything else can use it. A table whose odds
	// disagree with the configured RTP must never reach a player, so this
	// fails the process in the first milliseconds rather than quietly
	// changing the house edge.
	if err := game.Validate(cfg.RTPBP); err != nil {
		return err
	}
	log.Info("wheel validated",
		"segments", len(game.Wheel), "rtp_bp", game.RTPBP(), "max_multiplier_bp", game.MaxMultBP())

	pool, err := db.Open(ctx, cfg.DatabaseURL, cfg.DBMaxConns, cfg.DBMinConns)
	if err != nil {
		return err
	}
	defer pool.Close()

	if cfg.MigrateOnBoot {
		log.Warn("MIGRATE_ON_BOOT is set; applying migrations (development only)")
		if err := db.MigrateUp(ctx, cfg.DatabaseURL); err != nil {
			return err
		}
	}
	// Running new code against an old schema corrupts data silently; refusing
	// to start is the loud alternative.
	if err := db.AssertCurrent(ctx, cfg.DatabaseURL); err != nil {
		return err
	}

	redis, err := rds.Open(ctx, cfg.RedisURL, cfg.RedisPoolSize)
	if err != nil {
		return err
	}
	defer redis.Close()

	tokens := auth.NewTokenService(cfg.JWTSecret, cfg.JWTIssuer, cfg.AccessTokenTTL, cfg.RefreshTTL, redis)
	userSvc := users.NewService(pool, cfg.DemoGrantCents)
	gameSvc := game.NewService(pool, game.Economics{
		RTPBP:         cfg.RTPBP,
		RakeBP:        cfg.RakeBP,
		ReferralBP:    cfg.ReferralBP,
		MinStakeCents: cfg.MinStakeCents,
		MaxStakeCents: cfg.MaxStakeCents,
	})

	router := httpx.NewRouter(httpx.Deps{
		Cfg: cfg, Log: log, DB: pool, Redis: redis, Tokens: tokens,
	})

	apiSrv, err := api.New(api.Deps{
		Cfg: cfg, Log: log, DB: pool, Redis: redis,
		Tokens: tokens, Users: userSvc, Game: gameSvc,
	})
	if err != nil {
		return err
	}
	apiSrv.Routes(router)

	srv := &http.Server{
		Addr:    cfg.HTTPAddr,
		Handler: router,
		// Slowloris and runaway-handler protection. Every one of these is a
		// timeout the Go default leaves unbounded.
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    16 << 10,
		ErrorLog:          slog.NewLogLogger(log.Handler(), slog.LevelError),
		BaseContext:       func(net.Listener) context.Context { return ctx },
	}

	errCh := make(chan error, 1)
	go func() {
		log.Info("listening",
			"addr", cfg.HTTPAddr, "env", cfg.Env, "version", version)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return fmt.Errorf("http server: %w", err)
	case <-ctx.Done():
		log.Info("shutdown signal received, draining")
	}

	// Stop accepting, let in-flight requests finish. A spin holds row locks for
	// well under a millisecond, so 15s is generous.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("graceful shutdown: %w", err)
	}
	log.Info("stopped cleanly")
	return nil
}

// healthcheck exists so the distroless image needs no curl.
func healthcheck() error {
	addr := os.Getenv("HTTP_ADDR")
	if addr == "" {
		addr = ":8080"
	}
	if strings.HasPrefix(addr, ":") {
		addr = "127.0.0.1" + addr
	}
	c := &http.Client{Timeout: 3 * time.Second}
	resp, err := c.Get("http://" + addr + "/healthz")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("healthz returned %d", resp.StatusCode)
	}
	return nil
}

func newLogger(cfg *config.Config) *slog.Logger {
	level := slog.LevelInfo
	switch cfg.LogLevel {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	}

	opts := &slog.HandlerOptions{Level: level, ReplaceAttr: redact}
	var h slog.Handler
	if cfg.LogFormat == "text" {
		h = slog.NewTextHandler(os.Stdout, opts)
	} else {
		h = slog.NewJSONHandler(os.Stdout, opts)
	}
	l := slog.New(h)
	slog.SetDefault(l)
	return l
}

// redactedKeys never reach a log sink in clear text.
//
// Phones are the account identifier, so a leaked phone is a leaked user list;
// they are logged only via auth.Mask. M-Pesa receipts are deliberately NOT
// here: they are the reconciliation key and are not secret.
var redactedKeys = map[string]bool{
	"password": true, "password_hash": true, "authorization": true,
	"refresh": true, "refresh_token": true, "access": true, "access_token": true,
	"jwt_secret": true, "secret": true, "passkey": true,
	"securitycredential": true, "security_credential": true,
	"initiator_password": true, "consumer_secret": true,
	"pin": true, "token": true,
}

func redact(_ []string, a slog.Attr) slog.Attr {
	if redactedKeys[strings.ToLower(a.Key)] {
		return slog.String(a.Key, "[redacted]")
	}
	// A raw phone number that slipped through gets masked rather than dropped,
	// so the log is still useful for correlation.
	if strings.ToLower(a.Key) == "phone" {
		if s, ok := a.Value.Any().(string); ok {
			return slog.String(a.Key, auth.Mask(s))
		}
	}
	return a
}
