/**
 * How to describe a spin result to a player, truthfully.
 *
 * This exists because "you won KES 10" is a lie when the bet was KES 10 and
 * the multiplier was 1x: the payout really is KES 10, but the balance did not
 * move. The gross payout and the net change are different numbers and the UI
 * must show both, or players will believe the maths is broken — and be right
 * to, because what they were shown did not match what their balance did.
 *
 * PURE. No DOM. Mirrors to `lib/game/outcome.dart`.
 */

import type { Cents, BasisPoints } from './types';

export type OutcomeKind =
  /** multiplier 0 — the stake is gone */
  | 'loss'
  /** payout exactly equals the stake — balance unchanged */
  | 'refund'
  /** payout exceeds the stake */
  | 'win';

export interface Outcome {
  kind: OutcomeKind;
  /** stake × multiplier. What the wheel paid. */
  payoutCents: Cents;
  /** payoutCents − stakeCents. What the balance actually did. */
  netCents: Cents;
  multiplierBp: BasisPoints;
  stakeCents: Cents;
}

/**
 * Derives the outcome, and asserts the payout matches the multiplier.
 *
 * The server is authoritative, so a mismatch here means the client and server
 * disagree about the arithmetic — which is worth surfacing loudly in
 * development rather than quietly rendering a wrong number.
 */
export function describeOutcome(
  stakeCents: Cents,
  multiplierBp: BasisPoints,
  payoutCents: Cents,
): Outcome {
  const expected = expectedPayout(stakeCents, multiplierBp);
  if (payoutCents !== expected) {
    throw new PayoutMismatchError(stakeCents, multiplierBp, payoutCents, expected);
  }

  const netCents = payoutCents - stakeCents;
  const kind: OutcomeKind =
    payoutCents === 0 ? 'loss' : netCents === 0 ? 'refund' : 'win';

  return { kind, payoutCents, netCents, multiplierBp, stakeCents };
}

/**
 * stake × multiplier, truncating exactly as Go and Postgres do.
 *
 * `Math.trunc` and not `Math.floor`: they differ for negatives, and although
 * no negative reaches here, matching the server's rule exactly is the point.
 * JS numbers are doubles, so this is exact only while stake × bp stays under
 * 2^53 — at the KES 50,000 ceiling the product is ~10^13, which is three
 * orders of magnitude inside that. isSafe() below pins it.
 */
export function expectedPayout(stakeCents: Cents, multiplierBp: BasisPoints): Cents {
  return Math.trunc((stakeCents * multiplierBp) / 10000);
}

/** True while the product is exactly representable as a JS number. */
export function isSafePayout(stakeCents: Cents, multiplierBp: BasisPoints): boolean {
  return stakeCents * multiplierBp <= Number.MAX_SAFE_INTEGER;
}

export class PayoutMismatchError extends Error {
  constructor(
    readonly stakeCents: Cents,
    readonly multiplierBp: BasisPoints,
    readonly got: Cents,
    readonly expected: Cents,
  ) {
    super(
      `payout mismatch: server said ${got} for ${stakeCents} × ${multiplierBp}bp, ` +
        `client computed ${expected}`,
    );
    this.name = 'PayoutMismatchError';
  }
}

/** "5x" as a whole number of times, for copy that reads naturally. */
export function multiplierTimes(bp: BasisPoints): number {
  return bp / 10000;
}
