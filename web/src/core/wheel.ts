/**
 * Wheel geometry and spin animation maths. PURE — no canvas, no DOM.
 *
 * The renderer (ui/wheel-view.ts on web, a CustomPainter on Flutter) consumes
 * these numbers; it does not compute them. That split is what lets the same
 * maths be unit-tested here and ported to `lib/game/spin_math.dart` unchanged.
 *
 * THE OUTCOME IS ALWAYS THE SERVER'S. Nothing in this file decides anything;
 * it only works out how to rotate the wheel so that the segment the server
 * already chose ends up under the pointer.
 */

export const SPIN_REVOLUTIONS = 6;
export const SPIN_DURATION_MS = 9000;

/** Degrees per segment for a wheel of `n` segments. */
export function segmentAngle(n: number): number {
  return 360 / n;
}

/**
 * The smallest rotation greater than `from + revolutions * 360` that leaves
 * 1-indexed `segment` under a pointer fixed at 12 o'clock.
 *
 * Derivation: segment k occupies wheel-local [(k-1)·S, k·S). After rotating
 * clockwise by θ, the wheel-local angle under the pointer is (−θ) mod 360.
 * We want that to land inside segment k, so we aim at its middle plus jitter
 * and solve the congruence.
 *
 * `from` matters: we solve from wherever the wheel currently rests and keep the
 * angle monotonic. The reference implementation reset the rotation to 0 before
 * every spin, which snaps visibly.
 */
export function targetAngle(
  from: number,
  segment: number,
  segmentCount: number,
  rng: () => number = Math.random,
): number {
  if (segment < 1 || segment > segmentCount) {
    throw new RangeError(
      `segment ${segment} out of range 1..${segmentCount}`,
    );
  }
  const S = segmentAngle(segmentCount);

  // Land anywhere inside the segment except hard against a boundary, so the
  // pointer never sits ambiguously on a divider.
  const margin = Math.min(2, S / 4);
  const jitter = margin + rng() * (S - 2 * margin);

  const want = mod360(360 - ((segment - 1) * S + jitter));
  const base = from + SPIN_REVOLUTIONS * 360;
  return base + mod360(want - base);
}

/**
 * Which 1-indexed segment is under the pointer at this rotation.
 * The exact inverse of targetAngle, used to assert the animation landed where
 * the server said it would.
 */
export function indicatedSegment(degrees: number, segmentCount: number): number {
  const S = segmentAngle(segmentCount);
  return Math.floor(mod360(-degrees) / S) + 1;
}

function mod360(d: number): number {
  return ((d % 360) + 360) % 360;
}

// ── where each segment is DRAWN ─────────────────────────────────────────────
//
// These live here, beside indicatedSegment, rather than in the renderer —
// because when the drawing and the maths each defined their own geometry they
// disagreed by half a segment, and the pointer showed segment k+1 for every
// result the server sent. Self-consistent maths is not enough: the tiles have
// to be placed by the same formula that decides what the pointer reads.
//
// The canonical mapping, derived once:
//
//   - the pointer is fixed at canvas -90deg (12 o'clock)
//   - a face feature drawn at canvas angle `a` appears at `a + rotation`
//   - so the feature under the pointer satisfies a + rotation = -90
//   - indicatedSegment defines the pointer as reading wheel-local
//     phi = mod360(-rotation), and segment k as occupying [(k-1)S, kS)
//
//   => wheel-local phi is drawn at canvas angle (phi - 90)
//   => segment k spans canvas [(k-1)S - 90, kS - 90)
//
// Note what this means: at rotation 0 the pointer sits on the LEADING EDGE of
// segment 1, not its centre. That is correct and invisible in play, because
// targetAngle always lands mid-segment.

export interface SegmentArc {
  /** Canvas-space start angle in degrees. */
  startDeg: number;
  /** Canvas-space end angle in degrees. */
  endDeg: number;
  /** Canvas-space centre, where the label goes. */
  midDeg: number;
}

/** Canvas-space arc for a 1-indexed segment. */
export function segmentArcDeg(segment: number, segmentCount: number): SegmentArc {
  if (segment < 1 || segment > segmentCount) {
    throw new RangeError(`segment ${segment} out of range 1..${segmentCount}`);
  }
  const S = segmentAngle(segmentCount);
  const startDeg = (segment - 1) * S - 90;
  const endDeg = segment * S - 90;
  return { startDeg, endDeg, midDeg: (startDeg + endDeg) / 2 };
}

/**
 * Canvas-space angle of the boundary BEFORE a 1-indexed segment — where the
 * pins sit. `boundaryAngleDeg(1)` is the pointer's resting line.
 */
export function boundaryAngleDeg(segment: number, segmentCount: number): number {
  return (segment - 1) * segmentAngle(segmentCount) - 90;
}

/**
 * Quartic ease-out. Matches the reference implementation's GSAP Power3.easeOut
 * closely, and unlike a cubic-bezier approximation it is exact and cheap.
 */
export function easeOutQuart(t: number): number {
  const u = 1 - t;
  return 1 - u * u * u * u;
}

/** Eased rotation at time `elapsed` into a spin from `from` to `to`. */
export function rotationAt(
  from: number,
  to: number,
  elapsedMs: number,
  durationMs: number = SPIN_DURATION_MS,
): number {
  if (elapsedMs >= durationMs) return to;
  if (elapsedMs <= 0) return from;
  return from + (to - from) * easeOutQuart(elapsedMs / durationMs);
}

/**
 * The times, in ms from the start of the spin, at which the pointer crosses a
 * segment boundary — i.e. when to play a tick.
 *
 * Precomputed by inverting the easing rather than tested per frame: the ticks
 * bunch up at the start (dozens in the first second) and a per-frame check at
 * 60fps would miss them.
 */
export function tickTimes(
  from: number,
  to: number,
  segmentCount: number,
  durationMs: number = SPIN_DURATION_MS,
): number[] {
  const S = segmentAngle(segmentCount);
  const total = to - from;
  if (total <= 0) return [];

  const first = Math.ceil(from / S);
  const last = Math.floor(to / S);
  const out: number[] = [];

  // Cap the count: a long spin crosses hundreds of boundaries and the ear
  // cannot resolve more than a few per second anyway.
  const maxTicks = 220;
  const step = Math.max(1, Math.ceil((last - first) / maxTicks));

  for (let k = first; k <= last; k += step) {
    const progress = (k * S - from) / total; // eased progress at this boundary
    const t = inverseEaseOutQuart(progress) * durationMs;
    if (t >= 0 && t <= durationMs) out.push(t);
  }
  return out;
}

/** Inverse of easeOutQuart, for turning an eased position back into a time. */
function inverseEaseOutQuart(p: number): number {
  const c = Math.min(1, Math.max(0, p));
  return 1 - Math.pow(1 - c, 1 / 4);
}
