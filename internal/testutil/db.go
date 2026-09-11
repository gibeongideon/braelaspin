// Package testutil provides a real Postgres for integration tests.
//
// These tests run against an actual database on purpose. The bugs this project
// exists to avoid — lost updates, double-spends, skipped debits — are all
// concurrency bugs in SQL, and none of them reproduce against a mock.
package testutil

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dibon/braelaspin/internal/db"
	"github.com/dibon/braelaspin/internal/wallet"
)

var (
	setupOnce sync.Once
	pkgURL    string
	setupErr  error
)

// packageDBName derives a database name from the test binary, e.g.
// ".../internal/api.test" -> "braelaspin_test_api".
//
// This matters because `go test ./...` runs each package's tests in a SEPARATE
// PROCESS, IN PARALLEL. Sharing one database means one package's TRUNCATE wipes
// another package's fixtures mid-test, producing deadlocks and phantom
// "no wallet for user N" failures that look like production bugs but are not.
// A database per package removes the shared mutable state entirely, and keeps
// `go test ./...` correct however it is invoked rather than relying on -p 1.
func packageDBName() string {
	base := strings.TrimSuffix(filepath.Base(os.Args[0]), ".test")
	var b strings.Builder
	for _, r := range strings.ToLower(base) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' {
			b.WriteRune(r)
		}
	}
	name := b.String()
	if name == "" {
		name = "default"
	}
	return "braelaspin_test_" + name
}

// DB returns a migrated, empty database, or skips the test if none is
// configured. Set TEST_DATABASE_URL (see `make test-integration`).
//
// Tests within a package must NOT call t.Parallel(): they share this database
// and each one truncates it on entry.
func DB(t *testing.T) *pgxpool.Pool {
	t.Helper()

	base := os.Getenv("TEST_DATABASE_URL")
	if base == "" {
		t.Skip("TEST_DATABASE_URL not set; run `make test-integration`")
	}

	ctx := context.Background()
	setupOnce.Do(func() { pkgURL, setupErr = provision(ctx, base, packageDBName()) })
	if setupErr != nil {
		t.Fatalf("provision test database: %v", setupErr)
	}

	pool, err := db.Open(ctx, pkgURL, 30, 2)
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}

	truncate(t, pool)
	t.Cleanup(func() {
		// Every integration test ends by proving the money is still correct.
		// This one line is what turns "we think the ledger is right" into
		// "the ledger is provably right after every test we have ever run".
		AssertMoneyIsCorrect(t, pool)
		pool.Close()
	})
	return pool
}

// provision creates this package's database if absent and migrates it,
// returning its URL.
func provision(ctx context.Context, baseURL, name string) (string, error) {
	admin, err := pgx.Connect(ctx, baseURL)
	if err != nil {
		return "", fmt.Errorf("connect to %s: %w", baseURL, err)
	}
	// CREATE DATABASE cannot run inside a transaction and Postgres has no
	// IF NOT EXISTS for it, so tolerate 42P04 (duplicate_database) from a
	// concurrent package doing the same thing.
	_, err = admin.Exec(ctx, `CREATE DATABASE "`+name+`"`)
	if err != nil {
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "42P04" {
			_ = admin.Close(ctx)
			return "", fmt.Errorf("create database %s: %w", name, err)
		}
	}
	_ = admin.Close(ctx)

	url, err := withDBName(baseURL, name)
	if err != nil {
		return "", err
	}
	if err := db.MigrateUp(ctx, url); err != nil {
		return "", fmt.Errorf("migrate %s: %w", name, err)
	}
	return url, nil
}

func withDBName(raw, name string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("parse TEST_DATABASE_URL: %w", err)
	}
	u.Path = "/" + name
	return u.String(), nil
}

// Reset empties every table and zeroes the house. Use it between sub-tests
// that each need a clean ledger; DB() already does it once per test.
func Reset(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	truncate(t, pool)
}

func truncate(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	// transactions has DO INSTEAD NOTHING rules on UPDATE/DELETE, but TRUNCATE
	// bypasses rules, which is exactly why it is confined to test setup.
	_, err := pool.Exec(ctx, `
		TRUNCATE admin_audit, mpesa_callbacks, withdrawals, deposits,
		         spins, transactions, wallets, users RESTART IDENTITY CASCADE`)
	if err != nil {
		t.Fatalf("truncate: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`UPDATE house SET bankroll_cents = 0, rake_cents = 0 WHERE id = 1`); err != nil {
		t.Fatalf("reset house: %v", err)
	}
}

// RedisURL returns a Redis URL on a database index reserved for this test
// package, for the same reason DB() gives each package its own Postgres:
// packages run in parallel, and a FlushDB in one would wipe another's tokens
// and rate-limit counters mid-test.
//
// Redis ships with 16 databases; index 0 is left for development.
func RedisURL(t *testing.T) string {
	t.Helper()
	base := os.Getenv("TEST_REDIS_URL")
	if base == "" {
		base = "redis://localhost:6380"
	}
	u, err := url.Parse(base)
	if err != nil {
		t.Fatalf("parse TEST_REDIS_URL: %v", err)
	}

	var h uint32 = 2166136261 // FNV-1a
	for _, b := range []byte(packageDBName()) {
		h = (h ^ uint32(b)) * 16777619
	}
	u.Path = "/" + fmt.Sprint(1+h%15) // 1..15
	return u.String()
}

// AssertMoneyIsCorrect fails the test if any integrity invariant is violated.
func AssertMoneyIsCorrect(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := wallet.AssertInvariant(ctx, pool); err != nil {
		t.Errorf("MONEY INTEGRITY VIOLATED:\n%v", err)
	}
}

// NewUser inserts a user with a wallet and returns its id.
func NewUser(t *testing.T, pool *pgxpool.Pool, phone string, opts ...UserOpt) int64 {
	t.Helper()
	ctx := context.Background()

	// 8 chars, inside the users.ref_code CHECK of ^[A-Z0-9]{6,10}$.
	u := userSpec{refCode: fmt.Sprintf("TST%05d", atomic.AddInt64(&refCodeSeq, 1)%100000)}
	for _, o := range opts {
		o(&u)
	}

	var id int64
	err := pool.QueryRow(ctx, `
		INSERT INTO users (phone, password_hash, ref_code, referred_by, is_admin)
		VALUES ($1, 'x', $2, $3, $4) RETURNING id`,
		phone, u.refCode, u.referredBy, u.isAdmin).Scan(&id)
	if err != nil {
		t.Fatalf("create user %s: %v", phone, err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO wallets (user_id, real_cents, demo_cents) VALUES ($1, $2, $3)`,
		id, u.realCents, u.demoCents); err != nil {
		t.Fatalf("create wallet for %s: %v", phone, err)
	}

	// Funding must go through the ledger, or invariant I1 fails immediately —
	// which is the point: there is no back door for creating money, not even
	// in tests.
	if u.realCents > 0 {
		ledgerSeed(t, pool, id, "deposit", true, u.realCents)
	}
	if u.demoCents > 0 {
		ledgerSeed(t, pool, id, "demo_grant", false, u.demoCents)
	}
	return id
}

func ledgerSeed(t *testing.T, pool *pgxpool.Pool, userID int64, kind string, isReal bool, amount int64) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO transactions (user_id, kind, is_real, amount_cents, balance_after_cents, memo)
		VALUES ($1, $2, $3, $4, $4, 'test seed')`, userID, kind, isReal, amount); err != nil {
		t.Fatalf("seed ledger for user %d: %v", userID, err)
	}
}

// refCodeSeq keeps generated referral codes unique within a test run.
var refCodeSeq int64

type userSpec struct {
	refCode    string
	referredBy *int64
	realCents  int64
	demoCents  int64
	isAdmin    bool
}

type UserOpt func(*userSpec)

func WithReal(cents int64) UserOpt    { return func(u *userSpec) { u.realCents = cents } }
func WithDemo(cents int64) UserOpt    { return func(u *userSpec) { u.demoCents = cents } }
func WithRefCode(code string) UserOpt { return func(u *userSpec) { u.refCode = code } }
func WithAdmin() UserOpt              { return func(u *userSpec) { u.isAdmin = true } }
func ReferredBy(id int64) UserOpt     { return func(u *userSpec) { u.referredBy = &id } }

// FundHouse sets the bankroll directly. Real play needs a bankroll to pass
// admission control.
func FundHouse(t *testing.T, pool *pgxpool.Pool, cents int64) {
	t.Helper()
	if _, err := pool.Exec(context.Background(),
		`UPDATE house SET bankroll_cents = $1 WHERE id = 1`, cents); err != nil {
		t.Fatalf("fund house: %v", err)
	}
}

// Balances reads a wallet.
func Balances(t *testing.T, pool *pgxpool.Pool, userID int64) wallet.Balances {
	t.Helper()
	b, err := wallet.Get(context.Background(), pool, userID)
	if err != nil {
		t.Fatalf("read wallet %d: %v", userID, err)
	}
	return b
}

// House reads the house row.
func House(t *testing.T, pool *pgxpool.Pool) wallet.House {
	t.Helper()
	h, err := wallet.GetHouse(context.Background(), pool)
	if err != nil {
		t.Fatalf("read house: %v", err)
	}
	return h
}
