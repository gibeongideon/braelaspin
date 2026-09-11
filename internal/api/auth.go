package api

import (
	"errors"
	"net/http"

	"github.com/dibon/braelaspin/internal/auth"
	"github.com/dibon/braelaspin/internal/httpx"
	"github.com/dibon/braelaspin/internal/users"
)

// ── wire types ───────────────────────────────────────────────────────────────

type registerReq struct {
	Phone    string `json:"phone"`
	Password string `json:"password"`
	RefCode  string `json:"ref_code,omitempty"`
}

type loginReq struct {
	Phone    string `json:"phone"`
	Password string `json:"password"`
}

type refreshReq struct {
	Refresh string `json:"refresh"`
}

type userDTO struct {
	ID            int64  `json:"id"`
	Phone         string `json:"phone"`
	PhoneDisplay  string `json:"phone_display"`
	RefCode       string `json:"ref_code"`
	RefLink       string `json:"ref_link"`
	PhoneVerified bool   `json:"phone_verified"`
	IsAdmin       bool   `json:"is_admin"`
}

type sessionResp struct {
	Access           string  `json:"access"`
	Refresh          string  `json:"refresh"`
	AccessExpiresAt  string  `json:"access_expires_at"`
	RefreshExpiresAt string  `json:"refresh_expires_at"`
	User             userDTO `json:"user"`
}

func (s *Server) userDTO(u *users.User) userDTO {
	return userDTO{
		ID:            u.ID,
		Phone:         u.Phone,
		PhoneDisplay:  auth.Pretty(u.Phone),
		RefCode:       u.RefCode,
		RefLink:       s.cfg.BaseURL + "/r/" + u.RefCode,
		PhoneVerified: u.PhoneVerified,
		IsAdmin:       u.IsAdmin,
	}
}

func (s *Server) session(t *auth.Tokens, u *users.User) sessionResp {
	return sessionResp{
		Access:           t.Access,
		Refresh:          t.Refresh,
		AccessExpiresAt:  t.AccessExpiresAt.UTC().Format("2006-01-02T15:04:05Z"),
		RefreshExpiresAt: t.RefreshExpiresAt.UTC().Format("2006-01-02T15:04:05Z"),
		User:             s.userDTO(u),
	}
}

// ── handlers ─────────────────────────────────────────────────────────────────

func (s *Server) register(w http.ResponseWriter, r *http.Request) error {
	var req registerReq
	if err := httpx.Decode(w, r, &req); err != nil {
		return err
	}

	phone, err := auth.Normalise(req.Phone)
	if err != nil {
		// Registration DOES report a bad phone specifically: the user needs to
		// know their number was unusable. (Login deliberately does not — see
		// below.)
		return httpx.Errorf(http.StatusBadRequest, "invalid_phone",
			"Enter a valid Kenyan mobile number, e.g. 0712 345 678.").WithCause(err)
	}
	if err := auth.ValidatePassword(req.Password); err != nil {
		code := "password_too_short"
		msg := "Choose a password of at least 8 characters."
		if errors.Is(err, auth.ErrPasswordTooLong) {
			code, msg = "password_too_long", "That password is too long."
		}
		return httpx.Errorf(http.StatusBadRequest, code, msg).WithCause(err)
	}

	// Hashed outside the transaction: argon2 takes ~50-100ms and a database
	// transaction has no business being open for it.
	hash, err := auth.HashPassword(req.Password, s.argon)
	if err != nil {
		return err
	}

	u, err := s.users.Create(r.Context(), users.CreateParams{
		Phone:        phone,
		PasswordHash: hash,
		ReferrerCode: req.RefCode,
	})
	if err != nil {
		switch {
		case errors.Is(err, users.ErrPhoneTaken):
			return httpx.ErrConflict("phone_taken",
				"That number is already registered. Try signing in instead.").WithCause(err)
		case errors.Is(err, users.ErrReferrerUnknown):
			return httpx.Errorf(http.StatusBadRequest, "unknown_referral_code",
				"That referral code was not recognised.").WithCause(err)
		}
		return err
	}

	tokens, err := s.tokens.Issue(r.Context(), u.ID, u.IsAdmin)
	if err != nil {
		return err
	}

	httpx.Logger(r.Context()).Info("user registered",
		"user_id", u.ID, "phone", u.Phone, "referred", u.ReferredBy != nil)

	httpx.JSON(w, http.StatusCreated, s.session(tokens, u))
	return nil
}

// invalidCredentials is returned for every failed login, whatever the cause.
//
// It must not distinguish "no such number" from "wrong password", or the
// endpoint becomes a way to enumerate which phone numbers hold accounts.
func invalidCredentials(cause error) *httpx.APIError {
	return httpx.Errorf(http.StatusUnauthorized, "invalid_credentials",
		"That number or password is not correct.").WithCause(cause)
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) error {
	var req loginReq
	if err := httpx.Decode(w, r, &req); err != nil {
		return err
	}

	phone, err := auth.Normalise(req.Phone)
	if err != nil {
		// An unparseable number cannot match any account, and saying so would
		// leak the same information as a user-exists probe.
		return invalidCredentials(err)
	}

	u, hash, err := s.users.ByPhone(r.Context(), phone)
	if errors.Is(err, users.ErrNotFound) {
		// Verify against a dummy hash anyway, so an unknown number costs the
		// same time as a known one with a wrong password.
		_, _ = auth.VerifyPassword(req.Password, s.dummyHash)
		return invalidCredentials(err)
	}
	if err != nil {
		return err
	}

	ok, err := auth.VerifyPassword(req.Password, hash)
	if err != nil {
		// A malformed stored hash is our bug, not the user's. Log it loudly
		// but tell the client only that the login failed.
		httpx.Logger(r.Context()).Error("stored password hash is unusable",
			"user_id", u.ID, "err", err)
		return invalidCredentials(err)
	}
	if !ok {
		return invalidCredentials(errors.New("password mismatch"))
	}

	if !u.IsActive {
		return httpx.Errorf(http.StatusForbidden, "account_suspended",
			"This account has been suspended. Contact support.")
	}

	// A successful login clears any lingering force-logout marker, so the user
	// is not locked out by their own earlier logout-all.
	if err := s.tokens.AllowAgain(r.Context(), u.ID); err != nil {
		return err
	}

	tokens, err := s.tokens.Issue(r.Context(), u.ID, u.IsAdmin)
	if err != nil {
		return err
	}

	httpx.Logger(r.Context()).Info("login", "user_id", u.ID)
	httpx.JSON(w, http.StatusOK, s.session(tokens, u))
	return nil
}

func (s *Server) refresh(w http.ResponseWriter, r *http.Request) error {
	var req refreshReq
	if err := httpx.Decode(w, r, &req); err != nil {
		return err
	}
	if req.Refresh == "" {
		return httpx.ErrBadRequest("A refresh token is required.")
	}

	// lookupAdmin reads the flag from the database so the new access token
	// reflects current privileges rather than replaying the expiring token's
	// claim. It also refuses a deactivated account.
	tokens, err := s.tokens.Rotate(r.Context(), req.Refresh, s.users.IsAdmin)
	if err != nil {
		switch {
		case errors.Is(err, auth.ErrTokenReused):
			return httpx.Errorf(http.StatusUnauthorized, "token_reused",
				"Your session has expired. Please sign in again.").WithCause(err)
		case errors.Is(err, auth.ErrUserSuspended):
			return httpx.Errorf(http.StatusForbidden, "account_suspended",
				"This account has been suspended. Contact support.").WithCause(err)
		case errors.Is(err, users.ErrNotFound):
			return httpx.Errorf(http.StatusUnauthorized, "token_reused",
				"Your session has expired. Please sign in again.").WithCause(err)
		}
		return err
	}

	httpx.JSON(w, http.StatusOK, map[string]string{
		"access":             tokens.Access,
		"refresh":            tokens.Refresh,
		"access_expires_at":  tokens.AccessExpiresAt.UTC().Format("2006-01-02T15:04:05Z"),
		"refresh_expires_at": tokens.RefreshExpiresAt.UTC().Format("2006-01-02T15:04:05Z"),
	})
	return nil
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) error {
	var req refreshReq
	if err := httpx.Decode(w, r, &req); err != nil {
		return err
	}
	// Logout is idempotent: revoking an already-dead token is a success, not
	// an error. A client that retries must not see a failure.
	if req.Refresh != "" {
		if err := s.tokens.Revoke(r.Context(), req.Refresh); err != nil {
			return err
		}
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// logoutAll invalidates every session for the caller, including access tokens
// that have not yet expired.
func (s *Server) logoutAll(w http.ResponseWriter, r *http.Request) error {
	uid := httpx.UserID(r.Context())
	if err := s.tokens.RevokeAll(r.Context(), uid); err != nil {
		return err
	}
	httpx.Logger(r.Context()).Info("logout-all", "user_id", uid)
	w.WriteHeader(http.StatusNoContent)
	return nil
}
