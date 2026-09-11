/** Sign in / sign up. One screen, two tabs. */

import { h, mount, Scope } from '../dom';
import { toast } from '../toast';
import { normalisePhone, prettyPhone } from '../../core/phone';
import { ApiError } from '../../core/errors';
import type { Store } from '../../core/store';

export function AuthScreen(store: Store): { el: HTMLElement; scope: Scope } {
  const scope = new Scope();
  let mode: 'login' | 'register' = 'login';

  const phone = h('input', {
    class: 'input', type: 'tel', inputmode: 'tel',
    placeholder: '0712 345 678', autocomplete: 'tel',
  });
  const password = h('input', {
    class: 'input', type: 'password',
    placeholder: 'At least 8 characters', autocomplete: 'current-password',
  });
  const refCode = h('input', { class: 'input', placeholder: 'Referral code (optional)' });
  const phoneHint = h('p', { class: 'hint' });
  const submit = h('button', { class: 'btn btn-primary', type: 'submit' });
  const tabs = h('div', { class: 'tabs', role: 'tablist' });
  const refField = h('div', { class: 'field' },
    h('label', { text: 'Referral code' }), refCode,
  );

  // Echo the normalised number back as they type, so they see exactly what
  // the account will be created under before they commit to it.
  phone.addEventListener('input', () => {
    const r = normalisePhone(phone.value);
    if (phone.value.trim() === '') {
      phoneHint.textContent = '';
      phone.classList.remove('err');
    } else if (r.ok) {
      phoneHint.className = 'hint';
      phoneHint.textContent = `Will use ${prettyPhone(r.phone)}`;
      phone.classList.remove('err');
    } else {
      phoneHint.className = 'hint err';
      phoneHint.textContent = 'Enter a valid Kenyan mobile number.';
      phone.classList.add('err');
    }
  });

  function renderTabs() {
    mount(tabs,
      tab('Sign in', 'login'),
      tab('Create account', 'register'),
    );
    submit.textContent = mode === 'login' ? 'Sign in' : 'Create account';
    password.setAttribute('autocomplete', mode === 'login' ? 'current-password' : 'new-password');
    refField.style.display = mode === 'register' ? '' : 'none';
  }

  function tab(label: string, value: 'login' | 'register') {
    return h('button', {
      type: 'button', text: label, role: 'tab',
      'aria-selected': String(mode === value),
      onClick: () => { mode = value; renderTabs(); },
    });
  }

  const form = h('form', {
    onSubmit: async (e: SubmitEvent) => {
      e.preventDefault();
      const p = normalisePhone(phone.value);
      if (!p.ok) {
        toast('Enter a valid Kenyan mobile number.', { kind: 'error' });
        phone.focus();
        return;
      }
      if (password.value.length < 8) {
        toast('Choose a password of at least 8 characters.', { kind: 'error' });
        password.focus();
        return;
      }

      submit.disabled = true;
      mount(submit, h('span', { class: 'spinner' }), 'Please wait…');
      try {
        if (mode === 'login') {
          await store.login(p.phone, password.value);
        } else {
          await store.register(p.phone, password.value, refCode.value.trim() || undefined);
          toast('Welcome! Practice balance added.', { kind: 'win' });
        }
      } catch (err) {
        const msg = err instanceof ApiError ? err.userMessage : 'Something went wrong.';
        toast(msg, { kind: 'error' });
      } finally {
        submit.disabled = false;
        renderTabs();
      }
    },
  },
    h('div', { class: 'field' }, h('label', { text: 'Phone number' }), phone, phoneHint),
    h('div', { class: 'field' }, h('label', { text: 'Password' }), password),
    refField,
    h('div', { style: 'height:6px' }),
    submit,
  );

  renderTabs();

  const el = h('div', { class: 'screen auth' },
    h('div', { class: 'logo' },
      h('div', { class: 'mark', text: '⚡' }),
      h('h1', { class: 'h1', text: 'Braela Spin' }),
      h('p', { class: 'sub', text: 'Spin the wheel. Win up to 200x.' }),
    ),
    tabs,
    form,
    h('p', {
      class: 'hint center', style: 'margin-top:18px',
      text: 'New accounts get KES 5,000 in practice credit. 18+. Play responsibly.',
    }),
  );

  return { el, scope };
}
