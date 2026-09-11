// Package testutil provides a real Postgres for integration tests.
//
// These tests run against an actual database on purpose. The bugs this project
// exists to avoid — lost updates, double-spends, skipped debits — are all
// concurrency bugs in SQL, and none of them reproduce against a mock.
package testutil

import (
	"context"
	"fmt"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dibon/braelaspin/internal/db"
	"github.com/dibon/braelaspin/internal/wallet"
)

// DB returns a migrated, empty database, or skips the test if none is
// configured. Set TEST_DATABASE_URL (see `make test-integration`).
func DB(t *testing.T) *pgxpool.Pool {
	t.Helper()

	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set; run `make test-integration`")
	}

	ctx := context.Background()
	if err := db.MigrateUp(ctx, url); err != nil {
		t.Fatalf("migrate test database: %v", err)
	}

	pool, err := db.Open(ctx, url, 30, 2)
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
