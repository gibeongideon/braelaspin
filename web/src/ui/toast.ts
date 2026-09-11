/**
 * Toasts.
 *
 * The reference implementation monkey-patched window.alert() and classified
 * messages by keyword, which is genuinely good UX hidden in a bad mechanism.
 * Here the caller states the kind, and an optional action turns a dead-end
 * error into a way forward — the insufficient-funds toast offering "Deposit"
 * is the highest-value instance of that.
 */

import { h } from './dom';

export type ToastKind = 'info' | 'win' | 'error' | 'warn';

const GLYPH: Record<ToastKind, string> = {
  info: 'ℹ️', win: '🎉', error: '⚠️', warn: '⏳',
};

let host: HTMLElement | null = null;

function getHost(): HTMLElement {
  if (!host) {
    host = h('div', { class: 'toasts', role: 'status', 'aria-live': 'polite' });
    document.body.append(host);
  }
  return host;
}

export interface ToastOptions {
  kind?: ToastKind;
  timeoutMs?: number;
  action?: { label: string; onClick: () => void };
}

export function toast(message: string, opts: ToastOptions = {}): void {
  const kind = opts.kind ?? 'info';
  // A win is worth reading; an error the user must act on should not vanish.
  const timeout = opts.timeoutMs ?? (kind === 'error' ? 6000 : 3800);

  const el = h('div', { class: `toast ${kind}` },
    h('span', { class: 'gl', 'aria-hidden': 'true', text: GLYPH[kind] }),
    h('span', { class: 'msg', text: message }),
    opts.action
      ? h('button', {
          text: opts.action.label,
          onClick: () => { opts.action!.onClick(); dismiss(); },
        })
      : null,
  );

  function dismiss() {
    el.style.transition = 'opacity .18s, transform .18s';
    el.style.opacity = '0';
    el.style.transform = 'translateY(-8px)';
    setTimeout(() => el.remove(), 180);
  }

  getHost().append(el);
  setTimeout(dismiss, timeout);
}
