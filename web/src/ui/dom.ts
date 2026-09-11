/**
 * A ~50-line DOM helper instead of a framework.
 *
 * The app is five screens whose entire state is eleven signals. A virtual DOM
 * would be more machinery than the problem needs, and this keeps the structure
 * close to the Flutter widget tree it will be ported to.
 */

type Child = Node | string | number | null | undefined | false;

export interface Attrs {
  class?: string;
  text?: string;
  html?: string;
  onClick?: (e: MouseEvent) => void;
  onInput?: (e: Event) => void;
  onSubmit?: (e: SubmitEvent) => void;
  [key: string]: unknown;
}

/** h('div', {class:'card'}, 'hello') */
export function h<K extends keyof HTMLElementTagNameMap>(
  tag: K,
  attrs: Attrs = {},
  ...children: Child[]
): HTMLElementTagNameMap[K] {
  const el = document.createElement(tag);

  for (const [k, v] of Object.entries(attrs)) {
    if (v === null || v === undefined || v === false) continue;

    if (k === 'class') el.className = String(v);
    else if (k === 'text') el.textContent = String(v);
    else if (k === 'html') el.innerHTML = String(v);
    else if (k === 'onClick') el.addEventListener('click', v as EventListener);
    else if (k === 'onInput') el.addEventListener('input', v as EventListener);
    else if (k === 'onSubmit') el.addEventListener('submit', v as EventListener);
    else if (k.startsWith('on') && typeof v === 'function') {
      el.addEventListener(k.slice(2).toLowerCase(), v as EventListener);
    } else if (v === true) el.setAttribute(k, '');
    else el.setAttribute(k, String(v));
  }

  for (const c of children.flat()) {
    if (c === null || c === undefined || c === false) continue;
    el.append(c instanceof Node ? c : document.createTextNode(String(c)));
  }
  return el;
}

export function clear(el: Element): void {
  el.replaceChildren();
}

export function mount(parent: Element, ...nodes: Child[]): void {
  clear(parent);
  for (const n of nodes) {
    if (n === null || n === undefined || n === false) continue;
    parent.append(n instanceof Node ? n : document.createTextNode(String(n)));
  }
}

/**
 * Collects unsubscribe functions for a screen so leaving it detaches every
 * listener. Without this, subscriptions accumulate on every navigation and the
 * app slowly starts repainting dead views.
 */
export class Scope {
  #cleanups: (() => void)[] = [];

  add(fn: () => void): void {
    this.#cleanups.push(fn);
  }

  dispose(): void {
    for (const fn of this.#cleanups.splice(0)) fn();
  }
}
