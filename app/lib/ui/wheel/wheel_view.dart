/// The wheel widget: the painter plus an AnimationController.
///
/// `CustomPaint(painter: ...)` driven by an AnimatedBuilder scoped to the
/// canvas alone, so a spin repaints 340x340 pixels and rebuilds no widgets at
/// all. The face Picture is rebuilt only when the size or segments change.
library;

import 'dart:ui' as ui;

import 'package:flutter/widgets.dart';

import '../../core/models.dart';
import '../../core/spin_math.dart';
import 'wheel_painter.dart';

class WheelController extends ChangeNotifier {
  double _rotation = restingRotation(12);
  double get rotation => _rotation;

  bool _spinning = false;
  bool get spinning => _spinning;

  void _set(double v) {
    _rotation = v;
    notifyListeners();
  }
}

class WheelView extends StatefulWidget {
  const WheelView({
    super.key,
    required this.segments,
    required this.controller,
    this.onTick,
  });

  final List<Segment> segments;
  final WheelController controller;
  final VoidCallback? onTick;

  @override
  State<WheelView> createState() => WheelViewState();
}

class WheelViewState extends State<WheelView> with SingleTickerProviderStateMixin {
  late final AnimationController _anim = AnimationController(vsync: this, duration: kSpinDuration);

  ui.Picture? _face;
  double _facePx = 0;
  int _faceSegments = 0;

  double _from = 0;
  double _to = 0;
  List<double> _ticks = const [];
  int _nextTick = 0;

  @override
  void initState() {
    super.initState();
    _anim.addListener(_onFrame);
  }

  void _onFrame() {
    final n = widget.segments.length;
    if (n == 0) return;
    widget.controller._set(rotationWithSettle(_from, _to, _anim.value, n));

    while (_nextTick < _ticks.length && _ticks[_nextTick] <= _anim.value) {
      widget.onTick?.call();
      _nextTick++;
    }
  }

  /// Animate to the segment the SERVER chose. Completes when it comes to rest.
  Future<void> spinTo(int segment) async {
    final n = widget.segments.length;
    if (n == 0) return;

    final reduced = MediaQuery.maybeDisableAnimationsOf(context) ?? false;
    _from = widget.controller.rotation;
    _to = targetAngle(_from, segment, n);

    // Reduced motion: show the outcome immediately rather than denying it.
    if (reduced) {
      widget.controller._set(_to);
      return;
    }

    _ticks = tickFractions(_from, _to, n);
    _nextTick = 0;
    widget.controller._spinning = true;

    _anim.reset();
    await _anim.forward();

    widget.controller._set(_to);
    widget.controller._spinning = false;

    // The animation must agree with the server. If it ever does not, that is a
    // geometry bug and we want to know in debug rather than quietly showing
    // the player the wrong prize.
    assert(() {
      final landed = indicatedSegment(_to, n);
      if (landed != segment) {
        throw StateError('wheel landed on $landed but the server said $segment');
      }
      return true;
    }());
  }

  void _ensureFace(double px) {
    if (_face != null && _facePx == px && _faceSegments == widget.segments.length) return;
    _face?.dispose();
    _face = recordFace(widget.segments, px);
    _facePx = px;
    _faceSegments = widget.segments.length;
  }

  @override
  void dispose() {
    _anim.dispose();
    _face?.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return LayoutBuilder(
      builder: (context, constraints) {
        final px = constraints.maxWidth;
        _ensureFace(px);
        return AnimatedBuilder(
          animation: widget.controller,
          builder: (_, __) => CustomPaint(
            size: Size(px, px),
            painter: WheelPainter(
              rotationDeg: widget.controller.rotation,
              segments: widget.segments,
              face: _face,
            ),
          ),
        );
      },
    );
  }
}

/// The flapper: a mounted post with a tapered needle, fixed at 12 o'clock
/// while the pegs sweep underneath it.
class Flapper extends StatelessWidget {
  const Flapper({super.key, this.size = 26});
  final double size;

  @override
  Widget build(BuildContext context) {
    return SizedBox(
      width: size,
      height: size * 1.5,
      child: CustomPaint(painter: _FlapperPainter()),
    );
  }
}

class _FlapperPainter extends CustomPainter {
  @override
  void paint(Canvas canvas, Size size) {
    final w = size.width;
    final h = size.height;

    canvas.drawPath(
      Path()
        ..moveTo(w / 2, h)
        ..lineTo(0, h * 0.26)
        ..lineTo(w, h * 0.26)
        ..close(),
      Paint()
        ..shader = ui.Gradient.linear(
          Offset(w / 2, h * 0.26), Offset(w / 2, h),
          [const Color(0xFFFF8A3D), const Color(0xFFD94F12), const Color(0xFFA8380A)],
          [0.0, 0.65, 1.0],
        ),
    );

    final capR = w * 0.31;
    final capC = Offset(w / 2, capR);
    canvas.drawCircle(
      capC, capR,
      Paint()
        ..shader = ui.Gradient.radial(
          capC.translate(-capR * 0.32, -capR * 0.4), capR * 1.4,
          [
            const Color(0xFFFFFFFF), const Color(0xFFE8E2E4),
            const Color(0xFF9AA0A8), const Color(0xFF4B5157),
          ],
          [0.0, 0.22, 0.6, 1.0],
        ),
    );
  }

  @override
  bool shouldRepaint(_FlapperPainter oldDelegate) => false;
}
