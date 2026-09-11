/// Design tokens, mirroring `web/src/ui/styles.css`.
///
/// No Material theme: the app uses five colours and three text sizes, and
/// `uses-material-design: false` in pubspec keeps the icon font out of the
/// APK. Everything here is a const, so it costs nothing at runtime.
library;

import 'package:flutter/widgets.dart';

class T {
  const T._();

  // ground — warm, not blue-black
  static const bg = Color(0xFF120D10);
  static const surface = Color(0x0BFFFFFF);
  static const surface2 = Color(0x12FFFFFF);
  static const border = Color(0x17FFFFFF);
  static const border2 = Color(0x29FFFFFF);

  // brand
  static const amber = Color(0xFFF26B21);
  static const amberHi = Color(0xFFFF8A3D);
  static const amberLo = Color(0xFFD94F12);

  // semantic
  static const green = Color(0xFF3FA34D);
  static const greenHi = Color(0xFF52C163);
  static const red = Color(0xFFE0523F);
  static const gold = Color(0xFFF5C518);
  static const pink = Color(0xFFFF5EA8);

  // text
  static const t1 = Color(0xFFFFFFFF);
  static const t2 = Color(0xB8FFFFFF);
  static const t3 = Color(0x73FFFFFF);
  static const t4 = Color(0x47FFFFFF);

  static const r = Radius.circular(14);
  static const rLg = Radius.circular(22);
  static const pill = Radius.circular(999);

  static const brR = BorderRadius.all(r);
  static const brLg = BorderRadius.all(rLg);
  static const brPill = BorderRadius.all(pill);

  // type — a deliberately small scale
  static const h1 = TextStyle(fontSize: 21, fontWeight: FontWeight.w800, color: t1, height: 1.2);
  static const h2 = TextStyle(fontSize: 16, fontWeight: FontWeight.w700, color: t1);
  static const body = TextStyle(fontSize: 15, color: t1, height: 1.4);
  static const sub = TextStyle(fontSize: 13, color: t3, height: 1.4);
  static const hint = TextStyle(fontSize: 12, color: t3);
  static const num0 = TextStyle(
    fontSize: 27, fontWeight: FontWeight.w800, color: t1,
    fontFeatures: [FontFeature.tabularFigures()],
  );

  static const gradAmber = LinearGradient(
    begin: Alignment.topLeft, end: Alignment.bottomRight,
    colors: [amberHi, amberLo],
  );
}
