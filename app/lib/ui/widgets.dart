/// Small shared widgets. Hand-rolled rather than Material, because the app
/// needs a button, a card and a text field, and `uses-material-design: false`
/// keeps the icon font out of the APK.
library;

import 'package:flutter/services.dart';
import 'package:flutter/widgets.dart';

import 'theme.dart';

class AppButton extends StatelessWidget {
  const AppButton({
    super.key,
    required this.label,
    this.onPressed,
    this.primary = true,
    this.busy = false,
    this.compact = false,
  });

  final String label;
  final VoidCallback? onPressed;
  final bool primary;
  final bool busy;
  final bool compact;

  @override
  Widget build(BuildContext context) {
    final enabled = onPressed != null && !busy;
    return Opacity(
      opacity: enabled ? 1 : 0.45,
      child: GestureDetector(
        onTap: enabled
            ? () {
                HapticFeedback.lightImpact();
                onPressed!();
              }
            : null,
        child: Container(
          height: compact ? 42 : 54,
          alignment: Alignment.center,
          decoration: BoxDecoration(
            gradient: primary ? T.gradAmber : null,
            color: primary ? null : T.surface,
            border: primary ? null : Border.all(color: T.border2),
            borderRadius: T.brPill,
            boxShadow: primary
                ? const [BoxShadow(color: Color(0x59F26B21), blurRadius: 26, offset: Offset(0, 10))]
                : null,
          ),
          child: busy
              ? const _Spinner()
              : Text(
                  primary ? label.toUpperCase() : label,
                  style: TextStyle(
                    fontSize: compact ? 13 : 15,
                    fontWeight: FontWeight.w800,
                    color: T.t1,
                    letterSpacing: primary ? 0.6 : 0,
                  ),
                ),
        ),
      ),
    );
  }
}

class _Spinner extends StatefulWidget {
  const _Spinner();
  @override
  State<_Spinner> createState() => _SpinnerState();
}

class _SpinnerState extends State<_Spinner> with SingleTickerProviderStateMixin {
  late final AnimationController c =
      AnimationController(vsync: this, duration: const Duration(milliseconds: 700))..repeat();

  @override
  void dispose() {
    c.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => RotationTransition(
        turns: c,
        child: Container(
          width: 18,
          height: 18,
          decoration: const BoxDecoration(
            shape: BoxShape.circle,
            border: Border(
              top: BorderSide(color: T.t1, width: 2),
              left: BorderSide(color: Color(0x40FFFFFF), width: 2),
              right: BorderSide(color: Color(0x40FFFFFF), width: 2),
              bottom: BorderSide(color: Color(0x40FFFFFF), width: 2),
            ),
          ),
        ),
      );
}

class Card extends StatelessWidget {
  const Card({super.key, required this.child, this.padding = const EdgeInsets.all(15)});
  final Widget child;
  final EdgeInsets padding;

  @override
  Widget build(BuildContext context) => Container(
        padding: padding,
        decoration: BoxDecoration(
          color: T.surface,
          border: Border.all(color: T.border),
          borderRadius: T.brLg,
        ),
        child: child,
      );
}

class Field extends StatefulWidget {
  const Field({
    super.key,
    required this.controller,
    required this.label,
    this.hint,
    this.obscure = false,
    this.keyboard = TextInputType.text,
    this.error,
    this.helper,
    this.onChanged,
  });

  final TextEditingController controller;
  final String label;
  final String? hint;
  final bool obscure;
  final TextInputType keyboard;
  final String? error;
  final String? helper;
  final ValueChanged<String>? onChanged;

  @override
  State<Field> createState() => _FieldState();
}

class _FieldState extends State<Field> {
  // Owned here rather than constructed inline in build(): a FocusNode created
  // per frame leaks its listeners and never releases the keyboard cleanly.
  final FocusNode _node = FocusNode();

  @override
  void dispose() {
    _node.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Text(widget.label,
            style: const TextStyle(fontSize: 12, color: T.t3, fontWeight: FontWeight.w600)),
        const SizedBox(height: 6),
        GestureDetector(
          onTap: () => _node.requestFocus(),
          child: Container(
            padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 13),
            decoration: BoxDecoration(
              color: const Color(0x47000000),
              border: Border.all(color: widget.error != null ? T.red : T.border),
              borderRadius: T.brR,
            ),
            child: EditableText(
              controller: widget.controller,
              focusNode: _node,
              // >=16 stops the keyboard shrinking the layout on small screens.
              style: const TextStyle(fontSize: 16, color: T.t1),
              cursorColor: T.amber,
              backgroundCursorColor: T.t4,
              obscureText: widget.obscure,
              keyboardType: widget.keyboard,
              onChanged: widget.onChanged,
              selectionColor: const Color(0x66F26B21),
            ),
          ),
        ),
        if (widget.error != null || widget.helper != null) ...[
          const SizedBox(height: 5),
          Text(widget.error ?? widget.helper!,
              style: TextStyle(fontSize: 12, color: widget.error != null ? T.red : T.t3)),
        ],
      ],
    );
  }
}

/// A row in a list — icon, title, meta, trailing amount.
class Row2 extends StatelessWidget {
  const Row2({super.key, required this.icon, required this.title, this.meta, this.trailing, this.trailingColor});
  final String icon;
  final String title;
  final String? meta;
  final String? trailing;
  final Color? trailingColor;

  @override
  Widget build(BuildContext context) => Container(
        padding: const EdgeInsets.symmetric(vertical: 12),
        decoration: const BoxDecoration(
          border: Border(bottom: BorderSide(color: T.border)),
        ),
        child: Row(
          children: [
            Container(
              width: 34, height: 34,
              alignment: Alignment.center,
              decoration: const BoxDecoration(color: T.surface2, borderRadius: T.brR),
              child: Text(icon, style: const TextStyle(fontSize: 14)),
            ),
            const SizedBox(width: 11),
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(title, style: const TextStyle(fontSize: 14, fontWeight: FontWeight.w600, color: T.t1)),
                  if (meta != null) ...[
                    const SizedBox(height: 1),
                    Text(meta!, style: T.hint),
                  ],
                ],
              ),
            ),
            if (trailing != null)
              Text(trailing!,
                  style: TextStyle(
                    fontSize: 14, fontWeight: FontWeight.w700,
                    color: trailingColor ?? T.t2,
                    fontFeatures: const [FontFeature.tabularFigures()],
                  )),
          ],
        ),
      );
}

class EmptyState extends StatelessWidget {
  const EmptyState({super.key, required this.glyph, required this.title, this.hint, this.onRetry});
  final String glyph;
  final String title;
  final String? hint;
  final VoidCallback? onRetry;

  @override
  Widget build(BuildContext context) => Padding(
        padding: const EdgeInsets.symmetric(vertical: 40, horizontal: 20),
        child: Column(
          children: [
            Opacity(opacity: 0.5, child: Text(glyph, style: const TextStyle(fontSize: 32))),
            const SizedBox(height: 10),
            Text(title, style: T.sub, textAlign: TextAlign.center),
            if (hint != null) ...[
              const SizedBox(height: 4),
              Text(hint!, style: T.hint, textAlign: TextAlign.center),
            ],
            if (onRetry != null) ...[
              const SizedBox(height: 14),
              SizedBox(width: 140, child: AppButton(label: 'Try again', primary: false, compact: true, onPressed: onRetry)),
            ],
          ],
        ),
      );
}

/// Toast. The caller states the kind; an optional action turns a dead-end
/// error into a way forward, which is the highest-value piece of UX here.
void showToast(
  BuildContext context,
  String message, {
  String kind = 'info',
  ({String label, VoidCallback onTap})? action,
}) {
  final overlay = Overlay.maybeOf(context);
  if (overlay == null) return;

  final colour = switch (kind) {
    'win' => T.greenHi,
    'error' => T.red,
    'warn' => T.gold,
    _ => T.border2,
  };
  final glyph = switch (kind) {
    'win' => '🎉',
    'error' => '⚠️',
    'warn' => '⏳',
    _ => 'ℹ️',
  };

  late final OverlayEntry entry;
  entry = OverlayEntry(
    builder: (ctx) => Positioned(
      top: MediaQuery.of(ctx).padding.top + 12,
      left: 16, right: 16,
      child: Container(
        padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 12),
        decoration: BoxDecoration(
          color: const Color(0xFF241A20),
          border: Border.all(color: colour),
          borderRadius: T.brR,
          boxShadow: const [BoxShadow(color: Color(0xCC000000), blurRadius: 34, offset: Offset(0, 14))],
        ),
        child: Row(
          children: [
            Text(glyph, style: const TextStyle(fontSize: 17)),
            const SizedBox(width: 10),
            Expanded(child: Text(message, style: const TextStyle(fontSize: 14, color: T.t1))),
            if (action != null)
              GestureDetector(
                onTap: () {
                  entry.remove();
                  action.onTap();
                },
                child: Padding(
                  padding: const EdgeInsets.only(left: 10),
                  child: Text(action.label,
                      style: const TextStyle(fontSize: 13, fontWeight: FontWeight.w700, color: T.amberHi)),
                ),
              ),
          ],
        ),
      ),
    ),
  );

  overlay.insert(entry);
  // Errors the user must act on stay longer than a confirmation.
  Future<void>.delayed(Duration(milliseconds: kind == 'error' ? 6000 : 3800), () {
    if (entry.mounted) entry.remove();
  });
}
