/**
 * The stake ladder.
 *
 * Betting is SELECTION, not typing. A free-text amount field on a money
 * control invites typos in the one place a typo costs real money, needs
 * parsing, validation and clamping, and on a phone it summons a keyboard that
 * covers half the screen. A fixed ladder removes all of that: every value a
 * player can reach is a value we chose.
 *
 * The steppers move along this ladder rather than adding a fixed increment, so
 * "+" always lands on another preset and no arbitrary amount is reachable.
 *
 * PURE. Mirrors to `lib/game/stakes.dart`.
 */

import type { Cents } from './types';

/** Presets in cents. KES 5 up to KES 500. */
export const STAKE_LADDER: readonly Cents[] = [500, 1_000, 2_500, 5_000, 10_000, 50_000];

/**
 * The rung at or below `cents`, clamped into the ladder.
 * Used when a stake arrives from elsewhere (a clamp, a restored session) and
 * has to be shown on a control that only knows rungs.
 */
export function nearestRung(cents: Cents): number {
  let best = 0;
  for (let i = 0; i < STAKE_LADDER.length; i++) {
    if (STAKE_LADDER[i]! <= cents) best = i;
  }
  // Below the first rung, snap up to it rather than reporting -1.
  return best;
}

/** Exact rung index, or -1 when `cents` is not a preset. */
export function rungOf(cents: Cents): number {
  return STAKE_LADDER.indexOf(cents);
}

/**
 * The next rung up that is still affordable, or null when there is none.
 * `ceiling` is what the server would currently accept (balance and, for real
 * play, the bankroll cap).
 */
export function stepUp(cents: Cents, ceiling: Cents): Cents | null {
  const from = rungOf(cents) >= 0 ? rungOf(cents) : nearestRung(cents);
  for (let i = from + 1; i < STAKE_LADDER.length; i++) {
    if (STAKE_LADDER[i]! <= ceiling) return STAKE_LADDER[i]!;
  }
  return null;
}

/** The next rung down, or null when already at the bottom. */
export function stepDown(cents: Cents): Cents | null {
  const from = rungOf(cents) >= 0 ? rungOf(cents) : nearestRung(cents);
  return from > 0 ? STAKE_LADDER[from - 1]! : null;
}

/** Rungs that fit inside the ceiling. Anything above is shown disabled. */
export function affordableRungs(ceiling: Cents): readonly Cents[] {
  return STAKE_LADDER.filter((c) => c <= ceiling);
}

/**
 * The rung to start on for a given ceiling.
 *
 * Deliberately the SECOND rung (KES 10) when affordable rather than the
 * largest: a wheel app should not open with the biggest bet preselected.
 */
export function defaultStake(ceiling: Cents): Cents {
  const affordable = affordableRungs(ceiling);
  if (affordable.length === 0) return STAKE_LADDER[0]!;
  return affordable[Math.min(1, affordable.length - 1)]!;
}
