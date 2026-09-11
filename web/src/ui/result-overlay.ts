/**
 * The post-spin result card.
 *
 * Shows the gross payout AND the net change, always, because they are
 * different numbers and only showing one of them misleads:
 *
 *   bet 10, 5x  -> paid KES 50, balance +KES 40
 *   bet 10, 1x  -> paid KES 10, balance unchanged   ("money back", not "you won")
 *   bet 10, 0x  -> paid nothing, balance -KES 10
 *
 * The reference implementation showed the gross figure alone, which makes a
 * 1x result look like a win that the balance then appears to swallow.
 */

import { h } from './dom';
import { formatKes } from '../core/money';
import { multiplierTimes, type Outcome } from '../core/outcome';

export function showResult(outcome: Outcome, onDismiss?: () => void): void {
  const { kind, payoutCents, netCents, multiplierBp, stakeCents } = outcome;
  const times = multiplierTimes(multiplierBp);

  const overlay = h('div', {
    class: `result-overlay ${kind}`,
    role: 'alertdialog',
    'aria-live': 'assertive',
    'aria-label': ariaLabel(outcome),
  });

  const card = h('div', { class: `result-card ${kind}` },
    h('div', { class: 'result-mult', text: kind === 'loss' ? 'NO WIN' : `${times}× MULTIPLIER` }),

    // The headline is the gross payout — literally bet × multiplier.
    h('div', { class: 'result-amt', text: kind === 'loss' ? formatKes(0) : formatKes(payoutCents) }),

    // The arithmetic, shown so the player can check it themselves.
    h('div', { class: 'result-sum' },
      `${formatKes(stakeCents)} bet × ${times} = ${formatKes(payoutCents)}`),

    // What the balance actually did. This is the number that must never be
    // hidden, because it is the one the player will verify.
    h('div', { class: `result-net ${netCents > 0 ? 'pos' : netCents < 0 ? 'neg' : ''}` },
      netLine(kind, netCents)),
  );

  overlay.append(card);
  const close = () => { overlay.remove(); onDismiss?.(); };
  overlay.addEventListener('click', close);

  document.body.append(overlay);
  // A loss can go quickly; a win is worth reading.
  setTimeout(close, kind === 'win' ? 3200 : 2000);
}

function netLine(kind: Outcome['kind'], netCents: number): string {
  switch (kind) {
    case 'win':
      return `Balance ${formatKes(netCents, { symbol: true }).replace('KES', '+KES')}`;
    case 'refund':
      // Truthful: the payout was real, the balance did not move.
      return 'Your bet back — balance unchanged';
    case 'loss':
      return `Balance −${formatKes(Math.abs(netCents))}`;
  }
}

function ariaLabel(o: Outcome): string {
  switch (o.kind) {
    case 'win':
      return `Won ${formatKes(o.payoutCents)}. Balance up ${formatKes(o.netCents)}.`;
    case 'refund':
      return `Bet returned, ${formatKes(o.payoutCents)}. Balance unchanged.`;
    case 'loss':
      return `No win. Balance down ${formatKes(Math.abs(o.netCents))}.`;
  }
}
