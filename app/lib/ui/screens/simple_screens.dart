/// Wallet, Results, History, Invite and Profile.
///
/// Grouped in one file because each is a list and a couple of cards; five
/// files of thirty lines would be more ceremony than structure.
library;

import 'package:flutter/services.dart';
import 'package:flutter/widgets.dart';

import '../../core/errors.dart';
import '../../core/models.dart';
import '../../core/money.dart';
import '../../core/store.dart';
import '../theme.dart';
import '../widgets.dart' as w;

// ── Wallet ──────────────────────────────────────────────────────────────────

class WalletScreen extends StatelessWidget {
  const WalletScreen({super.key, required this.store});
  final Store store;

  @override
  Widget build(BuildContext context) => SafeArea(
        bottom: false,
        child: ListView(
          padding: const EdgeInsets.fromLTRB(16, 14, 16, 96),
          children: [
            const Text('Wallet', style: T.h1),
            const Text('Deposit, withdraw and practice credit.', style: T.sub),
            const SizedBox(height: 16),
            ValueListenableBuilder<Balances>(
              valueListenable: store.balances,
              builder: (_, b, __) => w.Card(
                child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
                  const Text('Real balance', style: T.sub),
                  Text(formatKes(b.realCents),
                      style: const TextStyle(
                        fontSize: 32, fontWeight: FontWeight.w800, color: T.t1,
                        fontFeatures: [FontFeature.tabularFigures()],
                      )),
                  Text(
                    'Withdrawable ${formatKes(b.withdrawableCents)}'
                    '${b.heldCents > 0 ? ' · ${formatKes(b.heldCents)} held' : ''}',
                    style: T.hint,
                  ),
                  const SizedBox(height: 14),
                  Row(children: [
                    Expanded(
                      child: w.AppButton(
                        label: 'Deposit', compact: true,
                        onPressed: () => w.showToast(
                            context, 'M-Pesa deposits arrive in the next release.', kind: 'warn'),
                      ),
                    ),
                    const SizedBox(width: 10),
                    Expanded(
                      child: w.AppButton(
                        label: 'Withdraw', compact: true, primary: false,
                        onPressed: () => w.showToast(
                            context, 'M-Pesa withdrawals arrive in the next release.', kind: 'warn'),
                      ),
                    ),
                  ]),
                ]),
              ),
            ),
            const SizedBox(height: 12),
            ValueListenableBuilder<Balances>(
              valueListenable: store.balances,
              builder: (_, b, __) => w.Card(
                child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
                  const Text('🎮 Practice balance', style: T.h2),
                  const SizedBox(height: 8),
                  Row(children: [
                    Expanded(
                      child: Text(formatKes(b.demoCents),
                          style: const TextStyle(
                            fontSize: 22, fontWeight: FontWeight.w800, color: T.t1,
                            fontFeatures: [FontFeature.tabularFigures()],
                          )),
                    ),
                    SizedBox(
                      width: 110,
                      child: w.AppButton(
                        label: 'Top up', compact: true, primary: false,
                        onPressed: () async {
                          try {
                            await store.topUpDemo();
                            if (context.mounted) {
                              w.showToast(context, 'Practice balance topped up.', kind: 'win');
                            }
                          } on ApiException catch (e) {
                            if (context.mounted) {
                              w.showToast(context, e.userMessage, kind: 'error');
                            }
                          }
                        },
                      ),
                    ),
                  ]),
                  const SizedBox(height: 6),
                  const Text('Free credit for practice mode. Once a day.', style: T.hint),
                ]),
              ),
            ),
          ],
        ),
      );
}

// ── Results (spin history) ──────────────────────────────────────────────────

class ResultsScreen extends StatefulWidget {
  const ResultsScreen({super.key, required this.store});
  final Store store;

  @override
  State<ResultsScreen> createState() => _ResultsScreenState();
}

class _ResultsScreenState extends State<ResultsScreen> {
  List<SpinRow>? _rows;
  String? _error;

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    setState(() {
      _rows = null;
      _error = null;
    });
    try {
      final page = await widget.store.api.get<Map<String, dynamic>>('/v1/game/spins?limit=50');
      final items = ((page['items'] as List?) ?? [])
          .map((s) => SpinRow.fromJson(s as Map<String, dynamic>))
          .toList(growable: false);
      if (mounted) setState(() => _rows = items);
    } on ApiException catch (e) {
      if (mounted) setState(() => _error = e.userMessage);
    }
  }

  @override
  Widget build(BuildContext context) {
    final rows = _rows;
    return SafeArea(
      bottom: false,
      child: ListView(
        padding: const EdgeInsets.fromLTRB(16, 14, 16, 96),
        children: [
          const Text('Results', style: T.h1),
          const Text('Every result, with the maths shown.', style: T.sub),
          const SizedBox(height: 16),
          if (_error != null)
            w.Card(child: w.EmptyState(glyph: '⚠️', title: _error!, onRetry: _load))
          else if (rows == null)
            const w.Card(child: w.EmptyState(glyph: '⏳', title: 'Loading…'))
          else if (rows.isEmpty)
            const w.Card(
              child: w.EmptyState(
                glyph: '🎯', title: 'No spins yet.',
                hint: 'Your results will appear here.',
              ),
            )
          else ...[
            _totals(rows),
            const SizedBox(height: 12),
            w.Card(
              child: Column(
                children: [
                  for (final s in rows)
                    w.Row2(
                      icon: s.multiplierBp == 0 ? '—' : '${multiplierTimes(s.multiplierBp)}×',
                      // The identity, written out: bet x multiplier = payout.
                      title: '${formatKes(s.stakeCents)} × ${multiplierTimes(s.multiplierBp)}'
                          ' = ${formatKes(s.payoutCents)}',
                      meta: '${formatWhen(s.createdAt)} · segment ${s.segmentIndex}'
                          '${s.isReal ? '' : ' · practice'}',
                      trailing: formatSigned(s.payoutCents - s.stakeCents),
                      trailingColor: s.payoutCents > s.stakeCents ? T.greenHi : T.t3,
                    ),
                ],
              ),
            ),
          ],
          const SizedBox(height: 14),
          const Text(
            'Payout is always your bet multiplied by the segment you landed on.',
            style: T.hint, textAlign: TextAlign.center,
          ),
        ],
      ),
    );
  }

  /// Totals, so the screen can be reconciled against the balance.
  Widget _totals(List<SpinRow> rows) {
    final staked = rows.fold<int>(0, (n, s) => n + s.stakeCents);
    final won = rows.fold<int>(0, (n, s) => n + s.payoutCents);
    return Row(children: [
      _stat('Spins', '${rows.length}'),
      _stat('Staked', formatKes(staked)),
      _stat('Returned', formatKes(won)),
    ]);
  }

  Widget _stat(String k, String v) => Expanded(
        child: Container(
          margin: const EdgeInsets.symmetric(horizontal: 4),
          padding: const EdgeInsets.symmetric(vertical: 11, horizontal: 8),
          decoration: BoxDecoration(
            color: T.surface, border: Border.all(color: T.border), borderRadius: T.brR,
          ),
          child: Column(children: [
            Text(k.toUpperCase(),
                style: const TextStyle(fontSize: 10, color: T.t3, letterSpacing: 0.7)),
            const SizedBox(height: 3),
            Text(v,
                style: const TextStyle(
                  fontSize: 14, fontWeight: FontWeight.w700, color: T.t1,
                  fontFeatures: [FontFeature.tabularFigures()],
                )),
          ]),
        ),
      );
}

// ── History (the ledger) ────────────────────────────────────────────────────

const _kindLabel = {
  'demo_grant': 'Practice credit', 'bet': 'Bet', 'win': 'Win',
  'deposit': 'Deposit', 'withdraw': 'Withdrawal',
  'withdraw_reversed': 'Withdrawal refunded', 'referral': 'Referral commission',
  'adjustment': 'Adjustment',
};
const _kindGlyph = {
  'demo_grant': '🎁', 'bet': '🎯', 'win': '🏆', 'deposit': '⬇️',
  'withdraw': '⬆️', 'withdraw_reversed': '↩️', 'referral': '👥', 'adjustment': '⚙️',
};

class HistoryScreen extends StatefulWidget {
  const HistoryScreen({super.key, required this.store});
  final Store store;

  @override
  State<HistoryScreen> createState() => _HistoryScreenState();
}

class _HistoryScreenState extends State<HistoryScreen> {
  String? _error;

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    setState(() => _error = null);
    try {
      await widget.store.loadHistory();
    } on ApiException catch (e) {
      if (mounted) setState(() => _error = e.userMessage);
    }
  }

  @override
  Widget build(BuildContext context) => SafeArea(
        bottom: false,
        child: ListView(
          padding: const EdgeInsets.fromLTRB(16, 14, 16, 96),
          children: [
            const Text('History', style: T.h1),
            const Text('Every bet, win and payment.', style: T.sub),
            const SizedBox(height: 16),
            if (_error != null)
              w.Card(child: w.EmptyState(glyph: '⚠️', title: _error!, onRetry: _load))
            else
              ValueListenableBuilder<List<Transaction>>(
                valueListenable: widget.store.transactions,
                builder: (_, txs, __) => w.Card(
                  child: txs.isEmpty
                      ? const w.EmptyState(
                          glyph: '📭', title: 'No activity yet.',
                          hint: 'Your bets, wins and payments will appear here.')
                      : Column(children: [
                          for (final t in txs)
                            w.Row2(
                              icon: _kindGlyph[t.kind] ?? '•',
                              title: '${_kindLabel[t.kind] ?? t.kind}'
                                  '${t.isReal ? '' : '  (practice)'}',
                              meta: formatWhen(t.createdAt),
                              trailing: formatSigned(t.amountCents),
                              trailingColor: t.amountCents > 0 ? T.greenHi : T.t2,
                            ),
                        ]),
                ),
              ),
          ],
        ),
      );
}

// ── Invite ──────────────────────────────────────────────────────────────────

class InviteScreen extends StatelessWidget {
  const InviteScreen({super.key, required this.store});
  final Store store;

  @override
  Widget build(BuildContext context) => SafeArea(
        bottom: false,
        child: ValueListenableBuilder<User?>(
          valueListenable: store.user,
          builder: (_, user, __) => ListView(
            padding: const EdgeInsets.fromLTRB(16, 14, 16, 96),
            children: [
              const Text('Invite friends', style: T.h1),
              const Text('Earn a commission on every bet they place.', style: T.sub),
              const SizedBox(height: 16),
              ValueListenableBuilder<ReferralStats>(
                valueListenable: store.referrals,
                builder: (_, r, __) => Row(children: [
                  _stat('Friends', '${r.count}'),
                  _stat('Earned', formatKes(r.earnedCents)),
                  _stat('Last 30d', formatKes(r.last30Cents)),
                ]),
              ),
              const SizedBox(height: 12),
              w.Card(
                child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
                  const Text('Your referral link', style: T.h2),
                  const SizedBox(height: 8),
                  Container(
                    padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 12),
                    decoration: BoxDecoration(
                      color: const Color(0x47000000),
                      border: Border.all(color: T.border2),
                      borderRadius: T.brR,
                    ),
                    child: Text(user?.refLink ?? '—',
                        style: const TextStyle(fontSize: 13, color: T.t1)),
                  ),
                  const SizedBox(height: 12),
                  w.AppButton(
                    label: '📋 Copy link', compact: true, primary: false,
                    onPressed: user == null
                        ? null
                        : () async {
                            await Clipboard.setData(ClipboardData(text: user.refLink));
                            if (context.mounted) w.showToast(context, 'Link copied.');
                          },
                  ),
                  const SizedBox(height: 10),
                  Text(
                    'Code: ${user?.refCode ?? '—'} · You earn 2% of every bet your friends place.',
                    style: T.hint,
                  ),
                ]),
              ),
            ],
          ),
        ),
      );

  Widget _stat(String k, String v) => Expanded(
        child: Container(
          margin: const EdgeInsets.symmetric(horizontal: 4),
          padding: const EdgeInsets.symmetric(vertical: 11, horizontal: 8),
          decoration: BoxDecoration(
            color: T.surface, border: Border.all(color: T.border), borderRadius: T.brR,
          ),
          child: Column(children: [
            Text(k.toUpperCase(),
                style: const TextStyle(fontSize: 10, color: T.t3, letterSpacing: 0.7)),
            const SizedBox(height: 3),
            Text(v,
                style: const TextStyle(fontSize: 14, fontWeight: FontWeight.w700, color: T.t1)),
          ]),
        ),
      );
}

// ── Profile ─────────────────────────────────────────────────────────────────

class ProfileScreen extends StatelessWidget {
  const ProfileScreen({super.key, required this.store, required this.onInvite});
  final Store store;
  final VoidCallback onInvite;

  @override
  Widget build(BuildContext context) => SafeArea(
        bottom: false,
        child: ValueListenableBuilder<User?>(
          valueListenable: store.user,
          builder: (_, user, __) => ListView(
            padding: const EdgeInsets.fromLTRB(16, 14, 16, 96),
            children: [
              const Text('Profile', style: T.h1),
              const SizedBox(height: 16),
              w.Card(
                child: Column(children: [
                  w.Row2(
                    icon: '📱',
                    title: user?.phoneDisplay ?? '—',
                    meta: (user?.phoneVerified ?? false) ? 'Verified' : 'Not verified',
                  ),
                  GestureDetector(
                    onTap: onInvite,
                    child: w.Row2(
                      icon: '🎟',
                      title: 'Invite friends',
                      meta: 'Your code: ${user?.refCode ?? '—'} · earn 2%',
                      trailing: '›',
                    ),
                  ),
                ]),
              ),
              const SizedBox(height: 12),
              w.Card(
                child: w.AppButton(
                  label: 'Sign out', primary: false,
                  onPressed: () async {
                    await store.logout();
                    if (context.mounted) w.showToast(context, 'Signed out.');
                  },
                ),
              ),
              const SizedBox(height: 20),
              const Text('18+. Play responsibly. Gambling can be addictive.',
                  style: T.hint, textAlign: TextAlign.center),
            ],
          ),
        ),
      );
}
