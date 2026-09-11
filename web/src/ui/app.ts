/**
 * App shell: the auth gate, the router, and the bottom nav.
 *
 * Routing is hash-based and deliberately trivial — five screens and one deep
 * link (/r/<code>) do not justify a router library. The structure mirrors the
 * Flutter shell: an IndexedStack-equivalent of four tabs plus an auth gate.
 */

import { h, mount, clear, Scope } from './dom';
import { toast } from './toast';
import type { Store } from '../core/store';
import { AuthScreen } from './screens/auth';
import { SpinScreen } from './screens/spin';
import { WalletScreen } from './screens/wallet';
import { HistoryScreen } from './screens/history';
import { SpinsScreen } from './screens/spins';
import { ReferScreen } from './screens/refer';

export type Route = 'spin' | 'wallet' | 'spins' | 'history' | 'refer' | 'profile';

const TABS: { route: Route; label: string; glyph: string; fab?: boolean }[] = [
  { route: 'wallet',  label: 'Wallet',  glyph: '👛' },
  { route: 'spins',   label: 'Spins',   glyph: '🎡' },
  { route: 'spin',    label: 'Spin',    glyph: '⚡', fab: true },
  { route: 'history', label: 'History', glyph: '🕘' },
  { route: 'profile', label: 'Profile', glyph: '👤' },
];

export function mountApp(root: HTMLElement, store: Store): void {
  const content = h('div', { class: 'shell' });
  const nav = h('nav', { class: 'nav', 'aria-label': 'Main' });
  let current: Route = 'spin';
  let scope: Scope | null = null;

  root.append(content, nav);

  function go(route: Route) {
    current = route;
    location.hash = route;
    render();
  }

  function renderNav(signedIn: boolean) {
    nav.style.display = signedIn ? '' : 'none';
    if (!signedIn) return;
    mount(nav, ...TABS.map((t) =>
      h('button', {
        class: t.fab ? 'fab' : '',
        'aria-current': current === t.route ? 'page' : undefined,
        onClick: () => go(t.route),
      },
        h('span', { class: 'gl', 'aria-hidden': 'true', text: t.glyph }),
        h('span', { text: t.label }),
      ),
    ));
  }

  function render() {
    // Detaching the old screen's subscriptions is what stops listeners piling
    // up on every navigation until dead views are repainting.
    scope?.dispose();
    scope = null;
    clear(content);

    const state = store.auth.value;
    if (state === 'unknown') {
      content.append(h('div', { class: 'screen center', style: 'padding-top:40vh' },
        h('div', { class: 'spinner', style: 'margin:0 auto' })));
      renderNav(false);
      return;
    }

    if (state === 'signed-out') {
      const s = AuthScreen(store);
      scope = s.scope;
      content.append(s.el);
      renderNav(false);
      return;
    }

    const screen =
      current === 'wallet'  ? WalletScreen(store)
    : current === 'spins'   ? SpinsScreen(store)
    : current === 'history' ? HistoryScreen(store)
    : current === 'refer'   ? ReferScreen(store)
    : current === 'profile' ? ProfileScreen(store, go)
    : SpinScreen(store, (r) => go(r as Route));

    scope = screen.scope;
    content.append(screen.el);
    renderNav(true);
  }

  store.auth.subscribe(() => render(), false);

  window.addEventListener('hashchange', () => {
    const r = location.hash.slice(1) as Route;
    if (TABS.some((t) => t.route === r) && r !== current) {
      current = r;
      render();
    }
  });

  // The API is the source of truth; refresh whenever the tab comes back.
  document.addEventListener('visibilitychange', () => {
    if (!document.hidden && store.auth.value === 'signed-in') {
      store.refreshMe().catch(() => {});
    }
  });

  const initial = location.hash.slice(1) as Route;
  if (TABS.some((t) => t.route === initial)) current = initial;

  render();
}

/** Small enough to live here rather than in its own file. */
function ProfileScreen(store: Store, go: (r: Route) => void): { el: HTMLElement; scope: Scope } {
  const scope = new Scope();
  const u = store.user.value;

  const el = h('div', { class: 'screen' },
    h('h1', { class: 'h1', text: 'Profile' }),
    h('div', { style: 'height:14px' }),
    h('div', { class: 'card' },
      h('div', { class: 'row' },
        h('div', { class: 'ico', text: '📱' }),
        h('div', { class: 'body' },
          h('div', { class: 'title', text: u?.phone_display ?? '—' }),
          h('div', { class: 'meta',
                     text: u?.phone_verified ? 'Verified' : 'Not verified' }),
        ),
      ),
      // Invite lost its tab slot to Spins, so it is reached from here.
      h('button', {
        class: 'row', style: 'width:100%;text-align:left;background:none',
        onClick: () => go('refer'),
      },
        h('div', { class: 'ico', text: '🎟' }),
        h('div', { class: 'body' },
          h('div', { class: 'title', text: 'Invite friends' }),
          h('div', { class: 'meta', text: `Your code: ${u?.ref_code ?? '—'} · earn 2%` }),
        ),
        h('div', { class: 'muted', text: '›' }),
      ),
    ),
    h('div', { class: 'card' },
      h('button', {
        class: 'btn btn-ghost', text: 'Sign out',
        onClick: async () => {
          await store.logout();
          toast('Signed out.', { kind: 'info' });
        },
      }),
    ),
    h('p', { class: 'hint center', style: 'margin-top:20px',
             text: '18+. Play responsibly. Gambling can be addictive.' }),
  );

  return { el, scope };
}
