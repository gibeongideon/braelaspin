/// The Braela mark: a prize wheel.
///
/// Drawn rather than shipped as an image so it is crisp at every size and
/// costs no APK bytes. The geometry matches `brand/make_icons.py` (which
/// generates the launcher icon) and the inline SVG in the web client, so the
/// three cannot drift.
///
/// A lightning bolt stood here first. It said "electricity" or "fast" and
/// could have belonged to any app; a logo should say what the product is.
library;

import 'dart:math' as math;

import 'package:flutter/widgets.dart';

const Color _ember = Color(0xFF8A2C08);

class BrandMark extends StatelessWidget {
  const BrandMark({super.key, this.size = 28, this.onAmber = true});

  final double size;

  /// True when sitting on the brand gradient (wedges burnt amber on white),
  /// false for a dark surface (wedges amber on white).
  final bool onAmber;

  @override
  Widget build(BuildContext context) => SizedBox(
    width: size,
    height: size,
    child: CustomPaint(painter: _MarkPainter(onAmber: onAmber)),
  );
}

class _MarkPainter extends CustomPainter {
  const _MarkPainter({required this.onAmber});
  final bool onAmber;

  @override
  void paint(Canvas canvas, Size size) {
    final s = size.width;
    final c = Offset(s / 2, s / 2);
    final r = s * 0.345;
    final alt = onAmber ? _ember : const Color(0xFFD94F12);
    const face = Color(0xFFFFFFFF);

    final box = Rect.fromCircle(center: c, radius: r);

    // Solid face first, so every wedge sits inside a disc.
    canvas.drawCircle(c, r, Paint()..color = face);

    // Alternating wedges. -90deg so a boundary sits under the pointer, the
    // same convention the real wheel uses.
    const wedges = 8;
    const step = 2 * math.pi / wedges;
    final wedgePaint = Paint()..color = alt;
    for (var i = 1; i < wedges; i += 2) {
      canvas.drawArc(box, i * step - math.pi / 2, step, true, wedgePaint);
    }

    // The rim. Without it the wedges read as a fan's blades, not a wheel.
    canvas.drawCircle(
      c,
      r,
      Paint()
        ..color = alt
        ..style = PaintingStyle.stroke
        ..strokeWidth = math.max(1.2, s * 0.030),
    );

    // Hub: a wheel turns about something.
    final hr = r * 0.34;
    canvas.drawCircle(c, hr, Paint()..color = alt);
    canvas.drawCircle(c, hr * 0.42, Paint()..color = face);

    // Pointer — chunky on purpose; a fine one is the first thing lost at size.
    final pw = s * 0.098;
    final top = c.dy - r - s * 0.080;
    final tip = c.dy - r + s * 0.070;
    canvas.drawPath(
      Path()
        ..moveTo(c.dx, tip + s * 0.012)
        ..lineTo(c.dx - pw * 1.20, top - s * 0.012)
        ..lineTo(c.dx + pw * 1.20, top - s * 0.012)
        ..close(),
      Paint()..color = alt,
    );
    canvas.drawPath(
      Path()
        ..moveTo(c.dx, tip)
        ..lineTo(c.dx - pw, top)
        ..lineTo(c.dx + pw, top)
        ..close(),
      Paint()..color = face,
    );
  }

  @override
  bool shouldRepaint(_MarkPainter old) => old.onAmber != onAmber;
}
