/// The API client.
///
/// Two things here are load-bearing and were proved necessary on the web:
///
///  1. SINGLE-FLIGHT REFRESH. Refresh tokens rotate, so if several concurrent
///     requests each hit an expired access token and each fire their own
///     refresh, all but one present an already-consumed token and the server
///     (correctly) answers `token_reused` and kills the session. One shared
///     in-flight refresh is not an optimisation; it is required for
///     correctness.
///
///  2. IDEMPOTENCY KEYS on every money-moving POST, generated when the user
///     acts and REUSED on retry. This is what makes "did my bet go through?"
///     answerable after a timeout.
///
/// Uses dart:io HttpClient directly — no package:http. It is one class, and
/// the dependency would cost APK bytes for an API we already have.
library;

import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'dart:math' as math;

import 'errors.dart';

/// Where tokens are kept. Abstracted so the browser and Keystore-backed
/// implementations can differ while the client stays identical.
abstract class TokenStore {
  String? get access;
  String? get refresh;
  Future<void> save(String access, String refresh);
  Future<void> clear();
}

class Api {
  Api({
    required this.baseUrl,
    required this.tokens,
    this.onAuthLost,
    this.timeout = const Duration(seconds: 15),
  });

  final String baseUrl;
  final TokenStore tokens;
  final void Function()? onAuthLost;
  final Duration timeout;

  final HttpClient _client = HttpClient()..connectionTimeout = const Duration(seconds: 10);

  /// The single in-flight refresh, shared by every caller that needs it.
  Future<bool>? _refreshing;

  Future<T> get<T>(String path, {bool auth = true}) =>
      _send<T>('GET', path, null, auth: auth);

  Future<T> post<T>(String path, [Object? body, bool auth = true, String? idempotencyKey]) =>
      _send<T>('POST', path, body, auth: auth, idempotencyKey: idempotencyKey);

  Future<T> _send<T>(
    String method,
    String path,
    Object? body, {
    bool auth = true,
    String? idempotencyKey,
    bool retried = false,
  }) async {
    final uri = Uri.parse('$baseUrl$path');
    HttpClientResponse res;
    String text;

    try {
      final req = await _client.openUrl(method, uri).timeout(timeout);
      req.headers.set(HttpHeaders.acceptHeader, 'application/json');
      if (body != null) {
        req.headers.set(HttpHeaders.contentTypeHeader, 'application/json');
      }
      if (idempotencyKey != null) {
        req.headers.set('X-Idempotency-Key', idempotencyKey);
      }
      if (auth && tokens.access != null) {
        req.headers.set(HttpHeaders.authorizationHeader, 'Bearer ${tokens.access}');
      }
      if (body != null) req.write(jsonEncode(body));

      res = await req.close().timeout(timeout);
      text = await res.transform(utf8.decoder).join().timeout(timeout);
    } on TimeoutException {
      // The request MAY have been processed. The caller must treat this as
      // unknown, never as failed.
      throw ApiException(kind: ErrorKind.timeout, code: 'timeout');
    } on SocketException {
      throw ApiException(kind: ErrorKind.offline, code: 'offline');
    } on HandshakeException {
      throw ApiException(kind: ErrorKind.offline, code: 'offline');
    }

    if (res.statusCode == 204) return null as T;

    final payload = text.isEmpty ? null : _tryDecode(text);

    if (res.statusCode >= 200 && res.statusCode < 300) return payload as T;

    // 401 -> refresh once, then replay the original request exactly.
    if (res.statusCode == 401 && auth && !retried) {
      if (await _refreshOnce()) {
        return _send<T>(method, path, body,
            auth: auth, idempotencyKey: idempotencyKey, retried: true);
      }
      await tokens.clear();
      onAuthLost?.call();
    }

    final err = (payload is Map<String, dynamic>) ? payload['error'] : null;
    throw ApiException(
      kind: _kindFor(res.statusCode),
      code: err is Map ? (err['code'] as String? ?? 'http_${res.statusCode}')
                       : 'http_${res.statusCode}',
      status: res.statusCode,
      meta: err is Map ? Map<String, dynamic>.from(err['meta'] as Map? ?? {}) : const {},
    );
  }

  /// Refresh the session, collapsing concurrent callers onto one request.
  Future<bool> _refreshOnce() {
    final inFlight = _refreshing;
    if (inFlight != null) return inFlight;

    final future = () async {
      final rt = tokens.refresh;
      if (rt == null) return false;
      try {
        final req = await _client.postUrl(Uri.parse('$baseUrl/v1/auth/refresh'));
        req.headers.set(HttpHeaders.contentTypeHeader, 'application/json');
        req.write(jsonEncode({'refresh': rt}));
        final res = await req.close().timeout(timeout);
        final text = await res.transform(utf8.decoder).join();
        if (res.statusCode != 200) return false;
        final j = jsonDecode(text) as Map<String, dynamic>;
        await tokens.save(j['access'] as String, j['refresh'] as String);
        return true;
      } catch (_) {
        return false;
      } finally {
        _refreshing = null;
      }
    }();

    _refreshing = future;
    return future;
  }

  void close() => _client.close(force: true);
}

ErrorKind _kindFor(int status) {
  if (status == 429) return ErrorKind.rateLimited;
  if (status == 401 || status == 403) return ErrorKind.auth;
  if (status >= 500) return ErrorKind.server;
  return ErrorKind.client;
}

Object? _tryDecode(String s) {
  try {
    return jsonDecode(s);
  } catch (_) {
    return null;
  }
}

/// A fresh idempotency key. Generated when the user acts, and reused verbatim
/// on retry.
String newClientRef() {
  final r = math.Random.secure();
  final b = List<int>.generate(16, (_) => r.nextInt(256));
  return b.map((x) => x.toRadixString(16).padLeft(2, '0')).join();
}
