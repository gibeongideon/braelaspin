-- +goose Up
-- +goose StatementBegin

-- ══════════════════════════════════════════════════════════════════════════════
-- braelaspin initial schema
--
-- Money is bigint CENTS of KES, everywhere, with no exceptions. Never a float,
-- and never a numeric that someone might read into a float64. Cents rather than
-- whole shillings because referral commission at 2% of a KES 5 stake is 10c.
--
-- Most of the correctness of this system lives in the constraints below rather
-- than in Go code. Read the CHECKs and the partial unique indexes as the spec.
-- ══════════════════════════════════════════════════════════════════════════════


-- ── identity ─────────────────────────────────────────────────────────────────
CREATE TABLE users (
  id             bigserial PRIMARY KEY,
  phone          text NOT NULL UNIQUE CHECK (phone ~ '^254[17][0-9]{8}$'),
  password_hash  text NOT NULL,
  ref_code       text NOT NULL UNIQUE CHECK (ref_code ~ '^[A-Z0-9]{6,10}$'),
  referred_by    bigint REFERENCES users(id) CHECK (referred_by <> id),
  phone_verified boolean NOT NULL DEFAULT false,   -- reserved for SMS OTP in v2
  is_admin       boolean NOT NULL DEFAULT false,
  is_active      boolean NOT NULL DEFAULT true,
  demo_topup_at  timestamptz,
  created_at     timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX users_referred_by_idx ON users (referred_by) WHERE referred_by IS NOT NULL;


-- ── balances ─────────────────────────────────────────────────────────────────
-- Three columns, not eight. Cumulative deposit/withdraw totals are SUM() over
-- the ledger, never denormalised counters that can drift out of agreement.
--
-- held_cents is the withdrawal hold: funds leave real_cents the instant a
-- withdrawal is requested, so they cannot be gambled while a payout is in
-- flight, and SUM(held_cents) is checkable against the open withdrawals.
CREATE TABLE wallets (
  user_id    bigint PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
  real_cents bigint NOT NULL DEFAULT 0 CHECK (real_cents >= 0),
  demo_cents bigint NOT NULL DEFAULT 0 CHECK (demo_cents >= 0),
  held_cents bigint NOT NULL DEFAULT 0 CHECK (held_cents >= 0),
  updated_at timestamptz NOT NULL DEFAULT now()
);


-- ── the ledger ───────────────────────────────────────────────────────────────
-- Append-only. This is what the player sees in History, and what proves the
-- wallet columns above are correct (invariant I1).
CREATE TABLE transactions (
  id                  bigserial PRIMARY KEY,
  user_id             bigint NOT NULL REFERENCES users(id),
  kind                text   NOT NULL CHECK (kind IN (
                        'demo_grant','bet','win','deposit','withdraw',
                        'withdraw_reversed','referral','adjustment')),
  is_real             boolean NOT NULL,
  amount_cents        bigint NOT NULL CHECK (amount_cents <> 0),  -- signed
  balance_after_cents bigint NOT NULL CHECK (balance_after_cents >= 0),
  ref_id              bigint,     -- spins.id | deposits.id | withdrawals.id
  memo                text,
  created_at          timestamptz NOT NULL DEFAULT now(),

  -- real-money-only kinds can never appear on a demo row, and vice versa.
  -- This is invariant I3 as a constraint: the rule that, if broken, would
  -- print real money out of demo tokens.
  CONSTRAINT tx_realm_kinds CHECK (
    (is_real AND kind <> 'demo_grant') OR
    (NOT is_real AND kind IN ('demo_grant','bet','win','adjustment'))
  )
);
CREATE INDEX tx_user_idx ON transactions (user_id, id DESC);
CREATE INDEX tx_kind_idx ON transactions (kind, id DESC);
CREATE INDEX tx_ref_idx  ON transactions (kind, ref_id) WHERE ref_id IS NOT NULL;

-- A spin pays its referrer at most once.
CREATE UNIQUE INDEX tx_referral_once ON transactions (ref_id) WHERE kind = 'referral';

-- ── spins ────────────────────────────────────────────────────────────────────
CREATE TABLE spins (
  id            bigserial PRIMARY KEY,
  user_id       bigint NOT NULL REFERENCES users(id),
  is_real       boolean NOT NULL,
  stake_cents   bigint   NOT NULL CHECK (stake_cents > 0),
  segment_index smallint NOT NULL CHECK (segment_index BETWEEN 1 AND 12),
  multiplier_bp integer  NOT NULL CHECK (multiplier_bp >= 0),
  payout_cents  bigint   NOT NULL CHECK (payout_cents >= 0),
  rake_cents    bigint   NOT NULL DEFAULT 0 CHECK (rake_cents >= 0),
  client_ref    text     NOT NULL,
  created_at    timestamptz NOT NULL DEFAULT now(),

  -- The payout can never disagree with the segment that produced it.
  -- Integer division matches the Go engine exactly (both truncate).
  CONSTRAINT spins_payout_exact CHECK (payout_cents = stake_cents * multiplier_bp / 10000),
  -- Demo spins take no rake: there is no house money involved.
  CONSTRAINT spins_demo_no_rake CHECK (is_real OR rake_cents = 0)
);
CREATE INDEX spins_user_idx ON spins (user_id, id DESC);
CREATE INDEX spins_real_idx ON spins (created_at DESC) WHERE is_real;
CREATE UNIQUE INDEX spins_client_ref ON spins (user_id, client_ref);


-- ── money in ─────────────────────────────────────────────────────────────────
CREATE TABLE deposits (
  id                  bigserial PRIMARY KEY,
  user_id             bigint NOT NULL REFERENCES users(id),
  amount_cents        bigint NOT NULL CHECK (amount_cents > 0),
  credited_cents      bigint CHECK (credited_cents IS NULL OR credited_cents > 0),
  status              text NOT NULL DEFAULT 'pending'
                        CHECK (status IN ('pending','success','failed','expired')),
  checkout_request_id text,
  merchant_request_id text,
  mpesa_receipt       text,
  result_code         integer,
  result_desc         text,
  needs_review        boolean NOT NULL DEFAULT false,
  is_manual_claim     boolean NOT NULL DEFAULT false,
  client_ref          text NOT NULL,
  created_at          timestamptz NOT NULL DEFAULT now(),
  updated_at          timestamptz NOT NULL DEFAULT now(),

  -- success <=> we hold a receipt, and we credited no more than was requested.
  CONSTRAINT dep_success_has_receipt CHECK ((status = 'success') = (mpesa_receipt IS NOT NULL)),
  CONSTRAINT dep_success_has_credit  CHECK ((status = 'success') = (credited_cents IS NOT NULL)),
  CONSTRAINT dep_credit_bounded      CHECK (credited_cents IS NULL OR credited_cents <= amount_cents)
);
-- The hard idempotency floor for M-Pesa deposits. A constraint, not a count()
-- query: the reference implementation used `.count() == 1`, which is a race.
CREATE UNIQUE INDEX dep_receipt_key  ON deposits (mpesa_receipt)       WHERE mpesa_receipt IS NOT NULL;
CREATE UNIQUE INDEX dep_checkout_key ON deposits (checkout_request_id) WHERE checkout_request_id IS NOT NULL;
CREATE UNIQUE INDEX dep_client_ref   ON deposits (user_id, client_ref);
CREATE INDEX dep_pending_idx ON deposits (created_at) WHERE status = 'pending';
CREATE INDEX dep_review_idx  ON deposits (created_at DESC) WHERE needs_review;


-- ── money out ────────────────────────────────────────────────────────────────
CREATE TABLE withdrawals (
  id            bigserial PRIMARY KEY,
  user_id       bigint NOT NULL REFERENCES users(id),
  amount_cents  bigint NOT NULL CHECK (amount_cents > 0),   -- gross debit from the wallet
  fee_cents     bigint NOT NULL DEFAULT 0 CHECK (fee_cents >= 0),
  status        text NOT NULL DEFAULT 'pending'
                  CHECK (status IN ('pending','approved','sending','paid','failed','rejected')),
  conversation_id            text,
  originator_conversation_id text,
  mpesa_receipt text,
  result_code   integer,
  result_desc   text,
  reviewed_by   bigint REFERENCES users(id),
  reviewed_at   timestamptz,
  review_note   text,
  attempts      integer NOT NULL DEFAULT 0 CHECK (attempts >= 0),
  next_try_at   timestamptz,
  sent_at       timestamptz,
  settled_at    timestamptz,
  client_ref    text NOT NULL,
  created_at    timestamptz NOT NULL DEFAULT now(),
  updated_at    timestamptz NOT NULL DEFAULT now(),

  -- A fee that swallows the whole withdrawal would pay the user nothing.
  CONSTRAINT wd_fee_bounded  CHECK (fee_cents < amount_cents),
  CONSTRAINT wd_paid_settled CHECK ((status = 'paid') = (settled_at IS NOT NULL)),
  CONSTRAINT wd_paid_receipt CHECK (status <> 'paid' OR mpesa_receipt IS NOT NULL)
);
-- One open withdrawal per user, enforced by the DATABASE. The reference
-- implementation did this with a Python any() over a select_for_update()
-- evaluated outside any atomic block, so the row lock never actually held.
CREATE UNIQUE INDEX wd_one_open ON withdrawals (user_id)
  WHERE status IN ('pending','approved','sending');
CREATE UNIQUE INDEX wd_client_ref ON withdrawals (user_id, client_ref);
CREATE UNIQUE INDEX wd_conv_key   ON withdrawals (conversation_id) WHERE conversation_id IS NOT NULL;
CREATE UNIQUE INDEX wd_orig_key   ON withdrawals (originator_conversation_id)
  WHERE originator_conversation_id IS NOT NULL;
CREATE INDEX wd_dispatch_idx ON withdrawals (next_try_at) WHERE status IN ('approved','sending');
CREATE INDEX wd_user_idx     ON withdrawals (user_id, id DESC);
CREATE INDEX wd_queue_idx    ON withdrawals (created_at) WHERE status = 'pending';


-- ── house ────────────────────────────────────────────────────────────────────
-- One row. The CHECK (bankroll_cents >= 0) is the backstop that makes the
-- reference implementation's silently-skipped-debit bug unrepresentable here:
-- if a payout would drive the pool negative, the whole spin transaction aborts
-- and the stake is not taken.
CREATE TABLE house (
  id             integer PRIMARY KEY DEFAULT 1 CHECK (id = 1),
  bankroll_cents bigint NOT NULL DEFAULT 0 CHECK (bankroll_cents >= 0),
  rake_cents     bigint NOT NULL DEFAULT 0 CHECK (rake_cents >= 0),
  updated_at     timestamptz NOT NULL DEFAULT now()
);
INSERT INTO house (id) VALUES (1);


-- ── raw webhook archive ──────────────────────────────────────────────────────
-- Every callback lands here BEFORE any logic runs, valid or not. When Safaricom
-- and our own records disagree, this table is the evidence.
CREATE TABLE mpesa_callbacks (
  id           bigserial PRIMARY KEY,
  channel      text  NOT NULL CHECK (channel IN ('stk','b2c_result','b2c_timeout')),
  body         jsonb NOT NULL,
  body_sha256  bytea NOT NULL,
  source_ip    inet,
  handled      boolean NOT NULL DEFAULT false,
  handle_error text,
  created_at   timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX cb_dedupe ON mpesa_callbacks (channel, body_sha256);
CREATE INDEX cb_unhandled_idx ON mpesa_callbacks (created_at) WHERE NOT handled;


-- ── admin audit ──────────────────────────────────────────────────────────────
CREATE TABLE admin_audit (
  id         bigserial PRIMARY KEY,
  actor_id   bigint NOT NULL REFERENCES users(id),
  action     text   NOT NULL,
  target     text,
  detail     jsonb,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX audit_actor_idx ON admin_audit (actor_id, id DESC);
CREATE INDEX audit_time_idx  ON admin_audit (created_at DESC);

-- +goose StatementEnd

-- The ledger is append-only. These rules make UPDATE and DELETE silent no-ops
-- rather than errors, which is deliberate: a buggy or malicious statement
-- cannot rewrite financial history even if it reaches the database.
-- +goose StatementBegin
CREATE RULE tx_no_update AS ON UPDATE TO transactions DO INSTEAD NOTHING;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE RULE tx_no_delete AS ON DELETE TO transactions DO INSTEAD NOTHING;
-- +goose StatementEnd


-- +goose Down
-- +goose StatementBegin
DROP RULE IF EXISTS tx_no_delete ON transactions;
DROP RULE IF EXISTS tx_no_update ON transactions;
DROP TABLE IF EXISTS admin_audit;
DROP TABLE IF EXISTS mpesa_callbacks;
DROP TABLE IF EXISTS house;
DROP TABLE IF EXISTS withdrawals;
DROP TABLE IF EXISTS deposits;
DROP TABLE IF EXISTS spins;
DROP TABLE IF EXISTS transactions;
DROP TABLE IF EXISTS wallets;
DROP TABLE IF EXISTS users;
-- +goose StatementEnd
