/**
 * Money and time formatting.
 *
 * Hand-written rather than Intl/`intl` package, for the same reason the Flutter
 * client avoids `package:intl`: we need exactly two formats, and the locale
 * tables cost more than the feature is worth. Mirrors `lib/core/fmt.dart`.
 */

import type { Cents, BasisPoints } from './types';

/** "KES 1,234.50" — the canonical display form. */
export function formatKes(cents: Cents, opts: { symbol?: boolean } = {}): string {
  const symbol = opts.symbol !== false;
  const negative = cents < 0;
  const abs = Math.abs(Math.trunc(cents));

  const shillings = Math.floor(abs / 100);
  const rem = abs % 100;

  let s = groupThousands(shillings);
  // Whole amounts read better without ".00"; partial amounts need the cents.
  if (rem !== 0) s += '.' + String(rem).padStart(2, '0');

  return (negative ? '-' : '') + (symbol ? 'KES ' : '') + s;
}

/** Signed form for ledger rows: "+KES 500" / "-KES 20". */
export function formatSigned(cents: Cents): string {
  const sign = cents > 0 ? '+' : cents < 0 ? '-' : '';
  return sign + formatKes(Math.abs(cents));
}

function groupThousands(n: number): string {
  const s = String(n);
  if (s.length <= 3) return s;
  let out = '';
  for (let i = 0; i < s.length; i++) {
    if (i > 0 && (s.length - i) % 3 === 0) out += ',';
    out += s[i];
  }
  return out;
}

/**
 * Parse user input in SHILLINGS into cents.
 * Returns null for anything unparseable — never a silent 0, which on a stake
 * field is the difference between a rejection and a wrong bet.
 */
export function parseShillings(input: string): Cents | null {
  const t = input.trim().replace(/,/g, '').replace(/^KES\s*/i, '');
  if (t === '' || !/^\d+(\.\d{1,2})?$/.test(t)) return null;
  const value = Math.round(parseFloat(t) * 100);
  return Number.isFinite(value) ? value : null;
}

/** "2x", "200x", "—" for a losing segment. */
export function formatMultiplier(bp: BasisPoints): string {
  if (bp === 0) return '—';
  const x = bp / 10000;
  return (Number.isInteger(x) ? String(x) : x.toFixed(1)) + 'x';
}

/** Basis points as a percentage: 9000 -> "90%". */
export function formatBp(bp: BasisPoints): string {
  const pct = bp / 100;
  return (Number.isInteger(pct) ? String(pct) : pct.toFixed(2)) + '%';
}

/** "just now" / "5 min ago" / "3 h ago" / "12 Sep". */
export function formatWhen(iso: string, now: Date = new Date()): string {
  const then = new Date(iso);
  if (Number.isNaN(then.getTime())) return '';

  const secs = Math.floor((now.getTime() - then.getTime()) / 1000);
  if (secs < 45) return 'just now';
  if (secs < 3600) return `${Math.floor(secs / 60)} min ago`;
  if (secs < 86400) return `${Math.floor(secs / 3600)} h ago`;
  if (secs < 7 * 86400) return `${Math.floor(secs / 86400)} d ago`;

  const months = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun',
                  'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec'];
  const d = `${then.getDate()} ${months[then.getMonth()]}`;
  return then.getFullYear() === now.getFullYear()
    ? d
    : `${d} ${then.getFullYear()}`;
}
