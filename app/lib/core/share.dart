/// Referral sharing.
///
/// The invite text and every share URL are built here so the two clients
/// cannot drift into inviting people differently, and so the message can be
/// reworded (or translated to Swahili) without touching either UI.
///
/// Dart twin of `web/src/core/share.ts`.
library;

enum Channel { whatsapp, sms, telegram, facebook, x, copy }

/// The invite message.
///
/// Deliberately states the practice credit rather than promising winnings. A
/// referral link that leads with "win big" is the kind of copy that gets a
/// gambling app pulled, and it is not what actually converts — the free
/// practice balance is.
String inviteText(String refLink) =>
    'Spin to win on Braela — up to 200x your bet. '
    'Sign up with my link and start with KES 10,000 in free practice credit: $refLink';

/// A short form for channels that show the URL separately.
String inviteBlurb() =>
    'Spin to win on Braela — up to 200x your bet, plus KES 10,000 free practice credit.';

/// The URL that opens a channel with the invite pre-filled, or null for
/// [Channel.copy], where the caller writes to the clipboard.
String? shareUrl(Channel channel, String refLink) {
  final full = Uri.encodeComponent(inviteText(refLink));
  final url = Uri.encodeComponent(refLink);
  final blurb = Uri.encodeComponent(inviteBlurb());

  switch (channel) {
    // wa.me without a number opens the contact picker, which is what we want.
    case Channel.whatsapp:
      return 'https://wa.me/?text=$full';
    case Channel.telegram:
      return 'https://t.me/share/url?url=$url&text=$blurb';
    case Channel.x:
      return 'https://twitter.com/intent/tweet?text=$blurb&url=$url';
    // Facebook's sharer takes only a URL; it scrapes its own preview text.
    case Channel.facebook:
      return 'https://www.facebook.com/sharer/sharer.php?u=$url';
    case Channel.sms:
      return 'sms:?body=$full';
    case Channel.copy:
      return null;
  }
}

class ChannelInfo {
  const ChannelInfo(this.id, this.label, this.glyph);
  final Channel id;
  final String label;
  final String glyph;
}

/// WhatsApp first because it is how Kenya actually shares things.
const List<ChannelInfo> kChannels = [
  ChannelInfo(Channel.whatsapp, 'WhatsApp', '💬'),
  ChannelInfo(Channel.sms, 'SMS', '✉️'),
  ChannelInfo(Channel.telegram, 'Telegram', '✈️'),
  ChannelInfo(Channel.facebook, 'Facebook', '📘'),
  ChannelInfo(Channel.x, 'X', '✖️'),
  ChannelInfo(Channel.copy, 'Copy', '📋'),
];
