/**
 * Wallet — balances plus the deposit/withdraw entry points.
 *
 * Deposit and withdraw are stubbed until M6/M7 (they need Daraja credentials).
 * The screen is wired so only the handler bodies change.
 */

import { h, mount, Scope } from '../dom';
import { toast } from '../toast';
import { formatKes } from '../../core/money';
import { prettyPhone } from '../../core/phone';
import { ApiError } from '../../core/errors';
import type { Store } from '../../core/store';

export function WalletScreen(store: Store): { el: HTMLElement; scope: Scope } {
  const scope = new Scope();
  const balanceCard = h('div', { class: 'card' });

  scope.add(store.balances.subscribe(() => render()));
  scope.add(store.user.subscribe(() => render()));

  function render() {
    const b = store.balances.value;
    const u = store.user.value;
    mount(balanceCard,
      h('p', { class: 'sub', text: 'Real balance' }),
      h('div', { style: 'font-size:32px;font-weight:800;margin:2px 0 4px', class: 'num',
                 text: formatKes(b.real_cents) }),
      h('p', { class: 'hint', text: `Withdrawable ${formatKes(b.withdrawable_cents)}` +
        (b.held_cents > 0 ? ` · ${formatKes(b.held_cents)} held for a pending withdrawal` : '') }),
      h('div', { style: 'height:14px' }),
      h('div', { class: 'btn-row' },
        h('button', { class: 'btn btn-primary btn-sm', text: 'Deposit', onClick: onDeposit }),
        h('button', { class: 'btn btn-ghost btn-sm', text: 'Withdraw', onClick: onWithdraw }),
      ),
      u ? h('p', { class: 'hint center', style: 'margin-top:12px',
                   text: `M-Pesa number: ${prettyPhone(u.phone)}` }) : null,
    );
  }

  function onDeposit() {
    toast('M-Pesa deposits arrive in the next release.', { kind: 'warn' });
  }

  function onWithdraw() {
    toast('M-Pesa withdrawals arrive in the next release.', { kind: 'warn' });
  }

  const el = h('div', { class: 'screen' },
    h('h1', { class: 'h1', text: 'Wallet' }),
    h('p', { class: 'sub', text: 'Deposit, withdraw and practice credit.' }),
    h('div', { style: 'height:14px' }),
    balanceCard,

    h('div', { class: 'card' },
      h('h2', { class: 'h2', text: '🎮 Practice balance' }),
      h('div', { style: 'display:flex;align-items:center;gap:12px' },
        h('div', { class: 'num', style: 'font-size:22px;font-weight:800;flex:1',
                   text: formatKes(store.balances.value.demo_cents) }),
        h('button', {
          class: 'btn btn-ghost btn-sm', style: 'width:auto', text: 'Top up',
          onClick: async (e: MouseEvent) => {
            const btn = e.currentTarget as HTMLButtonElement;
            btn.disabled = true;
            try {
              await store.topUpDemo();
              toast('Practice balance topped up.', { kind: 'win' });
            } catch (err) {
              toast(err instanceof ApiError ? err.userMessage : 'Could not top up.',
                    { kind: 'error' });
            } finally {
              btn.disabled = false;
            }
          },
        }),
      ),
      h('p', { class: 'hint', text: 'Free credit for practice mode. Once a day.' }),
    ),
  );

  render();
  return { el, scope };
}
