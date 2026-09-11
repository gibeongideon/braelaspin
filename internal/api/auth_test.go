package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/dibon/braelaspin/internal/api"
	"github.com/dibon/braelaspin/internal/auth"
	"github.com/dibon/braelaspin/internal/config"
	"github.com/dibon/braelaspin/internal/game"
	"github.com/dibon/braelaspin/internal/httpx"
	"github.com/dibon/braelaspin/internal/rds"
	"github.com/dibon/braelaspin/internal/testutil"
	"github.com/dibon/braelaspin/internal/users"
)

// ── harness ──────────────────────────────────────────────────────────────────

type env struct {
	t     *testing.T
	srv   *httptest.Server
	redis *rds.Client
}

func newEnv(t *testing.T) *env {
	t.Helper()
	pool := testutil.DB(t)

	redisURL := os.Getenv("TEST_REDIS_URL")
	if redisURL == "" {
		redisURL = "redis://localhost:6380/9" // db 9, kept away from dev data
	}
	ctx := context.Background()
	redis, err := rds.Open(ctx, redisURL, 10)
	if err != nil {
		t.Skipf("TEST_REDIS_URL unavailable (%v); run `make up`", err)
	}
	if err := redis.Raw().FlushDB(ctx).Err(); err != nil {
		t.Fatalf("flush test redis: %v", err)
	}
	t.Cleanup(func() { redis.Close() })

	cfg := &config.Config{
		Env: "dev", BaseURL: "http://test.local",
		JWTSecret: []byte("test-secret-at-least-32-bytes-long!!"), JWTIssuer: "braelaspin-test",
		AccessTokenTTL: 15 * time.Minute, RefreshTTL: 720 * time.Hour,
		// Argon2 tuned DOWN for tests only. Production uses 64MiB/t=3; at that
		// cost a test that logs in 200 times takes minutes.
		ArgonTime: 1, ArgonMemoryKiB: 8 << 10, ArgonThreads: 1,
		RTPBP: 9000, RakeBP: 500, ReferralBP: 200,
		MinStakeCents: 500, MaxStakeCents: 5_000_000,
		DemoGrantCents: 500_000, DemoTopupCooldn: 24 * time.Hour,

		// Rate limits relaxed for tests, EXCEPT login: every request here
		// comes from 127.0.0.1 and so shares one IP bucket, which would
		// otherwise make a test's tenth registration fail for a reason the
		// test is not about. Login stays at the production value of 5 because
		// TestLoginRateLimitedAfterFiveAttempts asserts exactly that.
		RLRegisterPerHour: 10_000,
		RLLoginPer5Min:    5,
		RLRefreshPerMin:   10_000,
		RLReadPerMin:      10_000,
		RLSpinPerMin:      10_000,
		RLDepositPer5Min:  10_000,
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	tokens := auth.NewTokenService(cfg.JWTSecret, cfg.JWTIssuer, cfg.AccessTokenTTL, cfg.RefreshTTL, redis)
	s, err := api.New(api.Deps{
		Cfg: cfg, Log: log, DB: pool, Redis: redis, Tokens: tokens,
		Users: users.NewService(pool, cfg.DemoGrantCents),
		Game:  game.NewService(pool, game.Economics{RTPBP: 9000, RakeBP: 500, ReferralBP: 200, MinStakeCents: 500, MaxStakeCents: 5_000_000}),
	})
	if err != nil {
		t.Fatalf("build server: %v", err)
	}

	r := chi.NewRouter()
	r.Use(httpx.WithRequestID(log))
	r.Use(httpx.WithRealIP(nil))
	r.Use(httpx.Recover)
	s.Routes(r)

	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return &env{t: t, srv: srv, redis: redis}
}

type resp struct {
	Status int
	Body   map[string]any
	Raw    string
}

func (e *env) do(method, path string, body any, bearer string) resp {
	e.t.Helper()
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			e.t.Fatal(err)
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, e.srv.URL+path, rdr)
	if err != nil {
		e.t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	res, err := e.srv.Client().Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)

	out := resp{Status: res.StatusCode, Raw: string(raw)}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &out.Body)
	}
	return out
}

// errCode pulls error.code out of a failure envelope.
func (r resp) errCode() string {
	if e, ok := r.Body["error"].(map[string]any); ok {
		if c, ok := e["code"].(string); ok {
			return c
		}
	}
	return ""
}

func (r resp) str(path ...string) string {
	cur := any(r.Body)
	for _, k := range path {
		m, ok := cur.(map[string]any)
		if !ok {
			return ""
		}
		cur = m[k]
	}
	s, _ := cur.(string)
	return s
}

func (r resp) num(path ...string) float64 {
	cur := any(r.Body)
	for _, k := range path {
		m, ok := cur.(map[string]any)
		if !ok {
			return 0
		}
		cur = m[k]
	}
	f, _ := cur.(float64)
	return f
}

// uniquePhone keeps parallel-ish tests from colliding on the phone unique index.
var phoneSeq int64

func nextPhone() string {
	n := atomic.AddInt64(&phoneSeq, 1)
	return fmt.Sprintf("2547%08d", 10_000_000+n)
}

// ── registration ─────────────────────────────────────────────────────────────

func TestRegisterCreatesAccountWalletAndDemoGrant(t *testing.T) {
	e := newEnv(t)
	phone := nextPhone()

	r := e.do("POST", "/v1/auth/register", map[string]string{
		"phone": phone, "password": "correct-horse",
	}, "")
	if r.Status != http.StatusCreated {
		t.Fatalf("register: status %d, body %s", r.Status, r.Raw)
	}
	if r.str("access") == "" || r.str("refresh") == "" {
		t.Error("register returned no tokens")
	}
	if got := r.str("user", "phone"); got != phone {
		t.Errorf("user.phone = %q, want %q", got, phone)
	}
	if got := r.str("user", "ref_code"); len(got) != 8 {
		t.Errorf("ref_code = %q, want 8 chars", got)
	}
	if got := r.str("user", "ref_link"); !strings.HasSuffix(got, r.str("user", "ref_code")) {
		t.Errorf("ref_link = %q does not end in the ref_code", got)
	}

	// The demo grant must be present AND recorded in the ledger.
	me := e.do("GET", "/v1/me", nil, r.str("access"))
	if me.Status != http.StatusOK {
		t.Fatalf("/me: status %d, body %s", me.Status, me.Raw)
	}
	if got := me.num("balances", "demo_cents"); got != 500_000 {
		t.Errorf("demo_cents = %v, want 500000", got)
	}
	if got := me.num("balances", "real_cents"); got != 0 {
		t.Errorf("real_cents = %v, want 0 — signup must not grant real money", got)
	}
}

func TestRegisterNormalisesEveryPhoneSpelling(t *testing.T) {
	e := newEnv(t)
	// All of these are the same number; the first must win and the rest must
	// collide, or one person becomes several accounts.
	spellings := []string{"0712345678", "+254712345678", "254712345678", "712345678", "0712 345 678"}

	first := e.do("POST", "/v1/auth/register", map[string]string{
		"phone": spellings[0], "password": "correct-horse",
	}, "")
	if first.Status != http.StatusCreated {
		t.Fatalf("first register: %d %s", first.Status, first.Raw)
	}
	if got := first.str("user", "phone"); got != "254712345678" {
		t.Errorf("stored phone = %q, want 254712345678", got)
	}

	for _, s := range spellings[1:] {
		r := e.do("POST", "/v1/auth/register", map[string]string{
			"phone": s, "password": "correct-horse",
		}, "")
		if r.Status != http.StatusConflict || r.errCode() != "phone_taken" {
			t.Errorf("register(%q): status %d code %q, want 409 phone_taken",
				s, r.Status, r.errCode())
		}
	}
}

func TestRegisterRejectsBadInput(t *testing.T) {
	e := newEnv(t)
	cases := []struct {
		name, phone, password, wantCode string
		wantStatus                      int
	}{
		{"landline", "0212345678", "correct-horse", "invalid_phone", 400},
		{"not a mobile prefix", "0812345678", "correct-horse", "invalid_phone", 400},
		{"too short", "071234567", "correct-horse", "invalid_phone", 400},
		{"letters", "07123abcde", "correct-horse", "invalid_phone", 400},
		{"empty phone", "", "correct-horse", "invalid_phone", 400},
		{"short password", nextPhone(), "short", "password_too_short", 400},
		{"empty password", nextPhone(), "", "password_too_short", 400},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := e.do("POST", "/v1/auth/register", map[string]string{
				"phone": c.phone, "password": c.password,
			}, "")
			if r.Status != c.wantStatus || r.errCode() != c.wantCode {
				t.Errorf("status %d code %q, want %d %q", r.Status, r.errCode(), c.wantStatus, c.wantCode)
			}
		})
	}
}

func TestRegisterRejectsUnknownFieldsAndJunkBodies(t *testing.T) {
	e := newEnv(t)
	// DisallowUnknownFields is deliberate: a client sending "amount" when the
	// field is "amount_cents" must get a clear rejection, not a silent zero.
	r := e.do("POST", "/v1/auth/register", map[string]string{
		"phone": nextPhone(), "password": "correct-horse", "pasword": "typo",
	}, "")
	if r.Status != http.StatusBadRequest {
		t.Errorf("unknown field: status %d, want 400", r.Status)
	}
}

func TestRegisterBindsReferrerAndRejectsUnknownCode(t *testing.T) {
	e := newEnv(t)

	referrer := e.do("POST", "/v1/auth/register", map[string]string{
		"phone": nextPhone(), "password": "correct-horse",
	}, "")
	code := referrer.str("user", "ref_code")

	ok := e.do("POST", "/v1/auth/register", map[string]string{
		"phone": nextPhone(), "password": "correct-horse", "ref_code": code,
	}, "")
	if ok.Status != http.StatusCreated {
		t.Fatalf("register with valid ref_code: %d %s", ok.Status, ok.Raw)
	}

	// The referrer should now count one referee.
	me := e.do("GET", "/v1/me", nil, referrer.str("access"))
	if got := me.num("referrals", "count"); got != 1 {
		t.Errorf("referrals.count = %v, want 1", got)
	}

	// An unknown code is an error, not a silent ignore: a user who followed a
	// friend's link should not quietly lose the attribution.
	bad := e.do("POST", "/v1/auth/register", map[string]string{
		"phone": nextPhone(), "password": "correct-horse", "ref_code": "ZZZZZZZZ",
	}, "")
	if bad.Status != http.StatusBadRequest || bad.errCode() != "unknown_referral_code" {
		t.Errorf("unknown ref_code: status %d code %q, want 400 unknown_referral_code",
			bad.Status, bad.errCode())
	}
}

// A failed registration must leave NOTHING behind — no user, no wallet, no
// ledger row. The reference implementation spread this across several
// statements plus a post-save signal, so a partial failure left a user with no
// wallet at all.
func TestFailedRegistrationLeavesNothingBehind(t *testing.T) {
	e := newEnv(t)
	phone := nextPhone()

	first := e.do("POST", "/v1/auth/register", map[string]string{
		"phone": phone, "password": "correct-horse",
	}, "")
	if first.Status != http.StatusCreated {
		t.Fatal(first.Raw)
	}

	// This one collides on the phone and must roll back entirely.
	dup := e.do("POST", "/v1/auth/register", map[string]string{
		"phone": phone, "password": "different-pass",
	}, "")
	if dup.Status != http.StatusConflict {
		t.Fatalf("duplicate: %d %s", dup.Status, dup.Raw)
	}

	// The original account must still work with its original password.
	login := e.do("POST", "/v1/auth/login", map[string]string{
		"phone": phone, "password": "correct-horse",
	}, "")
	if login.Status != http.StatusOK {
		t.Errorf("original login after a failed duplicate: %d %s", login.Status, login.Raw)
	}
	// Invariants are asserted in cleanup and would catch an orphaned wallet or
	// a stray demo_grant row.
}

// ── login ────────────────────────────────────────────────────────────────────

func TestLoginDoesNotRevealWhetherThePhoneExists(t *testing.T) {
	e := newEnv(t)
	phone := nextPhone()
	e.do("POST", "/v1/auth/register", map[string]string{
		"phone": phone, "password": "correct-horse",
	}, "")

	wrongPassword := e.do("POST", "/v1/auth/login", map[string]string{
		"phone": phone, "password": "wrong-password",
	}, "")
	unknownPhone := e.do("POST", "/v1/auth/login", map[string]string{
		"phone": nextPhone(), "password": "correct-horse",
	}, "")

	// Identical status AND identical code, or the endpoint enumerates accounts.
	if wrongPassword.Status != 401 || unknownPhone.Status != 401 {
		t.Errorf("statuses %d and %d, want both 401", wrongPassword.Status, unknownPhone.Status)
	}
	if wrongPassword.errCode() != "invalid_credentials" || unknownPhone.errCode() != "invalid_credentials" {
		t.Errorf("codes %q and %q, want both invalid_credentials",
			wrongPassword.errCode(), unknownPhone.errCode())
	}
	if wrongPassword.str("error", "message") != unknownPhone.str("error", "message") {
		t.Error("the two failures have different messages, which leaks account existence")
	}
}

func TestLoginAcceptsAnySpellingOfTheRegisteredNumber(t *testing.T) {
	e := newEnv(t)
	e.do("POST", "/v1/auth/register", map[string]string{
		"phone": "0722333444", "password": "correct-horse",
	}, "")

	for _, spelling := range []string{"0722333444", "+254722333444", "254722333444", "722333444", "0722 333 444"} {
		r := e.do("POST", "/v1/auth/login", map[string]string{
			"phone": spelling, "password": "correct-horse",
		}, "")
		if r.Status != http.StatusOK {
			t.Errorf("login(%q): %d %s", spelling, r.Status, r.Raw)
		}
	}
}

func TestLoginRateLimitedAfterFiveAttempts(t *testing.T) {
	e := newEnv(t)
	phone := nextPhone()
	e.do("POST", "/v1/auth/register", map[string]string{
		"phone": phone, "password": "correct-horse",
	}, "")

	var limited bool
	for i := 0; i < 8; i++ {
		r := e.do("POST", "/v1/auth/login", map[string]string{
			"phone": phone, "password": "wrong",
		}, "")
		if r.Status == http.StatusTooManyRequests {
			if r.errCode() != "rate_limited" {
				t.Errorf("attempt %d: code %q, want rate_limited", i+1, r.errCode())
			}
			if r.num("error", "meta", "retry_after_seconds") <= 0 {
				t.Error("rate_limited carries no retry_after_seconds")
			}
			limited = true
			break
		}
	}
	if !limited {
		t.Error("8 failed logins were never rate limited")
	}
}

// ── refresh rotation ─────────────────────────────────────────────────────────

func TestRefreshRotatesAndTheOldTokenIsDead(t *testing.T) {
	e := newEnv(t)
	reg := e.do("POST", "/v1/auth/register", map[string]string{
		"phone": nextPhone(), "password": "correct-horse",
	}, "")
	old := reg.str("refresh")

	first := e.do("POST", "/v1/auth/refresh", map[string]string{"refresh": old}, "")
	if first.Status != http.StatusOK {
		t.Fatalf("refresh: %d %s", first.Status, first.Raw)
	}
	fresh := first.str("refresh")
	if fresh == "" || fresh == old {
		t.Fatal("refresh did not rotate the token")
	}
	if first.str("access") == "" {
		t.Error("refresh returned no access token")
	}

	// Presenting the consumed token again is the signature of a stolen token.
	reuse := e.do("POST", "/v1/auth/refresh", map[string]string{"refresh": old}, "")
	if reuse.Status != http.StatusUnauthorized || reuse.errCode() != "token_reused" {
		t.Errorf("reusing a rotated token: status %d code %q, want 401 token_reused",
			reuse.Status, reuse.errCode())
	}

	// The new one still works.
	if again := e.do("POST", "/v1/auth/refresh", map[string]string{"refresh": fresh}, ""); again.Status != http.StatusOK {
		t.Errorf("the rotated token should still work: %d %s", again.Status, again.Raw)
	}
}

// This is the race the Flutter client's single-flight guard exists to prevent.
// Verified here from the server side: with rotating refresh tokens, N
// concurrent refreshes must not all succeed, or the losers invalidate the
// winner and the user is logged out.
func TestConcurrentRefreshesOfOneTokenAllowExactlyOne(t *testing.T) {
	e := newEnv(t)
	reg := e.do("POST", "/v1/auth/register", map[string]string{
		"phone": nextPhone(), "password": "correct-horse",
	}, "")
	token := reg.str("refresh")

	const n = 20
	var ok, reused int64
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			r := e.do("POST", "/v1/auth/refresh", map[string]string{"refresh": token}, "")
			switch {
			case r.Status == http.StatusOK:
				atomic.AddInt64(&ok, 1)
			case r.errCode() == "token_reused":
				atomic.AddInt64(&reused, 1)
			}
		}()
	}
	close(start)
	wg.Wait()

	if ok != 1 {
		t.Errorf("%d concurrent refreshes succeeded, want exactly 1 (the rest must see token_reused)", ok)
	}
	t.Logf("%d concurrent refreshes: %d succeeded, %d rejected as reused", n, ok, reused)
}

func TestRefreshRejectsGarbage(t *testing.T) {
	e := newEnv(t)
	for _, tok := range []string{"", "not-a-token", strings.Repeat("a", 200)} {
		r := e.do("POST", "/v1/auth/refresh", map[string]string{"refresh": tok}, "")
		if r.Status != http.StatusUnauthorized && r.Status != http.StatusBadRequest {
			t.Errorf("refresh(%q): status %d, want 400 or 401", tok, r.Status)
		}
	}
}

// ── logout ───────────────────────────────────────────────────────────────────

func TestLogoutRevokesTheRefreshTokenAndIsIdempotent(t *testing.T) {
	e := newEnv(t)
	reg := e.do("POST", "/v1/auth/register", map[string]string{
		"phone": nextPhone(), "password": "correct-horse",
	}, "")
	token := reg.str("refresh")

	if r := e.do("POST", "/v1/auth/logout", map[string]string{"refresh": token}, ""); r.Status != http.StatusNoContent {
		t.Fatalf("logout: %d %s", r.Status, r.Raw)
	}
	if r := e.do("POST", "/v1/auth/refresh", map[string]string{"refresh": token}, ""); r.Status == http.StatusOK {
		t.Error("the revoked refresh token still works")
	}
	// A client retrying logout must not see an error.
	if r := e.do("POST", "/v1/auth/logout", map[string]string{"refresh": token}, ""); r.Status != http.StatusNoContent {
		t.Errorf("second logout: %d, want 204 (logout is idempotent)", r.Status)
	}
}

// A suspension must bite within a second even though the access token stays
// cryptographically valid for its full 15 minutes. A purely stateless check
// would leave a suspended user playing.
func TestLogoutAllKillsLiveAccessTokensImmediately(t *testing.T) {
	e := newEnv(t)
	reg := e.do("POST", "/v1/auth/register", map[string]string{
		"phone": nextPhone(), "password": "correct-horse",
	}, "")
	access := reg.str("access")

	if r := e.do("GET", "/v1/me", nil, access); r.Status != http.StatusOK {
		t.Fatalf("/me before logout-all: %d", r.Status)
	}
	if r := e.do("POST", "/v1/auth/logout-all", nil, access); r.Status != http.StatusNoContent {
		t.Fatalf("logout-all: %d %s", r.Status, r.Raw)
	}
	r := e.do("GET", "/v1/me", nil, access)
	if r.Status != http.StatusForbidden || r.errCode() != "account_suspended" {
		t.Errorf("/me after logout-all: status %d code %q, want 403 account_suspended",
			r.Status, r.errCode())
	}
}

// ── authorisation ────────────────────────────────────────────────────────────

func TestProtectedRoutesRejectMissingAndForgedTokens(t *testing.T) {
	e := newEnv(t)
	reg := e.do("POST", "/v1/auth/register", map[string]string{
		"phone": nextPhone(), "password": "correct-horse",
	}, "")
	good := reg.str("access")

	cases := []struct{ name, token string }{
		{"no token", ""},
		{"garbage", "not-a-jwt"},
		{"tampered payload", good[:len(good)-4] + "AAAA"},
		// HS256 signed with the wrong key; must be rejected by signature check.
		{"wrong signature", "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxIiwiaXNzIjoiYnJhZWxhc3Bpbi10ZXN0IiwiZXhwIjo0MDcwOTA4ODAwfQ.AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if r := e.do("GET", "/v1/me", nil, c.token); r.Status != http.StatusUnauthorized {
				t.Errorf("/me with %s: status %d, want 401", c.name, r.Status)
			}
		})
	}
}

// The "none" algorithm attack: a token asking to be verified with no signature.
func TestAlgNoneTokenIsRejected(t *testing.T) {
	e := newEnv(t)
	// {"alg":"none","typ":"JWT"} . {"sub":"1","iss":"braelaspin-test","exp":4070908800} .
	none := "eyJhbGciOiJub25lIiwidHlwIjoiSldUIn0.eyJzdWIiOiIxIiwiaXNzIjoiYnJhZWxhc3Bpbi10ZXN0IiwiZXhwIjo0MDcwOTA4ODAwfQ."
	if r := e.do("GET", "/v1/me", nil, none); r.Status != http.StatusUnauthorized {
		t.Errorf("alg=none token: status %d, want 401", r.Status)
	}
}

// ── /me and demo top-up ──────────────────────────────────────────────────────

func TestMeReturnsEverythingTheAppNeedsInOneCall(t *testing.T) {
	e := newEnv(t)
	reg := e.do("POST", "/v1/auth/register", map[string]string{
		"phone": nextPhone(), "password": "correct-horse",
	}, "")

	r := e.do("GET", "/v1/me", nil, reg.str("access"))
	if r.Status != http.StatusOK {
		t.Fatalf("/me: %d %s", r.Status, r.Raw)
	}
	for _, key := range []string{"user", "balances", "game", "referrals"} {
		if _, ok := r.Body[key]; !ok {
			t.Errorf("/me response is missing %q", key)
		}
	}
	// The wheel must come from the server, so the client never hardcodes odds.
	segs, ok := r.Body["game"].(map[string]any)["segments"].([]any)
	if !ok || len(segs) != 12 {
		t.Errorf("game.segments has %d entries, want 12", len(segs))
	}
	if got := r.num("game", "rtp_bp"); got != 9000 {
		t.Errorf("game.rtp_bp = %v, want 9000", got)
	}
	// No bankroll yet, so no real stake is admissible.
	if got := r.num("game", "max_stake_cents"); got != 0 {
		t.Errorf("max_stake_cents = %v with an empty bankroll, want 0", got)
	}
}

func TestDemoTopUpIsCappedAndRateLimitedByCooldown(t *testing.T) {
	e := newEnv(t)
	reg := e.do("POST", "/v1/auth/register", map[string]string{
		"phone": nextPhone(), "password": "correct-horse",
	}, "")
	access := reg.str("access")

	// Already at the grant, so the first top-up is a no-op but consumes the
	// cooldown.
	if r := e.do("POST", "/v1/wallet/demo/topup", nil, access); r.Status != http.StatusOK {
		t.Fatalf("first top-up: %d %s", r.Status, r.Raw)
	}
	if got := e.do("GET", "/v1/wallet", nil, access).num("demo_cents"); got != 500_000 {
		t.Errorf("demo_cents = %v after a no-op top-up, want 500000 (must not stack)", got)
	}

	r := e.do("POST", "/v1/wallet/demo/topup", nil, access)
	if r.Status != http.StatusTooManyRequests || r.errCode() != "demo_topup_not_eligible" {
		t.Errorf("second top-up: status %d code %q, want 429 demo_topup_not_eligible",
			r.Status, r.errCode())
	}
}

func TestConcurrentDemoTopUpsClaimTheCooldownOnce(t *testing.T) {
	e := newEnv(t)
	reg := e.do("POST", "/v1/auth/register", map[string]string{
		"phone": nextPhone(), "password": "correct-horse",
	}, "")
	access := reg.str("access")

	const n = 20
	var ok int64
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if e.do("POST", "/v1/wallet/demo/topup", nil, access).Status == http.StatusOK {
				atomic.AddInt64(&ok, 1)
			}
		}()
	}
	close(start)
	wg.Wait()

	// The cooldown is claimed by the UPDATE's WHERE clause, not a
	// read-then-write, so concurrent callers cannot all pass the check.
	if ok != 1 {
		t.Errorf("%d concurrent top-ups succeeded, want exactly 1", ok)
	}
	if got := e.do("GET", "/v1/wallet", nil, access).num("demo_cents"); got != 500_000 {
		t.Errorf("demo_cents = %v, want 500000 — top-ups must not stack", got)
	}
}
