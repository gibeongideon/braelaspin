/// The spin screen — the product.
///
/// Deliberately spare, mirroring the web client. Everything on it is the
/// wheel, the bet, or the action; anything not needed mid-spin lives one tap
/// away. No amount field: betting is selection (core/stakes.dart), so no
/// keyboard ever covers the wheel and no typo can reach a bet.
library;

import 'package:flutter/services.dart';
import 'package:flutter/widgets.dart';

import '../../core/celebration.dart';
import '../../core/errors.dart';
import '../../core/models.dart';
import '../../core/money.dart';
import '../../core/outcome.dart';
import '../../core/stakes.dart';
import '../../core/store.dart';
import '../result_overlay.dart';
import '../theme.dart';
import '../ticker.dart';
import '../widgets.dart';
import '../wheel/wheel_view.dart';

class SpinScreen extends StatefulWidget {
  const SpinScreen({super.key, required this.store, required this.onNavigate});
  final Store store;
  final void Function(int tab) onNavigate;

  @override
  State<SpinScreen> createState() => _SpinScreenState();
}

class _SpinScreenState extends State<SpinScreen> {
  final _wheel = WheelController();
  final _wheelKey = GlobalKey<WheelViewState>();
  final _ticker = Ticker();

  Store get store => widget.store;

  @override
  void dispose() {
    _wheel.dispose();
    _ticker.dispose();
    super.dispose();
  }

  Future<void> _spin() async {
    if (_wheel.spinning) return;
    HapticFeedback.mediumImpact();
    try {
      final result = await store.placeSpin();

      // The bet is already committed server-side, so show it leaving now.
      store.applyStakeDebit(result);

      await _wheelKey.currentState?.spinTo(result.segmentIndex);

      // The payout lands only once the wheel has shown why.
      store.settleSpin(result);

      // Re-derive the payout from the stake and multiplier and compare with
      // what the server sent. A mismatch means the two disagree about the
      // arithmetic, and the player must not be shown a number we cannot stand
      // behind.
      try {
        final outcome = describeOutcome(
            result.stakeCents, result.multiplierBp, result.payoutCents);
        final party = celebrationFor(outcome.kind, outcome.multiplierBp);
        if (party.chime.isNotEmpty) _ticker.chime(party.chime);
        if (outcome.kind == OutcomeKind.win) HapticFeedback.heavyImpact();
        if (mounted) await showResult(context, outcome);
      } on PayoutMismatch catch (e) {
        debugPrint('$e');
        if (mounted) {
          showToast(context, 'We could not verify that result. Check your history.', kind: 'error');
        }
      }
      store.clearSpin();
    } on ApiException catch (e) {
      if (!mounted) return;
      // Turning the dead end into a route forward is the single most valuable
      // piece of UX in the app.
      if (e.code == 'insufficient_funds') {
        showToast(context, e.userMessage,
            kind: 'error', action: (label: 'Deposit', onTap: () => widget.onNavigate(0)));
        return;
      }
      if (e.code == 'stake_too_small' || e.code == 'stake_too_large') {
        store.stake.value = store.clampStake(store.stake.value);
      }
      showToast(context, e.userMessage, kind: e.kind == ErrorKind.timeout ? 'warn' : 'error');
    } catch (_) {
      if (mounted) showToast(context, 'Something went wrong.', kind: 'error');
    }
  }

  @override
  Widget build(BuildContext context) {
    return SafeArea(
      bottom: false,
      child: ListView(
        padding: const EdgeInsets.fromLTRB(16, 8, 16, 96),
        children: [
          _topBar(),
          const SizedBox(height: 14),
          _wheelStack(),
          const SizedBox(height: 16),
          _betCard(),
        ],
      ),
    );
  }

  Widget _topBar() => Row(
        children: [
          Container(
            width: 28, height: 28,
            alignment: Alignment.center,
            decoration: const BoxDecoration(
              gradient: T.gradAmber,
              borderRadius: BorderRadius.all(Radius.circular(9)),
            ),
            child: const Text('⚡', style: TextStyle(fontSize: 15)),
          ),
          const SizedBox(width: 9),
          const Text('BRAELA',
              style: TextStyle(
                fontSize: 15, fontWeight: FontWeight.w800, letterSpacing: 2, color: T.t1,
              )),
          const Spacer(),
          ValueListenableBuilder(
            valueListenable: store.balances,
            builder: (_, __, ___) => ValueListenableBuilder(
              valueListenable: store.hideBalance,
              builder: (_, hidden, ___) => ValueListenableBuilder(
                valueListenable: store.realMode,
                builder: (_, __, ___) => GestureDetector(
                  onTap: () => store.hideBalance.value = !store.hideBalance.value,
                  child: Container(
                    padding: const EdgeInsets.symmetric(horizontal: 13, vertical: 7),
                    decoration: BoxDecoration(
                      color: T.surface2,
                      border: Border.all(color: T.border),
                      borderRadius: T.brPill,
                    ),
                    child: Row(mainAxisSize: MainAxisSize.min, children: [
                      Container(
                        width: 17, height: 17,
                        decoration: const BoxDecoration(
                          shape: BoxShape.circle,
                          gradient: LinearGradient(colors: [T.gold, Color(0xFFC9940A)]),
                        ),
                      ),
                      const SizedBox(width: 8),
                      Text(
                        hidden ? '••••' : formatKes(store.activeBalance, symbol: false),
                        style: const TextStyle(
                          fontSize: 14, fontWeight: FontWeight.w700, color: T.t1,
                          fontFeatures: [FontFeature.tabularFigures()],
                        ),
                      ),
                      const SizedBox(width: 6),
                      const Text('👁', style: TextStyle(fontSize: 12, color: T.t3)),
                    ]),
                  ),
                ),
              ),
            ),
          ),
        ],
      );

  Widget _wheelStack() => ValueListenableBuilder<GameConfig>(
        valueListenable: store.config,
        builder: (_, cfg, __) {
          if (cfg.segments.isEmpty) {
            return const AspectRatio(aspectRatio: 1, child: SizedBox());
          }
          return AspectRatio(
            aspectRatio: 1,
            child: Stack(
              alignment: Alignment.topCenter,
              children: [
                // Ambient shadow and warm bounce light under the wheel.
                Positioned.fill(
                  child: Padding(
                    padding: const EdgeInsets.all(10),
                    child: Container(
                      decoration: const BoxDecoration(
                        shape: BoxShape.circle,
                        boxShadow: [
                          BoxShadow(color: Color(0xD9000000), blurRadius: 40, offset: Offset(0, 18)),
                          BoxShadow(color: Color(0x40F26B21), blurRadius: 70),
                        ],
                      ),
                    ),
                  ),
                ),
                Positioned.fill(
                  child: WheelView(
                    key: _wheelKey,
                    segments: cfg.segments,
                    controller: _wheel,
                    onTick: _ticker.tick,
                  ),
                ),
                const Positioned(top: -2, child: Flapper()),
              ],
            ),
          );
        },
      );

  Widget _betCard() => Card(
        padding: const EdgeInsets.all(14),
        child: Column(children: [
          _modeToggle(),
          const SizedBox(height: 14),
          _stepper(),
          const SizedBox(height: 12),
          _chips(),
          _limitHint(),
          const SizedBox(height: 12),
          ValueListenableBuilder<SpinPhase>(
            valueListenable: store.spin,
            builder: (_, phase, __) {
              final busy = phase == SpinPhase.requesting ||
                  phase == SpinPhase.spinning ||
                  phase == SpinPhase.checking;
              return ValueListenableBuilder(
                valueListenable: store.balances,
                builder: (_, __, ___) => AppButton(
                  label: switch (phase) {
                    SpinPhase.checking => 'Checking your spin…',
                    SpinPhase.spinning => 'Spinning…',
                    SpinPhase.requesting => 'Placing…',
                    _ => 'Spin now',
                  },
                  busy: busy,
                  onPressed: busy || store.maxStake() < store.config.value.minStakeCents
                      ? null
                      : _spin,
                ),
              );
            },
          ),
        ]),
      );

  Widget _modeToggle() => ValueListenableBuilder<bool>(
        valueListenable: store.realMode,
        builder: (_, real, __) => Container(
          padding: const EdgeInsets.all(4),
          decoration: BoxDecoration(
            color: const Color(0x4D000000),
            border: Border.all(color: T.border),
            borderRadius: T.brPill,
          ),
          child: Row(children: [
            _modeTab('🎮 Practice', !real, () => store.realMode.value = false, false),
            _modeTab('💵 Real money', real, () {
              if (store.balances.value.realCents <= 0) {
                showToast(context, 'Deposit first to play with real money.',
                    kind: 'warn', action: (label: 'Deposit', onTap: () => widget.onNavigate(0)));
                return;
              }
              store.realMode.value = true;
            }, true),
          ]),
        ),
      );

  Widget _modeTab(String label, bool active, VoidCallback onTap, bool isReal) => Expanded(
        child: GestureDetector(
          onTap: () {
            HapticFeedback.selectionClick();
            onTap();
          },
          child: Container(
            padding: const EdgeInsets.symmetric(vertical: 10),
            alignment: Alignment.center,
            decoration: BoxDecoration(
              gradient: active && isReal ? T.gradAmber : null,
              color: active && !isReal ? T.surface2 : null,
              borderRadius: T.brPill,
            ),
            child: Text(label,
                style: TextStyle(
                  fontSize: 13, fontWeight: FontWeight.w700,
                  color: active ? T.t1 : T.t2,
                )),
          ),
        ),
      );

  Widget _stepper() => ValueListenableBuilder<Cents>(
        valueListenable: store.stake,
        builder: (_, stake, __) => ValueListenableBuilder(
          valueListenable: store.balances,
          builder: (_, __, ___) {
            final ceiling = store.maxStake();
            return Row(children: [
              _stepBtn('−', stepDown(stake) == null, () {
                final n = stepDown(stake);
                if (n != null) store.stake.value = n;
              }),
              Expanded(
                child: Text(
                  formatKes(stake),
                  textAlign: TextAlign.center,
                  style: T.num0,
                ),
              ),
              _stepBtn('+', stepUp(stake, ceiling) == null, () {
                final n = stepUp(stake, ceiling);
                if (n != null) store.stake.value = n;
              }),
            ]);
          },
        ),
      );

  /// 54px square: comfortably above the 44px minimum touch target, and far
  /// enough apart to be hard to mis-tap mid-game.
  Widget _stepBtn(String glyph, bool disabled, VoidCallback onTap) => Opacity(
        opacity: disabled ? 0.3 : 1,
        child: GestureDetector(
          onTap: disabled
              ? null
              : () {
                  HapticFeedback.selectionClick();
                  onTap();
                },
          child: Container(
            width: 54, height: 54,
            alignment: Alignment.center,
            decoration: BoxDecoration(
              color: T.surface2,
              border: Border.all(color: T.border2),
              borderRadius: T.brR,
            ),
            child: Text(glyph,
                style: const TextStyle(fontSize: 26, fontWeight: FontWeight.w600, color: T.t1)),
          ),
        ),
      );

  Widget _chips() => ValueListenableBuilder<Cents>(
        valueListenable: store.stake,
        builder: (_, stake, __) => ValueListenableBuilder(
          valueListenable: store.balances,
          builder: (_, __, ___) {
            final ceiling = store.maxStake();
            return Row(
              children: [
                for (final c in kStakeLadder)
                  Expanded(
                    child: Padding(
                      padding: const EdgeInsets.symmetric(horizontal: 3),
                      child: Opacity(
                        opacity: c > ceiling ? 0.32 : 1,
                        child: GestureDetector(
                          onTap: c > ceiling
                              ? null
                              : () {
                                  HapticFeedback.selectionClick();
                                  store.stake.value = c;
                                },
                          child: Container(
                            padding: const EdgeInsets.symmetric(vertical: 9),
                            alignment: Alignment.center,
                            decoration: BoxDecoration(
                              gradient: stake == c ? T.gradAmber : null,
                              color: stake == c ? null : T.surface,
                              border: stake == c ? null : Border.all(color: T.border),
                              borderRadius: T.brPill,
                            ),
                            child: Text(
                              formatKes(c, symbol: false),
                              style: TextStyle(
                                fontSize: 13, fontWeight: FontWeight.w700,
                                color: stake == c ? T.t1 : T.t2,
                                fontFeatures: const [FontFeature.tabularFigures()],
                              ),
                            ),
                          ),
                        ),
                      ),
                    ),
                  ),
              ],
            );
          },
        ),
      );

  /// Only says something when it is not obvious: in practice mode the ceiling
  /// is the player's own free balance and needs no commentary. The line keeps
  /// its height so the button never jumps.
  Widget _limitHint() => ValueListenableBuilder(
        valueListenable: store.realMode,
        builder: (_, real, __) => ValueListenableBuilder(
          valueListenable: store.balances,
          builder: (_, __, ___) {
            final ceiling = store.maxStake();
            final min = store.config.value.minStakeCents;
            final text = !real
                ? ''
                : ceiling < min
                    ? 'Real-money play is unavailable right now.'
                    : 'Max bet ${formatKes(ceiling)}';
            return SizedBox(
              height: 26,
              child: Center(child: Text(text, style: T.hint)),
            );
          },
        ),
      );
}
