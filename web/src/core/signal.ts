/**
 * Signal — the whole state primitive.
 *
 * This is deliberately a direct equivalent of Flutter's ValueNotifier, so the
 * web and mobile clients share one mental model: a handful of independent
 * observable scalars, each rendered by whatever is listening to it, with no
 * framework in between.
 *
 * That symmetry is the point. `core/` must port to Dart with the structure
 * intact, and a store built on signals maps to ValueNotifier without a rewrite.
 *
 * NOTHING IN core/ MAY TOUCH THE DOM. This file, and every other file under
 * core/, must run unchanged in a test with no browser. `npm run check:core`
 * enforces it.
 */

export type Listener<T> = (value: T) => void;
export type Unsubscribe = () => void;

export class Signal<T> {
  #value: T;
  #listeners = new Set<Listener<T>>();

  constructor(initial: T) {
    this.#value = initial;
  }

  get value(): T {
    return this.#value;
  }

  set value(next: T) {
    // Object.is so NaN and -0 behave, and so setting an identical primitive
    // does not trigger a repaint.
    if (Object.is(this.#value, next)) return;
    this.#value = next;
    this.#emit();
  }

  /**
   * Replace the value unconditionally and notify.
   * Use for objects mutated in place, where identity has not changed.
   */
  force(next: T): void {
    this.#value = next;
    this.#emit();
  }

  /** Derive the next value from the current one. */
  update(fn: (current: T) => T): void {
    this.value = fn(this.#value);
  }

  /**
   * Subscribe to changes. Returns an unsubscribe function.
   * `immediate` fires the listener once with the current value, which is what
   * a freshly-mounted view almost always wants.
   */
  subscribe(fn: Listener<T>, immediate = true): Unsubscribe {
    this.#listeners.add(fn);
    if (immediate) fn(this.#value);
    return () => {
      this.#listeners.delete(fn);
    };
  }

  #emit(): void {
    // Copy before iterating: a listener may unsubscribe itself, and mutating
    // the Set mid-iteration would skip the next listener.
    for (const fn of [...this.#listeners]) fn(this.#value);
  }
}

/**
 * Combine several signals into one derived, read-only signal.
 * Recomputes whenever any input changes.
 */
export function derived<T>(
  sources: Signal<unknown>[],
  compute: () => T,
): { readonly value: T; subscribe: (fn: Listener<T>, immediate?: boolean) => Unsubscribe } {
  const out = new Signal<T>(compute());
  for (const s of sources) {
    s.subscribe(() => {
      out.value = compute();
    }, false);
  }
  return {
    get value() {
      return out.value;
    },
    subscribe: (fn, immediate = true) => out.subscribe(fn, immediate),
  };
}
