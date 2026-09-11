/**
 * The post-spin result card.
 *
 * Two jobs that pull in opposite directions, and both have to be honoured:
 *
 *  1. BE EXCITING. Tiered by prize (see core/celebration.ts): rotating rays,
 *     a counting number, confetti and a rising chime, scaled from a quiet
 *     acknowledgement for a loss up to a full jackpot.
 *
 *  2. BE HONEST. The gross payout and the net balance change are different
 *     numbers, and the card always shows both plus the arithmetic that links
 *     them. A player who checks our maths must find it correct — no amount of
 *     confetti is worth "you won KES 10" while the balance sits still.
 *
 * The celebration is a wrapper. The content underneath it stays literal.
 */

import { h } from './dom';
import { formatKes } from '../core/money';
import { multiplierTimes, type Outcome } from '../core/outcome';
import { celebrationFor, confettiColours } from '../core/celebration';

export function showResult(outcome: Outcome, onDismiss?: () => void): void {
  const { kind, payoutCents, netCents, multiplierBp, stakeCents } = outcome;
  const times = multiplierTimes(multiplierBp);
  const party = celebrationFor(kind, multiplierBp);
  const reduced = window.matchMedia?.('(prefers-reduced-motion: reduce)').matches ?? false;

  const amountEl = h('div', { class: 'rc-amount num' });

  const card = h('div', { class: `rc-card ${party.tier}` },
    party.rays && !reduced ? h('div', { class: 'rc-rays', 'aria-hidden': 'true' }) : null,

    // The multiplier as a physical token, echoing the wheel tile it came from.
    h('div', { class: 'rc-badge', 'aria-hidden': 'true' },
      h('span', { text: kind === 'loss' ? '—' : `${times}×` })),

    h('div', { class: 'rc-title', text: party.title }),
    amountEl,

    // The arithmetic, for anyone who wants to check us. Omitted on a loss,
    // where "bet x 0 = 0" is noise rather than information.
    kind !== 'loss'
      ? h('div', { class: 'rc-sum', text: `${formatKes(stakeCents)} × ${times} = ${formatKes(payoutCents)}` })
      : null,

    // What the balance actually did. Never hidden, whatever the tier.
    h('div', { class: `rc-net ${netCents > 0 ? 'pos' : netCents < 0 ? 'neg' : ''}`,
               text: netLine(kind, netCents) }),
  );

  const overlay = h('div', {
    class: `rc-overlay ${party.tier}`,
    role: 'alertdialog',
    'aria-live': 'assertive',
    'aria-label': ariaLabel(outcome),
  }, card);

  // Amount: counted up for wins, stated outright otherwise.
  const finalText = kind === 'loss' ? formatKes(0) : formatKes(payoutCents);
  if (party.countUp && !reduced) {
    countUp(amountEl, payoutCents, 620);
  } else {
    amountEl.textContent = finalText;
  }

  if (party.confetti > 0 && !reduced) {
    overlay.append(confetti(party.confetti, confettiColours(party.tier)));
  }
  if (party.chime.length > 0) chime(party.chime);

  let closed = false;
  const close = () => {
    if (closed) return;
    closed = true;
    overlay.classList.add('leaving');
    setTimeout(() => overlay.remove(), 180);
    onDismiss?.();
  };
  overlay.addEventListener('click', close);
  document.body.append(overlay);
  setTimeout(close, party.dismissMs);
}

function netLine(kind: Outcome['kind'], netCents: number): string {
  switch (kind) {
    case 'win':    return `Balance +${formatKes(netCents)}`;
    case 'refund': return 'Your bet back — balance unchanged';
    case 'loss':   return `Balance −${formatKes(Math.abs(netCents))}`;
  }
}

function ariaLabel(o: Outcome): string {
  switch (o.kind) {
    case 'win':    return `Won ${formatKes(o.payoutCents)}. Balance up ${formatKes(o.netCents)}.`;
    case 'refund': return `Bet returned, ${formatKes(o.payoutCents)}. Balance unchanged.`;
    case 'loss':   return `No win. Balance down ${formatKes(Math.abs(o.netCents))}.`;
  }
}

/**
 * Counts the amount up to its final value.
 *
 * Eased so it decelerates into the number, and it ALWAYS finishes on the exact
 * payout — the last frame assigns the real value rather than whatever the
 * interpolation produced.
 */
function countUp(el: HTMLElement, toCents: number, durationMs: number): void {
  const started = performance.now();
  const step = (now: number) => {
    const t = Math.min(1, (now - started) / durationMs);
    const eased = 1 - Math.pow(1 - t, 3);
    if (t < 1) {
      el.textContent = formatKes(Math.round(toCents * eased));
      requestAnimationFrame(step);
    } else {
      el.textContent = formatKes(toCents); // exact, always
    }
  };
  requestAnimationFrame(step);
}

/**
 * Confetti as plain elements with CSS animations.
 *
 * No canvas and no rAF loop: the browser compositor handles transforms on a
 * few dozen absolutely-positioned divs far more cheaply than we could, and
 * the whole lot is removed with its parent.
 */
function confetti(count: number, colours: readonly string[]): HTMLElement {
  const layer = h('div', { class: 'rc-confetti', 'aria-hidden': 'true' });
  for (let i = 0; i < count; i++) {
    const colour = colours[i % colours.length]!;
    const left = Math.random() * 100;
    const delay = Math.random() * 0.5;
    const duration = 1.9 + Math.random() * 1.5;
    const drift = (Math.random() - 0.5) * 180;
    const spin = 360 + Math.random() * 720;
    const size = 6 + Math.random() * 6;
    // Roughly a third are ribbons rather than squares, which reads better in
    // motion than a uniform shape.
    const ribbon = i % 3 === 0;

    layer.append(h('i', {
      style: [
        `left:${left}%`,
        `background:${colour}`,
        `width:${size}px`,
        `height:${ribbon ? size * 2.2 : size}px`,
        `animation-delay:${delay}s`,
        `animation-duration:${duration}s`,
        `--drift:${drift}px`,
        `--spin:${spin}deg`,
        ribbon ? 'border-radius:2px' : 'border-radius:50%',
      ].join(';'),
    }));
  }
  return layer;
}

/** A short rising arpeggio. Same WebAudio approach as the wheel's ticks. */
function chime(notes: readonly number[]): void {
  try {
    const ctx = new AudioContext();
    notes.forEach((hz, i) => {
      const at = ctx.currentTime + i * 0.085;
      const osc = ctx.createOscillator();
      const gain = ctx.createGain();
      osc.type = 'triangle';
      osc.frequency.setValueAtTime(hz, at);
      gain.gain.setValueAtTime(0, at);
      gain.gain.linearRampToValueAtTime(0.07, at + 0.012);
      gain.gain.exponentialRampToValueAtTime(0.0001, at + 0.34);
      osc.connect(gain).connect(ctx.destination);
      osc.start(at);
      osc.stop(at + 0.36);
    });
    setTimeout(() => void ctx.close(), (notes.length * 85) + 600);
  } catch {
    // Sound is a nicety; never let it break the result.
  }
}
