package wallet_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/dibon/braelaspin/internal/testutil"
	"github.com/dibon/braelaspin/internal/wallet"
)

// inTx runs fn in a transaction, committing on success.
func inTx(t *testing.T, pool interface {
	Begin(context.Context) (pgx.Tx, error)
}, fn func(pgx.Tx) error) error {
	t.Helper()
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func TestMoveCreditsAndDebits(t *testing.T) {
	pool := testutil.DB(t)
	u := testutil.NewUser(t, pool, "254712000001", testutil.WithReal(10_000))

	err := inTx(t, pool, func(tx pgx.Tx) error {
		if _, err := wallet.Lock(context.Background(), tx, u); err != nil {
			return err
		}
		after, err := wallet.Move(context.Background(), tx, wallet.MoveParams{
			UserID: u, Kind: wallet.KindBet, IsReal: true, Delta: -2_500,
		})
		if err != nil {
			return err
		}
		if after != 7_500 {
			t.Errorf("balance after debit = %d, want 7500", after)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("move: %v", err)
	}

	if got := testutil.Balances(t, pool, u).RealCents; got != 7_500 {
		t.Errorf("real_cents = %d, want 7500", got)
	}
}

func TestMoveRefusesToOverdraw(t *testing.T) {
	pool := testutil.DB(t)
	u := testutil.NewUser(t, pool, "254712000002", testutil.WithReal(1_000))

	err := inTx(t, pool, func(tx pgx.Tx) error {
		if _, err := wallet.Lock(context.Background(), tx, u); err != nil {
			return err
		}
		_, err := wallet.Move(context.Background(), tx, wallet.MoveParams{
			UserID: u, Kind: wallet.KindBet, IsReal: true, Delta: -1_001,
		})
		return err
	})
	if !errors.Is(err, wallet.ErrInsufficientFunds) {
		t.Fatalf("overdraw error = %v, want ErrInsufficientFunds", err)
	}
	// The whole transaction must roll back: no partial debit, no orphan ledger row.
	if got := testutil.Balances(t, pool, u).RealCents; got != 1_000 {
		t.Errorf("real_cents = %d after a refused overdraw, want 1000 untouched", got)
	}
}

func TestMoveRejectsZeroDelta(t *testing.T) {
	pool := testutil.DB(t)
	u := testutil.NewUser(t, pool, "254712000003", testutil.WithReal(1_000))
	err := inTx(t, pool, func(tx pgx.Tx) error {
		_, err := wallet.Move(context.Background(), tx, wallet.MoveParams{
			UserID: u, Kind: wallet.KindBet, IsReal: true, Delta: 0,
		})
		return err
	})
	if err == nil {
		t.Fatal("a zero-delta ledger row was accepted; it moves nothing and is always a bug")
	}
}

// THE regression test for the reference implementation's lost-update bug.
//
// Its balance updates read the row without a lock, computed a new absolute
// value in Python, and wrote the whole row back. Under concurrency that loses
// updates: N goroutines all read the same balance and all believe they can
// afford the same money.
//
// Here every read is FOR UPDATE and every write is relative, so exactly one
// debit can succeed.
func TestConcurrentDebitsAllowExactlyOne(t *testing.T) {
	pool := testutil.DB(t)
	const stake = 1_000
	u := testutil.NewUser(t, pool, "254712000004", testutil.WithReal(stake)) // affords exactly one

	const goroutines = 200
	var succeeded, insufficient, other int64
	var wg sync.WaitGroup
	start := make(chan struct{})

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start // release them all at once, to maximise contention
			err := inTx(t, pool, func(tx pgx.Tx) error {
				if _, err := wallet.Lock(context.Background(), tx, u); err != nil {
					return err
				}
				_, err := wallet.Move(context.Background(), tx, wallet.MoveParams{
					UserID: u, Kind: wallet.KindBet, IsReal: true, Delta: -stake,
				})
				return err
			})
			switch {
			case err == nil:
				atomic.AddInt64(&succeeded, 1)
			case errors.Is(err, wallet.ErrInsufficientFunds):
				atomic.AddInt64(&insufficient, 1)
			default:
				atomic.AddInt64(&other, 1)
				t.Errorf("unexpected error: %v", err)
			}
		}()
	}
	close(start)
	wg.Wait()

	if succeeded != 1 {
		t.Errorf("%d debits succeeded, want exactly 1 — the balance afforded one", succeeded)
	}
	if insufficient != goroutines-1 {
		t.Errorf("%d rejected as insufficient, want %d", insufficient, goroutines-1)
	}
	if other != 0 {
		t.Errorf("%d failed unexpectedly", other)
	}
	if got := testutil.Balances(t, pool, u).RealCents; got != 0 {
		t.Errorf("final balance = %d, want 0", got)
	}
	t.Logf("%d goroutines: 1 succeeded, %d correctly rejected", goroutines, insufficient)
}

// The house row is the other contended hot spot: every real spin touches it.
func TestConcurrentHouseMovesNeverGoNegative(t *testing.T) {
	pool := testutil.DB(t)
	testutil.FundHouse(t, pool, 10_000)

	const goroutines = 100
	const payout = 1_000 // 10 of these fit
	var succeeded, exhausted int64
	var wg sync.WaitGroup
	start := make(chan struct{})

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			err := inTx(t, pool, func(tx pgx.Tx) error {
				if _, err := wallet.HouseLock(context.Background(), tx); err != nil {
					return err
				}
				_, err := wallet.MoveHouse(context.Background(), tx, -payout, 0)
				return err
			})
			switch {
			case err == nil:
				atomic.AddInt64(&succeeded, 1)
			case errors.Is(err, wallet.ErrBankrollExhausted):
				atomic.AddInt64(&exhausted, 1)
			default:
				t.Errorf("unexpected error: %v", err)
			}
		}()
	}
	close(start)
	wg.Wait()

	if succeeded != 10 {
		t.Errorf("%d house debits succeeded, want exactly 10", succeeded)
	}
	if got := testutil.House(t, pool).BankrollCents; got != 0 {
		t.Errorf("bankroll = %d, want 0 (never negative, never silently skipped)", got)
	}
	t.Logf("%d goroutines against a 10-payout bankroll: %d succeeded, %d exhausted",
		goroutines, succeeded, exhausted)
}

// The ledger must be append-only even against a direct statement.
func TestLedgerIsAppendOnly(t *testing.T) {
	pool := testutil.DB(t)
	u := testutil.NewUser(t, pool, "254712000005", testutil.WithReal(5_000))
	ctx := context.Background()

	var before int64
	if err := pool.QueryRow(ctx,
		`SELECT amount_cents FROM transactions WHERE user_id = $1`, u).Scan(&before); err != nil {
		t.Fatal(err)
	}

	// Both of these are silent no-ops thanks to the DO INSTEAD NOTHING rules.
	if _, err := pool.Exec(ctx,
		`UPDATE transactions SET amount_cents = 999999 WHERE user_id = $1`, u); err != nil {
		t.Fatalf("UPDATE should be a silent no-op, got error: %v", err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM transactions WHERE user_id = $1`, u); err != nil {
		t.Fatalf("DELETE should be a silent no-op, got error: %v", err)
	}

	var after int64
	var count int
	if err := pool.QueryRow(ctx,
		`SELECT amount_cents, COUNT(*) OVER () FROM transactions WHERE user_id = $1`, u).
		Scan(&after, &count); err != nil {
		t.Fatalf("the row was deleted despite the append-only rule: %v", err)
	}
	if after != before {
		t.Errorf("ledger row was rewritten: %d -> %d", before, after)
	}
}

func TestHoldMovesMoneyOutOfReachThenReverses(t *testing.T) {
	pool := testutil.DB(t)
	u := testutil.NewUser(t, pool, "254712000006", testutil.WithReal(10_000))
	ctx := context.Background()
	wdID := int64(1)

	// Hold: the money leaves the spendable balance immediately, so it cannot
	// be gambled while a payout is in flight.
	if err := inTx(t, pool, func(tx pgx.Tx) error {
		if _, err := wallet.Lock(ctx, tx, u); err != nil {
			return err
		}
		_, err := wallet.Hold(ctx, tx, u, 4_000, &wdID)
		return err
	}); err != nil {
		t.Fatalf("hold: %v", err)
	}

	b := testutil.Balances(t, pool, u)
	if b.RealCents != 6_000 || b.HeldCents != 4_000 {
		t.Fatalf("after hold: real=%d held=%d, want 6000/4000", b.RealCents, b.HeldCents)
	}

	// A hold larger than what remains must be refused.
	err := inTx(t, pool, func(tx pgx.Tx) error {
		if _, err := wallet.Lock(ctx, tx, u); err != nil {
			return err
		}
		_, err := wallet.Hold(ctx, tx, u, 6_001, &wdID)
		return err
	})
	if !errors.Is(err, wallet.ErrInsufficientFunds) {
		t.Errorf("over-hold error = %v, want ErrInsufficientFunds", err)
	}

	// Reverse: the payout failed, so the money comes back and the hold clears.
	// The reference implementation had no equivalent of this path at all — a
	// failed B2C left the user debited permanently.
	if err := inTx(t, pool, func(tx pgx.Tx) error {
		if _, err := wallet.Lock(ctx, tx, u); err != nil {
			return err
		}
		_, err := wallet.ReverseHold(ctx, tx, u, 4_000, &wdID, "b2c failed")
		return err
	}); err != nil {
		t.Fatalf("reverse: %v", err)
	}

	b = testutil.Balances(t, pool, u)
	if b.RealCents != 10_000 || b.HeldCents != 0 {
		t.Errorf("after reversal: real=%d held=%d, want 10000/0 (player made whole)",
			b.RealCents, b.HeldCents)
	}
}

func TestSettledHoldDoesNotReturnMoney(t *testing.T) {
	pool := testutil.DB(t)
	u := testutil.NewUser(t, pool, "254712000007", testutil.WithReal(10_000))
	ctx := context.Background()
	wdID := int64(1)

	if err := inTx(t, pool, func(tx pgx.Tx) error {
		if _, err := wallet.Lock(ctx, tx, u); err != nil {
			return err
		}
		_, err := wallet.Hold(ctx, tx, u, 4_000, &wdID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	// Payout succeeded: release the hold WITHOUT crediting the player.
	if err := inTx(t, pool, func(tx pgx.Tx) error {
		return wallet.ReleaseHold(ctx, tx, u, 4_000)
	}); err != nil {
		t.Fatal(err)
	}

	b := testutil.Balances(t, pool, u)
	if b.RealCents != 6_000 || b.HeldCents != 0 {
		t.Errorf("after settlement: real=%d held=%d, want 6000/0", b.RealCents, b.HeldCents)
	}
}

// Locking two wallets must be deadlock-free regardless of the order callers
// name them, because Lock sorts internally.
func TestConcurrentTwoWalletLocksDoNotDeadlock(t *testing.T) {
	pool := testutil.DB(t)
	a := testutil.NewUser(t, pool, "254712000008", testutil.WithReal(100_000))
	b := testutil.NewUser(t, pool, "254712000009", testutil.WithReal(100_000))
	ctx := context.Background()

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		for _, pair := range [][2]int64{{a, b}, {b, a}} { // deliberately opposite orders
			wg.Add(1)
			go func(x, y int64) {
				defer wg.Done()
				if err := inTx(t, pool, func(tx pgx.Tx) error {
					if _, err := wallet.Lock(ctx, tx, x, y); err != nil {
						return err
					}
					if _, err := wallet.Move(ctx, tx, wallet.MoveParams{
						UserID: x, Kind: wallet.KindBet, IsReal: true, Delta: -10,
					}); err != nil {
						return err
					}
					_, err := wallet.Move(ctx, tx, wallet.MoveParams{
						UserID: y, Kind: wallet.KindReferral, IsReal: true, Delta: 10,
					})
					return err
				}); err != nil {
					t.Errorf("two-wallet move failed (deadlock?): %v", err)
				}
			}(pair[0], pair[1])
		}
	}
	wg.Wait()

	// Money is only ever moved between the two, so the total is conserved.
	total := testutil.Balances(t, pool, a).RealCents + testutil.Balances(t, pool, b).RealCents
	if total != 200_000 {
		t.Errorf("total across both wallets = %d, want 200000 conserved", total)
	}
}

func TestDemoAndRealAreSeparateMoney(t *testing.T) {
	pool := testutil.DB(t)
	u := testutil.NewUser(t, pool, "254712000010",
		testutil.WithReal(1_000), testutil.WithDemo(500_000))
	ctx := context.Background()

	// Spending demo must not touch real, and vice versa.
	if err := inTx(t, pool, func(tx pgx.Tx) error {
		if _, err := wallet.Lock(ctx, tx, u); err != nil {
			return err
		}
		_, err := wallet.Move(ctx, tx, wallet.MoveParams{
			UserID: u, Kind: wallet.KindBet, IsReal: false, Delta: -100_000,
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}

	b := testutil.Balances(t, pool, u)
	if b.RealCents != 1_000 {
		t.Errorf("real_cents = %d after a demo bet, want 1000 untouched", b.RealCents)
	}
	if b.DemoCents != 400_000 {
		t.Errorf("demo_cents = %d, want 400000", b.DemoCents)
	}
}
