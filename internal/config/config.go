// Package config loads every runtime setting from the environment exactly once.
//
// The contract that matters: Load reports EVERY missing or malformed key in a
// single error. A bad deploy fails in the first 50ms with a complete list, not
// one key at a time and not at the first M-Pesa call three hours later.
// No secret has a default.
package config

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	// core
	Env               string
	HTTPAddr          string
	BaseURL           string
	LogLevel          string
	LogFormat         string
	TrustedProxyCIDRs []*net.IPNet

	// postgres
	DatabaseURL   string
	DBMaxConns    int32
	DBMinConns    int32
	MigrateOnBoot bool

	// redis
	RedisURL      string
	RedisPoolSize int

	// auth
	JWTSecret      []byte
	JWTIssuer      string
	AccessTokenTTL time.Duration
	RefreshTTL     time.Duration
	ArgonTime      uint32
	ArgonMemoryKiB uint32
	ArgonThreads   uint8

	// game economics (basis points; 10000 = 100%)
	RTPBP           int
	RakeBP          int
	ReferralBP      int
	MinStakeCents   int64
	MaxStakeCents   int64
	DemoGrantCents  int64
	DemoTopupCooldn time.Duration

	// withdrawals
	WithdrawFeeCents     int64
	MinWithdrawCents     int64
	MaxWithdrawCents     int64
	AutoApproveCeilCents int64

	// m-pesa shared
	MpesaEnv            string
	MpesaBaseURL        string
	CallbackSecret      string
	CallbackCIDRs       []*net.IPNet
	CallbackIPEnforce   bool
	VerifyThreshCents   int64
	MpesaDialShortcode  string
	MpesaManualTill     string

	// m-pesa c2b (STK push)
	C2BKey      string
	C2BSecret   string
	C2BShortode string
	C2BPasskey  string
	C2BTxnType  string

	// m-pesa b2c (payouts)
	B2CKey          string
	B2CSecret       string
	B2CShortcode    string
	B2CInitiator    string
	B2CInitiatorPwd string
	B2CCertPath     string
	B2CCommandID    string

	// workers
	WorkerEnabled bool
}

// loader accumulates problems instead of returning on the first one.
type loader struct {
	probs []string
}

func (l *loader) bad(key, why string) {
	l.probs = append(l.probs, fmt.Sprintf("%s: %s", key, why))
}

// req returns a required string. Empty is a problem — this is what secrets use.
func (l *loader) req(key string) string {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		l.bad(key, "required but not set")
	}
	return v
}

func (l *loader) str(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

func (l *loader) oneOf(key, def string, allowed ...string) string {
	v := l.str(key, def)
	for _, a := range allowed {
		if v == a {
			return v
		}
	}
	l.bad(key, fmt.Sprintf("must be one of %s, got %q", strings.Join(allowed, "|"), v))
	return def
}

func (l *loader) i64(key string, def int64) int64 {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return def
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		l.bad(key, fmt.Sprintf("must be an integer, got %q", raw))
		return def
	}
	return n
}

func (l *loader) intn(key string, def int) int { return int(l.i64(key, int64(def))) }

func (l *loader) boolean(key string, def bool) bool {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return def
	}
	b, err := strconv.ParseBool(raw)
	if err != nil {
		l.bad(key, fmt.Sprintf("must be true or false, got %q", raw))
		return def
	}
	return b
}

func (l *loader) dur(key string, def time.Duration) time.Duration {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return def
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		l.bad(key, fmt.Sprintf("must be a duration like 15m or 720h, got %q", raw))
		return def
	}
	if d <= 0 {
		l.bad(key, "must be positive")
		return def
	}
	return d
}

// bp reads a basis-point value and holds it to 0..10000.
func (l *loader) bp(key string, def int) int {
	n := l.intn(key, def)
	if n < 0 || n > 10000 {
		l.bad(key, fmt.Sprintf("must be 0..10000 basis points, got %d", n))
		return def
	}
	return n
}

func (l *loader) cidrs(key, def string) []*net.IPNet {
	raw := l.str(key, def)
	if raw == "" {
		return nil
	}
	var out []*net.IPNet
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		_, n, err := net.ParseCIDR(part)
		if err != nil {
			l.bad(key, fmt.Sprintf("%q is not a CIDR block", part))
			continue
		}
		out = append(out, n)
	}
	return out
}

func (l *loader) urlish(key, def string) string {
	v := l.str(key, def)
	if v == "" {
		l.bad(key, "required but not set")
		return v
	}
	u, err := url.Parse(v)
	if err != nil || u.Scheme == "" || u.Host == "" {
		l.bad(key, fmt.Sprintf("must be an absolute URL, got %q", v))
	}
	return v
}

// Load reads the environment and returns a validated Config, or an error
// naming every problem found.
func Load() (*Config, error) {
	l := &loader{}
	c := &Config{}

	// ── core ───────────────────────────────────────────────────────────────
	c.Env = l.oneOf("APP_ENV", "dev", "dev", "staging", "prod")
	c.HTTPAddr = l.str("HTTP_ADDR", ":8080")
	c.BaseURL = l.urlish("BASE_URL", "http://localhost:8080")
	c.LogLevel = l.oneOf("LOG_LEVEL", "info", "debug", "info", "warn", "error")
	c.LogFormat = l.oneOf("LOG_FORMAT", "json", "json", "text")
	c.TrustedProxyCIDRs = l.cidrs("TRUSTED_PROXY_CIDRS", "127.0.0.1/32,::1/128")

	// ── postgres ───────────────────────────────────────────────────────────
	c.DatabaseURL = l.req("DATABASE_URL")
	c.DBMaxConns = int32(l.intn("DB_MAX_CONNS", 20))
	c.DBMinConns = int32(l.intn("DB_MIN_CONNS", 2))
	c.MigrateOnBoot = l.boolean("MIGRATE_ON_BOOT", false)

	// ── redis ──────────────────────────────────────────────────────────────
	c.RedisURL = l.req("REDIS_URL")
	c.RedisPoolSize = l.intn("REDIS_POOL_SIZE", 20)

	// ── auth ───────────────────────────────────────────────────────────────
	c.JWTSecret = []byte(l.req("JWT_SECRET"))
	c.JWTIssuer = l.str("JWT_ISSUER", "braelaspin")
	c.AccessTokenTTL = l.dur("ACCESS_TOKEN_TTL", 15*time.Minute)
	c.RefreshTTL = l.dur("REFRESH_TOKEN_TTL", 720*time.Hour)
	c.ArgonTime = uint32(l.intn("ARGON2_TIME", 3))
	c.ArgonMemoryKiB = uint32(l.intn("ARGON2_MEMORY_KIB", 65536))
	c.ArgonThreads = uint8(l.intn("ARGON2_PARALLELISM", 2))

	// ── game economics ─────────────────────────────────────────────────────
	c.RTPBP = l.bp("RTP_BP", 9000)
	c.RakeBP = l.bp("RAKE_BP", 500)
	c.ReferralBP = l.bp("REFERRAL_BP", 200)
	c.MinStakeCents = l.i64("MIN_STAKE_CENTS", 500)
	c.MaxStakeCents = l.i64("MAX_STAKE_CENTS", 5_000_000)
	c.DemoGrantCents = l.i64("DEMO_GRANT_CENTS", 500_000)
	c.DemoTopupCooldn = l.dur("DEMO_TOPUP_COOLDOWN", 24*time.Hour)

	// ── withdrawals ────────────────────────────────────────────────────────
	c.WithdrawFeeCents = l.i64("WITHDRAW_FEE_CENTS", 3_000)
	c.MinWithdrawCents = l.i64("MIN_WITHDRAW_CENTS", 10_000)
	c.MaxWithdrawCents = l.i64("MAX_WITHDRAW_CENTS", 7_000_000)
	c.AutoApproveCeilCents = l.i64("AUTO_APPROVE_CEILING_CENTS", 0)

	// ── m-pesa ─────────────────────────────────────────────────────────────
	c.MpesaEnv = l.oneOf("MPESA_ENV", "sandbox", "sandbox", "production")
	c.MpesaBaseURL = l.urlish("MPESA_BASE_URL", "https://sandbox.safaricom.co.ke")
	c.CallbackSecret = l.req("MPESA_CALLBACK_SECRET")
	// Safaricom's published egress ranges. Verify against your own logs before enforcing.
	c.CallbackCIDRs = l.cidrs("MPESA_CALLBACK_ALLOWED_CIDRS",
		"196.201.214.0/24,196.201.213.0/24,196.201.212.0/24")
	c.CallbackIPEnforce = l.boolean("MPESA_CALLBACK_IP_ENFORCE", false)
	c.VerifyThreshCents = l.i64("MPESA_VERIFY_THRESHOLD_CENTS", 500_000)
	c.MpesaDialShortcode = l.str("MPESA_DIAL_SHORTCODE", "*334#")
	c.MpesaManualTill = l.str("MPESA_MANUAL_TILL", "")

	c.C2BKey = l.req("MPESA_C2B_CONSUMER_KEY")
	c.C2BSecret = l.req("MPESA_C2B_CONSUMER_SECRET")
	c.C2BShortode = l.req("MPESA_C2B_SHORTCODE")
	c.C2BPasskey = l.req("MPESA_C2B_PASSKEY")
	c.C2BTxnType = l.oneOf("MPESA_C2B_TRANSACTION_TYPE", "CustomerPayBillOnline",
		"CustomerPayBillOnline", "CustomerBuyGoodsOnline")

	c.B2CKey = l.req("MPESA_B2C_CONSUMER_KEY")
	c.B2CSecret = l.req("MPESA_B2C_CONSUMER_SECRET")
	c.B2CShortcode = l.req("MPESA_B2C_SHORTCODE")
	c.B2CInitiator = l.req("MPESA_B2C_INITIATOR_NAME")
	c.B2CInitiatorPwd = l.req("MPESA_B2C_INITIATOR_PASSWORD")
	c.B2CCertPath = l.req("MPESA_B2C_CERT_PATH")
	c.B2CCommandID = l.oneOf("MPESA_B2C_COMMAND_ID", "BusinessPayment",
		"BusinessPayment", "SalaryPayment", "PromotionPayment")

	c.WorkerEnabled = l.boolean("WORKER_ENABLED", true)

	// ── cross-field checks ─────────────────────────────────────────────────
	if c.MinStakeCents <= 0 {
		l.bad("MIN_STAKE_CENTS", "must be positive")
	}
	if c.MaxStakeCents < c.MinStakeCents {
		l.bad("MAX_STAKE_CENTS", "must be >= MIN_STAKE_CENTS")
	}
	if c.MinWithdrawCents <= c.WithdrawFeeCents {
		l.bad("MIN_WITHDRAW_CENTS", "must exceed WITHDRAW_FEE_CENTS, or a withdrawal pays out nothing")
	}
	if c.MaxWithdrawCents < c.MinWithdrawCents {
		l.bad("MAX_WITHDRAW_CENTS", "must be >= MIN_WITHDRAW_CENTS")
	}
	// The edge must cover what we pay out of it. RTP 9000 leaves 1000bp of edge;
	// rake + referral must fit inside that or the house loses money on every spin.
	if edge := 10000 - c.RTPBP; c.RakeBP+c.ReferralBP > edge {
		l.bad("RAKE_BP+REFERRAL_BP", fmt.Sprintf(
			"total %dbp exceeds the %dbp house edge implied by RTP_BP=%d",
			c.RakeBP+c.ReferralBP, edge, c.RTPBP))
	}
	if c.DBMinConns > c.DBMaxConns {
		l.bad("DB_MIN_CONNS", "must be <= DB_MAX_CONNS")
	}

	// ── production-only assertions ─────────────────────────────────────────
	if c.Env == "prod" {
		if len(c.JWTSecret) < 32 {
			l.bad("JWT_SECRET", "must be at least 32 bytes in production")
		}
		if c.MpesaEnv != "production" || strings.Contains(c.MpesaBaseURL, "sandbox") {
			l.bad("MPESA_BASE_URL", "points at sandbox while APP_ENV=prod")
		}
		if c.MigrateOnBoot {
			l.bad("MIGRATE_ON_BOOT", "must be false in production; run `braelaspin migrate up` as a deploy step")
		}
		if strings.Contains(c.DatabaseURL, "sslmode=disable") {
			l.bad("DATABASE_URL", "sslmode=disable is not allowed in production")
		}
		if !c.CallbackIPEnforce {
			l.bad("MPESA_CALLBACK_IP_ENFORCE", "must be true in production once the allowlist is confirmed")
		}
		if strings.HasPrefix(c.BaseURL, "http://") {
			l.bad("BASE_URL", "must be https in production")
		}
	}

	if len(l.probs) > 0 {
		return nil, fmt.Errorf("invalid configuration (%d problem(s)):\n  - %s",
			len(l.probs), strings.Join(l.probs, "\n  - "))
	}
	return c, nil
}

// IsProd reports whether this is the production environment.
func (c *Config) IsProd() bool { return c.Env == "prod" }

// CallbackURL builds a webhook URL carrying the shared secret in its path.
func (c *Config) CallbackURL(channel string) string {
	return strings.TrimRight(c.BaseURL, "/") + "/webhooks/mpesa/" + channel + "/" + c.CallbackSecret
}
