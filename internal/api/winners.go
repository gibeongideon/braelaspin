package api

import (
	"net/http"
	"time"

	"github.com/dibon/braelaspin/internal/auth"
	"github.com/dibon/braelaspin/internal/httpx"
)

type winner struct {
	Name        string    `json:"name"`
	PayoutCents int64     `json:"payout_cents"`
	MultiplierB int       `json:"multiplier_bp"`
	CreatedAt   time.Time `json:"created_at"`
}

// winners lists recent real-money wins, for the social-proof strip.
//
// Two rules make this safe to show to strangers:
//
//  1. REAL spins only. Showing practice wins as if they were real would be
//     straightforwardly dishonest.
//  2. The phone number never leaves the server. Players are identified by a
//     masked derivative, because the reference implementation leaked referees'
//     full numbers to anyone holding a referral code and this is the same
//     class of mistake.
func (s *Server) winners(w http.ResponseWriter, r *http.Request) error {
	rows, err := s.db.Query(r.Context(), `
		SELECT u.phone, sp.payout_cents, sp.multiplier_bp, sp.created_at
		  FROM spins sp
		  JOIN users u ON u.id = sp.user_id
		 WHERE sp.is_real
		   AND sp.payout_cents > sp.stake_cents   -- a real gain, not a 1x refund
		   AND sp.created_at > now() - interval '24 hours'
		 ORDER BY sp.payout_cents DESC, sp.id DESC
		 LIMIT 10`)
	if err != nil {
		return err
	}
	defer rows.Close()

	out := make([]winner, 0, 10)
	for rows.Next() {
		var phone string
		var wn winner
		if err := rows.Scan(&phone, &wn.PayoutCents, &wn.MultiplierB, &wn.CreatedAt); err != nil {
			return err
		}
		wn.Name = auth.Mask(phone)
		out = append(out, wn)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	// Short cache: this is decoration, and it must never cost a database query
	// per page view.
	w.Header().Set("Cache-Control", "public, max-age=30")
	httpx.JSON(w, http.StatusOK, map[string]any{"items": out})
	return nil
}
