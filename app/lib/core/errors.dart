/// Server error codes -> the words a player reads.
///
/// EVERY user-facing failure string lives in this one file. The server sends
/// terse stable codes and never prose, so copy can change (or be translated to
/// Swahili) without touching the backend, and both clients stay consistent.
library;

enum ErrorKind {
  /// never reached the server — provably nothing happened
  offline,

  /// sent, outcome unknown — the dangerous one
  timeout,

  /// 4xx, the request was wrong
  client,

  /// 401/403, session is over
  auth,

  /// 5xx, our fault
  server,
  rateLimited,
}

class ApiException implements Exception {
  ApiException({
    required this.kind,
    required this.code,
    this.status = 0,
    this.meta = const {},
  });

  final ErrorKind kind;
  final String code;
  final int status;
  final Map<String, dynamic> meta;

  /// True when the request certainly never took effect, so retrying is safe.
  bool get definitelyDidNotHappen =>
      kind == ErrorKind.offline ||
      kind == ErrorKind.client ||
      kind == ErrorKind.rateLimited;

  String get userMessage => userMessageFor(code, meta, kind);

  @override
  String toString() => 'ApiException($code)';
}

const Map<String, String> _copy = {
  // auth
  'invalid_credentials': 'That number or password is not correct.',
  'invalid_phone': 'Enter a valid Kenyan mobile number, e.g. 0712 345 678.',
  'phone_taken': 'That number is already registered. Try signing in instead.',
  'password_too_short': 'Choose a password of at least 8 characters.',
  'password_too_long': 'That password is too long.',
  'unknown_referral_code': "We don't recognise that referral code.",
  'token_reused': 'Your session has expired. Please sign in again.',
  'account_suspended': 'This account has been suspended. Contact support.',
  'unauthorized': 'Please sign in to continue.',
  'forbidden': "You don't have access to that.",
  // game
  'stake_too_small': 'That bet is below the minimum.',
  'stake_too_large': 'That bet is above the maximum.',
  'insufficient_funds': "You don't have enough for that bet.",
  'stake_exceeds_bankroll': 'That bet is too large right now. Try a smaller amount.',
  'spin_in_flight': 'That spin is still being processed. Please wait a moment.',
  // money
  'withdrawal_already_pending': 'You already have a withdrawal in progress.',
  'demo_topup_not_eligible': 'You can top up your practice balance once a day.',
  'mpesa_rejected': "M-Pesa couldn't start that payment. Please try again.",
  // generic
  'rate_limited': 'Too many attempts. Please wait a moment.',
  'bad_request': "That didn't look right. Please check and try again.",
  'not_found': 'Not found.',
  'internal': 'Something went wrong on our side. Please try again.',
};

const Map<ErrorKind, String> _byKind = {
  ErrorKind.offline: 'No connection. Your request was not sent.',
  ErrorKind.timeout: "That took too long. We're checking what happened.",
  ErrorKind.client: "That didn't look right. Please check and try again.",
  ErrorKind.auth: 'Please sign in to continue.',
  ErrorKind.server: 'Something went wrong on our side. Please try again.',
  ErrorKind.rateLimited: 'Too many attempts. Please wait a moment.',
};

String userMessageFor(
  String code, [
  Map<String, dynamic> meta = const {},
  ErrorKind kind = ErrorKind.client,
]) {
  // A few codes carry a number worth showing, so the player learns the actual
  // limit instead of guessing.
  if (code == 'rate_limited' && meta['retry_after_seconds'] is num) {
    final s = (meta['retry_after_seconds'] as num).round();
    return s > 60
        ? 'Too many attempts. Try again in ${(s / 60).ceil()} minutes.'
        : 'Too many attempts. Try again in $s seconds.';
  }
  return _copy[code] ?? _byKind[kind]!;
}
