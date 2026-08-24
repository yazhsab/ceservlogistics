typedef JsonMap = Map<String, dynamic>;

String _string(dynamic value) => value?.toString() ?? '';
int _integer(dynamic value) =>
    value is num ? value.toInt() : int.tryParse(_string(value)) ?? 0;
double? _doubleOrNull(dynamic value) =>
    value is num ? value.toDouble() : double.tryParse(_string(value));
JsonMap _map(dynamic value) =>
    value is Map<String, dynamic> ? value : <String, dynamic>{};

class ApiFailure implements Exception {
  const ApiFailure({
    required this.code,
    required this.message,
    this.statusCode,
    this.requestId,
    this.details = const <String, dynamic>{},
    this.retryable = false,
  });

  final String code;
  final String message;
  final int? statusCode;
  final String? requestId;
  final JsonMap details;
  final bool retryable;

  @override
  String toString() => message;
}

class TokenPair {
  const TokenPair({required this.accessToken, required this.refreshToken});

  final String accessToken;
  final String refreshToken;

  factory TokenPair.fromJson(JsonMap json) => TokenPair(
    accessToken: _string(json['accessToken']),
    refreshToken: _string(json['refreshToken']),
  );
}

class UserProfile {
  const UserProfile({
    required this.id,
    required this.email,
    required this.fullName,
    required this.organizationName,
    required this.currency,
    required this.permissions,
    required this.operatingUnitIds,
  });

  final String id;
  final String email;
  final String fullName;
  final String organizationName;
  final String currency;
  final Set<String> permissions;
  final List<String> operatingUnitIds;

  bool can(String permission) => permissions.contains(permission);

  factory UserProfile.fromJson(JsonMap json) {
    final organization = _map(json['organization']);
    return UserProfile(
      id: _string(json['id']),
      email: _string(json['email']),
      fullName: _string(json['fullName']),
      organizationName: _string(organization['name']),
      currency: _string(organization['currency']),
      permissions: (json['permissions'] as List<dynamic>? ?? const <dynamic>[])
          .map(_string)
          .toSet(),
      operatingUnitIds:
          (json['operatingUnitIds'] as List<dynamic>? ?? const <dynamic>[])
              .map(_string)
              .toList(growable: false),
    );
  }
}

class LoginResult {
  const LoginResult({required this.tokens, required this.user});

  final TokenPair tokens;
  final UserProfile user;

  factory LoginResult.fromJson(JsonMap json) => LoginResult(
    tokens: TokenPair.fromJson(_map(json['tokens'])),
    user: UserProfile.fromJson(_map(json['user'])),
  );
}

class DeliveryRunSummary {
  const DeliveryRunSummary({
    required this.id,
    required this.runCode,
    required this.status,
    required this.runDate,
    required this.plannedStops,
    required this.completedStops,
    required this.deliveredCount,
    required this.failedCount,
    required this.codExpectedMinor,
    required this.codCollectedMinor,
    required this.currency,
  });

  final String id;
  final String runCode;
  final String status;
  final String runDate;
  final int plannedStops;
  final int completedStops;
  final int deliveredCount;
  final int failedCount;
  final int codExpectedMinor;
  final int codCollectedMinor;
  final String currency;

  factory DeliveryRunSummary.fromJson(JsonMap json) => DeliveryRunSummary(
    id: _string(json['id']),
    runCode: _string(json['runCode']),
    status: _string(json['status']),
    runDate: _string(json['runDate']),
    plannedStops: _integer(json['plannedStops']),
    completedStops: _integer(json['completedStops']),
    deliveredCount: _integer(json['deliveredCount']),
    failedCount: _integer(json['failedCount']),
    codExpectedMinor: _integer(json['codExpectedMinor']),
    codCollectedMinor: _integer(json['codCollectedMinor']),
    currency: _string(json['currency']),
  );
}

class DeliveryStop {
  const DeliveryStop({
    required this.id,
    required this.stopSequence,
    required this.status,
    required this.shipmentId,
    required this.awb,
    required this.pieceCount,
    required this.paymentMode,
    required this.codAmountMinor,
    required this.currency,
    required this.recipientName,
    required this.recipientPhone,
    required this.line1,
    required this.line2,
    required this.landmark,
    required this.city,
    required this.pincode,
    required this.isHeld,
    this.latitude,
    this.longitude,
  });

  final String id;
  final int stopSequence;
  final String status;
  final String shipmentId;
  final String awb;
  final int pieceCount;
  final String paymentMode;
  final int codAmountMinor;
  final String currency;
  final String recipientName;
  final String recipientPhone;
  final String line1;
  final String line2;
  final String landmark;
  final String city;
  final String pincode;
  final bool isHeld;
  final double? latitude;
  final double? longitude;

  String get address => <String>[
    line1,
    line2,
    landmark,
    '$city $pincode',
  ].where((part) => part.trim().isNotEmpty).join(', ');

  factory DeliveryStop.fromJson(JsonMap json) => DeliveryStop(
    id: _string(json['id']),
    stopSequence: _integer(json['stopSequence']),
    status: _string(json['status']),
    shipmentId: _string(json['shipmentId']),
    awb: _string(json['awb']),
    pieceCount: _integer(json['pieceCount']),
    paymentMode: _string(json['paymentMode']),
    codAmountMinor: _integer(json['codAmountMinor']),
    currency: _string(json['currency']),
    recipientName: _string(json['recipientName']),
    recipientPhone: _string(json['recipientPhone']),
    line1: _string(json['line1']),
    line2: _string(json['line2']),
    landmark: _string(json['landmark']),
    city: _string(json['city']),
    pincode: _string(json['pincode']),
    isHeld: json['isHeld'] == true,
    latitude: _doubleOrNull(json['latitude']),
    longitude: _doubleOrNull(json['longitude']),
  );
}

class DeliveryRun {
  const DeliveryRun({
    required this.id,
    required this.runCode,
    required this.status,
    required this.plannedStops,
    required this.completedStops,
    required this.deliveredCount,
    required this.failedCount,
    required this.codExpectedMinor,
    required this.codCollectedMinor,
    required this.currency,
    required this.stops,
  });

  final String id;
  final String runCode;
  final String status;
  final int plannedStops;
  final int completedStops;
  final int deliveredCount;
  final int failedCount;
  final int codExpectedMinor;
  final int codCollectedMinor;
  final String currency;
  final List<DeliveryStop> stops;

  factory DeliveryRun.fromJson(JsonMap json) => DeliveryRun(
    id: _string(json['id']),
    runCode: _string(json['runCode']),
    status: _string(json['status']),
    plannedStops: _integer(json['plannedStops']),
    completedStops: _integer(json['completedStops']),
    deliveredCount: _integer(json['deliveredCount']),
    failedCount: _integer(json['failedCount']),
    codExpectedMinor: _integer(json['codExpectedMinor']),
    codCollectedMinor: _integer(json['codCollectedMinor']),
    currency: _string(json['currency']),
    stops: (json['stops'] as List<dynamic>? ?? const <dynamic>[])
        .map((value) => DeliveryStop.fromJson(_map(value)))
        .toList(growable: false),
  );
}

class ShipmentPiece {
  const ShipmentPiece({
    required this.id,
    required this.sequence,
    required this.pieceBarcode,
    required this.reference,
  });

  final String id;
  final int sequence;
  final String pieceBarcode;
  final String reference;

  factory ShipmentPiece.fromJson(JsonMap json) => ShipmentPiece(
    id: _string(json['id']),
    sequence: _integer(json['sequence']),
    pieceBarcode: _string(json['pieceBarcode']),
    reference: _string(json['reference']),
  );
}

class ShipmentDetail {
  const ShipmentDetail({
    required this.id,
    required this.awb,
    required this.status,
    required this.pieceCount,
    required this.packages,
  });

  final String id;
  final String awb;
  final String status;
  final int pieceCount;
  final List<ShipmentPiece> packages;

  factory ShipmentDetail.fromJson(JsonMap json) => ShipmentDetail(
    id: _string(json['id']),
    awb: _string(json['awb']),
    status: _string(json['status']),
    pieceCount: _integer(json['pieceCount']),
    packages: (json['packages'] as List<dynamic>? ?? const <dynamic>[])
        .map((value) => ShipmentPiece.fromJson(_map(value)))
        .toList(growable: false),
  );
}

enum ScanOutcome { accepted, duplicate, rejected }

class ScanResult {
  const ScanResult({
    required this.barcode,
    required this.outcome,
    required this.scanId,
    required this.awb,
    required this.fromStatus,
    required this.toStatus,
    required this.rejectionCode,
    required this.rejectionMessage,
    required this.nextAction,
    required this.occurredAt,
  });

  final String barcode;
  final ScanOutcome outcome;
  final String scanId;
  final String awb;
  final String fromStatus;
  final String toStatus;
  final String rejectionCode;
  final String rejectionMessage;
  final String nextAction;
  final DateTime occurredAt;

  factory ScanResult.fromJson(JsonMap json) {
    final rawOutcome = _string(json['outcome']);
    return ScanResult(
      barcode: _string(json['barcode']),
      outcome: switch (rawOutcome) {
        'ACCEPTED' => ScanOutcome.accepted,
        'DUPLICATE' => ScanOutcome.duplicate,
        _ => ScanOutcome.rejected,
      },
      scanId: _string(json['scanId']),
      awb: _string(json['awb']),
      fromStatus: _string(json['fromStatus']),
      toStatus: _string(json['toStatus']),
      rejectionCode: _string(json['rejectionCode']),
      rejectionMessage: _string(json['rejectionMessage']),
      nextAction: _string(json['nextAction']),
      occurredAt:
          DateTime.tryParse(_string(json['occurredAt'])) ?? DateTime.now(),
    );
  }
}

class DeliveryAttemptResult {
  const DeliveryAttemptResult({
    required this.attemptId,
    required this.awb,
    required this.outcome,
    required this.ndrCaseId,
    required this.nextAction,
    required this.replayed,
  });

  final String attemptId;
  final String awb;
  final String outcome;
  final String ndrCaseId;
  final String nextAction;
  final bool replayed;

  factory DeliveryAttemptResult.fromJson(JsonMap json) => DeliveryAttemptResult(
    attemptId: _string(json['attemptId']),
    awb: _string(json['awb']),
    outcome: _string(json['outcome']),
    ndrCaseId: _string(json['ndrCaseId']),
    nextAction: _string(json['nextAction']),
    replayed: json['replayed'] == true,
  );
}

class NdrReason {
  const NdrReason({
    required this.code,
    required this.name,
    required this.isActive,
  });

  final String code;
  final String name;
  final bool isActive;

  factory NdrReason.fromJson(JsonMap json) => NdrReason(
    code: _string(json['code']),
    name: _string(json['name']),
    isActive: json['isActive'] != false,
  );
}
