/// The Dart core suite.
///
/// These are the SAME assertions as `web/src/core/*.test.ts`, deliberately.
/// The architecture's whole claim is that core/ ports between the two clients
/// without changing behaviour; running matching tests on both sides is how
/// that claim is checked rather than assumed.
library;

import 'dart:math' as math;

import 'package:flutter_test/flutter_test.dart';

import 'package:braelaspin/core/celebration.dart';
import 'package:braelaspin/core/money.dart';
import 'package:braelaspin/core/outcome.dart';
import 'package:braelaspin/core/phone.dart';
import 'package:braelaspin/core/spin_math.dart';
import 'package:braelaspin/core/stakes.dart';

const int n = 12;
double mod360(double d) => ((d % 360) + 360) % 360;

void main() {
  // ── the payout identity ──────────────────────────────────────────────────
  group('payout = bet x multiplier', () {
    test('holds for every multiplier on the wheel', () {
      const bps = [0, 10000, 20000, 50000, 100000, 500000, 2000000];
      const stakes = [500, 1000, 2500, 10000, 123456, 5000000];
      for (final bp in bps) {
        for (final stake in stakes) {
          expect(expectedPayout(stake, bp), stake * (bp ~/ 10000));
        }
      }
    });

    test("the brief's example: KES 10 at 5x pays KES 50, net KES 40", () {
      final o = describeOutcome(1000, 50000, 5000);
      expect(o.payoutCents, 5000);
      expect(o.netCents, 4000);
      expect(o.kind, OutcomeKind.win);
    });

    test('1x is a refund, not a win — the balance does not move', () {
      final o = describeOutcome(1000, 10000, 1000);
      expect(o.netCents, 0);
      expect(o.kind, OutcomeKind.refund);
    });

    test('0x is a loss of exactly the stake', () {
      final o = describeOutcome(1000, 0, 0);
      expect(o.netCents, -1000);
      expect(o.kind, OutcomeKind.loss);
    });

    test('a disagreement with the server is caught, never rendered', () {
      expect(() => describeOutcome(1000, 50000, 6000), throwsA(isA<PayoutMismatch>()));
    });

    test('truncation matches Go and Postgres', () {
      expect(expectedPayout(333, 15000), 499);
      expect(expectedPayout(1, 15000), 1);
      expect(expectedPayout(1, 5000), 0);
      expect(expectedPayout(7, 33333), 23);
      expect(expectedPayout(99, 12345), 122);
    });

    test('no overflow at the configured ceiling', () {
      // Dart ints are 64-bit, same as Go's int64.
      expect(expectedPayout(5000000, 2000000), 5000000 * 200);
    });
  });

  // ── the two-phase balance ────────────────────────────────────────────────
  group('balance sequence: bet leaves, then payout lands', () {
    const start = 500000;
    const stakeC = 1000;

    ({int payout, int balance}) server(int multBp) {
      final p = expectedPayout(stakeC, multBp);
      return (payout: p, balance: start - stakeC + p);
    }

    test('a LOSS: bet leaves, nothing comes back', () {
      final r = server(0);
      expect(balanceAfterStake(r.balance, r.payout), start - stakeC);
      expect(r.balance, start - stakeC);
    });

    test('a WIN at 5x: bet leaves, then the payout lands', () {
      final r = server(50000);
      expect(balanceAfterStake(r.balance, r.payout), 499000);
      expect(r.balance, 504000);
    });

    test('phase 1 always equals start - stake, whatever the multiplier', () {
      for (final bp in [0, 10000, 20000, 50000, 100000, 500000, 2000000]) {
        final r = server(bp);
        expect(balanceAfterStake(r.balance, r.payout), start - stakeC);
      }
    });
  });

  // ── wheel geometry ───────────────────────────────────────────────────────
  group('wheel geometry', () {
    test('lands on exactly the segment the server chose', () {
      final rng = math.Random(42);
      for (var seg = 1; seg <= n; seg++) {
        for (var i = 0; i < 200; i++) {
          final to = targetAngle(rng.nextDouble() * 5000, seg, n, rng: rng);
          expect(indicatedSegment(to, n), seg);
        }
      }
    });

    test('always rotates forward by at least the revolution count', () {
      final rng = math.Random(7);
      for (var seg = 1; seg <= n; seg++) {
        final from = rng.nextDouble() * 1000;
        final to = targetAngle(from, seg, n, rng: rng);
        expect(to, greaterThan(from + kSpinRevolutions * 360));
        expect(to, lessThanOrEqualTo(from + (kSpinRevolutions + 1) * 360));
      }
    });

    test('is monotonic across consecutive spins — no reset-to-zero snap', () {
      final rng = math.Random(3);
      var angle = 0.0;
      for (var i = 0; i < 30; i++) {
        final next = targetAngle(angle, (i % n) + 1, n, rng: rng);
        expect(next, greaterThan(angle));
        angle = next;
      }
    });

    test('rejects an out-of-range segment', () {
      expect(() => targetAngle(0, 0, n), throwsRangeError);
      expect(() => targetAngle(0, n + 1, n), throwsRangeError);
    });
  });

  // ── pointer alignment: the painter vs the maths ──────────────────────────
  //
  // REGRESSION, ported from the web suite. The web renderer once defined its
  // own tile geometry and disagreed with indicatedSegment by half a segment,
  // so the pointer showed segment k+1 for EVERY result. The painter now
  // consumes segmentArcDeg; these tests hold it to that.
  group('pointer alignment', () {
    int drawnSegmentUnderPointer(double rotation, int count) {
      final a = -90 - rotation;
      for (var seg = 1; seg <= count; seg++) {
        if (mod360(a - segmentArcDeg(seg, count).startDeg) < segmentAngle(count)) {
          return seg;
        }
      }
      throw StateError('no tile under the pointer — arcs do not tile the circle');
    }

    test('the drawn tile under the pointer IS the chosen segment', () {
      final rng = math.Random(11);
      for (var seg = 1; seg <= n; seg++) {
        for (var i = 0; i < 50; i++) {
          final to = targetAngle(rng.nextDouble() * 5000, seg, n, rng: rng);
          expect(drawnSegmentUnderPointer(to, n), seg);
        }
      }
    });

    test('agrees with indicatedSegment at every rotation', () {
      for (var deg = 0.0; deg < 720; deg += 0.5) {
        expect(drawnSegmentUnderPointer(deg, n), indicatedSegment(deg, n));
      }
    });

    test('the arcs tile the circle exactly', () {
      final s = segmentAngle(n);
      for (var seg = 1; seg <= n; seg++) {
        final arc = segmentArcDeg(seg, n);
        expect(arc.endDeg - arc.startDeg, closeTo(s, 1e-9));
        if (seg > 1) {
          expect(arc.startDeg, closeTo(segmentArcDeg(seg - 1, n).endDeg, 1e-9));
        }
      }
    });

    test('pegs sit on the boundaries', () {
      for (var seg = 1; seg <= n; seg++) {
        expect(boundaryAngleDeg(seg, n), closeTo(segmentArcDeg(seg, n).startDeg, 1e-9));
      }
    });
  });

  // ── settle physics ───────────────────────────────────────────────────────
  group('settle physics never shows the wrong prize', () {
    test('ends at EXACTLY the landing angle', () {
      final rng = math.Random(5);
      for (var seg = 1; seg <= n; seg++) {
        final from = rng.nextDouble() * 3000;
        final to = targetAngle(from, seg, n, rng: rng);
        expect(rotationWithSettle(from, to, 1.0, n), to);
        expect(rotationWithSettle(from, to, 1.5, n), to);
      }
    });

    test('the pointer stays on the winning segment through the ENTIRE settle', () {
      final rng = math.Random(9);
      for (var seg = 1; seg <= n; seg++) {
        for (var trial = 0; trial < 20; trial++) {
          final from = rng.nextDouble() * 3000;
          final to = targetAngle(from, seg, n, rng: rng);
          const brake = 1 - kSettleFraction;
          for (var t = brake; t <= 1.0; t += 0.0005) {
            expect(indicatedSegment(rotationWithSettle(from, to, t, n), n), seg);
          }
        }
      }
    });

    test('the overshoot is bounded by the room inside the segment', () {
      final rng = math.Random(13);
      final s = segmentAngle(n);
      for (var seg = 1; seg <= n; seg++) {
        for (var i = 0; i < 100; i++) {
          final to = targetAngle(0, seg, n, rng: rng);
          final room = mod360(-to) % s;
          final over = settleOvershootDeg(to, n);
          expect(over, greaterThanOrEqualTo(0));
          expect(over, lessThanOrEqualTo(room + 1e-9));
        }
      }
    });

    test('actually overshoots — the flourish is present', () {
      final to = targetAngle(0, 5, n, rng: math.Random(1));
      final peak = rotationWithSettle(0, to, 1 - kSettleFraction, n);
      expect(peak, greaterThan(to));
    });

    test('the resting rotation centres a tile under the pointer', () {
      final s = segmentAngle(n);
      expect(mod360(-restingRotation(n)) % s, closeTo(s / 2, 1e-9));
    });
  });

  // ── money formatting ─────────────────────────────────────────────────────
  group('money formatting', () {
    test('formats cents as KES', () {
      expect(formatKes(0), 'KES 0');
      expect(formatKes(500), 'KES 5');
      expect(formatKes(123450), 'KES 1,234.50');
      expect(formatKes(100000000), 'KES 1,000,000');
      expect(formatKes(-2500), '-KES 25');
      expect(formatKes(1234, symbol: false), '12.34');
    });

    test('signs ledger amounts', () {
      expect(formatSigned(500), '+KES 5');
      expect(formatSigned(-500), '-KES 5');
      expect(formatSigned(0), 'KES 0');
    });

    test('formats multipliers', () {
      expect(formatMultiplier(0), '—');
      expect(formatMultiplier(10000), '1x');
      expect(formatMultiplier(2000000), '200x');
    });
  });

  // ── phone normalisation ──────────────────────────────────────────────────
  group('phone normalisation (mirrors internal/auth/phone.go)', () {
    test('accepts every Kenyan spelling', () {
      for (final input in [
        '0712345678', '+254712345678', '254712345678', '712345678',
        '0712 345 678', '+254 712 345 678', '254-712-345-678', '(0712) 345-678',
      ]) {
        final r = normalisePhone(input);
        expect(r.valid, isTrue, reason: '$input should be valid');
        expect(r.phone, '254712345678');
      }
    });

    test('accepts the 01x range', () {
      expect(normalisePhone('0112345678').phone, '254112345678');
    });

    test('rejects anything else', () {
      for (final bad in [
        '', '   ', '0812345678', '0212345678', '071234567', '07123456789',
        'abcdefghij', '0712345abc', '254712345678-need_update', '+1 555 123 4567',
      ]) {
        expect(normalisePhone(bad).valid, isFalse, reason: '$bad should be rejected');
      }
    });

    test('is idempotent, so one person cannot become two accounts', () {
      final once = normalisePhone('0712345678');
      expect(normalisePhone(once.phone).phone, once.phone);
    });

    test('formats and masks', () {
      expect(prettyPhone('254712345678'), '0712 345 678');
      expect(maskPhone('254712345678'), '2547*****678');
    });
  });

  // ── stake ladder ─────────────────────────────────────────────────────────
  group('stake ladder', () {
    test('is ascending and every rung is a whole shilling', () {
      for (var i = 1; i < kStakeLadder.length; i++) {
        expect(kStakeLadder[i], greaterThan(kStakeLadder[i - 1]));
      }
      for (final c in kStakeLadder) {
        expect(c % 100, 0);
      }
    });

    test('steps up only to affordable rungs', () {
      expect(stepUp(500, 10000), 1000);
      expect(stepUp(10000, 10000), isNull);
    });

    test('steps are reversible', () {
      for (final c in kStakeLadder.take(kStakeLadder.length - 1)) {
        expect(stepDown(stepUp(c, 50000)!), c);
      }
    });

    test('opens on the second rung, never the largest', () {
      expect(defaultStake(50000), 1000);
      expect(defaultStake(0), 500);
    });
  });

  // ── celebration tiers ────────────────────────────────────────────────────
  group('celebration tiers', () {
    test('escalates with the multiplier', () {
      expect(celebrationFor(OutcomeKind.win, 20000).tier, Tier.small);
      expect(celebrationFor(OutcomeKind.win, 50000).tier, Tier.big);
      expect(celebrationFor(OutcomeKind.win, 500000).tier, Tier.huge);
      expect(celebrationFor(OutcomeKind.win, 2000000).tier, Tier.jackpot);
    });

    test('treats a loss quietly — never loud', () {
      final c = celebrationFor(OutcomeKind.loss, 0);
      expect(c.confetti, 0);
      expect(c.rays, isFalse);
      expect(c.chime, isEmpty);
      expect(c.dismiss.inMilliseconds, lessThan(2000));
    });

    test('confetti and dismiss time rise with the tier', () {
      final cs = [
        celebrationFor(OutcomeKind.loss, 0),
        celebrationFor(OutcomeKind.refund, 10000),
        celebrationFor(OutcomeKind.win, 20000),
        celebrationFor(OutcomeKind.win, 50000),
        celebrationFor(OutcomeKind.win, 500000),
        celebrationFor(OutcomeKind.win, 2000000),
      ];
      for (var i = 1; i < cs.length; i++) {
        expect(cs[i].confetti, greaterThanOrEqualTo(cs[i - 1].confetti));
        expect(cs[i].dismiss, greaterThanOrEqualTo(cs[i - 1].dismiss));
      }
    });
  });
}
