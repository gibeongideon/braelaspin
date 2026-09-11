// Package rds wraps Redis for the four things we actually use it for:
// refresh tokens, rate limits, idempotency, and the Daraja access token.
//
// Nothing that must survive a restart lives here. Note the deliberate absence
// of a "game state" concept: a spin resolves atomically inside one Postgres
// transaction, so there is no multi-step in-flight state to park. The
// idempotency key is the nearest honest equivalent.
package rds

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

type Client struct {
	r *redis.Client
}

func Open(ctx context.Context, url string, poolSize int) (*Client, error) {
	opt, err := redis.ParseURL(url)
	if err != nil {
		return nil, fmt.Errorf("parse REDIS_URL: %w", err)
	}
	opt.PoolSize = poolSize
	opt.ReadTimeout = 3 * time.Second
	opt.WriteTimeout = 3 * time.Second

	c := redis.NewClient(opt)
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := c.Ping(pingCtx).Err(); err != nil {
		_ = c.Close()
		return nil, fmt.Errorf("ping redis: %w", err)
	}
	return &Client{r: c}, nil
}

func (c *Client) Close() error                { return c.r.Close() }
func (c *Client) Raw() *redis.Client          { return c.r }
func (c *Client) Ping(ctx context.Context) error { return c.r.Ping(ctx).Err() }

// ── refresh tokens ───────────────────────────────────────────────────────────
// Key: rt:{jti} -> "{userID}|{familyID}". Deleting the key revokes the token,
// which is what makes logout instant despite a stateless access JWT.

func rtKey(jti string) string { return "rt:" + jti }

func (c *Client) PutRefresh(ctx context.Context, jti string, userID int64, family string, ttl time.Duration) error {
	return c.r.Set(ctx, rtKey(jti), fmt.Sprintf("%d|%s", userID, family), ttl).Err()
}

var ErrNotFound = errors.New("rds: key not found")

func (c *Client) GetRefresh(ctx context.Context, jti string) (userID int64, family string, err error) {
	v, err := c.r.Get(ctx, rtKey(jti)).Result()
	if errors.Is(err, redis.Nil) {
		return 0, "", ErrNotFound
	}
	if err != nil {
		return 0, "", err
	}
	if _, err := fmt.Sscanf(v, "%d|%s", &userID, &family); err != nil {
		return 0, "", fmt.Errorf("rds: malformed refresh value %q: %w", v, err)
	}
	return userID, family, nil
}

func (c *Client) DelRefresh(ctx context.Context, jti string) error {
	return c.r.Del(ctx, rtKey(jti)).Err()
}

// ConsumeRefresh atomically reads AND deletes a refresh token, so exactly one
// caller can ever redeem it.
//
// This must not be a Get followed by a Del. With rotating refresh tokens, N
// concurrent refreshes would then all read the same live token, all delete it
// (deletes being idempotent), and all issue new pairs — the same lost-update
// shape the wallet code takes such care to avoid. GETDEL is one round trip and
// one atomic operation, so the losers see ErrNotFound and are correctly told
// the token was reused.
func (c *Client) ConsumeRefresh(ctx context.Context, jti string) (userID int64, family string, err error) {
	v, err := c.r.GetDel(ctx, rtKey(jti)).Result()
	if errors.Is(err, redis.Nil) {
		return 0, "", ErrNotFound
	}
	if err != nil {
		return 0, "", err
	}
	if _, err := fmt.Sscanf(v, "%d|%s", &userID, &family); err != nil {
		return 0, "", fmt.Errorf("rds: malformed refresh value %q: %w", v, err)
	}
	return userID, family, nil
}

// ── user bans / force-logout ─────────────────────────────────────────────────
// Set when a user is suspended or logs out everywhere. The auth middleware
// checks it, so a suspension takes effect within a second even though the
// access token stays cryptographically valid for its full 15 minutes.

func (c *Client) BanUser(ctx context.Context, userID int64, ttl time.Duration) error {
	return c.r.Set(ctx, fmt.Sprintf("uban:%d", userID), "1", ttl).Err()
}

func (c *Client) IsBanned(ctx context.Context, userID int64) (bool, error) {
	n, err := c.r.Exists(ctx, fmt.Sprintf("uban:%d", userID)).Result()
	return n > 0, err
}

func (c *Client) UnbanUser(ctx context.Context, userID int64) error {
	return c.r.Del(ctx, fmt.Sprintf("uban:%d", userID)).Err()
}

// ── rate limiting ────────────────────────────────────────────────────────────

// Allow implements a fixed-window counter. INCR then EXPIRE only on the first
// hit, in one round trip. A fixed window can admit up to 2x the limit across a
// boundary; for login throttling and spin flood control that is fine, and it
// costs one key instead of a sorted set per subject.
func (c *Client) Allow(ctx context.Context, action, subject string, limit int, window time.Duration) (ok bool, retryAfter time.Duration, err error) {
	key := fmt.Sprintf("rl:%s:%s", action, subject)

	pipe := c.r.TxPipeline()
	incr := pipe.Incr(ctx, key)
	// ExpireNX sets the TTL only if the key has none, so the window is fixed
	// from the first hit. A plain Expire here would slide the window forward on
	// every request, and a legitimately busy caller (spin is 60/min) would stay
	// locked out for as long as it kept trying.
	pipe.ExpireNX(ctx, key, window)
	if _, err := pipe.Exec(ctx); err != nil {
		return false, 0, err
	}

	n := incr.Val()
	if n == 1 {
		return true, 0, nil
	}
	if n > int64(limit) {
		ttl, err := c.r.TTL(ctx, key).Result()
		if err != nil || ttl < 0 {
			ttl = window
		}
		return false, ttl, nil
	}
	return true, 0, nil
}

// ── idempotency ──────────────────────────────────────────────────────────────
// Every money-moving POST carries a client_ref. Claim wins the right to do the
// work; a loser reads the stored response and returns it byte-identically, so
// a client retry after a network timeout can never double-charge.

func idemKey(userID int64, clientRef string) string {
	return fmt.Sprintf("idem:%d:%s", userID, clientRef)
}

// Claim reports whether the caller may proceed. If it returns false, prior
// holds the stored response — which may be empty if the original attempt is
// still in flight, in which case the caller should tell the client to retry.
func (c *Client) Claim(ctx context.Context, userID int64, clientRef string, ttl time.Duration) (won bool, prior []byte, err error) {
	key := idemKey(userID, clientRef)
	ok, err := c.r.SetNX(ctx, key, "", ttl).Result()
	if err != nil {
		return false, nil, err
	}
	if ok {
		return true, nil, nil
	}
	v, err := c.r.Get(ctx, key).Bytes()
	if errors.Is(err, redis.Nil) {
		return false, nil, nil
	}
	return false, v, err
}

// Store records the response for a claimed key so retries can replay it.
func (c *Client) Store(ctx context.Context, userID int64, clientRef string, resp []byte, ttl time.Duration) error {
	return c.r.Set(ctx, idemKey(userID, clientRef), resp, ttl).Err()
}

// Release drops a claim whose work failed, so the client can genuinely retry
// rather than replaying an empty success.
func (c *Client) Release(ctx context.Context, userID int64, clientRef string) error {
	return c.r.Del(ctx, idemKey(userID, clientRef)).Err()
}

// ── Daraja access token ──────────────────────────────────────────────────────
// Cached with a TTL of expires_in minus a safety margin. The SetNX lock elects
// one refresher so that token expiry does not stampede Safaricom with N
// concurrent oauth calls.

func (c *Client) GetDarajaToken(ctx context.Context, kind string) (string, error) {
	v, err := c.r.Get(ctx, "daraja:"+kind).Result()
	if errors.Is(err, redis.Nil) {
		return "", ErrNotFound
	}
	return v, err
}

func (c *Client) PutDarajaToken(ctx context.Context, kind, token string, ttl time.Duration) error {
	if ttl <= 0 {
		return nil // already expired; don't cache a dead token
	}
	return c.r.Set(ctx, "daraja:"+kind, token, ttl).Err()
}

func (c *Client) DelDarajaToken(ctx context.Context, kind string) error {
	return c.r.Del(ctx, "daraja:"+kind).Err()
}

// LockDaraja tries to become the one goroutine that refreshes this token.
func (c *Client) LockDaraja(ctx context.Context, kind string) (bool, error) {
	return c.r.SetNX(ctx, "daraja:lock:"+kind, "1", 10*time.Second).Result()
}

// ── push fan-out ─────────────────────────────────────────────────────────────
// Any replica publishes; whichever one holds the user's WebSocket delivers.

func (c *Client) PublishUser(ctx context.Context, userID int64, payload []byte) error {
	return c.r.Publish(ctx, fmt.Sprintf("bs:ev:user:%d", userID), payload).Err()
}

func (c *Client) SubscribeUsers(ctx context.Context) *redis.PubSub {
	return c.r.PSubscribe(ctx, "bs:ev:user:*")
}
