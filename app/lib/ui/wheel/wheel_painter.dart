/// The wheel, painted.
///
/// Performance shape, and the reason the whole thing holds 60fps on a cheap
/// phone: the static face is recorded ONCE into a [ui.Picture], and each frame
/// is one canvas rotate plus one drawPicture. Repainting twelve arcs and
/// twelve text layouts every frame — the obvious implementation — is what
/// makes wheels stutter on the hardware this app targets.
///
/// Realism comes from four cues:
///   1. A FIXED light source. The bezel and gloss are painted AFTER the
///      rotation, so highlights stay put while the wheel turns underneath. A
///      highlight that rotates with the wheel is the clearest possible tell
///      that something is a drawing and not an object.
///   2. Depth on the rim: an outer bevel and an inner shadow.
///   3. Pegs that belong to the wheel, sweeping past the fixed flapper.
///   4. Overshoot and settle, from core/spin_math.dart.
///
/// ALL angles come from core/spin_math.dart. This file paints; it does not
/// compute geometry. When the web renderer computed its own, it drifted half a
/// segment and the pointer showed the wrong prize on every spin.
library;

import 'dart:math' as math;
import 'dart:ui' as ui;

import 'package:flutter/widgets.dart';

import '../../core/models.dart';
import '../../core/money.dart';
import '../../core/spin_math.dart';
import '../theme.dart';

const double _deg = math.pi / 180;

class _Tier {
  const _Tier(this.a, this.b, this.text, this.rim);
  final Color a, b, text, rim;
}

/// Bigger prize, hotter tile.
///
/// Losing tiles alternate two dark shades: a run of identical dark tiles reads
/// as one big gap in the wheel, and alternating keeps all twelve segments
/// visible while it spins.
_Tier _tierFor(int multBp, int index) {
  if (multBp == 0) {
    return index.isEven
        ? const _Tier(Color(0xFF3A3037), Color(0xFF241D22), Color(0x80FFFFFF), Color(0x1AFFFFFF))
        : const _Tier(Color(0xFF2F262C), Color(0xFF1D171B), Color(0x6BFFFFFF), Color(0x12FFFFFF));
  }
  if (multBp <= 20000) {
    return const _Tier(Color(0xFF4FBF5F), Color(0xFF2C7838), Color(0xFFFFFFFF), Color(0x4DFFFFFF));
  }
  if (multBp <= 100000) {
    return const _Tier(Color(0xFFFF9648), Color(0xFFD04A0E), Color(0xFFFFFFFF), Color(0x4DFFFFFF));
  }
  if (multBp <= 500000) {
    return const _Tier(Color(0xFFFFD447), Color(0xFFC28F06), Color(0xFF3A2A00), Color(0x6BFFFFFF));
  }
  return const _Tier(Color(0xFFFF6FB1), Color(0xFFB81F96), Color(0xFFFFFFFF), Color(0x5CFFFFFF));
}

class WheelPainter extends CustomPainter {
  WheelPainter({required this.rotationDeg, required this.segments, required this.face})
      : super(repaint: null);

  final double rotationDeg;
  final List<Segment> segments;

  /// The pre-recorded static face. Built by [recordFace] when the size or the
  /// segments change, never per frame.
  final ui.Picture? face;

  @override
  void paint(Canvas canvas, Size size) {
    final r = size.width / 2;
    final c = Offset(r, r);

    if (face != null) {
      canvas.save();
      canvas.translate(c.dx, c.dy);
      canvas.rotate(rotationDeg * _deg);
      canvas.translate(-c.dx, -c.dy);
      canvas.drawPicture(face!);
      canvas.restore();
    }

    // Fixed overlays: the light does not turn with the wheel.
    canvas.save();
    canvas.translate(c.dx, c.dy);
    _paintBezel(canvas, size.width);
    _paintGloss(canvas, size.width);
    _paintHub(canvas, size.width);
    canvas.restore();
  }

  @override
  bool shouldRepaint(WheelPainter old) =>
      old.rotationDeg != rotationDeg || old.face != face;
}

/// Records the rotating part — tiles, labels, pegs — into a Picture.
ui.Picture recordFace(List<Segment> segments, double px) {
  final recorder = ui.PictureRecorder();
  final canvas = Canvas(recorder);
  final r = px / 2;
  canvas.translate(r, r);

  final n = segments.isEmpty ? 12 : segments.length;
  final tileOuter = r * 0.865;
  final tileInner = r * 0.455;
  final pegRadius = r * 0.905;

  // Backing disc, darker than any tile so the gaps read as depth.
  canvas.drawCircle(Offset.zero, r * 0.95, Paint()..color = const Color(0xFF0A0709));

  for (var i = 0; i < segments.length; i++) {
    final seg = segments[i];
    final arc = segmentArcDeg(i + 1, n);
    final gap = segmentAngle(n) * 0.045; // tight: real wheels have thin dividers
    final start = (arc.startDeg + gap) * _deg;
    final sweep = ((arc.endDeg - gap) - (arc.startDeg + gap)) * _deg;
    final mid = arc.midDeg * _deg;
    final t = _tierFor(seg.multiplierBp, i);

    final path = Path()
      ..arcTo(Rect.fromCircle(center: Offset.zero, radius: tileOuter), start, sweep, true)
      ..arcTo(Rect.fromCircle(center: Offset.zero, radius: tileInner), start + sweep, -sweep, false)
      ..close();

    // Radial gradient: lighter at the rim, darker toward the hub, so each tile
    // looks like a lit surface rather than a flat fill.
    canvas.drawPath(
      path,
      Paint()
        ..shader = ui.Gradient.radial(
          Offset.zero, tileOuter,
          [t.b, t.a, t.b], [0.0, 0.82, 1.0],
        ),
    );

    // Bright outer edge — the catch of light on a raised face.
    canvas.save();
    canvas.clipPath(path);
    canvas.drawArc(
      Rect.fromCircle(center: Offset.zero, radius: tileOuter - px * 0.004),
      start, sweep, false,
      Paint()
        ..color = t.rim
        ..style = PaintingStyle.stroke
        ..strokeWidth = px * 0.008,
    );
    canvas.restore();

    // Label, radial, flipped in the lower half so it is never upside down.
    final upsideDown = math.cos(mid) < 0;
    final tp = TextPainter(
      text: TextSpan(
        text: formatMultiplier(seg.multiplierBp),
        style: TextStyle(
          color: t.text,
          fontSize: px * (seg.multiplierBp >= 500000 ? 0.049 : 0.055),
          fontWeight: FontWeight.w800,
          shadows: seg.multiplierBp > 0
              ? [Shadow(color: const Color(0x73000000), blurRadius: px * 0.01, offset: Offset(0, px * 0.002))]
              : null,
        ),
      ),
      textDirection: TextDirection.ltr,
    )..layout();

    canvas.save();
    canvas.rotate(mid);
    canvas.translate((tileInner + tileOuter) / 2, 0);
    if (upsideDown) canvas.rotate(math.pi);
    tp.paint(canvas, Offset(-tp.width / 2, -tp.height / 2));
    canvas.restore();
  }

  // Pegs belong to the WHEEL, so they sweep past the fixed flapper — which is
  // what the ticking is.
  for (var i = 1; i <= n; i++) {
    final a = boundaryAngleDeg(i, n) * _deg;
    final p = Offset(math.cos(a) * pegRadius, math.sin(a) * pegRadius);
    final pr = px * 0.0105;
    canvas.drawCircle(
      p, pr,
      Paint()
        ..shader = ui.Gradient.radial(
          p.translate(-pr * 0.4, -pr * 0.4), pr,
          [const Color(0xFFFFFFFF), const Color(0xFFCDD2D8), const Color(0xFF6B7280)],
          [0.0, 0.5, 1.0],
        ),
    );
    canvas.drawCircle(
      p, pr,
      Paint()
        ..color = const Color(0x80000000)
        ..style = PaintingStyle.stroke
        ..strokeWidth = px * 0.0022,
    );
  }

  return recorder.endRecording();
}

void _paintBezel(Canvas canvas, double px) {
  final r = px / 2 * 0.955;
  final w = px * 0.022;

  canvas.drawCircle(
    Offset.zero, r,
    Paint()
      ..shader = ui.Gradient.linear(
        Offset(-r, -r), Offset(r, r),
        [
          const Color(0xFF6D6068), const Color(0xFF2B2329), const Color(0xFF151013),
          const Color(0xFF3A3138), const Color(0xFF0E0A0C),
        ],
        [0.0, 0.3, 0.55, 0.8, 1.0],
      )
      ..style = PaintingStyle.stroke
      ..strokeWidth = w,
  );

  // Inner shadow where the tiles meet the bezel — reads as recess.
  canvas.drawCircle(
    Offset.zero, r - w * 0.6,
    Paint()
      ..color = const Color(0x8C000000)
      ..style = PaintingStyle.stroke
      ..strokeWidth = px * 0.012,
  );
}

/// Specular sweep, anchored to the top-left while the wheel turns beneath it.
void _paintGloss(Canvas canvas, double px) {
  final r = px / 2 * 0.94;
  canvas.save();
  canvas.clipPath(Path()..addOval(Rect.fromCircle(center: Offset.zero, radius: r)));
  canvas.drawRect(
    Rect.fromCircle(center: Offset.zero, radius: r),
    Paint()
      ..shader = ui.Gradient.linear(
        Offset(-r, -r), Offset(r * 0.45, r * 0.55),
        [
          const Color(0x21FFFFFF), const Color(0x09FFFFFF),
          const Color(0x00FFFFFF), const Color(0x33000000),
        ],
        [0.0, 0.38, 0.7, 1.0],
      ),
  );
  canvas.restore();
}

/// Fixed hub: recessed ring, brushed plate, brand disc.
void _paintHub(Canvas canvas, double px) {
  final r = px * 0.225;

  canvas.drawCircle(Offset.zero, r * 1.06, Paint()..color = const Color(0xFF0A0709));

  canvas.drawCircle(
    Offset.zero, r,
    Paint()
      ..shader = ui.Gradient.linear(
        Offset(-r, -r), Offset(r, r),
        [const Color(0xFF3B323A), const Color(0xFF221B20), const Color(0xFF100C0F)],
        [0.0, 0.45, 1.0],
      ),
  );
  canvas.drawCircle(
    Offset.zero, r,
    Paint()
      ..color = const Color(0x17FFFFFF)
      ..style = PaintingStyle.stroke
      ..strokeWidth = px * 0.0035,
  );

  // Brand disc with a warm glow
  final br = r * 0.56;
  canvas.drawCircle(
    Offset.zero, br * 1.25,
    Paint()
      ..color = const Color(0x8CF26B21)
      ..maskFilter = MaskFilter.blur(BlurStyle.normal, px * 0.035),
  );
  canvas.drawCircle(
    Offset.zero, br,
    Paint()
      ..shader = ui.Gradient.linear(
        Offset(-br, -br), Offset(br, br),
        [const Color(0xFFFFA45C), T.amber, const Color(0xFFC53F08)],
        [0.0, 0.5, 1.0],
      ),
  );
  canvas.drawArc(
    Rect.fromCircle(center: Offset.zero, radius: br * 0.97),
    math.pi * 1.08, math.pi * 0.84, false,
    Paint()
      ..color = const Color(0x66FFFFFF)
      ..style = PaintingStyle.stroke
      ..strokeWidth = px * 0.004,
  );

  final tp = TextPainter(
    text: TextSpan(
      text: '⚡',
      style: TextStyle(fontSize: br * 1.05, color: const Color(0xFFFFFFFF)),
    ),
    textDirection: TextDirection.ltr,
  )..layout();
  tp.paint(canvas, Offset(-tp.width / 2, -tp.height / 2));
}
