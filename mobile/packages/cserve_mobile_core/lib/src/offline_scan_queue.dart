import 'dart:convert';

import 'package:shared_preferences/shared_preferences.dart';

import 'models.dart';

class QueuedScan {
  const QueuedScan({
    required this.eventId,
    required this.barcode,
    required this.scanType,
    required this.occurredAt,
    required this.operatingUnitId,
    this.reason,
    this.reasonCode,
    this.remarks,
    this.sortDestination,
  });

  final String eventId;
  final String barcode;
  final String scanType;
  final DateTime occurredAt;
  final String operatingUnitId;
  final String? reason;
  final String? reasonCode;
  final String? remarks;
  final String? sortDestination;

  JsonMap toJson() => <String, dynamic>{
    'eventId': eventId,
    'barcode': barcode,
    'scanType': scanType,
    'occurredAt': occurredAt.toUtc().toIso8601String(),
    'operatingUnitId': operatingUnitId,
    'reason': reason,
    'reasonCode': reasonCode,
    'remarks': remarks,
    'sortDestination': sortDestination,
  };

  factory QueuedScan.fromJson(JsonMap json) => QueuedScan(
    eventId: json['eventId']?.toString() ?? '',
    barcode: json['barcode']?.toString() ?? '',
    scanType: json['scanType']?.toString() ?? '',
    occurredAt:
        DateTime.tryParse(json['occurredAt']?.toString() ?? '') ??
        DateTime.now(),
    operatingUnitId: json['operatingUnitId']?.toString() ?? '',
    reason: json['reason']?.toString(),
    reasonCode: json['reasonCode']?.toString(),
    remarks: json['remarks']?.toString(),
    sortDestination: json['sortDestination']?.toString(),
  );
}

class OfflineScanQueue {
  static const _storageKey = 'cserve_offline_scans_v1';

  Future<List<QueuedScan>> readAll() async {
    final preferences = await SharedPreferences.getInstance();
    final values = preferences.getStringList(_storageKey) ?? const <String>[];
    return values
        .map((value) => QueuedScan.fromJson(jsonDecode(value) as JsonMap))
        .toList(growable: false);
  }

  Future<void> enqueue(QueuedScan scan) async {
    final preferences = await SharedPreferences.getInstance();
    final values = preferences.getStringList(_storageKey) ?? <String>[];
    if (values.any(
      (value) => (jsonDecode(value) as JsonMap)['eventId'] == scan.eventId,
    )) {
      return;
    }
    await preferences.setStringList(_storageKey, <String>[
      ...values,
      jsonEncode(scan.toJson()),
    ]);
  }

  Future<void> remove(String eventId) async {
    final preferences = await SharedPreferences.getInstance();
    final values = preferences.getStringList(_storageKey) ?? <String>[];
    final retained = values
        .where((value) {
          final json = jsonDecode(value) as JsonMap;
          return json['eventId'] != eventId;
        })
        .toList(growable: false);
    await preferences.setStringList(_storageKey, retained);
  }
}
