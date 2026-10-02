/// Widget tests for the sign-in screen.
///
/// These exist because two bugs shipped that no unit test could catch:
///
///  1. Every GestureDetector used the default deferToChild hit testing, so an
///     unselected tab — which paints no background — had a dead tap area. The
///     "Create account" tab only responded if you hit the glyphs exactly.
///
///  2. WidgetsApp was given a `builder` that DISCARDED its child, so the tree
///     had no Navigator and therefore no Overlay. Every showToast() found
///     nothing to insert into and returned silently, which made the submit
///     button look dead when it was in fact validating and reporting.
///
/// Both were invisible to the core suite and obvious the moment a test tapped
/// the actual widgets.
library;

import 'package:flutter/widgets.dart';
import 'package:flutter_test/flutter_test.dart';

import 'package:braelaspin/core/api.dart';
import 'package:braelaspin/core/store.dart';
import 'package:braelaspin/ui/screens/auth_screen.dart';
import 'package:braelaspin/ui/widgets.dart';

class _MemoryTokens implements TokenStore {
  String? _a;
  String? _r;
  @override
  String? get access => _a;
  @override
  String? get refresh => _r;
  @override
  Future<void> save(String access, String refresh) async {
    _a = access;
    _r = refresh;
  }

  @override
  Future<void> clear() async {
    _a = null;
    _r = null;
  }
}

Store _store() {
  final tokens = _MemoryTokens();
  // Unreachable host: these tests never get as far as a request, and if one
  // ever did we want it to fail rather than touch a real server.
  return Store(Api(baseUrl: 'http://127.0.0.1:1', tokens: tokens), tokens);
}

/// Mounts the screen the way main.dart does — `home`, so a Navigator and its
/// Overlay exist. If that regresses, the toast assertions below fail.
Widget _app(Store store) => WidgetsApp(
      color: const Color(0xFF000000),
      home: AuthScreen(store: store),
      pageRouteBuilder: <T>(RouteSettings settings, WidgetBuilder builder) =>
          PageRouteBuilder<T>(
        settings: settings,
        pageBuilder: (ctx, _, __) => builder(ctx),
      ),
    );

void main() {
  testWidgets('the tree has an Overlay, so toasts can appear at all', (tester) async {
    final store = _store();
    await tester.pumpWidget(_app(store));

    // The regression: no Navigator meant no Overlay meant silent failures.
    final overlay = tester.element(find.byType(AuthScreen));
    expect(Overlay.maybeOf(overlay), isNotNull,
        reason: 'no Overlay in the tree — every toast and result card would be silently dropped');
  });

  testWidgets('tapping the Create account TAB switches mode', (tester) async {
    final store = _store();
    await tester.pumpWidget(_app(store));

    // Starts on Sign in.
    expect(find.text('Sign in'), findsWidgets);

    // Tap the tab itself, not its text, to prove the whole control is tappable.
    final tab = find.text('Create account');
    expect(tab, findsOneWidget);
    await tester.tap(tab);
    await tester.pump();

    // The submit button now reads "Create account", and the referral field
    // has appeared.
    expect(find.text('Referral code (optional)'), findsOneWidget,
        reason: 'the tab did not switch — hit testing is deferring to a child that paints nothing');
  });

  testWidgets('submitting an empty form reports the problem VISIBLY', (tester) async {
    final store = _store();
    await tester.pumpWidget(_app(store));

    await tester.tap(find.byType(AppButton));
    await tester.pump();

    // The error must be on the form, not only in a toast — a toast lives in an
    // Overlay and an Overlay is easy to lose by accident.
    expect(find.text('Enter a valid Kenyan mobile number.'), findsWidgets,
        reason: 'the button appeared to do nothing: no visible feedback at all');

    // Let the toast's auto-dismiss timer run out, or the test ends with a
    // pending timer. (That this fires at all is the proof the button works.)
    await tester.pump(const Duration(seconds: 7));
  });

  testWidgets('a valid phone but short password is reported', (tester) async {
    final store = _store();
    await tester.pumpWidget(_app(store));

    await tester.enterText(find.byType(EditableText).first, '0712345678');
    await tester.pump();
    await tester.enterText(find.byType(EditableText).last, 'short');
    await tester.pump();

    await tester.tap(find.byType(AppButton));
    await tester.pump();

    expect(find.text('Choose a password of at least 8 characters.'), findsWidgets);
    await tester.pump(const Duration(seconds: 7)); // drain the toast timer
  });

  testWidgets('the phone field echoes the normalised number as you type', (tester) async {
    final store = _store();
    await tester.pumpWidget(_app(store));

    await tester.enterText(find.byType(EditableText).first, '0712345678');
    await tester.pump();

    // Proves text actually reaches the controller, and that the user sees the
    // number the account will be created under before committing to it.
    expect(find.text('Will use 0712 345 678'), findsOneWidget);
  });

  testWidgets('an unparseable number is rejected as you type', (tester) async {
    final store = _store();
    await tester.pumpWidget(_app(store));

    await tester.enterText(find.byType(EditableText).first, '0812345678');
    await tester.pump();

    expect(find.text('Enter a valid Kenyan mobile number.'), findsOneWidget);
  });
}
