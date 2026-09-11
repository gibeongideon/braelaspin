/**
 * Spin history — every round, with the arithmetic shown.
 *
 * This screen exists so a player who doubts a result can check it. Each row
 * states the bet, the multiplier and the payout, so `bet × multiplier = payout`
 * is verifiable by eye without trusting us.
 */

import { h, mount, Scope } from '../dom';
import { formatKes, formatWhen } from '../../core/money';
import { multiplierTimes } from '../../core/outcome';
import { ApiError } from '../../core/errors';
import type { Store } from '../../core/store';
import type { Cents, BasisPoints } from '../../core/types';

interface SpinRow {
  id: number;
  is_real: boolean;
  stake_cents: Cents;
  segment_index: number;
  multiplier_bp: BasisPoints;
  payout_cents: Cents;
  created_at: string;
}

export function SpinsScreen(store: Store): { el: HTMLElement; scope: Scope } {
  const scope = new Scope();
  const list = h('div', { class: 'card' });
  const summary = h('div', { class: 'stats' });

  async function load() {
    mount(list, ...Array.from({ length: 4 }, () => h('div', { class: 'skeleton' })));
    try {
      const page = await store.api.get<{ items: SpinRow[] }>('/v1/game/spins?limit=50');
      render(page.items ?? []);
    } catch (e) {
      mount(list, h('div', { class: 'retry' },
        h('div', { class: 'big', text: '⚠️' }),
        h('p', { text: e instanceof ApiError ? e.userMessage : 'Could not load your spins.' }),
        h('button', { class: 'btn btn-ghost btn-sm', text: 'Try again', onClick: load }),
      ));
    }
  }

  function render(items: SpinRow[]) {
    if (items.length === 0) {
      mount(summary);
      mount(list, h('div', { class: 'empty' },
        h('div', { class: 'big', text: '🎡' }),
        h('p', { text: 'No spins yet.' }),
        h('p', { class: 'hint', text: 'Your results will appear here.' }),
      ));
      return;
    }

    // Totals, so the player can reconcile the screen against their balance.
    const staked = items.reduce((n, s) => n + s.stake_cents, 0);
    const won = items.reduce((n, s) => n + s.payout_cents, 0);
    mount(summary,
      stat('Spins', String(items.length)),
      stat('Staked', formatKes(staked)),
      stat('Returned', formatKes(won)),
    );

    mount(list, ...items.map((s) => {
      const net = s.payout_cents - s.stake_cents;
      const times = multiplierTimes(s.multiplier_bp);
      return h('div', { class: 'row' },
        h('div', {
          class: 'ico',
          style: s.payout_cents > 0 ? 'background:rgba(63,163,77,.18)' : '',
          text: s.multiplier_bp === 0 ? '—' : `${times}×`,
        }),
        h('div', { class: 'body' },
          // The identity, written out: bet × multiplier = payout.
          h('div', { class: 'title num',
            text: `${formatKes(s.stake_cents)} × ${times} = ${formatKes(s.payout_cents)}` }),
          h('div', { class: 'meta' },
            formatWhen(s.created_at),
            ' · segment ', String(s.segment_index),
            !s.is_real ? ' · practice' : '',
          ),
        ),
        h('div', {
          class: `amt ${net > 0 ? 'pos' : 'neg'}`,
          text: (net > 0 ? '+' : net < 0 ? '−' : '') + formatKes(Math.abs(net)),
        }),
      );
    }));
  }

  function stat(k: string, v: string) {
    return h('div', { class: 'stat' },
      h('div', { class: 'k', text: k }),
      h('div', { class: 'v', text: v }));
  }

  void load();

  const el = h('div', { class: 'screen' },
    h('h1', { class: 'h1', text: 'Your spins' }),
    h('p', { class: 'sub', text: 'Every result, with the maths shown.' }),
    h('div', { style: 'height:14px' }),
    summary,
    list,
    h('p', { class: 'hint center', style: 'margin-top:14px',
             text: 'Payout is always your bet multiplied by the segment you landed on.' }),
  );

  return { el, scope };
}
