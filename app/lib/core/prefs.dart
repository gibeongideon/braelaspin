/// Device-local preferences.
///
/// Deliberately NOT server state. Whether this phone is muted is a property of
/// the device and the room it is in, not of the account — syncing it would
/// silence someone's laptop because they muted their phone on a bus.
///
/// Abstracted the same way TokenStore is, so core/ stays free of platform
/// channels. Dart twin of `web/src/core/prefs.ts`.
library;

const String kPrefSound = 'sound';

abstract class PrefsStore {
  bool? getBool(String key);
  Future<void> setBool(String key, bool value);
}

/// A PrefsStore that forgets everything. Used when storage is unavailable, so
/// a device with a broken Keystore still plays rather than crashing.
class MemoryPrefs implements PrefsStore {
  final Map<String, bool> _m = {};
  @override
  bool? getBool(String key) => _m[key];
  @override
  Future<void> setBool(String key, bool value) async => _m[key] = value;
}
