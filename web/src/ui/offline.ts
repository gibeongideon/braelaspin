/**
 * Offline banner.
 *
 * `navigator.onLine` is famously unreliable — it reports the network
 * interface, not reachability — so this treats it as a hint only. It is
 * enough to explain a failure the player is already seeing, and the API
 * client remains the real authority on whether a request succeeded.
 */

import { h } from './dom';

export function mountOfflineBanner(): void {
  let banner: HTMLElement | null = null;

  const show = () => {
    if (banner) return;
    banner = h('div', { class: 'offline-banner', role: 'status',
      text: 'No connection — your bets are not being sent.' });
    document.body.append(banner);
  };
  const hide = () => { banner?.remove(); banner = null; };

  window.addEventListener('offline', show);
  window.addEventListener('online', hide);
  if (!navigator.onLine) show();
}
