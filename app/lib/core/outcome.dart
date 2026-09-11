/// How to describe a spin result to a player, truthfully.
///
/// "You won KES 10" is a lie when the bet was KES 10 and the multiplier was
/// 1x: the payout really is KES 10, but the balance did not move. The gross
/// payout and the net change are different numbers and the UI must show both,
/// or players will believe the maths is broken — and be right to, because what
/// they were shown did not match what their balance did.
///
/// Dart twin of `web/src/core/outcome.ts`.
library;

import 'models.dart';

enum OutcomeKind {
  /// multiplier 0 — the stake is gone
  loss,

  /// payout exactly equals the stake — balance unchanged
  refund,

  /// payout exceeds the stake
  win,
}

class Outcome {
  const Outcome({
    required this.kind,
    required this.payoutCents,
    required this.netCents,
    required this.multiplierBp,
    required this.stakeCents,
  });

  final OutcomeKind kind;

  /// stake x multiplier. What the wheel paid.
  final Cents payoutCents;

  /// payoutCents - stakeCents. What the balance actually did.
  final Cents netCents;
  final BasisPoints multiplierBp;
  final Cents stakeCents;
}

class PayoutMismatch implements Exception {
  const PayoutMismatch(this.stakeCents, this.multiplierBp, this.got, this.expected);
  final Cents stakeCents;
  final BasisPoints multiplierBp;
  final Cents got;
  final Cents expected;

  @override
  String toString() =>
      'payout mismatch: server said $got for $stakeCents x ${multiplierBp}bp, '
      'client computed $expected';
}

/// stake x multiplier, truncating exactly as Go and Postgres do.
///
/// Integer arithmetic throughout: `~/` is truncating integer division and Dart
/// ints are 64-bit, so this is the same operation the server performs rather
/// than a floating-point approximation of it.
Cents expectedPayout(Cents stakeCents, BasisPoints multiplierBp) =>
    (stakeCents * multiplierBp) ~/ 10000;

/// Derives the outcome, asserting the payout matches the multiplier.
///
/// The server is authoritative, so a mismatch means the two disagree about the
/// arithmetic — worth surfacing loudly rather than quietly rendering a wrong
/// number.
Outcome describeOutcome(Cents stakeCents, BasisPoints multiplierBp, Cents payoutCents) {
  final expected = expectedPayout(stakeCents, multiplierBp);
  if (payoutCents != expected) {
    throw PayoutMismatch(stakeCents, multiplierBp, payoutCents, expected);
  }

  final net = payoutCents - stakeCents;
  final kind = payoutCents == 0
      ? OutcomeKind.loss
      : (net == 0 ? OutcomeKind.refund : OutcomeKind.win);

  return Outcome(
    kind: kind,
    payoutCents: payoutCents,
    netCents: net,
    multiplierBp: multiplierBp,
    stakeCents: stakeCents,
  );
}

/// The balance immediately after the bet is taken but before the payout lands.
///
/// Derived from the server's own two numbers rather than by subtracting the
/// stake from a local value that could be stale. For a losing spin the payout
/// is 0, so this equals the final balance and settling changes nothing — the
/// bet leaves, and on a loss nothing comes back.
Cents balanceAfterStake(Cents finalBalanceCents, Cents payoutCents) =>
    finalBalanceCents - payoutCents;
