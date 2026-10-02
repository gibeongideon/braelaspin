/** Referrals — code, link, share, earnings. */

import { h, mount, Scope } from '../dom';
import { toast } from '../toast';
import { formatKes } from '../../core/money';
import { CHANNELS, inviteText, shareUrl } from '../../core/share';
import type { Store } from '../../core/store';

export function ReferScreen(store: Store): { el: HTMLElement; scope: Scope } {
  const scope = new Scope();
  const codeBox = h('div', { class: 'card' });
  const statsBox = h('div', { class: 'stats' });

  scope.add(store.user.subscribe(() => renderCode()));
  scope.add(store.referrals.subscribe(() => renderStats()));

  function renderCode() {
    const u = store.user.value;
    if (!u) return;
    mount(codeBox,
      h('h2', { class: 'h2', text: 'Your referral link' }),
      h('div', { class: 'ref-box' }, u.ref_link),
      h('div', { style: 'height:14px' }),

      // One tap per channel. WhatsApp first because it is how Kenya actually
      // shares things. The native share sheet is offered too where the browser
      // supports it, since it lists apps we cannot enumerate.
      h('div', { class: 'share-grid' },
        ...CHANNELS.map((c) =>
          h('button', {
            class: `share-btn ${c.id}`,
            type: 'button',
            'aria-label': `Share via ${c.label}`,
            onClick: async () => {
              if (c.id === 'copy') {
                try {
                  await navigator.clipboard.writeText(inviteText(u.ref_link));
                  toast('Invite copied — paste it anywhere.', { kind: 'info' });
                } catch {
                  toast('Could not copy. Select the link above instead.', { kind: 'warn' });
                }
                return;
              }
              const url = shareUrl(c.id, u.ref_link);
              if (url) window.open(url, '_blank', 'noopener,noreferrer');
            },
          },
            h('span', { class: 'g', 'aria-hidden': 'true', text: c.glyph }),
            h('span', { text: c.label }),
          ),
        ),
      ),

      navigator.share
        ? h('button', {
            class: 'btn btn-primary btn-sm', style: 'margin-top:12px',
            text: '↗ More sharing options',
            onClick: async () => {
              try {
                await navigator.share({ title: 'Braela Spin', text: inviteText(u.ref_link) });
              } catch { /* user dismissed the sheet */ }
            },
          })
        : null,

      h('p', { class: 'hint', style: 'margin-top:10px',
               text: `Code: ${u.ref_code} · You earn 2% of every bet your friends place.` }),
    );
  }

  function renderStats() {
    const r = store.referrals.value;
    mount(statsBox,
      h('div', { class: 'stat' },
        h('div', { class: 'k', text: 'Friends' }),
        h('div', { class: 'v', text: String(r.count) }),
      ),
      h('div', { class: 'stat' },
        h('div', { class: 'k', text: 'Earned' }),
        h('div', { class: 'v', text: formatKes(r.earned_cents) }),
      ),
      h('div', { class: 'stat' },
        h('div', { class: 'k', text: 'Last 30d' }),
        h('div', { class: 'v', text: formatKes(r.last_30_cents) }),
      ),
    );
  }

  renderCode();
  renderStats();

  const el = h('div', { class: 'screen' },
    h('h1', { class: 'h1', text: 'Invite friends' }),
    h('p', { class: 'sub', text: 'Earn a commission on every bet they place.' }),
    h('div', { style: 'height:14px' }),
    statsBox,
    codeBox,
    h('div', { class: 'card' },
      h('h2', { class: 'h2', text: 'How it works' }),
      ...[
        ['1', 'Share your link with friends.'],
        ['2', 'They sign up and get practice credit.'],
        ['3', 'You earn 2% of every real bet they place — win or lose.'],
      ].map(([n, t]) =>
        h('div', { class: 'row' },
          h('div', { class: 'ico', text: n }),
          h('div', { class: 'body' }, h('div', { class: 'title', text: t })),
        ),
      ),
    ),
  );

  return { el, scope };
}
