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
