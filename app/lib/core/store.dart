/// Application state and the operations that change it.
///
/// PURE Dart — no widgets. Views listen to the notifiers; nothing here knows a
/// view exists. Dart twin of `web/src/core/store.ts`, where `Signal<T>` is
/// `ValueNotifier<T>`: the same eleven pieces of state, the same operations.
///
/// The whole app's mutable state is the notifiers below. That is deliberate:
/// small enough to hold in your head, and each view listens only to what it
/// draws, so a spin repaints the wheel and the balance and nothing else.
library;

import 'package:flutter/foundation.dart';

import 'api.dart';
import 'errors.dart';
import 'models.dart';
import 'outcome.dart';

enum AuthState { unknown, signedOut, signedIn }

/// What the spin is doing right now. The UI is a function of this.
enum SpinPhase {
  idle,
  requesting,
  spinning,
  settled,

  /// Sent, outcome unknown. We must NOT tell the user it failed.
  checking,
}

class Store {
  Store(this.api, this.tokens);

  final Api api;
  final TokenStore tokens;

  final auth = ValueNotifier<AuthState>(AuthState.unknown);
  final user = ValueNotifier<User?>(null);
  final balances = ValueNotifier<Balances>(const Balances());
  final config = ValueNotifier<GameConfig>(const GameConfig());
  final referrals = ValueNotifier<ReferralStats>(const ReferralStats());

  final stake = ValueNotifier<Cents>(1000); // KES 10
  final realMode = ValueNotifier<bool>(false);
  final hideBalance = ValueNotifier<bool>(false);
  final spin = ValueNotifier<SpinPhase>(SpinPhase.idle);
  final transactions = ValueNotifier<List<Transaction>>(const []);

  SpinResult? lastResult;

  /// The balance the active mode spends from.
  Cents get activeBalance =>
      realMode.value ? balances.value.realCents : balances.value.demoCents;

  /// The largest stake the server would currently accept, or 0 when none is.
  ///
  /// 0 is a MEANINGFUL value for both inputs — an empty bankroll caps real
  /// play at zero, and an empty wallet affords nothing — so neither may be
  /// treated as "unset".
  Cents maxStake() {
    final limits = <Cents>[activeBalance];
    // The bankroll cap applies to real play only; demo has no house exposure.
    if (realMode.value) limits.add(config.value.maxStakeCents);
    final m = limits.reduce((a, b) => a < b ? a : b);
    return m < 0 ? 0 : m;
  }

  /// Clamp a stake into the acceptable range, or 0 when nothing is affordable.
  Cents clampStake(Cents cents) {
    final min = config.value.minStakeCents;
    final ceiling = maxStake();
    if (ceiling < min) return 0;
    return cents < min ? min : (cents > ceiling ? ceiling : cents);
  }

  // ── session ────────────────────────────────────────────────────────────

  Future<void> boot() async {
    if (tokens.refresh == null) {
      auth.value = AuthState.signedOut;
      return;
    }
    try {
      await refreshMe();
      auth.value = AuthState.signedIn;
    } catch (_) {
      await tokens.clear();
      auth.value = AuthState.signedOut;
    }
  }

  Future<void> register(String phone, String password, [String? refCode]) async {
    final j = await api.post<Map<String, dynamic>>('/v1/auth/register', {
      'phone': phone,
      'password': password,
      if (refCode != null && refCode.isNotEmpty) 'ref_code': refCode,
    }, false);
    await _adopt(Session.fromJson(j));
    await refreshMe();
  }

  Future<void> login(String phone, String password) async {
    final j = await api.post<Map<String, dynamic>>(
        '/v1/auth/login', {'phone': phone, 'password': password}, false);
    await _adopt(Session.fromJson(j));
    await refreshMe();
  }

  Future<void> logout() async {
    final rt = tokens.refresh;
    try {
      if (rt != null) await api.post<void>('/v1/auth/logout', {'refresh': rt}, false);
    } catch (_) {
      // Best effort: the local session is cleared regardless, and a failed
      // revoke must never trap the user in a signed-in state.
    }
    await tokens.clear();
    user.value = null;
    balances.value = const Balances();
    transactions.value = const [];
    auth.value = AuthState.signedOut;
  }

  Future<void> _adopt(Session s) async {
    await tokens.save(s.access, s.refresh);
    user.value = s.user;
    auth.value = AuthState.signedIn;
  }

  /// One call that repaints everything. Run on launch and on resume.
  Future<void> refreshMe() async {
    final me = Me.fromJson(await api.get<Map<String, dynamic>>('/v1/me'));
    user.value = me.user;
    balances.value = me.balances;
    config.value = me.game;
    referrals.value = me.referrals;
  }

  Future<void> refreshBalances() async {
    balances.value = Balances.fromJson(await api.get<Map<String, dynamic>>('/v1/wallet'));
  }

  Future<void> loadHistory() async {
    final page = await api.get<Map<String, dynamic>>('/v1/history?limit=50');
    transactions.value = ((page['items'] as List?) ?? [])
        .map((t) => Transaction.fromJson(t as Map<String, dynamic>))
        .toList(growable: false);
  }

  Future<void> topUpDemo() async {
    await api.post<Map<String, dynamic>>('/v1/wallet/demo/topup');
    await refreshBalances();
  }

  // ── the spin ───────────────────────────────────────────────────────────

  /// Places one spin.
  ///
  /// A TIMEOUT is not a failure: the bet may have been taken. We move to
  /// `checking` and ask the server what actually happened rather than telling
  /// the user it did not go through — telling someone their money-losing bet
  /// failed, when it did not, is the worst thing this client could do.
  Future<SpinResult> placeSpin() async {
    final clientRef = newClientRef();
    spin.value = SpinPhase.requesting;

    try {
      final j = await api.post<Map<String, dynamic>>(
        '/v1/game/spin',
        {'stake_cents': stake.value, 'real': realMode.value, 'client_ref': clientRef},
        true,
        clientRef,
      );
      final result = SpinResult.fromJson(j);
      lastResult = result;
      spin.value = SpinPhase.spinning;
      return result;
    } on ApiException catch (e) {
      if (e.kind == ErrorKind.timeout) {
        spin.value = SpinPhase.checking;
        final recovered = await _recoverSpin(clientRef);
        if (recovered != null) {
          lastResult = recovered;
          spin.value = SpinPhase.spinning;
          return recovered;
        }
      }
      spin.value = SpinPhase.idle;
      rethrow;
    }
  }

  /// Did the spin we lost the response to actually happen?
  Future<SpinResult?> _recoverSpin(String clientRef) async {
    for (var attempt = 0; attempt < 3; attempt++) {
      await Future<void>.delayed(Duration(seconds: attempt + 1));
      try {
        final j = await api.get<Map<String, dynamic>>(
            '/v1/game/spins/by-ref/${Uri.encodeComponent(clientRef)}');
        return SpinResult.fromJson(j);
      } on ApiException catch (e) {
        // 404 means it genuinely never happened — stop asking.
        if (e.status == 404) return null;
      }
    }
    return null;
  }

  /// Phase 1: the bet has been taken.
  ///
  /// Applied as soon as the server accepts the spin, BEFORE the wheel
  /// animates, so the player sees their stake leave immediately — which is
  /// what actually happened, since the debit is already committed.
  void applyStakeDebit(SpinResult r) =>
      _setBalance(r.isReal, balanceAfterStake(r.balanceCents, r.payoutCents));

  /// Phase 2: the wheel has stopped, so the payout lands.
  ///
  /// On a win the balance rises; on a loss it is already at the post-bet
  /// figure and nothing moves. Deliberately NOT applied before the animation:
  /// the number must not change before the wheel has shown why.
  void settleSpin(SpinResult r) {
    _setBalance(r.isReal, r.balanceCents);
    spin.value = SpinPhase.settled;
  }

  void _setBalance(bool isReal, Cents cents) {
    balances.value = isReal
        ? balances.value.copyWith(realCents: cents)
        : balances.value.copyWith(demoCents: cents);
  }

  void clearSpin() => spin.value = SpinPhase.idle;

  void dispose() {
    for (final n in [auth, user, balances, config, referrals, stake, realMode,
                     hideBalance, spin, transactions]) {
      n.dispose();
    }
    api.close();
  }
}
