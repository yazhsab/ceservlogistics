import 'dart:io';

import 'package:dio/dio.dart';

import 'models.dart';
import 'offline_scan_queue.dart';
import 'session_store.dart';

class CServeApiClient {
  CServeApiClient({required String baseUrl, required SessionStore sessionStore})
    : _sessionStore = sessionStore,
      _dio = Dio(
        BaseOptions(
          baseUrl: '${baseUrl.trim().replaceFirst(RegExp(r'/+$'), '')}/api/v1/',
          connectTimeout: const Duration(seconds: 12),
          receiveTimeout: const Duration(seconds: 20),
          sendTimeout: const Duration(seconds: 20),
          headers: const <String, String>{'Accept': 'application/json'},
        ),
      );

  final Dio _dio;
  final SessionStore _sessionStore;
  Future<void>? _refreshing;

  Future<LoginResult> login({
    required String email,
    required String password,
  }) async {
    final response = await _request(
      'auth/login',
      method: 'POST',
      authenticated: false,
      data: <String, dynamic>{'email': email.trim(), 'password': password},
    );
    final result = LoginResult.fromJson(_asMap(response.data));
    await _sessionStore.saveSession(result.tokens, result.user);
    return result;
  }

  Future<UserProfile> me() async {
    final response = await _request('auth/me');
    return UserProfile.fromJson(_asMap(response.data));
  }

  Future<void> logout() async {
    final refresh = await _sessionStore.readRefreshToken();
    try {
      await _request(
        'auth/logout',
        method: 'POST',
        data: refresh == null
            ? null
            : <String, dynamic>{'refreshToken': refresh},
      );
    } on ApiFailure {
      // Local sign-out must still work if the device is offline.
    } finally {
      await _sessionStore.clearSession();
    }
  }

  Future<List<DeliveryRunSummary>> listMyDeliveryRuns() async {
    final response = await _request(
      'delivery-runs',
      queryParameters: const <String, dynamic>{'mine': true, 'limit': 50},
    );
    final data =
        _asMap(response.data)['data'] as List<dynamic>? ?? const <dynamic>[];
    return data
        .map((value) => DeliveryRunSummary.fromJson(_asMap(value)))
        .toList(growable: false);
  }

  Future<DeliveryRun> getDeliveryRun(String runId) async {
    final response = await _request('delivery-runs/$runId');
    return DeliveryRun.fromJson(_asMap(response.data));
  }

  Future<ShipmentDetail> getShipment(String shipmentId) async {
    final response = await _request('shipments/$shipmentId');
    return ShipmentDetail.fromJson(_asMap(response.data));
  }

  Future<ScanResult> performScan({
    required String barcode,
    required String scanType,
    required String deviceId,
    required String deviceEventId,
    required DateTime occurredAt,
    String operatingUnitId = '',
    String? reason,
    String? reasonCode,
    String? remarks,
    String? sortDestination,
  }) async {
    final response = await _request(
      'scans',
      method: 'POST',
      queryParameters: operatingUnitId.isEmpty
          ? null
          : <String, dynamic>{'operatingUnitId': operatingUnitId},
      headers: <String, String>{
        'X-Device-Id': deviceId,
        'X-Device-Event-Id': deviceEventId,
      },
      data: <String, dynamic>{
        'barcode': barcode.trim(),
        'scanType': scanType,
        'occurredAt': occurredAt.toUtc().toIso8601String(),
        if (operatingUnitId.isNotEmpty) 'operatingUnitId': operatingUnitId,
        if (reason != null && reason.isNotEmpty) 'reason': reason,
        if (reasonCode != null && reasonCode.isNotEmpty)
          'reasonCode': reasonCode,
        if (remarks != null && remarks.isNotEmpty) 'remarks': remarks,
        if (sortDestination != null && sortDestination.isNotEmpty)
          'sortDestination': sortDestination,
      },
    );
    return ScanResult.fromJson(_asMap(response.data));
  }

  Future<ScanResult> replayQueuedScan(QueuedScan scan, String deviceId) =>
      performScan(
        barcode: scan.barcode,
        scanType: scan.scanType,
        deviceId: deviceId,
        deviceEventId: scan.eventId,
        occurredAt: scan.occurredAt,
        operatingUnitId: scan.operatingUnitId,
        reason: scan.reason,
        reasonCode: scan.reasonCode,
        remarks: scan.remarks,
        sortDestination: scan.sortDestination,
      );

  Future<DeliveryAttemptResult> recordDeliveryAttempt({
    required JsonMap body,
    required String deviceId,
    required String deviceEventId,
    String operatingUnitId = '',
  }) async {
    final response = await _request(
      'deliveries/attempts',
      method: 'POST',
      queryParameters: operatingUnitId.isEmpty
          ? null
          : <String, dynamic>{'operatingUnitId': operatingUnitId},
      headers: <String, String>{
        'X-Device-Id': deviceId,
        'X-Device-Event-Id': deviceEventId,
      },
      data: body,
    );
    return DeliveryAttemptResult.fromJson(_asMap(response.data));
  }

  Future<JsonMap> issueDeliveryOtp(String barcode) async {
    final response = await _request(
      'deliveries/otp',
      method: 'POST',
      data: <String, dynamic>{'barcode': barcode},
    );
    return _asMap(response.data);
  }

  Future<List<NdrReason>> listNdrReasons() async {
    final response = await _request('ndr/reasons');
    final data =
        _asMap(response.data)['data'] as List<dynamic>? ?? const <dynamic>[];
    return data
        .map((value) => NdrReason.fromJson(_asMap(value)))
        .where((reason) => reason.isActive)
        .toList(growable: false);
  }

  Future<JsonMap> submitDeliveryPhoto({
    required String barcode,
    required String recipientName,
    required String recipientRelationship,
    required String photoPath,
    required String deviceId,
    String operatingUnitId = '',
    String remarks = '',
  }) async {
    final fileName = File(photoPath).uri.pathSegments.last;
    final response = await _request(
      'pod',
      method: 'POST',
      queryParameters: operatingUnitId.isEmpty
          ? null
          : <String, dynamic>{'operatingUnitId': operatingUnitId},
      headers: <String, String>{'X-Device-Id': deviceId},
      data: FormData.fromMap(<String, dynamic>{
        'barcode': barcode,
        'podType': 'DELIVERY',
        'recipientName': recipientName,
        'recipientRelationship': recipientRelationship,
        if (remarks.isNotEmpty) 'remarks': remarks,
        'deliveredAt': DateTime.now().toUtc().toIso8601String(),
        'photo': await MultipartFile.fromFile(photoPath, filename: fileName),
      }),
    );
    return _asMap(response.data);
  }

  Future<Response<dynamic>> _request(
    String path, {
    String method = 'GET',
    bool authenticated = true,
    dynamic data,
    Map<String, dynamic>? queryParameters,
    Map<String, String>? headers,
    bool retryAfterRefresh = true,
  }) async {
    final requestHeaders = <String, String>{...?headers};
    if (authenticated) {
      final accessToken = await _sessionStore.readAccessToken();
      if (accessToken != null && accessToken.isNotEmpty) {
        requestHeaders['Authorization'] = 'Bearer $accessToken';
      }
    }

    try {
      return await _dio.request<dynamic>(
        path,
        data: data,
        queryParameters: queryParameters,
        options: Options(method: method, headers: requestHeaders),
      );
    } on DioException catch (error) {
      if (authenticated &&
          retryAfterRefresh &&
          error.response?.statusCode == 401) {
        try {
          await _refreshTokens();
          return _request(
            path,
            method: method,
            data: data,
            queryParameters: queryParameters,
            headers: headers,
            retryAfterRefresh: false,
          );
        } on ApiFailure {
          await _sessionStore.clearSession();
        }
      }
      throw _apiFailure(error);
    }
  }

  Future<void> _refreshTokens() async {
    final activeRefresh = _refreshing;
    if (activeRefresh != null) return activeRefresh;
    final future = _performRefresh();
    _refreshing = future;
    try {
      await future;
    } finally {
      _refreshing = null;
    }
  }

  Future<void> _performRefresh() async {
    final refreshToken = await _sessionStore.readRefreshToken();
    if (refreshToken == null || refreshToken.isEmpty) {
      throw const ApiFailure(
        code: 'SESSION_EXPIRED',
        message: 'Please sign in again.',
      );
    }
    try {
      final response = await _dio.post<dynamic>(
        'auth/refresh',
        data: <String, dynamic>{'refreshToken': refreshToken},
      );
      await _sessionStore.updateTokens(
        TokenPair.fromJson(_asMap(response.data)),
      );
    } on DioException catch (error) {
      throw _apiFailure(error);
    }
  }

  static JsonMap _asMap(dynamic value) {
    if (value is Map<String, dynamic>) return value;
    if (value is Map) {
      return value.map((key, item) => MapEntry(key.toString(), item));
    }
    return <String, dynamic>{};
  }

  static ApiFailure _apiFailure(DioException error) {
    final status = error.response?.statusCode;
    final envelope = _asMap(error.response?.data);
    final apiError = _asMap(envelope['error']);
    final connectionFailure =
        error.type == DioExceptionType.connectionError ||
        error.type == DioExceptionType.connectionTimeout ||
        error.type == DioExceptionType.receiveTimeout ||
        error.type == DioExceptionType.sendTimeout;
    return ApiFailure(
      code:
          apiError['code']?.toString() ??
          (connectionFailure ? 'NETWORK_UNAVAILABLE' : 'REQUEST_FAILED'),
      message:
          apiError['message']?.toString() ??
          (connectionFailure
              ? 'No connection. The operation can be retried when you are online.'
              : 'The server could not complete this request.'),
      statusCode: status,
      requestId: apiError['requestId']?.toString(),
      details: _asMap(apiError['details']),
      retryable: connectionFailure || (status != null && status >= 500),
    );
  }
}
