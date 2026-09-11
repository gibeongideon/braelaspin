/**
 * Tests for the pure core. No DOM, no browser, no server.
 *
 * The wheel maths tests matter most: they are the same assertions the Flutter
 * client will need, and getting the congruence wrong means showing a player a
 * prize they did not win.
 */

import { describe, it, expect } from 'vitest';
import {
  targetAngle, indicatedSegment, segmentAngle, easeOutQuart,
  rotationAt, tickTimes, SPIN_REVOLUTIONS, SPIN_DURATION_MS,
} from './wheel';
import { formatKes, formatSigned, parseShillings, formatMultiplier, formatWhen } from './money';
import { normalisePhone, prettyPhone, maskPhone } from './phone';
import { Signal, derived } from './signal';
import { userMessageFor, ApiError } from './errors';

const N = 12;

describe('wheel geometry', () => {
  it('lands on exactly the segment the server chose, for every segment', () => {
    for (let seg = 1; seg <= N; seg++) {
      for (let trial = 0; trial < 200; trial++) {
        const from = Math.random() * 5000;
        const to = targetAngle(from, seg, N);
        expect(indicatedSegment(to, N)).toBe(seg);
      }
    }
  });

  it('always rotates forward by at least the full revolution count', () => {
    for (let seg = 1; seg <= N; seg++) {
      const from = Math.random() * 1000;
      const to = targetAngle(from, seg, N);
      expect(to).toBeGreaterThan(from + SPIN_REVOLUTIONS * 360);
      expect(to).toBeLessThanOrEqual(from + (SPIN_REVOLUTIONS + 1) * 360);
    }
  });

  it('never stops hard on a boundary', () => {
    const S = segmentAngle(N);
    for (let seg = 1; seg <= N; seg++) {
      for (let i = 0; i < 100; i++) {
        const to = targetAngle(0, seg, N);
        const within = ((((-to % 360) + 360) % 360) % S);
        expect(within).toBeGreaterThan(0.5);
        expect(within).toBeLessThan(S - 0.5);
      }
    }
  });

  it('is monotonic across consecutive spins — no reset-to-zero snap', () => {
    let angle = 0;
    for (let i = 0; i < 30; i++) {
      const next = targetAngle(angle, (i % N) + 1, N);
      expect(next).toBeGreaterThan(angle);
      angle = next;
    }
  });

  it('rejects an out-of-range segment rather than animating nonsense', () => {
    expect(() => targetAngle(0, 0, N)).toThrow(RangeError);
    expect(() => targetAngle(0, N + 1, N)).toThrow(RangeError);
  });

  it('eases from start to finish', () => {
    expect(easeOutQuart(0)).toBe(0);
    expect(easeOutQuart(1)).toBe(1);
    // Ease-OUT: most of the distance is covered early.
    expect(easeOutQuart(0.5)).toBeGreaterThan(0.9);
  });

  it('rotationAt is clamped at both ends', () => {
    expect(rotationAt(10, 100, -5)).toBe(10);
    expect(rotationAt(10, 100, SPIN_DURATION_MS + 1)).toBe(100);
    const mid = rotationAt(0, 100, SPIN_DURATION_MS / 2);
    expect(mid).toBeGreaterThan(0);
    expect(mid).toBeLessThan(100);
  });

  it('produces tick times inside the spin, in order, and bounded in count', () => {
    const to = targetAngle(0, 7, N);
    const ticks = tickTimes(0, to, N);
    expect(ticks.length).toBeGreaterThan(0);
    expect(ticks.length).toBeLessThanOrEqual(220);
    for (let i = 1; i < ticks.length; i++) {
      expect(ticks[i]).toBeGreaterThanOrEqual(ticks[i - 1]);
    }
    expect(ticks[0]).toBeGreaterThanOrEqual(0);
    expect(ticks[ticks.length - 1]).toBeLessThanOrEqual(SPIN_DURATION_MS);
  });
});

describe('money formatting', () => {
  it('formats cents as KES', () => {
    expect(formatKes(0)).toBe('KES 0');
    expect(formatKes(500)).toBe('KES 5');
    expect(formatKes(123450)).toBe('KES 1,234.50');
    expect(formatKes(100000000)).toBe('KES 1,000,000');
    expect(formatKes(-2500)).toBe('-KES 25');
    expect(formatKes(1234, { symbol: false })).toBe('12.34');
  });

  it('signs ledger amounts', () => {
    expect(formatSigned(500)).toBe('+KES 5');
    expect(formatSigned(-500)).toBe('-KES 5');
    expect(formatSigned(0)).toBe('KES 0');
  });

  it('parses shillings into cents, and rejects junk rather than returning 0', () => {
    expect(parseShillings('10')).toBe(1000);
    expect(parseShillings('10.50')).toBe(1050);
    expect(parseShillings('1,234')).toBe(123400);
    expect(parseShillings('KES 25')).toBe(2500);
    // A silent 0 on a stake field is the difference between a rejection and a
    // wrong bet, so these must all be null.
    expect(parseShillings('')).toBeNull();
    expect(parseShillings('abc')).toBeNull();
    expect(parseShillings('-5')).toBeNull();
    expect(parseShillings('1.234')).toBeNull();
    expect(parseShillings('1e3')).toBeNull();
  });

  it('formats multipliers', () => {
    expect(formatMultiplier(0)).toBe('—');
    expect(formatMultiplier(10000)).toBe('1x');
    expect(formatMultiplier(2000000)).toBe('200x');
  });

  it('formats relative times', () => {
    const now = new Date('2026-09-11T12:00:00Z');
    expect(formatWhen('2026-09-11T11:59:50Z', now)).toBe('just now');
    expect(formatWhen('2026-09-11T11:30:00Z', now)).toBe('30 min ago');
    expect(formatWhen('2026-09-11T09:00:00Z', now)).toBe('3 h ago');
    expect(formatWhen('not a date', now)).toBe('');
  });
});

describe('phone normalisation (mirrors internal/auth/phone.go)', () => {
  it('accepts every Kenyan spelling', () => {
    for (const input of [
      '0712345678', '+254712345678', '254712345678', '712345678',
      '0712 345 678', '+254 712 345 678', '254-712-345-678', '(0712) 345-678',
    ]) {
      const r = normalisePhone(input);
      expect(r.ok, `${input} should be valid`).toBe(true);
      if (r.ok) expect(r.phone).toBe('254712345678');
    }
  });

  it('accepts the 01x range', () => {
    const r = normalisePhone('0112345678');
    expect(r.ok).toBe(true);
    if (r.ok) expect(r.phone).toBe('254112345678');
  });

  it('rejects anything else', () => {
    for (const bad of [
      '', '   ', '0812345678', '0212345678', '071234567', '07123456789',
      'abcdefghij', '0712345abc', '254712345678-need_update', '+1 555 123 4567',
    ]) {
      expect(normalisePhone(bad).ok, `${bad} should be rejected`).toBe(false);
    }
  });

  it('is idempotent, so one person cannot become two accounts', () => {
    const once = normalisePhone('0712345678');
    expect(once.ok).toBe(true);
    if (!once.ok) return;
    const twice = normalisePhone(once.phone);
    expect(twice.ok && twice.phone).toBe(once.phone);
  });

  it('formats and masks', () => {
    expect(prettyPhone('254712345678')).toBe('0712 345 678');
    expect(maskPhone('254712345678')).toBe('2547*****678');
  });
});

describe('Signal (the ValueNotifier equivalent)', () => {
  it('notifies on change and not on an identical set', () => {
    const s = new Signal(1);
    let calls = 0;
    s.subscribe(() => calls++, false);
    s.value = 2;
    s.value = 2;
    expect(calls).toBe(1);
  });

  it('fires immediately on subscribe by default', () => {
    const s = new Signal('a');
    let seen = '';
    s.subscribe((v) => { seen = v; });
    expect(seen).toBe('a');
  });

  it('unsubscribes cleanly, including from inside a listener', () => {
    const s = new Signal(0);
    let calls = 0;
    const off = s.subscribe(() => { calls++; off(); }, false);
    s.value = 1;
    s.value = 2;
    expect(calls).toBe(1);
  });

  it('derives from several sources', () => {
    const a = new Signal(1);
    const b = new Signal(2);
    const sum = derived([a as Signal<unknown>, b as Signal<unknown>], () => a.value + b.value);
    expect(sum.value).toBe(3);
    a.value = 10;
    expect(sum.value).toBe(12);
  });
});

describe('error copy', () => {
  it('maps server codes to user-facing words', () => {
    expect(userMessageFor('insufficient_funds')).toContain("don't have enough");
    expect(userMessageFor('phone_taken')).toContain('already registered');
  });

  it('includes the wait time when rate limited', () => {
    expect(userMessageFor('rate_limited', { retry_after_seconds: 30 })).toContain('30 seconds');
    expect(userMessageFor('rate_limited', { retry_after_seconds: 300 })).toContain('5 minutes');
  });

  it('falls back by kind for an unknown code', () => {
    expect(userMessageFor('something_new', {}, 'server')).toContain('our side');
  });

  it('knows which failures are safe to retry', () => {
    expect(new ApiError({ kind: 'offline', code: 'offline' }).definitelyDidNotHappen).toBe(true);
    // A timeout is the dangerous one: the bet MAY have been taken.
    expect(new ApiError({ kind: 'timeout', code: 'timeout' }).definitelyDidNotHappen).toBe(false);
  });
});
