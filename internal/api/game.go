package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/dibon/braelaspin/internal/game"
	"github.com/dibon/braelaspin/internal/httpx"
	"github.com/dibon/braelaspin/internal/wallet"
)

type spinReq struct {
	StakeCents int64  `json:"stake_cents"`
	Real       bool   `json:"real"`
	ClientRef  string `json:"client_ref"`
}

// gameConfig is cached briefly: it changes only when the bankroll moves, and
// every client polls it on resume.
func (s *Server) gameConfig(w http.ResponseWriter, r *http.Request) error {
	cfg, err := s.game.Config(r.Context())
	if err != nil {
		return err
	}
	w.Header().Set("Cache-Control", "public, max-age=10")
	httpx.JSON(w, http.StatusOK, cfg)
	return nil
}

// spin plays one round.
//
// The idempotency dance is the part worth reading. A client that times out
// mid-request retries with the SAME client_ref; we must return the original
// outcome rather than playing a second round with their money.
func (s *Server) spin(w http.ResponseWriter, r *http.Request) error {
	var req spinReq
	if err := httpx.Decode(w, r, &req); err != nil {
		return err
	}
	if req.ClientRef == "" || len(req.ClientRef) > 64 {
		return httpx.ErrBadRequest("A client_ref is required.")
	}

	ctx := r.Context()
	uid := httpx.UserID(ctx)

	// Claim the key. The loser either replays the stored response or, if the
	// original is still in flight, is told to retry rather than being allowed
	// to play a second round.
	won, prior, err := s.redis.Claim(ctx, uid, req.ClientRef, 24*time.Hour)
	if err != nil {
		return err
	}
	if !won {
		if len(prior) > 0 {
			httpx.Raw(w, http.StatusOK, prior)
			return nil
		}
		return httpx.ErrConflict("spin_in_flight",
			"That spin is still being processed. Please wait a moment.")
	}

	result, err := s.game.Spin(ctx, game.Request{
		UserID:     uid,
		StakeCents: req.StakeCents,
		IsReal:     req.Real,
		ClientRef:  req.ClientRef,
	})
	if err != nil {
		// Release the claim so a corrected retry is possible. Without this a
		// rejected stake would leave the key held and the next attempt would
		// look like a duplicate.
		_ = s.redis.Release(ctx, uid, req.ClientRef)
		return mapSpinError(err)
	}

	body, err := json.Marshal(result)
	if err != nil {
		return err
	}
	// Store before responding: if we crash here, the retry replays rather than
	// replays-and-plays-again.
	if err := s.redis.Store(ctx, uid, req.ClientRef, body, 24*time.Hour); err != nil {
		httpx.Logger(ctx).Warn("could not store idempotent response",
			"user_id", uid, "client_ref", req.ClientRef, "err", err)
	}

	httpx.Logger(ctx).Info("spin",
		"user_id", uid, "real", result.IsReal, "stake_cents", result.StakeCents,
		"segment", result.SegmentIndex, "payout_cents", result.PayoutCents)

	httpx.Raw(w, http.StatusOK, body)
	return nil
}

// mapSpinError turns domain failures into stable client codes. The limit that
// was breached travels in meta so the client can show a real number.
func mapSpinError(err error) error {
	var lim *game.StakeLimitError
	if errors.As(err, &lim) {
		switch {
		case errors.Is(lim.Err, game.ErrStakeTooSmall):
			return httpx.Errorf(http.StatusBadRequest, "stake_too_small",
				"That bet is below the minimum.").WithMeta("min_stake_cents", lim.LimitCents)
		case errors.Is(lim.Err, game.ErrStakeTooLarge):
			return httpx.Errorf(http.StatusBadRequest, "stake_too_large",
				"That bet is above the maximum.").WithMeta("max_stake_cents", lim.LimitCents)
		case errors.Is(lim.Err, game.ErrInsufficientFunds):
			return httpx.Errorf(http.StatusBadRequest, "insufficient_funds",
				"You don't have enough for that bet.").WithMeta("balance_cents", lim.LimitCents)
		case errors.Is(lim.Err, game.ErrStakeExceedsBank):
			return httpx.Errorf(http.StatusBadRequest, "stake_exceeds_bankroll",
				"That bet is too large right now.").WithMeta("max_stake_cents", lim.LimitCents)
		}
	}
	if errors.Is(err, game.ErrDuplicateSpin) {
		return httpx.ErrConflict("duplicate_spin", "That spin was already played.")
	}
	if errors.Is(err, wallet.ErrBankrollExhausted) {
		// Admission control should have caught this, so reaching here is a bug
		// worth surfacing as a 500 rather than blaming the player.
		return httpx.ErrInternal().WithCause(err)
	}
	return err
}

// spinByRef answers "did my bet actually go through?" after a client timeout.
func (s *Server) spinByRef(w http.ResponseWriter, r *http.Request) error {
	ref := chi.URLParam(r, "ref")
	if ref == "" {
		return httpx.ErrBadRequest("A client_ref is required.")
	}
	result, err := s.game.FindByClientRef(r.Context(), httpx.UserID(r.Context()), ref)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Definitively: it never happened. The client can safely retry.
			return httpx.ErrNotFound()
		}
		return err
	}
	httpx.JSON(w, http.StatusOK, result)
	return nil
}

// spins lists recent rounds, newest first, keyset-paginated.
func (s *Server) spins(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	limit := clampLimit(r.URL.Query().Get("limit"), 25, 100)
	cursor := parseCursor(r.URL.Query().Get("cursor"))

	rows, err := s.db.Query(ctx, `
		SELECT id, is_real, stake_cents, segment_index, multiplier_bp, payout_cents, created_at
		  FROM spins
		 WHERE user_id = $1 AND ($2::bigint IS NULL OR id < $2)
		 ORDER BY id DESC
		 LIMIT $3`, httpx.UserID(ctx), cursor, limit)
	if err != nil {
		return err
	}
	defer rows.Close()

	type item struct {
		ID           int64     `json:"id"`
		IsReal       bool      `json:"is_real"`
		StakeCents   int64     `json:"stake_cents"`
		SegmentIndex int       `json:"segment_index"`
		MultiplierBP int       `json:"multiplier_bp"`
		PayoutCents  int64     `json:"payout_cents"`
		CreatedAt    time.Time `json:"created_at"`
	}
	items := make([]item, 0, limit)
	for rows.Next() {
		var it item
		if err := rows.Scan(&it.ID, &it.IsReal, &it.StakeCents, &it.SegmentIndex,
			&it.MultiplierBP, &it.PayoutCents, &it.CreatedAt); err != nil {
			return err
		}
		items = append(items, it)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	httpx.JSON(w, http.StatusOK, page(items, len(items) == limit,
		func() int64 { return items[len(items)-1].ID }))
	return nil
}

// history is the player-facing ledger — the source of truth for any dispute.
func (s *Server) history(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	limit := clampLimit(r.URL.Query().Get("limit"), 50, 200)
	cursor := parseCursor(r.URL.Query().Get("cursor"))
	kind := r.URL.Query().Get("kind")

	rows, err := s.db.Query(ctx, `
		SELECT id, kind, is_real, amount_cents, balance_after_cents, memo, created_at
		  FROM transactions
		 WHERE user_id = $1
		   AND ($2::bigint IS NULL OR id < $2)
		   AND ($3 = '' OR kind = $3)
		 ORDER BY id DESC
		 LIMIT $4`, httpx.UserID(ctx), cursor, kind, limit)
	if err != nil {
		return err
	}
	defer rows.Close()

	type item struct {
		ID                int64     `json:"id"`
		Kind              string    `json:"kind"`
		IsReal            bool      `json:"is_real"`
		AmountCents       int64     `json:"amount_cents"`
		BalanceAfterCents int64     `json:"balance_after_cents"`
		Memo              *string   `json:"memo,omitempty"`
		CreatedAt         time.Time `json:"created_at"`
	}
	items := make([]item, 0, limit)
	for rows.Next() {
		var it item
		if err := rows.Scan(&it.ID, &it.Kind, &it.IsReal, &it.AmountCents,
			&it.BalanceAfterCents, &it.Memo, &it.CreatedAt); err != nil {
			return err
		}
		items = append(items, it)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	httpx.JSON(w, http.StatusOK, page(items, len(items) == limit,
		func() int64 { return items[len(items)-1].ID }))
	return nil
}

// ── pagination helpers ───────────────────────────────────────────────────────

type pageResp[T any] struct {
	Items      []T    `json:"items"`
	NextCursor *int64 `json:"next_cursor,omitempty"`
}

// page builds a keyset-paginated response. Keyset rather than OFFSET because
// the ledger is append-only and high-traffic: OFFSET drifts when rows are
// inserted mid-scroll, and it gets slower the deeper you page.
func page[T any](items []T, hasMore bool, last func() int64) pageResp[T] {
	out := pageResp[T]{Items: items}
	if hasMore && len(items) > 0 {
		c := last()
		out.NextCursor = &c
	}
	return out
}

func clampLimit(raw string, def, max int) int {
	if raw == "" {
		return def
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return def
	}
	if n > max {
		return max
	}
	return n
}

func parseCursor(raw string) *int64 {
	if raw == "" {
		return nil
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || n <= 0 {
		return nil
	}
	return &n
}
