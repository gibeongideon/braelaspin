/// Wheel geometry and spin animation maths. PURE — no Flutter, no canvas.
///
/// Dart twin of `web/src/core/wheel.ts`, ported function for function, and
/// verified by the same assertions. The painter consumes these numbers; it
/// does not compute them. When the web renderer computed its own geometry it
/// drifted half a segment and the pointer showed the wrong prize on every
/// spin — so this file is the single source for both clients.
///
/// THE OUTCOME IS ALWAYS THE SERVER'S. Nothing here decides anything; it only
/// works out how to rotate the wheel so the chosen segment ends under the
/// pointer.
library;

import 'dart:math' as math;

const int kSpinRevolutions = 6;
const Duration kSpinDuration = Duration(milliseconds: 9000);

/// Fraction of the spin spent settling back after the overshoot.
const double kSettleFraction = 0.14;

/// Degrees per segment for a wheel of [n] segments.
double segmentAngle(int n) => 360.0 / n;

double _mod360(double d) => ((d % 360) + 360) % 360;

/// The smallest rotation greater than `from + revolutions * 360` that leaves
/// 1-indexed [segment] under a pointer fixed at 12 o'clock.
///
/// [from] matters: we solve from wherever the wheel rests and keep the angle
/// monotonic. Resetting to zero before each spin snaps visibly.
double targetAngle(double from, int segment, int segmentCount, {math.Random? rng}) {
  if (segment < 1 || segment > segmentCount) {
    throw RangeError('segment $segment out of range 1..$segmentCount');
  }
  final r = rng ?? math.Random();
  final s = segmentAngle(segmentCount);

  // Land inside the segment but never hard against a boundary, so the pointer
  // is never ambiguously on a divider.
  final margin = math.min(2.0, s / 4);
  final jitter = margin + r.nextDouble() * (s - 2 * margin);

  final want = _mod360(360 - ((segment - 1) * s + jitter));
  final base = from + kSpinRevolutions * 360;
  return base + _mod360(want - base);
}

/// Which 1-indexed segment is under the pointer at this rotation.
/// The exact inverse of [targetAngle].
int indicatedSegment(double degrees, int segmentCount) =>
    (_mod360(-degrees) / segmentAngle(segmentCount)).floor() + 1;

/// Canvas-space arc for a 1-indexed segment.
///
/// Derived from the same mapping as [indicatedSegment]: the pointer is at
/// canvas -90deg, a feature drawn at canvas `a` appears at `a + rotation`, and
/// segment k occupies wheel-local [(k-1)S, kS). Therefore segment k spans
/// canvas [(k-1)S - 90, kS - 90).
({double startDeg, double endDeg, double midDeg}) segmentArcDeg(int segment, int segmentCount) {
  if (segment < 1 || segment > segmentCount) {
    throw RangeError('segment $segment out of range 1..$segmentCount');
  }
  final s = segmentAngle(segmentCount);
  final start = (segment - 1) * s - 90;
  final end = segment * s - 90;
  return (startDeg: start, endDeg: end, midDeg: (start + end) / 2);
}

/// Canvas-space angle of the boundary before a 1-indexed segment — the pegs.
double boundaryAngleDeg(int segment, int segmentCount) =>
    (segment - 1) * segmentAngle(segmentCount) - 90;

/// A resting rotation that centres a segment under the pointer.
///
/// Segment 1 begins AT the pointer, so rotation 0 leaves it straddling a
/// boundary — correct, but it looks unfinished before the first spin.
double restingRotation(int segmentCount) => -segmentAngle(segmentCount) / 2;

/// Quartic ease-out.
double easeOutQuart(double t) {
  final u = 1 - t;
  return 1 - u * u * u * u;
}

/// Cubic ease-in-out — the settle has weight at both ends, like gravity.
double easeInOutCubic(double t) =>
    t < 0.5 ? 4 * t * t * t : 1 - math.pow(-2 * t + 2, 3) / 2;

/// How far past the landing angle the wheel travels before settling back.
///
/// A physical prize wheel does not glide to a halt: the flapper clicks over
/// the last peg, the wheel carries past, and gravity pulls it back.
///
/// The overshoot is BOUNDED so the pointer cannot leave the winning segment,
/// even for a frame. Advancing theta by d moves the pointer's reading to
/// phi - d, so staying inside requires d <= (phi mod S) — exactly the jitter
/// [targetAngle] chose. Taking a fraction of that is provably safe, which is
/// why this is computed rather than hardcoded.
double settleOvershootDeg(double to, int segmentCount) {
  final s = segmentAngle(segmentCount);
  final room = _mod360(-to) % s;
  return math.min(0.55 * room, s * 0.4);
}

/// Rotation at [t] (0..1 of the spin), with overshoot and settle.
///
/// GUARANTEE: at t >= 1 this returns EXACTLY [to], so the landing assertion
/// against the server's segment is unaffected.
double rotationWithSettle(double from, double to, double t, int segmentCount) {
  if (t >= 1) return to;
  if (t <= 0) return from;

  const brake = 1 - kSettleFraction;
  final peak = to + settleOvershootDeg(to, segmentCount);

  if (t <= brake) {
    return from + (peak - from) * easeOutQuart(t / brake);
  }
  final u = (t - brake) / kSettleFraction;
  return peak + (to - peak) * easeInOutCubic(u);
}

/// Times, as fractions of the spin, at which the pointer crosses a boundary —
/// when to play a tick.
///
/// Precomputed by inverting the easing rather than tested per frame: the ticks
/// bunch up at the start and a per-frame check at 60fps would miss them.
List<double> tickFractions(double from, double to, int segmentCount) {
  final s = segmentAngle(segmentCount);
  final total = to - from;
  if (total <= 0) return const [];

  final first = (from / s).ceil();
  final last = (to / s).floor();
  const maxTicks = 220;
  final step = math.max(1, ((last - first) / maxTicks).ceil());

  final out = <double>[];
  for (var k = first; k <= last; k += step) {
    final progress = (k * s - from) / total;
    final c = progress.clamp(0.0, 1.0);
    final t = 1 - math.pow(1 - c, 1 / 4);
    if (t >= 0 && t <= 1) out.add(t.toDouble());
  }
  return out;
}
