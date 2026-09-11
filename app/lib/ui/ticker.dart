/// Wheel sound.
///
/// Tones are synthesised natively (see MainActivity.kt) rather than shipped as
/// assets: a handful of sine bursts costs no APK bytes and no audio package,
/// and it matches the WebAudio approach the web client uses.
///
/// Ticks are THROTTLED. `tickFractions` caps at 220 boundary crossings over a
/// 9-second spin, which peaks around 24 per second early on — far faster than
/// the ear resolves, and far faster than the audio path can start clips. Above
/// ~14/sec it stops sounding like a wheel and starts sounding like noise.
library;

import 'package:flutter/services.dart';

class Ticker {
  static const _channel = MethodChannel('braelaspin/sound');

  bool enabled = true;
  DateTime _last = DateTime.fromMillisecondsSinceEpoch(0);

  static const _minGap = Duration(milliseconds: 70); // ~14/sec ceiling

  void tick() {
    if (!enabled) return;
    final now = DateTime.now();
    if (now.difference(_last) < _minGap) return;
    _last = now;

    // Fire and forget: a dropped tick is inaudible, and awaiting it would
    // stutter the animation.
    _channel.invokeMethod<void>('tick').catchError((_) {});
    HapticFeedback.selectionClick();
  }

  /// A short rising arpeggio for a win.
  void chime(List<double> notes) {
    if (!enabled || notes.isEmpty) return;
    _channel.invokeMethod<void>('chime', {
      'notes': notes,
      'noteMs': 200,
      'gapMs': 85,
    }).catchError((_) {});
  }

  void dispose() {}
}
