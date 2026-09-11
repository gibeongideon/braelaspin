/// Entry point and app shell.
///
/// Composition root: builds the token store, the API client and the Store,
/// then mounts the UI. The only file that knows about all three layers.
///
///   core/  pure business logic and state — the Dart twin of web/src/core/
///   ui/    the only code that touches widgets
library;

import 'dart:async';

import 'package:flutter/services.dart';
import 'package:flutter/widgets.dart';

import 'core/api.dart';
import 'core/stakes.dart';
import 'core/store.dart';
import 'ui/screens/auth_screen.dart';
import 'ui/screens/simple_screens.dart';
import 'ui/screens/spin_screen.dart';
import 'ui/theme.dart';

/// Where the API lives. Baked at build time:
///   flutter run --dart-define=API_BASE=http://10.0.2.2:8080
const String kApiBase = String.fromEnvironment(
  'API_BASE',
  defaultValue: 'https://braelaspin.dafeapp.com',
);

/// Refresh-token storage, encrypted by the Android Keystore.
///
/// The ACCESS token is held in MEMORY ONLY: it lives 15 minutes, so persisting
/// it would widen exposure for no benefit. Only the long-lived refresh token
/// is written, and the real defence is server-side — rotation on every use,
/// with reuse detection killing the family.
class KeystoreTokens implements TokenStore {
  static const _channel = MethodChannel('braelaspin/secure');
  static const _refreshKey = 'refresh';

  String? _access;
  String? _refresh;

  @override
  String? get access => _access;

  @override
  String? get refresh => _refresh;

  Future<void> load() async {
    try {
      _refresh = await _channel.invokeMethod<String>('read', {'key': _refreshKey});
    } catch (_) {
      _refresh = null;
    }
  }

  @override
  Future<void> save(String access, String refresh) async {
    _access = access;
    _refresh = refresh;
    try {
      await _channel.invokeMethod<void>('write', {'key': _refreshKey, 'value': refresh});
    } catch (_) {
      // A device whose Keystore refuses us still plays; the session just does
      // not survive a restart.
    }
  }

  @override
  Future<void> clear() async {
    _access = null;
    _refresh = null;
    try {
      await _channel.invokeMethod<void>('delete', {'key': _refreshKey});
    } catch (_) {/* nothing to clean up */}
  }
}

Future<void> main() async {
  WidgetsFlutterBinding.ensureInitialized();
  await SystemChrome.setPreferredOrientations([DeviceOrientation.portraitUp]);
  SystemChrome.setSystemUIOverlayStyle(const SystemUiOverlayStyle(
    statusBarColor: Color(0x00000000),
    statusBarIconBrightness: Brightness.light,
    systemNavigationBarColor: T.bg,
    systemNavigationBarIconBrightness: Brightness.light,
  ));

  final tokens = KeystoreTokens();
  await tokens.load();

  late final Store store;
  final api = Api(
    baseUrl: kApiBase,
    tokens: tokens,
    onAuthLost: () => store.auth.value = AuthState.signedOut,
  );
  store = Store(api, tokens);

  seedStake(store);
  runApp(BraelaApp(store: store));
  unawaited(store.boot());
}

class BraelaApp extends StatelessWidget {
  const BraelaApp({super.key, required this.store});
  final Store store;

  @override
  Widget build(BuildContext context) => WidgetsApp(
        title: 'Braela Spin',
        color: T.bg,
        // No Material or Cupertino: the app draws its own five widgets, and
        // skipping both keeps the icon fonts and theme machinery out of the APK.
        builder: (context, _) => Container(
          decoration: const BoxDecoration(
            gradient: RadialGradient(
              center: Alignment(-0.7, -1.1),
              radius: 1.5,
              colors: [Color(0x2BF26B21), T.bg],
            ),
          ),
          child: _Gate(store: store),
        ),
        pageRouteBuilder: <T2>(RouteSettings settings, WidgetBuilder builder) =>
            PageRouteBuilder<T2>(
          settings: settings,
          pageBuilder: (ctx, __, ___) => builder(ctx),
        ),
      );
}

/// The auth gate: a spinner while we find out, then either sign-in or the app.
class _Gate extends StatelessWidget {
  const _Gate({required this.store});
  final Store store;

  @override
  Widget build(BuildContext context) => ValueListenableBuilder<AuthState>(
        valueListenable: store.auth,
        builder: (_, state, __) => switch (state) {
          AuthState.unknown => const Center(
              child: SizedBox(width: 24, height: 24, child: _Boot()),
            ),
          AuthState.signedOut => AuthScreen(store: store),
          AuthState.signedIn => Shell(store: store),
        },
      );
}

class _Boot extends StatelessWidget {
  const _Boot();
  @override
  Widget build(BuildContext context) =>
      const Center(child: Text('⚡', style: TextStyle(fontSize: 30)));
}

/// The signed-in shell: five tabs with the spin action raised in the centre,
/// matching the web client.
class Shell extends StatefulWidget {
  const Shell({super.key, required this.store});
  final Store store;

  @override
  State<Shell> createState() => _ShellState();
}

class _ShellState extends State<Shell> with WidgetsBindingObserver {
  int _tab = 2; // open on Spin
  bool _invite = false;

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addObserver(this);
  }

  @override
  void dispose() {
    WidgetsBinding.instance.removeObserver(this);
    super.dispose();
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    // The API is the source of truth; refresh whenever the app comes back.
    if (state == AppLifecycleState.resumed &&
        widget.store.auth.value == AuthState.signedIn) {
      widget.store.refreshMe().catchError((_) {});
    }
  }

  void _go(int tab) => setState(() {
        _tab = tab;
        _invite = false;
      });

  @override
  Widget build(BuildContext context) {
    final store = widget.store;
    final body = _invite
        ? InviteScreen(store: store)
        : switch (_tab) {
            0 => WalletScreen(store: store),
            1 => ResultsScreen(store: store),
            3 => HistoryScreen(store: store),
            4 => ProfileScreen(store: store, onInvite: () => setState(() => _invite = true)),
            _ => SpinScreen(store: store, onNavigate: _go),
          };

    return Stack(children: [
      Positioned.fill(child: body),
      Positioned(left: 0, right: 0, bottom: 0, child: _nav()),
    ]);
  }

  Widget _nav() {
    const items = [
      ('👛', 'Wallet'), ('🎯', 'Results'), ('⚡', 'Spin'), ('🕘', 'History'), ('👤', 'Profile'),
    ];
    return Container(
      height: 68 + MediaQuery.of(context).padding.bottom,
      padding: EdgeInsets.only(bottom: MediaQuery.of(context).padding.bottom),
      decoration: const BoxDecoration(
        color: Color(0xEE120D10),
        border: Border(top: BorderSide(color: T.border)),
      ),
      child: Row(
        children: [
          for (var i = 0; i < items.length; i++)
            Expanded(
              child: GestureDetector(
                behavior: HitTestBehavior.opaque,
                onTap: () {
                  HapticFeedback.selectionClick();
                  _go(i);
                },
                child: i == 2 ? _fab(items[i], i) : _tabItem(items[i], i),
              ),
            ),
        ],
      ),
    );
  }

  Widget _tabItem((String, String) item, int i) {
    final active = _tab == i && !_invite;
    return Column(
      mainAxisAlignment: MainAxisAlignment.center,
      children: [
        Opacity(opacity: active ? 1 : 0.45, child: Text(item.$1, style: const TextStyle(fontSize: 18))),
        const SizedBox(height: 3),
        Text(item.$2,
            style: TextStyle(fontSize: 10, color: active ? T.amberHi : T.t4)),
      ],
    );
  }

  /// The raised centre action, as in the reference design.
  Widget _fab((String, String) item, int i) => Column(
        mainAxisAlignment: MainAxisAlignment.center,
        children: [
          Transform.translate(
            offset: const Offset(0, -22),
            child: Container(
              width: 50, height: 50,
              alignment: Alignment.center,
              decoration: const BoxDecoration(
                shape: BoxShape.circle,
                gradient: T.gradAmber,
                boxShadow: [
                  BoxShadow(color: Color(0x73F26B21), blurRadius: 22, offset: Offset(0, 8)),
                  BoxShadow(color: T.bg, blurRadius: 0, spreadRadius: 5),
                ],
              ),
              child: const Text('⚡', style: TextStyle(fontSize: 21)),
            ),
          ),
          Transform.translate(
            offset: const Offset(0, -18),
            child: Text(item.$2,
                style: TextStyle(
                    fontSize: 10, color: _tab == i && !_invite ? T.amberHi : T.t4)),
          ),
        ],
      );
}

/// Seeds the opening stake once the config arrives, so the app never opens on
/// a bet the player cannot afford.
void seedStake(Store store) {
  store.config.addListener(() {
    if (store.stake.value == 0) store.stake.value = defaultStake(store.maxStake());
  });
}
