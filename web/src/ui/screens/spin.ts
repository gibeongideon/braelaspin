/**
 * The spin screen — the product.
 *
 * Layout follows the reference: balance pill, headline, wheel with a fixed
 * pointer, a winners strip, and a full-width amber CTA.
 */

import { h, mount, Scope } from '../dom';
import { toast } from '../toast';
import { WheelView, Ticker } from '../wheel-view';
import { formatKes, parseShillings } from '../../core/money';
import { describeOutcome, PayoutMismatchError } from '../../core/outcome';
import { showResult } from '../result-overlay';
import { ApiError } from '../../core/errors';
import type { Store } from '../../core/store';

const CHIPS = [500, 1000, 2500, 5000, 10000, 50000]; // cents: 5, 10, 25, 50, 100, 500

export function SpinScreen(store: Store, nav: (route: string) => void): {
  el: HTMLElement; scope: Scope;
} {
  const scope = new Scope();
  const ticker = new Ticker();

  const canvas = h('canvas', { 'aria-hidden': 'true' });
  const wheelWrap = h('div', { class: 'wheel-wrap' },
    h('div', { class: 'pointer' }),
    canvas,
  );

  const wheel = new WheelView(canvas, { onTick: () => ticker.tick() });
  scope.add(() => wheel.destroy());

  const stakeInput = h('input', {
    class: 'input num',
    type: 'text',
    inputmode: 'decimal',
    'aria-label': 'Bet amount in shillings',
  });

  const chipRow = h('div', { class: 'chips' });
  const spinBtn = h('button', { class: 'btn btn-primary', type: 'button' });
  const modeRow = h('div', { class: 'mode', role: 'group', 'aria-label': 'Play mode' });
  const statsRow = h('div', { class: 'stats' });
  const balanceEl = h('span', { class: 'num' });
  const spinsHint = h('p', { class: 'spins-left' });

  // ── reactive wiring ────────────────────────────────────────────────────

  scope.add(store.config.subscribe((cfg) => {
    if (cfg.segments.length) wheel.setSegments(cfg.segments);
    renderChips();
  }));

  scope.add(store.balances.subscribe(() => { renderBalance(); renderStats(); renderChips(); }));
  scope.add(store.hideBalance.subscribe(() => renderBalance()));
  scope.add(store.realMode.subscribe(() => { renderMode(); renderBalance(); renderStats(); renderChips(); }));
  scope.add(store.stake.subscribe((c) => {
    if (document.activeElement !== stakeInput) stakeInput.value = String(c / 100);
    renderChips();
  }));
  scope.add(store.spin.subscribe((phase) => {
    const busy = phase.t === 'requesting' || phase.t === 'spinning' || phase.t === 'checking';
    spinBtn.disabled = busy;
    mount(spinBtn,
      busy ? h('span', { class: 'spinner' }) : null,
      phase.t === 'checking' ? 'Checking your spin…'
        : phase.t === 'spinning' ? 'Spinning…'
        : phase.t === 'requesting' ? 'Placing…'
        : 'Spin now',
    );
  }));

  function renderBalance() {
    const hidden = store.hideBalance.value;
    balanceEl.textContent = hidden ? '••••' : formatKes(store.activeBalance, { symbol: false });
  }

  function renderStats() {
    const b = store.balances.value;
    mount(statsRow,
      stat('Practice', formatKes(b.demo_cents)),
      stat('Real', formatKes(b.real_cents)),
      stat('Withdrawable', formatKes(b.withdrawable_cents)),
    );
  }

  function stat(k: string, v: string) {
    return h('div', { class: 'stat' },
      h('div', { class: 'k', text: k }),
      h('div', { class: 'v', text: v }),
    );
  }

  function renderMode() {
    const real = store.realMode.value;
    mount(modeRow,
      h('button', {
        text: '🎮 Practice', 'aria-pressed': String(!real), type: 'button',
        onClick: () => { store.realMode.value = false; },
      }),
      h('button', {
        class: 'real', text: '💵 Real money', 'aria-pressed': String(real), type: 'button',
        onClick: () => {
          if (store.balances.value.real_cents <= 0) {
            toast('Deposit first to play with real money.', {
              kind: 'warn',
              action: { label: 'Deposit', onClick: () => nav('wallet') },
            });
            return;
          }
          store.realMode.value = true;
        },
      }),
    );
  }

  function renderChips() {
    const cfg = store.config.value;
    const balance = store.activeBalance;
    mount(chipRow, ...CHIPS.map((c) => {
      const affordable = c <= balance && c >= cfg.min_stake_cents;
      return h('button', {
        class: 'chip',
        type: 'button',
        text: formatKes(c, { symbol: false }),
        'aria-pressed': String(store.stake.value === c),
        disabled: !affordable,
        title: affordable ? undefined : 'More than your balance',
        onClick: () => { store.stake.value = store.clampStake(c); },
      });
    }));

    const max = cfg.max_stake_cents;
    spinsHint.textContent = store.realMode.value && max > 0
      ? `Max bet right now: ${formatKes(max)}`
      : store.realMode.value
        ? 'Real-money play is warming up — try practice mode.'
        : 'Practice mode — no real money at stake.';
  }

  stakeInput.addEventListener('input', () => {
    const cents = parseShillings(stakeInput.value);
    if (cents !== null) store.stake.value = cents;
  });
  stakeInput.addEventListener('blur', () => {
    store.stake.value = store.clampStake(store.stake.value);
    stakeInput.value = String(store.stake.value / 100);
  });

  // ── the spin itself ────────────────────────────────────────────────────

  spinBtn.addEventListener('click', async () => {
    if (wheel.spinning) return;
    try {
      const result = await store.placeSpin();
      await wheel.spinTo(result.segment_index);

      // Balance commits only once the wheel has shown why.
      store.settleSpin(result);

      // Re-derive the payout from the stake and multiplier and compare with
      // what the server sent. They must agree; if they ever do not, that is an
      // arithmetic bug and the player must not be shown a number we cannot
      // stand behind.
      try {
        const outcome = describeOutcome(
          result.stake_cents, result.multiplier_bp, result.payout_cents);
        showResult(outcome);
      } catch (err) {
        if (err instanceof PayoutMismatchError) {
          console.error(err);
          toast('We could not verify that result. Check your history.', { kind: 'error' });
        } else {
          throw err;
        }
      }
      store.clearSpin();
    } catch (e) {
      if (!(e instanceof ApiError)) { toast('Something went wrong.', { kind: 'error' }); return; }

      // Turning the dead end into a route forward is the single most valuable
      // piece of UX in the app.
      if (e.code === 'insufficient_funds') {
        toast(e.userMessage, {
          kind: 'error',
          action: { label: 'Deposit', onClick: () => nav('wallet') },
        });
        return;
      }
      if (e.code === 'stake_too_small' || e.code === 'stake_too_large') {
        store.stake.value = store.clampStake(store.stake.value);
      }
      toast(e.userMessage, { kind: e.kind === 'timeout' ? 'warn' : 'error' });
    }
  });



  const onResize = () => wheel.resize();
  window.addEventListener('resize', onResize);
  scope.add(() => window.removeEventListener('resize', onResize));

  renderMode();
  renderBalance();
  renderStats();
  renderChips();
  queueMicrotask(() => wheel.resize());

  const el = h('div', { class: 'screen' },
    h('div', { class: 'topbar' },
      h('div', { class: 'brand' },
        h('span', { class: 'brand-mark', text: '⚡' }),
        'Braela',
      ),
      h('div', { class: 'balance-pill' },
        h('span', { class: 'coin' }),
        balanceEl,
        h('button', {
          class: 'icon-btn', style: 'width:22px;height:22px;border:0;background:none',
          'aria-label': 'Toggle balance visibility',
          text: '👁',
          onClick: () => { store.hideBalance.value = !store.hideBalance.value; },
        }),
      ),
    ),

    h('p', { class: 'headline' }, 'Spin to win up to ', h('em', { text: '200x' }), ' your stake'),

    wheelWrap,
    spinsHint,
    winnersStrip(),

    h('div', { class: 'card' },
      modeRow,
      h('div', { class: 'field', style: 'margin:14px 0 0' },
        h('label', { for: 'stake', text: 'Bet amount (KES)' }),
        stakeInput,
      ),
      chipRow,
      h('div', { style: 'height:14px' }),
      spinBtn,
    ),

    statsRow,
  );

  return { el, scope };
}

/**
 * Recent winners. Placeholder data for the wireframe — wire to a real
 * endpoint in M5. Kept because it is strong social proof in the reference.
 */
function winnersStrip(): HTMLElement {
  const sample = [
    ['Ronald', 2300], ['Shawn', 1500], ['Leslie', 3000], ['Colette', 4500],
  ] as const;
  return h('div', { class: 'winners', 'aria-label': 'Recent winners' },
    ...sample.map(([name, cents]) =>
      h('div', { class: 'winner' },
        h('span', { class: 'av', text: name[0] }),
        h('span', { text: name }),
        h('span', { class: 'amt', text: formatKes(cents, { symbol: false }) }),
      ),
    ),
  );
}
