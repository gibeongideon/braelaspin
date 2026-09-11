/**
 * Client-side payout arithmetic.
 *
 * The server is authoritative — these tests do not decide anything. They prove
 * the client recomputes the same number the server sent, so a disagreement is
 * caught and surfaced rather than rendered as if it were correct.
 */

import { describe, it, expect } from 'vitest';
import {
  describeOutcome, expectedPayout, isSafePayout, balanceAfterStake,
  multiplierTimes, PayoutMismatchError,
} from './outcome';

const WHEEL_BP = [0, 10_000, 20_000, 50_000, 100_000, 500_000, 2_000_000];

describe('payout = bet × multiplier', () => {
  it('holds for every multiplier on the wheel', () => {
    const stakes = [500, 1_000, 2_500, 10_000, 123_456, 5_000_000];
    for (const bp of WHEEL_BP) {
      for (const stake of stakes) {
        expect(expectedPayout(stake, bp)).toBe(stake * (bp / 10_000));
      }
    }
  });

  it('the brief\'s example: KES 10 at 5x pays KES 50, net KES 40', () => {
    const o = describeOutcome(1_000, 50_000, 5_000);
    expect(o.payoutCents).toBe(5_000);
    expect(o.netCents).toBe(4_000);
    expect(o.kind).toBe('win');
  });

  it('1x is a refund, not a win — the balance does not move', () => {
    const o = describeOutcome(1_000, 10_000, 1_000);
    expect(o.payoutCents).toBe(1_000);
    expect(o.netCents).toBe(0);
    // Calling this a "win" is what makes players think the maths is broken.
    expect(o.kind).toBe('refund');
  });

  it('0x is a loss of exactly the stake', () => {
    const o = describeOutcome(1_000, 0, 0);
    expect(o.payoutCents).toBe(0);
    expect(o.netCents).toBe(-1_000);
    expect(o.kind).toBe('loss');
  });

  it('200x on the maximum stake', () => {
    const o = describeOutcome(5_000_000, 2_000_000, 1_000_000_000);
    expect(o.payoutCents).toBe(5_000_000 * 200);
    expect(o.netCents).toBe(1_000_000_000 - 5_000_000);
    expect(o.kind).toBe('win');
  });
});

describe('disagreement with the server is caught, never rendered', () => {
  it('throws when the payout does not match the multiplier', () => {
    // Server says 5x on a KES 10 bet paid KES 60. It did not.
    expect(() => describeOutcome(1_000, 50_000, 6_000)).toThrow(PayoutMismatchError);
  });

  it('carries both numbers so the mismatch can be diagnosed', () => {
    try {
      describeOutcome(1_000, 50_000, 6_000);
      expect.unreachable();
    } catch (e) {
      expect(e).toBeInstanceOf(PayoutMismatchError);
      const err = e as PayoutMismatchError;
      expect(err.got).toBe(6_000);
      expect(err.expected).toBe(5_000);
    }
  });

  it('accepts an exact match', () => {
    expect(() => describeOutcome(1_000, 50_000, 5_000)).not.toThrow();
  });
});

describe('truncation matches Go and Postgres', () => {
  it('truncates toward zero', () => {
    // These only arise if a fractional multiplier is ever added; pinned so the
    // three implementations cannot silently diverge.
    expect(expectedPayout(333, 15_000)).toBe(499); // 499.5
    expect(expectedPayout(1, 15_000)).toBe(1);     // 1.5
    expect(expectedPayout(1, 5_000)).toBe(0);      // 0.5
    expect(expectedPayout(7, 33_333)).toBe(23);    // 23.33
    expect(expectedPayout(99, 12_345)).toBe(122);  // 122.2
  });

  it('stays inside exact-integer range at the configured ceiling', () => {
    expect(isSafePayout(5_000_000, 2_000_000)).toBe(true);
    // 10^13 vs 2^53 ≈ 9×10^15 — three orders of magnitude of headroom.
    expect(5_000_000 * 2_000_000).toBeLessThan(Number.MAX_SAFE_INTEGER);
  });
});

describe('multiplierTimes', () => {
  it('converts basis points to the number a player says', () => {
    expect(multiplierTimes(50_000)).toBe(5);
    expect(multiplierTimes(2_000_000)).toBe(200);
    expect(multiplierTimes(0)).toBe(0);
  });
});

// ── the two-phase balance the player actually sees ─────────────────────────
//
// Requirement: clicking spin reduces the balance by the bet immediately; when
// the wheel stops, a win adds the payout and a loss leaves it unchanged.
//
// Phase 1 is balanceAfterStake(finalBalance, payout) and phase 2 is
// finalBalance, both taken from the server's own response rather than computed
// from a local value that could be stale.
describe('balance sequence: bet leaves, then payout lands', () => {
  const START = 500_000; // KES 5,000
  const STAKE = 1_000;   // KES 10

  /** What the server returns, given a multiplier. */
  function serverResult(multBp: number) {
    const payout = expectedPayout(STAKE, multBp);
    return { payout_cents: payout, balance_cents: START - STAKE + payout };
  }

  it('a LOSS: bet leaves, nothing comes back', () => {
    const r = serverResult(0);
    const afterBet = balanceAfterStake(r.balance_cents, r.payout_cents);

    expect(afterBet).toBe(START - STAKE);        // 499,000 — the bet is gone
    expect(r.balance_cents).toBe(START - STAKE); // settling changes nothing
    expect(r.balance_cents - afterBet).toBe(0);
  });

  it('a WIN at 5x: bet leaves, then the payout lands', () => {
    const r = serverResult(50_000);
    const afterBet = balanceAfterStake(r.balance_cents, r.payout_cents);

    expect(afterBet).toBe(499_000);              // -KES 10 on click
    expect(r.balance_cents).toBe(504_000);       // +KES 50 when the wheel stops
    expect(r.balance_cents - afterBet).toBe(5_000);
    // Net over the whole spin is +KES 40.
    expect(r.balance_cents - START).toBe(4_000);
  });

  it('a REFUND at 1x: bet leaves, the same amount comes back', () => {
    const r = serverResult(10_000);
    const afterBet = balanceAfterStake(r.balance_cents, r.payout_cents);

    expect(afterBet).toBe(499_000);        // the bet really does leave
    expect(r.balance_cents).toBe(START);   // and returns in full
    expect(r.balance_cents - START).toBe(0);
  });

  it('a WIN at 200x', () => {
    const r = serverResult(2_000_000);
    const afterBet = balanceAfterStake(r.balance_cents, r.payout_cents);
    expect(afterBet).toBe(499_000);
    expect(r.balance_cents).toBe(499_000 + 200_000);
  });

  it('phase 1 always equals start − stake, whatever the multiplier', () => {
    for (const bp of [0, 10_000, 20_000, 50_000, 100_000, 500_000, 2_000_000]) {
      const r = serverResult(bp);
      expect(balanceAfterStake(r.balance_cents, r.payout_cents)).toBe(START - STAKE);
    }
  });

  it('phase 2 minus phase 1 is exactly the payout, every time', () => {
    for (const bp of [0, 10_000, 20_000, 50_000, 100_000, 500_000, 2_000_000]) {
      const r = serverResult(bp);
      const afterBet = balanceAfterStake(r.balance_cents, r.payout_cents);
      expect(r.balance_cents - afterBet).toBe(r.payout_cents);
    }
  });
});
