/**
 * The API client.
 *
 * Two things here are load-bearing and must survive the Flutter port:
 *
 *  1. SINGLE-FLIGHT REFRESH. Refresh tokens rotate, so if four concurrent
 *     requests each hit an expired access token and each fire their own
 *     refresh, three of them present an already-consumed token — and the server
 *     (correctly) answers `token_reused` and kills the session. One shared
 *     in-flight refresh is not an optimisation; it is required for correctness.
 *
 *  2. IDEMPOTENCY KEYS on every money-moving POST, generated when the user
 *     acts and REUSED on retry. This is what makes "did my bet go through?"
 *     answerable after a timeout.
 *
 * Mirrors `lib/net/api.dart`.
 */

import { ApiError } from './errors';
import type { ApiErrorBody } from './types';

/** Where tokens are kept. Abstracted so the Flutter port can swap in Keystore. */
export interface TokenStore {
  getAccess(): string | null;
  getRefresh(): string | null;
  set(access: string, refresh: string): void;
  clear(): void;
}

export interface ApiOptions {
  baseUrl: string;
  tokens: TokenStore;
  /** Called when the session is unrecoverable and the user must sign in. */
  onAuthLost?: () => void;
  timeoutMs?: number;
}

interface RequestOptions {
  auth?: boolean;
  idempotencyKey?: string;
  /** Internal: prevents an infinite refresh loop. */
  _retried?: boolean;
}

export class Api {
  #baseUrl: string;
  #tokens: TokenStore;
  #onAuthLost?: () => void;
  #timeoutMs: number;
  /** The single in-flight refresh, shared by every caller that needs it. */
  #refreshing: Promise<boolean> | null = null;

  constructor(opts: ApiOptions) {
    this.#baseUrl = opts.baseUrl.replace(/\/+$/, '');
    this.#tokens = opts.tokens;
    this.#onAuthLost = opts.onAuthLost;
    this.#timeoutMs = opts.timeoutMs ?? 15000;
  }

  get<T>(path: string, opts: RequestOptions = {}): Promise<T> {
    return this.#send<T>('GET', path, undefined, opts);
  }

  post<T>(path: string, body?: unknown, opts: RequestOptions = {}): Promise<T> {
    return this.#send<T>('POST', path, body, opts);
  }

  patch<T>(path: string, body?: unknown, opts: RequestOptions = {}): Promise<T> {
    return this.#send<T>('PATCH', path, body, opts);
  }

  async #send<T>(
    method: string,
    path: string,
    body: unknown,
    opts: RequestOptions,
  ): Promise<T> {
    const auth = opts.auth !== false;
    const headers: Record<string, string> = { Accept: 'application/json' };
    if (body !== undefined) headers['Content-Type'] = 'application/json';
    if (opts.idempotencyKey) headers['X-Idempotency-Key'] = opts.idempotencyKey;

    if (auth) {
      const token = this.#tokens.getAccess();
      if (token) headers.Authorization = `Bearer ${token}`;
    }

    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), this.#timeoutMs);

    let res: Response;
    try {
      res = await fetch(this.#baseUrl + path, {
        method,
        headers,
        body: body === undefined ? undefined : JSON.stringify(body),
        signal: controller.signal,
      });
    } catch (e) {
      clearTimeout(timer);
      // An abort means the request MAY have been processed — the caller must
      // treat it as unknown, not failed. A network error before send is safe.
      if (e instanceof DOMException && e.name === 'AbortError') {
        throw new ApiError({ kind: 'timeout', code: 'timeout' });
      }
      throw new ApiError({ kind: 'offline', code: 'offline' });
    } finally {
      clearTimeout(timer);
    }

    if (res.status === 204) return undefined as T;

    const requestId = res.headers.get('X-Request-Id') ?? undefined;
    const text = await res.text();
    const payload = text ? safeParse(text) : null;

    if (res.ok) return payload as T;

    // 401 -> refresh once, then replay the original request exactly.
    if (res.status === 401 && auth && !opts._retried) {
      const refreshed = await this.#refreshOnce();
      if (refreshed) {
        return this.#send<T>(method, path, body, { ...opts, _retried: true });
      }
      this.#tokens.clear();
      this.#onAuthLost?.();
    }

    const err = (payload as ApiErrorBody | null)?.error;
    throw new ApiError({
      kind: kindForStatus(res.status),
      code: err?.code ?? `http_${res.status}`,
      status: res.status,
      message: err?.message,
      meta: err?.meta,
      requestId: err?.request_id ?? requestId,
    });
  }

  /**
   * Refresh the session, collapsing concurrent callers onto one request.
   * Everyone awaits the same promise; only one token is ever consumed.
   */
  #refreshOnce(): Promise<boolean> {
    if (this.#refreshing) return this.#refreshing;

    this.#refreshing = (async () => {
      const refresh = this.#tokens.getRefresh();
      if (!refresh) return false;
      try {
        const res = await fetch(this.#baseUrl + '/v1/auth/refresh', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ refresh }),
        });
        if (!res.ok) return false;
        const next = (await res.json()) as { access: string; refresh: string };
        this.#tokens.set(next.access, next.refresh);
        return true;
      } catch {
        return false;
      } finally {
        // Cleared inside the same promise so the next 401 starts a fresh one.
        this.#refreshing = null;
      }
    })();

    return this.#refreshing;
  }
}

function kindForStatus(status: number) {
  if (status === 429) return 'rate_limited' as const;
  if (status === 401 || status === 403) return 'auth' as const;
  if (status >= 500) return 'server' as const;
  return 'client' as const;
}

function safeParse(text: string): unknown {
  try {
    return JSON.parse(text);
  } catch {
    return null;
  }
}

/**
 * A fresh idempotency key. Generated when the user acts, persisted alongside
 * the pending operation, and reused verbatim on retry.
 */
export function newClientRef(): string {
  const bytes = new Uint8Array(16);
  crypto.getRandomValues(bytes);
  return Array.from(bytes, (b) => b.toString(16).padStart(2, '0')).join('');
}
