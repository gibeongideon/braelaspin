/**
 * The spin screen — the product.
 *
 * Deliberately spare. Everything on it is either the wheel, the bet, or the
 * action; anything a player does not need mid-spin lives one tap away:
 *
 *   - no free-text amount field. Betting is selection (see core/stakes.ts):
 *     steppers walk a fixed ladder and chips jump to a rung, so every
 *     reachable amount is one we chose and no keyboard ever covers the wheel.
 *   - no balance breakdown. The pill shows the balance in play; Practice /
 *     Real / Withdrawable belong to the Wallet tab, and rendering "REAL KES 0"
 *     under a practice game is noise.
 *   - no headline. The wheel displays 200x in bright pink; saying it again in
 *     words costs a third of the first screen.
 */

import { h, mount, Scope } from '../dom';
import { toast } from '../toast';
import { WheelView, Ticker } from '../wheel-view';
import { formatKes } from '../../core/money';
import { STAKE_LADDER, stepUp, stepDown, defaultStake } from '../../core/stakes';
import { describeOutcome, PayoutMismatchError } from '../../core/outcome';
import { showResult } from '../result-overlay';
import { ApiError } from '../../core/errors';
import type { Store } from '../../core/store';

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

  const minusBtn = h('button', {
    class: 'step', type: 'button', 'aria-label': 'Lower the bet', text: '−',
  });
  const plusBtn = h('button', {
    class: 'step', type: 'button', 'aria-label': 'Raise the bet', text: '+',
  });
  const stakeValue = h('div', {
    class: 'stake-value num',
    role: 'status',
    'aria-live': 'polite',
    'aria-label': 'Current bet',
  });

  const chipRow = h('div', { class: 'chips' });
  const spinBtn = h('button', { class: 'btn btn-primary', type: 'button' });
  const modeRow = h('div', { class: 'mode', role: 'group', 'aria-label': 'Play mode' });
  const balanceEl = h('span', { class: 'num' });
  const limitHint = h('p', { class: 'limit-hint' });

  // ── reactive wiring ────────────────────────────────────────────────────

  scope.add(store.config.subscribe((cfg) => {
    if (cfg.segments.length) wheel.setSegments(cfg.segments);
    renderStake();
  }));
  scope.add(store.balances.subscribe(() => { renderBalance(); renderStake(); }));
  scope.add(store.hideBalance.subscribe(renderBalance));
  scope.add(store.realMode.subscribe(() => {
    renderMode();
    renderBalance();
    // Switching mode changes the ceiling, so re-seat the stake on a rung that
    // is actually affordable.
    store.stake.value = store.clampStake(store.stake.value) || defaultStake(store.maxStake());
    renderStake();
  }));
  scope.add(store.stake.subscribe(renderStake));

  scope.add(store.spin.subscribe((phase) => {
    const busy = phase.t === 'requesting' || phase.t === 'spinning' || phase.t === 'checking';
    spinBtn.disabled = busy || store.maxStake() < store.config.value.min_stake_cents;
    minusBtn.disabled = busy || stepDown(store.stake.value) === null;
    plusBtn.disabled = busy || stepUp(store.stake.value, store.maxStake()) === null;
    mount(spinBtn,
      busy ? h('span', { class: 'spinner' }) : null,
      phase.t === 'checking' ? 'Checking your spin…'
        : phase.t === 'spinning' ? 'Spinning…'
        : phase.t === 'requesting' ? 'Placing…'
        : 'Spin now',
    );
  }));

  function renderBalance() {
    balanceEl.textContent = store.hideBalance.value
      ? '••••'
      : formatKes(store.activeBalance, { symbol: false });
  }

  function renderMode() {
    const real = store.realMode.value;
    mount(modeRow,
      h('button', {
        text: '🎮 Practice', type: 'button', 'aria-pressed': String(!real),
        onClick: () => { store.realMode.value = false; },
      }),
      h('button', {
        class: 'real', text: '💵 Real money', type: 'button', 'aria-pressed': String(real),
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

  function renderStake() {
    const stake = store.stake.value;
    const ceiling = store.maxStake();
    const min = store.config.value.min_stake_cents;

    stakeValue.textContent = formatKes(stake);
    minusBtn.disabled = stepDown(stake) === null;
    plusBtn.disabled = stepUp(stake, ceiling) === null;

    mount(chipRow, ...STAKE_LADDER.map((c) =>
      h('button', {
        class: 'chip',
        type: 'button',
        text: formatKes(c, { symbol: false }),
        'aria-pressed': String(stake === c),
        disabled: c > ceiling,
        title: c > ceiling ? 'More than you can bet right now' : undefined,
        onClick: () => { store.stake.value = c; },
      }),
    ));

    // Only say something when it is not obvious. In practice mode the ceiling
    // is the player's own free balance and needs no commentary.
    limitHint.textContent = !store.realMode.value
      ? ''
      : ceiling < min
        ? 'Real-money play is unavailable right now.'
        : `Max bet ${formatKes(ceiling)}`;
  }

  minusBtn.addEventListener('click', () => {
    const next = stepDown(store.stake.value);
    if (next !== null) store.stake.value = next;
  });
  plusBtn.addEventListener('click', () => {
    const next = stepUp(store.stake.value, store.maxStake());
    if (next !== null) store.stake.value = next;
  });

  // ── the spin itself ────────────────────────────────────────────────────

  spinBtn.addEventListener('click', async () => {
    if (wheel.spinning) return;
    try {
      const result = await store.placeSpin();

      // The bet is already committed server-side, so show it leaving now.
      store.applyStakeDebit(result);

      await wheel.spinTo(result.segment_index);

      // The payout lands only once the wheel has shown why.
      store.settleSpin(result);

      // Re-derive the payout from the stake and multiplier and compare with
      // what the server sent. They must agree; if they ever do not, that is an
      // arithmetic bug and the player must not be shown a number we cannot
      // stand behind.
      try {
        showResult(describeOutcome(
          result.stake_cents, result.multiplier_bp, result.payout_cents));
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
  renderStake();
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
          class: 'eye',
          'aria-label': 'Toggle balance visibility',
          text: '👁',
          onClick: () => { store.hideBalance.value = !store.hideBalance.value; },
        }),
      ),
    ),

    wheelWrap,
    winnersStrip(store),

    h('div', { class: 'card bet-card' },
      modeRow,
      h('div', { class: 'stepper' }, minusBtn, stakeValue, plusBtn),
      chipRow,
      limitHint,
      spinBtn,
    ),
  );

  return { el, scope };
}

/**
 * Recent winners — real wins only, real money only, phones masked.
 * Renders nothing when there are none, rather than inventing names: fake
 * social proof on a gambling site is a lie about other people's money.
 */
function winnersStrip(store: Store): HTMLElement {
  const el = h('div', { class: 'winners', 'aria-label': 'Recent winners' });

  store.api
    .get<{ items: { name: string; payout_cents: number }[] }>('/v1/game/winners')
    .then(({ items }) => {
      if (!items?.length) { el.remove(); return; }
      mount(el, ...items.map((wn) =>
        h('div', { class: 'winner' },
          h('span', { class: 'av', text: '👤' }),
          h('span', { text: wn.name }),
          h('span', { class: 'amt', text: formatKes(wn.payout_cents, { symbol: false }) }),
        ),
      ));
    })
    .catch(() => el.remove()); // decoration; never surface an error for it

  return el;
}
