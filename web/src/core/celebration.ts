/**
 * How loudly to celebrate a result.
 *
 * A win of 1x and a win of 200x are the same event to the ledger and utterly
 * different events to a player. Tiering the response is most of what makes a
 * game feel like a game rather than a form submission.
 *
 * The classification lives here, pure and tested, so the web and Flutter
 * clients celebrate identically and so the thresholds are one table rather
 * than conditionals sprinkled through a view.
 *
 * Restraint on the loss tier is deliberate. Making a loss loud is how you
 * build something that feels predatory; it gets a short, quiet acknowledgement
 * and gets out of the way.
 */

import type { BasisPoints } from './types';
import type { OutcomeKind } from './outcome';

export type Tier = 'loss' | 'refund' | 'small' | 'big' | 'huge' | 'jackpot';

export interface Celebration {
  tier: Tier;
  /** Banner text. Short — it sits above a large number. */
  title: string;
  /** Confetti particle count. 0 means none. */
  confetti: number;
  /** Draw rotating rays behind the card. */
  rays: boolean;
  /** Count the amount up rather than showing it outright. */
  countUp: boolean;
  /** Milliseconds on screen before auto-dismiss. */
  dismissMs: number;
  /** Rising chime notes in Hz; empty for silence. */
  chime: readonly number[];
}

// Multiplier thresholds, in basis points, for the three win tiers.
const BIG_BP = 50_000;      // 5x
const HUGE_BP = 500_000;    // 50x
const JACKPOT_BP = 2_000_000; // 200x

export function celebrationFor(kind: OutcomeKind, multiplierBp: BasisPoints): Celebration {
  if (kind === 'loss') {
    return {
      tier: 'loss', title: 'No win', confetti: 0, rays: false,
      countUp: false, dismissMs: 1700, chime: [],
    };
  }
  if (kind === 'refund') {
    return {
      tier: 'refund', title: 'Bet returned', confetti: 0, rays: false,
      countUp: false, dismissMs: 2000, chime: [520],
    };
  }
  if (multiplierBp >= JACKPOT_BP) {
    return {
      tier: 'jackpot', title: 'JACKPOT!', confetti: 90, rays: true,
      countUp: true, dismissMs: 5200, chime: [523, 659, 784, 1047, 1319],
    };
  }
  if (multiplierBp >= HUGE_BP) {
    return {
      tier: 'huge', title: 'HUGE WIN!', confetti: 55, rays: true,
      countUp: true, dismissMs: 4200, chime: [523, 659, 784, 1047],
    };
  }
  if (multiplierBp >= BIG_BP) {
    return {
      tier: 'big', title: 'BIG WIN!', confetti: 32, rays: true,
      countUp: true, dismissMs: 3600, chime: [523, 659, 784],
    };
  }
  return {
    tier: 'small', title: 'You won', confetti: 14, rays: false,
    countUp: true, dismissMs: 3000, chime: [587, 784],
  };
}

/**
 * Confetti palette per tier. Warm for the mid tiers, gold for huge, and the
 * jackpot borrows the 200x tile's pink so the card and the wheel agree.
 */
export function confettiColours(tier: Tier): readonly string[] {
  switch (tier) {
    case 'jackpot': return ['#ff5ea8', '#ffd447', '#ff8a3d', '#8be9fd', '#ffffff'];
    case 'huge':    return ['#ffd447', '#ffa45c', '#ffffff', '#ffec99'];
    case 'big':     return ['#ff8a3d', '#ffd447', '#ffffff'];
    default:        return ['#4fbf5f', '#ffffff', '#9be7a6'];
  }
}
