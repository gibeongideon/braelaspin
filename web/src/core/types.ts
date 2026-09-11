/**
 * Wire types — the API contract, mirroring the Go DTOs exactly.
 *
 * These are the single source of truth for the shape of every request and
 * response. When a Go handler changes a field name, this file changes with it
 * and the compiler finds every call site, rather than a money endpoint
 * silently receiving `undefined`.
 *
 * NOTE FOR THE FLUTTER PORT: this file maps 1:1 to `lib/core/models.dart`.
 * Keep the field names identical to the JSON so both clients read the same.
 */

/** All money is integer CENTS of KES. Never a float, on either side. */
export type Cents = number;

/** Basis points: 10000 = 100% = 1x. */
export type BasisPoints = number;

export interface User {
  id: number;
  phone: string;
  phone_display: string;
  ref_code: string;
  ref_link: string;
  phone_verified: boolean;
  is_admin: boolean;
}

export interface Session {
  access: string;
  refresh: string;
  access_expires_at: string;
  refresh_expires_at: string;
  user: User;
}

export interface Balances {
  real_cents: Cents;
  demo_cents: Cents;
  held_cents: Cents;
  withdrawable_cents: Cents;
}

export interface Segment {
  index: number;
  multiplier_bp: BasisPoints;
  weight_bp: number;
}

export interface GameConfig {
  segments: Segment[];
  rtp_bp: BasisPoints;
  min_stake_cents: Cents;
  /** Lower of the configured ceiling and what the bankroll can cover. */
  max_stake_cents: Cents;
  max_multiplier_bp: BasisPoints;
}

export interface ReferralStats {
  count: number;
  earned_cents: Cents;
  last_30_cents: Cents;
}

export interface Me {
  user: User;
  balances: Balances;
  game: GameConfig;
  referrals: ReferralStats;
}

export interface SpinResult {
  spin_id: number;
  /** 1-indexed, matching the wire format the wheel animates to. */
  segment_index: number;
  multiplier_bp: BasisPoints;
  stake_cents: Cents;
  payout_cents: Cents;
  net_cents: Cents;
  balance_cents: Cents;
  is_real: boolean;
  created_at: string;
}

export type TxKind =
  | 'demo_grant'
  | 'bet'
  | 'win'
  | 'deposit'
  | 'withdraw'
  | 'withdraw_reversed'
  | 'referral'
  | 'adjustment';

export interface Transaction {
  id: number;
  kind: TxKind;
  is_real: boolean;
  amount_cents: Cents;
  balance_after_cents: Cents;
  created_at: string;
  memo?: string;
}

export type DepositStatus = 'pending' | 'success' | 'failed' | 'expired';

export interface Deposit {
  id: number;
  amount_cents: Cents;
  credited_cents?: Cents;
  status: DepositStatus;
  result_desc?: string;
  created_at: string;
}

export type WithdrawalStatus =
  | 'pending'
  | 'approved'
  | 'sending'
  | 'paid'
  | 'failed'
  | 'rejected';

export interface Withdrawal {
  id: number;
  amount_cents: Cents;
  fee_cents: Cents;
  status: WithdrawalStatus;
  result_desc?: string;
  created_at: string;
}

/** The one error envelope every endpoint uses. */
export interface ApiErrorBody {
  error: {
    code: string;
    message: string;
    meta?: Record<string, unknown>;
    request_id?: string;
  };
}
