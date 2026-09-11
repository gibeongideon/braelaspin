/** Transaction history, straight from the ledger. */

import { h, mount, Scope } from '../dom';
import { formatSigned, formatWhen } from '../../core/money';
import type { Store } from '../../core/store';
import type { TxKind } from '../../core/types';

const LABEL: Record<TxKind, string> = {
  demo_grant: 'Practice credit',
  bet: 'Bet',
  win: 'Win',
  deposit: 'Deposit',
  withdraw: 'Withdrawal',
  withdraw_reversed: 'Withdrawal refunded',
  referral: 'Referral commission',
  adjustment: 'Adjustment',
};

const GLYPH: Record<TxKind, string> = {
  demo_grant: '🎁', bet: '🎯', win: '🏆', deposit: '⬇️',
  withdraw: '⬆️', withdraw_reversed: '↩️', referral: '👥', adjustment: '⚙️',
};

export function HistoryScreen(store: Store): { el: HTMLElement; scope: Scope } {
  const scope = new Scope();
  const list = h('div', { class: 'card' });

  scope.add(store.transactions.subscribe((items) => {
    if (items.length === 0) {
      mount(list, h('div', { class: 'empty' },
        h('div', { class: 'big', text: '📭' }),
        h('p', { text: 'No activity yet.' }),
        h('p', { class: 'hint', text: 'Your bets, wins and payments will appear here.' }),
      ));
      return;
    }
    mount(list, ...items.map((tx) =>
      h('div', { class: 'row' },
        h('div', { class: 'ico', text: GLYPH[tx.kind] ?? '•' }),
        h('div', { class: 'body' },
          h('div', { class: 'title' },
            LABEL[tx.kind] ?? tx.kind,
            !tx.is_real ? h('span', { class: 'badge pending', style: 'margin-left:6px',
                                      text: 'practice' }) : null,
          ),
          h('div', { class: 'meta', text: formatWhen(tx.created_at) }),
        ),
        h('div', {
          class: `amt ${tx.amount_cents > 0 ? 'pos' : 'neg'}`,
          text: formatSigned(tx.amount_cents),
        }),
      ),
    ));
  }));

  // Refresh on entry; the ledger is the source of truth for disputes.
  store.loadHistory().catch(() => {
    mount(list, h('div', { class: 'empty' },
      h('p', { text: 'Could not load your history.' }),
      h('p', { class: 'hint', text: 'Check your connection and pull down to retry.' }),
    ));
  });

  const el = h('div', { class: 'screen' },
    h('h1', { class: 'h1', text: 'History' }),
    h('p', { class: 'sub', text: 'Every bet, win and payment.' }),
    h('div', { style: 'height:14px' }),
    list,
  );

  return { el, scope };
}
