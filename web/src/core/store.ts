/**
 * Application state and the operations that change it.
 *
 * Still PURE — no DOM. Views subscribe to the signals; nothing here knows a
 * view exists. This is the file that maps onto `lib/state/store.dart`.
 *
 * The whole app's mutable state is the handful of signals below. That is
 * deliberate: it is small enough to hold in your head, and each view subscribes
 * only to what it draws, so a spin repaints the wheel and the balance and
 * nothing else.
 */

import { Signal } from './signal';
import { Api, newClientRef, type TokenStore } from './api';
import { balanceAfterStake } from './outcome';
import { ApiError } from './errors';
import type {
  Balances, GameConfig, Me, ReferralStats, Session,
  SpinResult, Transaction, User, Cents,
} from './types';

export type AuthState = 'unknown' | 'signed-out' | 'signed-in';

/** What the spin is doing right now. The UI is a function of this. */
export type SpinPhase =
  | { t: 'idle' }
  | { t: 'requesting' }
  | { t: 'spinning'; result: SpinResult }
  | { t: 'settled'; result: SpinResult }
  /** Sent, outcome unknown. We must NOT tell the user it failed. */
  | { t: 'checking' };

const ZERO_BALANCES: Balances = {
  real_cents: 0, demo_cents: 0, held_cents: 0, withdrawable_cents: 0,
};

const FALLBACK_CONFIG: GameConfig = {
  segments: [], rtp_bp: 9000, min_stake_cents: 500,
  max_stake_cents: 0, max_multiplier_bp: 2_000_000,
};

export class Store {
  readonly auth = new Signal<AuthState>('unknown');
  readonly user = new Signal<User | null>(null);
  readonly balances = new Signal<Balances>(ZERO_BALANCES);
  readonly config = new Signal<GameConfig>(FALLBACK_CONFIG);
  readonly referrals = new Signal<ReferralStats>({ count: 0, earned_cents: 0, last_30_cents: 0 });

  readonly stake = new Signal<Cents>(1000); // KES 10
  readonly realMode = new Signal<boolean>(false);
  readonly hideBalance = new Signal<boolean>(false);
  readonly spin = new Signal<SpinPhase>({ t: 'idle' });
  readonly transactions = new Signal<Transaction[]>([]);
  readonly busy = new Signal<boolean>(false);

  readonly api: Api;
  #tokens: TokenStore;

  constructor(api: Api, tokens: TokenStore) {
    this.api = api;
    this.#tokens = tokens;
  }

  /** The balance the active mode spends from. */
  get activeBalance(): Cents {
    return this.realMode.value
      ? this.balances.value.real_cents
      : this.balances.value.demo_cents;
  }

  // ── session ────────────────────────────────────────────────────────────

  async boot(): Promise<void> {
    if (!this.#tokens.getRefresh()) {
      this.auth.value = 'signed-out';
      return;
    }
    try {
      await this.refreshMe();
      this.auth.value = 'signed-in';
    } catch {
      this.#tokens.clear();
      this.auth.value = 'signed-out';
    }
  }

  async register(phone: string, password: string, refCode?: string): Promise<void> {
    const s = await this.api.post<Session>('/v1/auth/register', {
      phone, password, ...(refCode ? { ref_code: refCode } : {}),
    }, { auth: false });
    this.#adoptSession(s);
    await this.refreshMe();
  }

  async login(phone: string, password: string): Promise<void> {
    const s = await this.api.post<Session>('/v1/auth/login', { phone, password }, { auth: false });
    this.#adoptSession(s);
    await this.refreshMe();
  }

  async logout(): Promise<void> {
    const refresh = this.#tokens.getRefresh();
    try {
      if (refresh) await this.api.post('/v1/auth/logout', { refresh }, { auth: false });
    } catch {
      // Logout is best-effort: the local session is cleared regardless, and a
      // failed revoke must never trap the user in a signed-in state.
    }
    this.#tokens.clear();
    this.user.value = null;
    this.balances.value = ZERO_BALANCES;
    this.transactions.value = [];
    this.auth.value = 'signed-out';
  }

  #adoptSession(s: Session): void {
    this.#tokens.set(s.access, s.refresh);
    this.user.value = s.user;
    this.auth.value = 'signed-in';
  }

  /** One call that repaints everything. Run on load and on tab focus. */
  async refreshMe(): Promise<void> {
    const me = await this.api.get<Me>('/v1/me');
    this.user.value = me.user;
    this.balances.force(me.balances);
    this.config.force(me.game);
    this.referrals.force(me.referrals);
  }

  async refreshBalances(): Promise<void> {
    this.balances.force(await this.api.get<Balances>('/v1/wallet'));
  }

  async loadHistory(): Promise<void> {
    const page = await this.api.get<{ items: Transaction[] }>('/v1/history?limit=50');
    this.transactions.force(page.items ?? []);
  }

  async topUpDemo(): Promise<void> {
    await this.api.post('/v1/wallet/demo/topup');
    await this.refreshBalances();
  }

  // ── the spin ───────────────────────────────────────────────────────────

  /**
   * Places one spin.
   *
   * Note the failure handling. A TIMEOUT is not a failure: the bet may have
   * been taken. We move to 'checking' and ask the server what actually
   * happened rather than telling the user it did not go through — telling
   * someone their money-losing bet failed, when it did not, is the worst thing
   * this client could do.
   */
  async placeSpin(): Promise<SpinResult> {
    const clientRef = newClientRef();
    this.spin.value = { t: 'requesting' };

    try {
      const result = await this.api.post<SpinResult>('/v1/game/spin', {
        stake_cents: this.stake.value,
        real: this.realMode.value,
        client_ref: clientRef,
      }, { idempotencyKey: clientRef });

      this.spin.value = { t: 'spinning', result };
      return result;
    } catch (e) {
      if (e instanceof ApiError && e.kind === 'timeout') {
        this.spin.value = { t: 'checking' };
        const recovered = await this.#recoverSpin(clientRef);
        if (recovered) {
          this.spin.value = { t: 'spinning', result: recovered };
          return recovered;
        }
      }
      this.spin.value = { t: 'idle' };
      throw e;
    }
  }

  /** Did the spin we lost the response to actually happen? */
  async #recoverSpin(clientRef: string): Promise<SpinResult | null> {
    for (let attempt = 0; attempt < 3; attempt++) {
      await sleep(1000 * (attempt + 1));
      try {
        return await this.api.get<SpinResult>(
          `/v1/game/spins/by-ref/${encodeURIComponent(clientRef)}`,
        );
      } catch (e) {
        // 404 means it genuinely never happened — stop asking.
        if (e instanceof ApiError && e.status === 404) return null;
      }
    }
    return null;
  }

  /**
   * Phase 1 of the balance update: the bet has been taken.
   *
   * Called as soon as the server accepts the spin, BEFORE the wheel animates,
   * so the player sees their stake leave immediately — which is what actually
   * happened, since the debit is already committed in the ledger by then.
   *
   * Uses the server's own two numbers (final balance minus payout) rather than
   * subtracting the stake from a local value that could be stale.
   */
  applyStakeDebit(result: SpinResult): void {
    this.#setBalance(
      result.is_real,
      balanceAfterStake(result.balance_cents, result.payout_cents),
    );
  }

  /**
   * Phase 2: the wheel has stopped, so the payout lands.
   *
   * On a win the balance rises by the payout. On a loss it is already at the
   * post-bet figure and nothing moves — which is exactly what a player
   * expects: the bet left, and nothing came back.
   *
   * Deliberately NOT applied before the animation: the number must not change
   * before the wheel has shown why.
   */
  settleSpin(result: SpinResult): void {
    this.#setBalance(result.is_real, result.balance_cents);
    this.spin.value = { t: 'settled', result };
  }

  #setBalance(isReal: boolean, cents: Cents): void {
    const b = { ...this.balances.value };
    if (isReal) b.real_cents = cents;
    else b.demo_cents = cents;
    // Funds held against an open withdrawal have already left real_cents.
    b.withdrawable_cents = b.real_cents;
    this.balances.force(b);
  }

  clearSpin(): void {
    this.spin.value = { t: 'idle' };
  }

  /** Clamp a stake to what the server would currently accept. */
  clampStake(cents: Cents): Cents {
    const cfg = this.config.value;
    const ceiling = Math.min(
      cfg.max_stake_cents || Number.MAX_SAFE_INTEGER,
      this.activeBalance || Number.MAX_SAFE_INTEGER,
    );
    return Math.max(cfg.min_stake_cents, Math.min(cents, ceiling));
  }
}

function sleep(ms: number): Promise<void> {
  return new Promise((r) => setTimeout(r, ms));
}
