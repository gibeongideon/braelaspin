/// How loudly to celebrate a result.
///
/// A win of 1x and a win of 200x are the same event to the ledger and utterly
/// different events to a player. Tiering the response is most of what makes a
/// game feel like a game rather than a form submission.
///
/// Restraint on the loss tier is deliberate. Making a loss loud is how you
/// build something that feels predatory; it gets a short, quiet
/// acknowledgement and gets out of the way.
///
/// Dart twin of `web/src/core/celebration.ts`.
library;

import 'models.dart';
import 'outcome.dart';

enum Tier { loss, refund, small, big, huge, jackpot }

class Celebration {
  const Celebration({
    required this.tier,
    required this.title,
    required this.confetti,
    required this.rays,
    required this.countUp,
    required this.dismiss,
    required this.chime,
  });

  final Tier tier;

  /// Banner text. Short — it sits above a large number.
  final String title;

  /// Confetti particle count. 0 means none.
  final int confetti;

  /// Draw rotating rays behind the card.
  final bool rays;

  /// Count the amount up rather than showing it outright.
  final bool countUp;

  /// Time on screen before auto-dismiss.
  final Duration dismiss;

  /// Rising chime notes in Hz; empty for silence.
  final List<double> chime;
}

const int _bigBp = 50000; // 5x
const int _hugeBp = 500000; // 50x
const int _jackpotBp = 2000000; // 200x

Celebration celebrationFor(OutcomeKind kind, BasisPoints multiplierBp) {
  if (kind == OutcomeKind.loss) {
    return const Celebration(
      tier: Tier.loss, title: 'No win', confetti: 0, rays: false,
      countUp: false, dismiss: Duration(milliseconds: 1700), chime: [],
    );
  }
  if (kind == OutcomeKind.refund) {
    return const Celebration(
      tier: Tier.refund, title: 'Bet returned', confetti: 0, rays: false,
      countUp: false, dismiss: Duration(milliseconds: 2000), chime: [520],
    );
  }
  if (multiplierBp >= _jackpotBp) {
    return const Celebration(
      tier: Tier.jackpot, title: 'JACKPOT!', confetti: 90, rays: true,
      countUp: true, dismiss: Duration(milliseconds: 5200),
      chime: [523, 659, 784, 1047, 1319],
    );
  }
  if (multiplierBp >= _hugeBp) {
    return const Celebration(
      tier: Tier.huge, title: 'HUGE WIN!', confetti: 55, rays: true,
      countUp: true, dismiss: Duration(milliseconds: 4200),
      chime: [523, 659, 784, 1047],
    );
  }
  if (multiplierBp >= _bigBp) {
    return const Celebration(
      tier: Tier.big, title: 'BIG WIN!', confetti: 32, rays: true,
      countUp: true, dismiss: Duration(milliseconds: 3600),
      chime: [523, 659, 784],
    );
  }
  return const Celebration(
    tier: Tier.small, title: 'You won', confetti: 14, rays: false,
    countUp: true, dismiss: Duration(milliseconds: 3000), chime: [587, 784],
  );
}
