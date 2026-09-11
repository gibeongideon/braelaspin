import { describe, it, expect } from 'vitest';
import {
  STAKE_LADDER, nearestRung, rungOf, stepUp, stepDown,
  affordableRungs, defaultStake,
} from './stakes';

describe('stake ladder', () => {
  it('is ascending, positive, and has no duplicates', () => {
    for (let i = 1; i < STAKE_LADDER.length; i++) {
      expect(STAKE_LADDER[i]!).toBeGreaterThan(STAKE_LADDER[i - 1]!);
    }
    expect(STAKE_LADDER[0]).toBeGreaterThan(0);
    expect(new Set(STAKE_LADDER).size).toBe(STAKE_LADDER.length);
  });

  it('every rung is a whole number of shillings', () => {
    // A preset of 1234 cents would render as "KES 12.34" on a bet chip.
    for (const c of STAKE_LADDER) expect(c % 100).toBe(0);
  });

  it('steps up only to affordable rungs', () => {
    const ceiling = 10_000;
    expect(stepUp(500, ceiling)).toBe(1_000);
    expect(stepUp(5_000, ceiling)).toBe(10_000);
    // 50_000 is above the ceiling, so there is nowhere left to go.
    expect(stepUp(10_000, ceiling)).toBeNull();
  });

  it('steps down to the floor and then stops', () => {
    expect(stepDown(1_000)).toBe(500);
    expect(stepDown(500)).toBeNull();
  });

  it('steps are reversible', () => {
    const ceiling = 50_000;
    for (const c of STAKE_LADDER.slice(0, -1)) {
      const up = stepUp(c, ceiling)!;
      expect(stepDown(up)).toBe(c);
    }
  });

  it('recovers from a stake that is not a preset', () => {
    // e.g. clamped to an odd bankroll ceiling.
    expect(nearestRung(7_777)).toBe(rungOf(5_000));
    expect(stepUp(7_777, 50_000)).toBe(10_000);
    expect(stepDown(7_777)).toBe(2_500);
  });

  it('snaps up when below the floor rather than going negative', () => {
    expect(nearestRung(1)).toBe(0);
    expect(stepDown(1)).toBeNull();
  });

  it('reports what a ceiling affords', () => {
    expect(affordableRungs(0)).toEqual([]);
    expect(affordableRungs(499)).toEqual([]);
    expect(affordableRungs(500)).toEqual([500]);
    expect(affordableRungs(1_000_000)).toEqual([...STAKE_LADDER]);
  });

  it('opens on the second rung, never the largest', () => {
    expect(defaultStake(50_000)).toBe(1_000);   // KES 10, not KES 500
    expect(defaultStake(1_000_000)).toBe(1_000);
    expect(defaultStake(500)).toBe(500);        // only one rung fits
    expect(defaultStake(0)).toBe(500);          // nothing fits: the floor
  });
});
