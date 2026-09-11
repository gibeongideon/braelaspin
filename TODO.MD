# braelaspin — TODO

> Task-level status. See **PLAN.MD** for *why* any of this is shaped the way it
> is — especially §3 (the three money bugs) before touching wallet or game code.
>
> `[x]` done & verified · `[~]` partially done · `[ ]` not started · `[!]` blocked
>
> Last updated: 2026-09-11

---

## Progress at a glance

| Phase | | Status |
|---|---|---|
| M0 | Toolchain & skeleton | **✅ complete** |
| M1 | Money foundation | **✅ complete** |
| M2 | Auth | **✅ complete** |
| **W1** | **Web scaffold (TypeScript)** | **✅ complete** |
| **W2** | **Web polish + payout accuracy** | **✅ complete** (deposit/withdraw still blocked on M6/M7) |
| **W3** | **Production deploy + CI/CD** | **✅ live** at https://braelaspin.dafeapp.com |
| **M3** | **Wheel widget (Flutter)** | **✅ complete** |
| **M4** | **Spin engine + Flutter app** | **✅ complete** — full app, all screens |
| M5 | Real money, referrals, history | ⬜ not started |
| M6 | M-Pesa deposits | 🚫 blocked on Daraja credentials |
| M7 | M-Pesa withdrawals | 🚫 blocked on Daraja credentials |
| M8 | Admin surface | ⬜ not started |
| M9 | WebSocket push | ⬜ not started |
| M10 | Hardening & release APK | ⬜ not started |

**Written so far:** ~6,500 lines of Go, ~3,000 of TypeScript, 248 of SQL.
**98 tests passing** (61 Go, 37 TypeScript), clean under `go vet`, the race
detector, and the core-purity check. Live at https://braelaspin.dafeapp.com.

---

## ✅ M0 — Toolchain & skeleton (COMPLETE)

### Toolchain — all installed under `$HOME`, no sudo
- [x] Go 1.27.1 → `~/.local/go`
- [x] JDK 17 (Temurin) → `~/.local/jdk17`
- [x] Android SDK **36** + build-tools 36.0.0 + platform-tools → `~/Android/Sdk`
- [x] Flutter 3.47.3 stable (Dart 3.13.3) → `~/.local/flutter`
- [x] `flutter doctor` green for Android (Linux-desktop toolchain missing — irrelevant, Android-only target)
- [x] Reclaimed 7 GB from regenerable caches only (npm, playwright, pip, unused Docker images). `~/anaconda3` (in active use), `~/Downloads` and `~/snap` deliberately untouched.

### Scaffolding
- [x] `go.mod` — 7 direct deps: pgx, go-redis, chi, jwt, x/crypto, goose, (coder/websocket pending M9)
- [x] `internal/config` — reports **every** bad key in one error; no secret has a default
- [x] Production-mode assertions (sandbox URL, `MIGRATE_ON_BOOT`, `sslmode=disable`, IP enforcement, http BASE_URL)
- [x] `internal/db` — pgx pool + `//go:embed` migrations via goose
- [x] `AssertCurrent` — server refuses to start against a schema older than the binary
- [x] `internal/rds` — 5 key patterns (refresh, ban, rate limit, idempotency, Daraja token) + pubsub helpers
- [x] `internal/httpx` — router, error envelope, middleware (requestID, realIP, recover, log, auth, admin, ratelimit, timeout)
- [x] `cmd/api` — `serve | migrate | healthcheck | version`
- [x] `_ "time/tzdata"` embedded (mandatory: the STK password depends on `Africa/Nairobi`)
- [x] slog with redaction hook (phones masked, secrets `[redacted]`, receipts kept)
- [x] `deploy/docker-compose.yml` — Postgres 5433, Redis 6380 (non-default, avoids the host's own)
- [x] Redis `--maxmemory-policy noeviction` (evicting a refresh token logs a user out; evicting an idempotency key can double-charge)
- [x] `Makefile` (15 targets), `.env.example` (every setting documented), `.gitignore`

### Verified
- [x] Config fail-fast, missing keys → lists all 3 at once
- [x] Config fail-fast, `APP_ENV=prod` → catches all 5 unsafe settings
- [x] `make up` → both containers healthy
- [x] `migrate up` applies, `migrate status` reports, re-run is idempotent
- [x] Server boots; wheel validated at startup (`segments=12 rtp_bp=9000`)
- [x] `/healthz` → 200 with **zero** dependencies
- [x] `/readyz` → 200, and **503 with Redis stopped while `/healthz` stays 200**
- [x] `X-Request-Id` generated and echoed
- [x] Graceful shutdown on SIGTERM ("stopped cleanly")

---

## ✅ M1 — Money foundation (COMPLETE)

### Schema — `internal/db/migrations/0001_init.sql` (248 lines)
- [x] 9 tables: users, wallets, transactions, spins, deposits, withdrawals, house, mpesa_callbacks, admin_audit
- [x] Money as `bigint` cents throughout — no floats anywhere
- [x] `users.phone` CHECK `^254[17][0-9]{8}$`; `ref_code` 6–10 upper-alnum; `referred_by <> id`
- [x] `wallets` — 3 balance columns (vs winzangu's 8), all `>= 0`
- [x] `transactions` append-only via `DO INSTEAD NOTHING` rules on UPDATE **and** DELETE
- [x] `tx_realm_kinds` CHECK — demo and real money cannot mix (invariant I3 as a constraint)
- [x] `tx_referral_once` — a spin pays its referrer at most once
- [x] `spins_payout_exact` CHECK — payout must equal `stake × mult / 10000`
- [x] `spins_demo_no_rake` CHECK
- [x] `dep_receipt_key` UNIQUE — the hard deposit-idempotency floor (winzangu used a racy `.count()`)
- [x] `wd_one_open` partial UNIQUE — one open withdrawal per user, enforced by the **database**
- [x] `house.bankroll_cents >= 0` — the Bug-2 backstop
- [x] `cb_dedupe` on callback body hash

### `internal/wallet` — the only code that moves money (329 + 196 lines)
- [x] `Move` — signed delta only; **no parameter for an absolute balance** (this is the Bug-2 fix)
- [x] `Lock` — always `ORDER BY user_id`, so two-wallet ops can't deadlock
- [x] `HouseLock` / `MoveHouse` — relative deltas, CHECK backstop
- [x] `Hold` / `ReleaseHold` / `ReverseHold` — the withdrawal hold (the Bug-3 fix)
- [x] Typed errors: `ErrInsufficientFunds`, `ErrBankrollExhausted`
- [x] Invariants I1, I2, I3, I4, I5, I5b, I6, I7, I8 as SQL
- [x] `AssertInvariant` wired into every integration test's `t.Cleanup`
- [x] `internal/testutil` — real-Postgres harness; **test funding goes through the ledger**, so there is no back door for creating money even in tests

### Verified — 10/10 integration tests against real Postgres
- [x] credit/debit updates balance and writes the ledger row
- [x] overdraw → `ErrInsufficientFunds`, **whole transaction rolls back**, balance untouched
- [x] zero-delta ledger row rejected
- [x] **200 goroutines on a balance affording one debit → exactly 1 succeeded, 199 rejected, final balance 0** ← the lost-update regression test
- [x] **100 goroutines against a 10-payout bankroll → exactly 10 succeeded, bankroll exactly 0, never negative** ← the skipped-debit regression test
- [x] ledger append-only holds against direct `UPDATE` and `DELETE`
- [x] hold → over-hold refused → reverse makes the player whole, `held_cents` cleared
- [x] settled hold releases **without** crediting the player
- [x] 100 two-wallet locks in **opposing** orders → no deadlock, money conserved
- [x] demo and real balances move independently
- [x] whole money path clean under `-race`

---

## ✅ M2 — Auth (COMPLETE)

### Services
- [x] `auth.Normalise` — every Kenyan spelling → `254XXXXXXXXX`; **rejects** unparseable input rather than storing it corrupted (winzangu appended `"-need_update"` and stored it anyway, which caused duplicate-account creation on deposit)
- [x] `auth.Mask` / `auth.Pretty`
- [x] argon2id in PHC format (parameters travel with the hash), constant-time verify
- [x] Password policy: 8–128 chars
- [x] JWT access tokens (HS256, 15 min), thin claims (`sub` + `adm`)
- [x] Opaque refresh tokens in Redis, **rotating**, reuse → kill the family
- [x] Redis ban key checked on every request, so a suspension takes effect within a second despite a live 15-min token
- [x] `VerifyAccess` **fails closed** if Redis is unreachable (authorisation, not abuse control)
- [x] `NewRefCode` — 8 chars, Crockford base32 (no I/L/O/U), **random not derived** (winzangu embedded the owner's last 4 phone digits in a publicly-shared code)
- [x] Tests: full Kenyan format matrix, all rejection cases, idempotency, all-spellings-collapse-to-one-identity, mask, pretty
- [x] `FuzzNormalise` — ~492k executions, no panic, no output the schema would reject

### Repository — `internal/users`
- [x] `Create` — user + wallet + demo grant + referral binding in **ONE transaction**
- [x] The demo grant goes through `wallet.Move`, so there is no back door for creating balance, not even at signup
- [x] An unknown referral code is an **error**, not a silent ignore — a user who followed a friend's link should not quietly lose the attribution
- [x] Referral-code collision retried (5 attempts) rather than failing the registration
- [x] `TopUpDemo` — cooldown claimed by the UPDATE's `WHERE`, not a read-then-write, and it tops up **to** the target so repeated claims cannot stack

### HTTP layer — `internal/api`
> **Structural change from the original plan:** handlers live in `internal/api`
> rather than `auth/handlers.go`. This keeps the service packages free of
> `net/http` and lets `GET /me` aggregate across users + wallet + game without
> those packages depending on one another.

- [x] `POST /v1/auth/register` — 201 with tokens + user
- [x] `POST /v1/auth/login` — one error for every failure mode, plus a dummy argon2 verify on unknown phones so latency is not an account-existence oracle
- [x] `POST /v1/auth/refresh` — rotating, `adm` flag re-read from the DB
- [x] `POST /v1/auth/logout` — idempotent (revoking a dead token is a success)
- [x] `POST /v1/auth/logout-all` — kills live access tokens within a second
- [x] `GET /v1/me` — profile + balances + game config + referral stats in one call
- [x] `GET /v1/wallet`, `POST /v1/wallet/demo/topup`
- [x] Rate limits mounted and **moved into config** (`RL_*`), so thresholds change without a rebuild

### Verified — 20 integration tests
- [x] register creates account + wallet + exactly one `demo_grant`; **no real money at signup**
- [x] all 5 spellings of one number collapse to one identity; the rest get `409 phone_taken`
- [x] bad input rejected: landline, wrong prefix, too short, letters, empty, weak password
- [x] unknown JSON fields rejected (`DisallowUnknownFields`)
- [x] referrer bound and counted; unknown code → `400 unknown_referral_code`
- [x] **a failed registration leaves nothing behind** — the original account still logs in
- [x] login does not reveal whether a phone exists (same status, code **and message**)
- [x] login accepts every spelling of the registered number
- [x] 6 bad logins → `rate_limited` with `retry_after_seconds`
- [x] refresh rotates; the old token is dead; the new one works
- [x] **20 concurrent refreshes of one token → exactly 1 succeeds, 19 `token_reused`**
- [x] logout revokes and is idempotent; logout-all kills live access tokens
- [x] protected routes reject missing / garbage / tampered / wrong-signature tokens
- [x] **`alg=none` token rejected**
- [x] `/me` returns all 12 segments and `rtp_bp=9000` from the server (client never hardcodes odds)
- [x] `max_stake_cents = 0` with an empty bankroll
- [x] demo top-up capped and cooldown-limited; **20 concurrent top-ups → exactly 1 succeeds, balance does not stack**

### Bugs found and fixed during M2
- [x] **`Rotate` had a read-then-delete race** — 15 of 20 concurrent refreshes all succeeded on the same token. Fixed with Redis `GETDEL` (`rds.ConsumeRefresh`), which is atomic. This was the same lost-update shape the wallet code guards against, and the test caught it.
- [x] **Test isolation** — `go test ./...` runs packages in parallel against one database, so one package's `TRUNCATE` wiped another's fixtures mid-test (deadlocks, phantom "no wallet for user N"). Each test package now provisions its own Postgres database and its own Redis DB index, so `go test ./...` is correct however it is invoked rather than depending on `-p 1`.

---

## 🔨 M4 — Spin engine + app shell (engine done, API + UI pending)

### Done — `internal/game` (205 + 325 lines)
- [x] 12-segment wheel table, weights summing to 10000
- [x] `Validate` — fails the process if weights don't sum or RTP disagrees with config
- [x] `Pick` — `crypto/rand`, cumulative walk, 1-indexed wire format, **fails closed** on entropy error
- [x] `Payout` truncates identically to the Postgres CHECK
- [x] `MaxAffordableStake` = bankroll / 200
- [x] Spin transaction: lock wallets → lock house → **pre-draw bankroll veto** → draw → insert → move money
- [x] Bankroll influences **admission only**, never the weights (the Bug-1 fix)
- [x] Demo spins use the **identical** wheel and RNG, skip house/rake/referral
- [x] Commission paid only on real play, only to a real referrer, never to a fallback user
- [x] `FindByClientRef` — crash/timeout recovery path
- [x] `Config` — segments + currently admissible stake range

### Verified — 12/12 tests
- [x] weights sum to exactly 10000
- [x] **RTP is exactly 9000 bp**
- [x] `Validate` rejects: RTP mismatch, zero weight, negative weight, bad sum, negative multiplier
- [x] `MaxMultBP` = 2,000,000; KES 100k bankroll → KES 500 max stake
- [x] `Payout` rounding matches Postgres integer division (8 cases incl. remainders)
- [x] every cumulative boundary maps to the right segment
- [x] 1-indexed wire format
- [x] fails closed on entropy failure
- [x] **χ² = 13.28 vs critical 34.53 (11 df) over 2,000,000 draws**
- [x] **realised RTP 9024 bp vs 9000 configured over 2,000,000 simulated spins (within 4σ)**
- [x] house edge covers rake + referral

### To do
- [ ] `GET /v1/game/config` handler (10 s cache)
- [ ] `POST /v1/game/spin` handler + Redis idempotency claim/store/release
- [ ] `GET /v1/game/spins` (keyset pagination), `GET /v1/game/spins/{id}`
- [ ] Rate limit: spin 60/min per user
- [ ] Integration tests: 200 concurrent spins on a one-spin balance → exactly one; replayed `client_ref` returns the **identical** response; stake above `bankroll/200` → `stake_exceeds_bankroll` writing **zero** rows; force the bankroll CHECK mid-spin → whole spin rolls back and the stake is *not* taken
- [ ] Flutter: `flutter create --platforms=android`, strip `cupertino_icons`
- [ ] Flutter: `api.dart` (HttpClient, **single-flight** 401 refresh, idempotency keys)
- [ ] Flutter: `native.dart` MethodChannel (Keystore-encrypted refresh token, tick sound, share, dial, initial link)
- [ ] Flutter: `store.dart`, `auth.dart`, `gate.dart`, `spin.dart`
- [ ] **Test login on a physical Android 6 device** — settles whether Dart's engine-bundled TLS roots avoid the ISRG-Root-X1 breakage on older Tecno/Infinix/itel handsets

---

## ✅ W1 — Web scaffold, TypeScript (COMPLETE)

> Web ships first and sets the patterns; Flutter follows them. See PLAN.MD §7b
> for the layering rule and the web→Flutter equivalence table.

### Decoupling — the rule this phase exists to establish
- [x] `src/core/` is PURE: no DOM, no framework, no imports from `ui/`
- [x] `src/ui/` is the only code that touches the DOM
- [x] `src/main.ts` is the composition root — the only file that knows both
- [x] **`npm run check:core` enforces it mechanically** and fails the build on any `document`/`window`/`localStorage`/`ui/` import inside `core/`
- [x] Frontend and backend are separate deployables: API serves JSON only, web is static files

### Core (ports to Dart essentially line for line)
- [x] `types.ts` — the API contract, mirroring the Go DTOs
- [x] `signal.ts` — `Signal<T>`, a direct ValueNotifier equivalent
- [x] `api.ts` — **single-flight 401 refresh** (mandatory: refresh tokens rotate) + idempotency keys
- [x] `store.ts` — the eleven signals + every operation; a **timeout is treated as unknown, not failed**, and triggers recovery by `client_ref`
- [x] `money.ts` — cents formatting; `parseShillings` returns null rather than a silent 0
- [x] `phone.ts` — mirrors `internal/auth/phone.go`, so users see the number that will be used
- [x] `errors.ts` — every user-facing string in one file; server sends codes, not prose
- [x] `wheel.ts` — pure spin maths: `targetAngle`, `indicatedSegment`, `easeOutQuart`, `tickTimes`

### UI
- [x] `styles.css` — design tokens from the reference: warm near-black, amber gradient, tile ring
- [x] `wheel-view.ts` — canvas renderer; **face cached into an offscreen canvas**, one rotate+blit per frame
- [x] `Ticker` — WebAudio tick, no asset to ship, created on first interaction so autoplay rules allow it
- [x] Screens: auth · spin · wallet · history · refer · profile
- [x] Bottom nav with the raised centre action, as in the reference
- [x] `Scope` disposal so subscriptions don't pile up across navigations
- [x] Toast system with **actions** — the insufficient-funds toast offers "Deposit"

### Backend changes this required
- [x] `internal/httpx/cors.go` — exact-match allowlist, **no wildcard, no origin reflection**
- [x] `CORS_ORIGINS` config + a production assertion rejecting `http://` or localhost origins
- [x] `internal/api/game.go` — `POST /v1/game/spin` (with Redis idempotency claim/store/release), `GET /v1/game/config`, `/v1/game/spins`, `/v1/game/spins/by-ref/{ref}`, `/v1/history`
- [x] Keyset pagination (not OFFSET — it drifts on an append-only ledger and slows down as you page)

### Verified
- [x] `check:core` passes — core is provably DOM-free
- [x] 26 core unit tests green, including: **every segment lands correctly over 2,400 randomised trials**, rotation is monotonic across spins, never stops on a boundary, the full Kenyan phone matrix, and `parseShillings` rejecting junk
- [x] `tsc --noEmit` clean under `strict`
- [x] **Bundle: 44 KB raw, ~16 KB gzipped** (12.15 JS + 3.23 CSS + 0.46 HTML)
- [x] End-to-end against the live API: CORS preflight allowed for the web origin, **403 for an attacker origin**, register → config → 5 demo spins → idempotent replay returns the identical response → bankroll veto → history
- [x] `Dockerfile` (nginx static) + `nginx.conf` with CSP, immutable asset caching, no-cache `index.html`, SPA fallback for `/r/<code>`

### Known gaps
- [ ] Refresh token is in `localStorage` — readable by any script on the origin. Better: httpOnly cookie (needs a backend change + CSRF story) — **M10**
- [ ] Winners strip uses placeholder data — needs a real endpoint (**W2**)
- [ ] Deposit/withdraw screens are stubs pending Daraja credentials (**M6/M7**)
- [ ] No E2E browser test (Playwright) — core logic is unit-tested, screens are not

---

## ✅ W2 — Web polish + payout accuracy (COMPLETE)

### Payout accuracy — the priority of this phase
The rule, in the product owner's words: **"if the spinner hits 5, the win is
bet × multiplier"**. That identity is now proved at four independent layers.

- [x] `internal/game/math_test.go` — the definitive Go proof
  - [x] `payout == stake × multiplier` for every wheel multiplier × 8 stakes
  - [x] the brief's example pinned: KES 10 at 5× pays **KES 50**, net **KES 40**
  - [x] **1× is exactly break-even** — required, or the RTP table is meaningless
  - [x] 0× pays exactly 0
  - [x] every wheel multiplier divides exactly, so no rounding occurs in production
  - [x] truncation-toward-zero pinned for the fractional case, if one is ever added
  - [x] no int64 overflow at the ceiling (10¹³, **922,337× headroom**)
- [x] **Go ⇄ Postgres differential test** — both compute the payout and must agree
      bit for bit across **116 stake/multiplier combinations**. Not an assumption
      that two languages round alike; a proof.
- [x] **Full ledger accounting asserted for all 12 segments**: player, referrer,
      bankroll and rake each land on their exact expected value, and total money
      in the system is **conserved** before vs after
- [x] Demo spins use identical maths but move no real money, no house, no rake,
      no commission
- [x] Unreferred players pay commission to nobody (the reference implementation
      paid user id 1)
- [x] 400 real spins: rake is exactly 500 bp of turnover and the bankroll lands
      on `start + turnover − payouts − rake` **to the cent**
- [x] `web/src/core/outcome.ts` — the client **recomputes** the payout and throws
      `PayoutMismatchError` rather than rendering a number it cannot verify
- [x] Verified on the live deployment: **40 spins, 0 arithmetic errors**, tracked
      balance matched the server exactly

### The display bug this phase fixed
- [x] The old overlay showed the **gross payout** with "Added to your balance".
      At 1× that reads as a win while the balance does not move — which makes
      players think the maths is broken, and they would be right to. The result
      card now always shows **three** things: the multiplier, the arithmetic
      (`KES 10 bet × 5 = KES 50`), and **what the balance actually did**.
      1× is labelled "Your bet back — balance unchanged", not a win.

### Pointer alignment — a real bug found and fixed
- [x] **The pointer was off by exactly one segment on every single spin.** The
      renderer defined its own tile geometry (`i*step - 90 - step/2`), centring
      segment 1 on the pointer, while `indicatedSegment` treats wheel-local 0 as
      segment 1's *leading edge*. The two disagreed by half a segment, so a
      player winning 5× saw the pointer resting on 10×.
- [x] Why the existing tests missed it: they only checked `core/wheel.ts`
      against itself. The maths was perfectly self-consistent — **nothing
      checked the drawing against the maths.**
- [x] Fixed by moving tile geometry INTO `core/wheel.ts` as
      `segmentArcDeg()` / `boundaryAngleDeg()`, derived from the same formula
      that decides what the pointer reads. The renderer now consumes them and
      computes no angles of its own, so the two cannot drift again.
- [x] 6 regression tests, including one that models the drawn arcs and asserts
      they agree with `indicatedSegment` at **every half-degree over two full
      turns**, not only at landing points.

### The balance the player sees — now two-phase
- [x] Click spin → **the bet leaves the balance immediately** (`applyStakeDebit`),
      because the debit is already committed in the ledger by the time the
      server responds
- [x] Wheel stops → `settleSpin` applies the payout: a **win adds it**, a
      **loss leaves the balance unchanged** at the post-bet figure
- [x] Phase 1 uses the server's own numbers (`balance_cents − payout_cents`)
      rather than subtracting the stake from a possibly-stale local value
- [x] 6 tests covering loss / refund / win / 200× — phase 1 always equals
      `start − stake`, and phase 2 minus phase 1 is always exactly the payout
- [x] Verified live: loss `499000→499000 (+0)`, 2× `498000→500000 (+2000)`,
      1× refund `499000→500000 (+1000)`, 5× `+5000`

### Second math audit — adversarial pass
Hunting for what was *not* yet proved, rather than re-running what passed.
**Three real defects found and fixed**, all in guard rails rather than the
payout identity itself.

- [x] **Overflow could defeat admission control.** `MaxExposure` computed
      `stake × 2,000,000 / 10000` unguarded. Above ~4.6e12 cents that wraps
      NEGATIVE, and a negative exposure makes `bankroll < exposure` false — so
      a stake no bankroll could cover would be **admitted and paid**. Reachable
      only via a misplaced zero in `MAX_STAKE_CENTS`, but now fails closed
      (returns `MaxInt64`, vetoed by every finite bankroll) and
      `game.ValidateStakeCeiling` refuses such a config at startup.
- [x] **`MaxAffordableStake` could return a negative maximum** on a bankroll
      above ~9.2e14 cents, and separately could advertise a stake above the
      safe ceiling that `MaxExposure` then vetoes — `GET /game/config` would
      promise a bet the server refuses. Now divides before multiplying and
      clamps to `MaxSafeStake()`. **My own new test caught the second half of
      this.**
- [x] **`clampStake` treated 0 as "no limit".** `cfg.max_stake_cents || MAX_SAFE`
      disabled the clamp in exactly the two cases it exists for: an empty
      bankroll (real-money cap of zero) and an empty wallet. Replaced with an
      explicit `maxStake()` that the hint text and the clamp now share, so the
      number shown is the number the server would accept.
- [x] **Latent:** the schema pins `segment_index BETWEEN 1 AND 12`. Changing the
      wheel size would fail every insert *on a player's spin*; `game.Validate`
      now catches the mismatch at startup instead.

### Verified in this pass
- [x] `rake + referral ≤ house edge` — already asserted in config (500+200 ≤ 1000)
- [x] Admission control is **conservative**: it requires `200 × stake` while the
      true worst case needs `199 × stake + rake + commission`
- [x] **Per-spin truncation** pinned: `Σ floor(sᵢ·r/10000) ≠ floor(Σ sᵢ·r/10000)`.
      Awkward stakes (501, 733, 999, 2507…) gave per-spin rake 1093 vs bulk
      1098 — the 5-cent gap **stays in the bankroll**, so the books still balance
      exactly. Anyone reconciling rake against turnover must expect this.
- [x] **JS doubles vs exact integers**: `expectedPayout` brute-forced against
      BigInt across the full wheel, every awkward stake, **20,000 random pairs**,
      and every boundary within ±1 of a multiple of 10000. Zero divergence.
- [x] **Invariants run against LIVE PRODUCTION data** — 5 users, 103 spins,
      152 ledger rows: no balance drift (real or demo), nothing negative, demo
      and real never mixed, **all 103 spins exact**, every spin's bet and win
      present in the ledger, no double-paid commission, append-only rules
      (`tx_no_update`, `tx_no_delete`) still attached.

### Screens and polish
- [x] Spin history screen (`/v1/game/spins`) with `bet × mult = payout` on every
      row, so a doubtful player can check us by eye
- [x] Totals (spins / staked / returned) to reconcile against the balance
- [x] `GET /v1/game/winners` — **real** wins only, real-money only, phones masked;
      renders nothing rather than inventing names
- [x] Offline banner, retry blocks on list failures, loading skeletons
- [x] Invite moved to Profile when Spins took its tab slot

### Still blocked
- [ ] Deposit screen + STK waiting view — needs Daraja credentials (**M6**)
- [ ] Withdraw screen + pending receipt — needs Daraja credentials (**M7**)
- [ ] Playwright E2E: register → spin → history

---

## ✅ W3 — Production deploy + CI/CD (LIVE)

**https://braelaspin.dafeapp.com** · also http://68.183.91.240 · see **DEPLOY.MD**

- [x] Deployed to `trader@68.183.91.240` — Caddy + Postgres + Redis + systemd
- [x] **Same-origin topology**: Caddy serves the web app at `/` and proxies
      `/v1/*` to the API, so production has no CORS surface at all
- [x] Let's Encrypt TLS on the domain (issued in ~5 s), HTTP→HTTPS 308 redirect
- [x] Bare IP kept working on plain HTTP, from one shared Caddy snippet so the
      two sites cannot drift
- [x] `.github/workflows/deploy.yml` — push to `main` → test → build → deploy →
      smoke test → **assert MT5 still running**; auto-rollback on failed health
- [x] `deploy/deploy.sh` — same pipeline locally in ~20 s
- [x] Dedicated ed25519 deploy key, revocable independently; host key pinned
- [x] `index.html` served `no-cache`, hashed assets `immutable` for a year
- [ ] **Add the three GitHub secrets** (`DEPLOY_SSH_KEY`, `DEPLOY_HOST`,
      `DEPLOY_KNOWN_HOSTS`) and push — see DEPLOY.MD
- [ ] Backups: nightly `pg_dump -Fc` + WAL archiving, restore drill ending in the
      invariant suite
- [ ] `APP_ENV=prod` (requires https, `sslmode=require`, callback IP enforcement)

### MT5 protection (this host runs live trading)
- [x] System-scoped units only; MT5's are user-scoped — no namespace overlap
- [x] No Docker — its iptables/FORWARD changes are needless risk beside live
      trading, and the daemon costs ~100 MB this box cannot spare
- [x] `MemoryMax=384M`, `CPUWeight=50`, `OOMScoreAdjust=500` — under pressure the
      kernel kills the API, never a trade
- [x] No Go/Node on the server; it receives prebuilt artifacts
- [x] Bootstrap refuses to run if 80/443/8080 are taken
- [x] Every deploy asserts MT5 is alive — verified unchanged across all deploys

---

## ✅ M3 + M4 — The Flutter app (COMPLETE)

> The payoff for building web-first. `core/` ported to Dart essentially line
> for line, and the **same 38 assertions** now run on both clients.

### The port — `app/lib/core/`, zero pub dependencies
| Web | Flutter | |
|---|---|---|
| `types.ts` | `models.dart` | |
| `wheel.ts` | `spin_math.dart` | identical maths, identical tests |
| `store.ts` | `store.dart` | `Signal<T>` → `ValueNotifier<T>` |
| `outcome.ts` | `outcome.dart` | |
| `stakes.ts` | `stakes.dart` | |
| `celebration.ts` | `celebration.dart` | |
| `errors.ts` | `errors.dart` | every player-facing string, one file |
| `api.ts` | `api.dart` | `dart:io` HttpClient — no `package:http` |

- [x] **Zero runtime dependencies.** `dart:io` for HTTP, `dart:convert` for JSON,
      `ValueNotifier` for state, `CustomPainter` for the wheel. Every package
      costs bytes a Kenyan user pays for on a metered connection.
- [x] `uses-material-design: false` — no icon font, no Material theme
- [x] 38 Dart tests mirroring the TypeScript suite, all passing

### The wheel — `app/lib/ui/wheel/`
- [x] `wheel_painter.dart` — the static face recorded ONCE into a `ui.Picture`;
      each frame is **one canvas rotate plus one drawPicture**. Repainting 12
      arcs and 12 text layouts per frame is what makes wheels stutter on the
      hardware this targets.
- [x] Geometry comes from `core/spin_math.dart`, never computed in the painter —
      the exact mistake that made the web pointer off-by-one
- [x] Fixed light source: bezel and gloss painted AFTER the rotation, so
      highlights stay put while the wheel turns underneath
- [x] Labels flip in the lower half so none are upside down
- [x] Losing tiles alternate two dark shades so all 12 stay visible
- [x] Metallic pegs on the wheel, sweeping past a fixed flapper
- [x] Overshoot-and-settle, bounded so the pointer cannot leave the winning tile
- [x] `AnimatedBuilder` scoped to the canvas: a spin rebuilds **no widgets**
- [x] Reduced-motion honoured via `MediaQuery.disableAnimations`
- [x] Debug `assert` that the landed segment equals the server's

### Screens
- [x] Auth (live phone normalisation echo), Spin, Wallet, Results, History,
      Invite, Profile
- [x] Five-tab shell with the raised centre action, matching the web client
- [x] Stepper + chip ladder — **no text input**, so no keyboard covers the wheel
- [x] Two-phase balance: bet leaves on tap, payout lands when the wheel stops
- [x] Tiered result card: rays, counting number, confetti, rising chime
- [x] `refreshMe()` on app resume — the API is the source of truth
- [x] Haptics on tap, selection and win

### Native — `android/app/src/main/kotlin/`
- [x] `MainActivity.kt` — tones synthesised with `AudioTrack` (sine + decay
      envelope). No audio package, no asset files.
- [x] Ticks **throttled to ~14/sec**; the raw 24/sec peak sounds like noise
- [x] `SecureStore.kt` — refresh token in **AES-GCM encrypted SharedPreferences
      with an Android Keystore key**, written against platform APIs so the
      zero-dependency footprint holds. The access token is never persisted.
- [x] `minSdk 23`, R8 + `shrinkResources`, ProGuard keeps for the channels

---

## ⬜ M3-original — Wheel widget checklist (superseded, kept for reference)

- [ ] `wheel_spec.dart` — segment fills/labels; **multipliers come from the API**, never hardcoded
- [ ] `wheel_face.dart` — record the static face once into a `ui.Picture`
- [ ] `wheel_painter.dart` — per frame: rotate + `drawPicture` + pins (one transform, not 12 arcs + 12 text layouts)
- [ ] `wheel_view.dart` — `AnimationController`, `spinTo(index)`, tick cursor
- [ ] `spin_math.dart` — `targetAngleDeg`, `indicatedSegment`, `QuartOut` curve
- [ ] Keep winzangu's feel: 15 s, ~7 revolutions, per-pin tick (precompute crossing times, don't poll per frame)
- [ ] **Do NOT reset the angle to 0 before each spin** — `xdarius.js:199` does and it snaps visibly; solve the congruence from where the wheel rests
- [ ] `CustomPainter(repaint: controller)` — zero widget rebuilds during a spin
- [ ] Block tab switching and system-back while spinning (`PopScope`)
- [ ] Debug harness: "spin to segment N"
- [ ] Verify: 200 consecutive spins land on the requested index (`indicatedSegment(final) == seg`)
- [ ] Verify: <8 ms/frame in `--profile` **on the cheapest target device**, not a flagship
- [ ] Verify: ticks don't buzz past 4 SoundPool streams

---

## ⬜ M5 — Real money, referrals, history

- [ ] `internal/referral` — code generation, binding at registration, commission, stats
- [ ] `GET /v1/referrals` — phones **masked** (winzangu leaked referees' full numbers to anyone with the code)
- [ ] `GET /v1/history` — keyset pagination over `transactions`
- [ ] `POST /v1/wallet/demo/topup` with cooldown → `429 demo_topup_not_eligible`
- [ ] Self-referral blocked
- [ ] Flutter: `wallet.dart`, `history.dart`, `refer.dart`, share intent, `/r/<code>` App Link
- [ ] Verify: after 10⁵ real spins, bankroll growth == `turnover × 300/10000` **to the cent**
- [ ] Verify: referee's spin credits the referrer exactly 2%, one row
- [ ] Verify: `referred_by IS NULL` pays nobody
- [ ] Verify: cold install from a referral link attributes correctly

---

## 🚫 M6 — M-Pesa deposits (BLOCKED)

**Blocked on:** Daraja sandbox credentials — C2B consumer key/secret, shortcode,
passkey. `.env` holds placeholders so everything else runs.

- [ ] `internal/mpesa/client.go` — token cache with `SetNX` stampede lock
- [ ] **Timestamp in `Africa/Nairobi`** (winzangu uses naive server-local time → invalid STK password off EAT)
- [ ] **Standard base64 for HTTP Basic**, not urlsafe (winzangu uses urlsafe; alphabets differ on `+`/`/`)
- [ ] STK push + STK query
- [ ] `parse.go` — every field optional (winzangu's date slicing panics on short strings and the bare `except` swallows it → silently dropped deposit)
- [ ] Webhook security: constant-time path secret · Safaricom CIDR allowlist (log-only first week) · 64 KB body · 5 s deadline · per-IP rate limit
- [ ] Archive every callback to `mpesa_callbacks` **before any logic runs**
- [ ] **Look up by `checkout_request_id`, never by phone** — and never create a user from a webhook (winzangu auto-created accounts whose password was the phone number)
- [ ] Credit `min(callback_amount, requested)` and flag `needs_review` on any difference
- [ ] Deposits above `MPESA_VERIFY_THRESHOLD_CENTS` confirmed by query, not by callback
- [ ] `POST /v1/wallet/deposits`, `GET /v1/wallet/deposits/{id}`, manual claim
- [ ] Deposit reconciler (90 s threshold, `pg_try_advisory_lock`)
- [ ] Flutter: `DepositScreen`, `StkWaitingView` (route, not dialog; 90 s ring; poll ladder 2 s→4 s), `DepositManualScreen`, `ACTION_DIAL` for `*334#`
- [ ] Copy rule: **an unresolved STK is `pending`, never `failed`**
- [ ] Verify: sandbox end-to-end over `make tunnel`
- [ ] Verify: same callback replayed 50× concurrently credits **once**
- [ ] Verify: 50 distinct receipts on one `checkout_request_id` → one credit, 49 `duplicate_receipt`
- [ ] Verify: forged callback on a guessed path → 404; right path, bad IP → 403
- [ ] Verify: kill the API before the callback, restart → reconciler credits within 60 s
- [ ] Verify: **callback arriving at t=120 s, after the client gave up, still credits**
- [ ] Verify: fuzz the parsers 60 s, zero panics

---

## 🚫 M7 — M-Pesa withdrawals (BLOCKED)

**Blocked on:** Daraja B2C credentials — key/secret, shortcode, initiator
name/password, sandbox `.cer`.

- [ ] `SecurityCredential` = `base64(RSA-PKCS1v15(initiator_password, cert))`, computed **at startup** so cert rotation is a file swap + restart
- [ ] `POST /v1/wallet/withdrawals` — hold at request time, `409` on a second open one
- [ ] Admin approve/reject; `AUTO_APPROVE_CEILING_CENTS=0` means a human sees every one
- [ ] Dispatcher: **`UPDATE ... SET status='sending'` COMMITS BEFORE the HTTP call.** Inverting this is how you pay a withdrawal twice.
- [ ] B2C v3 where available (own `OriginatorConversationID` → request-level idempotency); carry the id in `Remarks` as a v1/v2 fallback
- [ ] Result callback → settle (release hold, sweep fee to rake) **or** reverse (release + credit back) in one transaction
- [ ] **Timeout callback: record and back off. Reverse NOTHING.** A timeout means unknown.
- [ ] Exponential backoff `2^attempts` min, capped 30; the worker never declares failure — the reconciler does
- [ ] Withdrawal reconciler via transaction-status query; `withdrawal_stuck` alert after 1 h
- [ ] Flutter: `WithdrawScreen`, confirm sheet, pending receipt (never a blocking spinner), 3-state badge
- [ ] Verify: sandbox happy path → `paid`, hold released, I5 holds
- [ ] Verify: non-zero result reverses the hold **in the same transaction**, balance whole
- [ ] Verify: timeout callback reverses nothing; reconciler resolves it
- [ ] Verify: `SIGKILL` between the `sending` commit and the Daraja response, restart → reconciler **queries, never re-sends**
- [ ] Verify: two concurrent requests → one `409`
- [ ] Verify: a `failed` row without a reversal is refused by the schema

---

## ⬜ M8 — Admin surface

- [ ] `internal/admin` + `//go:embed admin.html` (one ~250-line vanilla-JS page — no npm, no second deployable)
- [ ] Withdrawal queue with user context (balance, lifetime deposits/withdrawals)
- [ ] Deposits needing review
- [ ] User lookup, suspend, adjust (**mandatory reason**; goes through `wallet.Move` so it appears in the player's own history)
- [ ] `GET /v1/admin/stats` — bankroll, rake, today's turnover and realised RTP, open holds
- [ ] `GET /v1/admin/integrity` — the I1–I8 lights
- [ ] `admin_audit` row for **every** mutation
- [ ] Verify: player JWT on any `/admin/*` → 403
- [ ] Verify: an adjustment moves money through the ledger and I1 still holds

> Contrast: winzangu's Django admin makes `tokens`, `trial_balance` and
> `withraw_power` inline-editable with **no ledger row written**, and
> `CentralBank.give_away` directly editable, which changes every player's odds.

---

## ⬜ M9 — WebSocket push

- [ ] `internal/ws` (coder/websocket) + `internal/push` (Redis `PSUBSCRIBE`)
- [ ] JWT in the **handshake header**, never a query string (which would land in nginx logs)
- [ ] 6 frame types: `hello`, `balance`, `deposit`, `withdrawal`, `tx`, `config`
- [ ] Emitters at the 4 existing commit points — **no money logic touched**
- [ ] `pingInterval` 20 s; drop slow clients after a 5 s write deadline
- [ ] Flutter `socket.dart`: jittered exponential backoff, lifecycle-aware, `GET /me` on resume
- [ ] **Acceptance criterion: disable the socket entirely and the whole app still works**

---

## ⬜ M10 — Hardening & release

- [ ] `MPESA_CALLBACK_IP_ENFORCE=true` after a week of log-only observation
- [ ] `POST /v1/client-error`; in-app update check `GET /v1/app/version`
- [ ] `deploy/Dockerfile` — distroless static, `CGO_ENABLED=0 -trimpath -s -w`
- [ ] 5-minute integrity cron; bankroll snapshots
- [ ] Flutter release: signing config, `key.properties` gitignored, R8 + `shrinkResources`, `minSdk 23`
- [ ] `--split-per-abi --obfuscate --split-debug-info --tree-shake-icons`
- [ ] CI size gate (fail the build on APK regression)
- [ ] Verify: `hey -n 20000 -c 100` on `/game/spin` → p99 < 50 ms, zero errors, invariants hold after
- [ ] Verify: `go vet` + `staticcheck` clean; `gitleaks` finds nothing
- [ ] Verify: a captured log file contains **no** unmasked phone or secret
- [ ] Verify: **arm64 APK < 10 MB**
- [ ] Verify: `APP_ENV=prod` with a sandbox M-Pesa URL refuses to start
- [ ] Verify: restore from `pg_dump` passes the invariant suite
- [ ] Verify: signed release APK on a physical device completes **deposit → spin → withdraw**
- [ ] **R8 breakage only shows in release builds** — every device test must be a release build

---

## Needed from the product owner

- [!] **Daraja sandbox credentials** (blocks M6, M7) — C2B + B2C key/secret, shortcode, passkey, initiator name/password, sandbox `.cer`
- [ ] Confirm the **referral rate** (currently 2% of turnover)
- [ ] Confirm **spin duration** (15 s as winzangu, or ~9 s)
- [ ] Confirm **distribution route** (Play + BCLB licence, or sideloaded APK) — decides whether the in-app updater is mandatory in v1
- [ ] A **physical Android device** for M3 frame-timing and the M4 Android-6 TLS check
- [ ] Decide on **responsible-gaming controls** (absent from winzangu entirely; a BCLB licence will require them)
