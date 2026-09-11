/// The post-spin result card.
///
/// Two jobs that pull against each other, and both are honoured:
///
///  1. BE EXCITING. Tiered by prize (core/celebration.dart): rotating rays, a
///     counting number, confetti and a rising chime, scaled from a quiet
///     acknowledgement for a loss up to a full jackpot.
///
///  2. BE HONEST. The gross payout and the net balance change are different
///     numbers, and the card always shows both plus the arithmetic linking
///     them. No amount of confetti is worth "you won KES 10" while the balance
///     sits still, so 1x reads "Your bet back", not a win.
library;

import 'dart:math' as math;

import 'package:flutter/widgets.dart';

import '../core/celebration.dart';
import '../core/money.dart';
import '../core/outcome.dart';
import 'theme.dart';

Future<void> showResult(BuildContext context, Outcome outcome) async {
  final overlay = Overlay.maybeOf(context);
  if (overlay == null) return;
  final party = celebrationFor(outcome.kind, outcome.multiplierBp);

  late final OverlayEntry entry;
  entry = OverlayEntry(
    builder: (_) => _ResultCard(
      outcome: outcome,
      party: party,
      onDismiss: () {
        if (entry.mounted) entry.remove();
      },
    ),
  );
  overlay.insert(entry);
}

class _ResultCard extends StatefulWidget {
  const _ResultCard({required this.outcome, required this.party, required this.onDismiss});
  final Outcome outcome;
  final Celebration party;
  final VoidCallback onDismiss;

  @override
  State<_ResultCard> createState() => _ResultCardState();
}

class _ResultCardState extends State<_ResultCard> with TickerProviderStateMixin {
  late final AnimationController _in =
      AnimationController(vsync: this, duration: const Duration(milliseconds: 420))..forward();
  late final AnimationController _rays =
      AnimationController(vsync: this, duration: const Duration(seconds: 14));
  late final AnimationController _count =
      AnimationController(vsync: this, duration: const Duration(milliseconds: 620));
  late final AnimationController _fall =
      AnimationController(vsync: this, duration: const Duration(milliseconds: 3400));

  late final List<_Particle> _confetti;

  @override
  void initState() {
    super.initState();
    final p = widget.party;
    if (p.rays) _rays.repeat();
    if (p.countUp) _count.forward();

    final rng = math.Random();
    _confetti = List.generate(p.confetti, (i) => _Particle.random(rng, i, p.tier));
    if (p.confetti > 0) _fall.forward();

    Future<void>.delayed(p.dismiss, () {
      if (mounted) widget.onDismiss();
    });
  }

  @override
  void dispose() {
    _in.dispose();
    _rays.dispose();
    _count.dispose();
    _fall.dispose();
    super.dispose();
  }

  Color get _accent => switch (widget.party.tier) {
        Tier.loss => T.t3,
        Tier.refund => T.gold,
        Tier.small => T.greenHi,
        Tier.big => T.amberHi,
        Tier.huge => T.gold,
        Tier.jackpot => T.pink,
      };

  @override
  Widget build(BuildContext context) {
    final o = widget.outcome;
    final p = widget.party;
    final times = multiplierTimes(o.multiplierBp);

    return GestureDetector(
      onTap: widget.onDismiss,
      child: Container(
        color: const Color(0xBD080507),
        child: Stack(
          alignment: Alignment.center,
          children: [
            if (p.confetti > 0)
              Positioned.fill(
                child: AnimatedBuilder(
                  animation: _fall,
                  builder: (_, __) => CustomPaint(painter: _ConfettiPainter(_confetti, _fall.value)),
                ),
              ),
            ScaleTransition(
              scale: CurvedAnimation(parent: _in, curve: Curves.elasticOut),
              child: _card(o, p, times),
            ),
          ],
        ),
      ),
    );
  }

  Widget _card(Outcome o, Celebration p, num times) {
    return Container(
      constraints: const BoxConstraints(minWidth: 272),
      padding: const EdgeInsets.fromLTRB(36, 30, 36, 24),
      decoration: BoxDecoration(
        gradient: LinearGradient(
          begin: Alignment.topCenter, end: Alignment.bottomCenter,
          colors: [_accent.withValues(alpha: 0.18), const Color(0xFF14100F)],
        ),
        border: Border.all(color: _accent.withValues(alpha: 0.55)),
        borderRadius: const BorderRadius.all(Radius.circular(28)),
        boxShadow: const [BoxShadow(color: Color(0xEB000000), blurRadius: 70, offset: Offset(0, 26))],
      ),
      child: Stack(
        alignment: Alignment.center,
        children: [
          if (p.rays)
            Positioned.fill(
              child: ClipRRect(
                borderRadius: const BorderRadius.all(Radius.circular(28)),
                child: AnimatedBuilder(
                  animation: _rays,
                  builder: (_, __) => CustomPaint(
                    painter: _RaysPainter(_rays.value * 2 * math.pi, _accent),
                  ),
                ),
              ),
            ),
          Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              // The multiplier as a token, echoing the wheel tile it came from.
              Container(
                width: 62, height: 62,
                alignment: Alignment.center,
                decoration: BoxDecoration(
                  shape: BoxShape.circle,
                  gradient: LinearGradient(
                    begin: Alignment.topLeft, end: Alignment.bottomRight,
                    colors: o.kind == OutcomeKind.loss
                        ? const [Color(0xFF4A3F47), Color(0xFF221B20)]
                        : [_accent, _accent.withValues(alpha: 0.6)],
                  ),
                  border: Border.all(color: const Color(0x40FFFFFF), width: 2),
                ),
                child: Text(
                  o.kind == OutcomeKind.loss ? '—' : '$times×',
                  style: TextStyle(
                    fontSize: 19, fontWeight: FontWeight.w800,
                    color: p.tier == Tier.huge ? const Color(0xFF3A2A00) : T.t1,
                  ),
                ),
              ),
              const SizedBox(height: 12),
              Text(
                p.title.toUpperCase(),
                style: TextStyle(
                  fontSize: p.tier == Tier.jackpot ? 15 : 12.5,
                  fontWeight: FontWeight.w800,
                  letterSpacing: p.tier == Tier.jackpot ? 3 : 2,
                  color: _accent,
                ),
              ),
              const SizedBox(height: 4),
              // The amount, counted up for wins.
              AnimatedBuilder(
                animation: _count,
                builder: (_, __) {
                  final shown = p.countUp
                      // Always finishes on the exact payout: the last frame
                      // assigns the real value, not the interpolation.
                      ? (_count.isCompleted
                          ? o.payoutCents
                          : (o.payoutCents * Curves.easeOutCubic.transform(_count.value)).round())
                      : (o.kind == OutcomeKind.loss ? 0 : o.payoutCents);
                  return Text(
                    formatKes(shown),
                    style: TextStyle(
                      fontSize: p.tier == Tier.jackpot ? 44 : (p.tier == Tier.huge ? 42 : 38),
                      fontWeight: FontWeight.w800,
                      letterSpacing: -0.8,
                      color: o.kind == OutcomeKind.loss ? T.t3 : _accent,
                      fontFeatures: const [FontFeature.tabularFigures()],
                    ),
                  );
                },
              ),
              // The arithmetic, for anyone who wants to check us. Omitted on a
              // loss, where "bet x 0 = 0" is noise rather than information.
              if (o.kind != OutcomeKind.loss) ...[
                const SizedBox(height: 2),
                Text(
                  '${formatKes(o.stakeCents)} × $times = ${formatKes(o.payoutCents)}',
                  style: const TextStyle(
                    fontSize: 12, color: T.t3,
                    fontFeatures: [FontFeature.tabularFigures()],
                  ),
                ),
              ],
              const SizedBox(height: 12),
              Container(
                padding: const EdgeInsets.only(top: 11),
                decoration: const BoxDecoration(border: Border(top: BorderSide(color: T.border))),
                // What the balance actually did. Never hidden, whatever the tier.
                child: Text(
                  _netLine(o),
                  style: TextStyle(
                    fontSize: 14, fontWeight: FontWeight.w700,
                    color: o.netCents > 0 ? T.greenHi : T.t3,
                    fontFeatures: const [FontFeature.tabularFigures()],
                  ),
                ),
              ),
            ],
          ),
        ],
      ),
    );
  }
}

String _netLine(Outcome o) => switch (o.kind) {
      OutcomeKind.win => 'Balance +${formatKes(o.netCents)}',
      OutcomeKind.refund => 'Your bet back — balance unchanged',
      OutcomeKind.loss => 'Balance −${formatKes(o.netCents.abs())}',
    };

class _RaysPainter extends CustomPainter {
  _RaysPainter(this.angle, this.colour);
  final double angle;
  final Color colour;

  @override
  void paint(Canvas canvas, Size size) {
    final c = Offset(size.width / 2, size.height / 2);
    final r = size.longestSide;
    final paint = Paint()..color = colour.withValues(alpha: 0.10);
    canvas.save();
    canvas.translate(c.dx, c.dy);
    canvas.rotate(angle);
    for (var i = 0; i < 20; i++) {
      final a = i * (2 * math.pi / 20);
      canvas.drawPath(
        Path()
          ..moveTo(0, 0)
          ..lineTo(math.cos(a) * r, math.sin(a) * r)
          ..lineTo(math.cos(a + 0.08) * r, math.sin(a + 0.08) * r)
          ..close(),
        paint,
      );
    }
    canvas.restore();
  }

  @override
  bool shouldRepaint(_RaysPainter old) => old.angle != angle;
}

class _Particle {
  _Particle(this.x, this.drift, this.delay, this.speed, this.spin, this.size, this.colour, this.ribbon);
  final double x, drift, delay, speed, spin, size;
  final Color colour;
  final bool ribbon;

  factory _Particle.random(math.Random r, int i, Tier tier) {
    const palettes = {
      Tier.jackpot: [Color(0xFFFF5EA8), Color(0xFFFFD447), Color(0xFFFF8A3D), Color(0xFF8BE9FD), Color(0xFFFFFFFF)],
      Tier.huge: [Color(0xFFFFD447), Color(0xFFFFA45C), Color(0xFFFFFFFF), Color(0xFFFFEC99)],
      Tier.big: [Color(0xFFFF8A3D), Color(0xFFFFD447), Color(0xFFFFFFFF)],
    };
    final cols = palettes[tier] ?? const [Color(0xFF4FBF5F), Color(0xFFFFFFFF), Color(0xFF9BE7A6)];
    return _Particle(
      r.nextDouble(),
      (r.nextDouble() - 0.5) * 180,
      r.nextDouble() * 0.2,
      0.6 + r.nextDouble() * 0.4,
      (r.nextDouble() * 2 - 1) * 6 * math.pi,
      6 + r.nextDouble() * 6,
      cols[i % cols.length],
      i % 3 == 0,
    );
  }
}

class _ConfettiPainter extends CustomPainter {
  _ConfettiPainter(this.particles, this.t);
  final List<_Particle> particles;
  final double t;

  @override
  void paint(Canvas canvas, Size size) {
    for (final p in particles) {
      final local = ((t - p.delay) / p.speed).clamp(0.0, 1.0);
      if (local <= 0) continue;
      final y = -20 + local * (size.height + 60);
      final x = p.x * size.width + p.drift * local;
      final paint = Paint()..color = p.colour.withValues(alpha: (1 - local * local).clamp(0.0, 1.0));

      canvas.save();
      canvas.translate(x, y);
      canvas.rotate(p.spin * local);
      final rect = Rect.fromCenter(
        center: Offset.zero, width: p.size, height: p.ribbon ? p.size * 2.2 : p.size);
      if (p.ribbon) {
        canvas.drawRRect(RRect.fromRectAndRadius(rect, const Radius.circular(2)), paint);
      } else {
        canvas.drawOval(rect, paint);
      }
      canvas.restore();
    }
  }

  @override
  bool shouldRepaint(_ConfettiPainter old) => old.t != t;
}
