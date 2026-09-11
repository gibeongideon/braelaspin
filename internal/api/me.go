package api

import (
	"errors"
	"net/http"

	"github.com/dibon/braelaspin/internal/game"
	"github.com/dibon/braelaspin/internal/httpx"
	"github.com/dibon/braelaspin/internal/users"
	"github.com/dibon/braelaspin/internal/wallet"
)

type balancesDTO struct {
	RealCents         int64 `json:"real_cents"`
	DemoCents         int64 `json:"demo_cents"`
	HeldCents         int64 `json:"held_cents"`
	WithdrawableCents int64 `json:"withdrawable_cents"`
}

func toBalances(b wallet.Balances) balancesDTO {
	return balancesDTO{
		RealCents: b.RealCents,
		DemoCents: b.DemoCents,
		HeldCents: b.HeldCents,
		// Funds already held against an open withdrawal are, by construction,
		// no longer in RealCents — so this needs no subtraction.
		WithdrawableCents: b.Withdrawable(),
	}
}

type meResp struct {
	User      userDTO             `json:"user"`
	Balances  balancesDTO         `json:"balances"`
	Game      *game.Config        `json:"game"`
	Referrals users.ReferralStats `json:"referrals"`
}

// me is the single call that repaints the whole app.
//
// The client hits it on launch and on resume, which is why it bundles profile,
// balances, game config and referral stats: four round trips on a Kenyan
// mobile connection is a visibly slower app than one.
func (s *Server) me(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	uid := httpx.UserID(ctx)

	u, err := s.users.ByID(ctx, uid)
	if err != nil {
		if errors.Is(err, users.ErrNotFound) {
			// The token is valid but the user is gone. Treat as unauthorised
			// rather than 404: the client should return to the sign-in screen.
			return httpx.ErrUnauthorized().WithCause(err)
		}
		return err
	}

	balances, err := wallet.Get(ctx, s.db, uid)
	if err != nil {
		return err
	}

	cfg, err := s.game.Config(ctx)
	if err != nil {
		return err
	}

	refs, err := s.users.ReferralStats(ctx, uid)
	if err != nil {
		return err
	}

	httpx.JSON(w, http.StatusOK, meResp{
		User:      s.userDTO(u),
		Balances:  toBalances(balances),
		Game:      cfg,
		Referrals: refs,
	})
	return nil
}

func (s *Server) walletBalances(w http.ResponseWriter, r *http.Request) error {
	b, err := wallet.Get(r.Context(), s.db, httpx.UserID(r.Context()))
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, toBalances(b))
	return nil
}

func (s *Server) demoTopUp(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	uid := httpx.UserID(ctx)

	after, err := s.users.TopUpDemo(ctx, uid, s.cfg.DemoTopupCooldn, s.cfg.DemoGrantCents)
	if err != nil {
		if errors.Is(err, users.ErrTopUpNotEligible) {
			return httpx.Errorf(http.StatusTooManyRequests, "demo_topup_not_eligible",
				"You can top up your practice balance once a day.").WithCause(err)
		}
		return err
	}

	httpx.Logger(ctx).Info("demo top-up", "user_id", uid, "demo_cents", after)
	httpx.JSON(w, http.StatusOK, map[string]int64{"demo_cents": after})
	return nil
}
