/**
 * Device-local preferences.
 *
 * Deliberately NOT server state. Whether this phone is muted is a property of
 * the device and the room it is in, not of the account — syncing it would mean
 * silencing someone's laptop because they muted their phone on a bus.
 *
 * Abstracted the same way TokenStore is, so core/ stays free of localStorage
 * and the Flutter client can back it with SharedPreferences.
 *
 * PURE. Mirrors to `lib/core/prefs.dart`.
 */

export interface PrefsStore {
  getBool(key: string): boolean | null;
  setBool(key: string, value: boolean): void;
}

export const PREF_SOUND = 'sound';

/** A PrefsStore that forgets everything. Used when storage is unavailable. */
export class MemoryPrefs implements PrefsStore {
  #m = new Map<string, boolean>();
  getBool(k: string) {
    return this.#m.has(k) ? this.#m.get(k)! : null;
  }
  setBool(k: string, v: boolean) {
    this.#m.set(k, v);
  }
}
