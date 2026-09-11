/**
 * Canvas wheel renderer.
 *
 * Built to read as a physical prize wheel rather than a flat chart, using four
 * cues that do most of the work:
 *
 *   1. A FIXED light source. The rim bevel and the gloss are painted AFTER the
 *      rotation, so highlights stay put while the wheel turns underneath. A
 *      highlight that rotates with the wheel is the single clearest tell that
 *      something is a drawing and not an object.
 *   2. Depth on the rim — an outer bezel with a bevel gradient, and an inner
 *      shadow where the tiles meet it.
 *   3. Pegs that belong to the wheel, so they sweep past the fixed flapper.
 *   4. Overshoot and settle, from core/wheel.ts.
 *
 * Performance shape, unchanged and the same one the Flutter CustomPainter will
 * use: the rotating face is recorded ONCE into an offscreen canvas, and each
 * frame is one rotate + one blit plus the fixed overlays.
 *
 * ALL angles come from core/wheel.ts. This file paints; it does not compute
 * geometry. When it did, it drifted half a segment and the pointer showed the
 * wrong prize for every spin.
 */

import {
  targetAngle, rotationWithSettle, indicatedSegment, tickTimes,
  segmentAngle, segmentArcDeg, boundaryAngleDeg, restingRotation,
  SPIN_DURATION_MS,
} from '../core/wheel';
import type { Segment } from '../core/types';
import { formatMultiplier } from '../core/money';

const TAU = Math.PI * 2;
const DEG = Math.PI / 180;

interface Tier { a: string; b: string; text: string; rim: string }

/**
 * Tier colours — bigger prize, hotter tile.
 *
 * Losing tiles alternate between two dark shades. A run of identical dark
 * tiles reads as one big gap in the wheel; alternating keeps twelve distinct
 * segments visible, which is what makes the spin legible.
 */
function tierColours(multBp: number, index: number): Tier {
  if (multBp === 0) {
    return index % 2 === 0
      ? { a: '#3a3037', b: '#241d22', text: 'rgba(255,255,255,0.5)', rim: 'rgba(255,255,255,0.10)' }
      : { a: '#2f262c', b: '#1d171b', text: 'rgba(255,255,255,0.42)', rim: 'rgba(255,255,255,0.07)' };
  }
  if (multBp <= 20_000) return { a: '#4fbf5f', b: '#2c7838', text: '#fff',    rim: 'rgba(255,255,255,0.3)' };
  if (multBp <= 100_000) return { a: '#ff9648', b: '#d04a0e', text: '#fff',    rim: 'rgba(255,255,255,0.3)' };
  if (multBp <= 500_000) return { a: '#ffd447', b: '#c28f06', text: '#3a2a00', rim: 'rgba(255,255,255,0.42)' };
  return { a: '#ff6fb1', b: '#b81f96', text: '#fff', rim: 'rgba(255,255,255,0.36)' };
}

export interface WheelCallbacks {
  onTick?: () => void;
  onSettled?: (segment: number) => void;
}

export class WheelView {
  readonly canvas: HTMLCanvasElement;
  #ctx: CanvasRenderingContext2D;
  #face: HTMLCanvasElement | null = null;
  #segments: Segment[] = [];
  #rotation = 0;
  #size = 0;
  #dpr = 1;
  #raf = 0;
  #cb: WheelCallbacks;
  #spinning = false;

  constructor(canvas: HTMLCanvasElement, cb: WheelCallbacks = {}) {
    this.canvas = canvas;
    const ctx = canvas.getContext('2d');
    if (!ctx) throw new Error('canvas 2d context unavailable');
    this.#ctx = ctx;
    this.#cb = cb;
  }

  get spinning(): boolean { return this.#spinning; }

  setSegments(segments: Segment[]): void {
    const first = this.#segments.length === 0;
    this.#segments = segments;
    this.#face = null;
    // Rest with a tile centred under the pointer rather than straddling a
    // boundary. Cosmetic only — the landing maths is untouched.
    if (first && segments.length) this.#rotation = restingRotation(segments.length);
    this.resize();
  }

  resize(): void {
    const rect = this.canvas.getBoundingClientRect();
    const css = Math.max(200, Math.min(rect.width || 320, 520));
    this.#dpr = Math.min(window.devicePixelRatio || 1, 2.5);
    this.#size = css;
    this.canvas.width = Math.round(css * this.#dpr);
    this.canvas.height = Math.round(css * this.#dpr);
    this.#face = null;
    this.#paint();
  }

  /** Animate to the segment the SERVER chose. Resolves when it comes to rest. */
  spinTo(segment: number): Promise<void> {
    const n = this.#segments.length;
    if (n === 0) return Promise.resolve();

    const reduced = window.matchMedia?.('(prefers-reduced-motion: reduce)').matches;
    const from = this.#rotation;
    const to = targetAngle(from, segment, n);

    if (reduced) {
      this.#rotation = to;
      this.#paint();
      this.#cb.onSettled?.(segment);
      return Promise.resolve();
    }

    const ticks = tickTimes(from, to, n);
    let nextTick = 0;
    const started = performance.now();
    this.#spinning = true;

    return new Promise<void>((resolve) => {
      const frame = (now: number) => {
        const elapsed = now - started;
        this.#rotation = rotationWithSettle(from, to, elapsed, n);
        this.#paint();

        while (nextTick < ticks.length && ticks[nextTick]! <= elapsed) {
          this.#cb.onTick?.();
          nextTick++;
        }

        if (elapsed < SPIN_DURATION_MS) {
          this.#raf = requestAnimationFrame(frame);
          return;
        }

        this.#rotation = to;
        this.#paint();
        this.#spinning = false;

        const landed = indicatedSegment(to, n);
        if (landed !== segment && import.meta.env.DEV) {
          console.error(
            `wheel landed on ${landed} but the server said ${segment} — spin maths is wrong`,
          );
        }
        this.#cb.onSettled?.(segment);
        resolve();
      };
      this.#raf = requestAnimationFrame(frame);
    });
  }

  destroy(): void {
    cancelAnimationFrame(this.#raf);
    this.#spinning = false;
  }

  // ── painting ───────────────────────────────────────────────────────────

  #paint(): void {
    const ctx = this.#ctx;
    const px = this.#size * this.#dpr;
    if (px === 0) return;
    if (!this.#face) this.#face = this.#recordFace();

    ctx.setTransform(1, 0, 0, 1, 0, 0);
    ctx.clearRect(0, 0, px, px);

    // 1. the rotating face — one transform, one blit
    ctx.save();
    ctx.translate(px / 2, px / 2);
    ctx.rotate(this.#rotation * DEG);
    ctx.drawImage(this.#face, -px / 2, -px / 2);
    ctx.restore();

    // 2-4. fixed overlays: the light does not turn with the wheel
    ctx.save();
    ctx.translate(px / 2, px / 2);
    this.#paintBezel(ctx, px);
    this.#paintGloss(ctx, px);
    this.#paintHub(ctx, px);
    ctx.restore();
  }

  /** The rotating part: tiles, separators, pegs, labels. */
  #recordFace(): HTMLCanvasElement {
    const px = this.#size * this.#dpr;
    const face = document.createElement('canvas');
    face.width = px;
    face.height = px;
    const ctx = face.getContext('2d')!;
    ctx.translate(px / 2, px / 2);

    const n = this.#segments.length || 12;
    const R = px / 2;

    const tileOuter = R * 0.865;
    const tileInner = R * 0.455;
    const pegRadius = R * 0.905;

    // Backing disc, darker than any tile so the gaps read as depth.
    ctx.beginPath();
    ctx.arc(0, 0, R * 0.95, 0, TAU);
    ctx.fillStyle = '#0a0709';
    ctx.fill();

    this.#segments.forEach((seg, i) => {
      const arc = segmentArcDeg(i + 1, n);
      const gap = segmentAngle(n) * 0.045; // tight: a real wheel has thin dividers
      const start = (arc.startDeg + gap) * DEG;
      const end = (arc.endDeg - gap) * DEG;
      const mid = arc.midDeg * DEG;
      const t = tierColours(seg.multiplier_bp, i);

      ctx.beginPath();
      ctx.arc(0, 0, tileOuter, start, end);
      ctx.arc(0, 0, tileInner, end, start, true);
      ctx.closePath();

      // Radial gradient: lighter at the rim, darker toward the hub, so each
      // tile looks like a lit surface rather than flat fill.
      const g = ctx.createRadialGradient(0, 0, tileInner, 0, 0, tileOuter);
      g.addColorStop(0, t.b);
      g.addColorStop(0.75, t.a);
      g.addColorStop(1, t.b);
      ctx.fillStyle = g;
      ctx.fill();

      // Bright outer edge — the catch of light on a raised face.
      ctx.save();
      ctx.clip();
      ctx.beginPath();
      ctx.arc(0, 0, tileOuter - px * 0.004, start, end);
      ctx.strokeStyle = t.rim;
      ctx.lineWidth = px * 0.008;
      ctx.stroke();
      ctx.restore();

      // Label, radial, flipped in the lower half so it is never upside down.
      const midDeg = arc.midDeg;
      const upsideDown = Math.cos(mid) < 0;
      ctx.save();
      ctx.rotate(mid);
      ctx.translate((tileInner + tileOuter) / 2, 0);
      ctx.rotate(upsideDown ? Math.PI : 0);
      ctx.fillStyle = t.text;
      ctx.font = `800 ${Math.round(px * (seg.multiplier_bp >= 500_000 ? 0.049 : 0.055))}px system-ui, sans-serif`;
      ctx.textAlign = 'center';
      ctx.textBaseline = 'middle';
      if (seg.multiplier_bp > 0) {
        ctx.shadowColor = 'rgba(0,0,0,0.45)';
        ctx.shadowBlur = px * 0.01;
        ctx.shadowOffsetY = px * 0.002;
      }
      ctx.fillText(formatMultiplier(seg.multiplier_bp), 0, 0);
      ctx.restore();
      void midDeg;
    });

    // Pegs belong to the WHEEL, so they sweep past the fixed flapper — which
    // is what the ticking is.
    for (let i = 1; i <= n; i++) {
      const a = boundaryAngleDeg(i, n) * DEG;
      const x = Math.cos(a) * pegRadius;
      const y = Math.sin(a) * pegRadius;
      const r = px * 0.0105;

      ctx.beginPath();
      ctx.arc(x, y, r, 0, TAU);
      const pg = ctx.createRadialGradient(x - r * 0.4, y - r * 0.4, r * 0.1, x, y, r);
      pg.addColorStop(0, '#ffffff');
      pg.addColorStop(0.5, '#cdd2d8');
      pg.addColorStop(1, '#6b7280');
      ctx.fillStyle = pg;
      ctx.fill();
      ctx.strokeStyle = 'rgba(0,0,0,0.5)';
      ctx.lineWidth = px * 0.0022;
      ctx.stroke();
    }

    return face;
  }

  /** Fixed outer bezel with a bevel, lit from the top left. */
  #paintBezel(ctx: CanvasRenderingContext2D, px: number): void {
    const R = px / 2;
    const w = px * 0.022;
    const r = R * 0.955;

    const g = ctx.createLinearGradient(-r, -r, r, r);
    g.addColorStop(0, '#6d6068');
    g.addColorStop(0.3, '#2b2329');
    g.addColorStop(0.55, '#151013');
    g.addColorStop(0.8, '#3a3138');
    g.addColorStop(1, '#0e0a0c');

    ctx.beginPath();
    ctx.arc(0, 0, r, 0, TAU);
    ctx.strokeStyle = g;
    ctx.lineWidth = w;
    ctx.stroke();

    // Inner shadow where the tiles meet the bezel — reads as recess.
    ctx.beginPath();
    ctx.arc(0, 0, r - w * 0.6, 0, TAU);
    ctx.strokeStyle = 'rgba(0,0,0,0.55)';
    ctx.lineWidth = px * 0.012;
    ctx.stroke();

    // Thin bright keyline at the very edge.
    ctx.beginPath();
    ctx.arc(0, 0, r + w * 0.5, 0, TAU);
    ctx.strokeStyle = 'rgba(255,255,255,0.07)';
    ctx.lineWidth = px * 0.003;
    ctx.stroke();
  }

  /**
   * Specular sweep. Painted after the rotation so the highlight stays anchored
   * to the top-left while the wheel turns beneath it.
   */
  #paintGloss(ctx: CanvasRenderingContext2D, px: number): void {
    const R = px / 2 * 0.94;
    ctx.save();
    ctx.beginPath();
    ctx.arc(0, 0, R, 0, TAU);
    ctx.clip();

    const g = ctx.createLinearGradient(-R, -R, R * 0.45, R * 0.55);
    g.addColorStop(0, 'rgba(255,255,255,0.13)');
    g.addColorStop(0.38, 'rgba(255,255,255,0.035)');
    g.addColorStop(0.7, 'rgba(255,255,255,0)');
    g.addColorStop(1, 'rgba(0,0,0,0.20)');
    ctx.fillStyle = g;
    ctx.fillRect(-R, -R, R * 2, R * 2);
    ctx.restore();
  }

  /** Fixed hub: recessed ring, brushed plate, brand disc. */
  #paintHub(ctx: CanvasRenderingContext2D, px: number): void {
    const r = px * 0.225;

    // Recess ring
    ctx.beginPath();
    ctx.arc(0, 0, r * 1.06, 0, TAU);
    ctx.fillStyle = '#0a0709';
    ctx.fill();

    // Brushed plate, lit top-left
    ctx.beginPath();
    ctx.arc(0, 0, r, 0, TAU);
    const plate = ctx.createLinearGradient(-r, -r, r, r);
    plate.addColorStop(0, '#3b323a');
    plate.addColorStop(0.45, '#221b20');
    plate.addColorStop(1, '#100c0f');
    ctx.fillStyle = plate;
    ctx.fill();
    ctx.strokeStyle = 'rgba(255,255,255,0.09)';
    ctx.lineWidth = px * 0.0035;
    ctx.stroke();

    // Brand disc with a warm glow
    const br = r * 0.56;
    ctx.save();
    ctx.shadowColor = 'rgba(242,107,33,0.55)';
    ctx.shadowBlur = px * 0.045;
    ctx.beginPath();
    ctx.arc(0, 0, br, 0, TAU);
    const bg = ctx.createLinearGradient(-br, -br, br, br);
    bg.addColorStop(0, '#ffa45c');
    bg.addColorStop(0.5, '#f26b21');
    bg.addColorStop(1, '#c53f08');
    ctx.fillStyle = bg;
    ctx.fill();
    ctx.restore();

    // Top-edge highlight on the disc
    ctx.beginPath();
    ctx.arc(0, 0, br * 0.97, Math.PI * 1.08, Math.PI * 1.92);
    ctx.strokeStyle = 'rgba(255,255,255,0.4)';
    ctx.lineWidth = px * 0.004;
    ctx.stroke();

    ctx.fillStyle = '#fff';
    ctx.font = `700 ${Math.round(br * 1.05)}px system-ui, sans-serif`;
    ctx.textAlign = 'center';
    ctx.textBaseline = 'middle';
    ctx.shadowColor = 'rgba(0,0,0,0.35)';
    ctx.shadowBlur = px * 0.008;
    ctx.fillText('⚡', 0, br * 0.04);
    ctx.shadowBlur = 0;
  }
}

/**
 * Tick sound, synthesised with WebAudio.
 *
 * Two oscillators shaped like a peg striking a flapper: a short noisy click
 * plus a woody body tone. No asset to ship, and the context is created on the
 * user's first interaction so autoplay policy permits it.
 */
export class Ticker {
  #ctx: AudioContext | null = null;
  enabled = true;

  tick(): void {
    if (!this.enabled) return;
    try {
      this.#ctx ??= new AudioContext();
      const ctx = this.#ctx;
      const now = ctx.currentTime;

      const click = ctx.createOscillator();
      const clickGain = ctx.createGain();
      click.type = 'square';
      click.frequency.setValueAtTime(1750, now);
      click.frequency.exponentialRampToValueAtTime(820, now + 0.02);
      clickGain.gain.setValueAtTime(0.045, now);
      clickGain.gain.exponentialRampToValueAtTime(0.0001, now + 0.03);
      click.connect(clickGain).connect(ctx.destination);
      click.start(now);
      click.stop(now + 0.035);

      const body = ctx.createOscillator();
      const bodyGain = ctx.createGain();
      body.type = 'triangle';
      body.frequency.setValueAtTime(320, now);
      bodyGain.gain.setValueAtTime(0.03, now);
      bodyGain.gain.exponentialRampToValueAtTime(0.0001, now + 0.055);
      body.connect(bodyGain).connect(ctx.destination);
      body.start(now);
      body.stop(now + 0.06);
    } catch {
      // Audio is a nicety; never let it break the spin.
    }
  }
}
