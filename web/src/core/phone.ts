/**
 * Kenyan mobile number normalisation.
 *
 * This is a deliberate mirror of `internal/auth/phone.go`. The server
 * re-normalises everything it receives and is the authority; this exists so the
 * user SEES the number that will actually be used before they submit, and so an
 * obviously bad number is caught without a round trip.
 *
 * Keep the two in lockstep. If the Go matrix gains a prefix, so does this.
 */

export type NormaliseResult =
  | { ok: true; phone: string }
  | { ok: false; reason: 'empty' | 'invalid' };

/**
 * Converts any accepted spelling to 254XXXXXXXXX.
 *
 *   0712345678   +254712345678   254712345678   712345678
 *   0112345678   +254 112 345 678
 *
 * Anything else is rejected rather than coerced — the reference implementation
 * appended "-need_update" to numbers it could not parse and stored them anyway,
 * which later caused duplicate accounts on deposit.
 */
export function normalisePhone(raw: string): NormaliseResult {
  if (!raw || raw.trim() === '') return { ok: false, reason: 'empty' };

  let digits = '';
  for (const ch of raw) {
    if (ch >= '0' && ch <= '9') digits += ch;
    else if ('+ -().'.includes(ch)) continue; // punctuation people type
    else return { ok: false, reason: 'invalid' }; // a letter makes it invalid
  }

  let national: string;
  if (digits.length === 12 && digits.startsWith('254')) national = digits.slice(3);
  else if (digits.length === 10 && digits[0] === '0') national = digits.slice(1);
  else if (digits.length === 9) national = digits;
  else return { ok: false, reason: 'invalid' };

  // 7 = Safaricom/Airtel, 1 = the 01x range (Airtel/Telkom).
  if (national[0] !== '7' && national[0] !== '1') return { ok: false, reason: 'invalid' };

  return { ok: true, phone: '254' + national };
}

/** 254712345678 -> "0712 345 678" */
export function prettyPhone(phone: string): string {
  if (phone.length !== 12 || !phone.startsWith('254')) return phone;
  const n = phone.slice(3);
  return `0${n.slice(0, 3)} ${n.slice(3, 6)} ${n.slice(6)}`;
}

/** 254712345678 -> "2547*****678", for anything shown to another user. */
export function maskPhone(phone: string): string {
  if (phone.length < 7) return '***';
  return phone.slice(0, 4) + '*'.repeat(phone.length - 7) + phone.slice(-3);
}
