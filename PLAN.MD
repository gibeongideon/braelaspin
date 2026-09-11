# braelaspin — Project Plan & Direction

> **Read this first.** It is the authoritative description of what this project
> is, why it is built the way it is, and where it is going. `TODO.MD` tracks
> task-level status; this file explains the *reasoning* behind the tasks.
>
> Last updated: 2026-09-11

---

## 1. What this is

A **spin-the-wheel real-money game** for the Kenyan market, delivered as an
Android app backed by a Go API.

It reimplements the end functionality of an existing Django app, **winzangu**
(`/home/rock/Desktop/2026_Projects/my/winzangu`), on a new stack. It is
explicitly **not a port**: the design and the logic are re-derived. The brief
was "same idea, better, simplified, less code."

### The three goals, in priority order

1. **A small APK.** The stated primary product constraint. Budget: **≤ 10 MB**
   per ABI (projected 8.6 MB arm64).
2. **Less code.** Same product ideas, fewer lines. Targets: ~3,000 lines of Go,
   ~4,500 of Dart, ~250 of Kotlin.
3. **Correct money.** winzangu has three money bugs (§3). They must be
   impossible here by construction, not merely avoided by intent.

### The stack (fixed by the brief)

```
Flutter App (Android)
     │
     │ HTTPS (all money operations, idempotent)
     │ WSS   (server→client push only, optional)
     ▼
Go API Server
     │
     ├── Redis      sessions (refresh tokens) · rate limits · idempotency · Daraja token
     │
     └── PostgreSQL users · wallets · transactions · spins · deposits · withdrawals · house
     ▲
     └── Safaricom Daraja webhooks (STK callback · B2C result · B2C timeout)
```

One Go binary serves the API, runs migrations, and probes its own health.
Background work is **one goroutine on a 60s ticker** guarded by
`pg_try_advisory_lock`, so replicas cooperate with no leader election, no
scheduler, and no worker image.

---

## 2. Decisions already taken

These were settled with the product owner. **Do not re-open them** without a
new decision.

| Decision | Choice | Rationale |
|---|---|---|
| Odds model | **Fixed-weight RTP table** | winzangu's bankroll-dependent selection made odds drift with pool depth; see §3 |
| Payment rails | **M-Pesa only** (Daraja STK + B2C) | Matches the phone-number identity model; keeps binary and APK small. No PayPal. |
| Auth | **Phone + password → JWT** | `phone_verified` column reserved so SMS OTP can be layered on in v2 |
| v1 feature set | demo balance, referrals, transaction history, admin | all confirmed in scope |
| Wheel | **12 segments, top prize 200×** | readable at 320dp; halves the painting code; makes the bankroll cap 5× easier to satisfy |
| WebSocket role | **push only, never the money path** | Kenyan carrier NAT and radio sleep kill idle sockets; money must not ride a transport that dies silently |

### The WebSocket call, spelled out

winzangu sends bets *and* spins over its socket. Copying that onto mobile would
be a mistake. Here:

- Every money operation is an **idempotent HTTPS POST**. That is canonical.
- The WS carries **only** server→client notifications (balance changed, deposit
  confirmed, withdrawal paid). No frames go out except protocol pongs.
- **Acceptance criterion: disable the socket entirely and the whole app still
  works.** Only deposit-confirmation latency degrades, and polling covers that.

This is why the WebSocket is Phase 9, not Phase 1.

---

## 3. The three bugs we exist to avoid

This is the single most important section for anyone touching the money code.

### Bug 1 — odds that drift with the bankroll

`winzangu/spinx/models.py:68-77`. `winner_selector` collected every segment the
house pool could afford, then picked **uniformly at random** from that set:

```python
for n in range(len(wheel_map)):
    if float(give_away)/float(bet_amount) >= float(wheel_map[n]):
        chosen.append((n, wheel_map[n]))
return chosen[randint(0, len(chosen)-1)]
```

So with a healthy pool and a small bet, player EV ran at **10×–70× the stake**,
not the intended 0.9×. The live DB showed 25 real bets totalling −1,896 against
13 wins totalling +1,937.

**Fix here.** Fixed weight table; RTP is one configured number, asserted against
the table at startup. The bankroll's *only* role is a **pre-draw veto** — it
refuses a stake whose worst case it could not pay, and then has zero influence
on which segment comes up. See `internal/game/wheel.go` and `spin.go`.

> **Never** let the bankroll influence the outcome. That is the exact road that
> produced `winner_selector`. If the max stake feels too restrictive, fund a
> bigger bankroll or lower the top prize.

### Bug 2 — the silently skipped debit

`winzangu/accounts/models.py:51-69`. `CentralBank.update_field(value, field)`
took the **new balance** as its argument and guarded it with `if value > 0`,
writing via a full-row save from a stale unlocked read. Three failure modes in
one shape: a payout large enough to drive the pool to ≤ 0 was **never debited**;
concurrent updates were lost; sibling columns were clobbered.

**Fix here.** `internal/wallet` is the only package that moves money, and its
`Move` signature has **no parameter for a new balance**. Every change is a
signed delta applied by Postgres as a relative `UPDATE` under a row lock, with
a `CHECK (>= 0)` backstop that aborts the whole transaction. The bug is not
expressible through the API.

### Bug 3 — the unreconciled payout

`winzangu/accounts/signals.py:66` and `models.py:531`. `confirmed` was set the
moment a B2C request was *queued* (`#let_respose_do_tiz`), and the signal that
should have reconciled the result is literally `pass`. A failed payout left the
user **debited, permanently**, with no compensating entry and no query that
could notice.

**Fix here.** Funds are **held** at request time (`wallet.Hold`). The B2C result
callback either settles or reverses, in one transaction. A **timeout means
unknown, never failed** — reversing on timeout is the easiest way to pay twice.
A reconciler resolves anything the callback misses. Invariant **I5** makes a
held-but-unaccounted balance detectable.

---

## 4. Economics

### The wheel — `internal/game/wheel.go`

Single source of truth. The client fetches it from `GET /v1/game/config` and
**never hardcodes a copy** (winzangu kept the map in `settings.py` *and* again
in its JavaScript, relying on humans to keep them equal).

| Multiplier | Segments | Weight /10000 | Probability | RTP contribution |
|---|---|---|---|---|
| 0 (lose) | 4 | 6277 | 62.77% | — |
| 1× | 2 | 2000 | 20.00% | 2000 bp |
| 2× | 2 | 1200 | 12.00% | 2400 bp |
| 5× | 1 | 400 | 4.00% | 2000 bp |
| 10× | 1 | 100 | 1.00% | 1000 bp |
| 50× | 1 | 20 | 0.20% | 1000 bp |
| **200×** | 1 | 3 | 0.03% | 600 bp |
| | **12** | **10000** | **100%** | **9000 bp = RTP 90.00%** |

Losing segments sit at positions 1, 4, 7, 10 so the wheel reads as alternating
rather than as two dead halves. A player wins something **37.23%** of the time —
more often than winzangu at typical pool depth, while the house edge is now
actually 10% instead of deeply negative.

`Validate()` runs at startup and **fails the process** if the weights do not sum
to 10000 or if `Σ(MultBP × Weight)/10000 ≠ RTP_BP`. A typo cannot reach a player.

### The 10% edge splits

```
RAKE_BP=500       5%  retained by the house
REFERRAL_BP=200   2%  to the referrer (0 when the player has no referrer)
                  3%  grows the bankroll
```

winzangu paid 5% referral against a nominal 10% edge, leaving almost nothing —
and `winner_selector` meant there was no edge at all. It also paid commission to
`User.objects.get(id=1)` when a player had no referrer, so the owner silently
collected 5% of every unreferred player's turnover, and the spin crashed if user
1 was ever deleted. **Here, no referrer means no commission.**

### Admission control

The bankroll must cover `MaxMult × stake`, so `max_stake = bankroll / 200`.
A KES 100,000 bankroll admits a KES 500 stake. **This is why the top prize is
200× and not winzangu's 1000×** — at 1000× the same bankroll would cap the stake
at KES 100.

### Variance

The 200× segment contributes 600 of the 9000 bp of RTP but dominates variance
(σ ≈ 2.4× the stake). Budget for it; do not "solve" it by touching the odds.

---

## 5. Data model

`internal/db/migrations/0001_init.sql`. Money is **`bigint` cents of KES**
everywhere — never a float, never a `numeric` someone might read into a
`float64`. Cents rather than whole shillings because referral at 2% of a KES 5
stake is 10 cents.

**Most of the correctness of this system lives in the constraints. Read the
CHECKs and partial unique indexes as the spec.**

| Table | Purpose | Load-bearing constraints |
|---|---|---|
| `users` | identity; phone IS the identity | `phone ~ '^254[17][0-9]{8}$'`, `ref_code` 6–10 upper-alnum, `referred_by <> id` |
| `wallets` | **3** balance columns | all `>= 0`; `held_cents` is the withdrawal hold |
| `transactions` | append-only ledger | `tx_realm_kinds` (demo/real never mix), `tx_referral_once`, `DO INSTEAD NOTHING` rules on UPDATE/DELETE |
| `spins` | one row per play | `spins_payout_exact` (payout must equal stake×mult/10000), `spins_demo_no_rake` |
| `deposits` | M-Pesa in | `dep_receipt_key` UNIQUE — the hard idempotency floor |
| `withdrawals` | M-Pesa out | `wd_one_open` — one open withdrawal per user, enforced by the DB |
| `house` | single row: bankroll + rake | `bankroll_cents >= 0` — the Bug-2 backstop |
| `mpesa_callbacks` | raw webhook archive | `cb_dedupe` on body hash |
| `admin_audit` | every admin mutation | — |

**Nine tables.** Deliberately absent: no referral-commission table (a commission
is a `transactions` row), no game-config table (the wheel is a Go const, the
knobs are env vars), no currency table (KES only). winzangu has 8 balance
columns of which 2 are unused; we have 3.

### Cut from winzangu, on purpose

| Cut | Why |
|---|---|
| `withraw_power` (an undocumented 1× wagering requirement) | a hidden rule that generates tickets and chargebacks. If you want it back, it must be disclosed in-app before the first deposit. |
| `is_marketer` parallel economy | duplicate bankroll fields and split referral chains across three models, unused |
| PayPal + `Currency` FX table | M-Pesa/KES only |
| Denormalised `cum_deposit`/`cum_withraw` counters | derive with `SUM()`; counters drift |

---

## 6. Money integrity — the invariants

`internal/wallet/invariant.go`. Each is SQL returning **zero rows when healthy**.
Run by every integration test's cleanup, by `GET /v1/admin/integrity`, and
(planned) by a 5-minute cron.

| ID | Rule |
|---|---|
| **I1** | every wallet balance equals the sum of its ledger rows (`real_cents` alone — `held_cents` is reconciled by I5) |
| **I2** | no balance and not the bankroll is ever negative |
| **I3** | demo and real money never mix — the rule that, if broken, prints real money from demo tokens |
| **I4** | deposits credit exactly once, for no more than was requested |
| **I5** | `SUM(held_cents)` equals the sum of open withdrawals — **the invariant whose absence is Bug 3** |
| **I5b** | every terminal withdrawal is either settled or reversed; never both, never neither |
| **I6** | spins are arithmetically exact and fully recorded in the ledger |
| **I7** | referral commissions are bounded, non-reflexive, paid at most once per spin |
| **I8** | the ledger's append-only rules are still attached |

> `require.NoError(t, wallet.AssertInvariant(ctx, db))` belongs in a `t.Cleanup`
> on **every** integration test. It is one line and it converts "we think the
> money is right" into "the money is provably right after every test we have run."

---

## 7. API surface

All under `/v1`, JSON in/out, `Authorization: Bearer <access>` unless public.
One error envelope everywhere:

```json
{"error":{"code":"insufficient_funds","message":"…","meta":{},"request_id":"…"}}
```

`code` is stable and machine-readable; **the Flutter client owns the user-facing
copy**, so the server stays terse and every player-visible string lives in one
Dart file. winzangu's `NC`/`MM`/`BET`/`888` become
`insufficient_funds`/`stake_too_small`/`ok`/`no_pending_spin`.

| Group | Endpoints |
|---|---|
| Auth (public) | `POST /auth/{register,login,refresh,logout}` |
| Player | `GET /me`, `GET /wallet`, `GET /history`, `POST /wallet/demo/topup`, `GET|PATCH /profile` |
| Game | `GET /game/config`, `POST /game/spin`, `GET /game/spins`, `GET /game/spins/{id}` |
| Money | `POST /wallet/deposits`, `GET /wallet/deposits/{id}`, `POST /wallet/deposits/manual-claim`, `POST /wallet/withdrawals`, `GET /wallet/withdrawals` |
| Referrals | `GET /referrals` (phones **masked**) |
| Webhooks (public, secret path) | `POST /webhooks/mpesa/{stk,b2c/result,b2c/timeout}/{secret}` |
| Admin (`is_admin`) | `GET /admin/withdrawals`, `POST /admin/withdrawals/{id}/{approve,reject}`, `GET /admin/{deposits,users,stats,integrity}`, `GET /admin` (embedded page) |
| Health | `GET /healthz` (no deps — liveness), `GET /readyz` (pings both — readiness) |

**One tap, not two.** winzangu requires a separate "place bet" (creating an
unspun `Stake`) then a "spin" that pops the oldest unspun row — hence its `888`
sentinel and "spins available" counter. `POST /game/spin` does both atomically,
removing a column, a sentinel, a UI counter and a failure mode.

---

## 8. Phases

Status legend: ✅ done & verified · 🔨 in progress · ⬜ not started · 🚫 blocked

| # | Phase | Status | Verified by |
|---|---|---|---|
| **M0** | Toolchain & skeleton | ✅ | config fail-fast proven both ways; `/healthz` 200 while `/readyz` 503 with Redis down; graceful shutdown |
| **M1** | Money foundation (schema, wallet, invariants) | ✅ | 10 integration tests incl. 200-goroutine and bankroll concurrency proofs; clean under `-race` |
| **M2** | Auth | 🔨 | phone/password/token services done + fuzzed; **DB-backed handlers not yet wired** |
| **M3** | The wheel (Flutter `CustomPainter`, offline) | ⬜ | 200 consecutive spins land on the requested index; <8 ms/frame on the cheapest target device |
| **M4** | Spin engine + app shell | 🔨 | engine + service done and statistically verified; **HTTP handlers and Flutter shell not started** |
| **M5** | Real money, referrals, history | ⬜ | bankroll growth == turnover × 300/10000 to the cent |
| **M6** | M-Pesa deposits | 🚫 | needs Daraja sandbox credentials (§11) |
| **M7** | M-Pesa withdrawals | 🚫 | needs Daraja sandbox credentials (§11) |
| **M8** | Admin surface | ⬜ | player JWT on `/admin/*` returns 403; every mutation writes an audit row |
| **M9** | WebSocket push | ⬜ | **disable the socket and the app still works** |
| **M10** | Hardening & release APK | ⬜ | arm64 APK < 10 MB; full deposit→spin→withdraw on a physical device |

### What "verified" means per phase

Not line coverage. The tests that earn their keep are the **concurrency and
failure** tests — they are the ones that would have caught all three winzangu
bugs. Each phase's row above names its acceptance criterion.

---

## 9. Repository layout

```
braelaspin/
├── PLAN.MD · TODO.MD           this file and the task tracker
├── Makefile                    every workflow (make help)
├── .env.example                every setting, documented
├── cmd/api/main.go             serve | migrate | healthcheck | version
├── internal/
│   ├── config/                 env → struct; reports EVERY bad key at once
│   ├── db/                     pgx pool + //go:embed migrations (goose)
│   │   └── migrations/         0001_init.sql — read the constraints as spec
│   ├── rds/                    refresh tokens · rate limits · idempotency · Daraja token · pubsub
│   ├── httpx/                  router · middleware · error envelope
│   ├── auth/                   phone · argon2id · JWT + refresh rotation
│   ├── wallet/                 ★ THE ONLY CODE THAT MOVES MONEY + invariants
│   ├── game/                   wheel (table + crypto/rand) · spin transaction
│   ├── testutil/               real-Postgres test harness
│   ├── mpesa/       (planned)  Daraja client · deposit · withdraw · webhooks · reconciler
│   ├── referral/    (planned)
│   ├── push/ ws/    (planned)
│   └── admin/       (planned)  handlers + //go:embed admin.html
├── deploy/                     docker-compose.yml (Postgres 5433, Redis 6380)
└── app/            (planned)   the Flutter app
```

### Toolchain locations

Installed **without sudo** (sudo requires a password on this machine):

```
Go 1.27.1       /home/rock/.local/go
JDK 17          /home/rock/.local/jdk17
Flutter 3.47.3  /home/rock/.local/flutter      (Dart 3.13.3)
Android SDK 36  /home/rock/Android/Sdk
GOPATH          /home/rock/.local/gopath
```

The `Makefile` sets `PATH` itself, so `make <target>` works from a bare shell.

---

## 10. Working commands

```bash
make help              # list all targets
make up                # Postgres (5433) + Redis (6380)
make migrate           # apply migrations
make run               # run the API against .env
make test              # unit tests, no DB needed
make test-integration  # creates braelaspin_test, runs the real-DB suite
make test-race         # money path under the race detector
make fuzz              # fuzz phone normalisation
make build             # stripped static binary → bin/
make tunnel            # expose :8080 for Daraja callbacks (needs cloudflared)
make app-run           # Flutter app on a connected device
make app-apk           # split release APKs
make app-size          # APK size against the 10 MB budget
```

---

## 11. Blockers & open items

### Blocking

- **Daraja sandbox credentials** are required to finish M6/M7: consumer
  key/secret (C2B *and* B2C), shortcode, passkey, B2C initiator name/password,
  and the sandbox `.cer`. `.env` currently holds placeholders so everything else
  runs. **Nothing else is blocked.**

### Decide before launch

- **Referral rate.** 2% of turnover against a 10% edge. Attractive in this
  market; past ~4% it starts eating the game's viability.
- **Spin duration.** winzangu's 15 s means 94% of the rotation finishes by
  t=7.5 s and the last half crawls. Consider ~9 s with a "Suspense mode" toggle.
- **Distribution.** Google Play prohibits real-money gambling outside an
  allow-list; Kenya is on it but needs a **BCLB licence** plus a per-app
  declaration. Plan for v1 shipping as a **sideloaded APK**, which requires an
  in-app update check (`GET /v1/app/version`) — cheap now, awkward to retrofit.
- **Responsible gaming is absent from winzangu entirely** — no age check, no
  deposit/loss/session limits, no self-exclusion, and a T&C checkbox linking to
  `href="#"`. Out of scope for v1 but a BCLB licence will require it.

### Security note, unrelated to this project

**Rotate winzangu's committed credentials.** `winzangu/settings.py` carries
live-looking `PAYPAL_WPP_PASSWORD` / `PAYPAL_WPP_SIGNATURE`, a hardcoded Django
`SECRET_KEY`, and `SECRET_MPESA_URL` in git. Treat them as compromised. Do not
port the vendored `paypal/` directory.

---

## 12. Conventions for contributors

1. **Only `internal/wallet` moves money.** If you find yourself writing
   `UPDATE wallets` anywhere else, stop.
2. **Never compute an absolute balance.** Signed deltas and relative SQL only.
   This is Bug 2.
3. **Lock order is wallets (ascending `user_id`) then house.** Every path that
   touches both follows it, or they deadlock.
4. **`READ COMMITTED` is sufficient** — because every read is `FOR UPDATE` and
   every write is relative. There is no retry-on-serialization-failure logic in
   this codebase and none should be added.
5. **Constraints are the spec.** Prefer a CHECK or a partial unique index over a
   Go guard. winzangu used a `.count() == 1` query for deposit idempotency,
   which is a race; we use a unique index.
6. **A timeout means unknown, never failed.** Especially for B2C.
7. **Fail closed on authorisation, open on abuse control.** The suspension check
   fails closed; the rate limiter fails open. Both are deliberate and commented.
8. **Never log a phone, token, password, passkey or `SecurityCredential`.**
   Phones go through `auth.Mask()`. M-Pesa receipts *are* logged — they are the
   reconciliation key and not secret.
9. **Client never computes an outcome.** The server returns a segment index; the
   app animates to it.
10. **Every integration test ends with the invariant assertion.**
