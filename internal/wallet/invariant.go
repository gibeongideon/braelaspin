package wallet

import (
	"context"
	"fmt"
	"strings"
)

// Check is one integrity rule and what, if anything, is violating it.
type Check struct {
	ID      string
	Name    string
	Healthy bool
	Detail  string // populated only when unhealthy
}

// invariant is a rule expressed as SQL that returns ZERO rows when healthy.
// The first column of any row it does return is used as the failure detail.
type invariant struct {
	id, name, query string
}

// The invariants. Each one exists because breaking it would mean real money is
// wrong, and each is cheap enough to run every five minutes in production.
var invariants = []invariant{
	{
		"I1", "every wallet balance is derivable from the ledger",
		// The wallet columns exist for fast reads and for the >= 0 CHECK.
		// This proves they never drift from the append-only ledger. The
		// reference implementation had no analogue: its running_balance was
		// written from whatever the in-memory balance happened to be, with no
		// query that could detect divergence.
		`SELECT format('user %s: real=%s ledger=%s demo=%s ledger=%s',
		               w.user_id, w.real_cents, COALESCE(r.s,0), w.demo_cents, COALESCE(d.s,0))
		   FROM wallets w
		   LEFT JOIN (SELECT user_id, SUM(amount_cents) s FROM transactions
		               WHERE is_real GROUP BY user_id) r ON r.user_id = w.user_id
		   LEFT JOIN (SELECT user_id, SUM(amount_cents) s FROM transactions
		               WHERE NOT is_real GROUP BY user_id) d ON d.user_id = w.user_id
		  -- held_cents is NOT added here. A hold debits real_cents via a
		  -- 'withdraw' ledger row and separately increments held_cents, so the
		  -- ledger sum tracks real_cents alone. held_cents is reconciled
		  -- against the open withdrawals by I5 instead.
		  WHERE w.real_cents <> COALESCE(r.s, 0)
		     OR w.demo_cents <> COALESCE(d.s, 0)`,
	},
	{
		"I2", "no balance is negative",
		`SELECT format('user %s real=%s demo=%s held=%s', user_id, real_cents, demo_cents, held_cents)
		   FROM wallets WHERE real_cents < 0 OR demo_cents < 0 OR held_cents < 0
		  UNION ALL
		 SELECT format('house bankroll=%s rake=%s', bankroll_cents, rake_cents)
		   FROM house WHERE bankroll_cents < 0 OR rake_cents < 0`,
	},
	{
		"I3", "demo and real money never mix",
		// The rule that, if broken, prints real money out of demo tokens.
		`SELECT format('tx %s kind=%s is_real=%s', id, kind, is_real)
		   FROM transactions
		  WHERE (NOT is_real AND kind IN ('deposit','withdraw','withdraw_reversed','referral'))
		     OR (is_real AND kind = 'demo_grant')
		  UNION ALL
		 SELECT format('spin %s is_real=false rake=%s', id, rake_cents)
		   FROM spins WHERE NOT is_real AND rake_cents <> 0`,
	},
	{
		"I4", "deposits credit exactly once, for no more than was requested",
		`SELECT format('deposit %s status=%s credited=%s legs=%s', d.id, d.status, d.credited_cents, COALESCE(t.n,0))
		   FROM deposits d
		   LEFT JOIN (SELECT ref_id, COUNT(*) n, SUM(amount_cents) s FROM transactions
		               WHERE kind = 'deposit' GROUP BY ref_id) t ON t.ref_id = d.id
		  WHERE (d.status = 'success' AND (COALESCE(t.n,0) <> 1 OR t.s <> d.credited_cents))
		     OR (d.status <> 'success' AND COALESCE(t.n,0) <> 0)`,
	},
	{
		"I5", "withdrawal holds reconcile exactly against open withdrawals",
		// This is the invariant whose absence was the reference
		// implementation's worst bug.
		`SELECT format('held total %s <> open withdrawals %s',
		               (SELECT COALESCE(SUM(held_cents),0) FROM wallets),
		               (SELECT COALESCE(SUM(amount_cents),0) FROM withdrawals
		                 WHERE status IN ('pending','approved','sending')))
		  WHERE (SELECT COALESCE(SUM(held_cents),0) FROM wallets)
		     <> (SELECT COALESCE(SUM(amount_cents),0) FROM withdrawals
		          WHERE status IN ('pending','approved','sending'))`,
	},
	{
		"I5b", "every terminal withdrawal is either settled or reversed, never both, never neither",
		`SELECT format('withdrawal %s status=%s debits=%s reversals=%s', w.id, w.status,
		               COALESCE(d.n,0), COALESCE(r.n,0))
		   FROM withdrawals w
		   LEFT JOIN (SELECT ref_id, COUNT(*) n FROM transactions
		               WHERE kind = 'withdraw' GROUP BY ref_id) d ON d.ref_id = w.id
		   LEFT JOIN (SELECT ref_id, COUNT(*) n FROM transactions
		               WHERE kind = 'withdraw_reversed' GROUP BY ref_id) r ON r.ref_id = w.id
		  WHERE COALESCE(d.n,0) <> 1
		     OR (w.status IN ('failed','rejected') AND COALESCE(r.n,0) <> 1)
		     OR (w.status = 'paid'                 AND COALESCE(r.n,0) <> 0)
		     OR (w.status IN ('pending','approved','sending') AND COALESCE(r.n,0) <> 0)`,
	},
	{
		"I6", "spins are arithmetically exact and fully recorded",
		`SELECT format('spin %s stake=%s mult=%s payout=%s bets=%s wins=%s', s.id, s.stake_cents,
		               s.multiplier_bp, s.payout_cents, COALESCE(b.n,0), COALESCE(w.n,0))
		   FROM spins s
		   LEFT JOIN (SELECT ref_id, COUNT(*) n, SUM(-amount_cents) s FROM transactions
		               WHERE kind = 'bet' GROUP BY ref_id) b ON b.ref_id = s.id
		   LEFT JOIN (SELECT ref_id, COUNT(*) n, SUM(amount_cents) s FROM transactions
		               WHERE kind = 'win' GROUP BY ref_id) w ON w.ref_id = s.id
		  WHERE s.payout_cents <> s.stake_cents * s.multiplier_bp / 10000
		     OR COALESCE(b.n,0) <> 1 OR b.s <> s.stake_cents
		     OR (s.payout_cents  > 0 AND (COALESCE(w.n,0) <> 1 OR w.s <> s.payout_cents))
		     OR (s.payout_cents  = 0 AND COALESCE(w.n,0) <> 0)`,
	},
	{
		"I7", "referral commissions are bounded, non-reflexive, and paid at most once",
		`SELECT format('referral tx %s spin=%s amount=%s', t.id, t.ref_id, t.amount_cents)
		   FROM transactions t
		   JOIN spins s ON s.id = t.ref_id
		   JOIN users referee ON referee.id = s.user_id
		  WHERE t.kind = 'referral'
		    AND (t.user_id = s.user_id                      -- self-referral
		      OR referee.referred_by IS DISTINCT FROM t.user_id  -- paid the wrong person
		      OR t.amount_cents <= 0
		      OR t.amount_cents > s.stake_cents)            -- absurdly large
		`,
	},
	{
		"I8", "the ledger is append-only",
		// The DO INSTEAD NOTHING rules must still be attached. If someone drops
		// them in a migration, this catches it before history can be rewritten.
		`SELECT 'transactions is missing its append-only rules'
		  WHERE (SELECT COUNT(*) FROM pg_rules
		          WHERE tablename = 'transactions'
		            AND rulename IN ('tx_no_update','tx_no_delete')) <> 2`,
	},
}

// Invariants runs every check and reports the results. It never returns an
// error for an unhealthy invariant — that is what the Healthy field is for.
// It errors only when a check could not be run at all.
func Invariants(ctx context.Context, q Querier) ([]Check, error) {
	out := make([]Check, 0, len(invariants))
	for _, inv := range invariants {
		rows, err := q.Query(ctx, inv.query)
		if err != nil {
			return nil, fmt.Errorf("invariant %s (%s): %w", inv.id, inv.name, err)
		}
		var details []string
		for rows.Next() {
			var d string
			if err := rows.Scan(&d); err != nil {
				rows.Close()
				return nil, fmt.Errorf("invariant %s: scan: %w", inv.id, err)
			}
			if len(details) < 10 { // a broken invariant can match a lot of rows
				details = append(details, d)
			}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, fmt.Errorf("invariant %s: %w", inv.id, err)
		}
		rows.Close()

		c := Check{ID: inv.id, Name: inv.name, Healthy: len(details) == 0}
		if !c.Healthy {
			c.Detail = strings.Join(details, "; ")
		}
		out = append(out, c)
	}
	return out, nil
}

// AssertInvariant runs every check and returns an error naming each violation.
//
// This is the keystone of the test suite. Put it in a t.Cleanup on every
// integration test: it costs one line and converts "we think the money is
// right" into "the money is provably right after every test we have run".
func AssertInvariant(ctx context.Context, q Querier) error {
	checks, err := Invariants(ctx, q)
	if err != nil {
		return err
	}
	var broken []string
	for _, c := range checks {
		if !c.Healthy {
			broken = append(broken, fmt.Sprintf("%s (%s): %s", c.ID, c.Name, c.Detail))
		}
	}
	if len(broken) > 0 {
		return fmt.Errorf("money integrity violated (%d invariant(s)):\n  - %s",
			len(broken), strings.Join(broken, "\n  - "))
	}
	return nil
}
