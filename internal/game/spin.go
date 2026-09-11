package game

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dibon/braelaspin/internal/wallet"
)

// Economics are the tunable knobs, all in basis points or cents.
type Economics struct {
	RTPBP         int
	RakeBP        int
	ReferralBP    int
	MinStakeCents int64
	MaxStakeCents int64
}

// Service runs spins.
type Service struct {
	pool *pgxpool.Pool
	econ Economics
	pick Picker
}

func NewService(pool *pgxpool.Pool, econ Economics) *Service {
	return &Service{pool: pool, econ: econ}
}

// WithPicker substitutes the entropy source. Tests use it; production does not.
func (s *Service) WithPicker(p Picker) *Service {
	c := *s
	c.pick = p
	return &c
}

// Request is one spin attempt.
type Request struct {
	UserID     int64
	StakeCents int64
	IsReal     bool
	ClientRef  string
}

// Result is what the player and the client animation need.
type Result struct {
	SpinID       int64     `json:"spin_id"`
	SegmentIndex int       `json:"segment_index"`
	MultiplierBP int       `json:"multiplier_bp"`
	StakeCents   int64     `json:"stake_cents"`
	PayoutCents  int64     `json:"payout_cents"`
	NetCents     int64     `json:"net_cents"`
	BalanceCents int64     `json:"balance_cents"`
	IsReal       bool      `json:"is_real"`
	CreatedAt    time.Time `json:"created_at"`
}

// Player-facing failures. These map onto stable API error codes, and the client
// turns ErrInsufficientFunds into a Deposit prompt rather than a dead end.
var (
	ErrStakeTooSmall     = errors.New("stake below minimum")
	ErrStakeTooLarge     = errors.New("stake above maximum")
	ErrInsufficientFunds = errors.New("insufficient funds")
	ErrStakeExceedsBank  = errors.New("stake exceeds what the bankroll can cover")
	ErrDuplicateSpin     = errors.New("duplicate client_ref")
)

// StakeLimitError carries the limit that was breached so the client can show a
// useful number instead of a bare rejection.
type StakeLimitError struct {
	Err        error
	LimitCents int64
}

func (e *StakeLimitError) Error() string {
	return fmt.Sprintf("%v (limit %d cents)", e.Err, e.LimitCents)
}
func (e *StakeLimitError) Unwrap() error { return e.Err }

// Spin plays one round.
//
// Everything happens in a single READ COMMITTED transaction. READ COMMITTED is
// sufficient -- not a compromise -- precisely because every balance read below
// is a FOR UPDATE read and every write is a relative UPDATE. There is no
// read-modify-write on stale data anywhere, so there is no serialization
// failure to retry and no retry loop in this codebase.
func (s *Service) Spin(ctx context.Context, req Request) (*Result, error) {
	if req.StakeCents < s.econ.MinStakeCents {
		return nil, &StakeLimitError{Err: ErrStakeTooSmall, LimitCents: s.econ.MinStakeCents}
	}
	if req.StakeCents > s.econ.MaxStakeCents {
		return nil, &StakeLimitError{Err: ErrStakeTooLarge, LimitCents: s.econ.MaxStakeCents}
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("spin: begin: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op once committed

	// ── who is playing, and who (if anyone) earns commission ──────────────
	var referrerID *int64
	if err := tx.QueryRow(ctx,
		`SELECT referred_by FROM users WHERE id = $1 AND is_active`, req.UserID).
		Scan(&referrerID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("spin: user %d not found or inactive", req.UserID)
		}
		return nil, fmt.Errorf("spin: load user %d: %w", req.UserID, err)
	}

	// Commission is only paid on real play, and only to a real referrer.
	//
	// The reference implementation fell back to `User.objects.get(id=1)` when a
	// player had no referrer, so the owner silently collected 5% of every
	// unreferred player's turnover -- and the whole spin crashed if user 1 was
	// ever deleted. Here, no referrer means no commission.
	payCommission := req.IsReal && referrerID != nil && *referrerID != req.UserID && s.econ.ReferralBP > 0

	// ── lock: wallets first (ascending id), then the house ────────────────
	// The ordering is fixed by convention across the whole codebase so that
	// concurrent spins by users who refer each other cannot deadlock.
	lockIDs := []int64{req.UserID}
	if payCommission {
		lockIDs = append(lockIDs, *referrerID)
	}
	balances, err := wallet.Lock(ctx, tx, lockIDs...)
	if err != nil {
		return nil, err
	}
	me := balances[req.UserID]

	if me.Spendable(req.IsReal) < req.StakeCents {
		return nil, &StakeLimitError{Err: ErrInsufficientFunds, LimitCents: me.Spendable(req.IsReal)}
	}

	var house wallet.House
	if req.IsReal {
		house, err = wallet.HouseLock(ctx, tx)
		if err != nil {
			return nil, err
		}
		// ── admission control ─────────────────────────────────────────────
		// The ONLY role the bankroll plays. It vetoes a stake whose worst case
		// it could not pay, BEFORE the draw -- and then has no influence
		// whatsoever on which segment comes up.
		//
		// This is the inverse of the reference implementation, which built the
		// candidate set from whatever the pool could afford and drew uniformly
		// from it, so the odds moved with the pool depth and player EV ran at
		// 10x-70x the stake.
		if exposure := MaxExposure(req.StakeCents); house.BankrollCents < exposure {
			return nil, &StakeLimitError{
				Err:        ErrStakeExceedsBank,
				LimitCents: MaxAffordableStake(house.BankrollCents),
			}
		}
	}

	// ── the draw ──────────────────────────────────────────────────────────
	segIdx, multBP, err := s.pick.Pick()
	if err != nil {
		return nil, fmt.Errorf("spin: %w", err)
	}
	payout := Payout(req.StakeCents, multBP)

	var rake, commission int64
	if req.IsReal {
		rake = req.StakeCents * int64(s.econ.RakeBP) / 10000
		if payCommission {
			commission = req.StakeCents * int64(s.econ.ReferralBP) / 10000
		}
	}

	// ── record ────────────────────────────────────────────────────────────
	var spinID int64
	var createdAt time.Time
	err = tx.QueryRow(ctx, `
		INSERT INTO spins (user_id, is_real, stake_cents, segment_index,
		                   multiplier_bp, payout_cents, rake_cents, client_ref)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		RETURNING id, created_at`,
		req.UserID, req.IsReal, req.StakeCents, segIdx, multBP, payout, rake, req.ClientRef).
		Scan(&spinID, &createdAt)
	if err != nil {
		if isUniqueViolation(err, "spins_client_ref") {
			return nil, ErrDuplicateSpin
		}
		return nil, fmt.Errorf("spin: insert: %w", err)
	}

	// ── move the money ────────────────────────────────────────────────────
	if _, err := wallet.Move(ctx, tx, wallet.MoveParams{
		UserID: req.UserID, Kind: wallet.KindBet, IsReal: req.IsReal,
		Delta: -req.StakeCents, RefID: &spinID,
	}); err != nil {
		if errors.Is(err, wallet.ErrInsufficientFunds) {
			// Unreachable: we hold the row lock and checked the balance above.
			return nil, &StakeLimitError{Err: ErrInsufficientFunds, LimitCents: me.Spendable(req.IsReal)}
		}
		return nil, err
	}

	balanceAfter := me.Spendable(req.IsReal) - req.StakeCents
	if payout > 0 {
		balanceAfter, err = wallet.Move(ctx, tx, wallet.MoveParams{
			UserID: req.UserID, Kind: wallet.KindWin, IsReal: req.IsReal,
			Delta: payout, RefID: &spinID,
		})
		if err != nil {
			return nil, err
		}
	}

	if req.IsReal {
		// Relative deltas, both of them. The stake comes in; the payout, the
		// rake and the commission go out.
		//
		// If this drives the bankroll negative the CHECK aborts the whole
		// transaction and the stake is never taken. That is the backstop
		// against the bug class where a payout larger than the pool was
		// silently not debited at all.
		if _, err := wallet.MoveHouse(ctx, tx,
			req.StakeCents-payout-rake-commission, rake); err != nil {
			if errors.Is(err, wallet.ErrBankrollExhausted) {
				return nil, fmt.Errorf("spin: bankroll could not cover payout %d for stake %d "+
					"despite admission control passing (this is a bug): %w", payout, req.StakeCents, err)
			}
			return nil, err
		}

		if commission > 0 {
			if _, err := wallet.Move(ctx, tx, wallet.MoveParams{
				UserID: *referrerID, Kind: wallet.KindReferral, IsReal: true,
				Delta: commission, RefID: &spinID,
				Memo:  fmt.Sprintf("%d bp of a %d cent stake", s.econ.ReferralBP, req.StakeCents),
			}); err != nil {
				return nil, err
			}
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("spin: commit: %w", err)
	}

	return &Result{
		SpinID:       spinID,
		SegmentIndex: segIdx,
		MultiplierBP: multBP,
		StakeCents:   req.StakeCents,
		PayoutCents:  payout,
		NetCents:     payout - req.StakeCents,
		BalanceCents: balanceAfter,
		IsReal:       req.IsReal,
		CreatedAt:    createdAt,
	}, nil
}

// FindByClientRef returns an already-played spin, so a client that timed out
// mid-request can discover what actually happened instead of guessing.
func (s *Service) FindByClientRef(ctx context.Context, userID int64, clientRef string) (*Result, error) {
	var r Result
	var balance int64
	err := s.pool.QueryRow(ctx, `
		SELECT s.id, s.segment_index, s.multiplier_bp, s.stake_cents, s.payout_cents,
		       s.is_real, s.created_at,
		       CASE WHEN s.is_real THEN w.real_cents ELSE w.demo_cents END
		  FROM spins s JOIN wallets w ON w.user_id = s.user_id
		 WHERE s.user_id = $1 AND s.client_ref = $2`, userID, clientRef).
		Scan(&r.SpinID, &r.SegmentIndex, &r.MultiplierBP, &r.StakeCents, &r.PayoutCents,
			&r.IsReal, &r.CreatedAt, &balance)
	if err != nil {
		return nil, err
	}
	r.NetCents = r.PayoutCents - r.StakeCents
	r.BalanceCents = balance
	return &r, nil
}

// Config is GET /v1/game/config.
type Config struct {
	Segments       []Segment `json:"segments"`
	RTPBP          int       `json:"rtp_bp"`
	MinStakeCents  int64     `json:"min_stake_cents"`
	MaxStakeCents  int64     `json:"max_stake_cents"`
	MaxMultiplier  int       `json:"max_multiplier_bp"`
}

// Config reports the wheel and the currently admissible stake range.
//
// MaxStakeCents is the real constraint: it is the lower of the configured
// ceiling and what the bankroll can currently cover, so the client can bound
// its input rather than discovering the limit through a rejection.
func (s *Service) Config(ctx context.Context) (*Config, error) {
	h, err := wallet.GetHouse(ctx, s.pool)
	if err != nil {
		return nil, err
	}
	maxStake := s.econ.MaxStakeCents
	if afford := MaxAffordableStake(h.BankrollCents); afford < maxStake {
		maxStake = afford
	}
	return &Config{
		Segments:      Segments(),
		RTPBP:         s.econ.RTPBP,
		MinStakeCents: s.econ.MinStakeCents,
		MaxStakeCents: maxStake,
		MaxMultiplier: MaxMultBP(),
	}, nil
}

func isUniqueViolation(err error, constraint string) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23505" && (constraint == "" || pgErr.ConstraintName == constraint)
	}
	return false
}
