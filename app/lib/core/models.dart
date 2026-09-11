/// Wire types — the API contract, mirroring the Go DTOs exactly.
///
/// This file is the Dart twin of `web/src/core/types.ts`. Keep the JSON keys
/// identical in both so the two clients read the same bytes the same way.
library;

/// All money is integer CENTS of KES. Never a double, on either side.
typedef Cents = int;

/// Basis points: 10000 = 100% = 1x.
typedef BasisPoints = int;

class User {
  const User({
    required this.id,
    required this.phone,
    required this.phoneDisplay,
    required this.refCode,
    required this.refLink,
    required this.phoneVerified,
    required this.isAdmin,
  });

  final int id;
  final String phone;
  final String phoneDisplay;
  final String refCode;
  final String refLink;
  final bool phoneVerified;
  final bool isAdmin;

  factory User.fromJson(Map<String, dynamic> j) => User(
        id: j['id'] as int,
        phone: j['phone'] as String? ?? '',
        phoneDisplay: j['phone_display'] as String? ?? '',
        refCode: j['ref_code'] as String? ?? '',
        refLink: j['ref_link'] as String? ?? '',
        phoneVerified: j['phone_verified'] as bool? ?? false,
        isAdmin: j['is_admin'] as bool? ?? false,
      );
}

class Session {
  const Session({required this.access, required this.refresh, required this.user});
  final String access;
  final String refresh;
  final User user;

  factory Session.fromJson(Map<String, dynamic> j) => Session(
        access: j['access'] as String,
        refresh: j['refresh'] as String,
        user: User.fromJson(j['user'] as Map<String, dynamic>),
      );
}

class Balances {
  const Balances({
    this.realCents = 0,
    this.demoCents = 0,
    this.heldCents = 0,
    this.withdrawableCents = 0,
  });

  final Cents realCents;
  final Cents demoCents;
  final Cents heldCents;
  final Cents withdrawableCents;

  factory Balances.fromJson(Map<String, dynamic> j) => Balances(
        realCents: j['real_cents'] as int? ?? 0,
        demoCents: j['demo_cents'] as int? ?? 0,
        heldCents: j['held_cents'] as int? ?? 0,
        withdrawableCents: j['withdrawable_cents'] as int? ?? 0,
      );

  Balances copyWith({Cents? realCents, Cents? demoCents}) => Balances(
        realCents: realCents ?? this.realCents,
        demoCents: demoCents ?? this.demoCents,
        heldCents: heldCents,
        // Funds held against an open withdrawal have already left realCents.
        withdrawableCents: realCents ?? this.realCents,
      );

  Cents spendable(bool isReal) => isReal ? realCents : demoCents;
}

class Segment {
  const Segment({required this.index, required this.multiplierBp, required this.weightBp});
  final int index;
  final BasisPoints multiplierBp;
  final int weightBp;

  factory Segment.fromJson(Map<String, dynamic> j) => Segment(
        index: j['index'] as int,
        multiplierBp: j['multiplier_bp'] as int,
        weightBp: j['weight_bp'] as int? ?? 0,
      );
}

class GameConfig {
  const GameConfig({
    this.segments = const [],
    this.rtpBp = 9000,
    this.minStakeCents = 500,
    this.maxStakeCents = 0,
    this.maxMultiplierBp = 2000000,
  });

  final List<Segment> segments;
  final BasisPoints rtpBp;
  final Cents minStakeCents;

  /// Lower of the configured ceiling and what the bankroll can cover.
  final Cents maxStakeCents;
  final BasisPoints maxMultiplierBp;

  factory GameConfig.fromJson(Map<String, dynamic> j) => GameConfig(
        segments: ((j['segments'] as List?) ?? [])
            .map((s) => Segment.fromJson(s as Map<String, dynamic>))
            .toList(growable: false),
        rtpBp: j['rtp_bp'] as int? ?? 9000,
        minStakeCents: j['min_stake_cents'] as int? ?? 500,
        maxStakeCents: j['max_stake_cents'] as int? ?? 0,
        maxMultiplierBp: j['max_multiplier_bp'] as int? ?? 2000000,
      );
}

class ReferralStats {
  const ReferralStats({this.count = 0, this.earnedCents = 0, this.last30Cents = 0});
  final int count;
  final Cents earnedCents;
  final Cents last30Cents;

  factory ReferralStats.fromJson(Map<String, dynamic> j) => ReferralStats(
        count: j['count'] as int? ?? 0,
        earnedCents: j['earned_cents'] as int? ?? 0,
        last30Cents: j['last_30_cents'] as int? ?? 0,
      );
}

class Me {
  const Me({
    required this.user,
    required this.balances,
    required this.game,
    required this.referrals,
  });
  final User user;
  final Balances balances;
  final GameConfig game;
  final ReferralStats referrals;

  factory Me.fromJson(Map<String, dynamic> j) => Me(
        user: User.fromJson(j['user'] as Map<String, dynamic>),
        balances: Balances.fromJson(j['balances'] as Map<String, dynamic>),
        game: GameConfig.fromJson(j['game'] as Map<String, dynamic>),
        referrals: ReferralStats.fromJson(j['referrals'] as Map<String, dynamic>),
      );
}

class SpinResult {
  const SpinResult({
    required this.spinId,
    required this.segmentIndex,
    required this.multiplierBp,
    required this.stakeCents,
    required this.payoutCents,
    required this.netCents,
    required this.balanceCents,
    required this.isReal,
  });

  final int spinId;

  /// 1-indexed, matching the wire format the wheel animates to.
  final int segmentIndex;
  final BasisPoints multiplierBp;
  final Cents stakeCents;
  final Cents payoutCents;
  final Cents netCents;
  final Cents balanceCents;
  final bool isReal;

  factory SpinResult.fromJson(Map<String, dynamic> j) => SpinResult(
        spinId: j['spin_id'] as int? ?? 0,
        segmentIndex: j['segment_index'] as int,
        multiplierBp: j['multiplier_bp'] as int,
        stakeCents: j['stake_cents'] as int,
        payoutCents: j['payout_cents'] as int,
        netCents: j['net_cents'] as int? ?? 0,
        balanceCents: j['balance_cents'] as int,
        isReal: j['is_real'] as bool? ?? false,
      );
}

class Transaction {
  const Transaction({
    required this.id,
    required this.kind,
    required this.isReal,
    required this.amountCents,
    required this.createdAt,
  });

  final int id;
  final String kind;
  final bool isReal;
  final Cents amountCents;
  final String createdAt;

  factory Transaction.fromJson(Map<String, dynamic> j) => Transaction(
        id: j['id'] as int,
        kind: j['kind'] as String,
        isReal: j['is_real'] as bool? ?? false,
        amountCents: j['amount_cents'] as int,
        createdAt: j['created_at'] as String? ?? '',
      );
}

class SpinRow {
  const SpinRow({
    required this.id,
    required this.isReal,
    required this.stakeCents,
    required this.segmentIndex,
    required this.multiplierBp,
    required this.payoutCents,
    required this.createdAt,
  });

  final int id;
  final bool isReal;
  final Cents stakeCents;
  final int segmentIndex;
  final BasisPoints multiplierBp;
  final Cents payoutCents;
  final String createdAt;

  factory SpinRow.fromJson(Map<String, dynamic> j) => SpinRow(
        id: j['id'] as int,
        isReal: j['is_real'] as bool? ?? false,
        stakeCents: j['stake_cents'] as int,
        segmentIndex: j['segment_index'] as int,
        multiplierBp: j['multiplier_bp'] as int,
        payoutCents: j['payout_cents'] as int,
        createdAt: j['created_at'] as String? ?? '',
      );
}
