// Package db owns the Postgres pool and the embedded migrations.
//
// Migrations are compiled into the binary, so there is no second image, no
// migrate CLI on the host, and no version skew between code and schema.
package db

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Open returns a ready connection pool, having verified it can actually talk
// to the database. A pool that has never round-tripped is not proof of much.
func Open(ctx context.Context, url string, maxConns, minConns int32) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("parse DATABASE_URL: %w", err)
	}
	cfg.MaxConns = maxConns
	cfg.MinConns = minConns
	cfg.MaxConnLifetime = 30 * time.Minute
	cfg.MaxConnIdleTime = 5 * time.Minute
	cfg.HealthCheckPeriod = 30 * time.Second

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return pool, nil
}

// migrationProvider wires goose to the embedded SQL over a database/sql handle
// derived from the same pgx config, so there is one connection string.
func withGoose(ctx context.Context, url string, fn func(*goose.Provider) error) error {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return fmt.Errorf("parse DATABASE_URL: %w", err)
	}
	sqlDB := stdlib.OpenDB(*cfg.ConnConfig)
	defer sqlDB.Close()

	// goose wants the SQL at the root of the FS it is given.
	sub, err := fs.Sub(migrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("sub migrations FS: %w", err)
	}
	p, err := goose.NewProvider(goose.DialectPostgres, sqlDB, sub)
	if err != nil {
		return fmt.Errorf("goose provider: %w", err)
	}
	return fn(p)
}

// MigrateUp applies every pending migration.
func MigrateUp(ctx context.Context, url string) error {
	return withGoose(ctx, url, func(p *goose.Provider) error {
		results, err := p.Up(ctx)
		if err != nil {
			return fmt.Errorf("migrate up: %w", err)
		}
		for _, r := range results {
			fmt.Printf("applied %s in %s\n", r.Source.Path, r.Duration)
		}
		if len(results) == 0 {
			fmt.Println("no pending migrations")
		}
		return nil
	})
}

// MigrateDown rolls back exactly one migration.
func MigrateDown(ctx context.Context, url string) error {
	return withGoose(ctx, url, func(p *goose.Provider) error {
		r, err := p.Down(ctx)
		if err != nil {
			return fmt.Errorf("migrate down: %w", err)
		}
		fmt.Printf("rolled back %s\n", r.Source.Path)
		return nil
	})
}

// Status prints the applied/pending state of every migration.
func Status(ctx context.Context, url string) error {
	return withGoose(ctx, url, func(p *goose.Provider) error {
		st, err := p.Status(ctx)
		if err != nil {
			return fmt.Errorf("migrate status: %w", err)
		}
		for _, s := range st {
			state := "pending"
			if s.State == goose.StateApplied {
				state = "applied"
			}
			fmt.Printf("%-8s %s\n", state, s.Source.Path)
		}
		return nil
	})
}

// AssertCurrent refuses to let the server start against a schema older than
// the one this binary was built for. Running new code against an old schema is
// how you get silent data corruption instead of a loud startup failure.
func AssertCurrent(ctx context.Context, url string) error {
	return withGoose(ctx, url, func(p *goose.Provider) error {
		st, err := p.Status(ctx)
		if err != nil {
			return fmt.Errorf("check migration status: %w", err)
		}
		var pending []string
		for _, s := range st {
			if s.State != goose.StateApplied {
				pending = append(pending, s.Source.Path)
			}
		}
		if len(pending) > 0 {
			return fmt.Errorf("database schema is behind this binary: %d migration(s) pending (%v); "+
				"run `braelaspin migrate up`", len(pending), pending)
		}
		return nil
	})
}
