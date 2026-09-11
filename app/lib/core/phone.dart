/// Kenyan mobile number normalisation.
///
/// A deliberate mirror of `internal/auth/phone.go` and
/// `web/src/core/phone.ts`. The server re-normalises everything it receives
/// and is the authority; this exists so the user SEES the number that will be
/// used before they submit.
///
/// Keep all three in lockstep.
library;

class PhoneResult {
  const PhoneResult.ok(this.phone) : valid = true;
  const PhoneResult.invalid() : phone = '', valid = false;
  final bool valid;
  final String phone;
}

/// Converts any accepted spelling to 254XXXXXXXXX.
///
/// Anything else is REJECTED rather than coerced. The reference implementation
/// appended "-need_update" to numbers it could not parse and stored them
/// anyway, which later caused duplicate accounts on deposit.
PhoneResult normalisePhone(String raw) {
  if (raw.trim().isEmpty) return const PhoneResult.invalid();

  final digits = StringBuffer();
  for (final ch in raw.split('')) {
    if (ch.codeUnitAt(0) >= 48 && ch.codeUnitAt(0) <= 57) {
      digits.write(ch);
    } else if ('+ -().'.contains(ch)) {
      continue; // punctuation people type
    } else {
      return const PhoneResult.invalid(); // a letter makes it invalid
    }
  }

  final d = digits.toString();
  String national;
  if (d.length == 12 && d.startsWith('254')) {
    national = d.substring(3);
  } else if (d.length == 10 && d.startsWith('0')) {
    national = d.substring(1);
  } else if (d.length == 9) {
    national = d;
  } else {
    return const PhoneResult.invalid();
  }

  // 7 = Safaricom/Airtel, 1 = the 01x range.
  if (!national.startsWith('7') && !national.startsWith('1')) {
    return const PhoneResult.invalid();
  }
  return PhoneResult.ok('254$national');
}

/// 254712345678 -> "0712 345 678"
String prettyPhone(String phone) {
  if (phone.length != 12 || !phone.startsWith('254')) return phone;
  final n = phone.substring(3);
  return '0${n.substring(0, 3)} ${n.substring(3, 6)} ${n.substring(6)}';
}

/// 254712345678 -> "2547*****678", for anything shown to another user.
String maskPhone(String phone) {
  if (phone.length < 7) return '***';
  return phone.substring(0, 4) +
      '*' * (phone.length - 7) +
      phone.substring(phone.length - 3);
}
