import { describe, it, expect } from 'vitest';
import { inviteText, inviteBlurb, shareUrl, CHANNELS, type Channel } from './share';

const LINK = 'https://braelaspin.dafeapp.com/r/AB12CD34';

describe('referral sharing', () => {
  it('the invite always carries the referral link', () => {
    expect(inviteText(LINK)).toContain(LINK);
  });

  it('leads with the free practice credit, not a promise of winnings', () => {
    // Copy that promises winnings is what gets a gambling app pulled.
    const t = inviteText(LINK).toLowerCase();
    expect(t).toContain('practice credit');
    expect(t).not.toMatch(/guarantee|win big|get rich|easy money/);
  });

  it('quotes the same practice credit the server actually grants', () => {
    // DEMO_GRANT_CENTS = 1_000_000 cents = KES 10,000.
    expect(inviteText(LINK)).toContain('KES 10,000');
    expect(inviteBlurb()).toContain('KES 10,000');
  });

  it('builds a URL for every channel except copy', () => {
    for (const c of CHANNELS) {
      const u = shareUrl(c.id, LINK);
      if (c.id === 'copy') {
        expect(u).toBeNull();
      } else {
        expect(u, `${c.id} produced no URL`).toBeTruthy();
      }
    }
  });

  it('percent-encodes the link so query strings cannot be broken', () => {
    const u = shareUrl('whatsapp', LINK)!;
    // The raw "://" must not appear unencoded inside the text parameter.
    expect(u).toContain('https%3A%2F%2F');
    expect(u.startsWith('https://wa.me/?text=')).toBe(true);
  });

  it('uses schemes the platforms actually accept', () => {
    expect(shareUrl('whatsapp', LINK)).toMatch(/^https:\/\/wa\.me\//);
    expect(shareUrl('telegram', LINK)).toMatch(/^https:\/\/t\.me\/share/);
    expect(shareUrl('x', LINK)).toMatch(/^https:\/\/twitter\.com\/intent/);
    expect(shareUrl('facebook', LINK)).toMatch(/^https:\/\/www\.facebook\.com\/sharer/);
    expect(shareUrl('sms', LINK)).toMatch(/^sms:\?body=/);
  });

  it('survives a link containing characters that need escaping', () => {
    const odd = 'https://x.test/r/A+B&C=D';
    const u = shareUrl('telegram', odd)!;
    expect(u).not.toContain('A+B&C=D');   // must be encoded
    expect(u).toContain(encodeURIComponent(odd));
  });

  it('every channel has a label and a glyph for the UI', () => {
    const ids = new Set<Channel>();
    for (const c of CHANNELS) {
      expect(c.label.length).toBeGreaterThan(0);
      expect(c.glyph.length).toBeGreaterThan(0);
      expect(ids.has(c.id)).toBe(false); // no duplicates
      ids.add(c.id);
    }
  });
});

// ── every server error code must have player-facing copy ───────────────────
//
// The server sends terse stable codes and the client owns the words. A code
// with no entry falls through to a generic message, which is how a player
// retrying after a dropped connection gets told "that didn't look right"
// instead of "that spin already went through".
describe('error copy covers every code the server can emit', () => {
  // Kept in step with internal/api/*.go by hand; the list is short and a
  // missing entry is exactly what this test exists to catch.
  const SERVER_CODES = [
    'invalid_phone', 'phone_taken', 'password_too_short', 'password_too_long',
    'unknown_referral_code', 'invalid_credentials', 'account_suspended',
    'token_reused', 'unauthorized', 'forbidden', 'not_found', 'bad_request',
    'rate_limited', 'internal', 'stake_too_small', 'stake_too_large',
    'insufficient_funds', 'stake_exceeds_bankroll', 'duplicate_spin',
    'spin_in_flight', 'demo_topup_not_eligible',
  ];

  // These two ARE the generic messages by design — `bad_request` and
  // `internal` say exactly what the kind-level fallback says, so comparing
  // them against it proves nothing.
  const INTENTIONALLY_GENERIC = new Set(['bad_request', 'internal']);

  it('has specific copy for each — never the generic fallback', async () => {
    const { userMessageFor } = await import('./errors');
    const generic = userMessageFor('a_code_that_does_not_exist', {}, 'client');
    for (const code of SERVER_CODES) {
      if (INTENTIONALLY_GENERIC.has(code)) continue;
      expect(userMessageFor(code), `no copy for "${code}"`).not.toBe(generic);
    }
  });

  it('the intentionally-generic codes still resolve to something', async () => {
    const { userMessageFor } = await import('./errors');
    for (const code of INTENTIONALLY_GENERIC) {
      expect(userMessageFor(code).length).toBeGreaterThan(10);
    }
  });
});
