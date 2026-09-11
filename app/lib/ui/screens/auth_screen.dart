/// Sign in / sign up. One screen, two tabs.
library;

import 'package:flutter/services.dart';
import 'package:flutter/widgets.dart';

import '../../core/errors.dart';
import '../../core/phone.dart';
import '../../core/store.dart';
import '../theme.dart';
import '../widgets.dart';

class AuthScreen extends StatefulWidget {
  const AuthScreen({super.key, required this.store});
  final Store store;

  @override
  State<AuthScreen> createState() => _AuthScreenState();
}

class _AuthScreenState extends State<AuthScreen> {
  final _phone = TextEditingController();
  final _password = TextEditingController();
  final _ref = TextEditingController();

  bool _register = false;
  bool _busy = false;
  String? _phoneHelper;
  String? _phoneError;

  @override
  void dispose() {
    _phone.dispose();
    _password.dispose();
    _ref.dispose();
    super.dispose();
  }

  /// Echo the normalised number back as they type, so they see exactly what
  /// the account will be created under before committing to it.
  void _onPhone(String raw) {
    setState(() {
      if (raw.trim().isEmpty) {
        _phoneHelper = null;
        _phoneError = null;
        return;
      }
      final r = normalisePhone(raw);
      if (r.valid) {
        _phoneHelper = 'Will use ${prettyPhone(r.phone)}';
        _phoneError = null;
      } else {
        _phoneHelper = null;
        _phoneError = 'Enter a valid Kenyan mobile number.';
      }
    });
  }

  Future<void> _submit() async {
    final p = normalisePhone(_phone.text);
    if (!p.valid) {
      showToast(context, 'Enter a valid Kenyan mobile number.', kind: 'error');
      return;
    }
    if (_password.text.length < 8) {
      showToast(context, 'Choose a password of at least 8 characters.', kind: 'error');
      return;
    }

    setState(() => _busy = true);
    try {
      if (_register) {
        await widget.store.register(p.phone, _password.text, _ref.text.trim());
        if (mounted) showToast(context, 'Welcome! Practice balance added.', kind: 'win');
      } else {
        await widget.store.login(p.phone, _password.text);
      }
    } on ApiException catch (e) {
      if (mounted) showToast(context, e.userMessage, kind: 'error');
    } catch (_) {
      if (mounted) showToast(context, 'Something went wrong.', kind: 'error');
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    return SafeArea(
      child: ListView(
        padding: const EdgeInsets.fromLTRB(20, 40, 20, 40),
        children: [
          Center(
            child: Column(
              children: [
                Container(
                  width: 60, height: 60,
                  alignment: Alignment.center,
                  decoration: const BoxDecoration(
                    gradient: T.gradAmber,
                    borderRadius: BorderRadius.all(Radius.circular(19)),
                    boxShadow: [BoxShadow(color: Color(0x66F26B21), blurRadius: 32, offset: Offset(0, 12))],
                  ),
                  child: const Text('⚡', style: TextStyle(fontSize: 30)),
                ),
                const SizedBox(height: 13),
                const Text('Braela Spin', style: T.h1),
                const SizedBox(height: 3),
                const Text('Spin the wheel. Win up to 200x.', style: T.sub),
              ],
            ),
          ),
          const SizedBox(height: 26),
          Container(
            padding: const EdgeInsets.all(4),
            decoration: BoxDecoration(
              color: const Color(0x4D000000),
              border: Border.all(color: T.border),
              borderRadius: T.brPill,
            ),
            child: Row(children: [
              _tab('Sign in', !_register, () => setState(() => _register = false)),
              _tab('Create account', _register, () => setState(() => _register = true)),
            ]),
          ),
          const SizedBox(height: 18),
          Field(
            controller: _phone,
            label: 'Phone number',
            keyboard: TextInputType.phone,
            helper: _phoneHelper,
            error: _phoneError,
            onChanged: _onPhone,
          ),
          const SizedBox(height: 12),
          Field(controller: _password, label: 'Password', obscure: true),
          if (_register) ...[
            const SizedBox(height: 12),
            Field(controller: _ref, label: 'Referral code (optional)'),
          ],
          const SizedBox(height: 20),
          AppButton(
            label: _register ? 'Create account' : 'Sign in',
            busy: _busy,
            onPressed: _submit,
          ),
          const SizedBox(height: 18),
          const Text(
            'New accounts get KES 5,000 in practice credit. 18+. Play responsibly.',
            style: T.hint,
            textAlign: TextAlign.center,
          ),
        ],
      ),
    );
  }

  Widget _tab(String label, bool active, VoidCallback onTap) => Expanded(
        child: GestureDetector(
          onTap: () {
            HapticFeedback.selectionClick();
            onTap();
          },
          child: Container(
            padding: const EdgeInsets.symmetric(vertical: 10),
            alignment: Alignment.center,
            decoration: BoxDecoration(
              color: active ? T.surface2 : null,
              borderRadius: T.brPill,
            ),
            child: Text(label,
                style: TextStyle(
                  fontSize: 14, fontWeight: FontWeight.w700,
                  color: active ? T.t1 : T.t3,
                )),
          ),
        ),
      );
}
