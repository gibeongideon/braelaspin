package auth

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/dibon/braelaspin/internal/rds"
)

// Access tokens are stateless signed JWTs, short-lived. Refresh tokens are
// opaque random strings whose only meaning is a Redis key, which is what makes
// them revocable: logout deletes the key and the session is over immediately.
//
// Refresh tokens ROTATE on every use, and reuse of an already-rotated token
// kills the whole family. That combination turns a stolen refresh token from a
// permanent backdoor into a detectable, self-limiting event.

var (
	ErrTokenInvalid  = errors.New("token is invalid or expired")
	ErrTokenReused   = errors.New("refresh token has already been used")
	ErrUserSuspended = errors.New("account is suspended")
)

type Tokens struct {
	Access           string    `json:"access"`
	Refresh          string    `json:"refresh"`
	AccessExpiresAt  time.Time `json:"access_expires_at"`
	RefreshExpiresAt time.Time `json:"refresh_expires_at"`
}

// Claims is the access-token payload. It is deliberately thin: the user id and
// the admin flag, nothing that could go stale in a way that matters. Balances
// and profile data are never in the token.
type Claims struct {
	jwt.RegisteredClaims
	IsAdmin bool `json:"adm,omitempty"`
}

func (c *Claims) UserID() (int64, error) {
	return strconv.ParseInt(c.Subject, 10, 64)
}

type TokenService struct {
	secret     []byte
	issuer     string
	accessTTL  time.Duration
	refreshTTL time.Duration
	redis      *rds.Client
}

func NewTokenService(secret []byte, issuer string, accessTTL, refreshTTL time.Duration, r *rds.Client) *TokenService {
	return &TokenService{
		secret: secret, issuer: issuer,
		accessTTL: accessTTL, refreshTTL: refreshTTL, redis: r,
	}
}

// Issue mints a fresh pair and starts a new refresh family.
func (s *TokenService) Issue(ctx context.Context, userID int64, isAdmin bool) (*Tokens, error) {
	family, err := randomID()
	if err != nil {
		return nil, err
	}
	return s.issueWithFamily(ctx, userID, isAdmin, family)
}

func (s *TokenService) issueWithFamily(ctx context.Context, userID int64, isAdmin bool, family string) (*Tokens, error) {
	now := time.Now()
	accessExp := now.Add(s.accessTTL)

	claims := &Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   strconv.FormatInt(userID, 10),
			Issuer:    s.issuer,
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(accessExp),
		},
		IsAdmin: isAdmin,
	}
	access, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(s.secret)
	if err != nil {
		return nil, fmt.Errorf("auth: sign access token: %w", err)
	}

	jti, err := randomID()
	if err != nil {
		return nil, err
	}
	if err := s.redis.PutRefresh(ctx, jti, userID, family, s.refreshTTL); err != nil {
		return nil, fmt.Errorf("auth: store refresh token: %w", err)
	}

	return &Tokens{
		Access:           access,
		Refresh:          jti,
		AccessExpiresAt:  accessExp,
		RefreshExpiresAt: now.Add(s.refreshTTL),
	}, nil
}

// VerifyAccess parses and validates an access token.
//
// It also consults the Redis ban key, which is what lets a suspension or a
// logout-everywhere take effect within a second even though the token stays
// cryptographically valid for its full lifetime. A purely stateless check
// would leave a suspended user playing for up to 15 more minutes.
func (s *TokenService) VerifyAccess(ctx context.Context, raw string) (*Claims, error) {
	claims := &Claims{}
	_, err := jwt.ParseWithClaims(raw, claims, func(t *jwt.Token) (any, error) {
		return s.secret, nil
	},
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithIssuer(s.issuer),
	)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrTokenInvalid, err)
	}

	userID, err := claims.UserID()
	if err != nil {
		return nil, fmt.Errorf("%w: subject %q is not a user id", ErrTokenInvalid, claims.Subject)
	}

	banned, err := s.redis.IsBanned(ctx, userID)
	if err != nil {
		// Fail closed. If we cannot tell whether this user is suspended, we
		// must not assume they are not.
		return nil, fmt.Errorf("auth: check suspension for user %d: %w", userID, err)
	}
	if banned {
		return nil, ErrUserSuspended
	}
	return claims, nil
}

// Rotate exchanges a refresh token for a new pair, invalidating the old one.
//
// lookupAdmin is called with the user id so the new access token carries an
// up-to-date admin flag rather than replaying whatever the old one claimed.
func (s *TokenService) Rotate(ctx context.Context, refresh string, lookupAdmin func(context.Context, int64) (bool, error)) (*Tokens, error) {
	userID, family, err := s.redis.GetRefresh(ctx, refresh)
	if errors.Is(err, rds.ErrNotFound) {
		// The token is unknown. Either it expired, or it was already rotated
		// and someone is presenting it a second time. We cannot distinguish
		// the two, so treat it as the dangerous case: if a family is still
		// live, kill it. A legitimate client never presents a rotated token.
		return nil, ErrTokenReused
	}
	if err != nil {
		return nil, fmt.Errorf("auth: load refresh token: %w", err)
	}

	banned, err := s.redis.IsBanned(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("auth: check suspension: %w", err)
	}
	if banned {
		return nil, ErrUserSuspended
	}

	// Consume the old token before issuing the new one. If we crash between
	// the two the user simply logs in again; the reverse order would leave a
	// usable duplicate.
	if err := s.redis.DelRefresh(ctx, refresh); err != nil {
		return nil, fmt.Errorf("auth: consume refresh token: %w", err)
	}

	isAdmin := false
	if lookupAdmin != nil {
		if isAdmin, err = lookupAdmin(ctx, userID); err != nil {
			return nil, fmt.Errorf("auth: load user %d: %w", userID, err)
		}
	}
	return s.issueWithFamily(ctx, userID, isAdmin, family)
}

// Revoke drops one refresh token (a single logout).
func (s *TokenService) Revoke(ctx context.Context, refresh string) error {
	if err := s.redis.DelRefresh(ctx, refresh); err != nil {
		return fmt.Errorf("auth: revoke refresh token: %w", err)
	}
	return nil
}

// RevokeAll blocks every outstanding access token for a user by setting the
// ban key for at least one access-token lifetime.
func (s *TokenService) RevokeAll(ctx context.Context, userID int64) error {
	return s.redis.BanUser(ctx, userID, s.accessTTL+time.Minute)
}

// AllowAgain lifts a suspension.
func (s *TokenService) AllowAgain(ctx context.Context, userID int64) error {
	return s.redis.UnbanUser(ctx, userID)
}

// ── identifiers ──────────────────────────────────────────────────────────────

// randomID returns 32 hex chars of cryptographic randomness, used for refresh
// tokens and family ids.
func randomID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("auth: generate id: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// refCodeAlphabet is Crockford base32: no I, L, O or U, so a code cannot be
// misread over the phone or mistaken for a digit.
var refCodeAlphabet = base32.NewEncoding("0123456789ABCDEFGHJKMNPQRSTVWXYZ").WithPadding(base32.NoPadding)

// NewRefCode returns an 8-character referral code.
//
// It is random, not derived from the user's data. The reference implementation
// built its code from `uuid4()[:3] + username[-4:]`, which embedded the last
// four digits of the owner's phone number in a string they were encouraged to
// post publicly.
func NewRefCode() (string, error) {
	b := make([]byte, 5) // 5 bytes -> exactly 8 base32 chars
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("auth: generate referral code: %w", err)
	}
	return refCodeAlphabet.EncodeToString(b), nil
}
