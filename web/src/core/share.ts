/**
 * Referral sharing.
 *
 * The invite text and every share URL are built here, in one place, so the two
 * clients cannot drift into inviting people differently — and so the message
 * can be reworded (or translated to Swahili) without touching either UI.
 *
 * PURE. Mirrors to `lib/core/share.dart`.
 */

export type Channel = 'whatsapp' | 'facebook' | 'x' | 'telegram' | 'sms' | 'copy';

/**
 * The invite message.
 *
 * Deliberately states the practice credit rather than promising winnings. A
 * referral link that leads with "win big" is the kind of copy that gets a
 * gambling app pulled, and it is not what actually converts — the free
 * practice balance is.
 */
export function inviteText(refLink: string): string {
  return `Spin to win on Braela — up to 200x your bet. ` +
    `Sign up with my link and start with KES 10,000 in free practice credit: ${refLink}`;
}

/** A short form for channels that show the URL separately. */
export function inviteBlurb(): string {
  return 'Spin to win on Braela — up to 200x your bet, plus KES 10,000 free practice credit.';
}

/**
 * The URL that opens a given channel with the invite pre-filled.
 *
 * WhatsApp first because it is how Kenya actually shares things; the rest
 * follow. `copy` has no URL — the caller writes to the clipboard.
 */
export function shareUrl(channel: Channel, refLink: string): string | null {
  const full = encodeURIComponent(inviteText(refLink));
  const url = encodeURIComponent(refLink);
  const blurb = encodeURIComponent(inviteBlurb());

  switch (channel) {
    // wa.me without a number opens the contact picker, which is what we want.
    case 'whatsapp': return `https://wa.me/?text=${full}`;
    case 'telegram': return `https://t.me/share/url?url=${url}&text=${blurb}`;
    case 'x':        return `https://twitter.com/intent/tweet?text=${blurb}&url=${url}`;
    // Facebook's sharer takes only a URL; it scrapes its own preview text.
    case 'facebook': return `https://www.facebook.com/sharer/sharer.php?u=${url}`;
    case 'sms':      return `sms:?body=${full}`;
    case 'copy':     return null;
  }
}

export const CHANNELS: { id: Channel; label: string; glyph: string }[] = [
  { id: 'whatsapp', label: 'WhatsApp', glyph: '💬' },
  { id: 'sms',      label: 'SMS',      glyph: '✉️' },
  { id: 'telegram', label: 'Telegram', glyph: '✈️' },
  { id: 'facebook', label: 'Facebook', glyph: '📘' },
  { id: 'x',        label: 'X',        glyph: '✖️' },
  { id: 'copy',     label: 'Copy',     glyph: '📋' },
];
