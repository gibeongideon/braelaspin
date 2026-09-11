/// The stake ladder.
///
/// Betting is SELECTION, not typing. A free-text amount field on a money
/// control invites typos in the one place a typo costs real money, needs
/// parsing and clamping, and on a phone summons a keyboard that covers the
/// wheel. A fixed ladder removes all of it: every value a player can reach is
/// one we chose.
///
/// Dart twin of `web/src/core/stakes.ts`.
library;

import 'models.dart';

/// Presets in cents. KES 5 up to KES 500.
const List<Cents> kStakeLadder = [500, 1000, 2500, 5000, 10000, 50000];

/// The rung at or below [cents], clamped into the ladder.
int nearestRung(Cents cents) {
  var best = 0;
  for (var i = 0; i < kStakeLadder.length; i++) {
    if (kStakeLadder[i] <= cents) best = i;
  }
  return best;
}

/// Exact rung index, or -1 when [cents] is not a preset.
int rungOf(Cents cents) => kStakeLadder.indexOf(cents);

/// The next affordable rung up, or null when there is none.
Cents? stepUp(Cents cents, Cents ceiling) {
  final from = rungOf(cents) >= 0 ? rungOf(cents) : nearestRung(cents);
  for (var i = from + 1; i < kStakeLadder.length; i++) {
    if (kStakeLadder[i] <= ceiling) return kStakeLadder[i];
  }
  return null;
}

/// The next rung down, or null when already at the bottom.
Cents? stepDown(Cents cents) {
  final from = rungOf(cents) >= 0 ? rungOf(cents) : nearestRung(cents);
  return from > 0 ? kStakeLadder[from - 1] : null;
}

/// Rungs that fit inside the ceiling.
List<Cents> affordableRungs(Cents ceiling) =>
    kStakeLadder.where((c) => c <= ceiling).toList(growable: false);

/// The rung to start on.
///
/// Deliberately the SECOND rung (KES 10) when affordable rather than the
/// largest: a wheel app should not open with the biggest bet preselected.
Cents defaultStake(Cents ceiling) {
  final a = affordableRungs(ceiling);
  if (a.isEmpty) return kStakeLadder.first;
  return a[a.length > 1 ? 1 : 0];
}
