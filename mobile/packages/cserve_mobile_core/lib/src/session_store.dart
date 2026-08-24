import 'dart:convert';

import 'package:flutter/foundation.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:uuid/uuid.dart';

import 'models.dart';

class SessionStore {
  SessionStore({FlutterSecureStorage? secureStorage})
    : _secureStorage = secureStorage ?? const FlutterSecureStorage();

  static const _accessTokenKey = 'cserve_access_token';
  static const _refreshTokenKey = 'cserve_refresh_token';
  static const _profileKey = 'cserve_user_profile';
  static const _baseUrlKey = 'cserve_api_base_url';
  static const _deviceIdKey = 'cserve_device_id';

  final FlutterSecureStorage _secureStorage;

  Future<void> saveSession(TokenPair tokens, UserProfile profile) async {
    await Future.wait(<Future<void>>[
      _secureStorage.write(key: _accessTokenKey, value: tokens.accessToken),
      _secureStorage.write(key: _refreshTokenKey, value: tokens.refreshToken),
      _secureStorage.write(
        key: _profileKey,
        value: jsonEncode(<String, dynamic>{
          'id': profile.id,
          'email': profile.email,
          'fullName': profile.fullName,
          'organization': <String, dynamic>{
            'name': profile.organizationName,
            'currency': profile.currency,
          },
          'permissions': profile.permissions.toList(),
          'operatingUnitIds': profile.operatingUnitIds,
        }),
      ),
    ]);
  }

  Future<void> updateTokens(TokenPair tokens) async {
    await Future.wait(<Future<void>>[
      _secureStorage.write(key: _accessTokenKey, value: tokens.accessToken),
      _secureStorage.write(key: _refreshTokenKey, value: tokens.refreshToken),
    ]);
  }

  Future<String?> readAccessToken() =>
      _secureStorage.read(key: _accessTokenKey);
  Future<String?> readRefreshToken() =>
      _secureStorage.read(key: _refreshTokenKey);

  Future<UserProfile?> readProfile() async {
    final raw = await _secureStorage.read(key: _profileKey);
    if (raw == null || raw.isEmpty) return null;
    try {
      return UserProfile.fromJson(jsonDecode(raw) as JsonMap);
    } on Object {
      return null;
    }
  }

  Future<void> clearSession() async {
    await Future.wait(<Future<void>>[
      _secureStorage.delete(key: _accessTokenKey),
      _secureStorage.delete(key: _refreshTokenKey),
      _secureStorage.delete(key: _profileKey),
    ]);
  }

  Future<String> readBaseUrl() async {
    final preferences = await SharedPreferences.getInstance();
    return preferences.getString(_baseUrlKey) ??
        (defaultTargetPlatform == TargetPlatform.android
            ? 'http://10.0.2.2:8080'
            : 'http://127.0.0.1:8080');
  }

  Future<void> saveBaseUrl(String value) async {
    final preferences = await SharedPreferences.getInstance();
    await preferences.setString(
      _baseUrlKey,
      value.trim().replaceFirst(RegExp(r'/+$'), ''),
    );
  }

  Future<String> deviceId() async {
    final preferences = await SharedPreferences.getInstance();
    final existing = preferences.getString(_deviceIdKey);
    if (existing != null && existing.isNotEmpty) return existing;
    final created = 'mobile-${const Uuid().v4()}';
    await preferences.setString(_deviceIdKey, created);
    return created;
  }

  String newEventId() => const Uuid().v4();
}
