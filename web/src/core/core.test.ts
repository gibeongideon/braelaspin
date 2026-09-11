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
  rotationAt, tickTimes, segmentArcDeg, boundaryAngleDeg,
  rotationWithSettle, settleOvershootDeg, restingRotation, easeInOutCubic,
  SETTLE_FRACTION, SPIN_REVOLUTIONS, SPIN_DURATION_MS,
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

// ── the pointer must sit on the segment the server chose ──────────────────
//
// REGRESSION: the renderer once defined its own tile geometry, centring
// segment 1 on the pointer while indicatedSegment treated wheel-local 0 as
// segment 1's leading edge. The two disagreed by half a segment, so the
// pointer showed segment k+1 for EVERY result — a player winning 5x saw the
// pointer on 10x. The maths was self-consistent and every test passed; nothing
// checked the drawing against it.
//
// These tests model what the renderer draws, using the same exported geometry
// the renderer now consumes, and assert it agrees with indicatedSegment.
describe('pointer alignment: drawn tiles vs the indicated segment', () => {
  const mod360 = (d: number) => ((d % 360) + 360) % 360;

  /**
   * Which tile the pointer physically overlaps, derived from the drawn arcs.
   *
   * The pointer is at canvas -90deg; a feature drawn at canvas `a` appears at
   * `a + rotation`, so the one under the pointer has a = -90 - rotation.
   */
  function drawnSegmentUnderPointer(rotation: number, n: number): number {
    const a = -90 - rotation;
    for (let seg = 1; seg <= n; seg++) {
      const { startDeg } = segmentArcDeg(seg, n);
      if (mod360(a - startDeg) < segmentAngle(n)) return seg;
    }
    throw new Error('no tile under the pointer — the arcs do not tile the circle');
  }

  it('the drawn tile under the pointer IS the segment the server chose', () => {
    for (let seg = 1; seg <= N; seg++) {
      for (let trial = 0; trial < 50; trial++) {
        const to = targetAngle(Math.random() * 5000, seg, N);
        expect(drawnSegmentUnderPointer(to, N)).toBe(seg);
      }
    }
  });

  it('agrees with indicatedSegment at every rotation, not just landing points', () => {
    for (let deg = 0; deg < 720; deg += 0.5) {
      expect(drawnSegmentUnderPointer(deg, N)).toBe(indicatedSegment(deg, N));
    }
  });

  it('the arcs tile the circle exactly, with no gap or overlap', () => {
    const S = segmentAngle(N);
    for (let seg = 1; seg <= N; seg++) {
      const arc = segmentArcDeg(seg, N);
      expect(arc.endDeg - arc.startDeg).toBeCloseTo(S, 10);
      expect(arc.midDeg).toBeCloseTo((arc.startDeg + arc.endDeg) / 2, 10);
      // Each arc begins exactly where the previous one ended.
      if (seg > 1) {
        expect(arc.startDeg).toBeCloseTo(segmentArcDeg(seg - 1, N).endDeg, 10);
      }
    }
    const total = segmentArcDeg(N, N).endDeg - segmentArcDeg(1, N).startDeg;
    expect(total).toBeCloseTo(360, 10);
  });

  it('segment 1 starts at the pointer, so rotation 0 reads segment 1', () => {
    expect(segmentArcDeg(1, N).startDeg).toBe(-90); // canvas 12 o'clock
    expect(indicatedSegment(0, N)).toBe(1);
    expect(boundaryAngleDeg(1, N)).toBe(-90);
  });

  it('pins sit on the boundaries between tiles', () => {
    for (let seg = 1; seg <= N; seg++) {
      expect(boundaryAngleDeg(seg, N)).toBeCloseTo(segmentArcDeg(seg, N).startDeg, 10);
    }
  });

  it('rejects an out-of-range segment', () => {
    expect(() => segmentArcDeg(0, N)).toThrow(RangeError);
    expect(() => segmentArcDeg(N + 1, N)).toThrow(RangeError);
  });
});

// ── overshoot and settle ───────────────────────────────────────────────────
//
// A physical wheel carries past the last peg and falls back. That is most of
// what makes it feel real — and it is also the riskiest visual flourish in the
// app, because if the wheel overshoots into the NEXT segment then for a few
// frames the pointer sits on a prize the player did not win.
//
// These tests exist to make that impossible, not merely unlikely.
describe('settle physics never shows the wrong prize', () => {
  const mod360 = (d: number) => ((d % 360) + 360) % 360;

  it('ends at EXACTLY the landing angle, so the assertion still holds', () => {
    for (let seg = 1; seg <= N; seg++) {
      const from = Math.random() * 3000;
      const to = targetAngle(from, seg, N);
      // Exact equality, not approximate: the final frame must be the angle
      // that indicatedSegment was verified against.
      expect(rotationWithSettle(from, to, SPIN_DURATION_MS, N)).toBe(to);
      expect(rotationWithSettle(from, to, SPIN_DURATION_MS + 500, N)).toBe(to);
    }
  });

  it('the pointer stays on the winning segment through the ENTIRE settle', () => {
    for (let seg = 1; seg <= N; seg++) {
      for (let trial = 0; trial < 20; trial++) {
        const from = Math.random() * 3000;
        const to = targetAngle(from, seg, N);
        const brakeMs = SPIN_DURATION_MS * (1 - SETTLE_FRACTION);

        // Sample every 5ms of the settle phase — the only window where the
        // wheel is past its landing angle.
        for (let t = brakeMs; t <= SPIN_DURATION_MS; t += 5) {
          const r = rotationWithSettle(from, to, t, N);
          expect(indicatedSegment(r, N)).toBe(seg);
        }
      }
    }
  });

  it('the overshoot is bounded by the room inside the segment', () => {
    const S = segmentAngle(N);
    for (let seg = 1; seg <= N; seg++) {
      for (let trial = 0; trial < 100; trial++) {
        const to = targetAngle(0, seg, N);
        const room = mod360(-to) % S; // distance to the leading edge
        const over = settleOvershootDeg(to, N);
        expect(over).toBeGreaterThanOrEqual(0);
        // Strictly inside the room, or the pointer would cross the boundary.
        expect(over).toBeLessThan(room + 1e-9);
        expect(over).toBeLessThanOrEqual(S * 0.4);
      }
    }
  });

  it('actually overshoots — the flourish is present, not a no-op', () => {
    const to = targetAngle(0, 5, N);
    const peak = rotationWithSettle(0, to, SPIN_DURATION_MS * (1 - SETTLE_FRACTION), N);
    expect(peak).toBeGreaterThan(to);
    // toBeCloseTo, not toBe: `peak` comes out of a multiply-add interpolation,
    // so it differs from the overshoot in the last couple of float bits. The
    // value that must be exact is the FINAL angle, asserted above.
    expect(peak - to).toBeCloseTo(settleOvershootDeg(to, N), 9);
    expect(settleOvershootDeg(to, N)).toBeGreaterThan(1); // a visible flourish
  });

  it('is monotonic increasing up to the peak, then decreasing', () => {
    const to = targetAngle(0, 9, N);
    const brake = SPIN_DURATION_MS * (1 - SETTLE_FRACTION);
    let prev = -Infinity;
    for (let t = 0; t <= brake; t += 50) {
      const r = rotationWithSettle(0, to, t, N);
      expect(r).toBeGreaterThanOrEqual(prev - 1e-9);
      prev = r;
    }
    prev = Infinity;
    for (let t = brake; t <= SPIN_DURATION_MS; t += 10) {
      const r = rotationWithSettle(0, to, t, N);
      expect(r).toBeLessThanOrEqual(prev + 1e-9);
      prev = r;
    }
  });

  it('clamps at both ends', () => {
    const to = targetAngle(100, 3, N);
    expect(rotationWithSettle(100, to, -1, N)).toBe(100);
    expect(rotationWithSettle(100, to, 0, N)).toBe(100);
  });

  it('easeInOutCubic is a proper 0..1 easing', () => {
    expect(easeInOutCubic(0)).toBe(0);
    expect(easeInOutCubic(1)).toBe(1);
    expect(easeInOutCubic(0.5)).toBeCloseTo(0.5, 10);
  });

  it('the resting rotation centres a tile under the pointer', () => {
    const rest = restingRotation(N);
    const S = segmentAngle(N);
    // Half a segment in, i.e. the middle of the tile.
    expect(mod360(-rest) % S).toBeCloseTo(S / 2, 10);
    // And it is still a valid, unambiguous reading.
    expect(indicatedSegment(rest, N)).toBeGreaterThanOrEqual(1);
    expect(indicatedSegment(rest, N)).toBeLessThanOrEqual(N);
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
