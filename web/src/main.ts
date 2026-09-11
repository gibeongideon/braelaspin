/**
 * Entry point. Wires the three layers together and starts the app.
 *
 *   core/  pure business logic and state  (ports to Dart)
 *   ui/    the only code that touches the DOM
 *   main   composition root
 */

import './ui/styles.css';
import { Api, type TokenStore } from './core/api';
import { Store } from './core/store';
import { mountApp } from './ui/app';
import { toast } from './ui/toast';

/** Where the API lives. Baked at build time; see .env.example. */
const API_BASE = import.meta.env.VITE_API_BASE ?? 'http://localhost:8080';

/**
 * Browser token storage.
 *
 * The ACCESS token is held in memory only: it lives 15 minutes, so persisting
 * it widens exposure for no benefit. Only the refresh token is stored, and the
 * real defence is server-side — rotation on every use, with reuse detection
 * killing the family.
 *
 * localStorage is readable by any script on this origin, so it is only as safe
 * as the app's XSS posture. The Flutter client does better (Keystore-encrypted);
 * the web equivalent would be an httpOnly cookie, which needs a backend change
 * and a CSRF story. Tracked in TODO.MD for M10.
 */
const REFRESH_KEY = 'braela.refresh';

let accessInMemory: string | null = null;

const tokens: TokenStore = {
  getAccess: () => accessInMemory,
  getRefresh: () => {
    try { return localStorage.getItem(REFRESH_KEY); } catch { return null; }
  },
  set(access, refresh) {
    accessInMemory = access;
    try { localStorage.setItem(REFRESH_KEY, refresh); } catch { /* private mode */ }
  },
  clear() {
    accessInMemory = null;
    try { localStorage.removeItem(REFRESH_KEY); } catch { /* ignore */ }
  },
};

const api = new Api({
  baseUrl: API_BASE,
  tokens,
  onAuthLost: () => {
    store.auth.value = 'signed-out';
    toast('Your session expired. Please sign in again.', { kind: 'warn' });
  },
});

const store = new Store(api, tokens);

/** Referral deep link: /r/<code> or ?ref=<code>, stashed until registration. */
(function captureReferral() {
  const fromPath = location.pathname.match(/^\/r\/([A-Z0-9]{6,10})$/i)?.[1];
  const fromQuery = new URLSearchParams(location.search).get('ref');
  const code = (fromPath ?? fromQuery)?.toUpperCase();
  if (code) {
    try { sessionStorage.setItem('braela.ref', code); } catch { /* ignore */ }
  }
})();

const root = document.getElementById('app');
if (!root) throw new Error('#app not found');

mountApp(root, store);

store.boot().catch(() => {
  store.auth.value = 'signed-out';
});

// Report unhandled failures rather than dying silently. Wire to
// POST /v1/client-error in M10.
window.addEventListener('unhandledrejection', (e) => {
  if (import.meta.env.DEV) console.error('unhandled rejection', e.reason);
});
