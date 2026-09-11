/// Money and time formatting.
///
/// Hand-written rather than package:intl, for the reason stated in the plan:
/// we need exactly two formats and the locale tables cost more than the
/// feature is worth. Dart twin of `web/src/core/money.ts`.
library;

import 'models.dart';

/// "KES 1,234.50" — the canonical display form.
String formatKes(Cents cents, {bool symbol = true}) {
  final negative = cents < 0;
  final abs = cents.abs();
  final shillings = abs ~/ 100;
  final rem = abs % 100;

  var s = _group(shillings);
  // Whole amounts read better without ".00"; partial amounts need the cents.
  if (rem != 0) s += '.${rem.toString().padLeft(2, '0')}';

  return '${negative ? '-' : ''}${symbol ? 'KES ' : ''}$s';
}

/// Signed form for ledger rows: "+KES 500" / "-KES 20".
String formatSigned(Cents cents) {
  final sign = cents > 0 ? '+' : (cents < 0 ? '-' : '');
  return '$sign${formatKes(cents.abs())}';
}

String _group(int n) {
  final s = n.toString();
  if (s.length <= 3) return s;
  final b = StringBuffer();
  for (var i = 0; i < s.length; i++) {
    if (i > 0 && (s.length - i) % 3 == 0) b.write(',');
    b.write(s[i]);
  }
  return b.toString();
}

/// "2x", "200x", or an em dash for a losing segment.
String formatMultiplier(BasisPoints bp) {
  if (bp == 0) return '—';
  final x = bp / 10000;
  return '${x == x.roundToDouble() ? x.toInt() : x.toStringAsFixed(1)}x';
}

/// Basis points as a whole number of times: 50000 -> 5.
num multiplierTimes(BasisPoints bp) {
  final x = bp / 10000;
  return x == x.roundToDouble() ? x.toInt() : x;
}

/// "just now" / "5 min ago" / "3 h ago" / "12 Sep".
String formatWhen(String iso, {DateTime? now}) {
  final then = DateTime.tryParse(iso);
  if (then == null) return '';
  final ref = now ?? DateTime.now();
  final secs = ref.difference(then).inSeconds;

  if (secs < 45) return 'just now';
  if (secs < 3600) return '${secs ~/ 60} min ago';
  if (secs < 86400) return '${secs ~/ 3600} h ago';
  if (secs < 7 * 86400) return '${secs ~/ 86400} d ago';

  const months = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun',
                  'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec'];
  final d = '${then.day} ${months[then.month - 1]}';
  return then.year == ref.year ? d : '$d ${then.year}';
}
