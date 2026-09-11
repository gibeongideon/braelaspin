// Package wallet is the only code in this program that moves money.
//
// Read the signature of Move before anything else. Note what it has no
// parameter for: a new balance. There is no argument, and no branch, that sets
// a balance to a computed absolute value — every change is expressed as a
// signed delta and applied by Postgres as a relative UPDATE under a row lock.
//
// That is deliberate. The reference implementation this replaces had a
// function taking the NEW BALANCE as its argument, guarding it with
// `if value > 0`, and writing it with a full-row save from a stale unlocked
// read. That single shape produced three distinct bugs: a large payout silently
// skipped its own debit, concurrent updates were lost, and sibling columns were
// clobbered. None of them are expressible through this API.
package wallet

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Kind enumerates the ledger entry kinds. These must match the CHECK
// constraint on transactions.kind.
type Kind string

const (
	KindDemoGrant        Kind = "demo_grant"
	KindBet              Kind = "bet"
	KindWin              Kind = "win"
	KindDeposit          Kind = "deposit"
	KindWithdraw         Kind = "withdraw"
	KindWithdrawReversed Kind = "withdraw_reversed"
	KindReferral         Kind = "referral"
	KindAdjustment       Kind = "adjustment"
)

// Balances is one row of the wallets table.
type Balances struct {
	UserID    int64
	RealCents int64
	DemoCents int64
	HeldCents int64
}

// Spendable returns the balance a spin may draw on for the given realm.
func (b Balances) Spendable(isReal bool) int64 {
	if isReal {
		return b.RealCents
	}
	return b.DemoCents
}

// Withdrawable is what the player may request a payout of. Funds already held
// against an open withdrawal are, by construction, no longer in RealCents.
func (b Balances) Withdrawable() int64 { return b.RealCents }

// House is the single-row house table.
type House struct {
	BankrollCents int64
	RakeCents     int64
}

// ErrInsufficientFunds is returned when a debit would drive a balance negative.
// It is distinguished from a generic failure because the API maps it to a
// specific client error code and the client turns it into a Deposit prompt.
var ErrInsufficientFunds = errors.New("wallet: insufficient funds")

// ErrBankrollExhausted means the house could not cover a movement. Reaching
// this indicates admission control upstream let through a stake it should have
// vetoed, so it is a bug, not an expected condition.
var ErrBankrollExhausted = errors.New("wallet: bankroll exhausted")

// MoveParams describes one balance change plus the ledger row that records it.
type MoveParams struct {
	UserID int64
	Kind   Kind
	IsReal bool
	// Delta is signed: negative debits, positive credits. Zero is rejected —
	// a ledger row that moves nothing is a bug, not a no-op.
	Delta int64
	RefID *int64
	Memo  string
}

// Move applies a signed delta to one wallet column and appends the matching
// ledger row, inside the caller's transaction.
//
// The caller MUST already hold the wallet row lock (see Lock). Move does not
// take the lock itself, because a caller that needs two wallets — a referral
// payout touches the referee's and the referrer's — has to lock both in a
// deterministic order before doing any work, and only the caller knows the set.
func Move(ctx context.Context, tx pgx.Tx, p MoveParams) (balanceAfter int64, err error) {
	if p.Delta == 0 {
		return 0, fmt.Errorf("wallet: %s for user %d has a zero delta", p.Kind, p.UserID)
	}
	if p.Kind == "" {
		return 0, fmt.Errorf("wallet: move for user %d has no kind", p.UserID)
	}

	column := "demo_cents"
	if p.IsReal {
		column = "real_cents"
	}

	// Relative, atomic, evaluated by Postgres under the row lock the caller
	// holds. A CHECK (>= 0) violation aborts the caller's whole transaction,
	// which is exactly right: the operation did not happen.
	q := fmt.Sprintf(
		`UPDATE wallets SET %s = %s + $2, updated_at = now() WHERE user_id = $1 RETURNING %s`,
		column, column, column)

	if err := tx.QueryRow(ctx, q, p.UserID, p.Delta).Scan(&balanceAfter); err != nil {
		if isCheckViolation(err, "wallets_"+column+"_check") || isAnyCheckViolation(err) {
			return 0, fmt.Errorf("%w: user %d %s %d", ErrInsufficientFunds, p.UserID, p.Kind, p.Delta)
		}
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, fmt.Errorf("wallet: no wallet for user %d", p.UserID)
		}
		return 0, fmt.Errorf("wallet: apply %s to user %d: %w", p.Kind, p.UserID, err)
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO transactions (user_id, kind, is_real, amount_cents, balance_after_cents, ref_id, memo)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		p.UserID, string(p.Kind), p.IsReal, p.Delta, balanceAfter, p.RefID, nullStr(p.Memo))
	if err != nil {
		return 0, fmt.Errorf("wallet: record %s for user %d: %w", p.Kind, p.UserID, err)
	}
	return balanceAfter, nil
}

// Lock takes a row lock on each named wallet and returns their balances.
//
// It always locks in ascending user_id order. That ordering is the whole
// reason this is a function rather than an inline query: a referral payout
// locks two wallets, and without a consistent order two concurrent spins by
// users who refer each other would deadlock.
func Lock(ctx context.Context, tx pgx.Tx, userIDs ...int64) (map[int64]Balances, error) {
	if len(userIDs) == 0 {
		return map[int64]Balances{}, nil
	}
	rows, err := tx.Query(ctx, `
		SELECT user_id, real_cents, demo_cents, held_cents
		  FROM wallets
		 WHERE user_id = ANY($1)
		 ORDER BY user_id
		   FOR UPDATE`, userIDs)
	if err != nil {
		return nil, fmt.Errorf("wallet: lock %v: %w", userIDs, err)
	}
	defer rows.Close()

	out := make(map[int64]Balances, len(userIDs))
	for rows.Next() {
		var b Balances
		if err := rows.Scan(&b.UserID, &b.RealCents, &b.DemoCents, &b.HeldCents); err != nil {
			return nil, fmt.Errorf("wallet: scan locked wallet: %w", err)
		}
		out[b.UserID] = b
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("wallet: lock %v: %w", userIDs, err)
	}
	for _, id := range userIDs {
		if _, ok := out[id]; !ok {
			return nil, fmt.Errorf("wallet: no wallet for user %d", id)
		}
	}
	return out, nil
}

// HouseLock locks the single house row and returns it. Convention, asserted in
// review: callers lock wallets FIRST, then the house. Every path that touches
// both follows that order so they cannot deadlock against each other.
func HouseLock(ctx context.Context, tx pgx.Tx) (House, error) {
	var h House
	err := tx.QueryRow(ctx,
		`SELECT bankroll_cents, rake_cents FROM house WHERE id = 1 FOR UPDATE`).
		Scan(&h.BankrollCents, &h.RakeCents)
	if err != nil {
		return h, fmt.Errorf("wallet: lock house: %w", err)
	}
	return h, nil
}

// MoveHouse applies signed deltas to the house row. Both are relative, for the
// same reason Move's is.
func MoveHouse(ctx context.Context, tx pgx.Tx, bankrollDelta, rakeDelta int64) (House, error) {
	var h House
	err := tx.QueryRow(ctx, `
		UPDATE house
		   SET bankroll_cents = bankroll_cents + $1,
		       rake_cents     = rake_cents + $2,
		       updated_at     = now()
		 WHERE id = 1
		 RETURNING bankroll_cents, rake_cents`, bankrollDelta, rakeDelta).
		Scan(&h.BankrollCents, &h.RakeCents)
	if err != nil {
		if isAnyCheckViolation(err) {
			// The bankroll would have gone negative. Because admission control
			// vetoes any stake the bankroll cannot cover BEFORE the draw, this
			// is unreachable in correct operation — so the transaction rolls
			// back (the spin never happened, the stake is untouched) rather
			// than paying out money we cannot account for.
			return h, fmt.Errorf("%w: bankroll delta %d", ErrBankrollExhausted, bankrollDelta)
		}
		return h, fmt.Errorf("wallet: move house: %w", err)
	}
	return h, nil
}

// Hold moves funds out of the spendable balance and into held_cents, as one
// atomic step, and records the debit in the ledger.
//
// This is the withdrawal hold. It is why a player cannot gamble money that is
// already queued for payout, and why SUM(held_cents) is continuously checkable
// against the sum of open withdrawals.
func Hold(ctx context.Context, tx pgx.Tx, userID, amount int64, withdrawalID *int64) (int64, error) {
	if amount <= 0 {
		return 0, fmt.Errorf("wallet: hold amount must be positive, got %d", amount)
	}
	after, err := Move(ctx, tx, MoveParams{
		UserID: userID, Kind: KindWithdraw, IsReal: true,
		Delta: -amount, RefID: withdrawalID, Memo: "withdrawal requested",
	})
	if err != nil {
		return 0, err
	}
	if _, err := tx.Exec(ctx,
		`UPDATE wallets SET held_cents = held_cents + $2 WHERE user_id = $1`,
		userID, amount); err != nil {
		return 0, fmt.Errorf("wallet: add hold for user %d: %w", userID, err)
	}
	return after, nil
}

// ReleaseHold drops a hold without returning the money to the player: the
// payout succeeded and the cash has left our float.
func ReleaseHold(ctx context.Context, tx pgx.Tx, userID, amount int64) error {
	if amount <= 0 {
		return fmt.Errorf("wallet: release amount must be positive, got %d", amount)
	}
	if _, err := tx.Exec(ctx,
		`UPDATE wallets SET held_cents = held_cents - $2, updated_at = now() WHERE user_id = $1`,
		userID, amount); err != nil {
		if isAnyCheckViolation(err) {
			return fmt.Errorf("wallet: releasing %d would drive user %d held_cents negative "+
				"(hold accounting is corrupt): %w", amount, userID, err)
		}
		return fmt.Errorf("wallet: release hold for user %d: %w", userID, err)
	}
	return nil
}

// ReverseHold drops a hold AND returns the money to the player, in one step:
// the payout failed or was rejected.
//
// The reference implementation had no equivalent of this. A failed payout there
// left the user debited with the withdrawal marked confirmed, permanently, with
// no compensating entry anywhere and no query that could have noticed.
func ReverseHold(ctx context.Context, tx pgx.Tx, userID, amount int64, withdrawalID *int64, reason string) (int64, error) {
	if err := ReleaseHold(ctx, tx, userID, amount); err != nil {
		return 0, err
	}
	return Move(ctx, tx, MoveParams{
		UserID: userID, Kind: KindWithdrawReversed, IsReal: true,
		Delta: amount, RefID: withdrawalID, Memo: reason,
	})
}

// Get reads balances without locking, for display.
func Get(ctx context.Context, q Querier, userID int64) (Balances, error) {
	var b Balances
	err := q.QueryRow(ctx,
		`SELECT user_id, real_cents, demo_cents, held_cents FROM wallets WHERE user_id = $1`, userID).
		Scan(&b.UserID, &b.RealCents, &b.DemoCents, &b.HeldCents)
	if err != nil {
		return b, fmt.Errorf("wallet: get user %d: %w", userID, err)
	}
	return b, nil
}

// GetHouse reads the house row without locking, for display.
func GetHouse(ctx context.Context, q Querier) (House, error) {
	var h House
	err := q.QueryRow(ctx, `SELECT bankroll_cents, rake_cents FROM house WHERE id = 1`).
		Scan(&h.BankrollCents, &h.RakeCents)
	if err != nil {
		return h, fmt.Errorf("wallet: get house: %w", err)
	}
	return h, nil
}

// Querier is the read subset shared by *pgxpool.Pool and pgx.Tx.
type Querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

var _ Querier = (*pgxpool.Pool)(nil)

// ── helpers ──────────────────────────────────────────────────────────────────

func isAnyCheckViolation(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23514" // check_violation
	}
	return false
}

func isCheckViolation(err error, constraint string) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23514" && pgErr.ConstraintName == constraint
	}
	return false
}

func nullStr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
