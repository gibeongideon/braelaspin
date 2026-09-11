/**
 * Canvas wheel renderer.
 *
 * Visual language follows the supplied reference: rounded rectangular TILES
 * arranged in a ring rather than pie wedges, a dark hub with a glowing brand
 * mark, and colour keyed to prize tier. Improvements on the reference: the
 * tiles carry real multipliers from the server rather than arbitrary numbers,
 * and the face is cached so a spin costs one transform per frame.
 *
 * Performance shape — the same one the Flutter CustomPainter will use:
 *   - the static face is drawn ONCE into an offscreen canvas
 *   - each frame does rotate() + drawImage(), not 12 tiles + 12 text layouts
 *
 * ALL geometry and easing comes from core/wheel.ts. This file only paints.
 */

import {
  targetAngle, rotationAt, indicatedSegment, tickTimes,
  segmentAngle, segmentArcDeg, boundaryAngleDeg, SPIN_DURATION_MS,
} from '../core/wheel';
import type { Segment } from '../core/types';
import { formatMultiplier } from '../core/money';

const TAU = Math.PI * 2;
const DEG = Math.PI / 180;

/** Tier colours. Bigger prize, hotter tile. */
function tierColours(multBp: number): { a: string; b: string; text: string } {
  if (multBp === 0)         return { a: '#2b2026', b: '#211920', text: 'rgba(255,255,255,0.38)' };
  if (multBp <= 20_000)     return { a: '#3fa34d', b: '#2f7d3a', text: '#ffffff' }; // 1x, 2x
  if (multBp <= 100_000)    return { a: '#ff8a3d', b: '#d94f12', text: '#ffffff' }; // 5x, 10x
  if (multBp <= 500_000)    return { a: '#f5c518', b: '#c9940a', text: '#3a2a00' }; // 50x
  return { a: '#ff5ea8', b: '#c026a6', text: '#ffffff' };                            // 200x jackpot
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
    this.#segments = segments;
    this.#face = null; // force a re-record
    this.resize();
  }

  /** Size to the element's box, accounting for device pixel ratio. */
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

  /**
   * Animate to the segment the SERVER chose.
   * Resolves when the wheel comes to rest.
   */
  spinTo(segment: number): Promise<void> {
    if (this.#segments.length === 0) return Promise.resolve();

    const reduced = window.matchMedia?.('(prefers-reduced-motion: reduce)').matches;
    const from = this.#rotation;
    const to = targetAngle(from, segment, this.#segments.length);

    // Reduced motion: show the outcome immediately rather than denying it.
    if (reduced) {
      this.#rotation = to;
      this.#paint();
      this.#cb.onSettled?.(segment);
      return Promise.resolve();
    }

    const ticks = tickTimes(from, to, this.#segments.length);
    let nextTick = 0;
    const started = performance.now();
    this.#spinning = true;

    return new Promise<void>((resolve) => {
      const frame = (now: number) => {
        const elapsed = now - started;
        this.#rotation = rotationAt(from, to, elapsed);
        this.#paint();

        while (nextTick < ticks.length && ticks[nextTick] <= elapsed) {
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

        // The animation must agree with the server. If it ever does not, that
        // is a geometry bug and we want to know in development rather than
        // quietly showing the player the wrong prize.
        const landed = indicatedSegment(to, this.#segments.length);
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

    // One rotate + one blit per frame: the whole performance story.
    ctx.translate(px / 2, px / 2);
    ctx.rotate(this.#rotation * DEG);
    ctx.drawImage(this.#face, -px / 2, -px / 2);
    ctx.setTransform(1, 0, 0, 1, 0, 0);

    this.#paintHub(ctx, px);
  }

  /** Draws the static wheel once into an offscreen canvas. */
  #recordFace(): HTMLCanvasElement {
    const px = this.#size * this.#dpr;
    const face = document.createElement('canvas');
    face.width = px;
    face.height = px;
    const ctx = face.getContext('2d')!;
    ctx.translate(px / 2, px / 2);

    const n = this.#segments.length || 12;
    const step = segmentAngle(n);
    const outer = px / 2;

    // Ring geometry, proportional so it holds at any size.
    const tileOuter = outer * 0.96;
    const tileInner = outer * 0.50;
    const tileH = tileOuter - tileInner;
    const gap = step * 0.13; // angular gap between tiles

    // Outer ring backdrop
    ctx.beginPath();
    ctx.arc(0, 0, outer * 0.99, 0, TAU);
    ctx.fillStyle = '#0d090b';
    ctx.fill();

    this.#segments.forEach((seg, i) => {
      // Geometry comes from core/wheel.ts, NOT from this file. When the
      // renderer defined its own it drifted half a segment out and the
      // pointer showed the wrong prize for every spin.
      const arc = segmentArcDeg(i + 1, n);
      const start = (arc.startDeg + gap / 2) * DEG;
      const end = (arc.endDeg - gap / 2) * DEG;
      const { a, b, text } = tierColours(seg.multiplier_bp);

      // Tile: an annular sector with rounded ends, which reads as the
      // reference's rounded rectangles once the ring is closed.
      ctx.beginPath();
      ctx.arc(0, 0, tileOuter, start, end);
      ctx.arc(0, 0, tileInner, end, start, true);
      ctx.closePath();

      const mid = arc.midDeg * DEG;
      const grad = ctx.createLinearGradient(
        Math.cos(mid) * tileInner, Math.sin(mid) * tileInner,
        Math.cos(mid) * tileOuter, Math.sin(mid) * tileOuter,
      );
      grad.addColorStop(0, b);
      grad.addColorStop(1, a);
      ctx.fillStyle = grad;
      ctx.fill();

      if (seg.multiplier_bp > 0) {
        ctx.strokeStyle = 'rgba(255,255,255,0.18)';
        ctx.lineWidth = Math.max(1, px * 0.002);
        ctx.stroke();
      }

      // Label, upright relative to its own radius.
      ctx.save();
      ctx.rotate(mid + Math.PI / 2);
      ctx.translate(0, -(tileInner + tileH * 0.52));
      ctx.fillStyle = text;
      ctx.font = `800 ${Math.round(px * 0.052)}px system-ui, sans-serif`;
      ctx.textAlign = 'center';
      ctx.textBaseline = 'middle';
      ctx.fillText(formatMultiplier(seg.multiplier_bp), 0, 0);
      ctx.restore();
    });

    // Pins on the tile boundaries, from the same shared formula.
    for (let i = 1; i <= n; i++) {
      const angle = boundaryAngleDeg(i, n) * DEG;
      const r = tileOuter + px * 0.008;
      ctx.beginPath();
      ctx.arc(Math.cos(angle) * r, Math.sin(angle) * r, px * 0.008, 0, TAU);
      ctx.fillStyle = 'rgba(255,255,255,0.55)';
      ctx.fill();
    }

    return face;
  }

  /** The hub does not rotate, so it is painted fresh each frame. */
  #paintHub(ctx: CanvasRenderingContext2D, px: number): void {
    const r = px * 0.21;
    ctx.save();
    ctx.translate(px / 2, px / 2);

    ctx.beginPath();
    ctx.arc(0, 0, r, 0, TAU);
    const g = ctx.createRadialGradient(0, 0, r * 0.2, 0, 0, r);
    g.addColorStop(0, '#241a20');
    g.addColorStop(1, '#100b0e');
    ctx.fillStyle = g;
    ctx.fill();
    ctx.strokeStyle = 'rgba(255,255,255,0.10)';
    ctx.lineWidth = Math.max(1, px * 0.004);
    ctx.stroke();

    // Brand mark
    ctx.beginPath();
    ctx.arc(0, 0, r * 0.52, 0, TAU);
    const bg = ctx.createLinearGradient(-r * 0.5, -r * 0.5, r * 0.5, r * 0.5);
    bg.addColorStop(0, '#ff8a3d');
    bg.addColorStop(1, '#d94f12');
    ctx.fillStyle = bg;
    ctx.fill();

    ctx.fillStyle = '#fff';
    ctx.font = `700 ${Math.round(r * 0.62)}px system-ui, sans-serif`;
    ctx.textAlign = 'center';
    ctx.textBaseline = 'middle';
    ctx.fillText('⚡', 0, r * 0.02);

    ctx.restore();
  }
}

/**
 * Tick sound, synthesised with WebAudio.
 *
 * No asset to ship, and no autoplay problem: the context is created on the
 * user's first interaction (the SPIN press) so browsers permit it.
 */
export class Ticker {
  #ctx: AudioContext | null = null;
  enabled = true;

  tick(): void {
    if (!this.enabled) return;
    try {
      this.#ctx ??= new AudioContext();
      const ctx = this.#ctx;
      const osc = ctx.createOscillator();
      const gain = ctx.createGain();
      osc.type = 'square';
      osc.frequency.value = 1350;
      gain.gain.setValueAtTime(0.05, ctx.currentTime);
      gain.gain.exponentialRampToValueAtTime(0.0001, ctx.currentTime + 0.035);
      osc.connect(gain).connect(ctx.destination);
      osc.start();
      osc.stop(ctx.currentTime + 0.04);
    } catch {
      // Audio is a nicety; never let it break the spin.
    }
  }
}
