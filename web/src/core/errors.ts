/**
 * Server error codes -> the words a player reads.
 *
 * EVERY user-facing failure string in the app lives in this one file. The
 * server deliberately sends terse, stable codes and never prose, so copy can be
 * changed (or translated to Swahili) without touching the backend, and both the
 * web and Flutter clients stay consistent.
 *
 * Mirrors `lib/core/api_error.dart`.
 */

export type ErrorKind =
  | 'offline'      // never reached the server — provably nothing happened
  | 'timeout'      // sent, outcome unknown — the dangerous one
  | 'client'       // 4xx, the request was wrong
  | 'auth'         // 401/403, session is over
  | 'server'       // 5xx, our fault
  | 'rate_limited';

export class ApiError extends Error {
  readonly kind: ErrorKind;
  readonly code: string;
  readonly status: number;
  readonly meta: Record<string, unknown>;
  readonly requestId?: string;

  constructor(init: {
    kind: ErrorKind;
    code: string;
    status?: number;
    message?: string;
    meta?: Record<string, unknown>;
    requestId?: string;
  }) {
    super(init.message ?? init.code);
    this.name = 'ApiError';
    this.kind = init.kind;
    this.code = init.code;
    this.status = init.status ?? 0;
    this.meta = init.meta ?? {};
    this.requestId = init.requestId;
  }

  /** True when the request certainly never took effect, so retrying is safe. */
  get definitelyDidNotHappen(): boolean {
    return this.kind === 'offline' || this.kind === 'client' || this.kind === 'rate_limited';
  }

  get userMessage(): string {
    return userMessageFor(this.code, this.meta, this.kind);
  }
}

const COPY: Record<string, string> = {
  // auth
  invalid_credentials: 'That number or password is not correct.',
  invalid_phone: 'Enter a valid Kenyan mobile number, e.g. 0712 345 678.',
  phone_taken: 'That number is already registered. Try signing in instead.',
  password_too_short: 'Choose a password of at least 8 characters.',
  password_too_long: 'That password is too long.',
  unknown_referral_code: "We don't recognise that referral code.",
  token_reused: 'Your session has expired. Please sign in again.',
  account_suspended: 'This account has been suspended. Contact support.',
  unauthorized: 'Please sign in to continue.',
  forbidden: "You don't have access to that.",

  // game
  stake_too_small: 'That bet is below the minimum.',
  stake_too_large: 'That bet is above the maximum.',
  insufficient_funds: "You don't have enough for that bet.",
  stake_exceeds_bankroll: 'That bet is too large right now. Try a smaller amount.',
  no_pending_spin: 'Place a bet to spin.',

  // money
  withdrawal_already_pending: 'You already have a withdrawal in progress.',
  demo_topup_not_eligible: 'You can top up your practice balance once a day.',
  mpesa_rejected: "M-Pesa couldn't start that payment. Please try again.",

  // generic
  rate_limited: 'Too many attempts. Please wait a moment.',
  bad_request: "That didn't look right. Please check and try again.",
  not_found: 'Not found.',
  internal: 'Something went wrong on our side. Please try again.',
};

const BY_KIND: Record<ErrorKind, string> = {
  offline: 'No connection. Your request was not sent.',
  timeout: "That took too long. We're checking what happened.",
  client: "That didn't look right. Please check and try again.",
  auth: 'Please sign in to continue.',
  server: 'Something went wrong on our side. Please try again.',
  rate_limited: 'Too many attempts. Please wait a moment.',
};

export function userMessageFor(
  code: string,
  meta: Record<string, unknown> = {},
  kind: ErrorKind = 'client',
): string {
  // A few codes carry a number worth showing, so the player learns the actual
  // limit instead of guessing.
  if (code === 'rate_limited' && typeof meta.retry_after_seconds === 'number') {
    const s = meta.retry_after_seconds;
    return s > 60
      ? `Too many attempts. Try again in ${Math.ceil(s / 60)} minutes.`
      : `Too many attempts. Try again in ${s} seconds.`;
  }
  return COPY[code] ?? BY_KIND[kind];
}
