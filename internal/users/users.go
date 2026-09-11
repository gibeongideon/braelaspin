// Package users owns the user record and the registration transaction.
//
// It deliberately knows nothing about HTTP and nothing about password hashing:
// the caller supplies an already-hashed password, because argon2 takes ~50-100ms
// and holding a database transaction open for that long is pure waste.
package users

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dibon/braelaspin/internal/auth"
	"github.com/dibon/braelaspin/internal/wallet"
)

type User struct {
	ID            int64      `json:"id"`
	Phone         string     `json:"phone"`
	RefCode       string     `json:"ref_code"`
	ReferredBy    *int64     `json:"-"`
	PhoneVerified bool       `json:"phone_verified"`
	IsAdmin       bool       `json:"is_admin"`
	IsActive      bool       `json:"is_active"`
	DemoTopupAt   *time.Time `json:"demo_topup_at,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
}

var (
	ErrPhoneTaken      = errors.New("phone number already registered")
	ErrNotFound        = errors.New("user not found")
	ErrReferrerUnknown = errors.New("referral code not recognised")
)

type Service struct {
	pool      *pgxpool.Pool
	demoGrant int64
}

func NewService(pool *pgxpool.Pool, demoGrantCents int64) *Service {
	return &Service{pool: pool, demoGrant: demoGrantCents}
}

const userCols = `id, phone, ref_code, referred_by, phone_verified, is_admin, is_active,
                  demo_topup_at, created_at`

func scanUser(row pgx.Row) (*User, error) {
	var u User
	err := row.Scan(&u.ID, &u.Phone, &u.RefCode, &u.ReferredBy, &u.PhoneVerified,
		&u.IsAdmin, &u.IsActive, &u.DemoTopupAt, &u.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// CreateParams is one registration.
type CreateParams struct {
	Phone        string // already normalised by auth.Normalise
	PasswordHash string
	// ReferrerCode is the code the new user arrived with, if any. An unknown
	// code is an error rather than a silent ignore: a user who followed a
	// friend's link should not discover later that the attribution vanished.
	ReferrerCode string
}

// Create registers a user, provisions their wallet, and posts the demo grant —
// all in ONE transaction. Either the whole account exists or none of it does.
//
// The reference implementation did this across several statements plus a
// post-save signal, so a failure partway through left a user with no wallet,
// and its referral binding was a separate UPDATE that could silently fail.
func (s *Service) Create(ctx context.Context, p CreateParams) (*User, error) {
	// A referral-code collision is astronomically unlikely (8 Crockford
	// base32 chars) but it is a unique constraint, so handle it rather than
	// failing a registration for it.
	const attempts = 5
	for i := 0; i < attempts; i++ {
		u, err := s.createOnce(ctx, p)
		if errors.Is(err, errRefCodeCollision) {
			continue
		}
		return u, err
	}
	return nil, fmt.Errorf("users: could not allocate a unique referral code in %d attempts", attempts)
}

var errRefCodeCollision = errors.New("ref code collision")

func (s *Service) createOnce(ctx context.Context, p CreateParams) (*User, error) {
	refCode, err := auth.NewRefCode()
	if err != nil {
		return nil, err
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("users: begin: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op after commit

	// Resolve the referrer first, so an unknown code fails before we create
	// anything at all.
	var referrerID *int64
	if p.ReferrerCode != "" {
		var id int64
		err := tx.QueryRow(ctx,
			`SELECT id FROM users WHERE ref_code = $1 AND is_active`, p.ReferrerCode).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrReferrerUnknown
		}
		if err != nil {
			return nil, fmt.Errorf("users: resolve referrer: %w", err)
		}
		referrerID = &id
	}

	u, err := scanUser(tx.QueryRow(ctx, `
		INSERT INTO users (phone, password_hash, ref_code, referred_by)
		VALUES ($1, $2, $3, $4)
		RETURNING `+userCols,
		p.Phone, p.PasswordHash, refCode, referrerID))
	if err != nil {
		switch {
		case isUnique(err, "users_phone_key"):
			return nil, ErrPhoneTaken
		case isUnique(err, "users_ref_code_key"):
			return nil, errRefCodeCollision
		}
		return nil, fmt.Errorf("users: insert: %w", err)
	}

	if _, err := tx.Exec(ctx, `INSERT INTO wallets (user_id) VALUES ($1)`, u.ID); err != nil {
		return nil, fmt.Errorf("users: provision wallet: %w", err)
	}

	// The demo grant goes through wallet.Move like every other money movement,
	// so it appears in the player's own history and satisfies invariant I1.
	// There is no back door for creating balance, not even at signup.
	//
	// No explicit row lock is needed: we inserted this wallet in this
	// transaction, so nothing else can see it yet.
	if s.demoGrant > 0 {
		if _, err := wallet.Move(ctx, tx, wallet.MoveParams{
			UserID: u.ID, Kind: wallet.KindDemoGrant, IsReal: false,
			Delta: s.demoGrant, Memo: "welcome bonus",
		}); err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("users: commit: %w", err)
	}
	return u, nil
}

// ByPhone loads a user and their password hash for a login attempt.
func (s *Service) ByPhone(ctx context.Context, phone string) (*User, string, error) {
	var u User
	var hash string
	err := s.pool.QueryRow(ctx,
		`SELECT `+userCols+`, password_hash FROM users WHERE phone = $1`, phone).
		Scan(&u.ID, &u.Phone, &u.RefCode, &u.ReferredBy, &u.PhoneVerified,
			&u.IsAdmin, &u.IsActive, &u.DemoTopupAt, &u.CreatedAt, &hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, "", ErrNotFound
	}
	if err != nil {
		return nil, "", fmt.Errorf("users: load by phone: %w", err)
	}
	return &u, hash, nil
}

func (s *Service) ByID(ctx context.Context, id int64) (*User, error) {
	u, err := scanUser(s.pool.QueryRow(ctx, `SELECT `+userCols+` FROM users WHERE id = $1`, id))
	if err != nil && !errors.Is(err, ErrNotFound) {
		return nil, fmt.Errorf("users: load %d: %w", id, err)
	}
	return u, err
}

// IsAdmin is used on token refresh so a new access token carries a current
// admin flag rather than replaying whatever the expiring one claimed.
func (s *Service) IsAdmin(ctx context.Context, id int64) (bool, error) {
	var isAdmin, isActive bool
	err := s.pool.QueryRow(ctx,
		`SELECT is_admin, is_active FROM users WHERE id = $1`, id).Scan(&isAdmin, &isActive)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, ErrNotFound
	}
	if err != nil {
		return false, fmt.Errorf("users: load flags for %d: %w", id, err)
	}
	if !isActive {
		return false, ErrNotFound // a deactivated account must not refresh
	}
	return isAdmin, nil
}

// ReferralStats is the affiliate summary shown in the app.
type ReferralStats struct {
	Count       int   `json:"count"`
	EarnedCents int64 `json:"earned_cents"`
	Last30Cents int64 `json:"last_30_cents"`
}

func (s *Service) ReferralStats(ctx context.Context, userID int64) (ReferralStats, error) {
	var st ReferralStats
	err := s.pool.QueryRow(ctx, `
		SELECT
		  (SELECT COUNT(*) FROM users WHERE referred_by = $1),
		  (SELECT COALESCE(SUM(amount_cents), 0) FROM transactions
		    WHERE user_id = $1 AND kind = 'referral'),
		  (SELECT COALESCE(SUM(amount_cents), 0) FROM transactions
		    WHERE user_id = $1 AND kind = 'referral'
		      AND created_at > now() - interval '30 days')`, userID).
		Scan(&st.Count, &st.EarnedCents, &st.Last30Cents)
	if err != nil {
		return st, fmt.Errorf("users: referral stats for %d: %w", userID, err)
	}
	return st, nil
}

// SetActive suspends or reinstates an account.
func (s *Service) SetActive(ctx context.Context, userID int64, active bool) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE users SET is_active = $2 WHERE id = $1`, userID, active)
	if err != nil {
		return fmt.Errorf("users: set active for %d: %w", userID, err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// TopUpDemo re-grants demo funds, subject to a cooldown.
//
// The cooldown is enforced by the UPDATE's WHERE clause rather than by a
// read-then-write, so two concurrent requests cannot both pass the check.
func (s *Service) TopUpDemo(ctx context.Context, userID int64, cooldown time.Duration, target int64) (int64, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("users: begin: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var ok bool
	err = tx.QueryRow(ctx, `
		UPDATE users SET demo_topup_at = now()
		 WHERE id = $1
		   AND (demo_topup_at IS NULL OR demo_topup_at < now() - $2::interval)
		 RETURNING true`, userID, cooldown.String()).Scan(&ok)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrTopUpNotEligible
	}
	if err != nil {
		return 0, fmt.Errorf("users: claim demo top-up: %w", err)
	}

	balances, err := wallet.Lock(ctx, tx, userID)
	if err != nil {
		return 0, err
	}
	// Top up TO the target rather than BY it, so repeated claims cannot stack
	// into an unbounded demo balance.
	delta := target - balances[userID].DemoCents
	if delta <= 0 {
		return balances[userID].DemoCents, tx.Commit(ctx)
	}

	after, err := wallet.Move(ctx, tx, wallet.MoveParams{
		UserID: userID, Kind: wallet.KindDemoGrant, IsReal: false,
		Delta: delta, Memo: "demo top-up",
	})
	if err != nil {
		return 0, err
	}
	return after, tx.Commit(ctx)
}

var ErrTopUpNotEligible = errors.New("demo top-up not yet available")

func isUnique(err error, constraint string) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23505" && (constraint == "" || pgErr.ConstraintName == constraint)
	}
	return false
}
