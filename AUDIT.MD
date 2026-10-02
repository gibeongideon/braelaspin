# Pre-launch audit

> Run 2026-10-02 against the live deployment and the working tree.
> **Verdict: not ready for real money.** Everything the product *does* is
> correct; what is missing is what you need around it.

---

## Verdict in one line

The money is provably right, the clients are at parity, and the security
posture is sound — but there are **no backups, no monitoring, no M-Pesa, and
no responsible-gaming controls**. Ship to real money only after the blockers
below.

---

## ✅ Verified correct

### Money — the thing that matters most
Run against **live production data: 8 users, 519 spins, 726 ledger rows.**

| Invariant | Result |
|---|---|
| I1 real balances == ledger sum | **0 drift** |
| I1 demo balances == ledger sum | **0 drift** |
| I2 nothing negative | **0** |
| I3 demo and real never mix | **0** |
| I6 `payout == stake × mult / 10000` | **0 violations in 519 spins** |
| I7 commission paid at most once | **0** |
| I8 append-only rules attached | **2 of 2** |

Plus, in test: Go and Postgres agree on the payout across 116 stake/multiplier
combinations; full ledger accounting asserted for all 12 segments with money
conserved; 200 concurrent debits yield exactly one success; 100 concurrent
house debits never drive the bankroll negative.

### Security
- No secrets in the repo or in git history; `.env` never committed
- Postgres, Redis and the API are **closed to the internet** (only 22/80/443 open)
- TLS with HSTS, `X-Frame-Options: DENY`, `nosniff`, referrer policy
- `alg=none` JWT rejected · anonymous admin access rejected · CORS rejects
  foreign origins (403)
- Login does not reveal whether a phone is registered (identical message)
- **400 log lines contain zero unmasked phones, passwords or tokens**

### Clients
- Web and Flutter at **full feature parity**, checked mechanically
- **201 tests**: 72 Go, 87 TypeScript, 44 Dart
- `core/` provably DOM-free; the same assertions run on both clients

---

## 🚫 Blockers — do not take real money until these are done

1. **No backups.** The ledger *is* the business. Nightly `pg_dump -Fc` plus WAL
   archiving, off-site, with a monthly restore drill that ends by running the
   invariant suite. Nothing else on this list matters as much.
2. **`APP_ENV=staging`.** Production mode additionally enforces HTTPS,
   `sslmode=require` and M-Pesa callback IP checks. Flipping it will fail
   until 3 and 4 are fixed — which is the point.
3. **Postgres has no TLS** (`sslmode=disable`). Tolerable only because it is on
   loopback; must change with `APP_ENV=prod`.
4. **M-Pesa is placeholder credentials.** Deposits and withdrawals cannot work.
   Blocked on Daraja sandbox keys.
5. **No monitoring or alerting.** At minimum alert when
   `GET /v1/admin/integrity` goes non-green, and on `withdrawal_stuck`.
6. **No responsible-gaming controls.** "18+" is static text. There is no age
   gate, no deposit/loss/session limits, no self-exclusion and no support
   links. A BCLB licence will require these, and they are the right thing
   regardless.

---

## ⚠️ Known and accepted

- **APK is 14.6 MB against a 10 MB budget.** `libflutter.so` alone is 11.2 MB;
  our code is 2.75 MB. Not closeable with Flutter for a sideloaded APK. Either
  revise the budget or ship via Play (an AAB would download at ~8–9 MB).
- **Refresh token in `localStorage` on web** — readable by any script on the
  origin. An httpOnly cookie is better but needs a CSRF story.
- **Practice pays better than real play** (RTP 9700 vs 9000). Deliberate, and
  **disclosed in both clients**. If that disclosure is ever removed, the demo
  table must go back to matching the real one — an undisclosed difference is a
  pattern regulators treat as deceptive.
- **The bare IP is plain HTTP.** No public CA issues certs for an IP. Prefer
  the domain; consider redirecting the IP once nothing depends on it.
- **Admin surface not built** (M8), nor WebSocket push (M9).

---

## Fixed during this audit

- `duplicate_spin` and `spin_in_flight` had no player-facing copy, so a retry
  after a dropped connection showed "that didn't look right" instead of "that
  spin already went through". Added, with a test that fails if any server code
  loses its copy.
- The web win chime ignored the mute flag entirely — a muted app still played
  fanfares. All sound now reads one setting.
- Five web/Flutter parity gaps closed: winners strip, offline banner, mute
  control, social share grid, practice-RTP disclosure.
